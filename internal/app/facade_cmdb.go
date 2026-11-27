package app

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/kinstall/kinstall/internal/authz"
	"github.com/kinstall/kinstall/internal/domain"
	"github.com/kinstall/kinstall/internal/inventory"
	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
)

// InlineCredentialSpec allows creating a Vault SSH credential inline during bulk host import.
type InlineCredentialSpec struct {
	Name      string `json:"name"`
	Kind      string `json:"kind"` // ssh_password | ssh_key
	Plaintext string `json:"plaintext"`
}

// HostImportOptions configures universal CMDB bulk import across xlsx/csv/yaml/json/ansible.
type HostImportOptions struct {
	Format              string                `json:"format"`
	Raw                 []byte                `json:"-"`
	DryRun              bool                  `json:"dryRun"`
	ColumnMap           map[string]string     `json:"columnMap,omitempty"`
	DefaultCredentialID string                `json:"defaultCredentialId,omitempty"`
	InlineCredential    *InlineCredentialSpec `json:"inlineCredential,omitempty"`
	DefaultBiz          string                `json:"defaultBiz,omitempty"`
	DefaultSet          string                `json:"defaultSet,omitempty"`
	DefaultModule       string                `json:"defaultModule,omitempty"`
	DefaultRegion       string                `json:"defaultRegion,omitempty"`
	DefaultSecurityArea string                `json:"defaultSecurityArea,omitempty"`
	DefaultZone         string                `json:"defaultZone,omitempty"`
	UpdateExisting      bool                  `json:"updateExisting"`
	SanitizeHostname    bool                  `json:"sanitizeHostname"`
}

// ImportHostsWithOptions parses, dry-run validates, and imports/upserts CMDB hosts with full topology & optional inline credential creation.
func (f *Facade) ImportHostsWithOptions(ctx context.Context, actor domain.Actor, projectID string, opts HostImportOptions) (*inventory.ImportResult, int, int, error) {
	if err := authz.Authorize(actor, projectID, "host.write"); err != nil {
		return nil, 0, 0, err
	}
	raw := opts.Raw
	if strings.EqualFold(strings.TrimSpace(opts.Format), "xlsx") {
		if dec, derr := base64.StdEncoding.DecodeString(strings.TrimSpace(string(raw))); derr == nil && len(dec) > 4 {
			raw = dec
		}
	}
	res, err := inventory.ParseHosts(opts.Format, raw, inventory.ParseOptions{
		ColumnMap:        opts.ColumnMap,
		SanitizeHostname: opts.SanitizeHostname,
	})
	if err != nil {
		return nil, 0, 0, sdk.NewError(sdk.CodeAPIInvalidRequest, "HOST", http.StatusBadRequest, sdk.CodeAPIInvalidRequest, false, err)
	}

	// Apply default CMDB topology & zone values to parsed rows
	for i := range res.Rows {
		if res.Rows[i].Biz == "" {
			res.Rows[i].Biz = strings.TrimSpace(opts.DefaultBiz)
		}
		if res.Rows[i].Set == "" {
			res.Rows[i].Set = strings.TrimSpace(opts.DefaultSet)
		}
		if res.Rows[i].Module == "" && res.Rows[i].Group == "" {
			res.Rows[i].Module = strings.TrimSpace(opts.DefaultModule)
		}
		if res.Rows[i].Region == "" {
			res.Rows[i].Region = strings.TrimSpace(opts.DefaultRegion)
		}
		if res.Rows[i].SecurityArea == "" {
			res.Rows[i].SecurityArea = strings.TrimSpace(opts.DefaultSecurityArea)
		}
		if res.Rows[i].Zone == "" {
			res.Rows[i].Zone = strings.TrimSpace(opts.DefaultZone)
		}
	}

	existingHosts, _ := f.store.Hosts().ListByProject(ctx, projectID)
	existingByName := make(map[string]domain.Host, len(existingHosts))
	existingByAddrPort := make(map[string]domain.Host, len(existingHosts))
	for _, eh := range existingHosts {
		existingByName[eh.Name] = eh
		p := eh.SSHPort
		if p <= 0 {
			p = 22
		}
		existingByAddrPort[fmt.Sprintf("%s:%d", eh.Address, p)] = eh
	}

	hasInlineCred := opts.InlineCredential != nil && strings.TrimSpace(opts.InlineCredential.Plaintext) != ""
	validRows := make([]inventory.HostImportRow, 0, len(res.Rows))
	for _, r := range res.Rows {
		rowOK := true
		exByName, nameExists := existingByName[r.Name]
		if nameExists && !opts.UpdateExisting {
			res.Errors = append(res.Errors, inventory.ImportRowError{
				RowNumber: r.RowNumber, Field: "name", Code: sdk.CodeHostClaimConflict,
				Message: fmt.Sprintf("host %q already exists in project", r.Name),
			})
			rowOK = false
		}
		apKey := fmt.Sprintf("%s:%d", r.Address, r.Port)
		if owner, dupAddr := existingByAddrPort[apKey]; dupAddr {
			if !opts.UpdateExisting || !nameExists || owner.ID != exByName.ID {
				res.Errors = append(res.Errors, inventory.ImportRowError{
					RowNumber: r.RowNumber, Field: "address", Code: sdk.CodeHostClaimConflict,
					Message: fmt.Sprintf("SSH endpoint %s is already registered by host %q", apKey, owner.Name),
				})
				rowOK = false
			}
		}
		credRef := strings.TrimSpace(r.Credential)
		if credRef == "" {
			credRef = strings.TrimSpace(opts.DefaultCredentialID)
		}
		if credRef != "" {
			if _, cerr := f.resolveHostCredentialID(ctx, projectID, credRef); cerr != nil {
				res.Errors = append(res.Errors, inventory.ImportRowError{
					RowNumber: r.RowNumber, Field: "credential", Code: sdk.CodeAPIInvalidRequest, Message: cerr.Error(),
				})
				rowOK = false
			}
		} else if !hasInlineCred {
			if creds, _ := f.store.Credentials().ListByProject(ctx, projectID); len(creds) == 0 && !opts.DryRun {
				res.Errors = append(res.Errors, inventory.ImportRowError{
					RowNumber: r.RowNumber, Field: "credential", Code: sdk.CodeAPIInvalidRequest,
					Message: "project has no credentials in vault; select/create an SSH credential in the import drawer",
				})
				rowOK = false
			}
		}
		if rowOK {
			validRows = append(validRows, r)
		}
	}
	res.Rows = validRows

	if opts.DryRun || len(res.Errors) > 0 {
		return res, 0, 0, nil
	}

	// Optionally create inline Vault SSH credential on real commit
	defaultCredID := strings.TrimSpace(opts.DefaultCredentialID)
	if hasInlineCred {
		cName := strings.TrimSpace(opts.InlineCredential.Name)
		if cName == "" {
			cName = fmt.Sprintf("cmdb-import-%s", time.Now().UTC().Format("20060102-150405"))
		}
		cKind := strings.TrimSpace(opts.InlineCredential.Kind)
		if cKind == "" {
			cKind = "ssh_password"
		}
		createdCred, cerr := f.CreateCredential(ctx, actor, projectID, cName, cKind, []byte(opts.InlineCredential.Plaintext))
		if cerr != nil {
			return res, 0, 0, cerr
		}
		defaultCredID = createdCred.ID
	}

	imported, updated := 0, 0
	for _, r := range res.Rows {
		credRef := strings.TrimSpace(r.Credential)
		if credRef == "" {
			credRef = defaultCredID
		}
		credID, cerr := f.resolveHostCredentialID(ctx, projectID, credRef)
		if cerr != nil {
			res.Errors = append(res.Errors, inventory.ImportRowError{
				RowNumber: r.RowNumber, Field: "credential", Code: sdk.CodeAPIInvalidRequest, Message: cerr.Error(),
			})
			continue
		}
		mergedLabels := r.MergedLabels()
		arch := r.Arch
		if arch == "" {
			arch = "amd64"
		}
		pageSize := r.PageSize
		if pageSize <= 0 {
			pageSize = 4096
		}

		if ex, exists := existingByName[r.Name]; exists && opts.UpdateExisting {
			if r.Address != "" {
				ex.Address = r.Address
			}
			if r.Port > 0 {
				ex.SSHPort = r.Port
			}
			if r.User != "" {
				ex.SSHUser = r.User
			}
			if credID != "" {
				ex.CredentialID = credID
			}
			if r.Zone != "" {
				ex.FaultZone = r.Zone
			}
			if r.Rack != "" {
				ex.FaultRack = r.Rack
			}
			if r.Arch != "" {
				ex.Arch = arch
			}
			if r.PageSize > 0 {
				ex.PageSize = pageSize
			}
			if ex.Labels == nil {
				ex.Labels = map[string]string{}
			}
			for k, v := range mergedLabels {
				ex.Labels[k] = v
			}
			if uerr := f.store.Hosts().Update(ctx, &ex); uerr != nil {
				res.Errors = append(res.Errors, inventory.ImportRowError{
					RowNumber: r.RowNumber, Field: "name", Code: sdk.CodeAPIInvalidRequest, Message: uerr.Error(),
				})
				continue
			}
			updated++
			continue
		}

		h := &domain.Host{
			ProjectID:    projectID,
			Name:         r.Name,
			Address:      r.Address,
			SSHPort:      r.Port,
			SSHUser:      r.User,
			CredentialID: credID,
			FaultZone:    r.Zone,
			FaultRack:    r.Rack,
			Labels:       mergedLabels,
			HostKeyFP:    r.HostKeyFP,
			Arch:         arch,
			PageSize:     pageSize,
		}
		if cerr := f.store.Hosts().Create(ctx, h); cerr != nil {
			res.Errors = append(res.Errors, inventory.ImportRowError{
				RowNumber: r.RowNumber, Field: "name", Code: sdk.CodeAPIInvalidRequest, Message: cerr.Error(),
			})
			continue
		}
		imported++
	}
	f.recordAudit(ctx, actor, projectID, "host.import", "project", projectID, "success", "", map[string]any{
		"format": opts.Format, "imported": imported, "updated": updated, "errors": len(res.Errors),
	})
	return res, imported, updated, nil
}

// ExportHostsTemplate generates a downloadable Excel (.xlsx) or UTF-8 BOM CSV import template.
func (f *Facade) ExportHostsTemplate(format string) (raw []byte, contentType, filename string, err error) {
	if strings.EqualFold(strings.TrimSpace(format), "csv") {
		return inventory.GenerateHostTemplateCSV(), "text/csv; charset=utf-8", "kinstall-cmdb-hosts-template.csv", nil
	}
	b, err := inventory.GenerateHostTemplateXLSX()
	if err != nil {
		return nil, "", "", err
	}
	return b, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", "kinstall-cmdb-hosts-template.xlsx", nil
}

// ExportHostsLedger exports all CMDB hosts (plus deployed instances and listening port counts) as .xlsx or .csv.
func (f *Facade) ExportHostsLedger(ctx context.Context, actor domain.Actor, projectID, format string) (raw []byte, contentType, filename string, err error) {
	if err := authz.Authorize(actor, projectID, "resource.read"); err != nil {
		return nil, "", "", err
	}
	hosts, err := f.store.Hosts().ListByProject(ctx, projectID)
	if err != nil {
		return nil, "", "", err
	}
	instances, _ := f.store.Instances().ListByProject(ctx, projectID)
	instByID := make(map[string]domain.Instance, len(instances))
	for _, inst := range instances {
		instByID[inst.ID] = inst
	}
	claimsByHost := make(map[string][]domain.HostClaim, len(hosts))
	for _, h := range hosts {
		if cl, cerr := f.store.Instances().ListClaimsByHost(ctx, h.ID); cerr == nil {
			claimsByHost[h.ID] = cl
		}
	}
	ts := time.Now().UTC().Format("20060102")
	if strings.EqualFold(strings.TrimSpace(format), "csv") {
		return inventory.ExportHostsCSV(hosts, claimsByHost, instByID), "text/csv; charset=utf-8", fmt.Sprintf("kinstall-cmdb-hosts-%s.csv", ts), nil
	}
	b, err := inventory.ExportHostsXLSX(hosts, claimsByHost, instByID)
	if err != nil {
		return nil, "", "", err
	}
	return b, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", fmt.Sprintf("kinstall-cmdb-hosts-%s.xlsx", ts), nil
}

// HostUpdatePatch describes mutable CMDB fields for a single host.
type HostUpdatePatch struct {
	Address      *string           `json:"address,omitempty"`
	SSHPort      *int              `json:"sshPort,omitempty"`
	SSHUser      *string           `json:"sshUser,omitempty"`
	CredentialID *string           `json:"credentialId,omitempty"`
	State        *string           `json:"state,omitempty"`
	FaultZone    *string           `json:"faultZone,omitempty"`
	FaultRack    *string           `json:"faultRack,omitempty"`
	Arch         *string           `json:"arch,omitempty"`
	PageSize     *int              `json:"pageSize,omitempty"`
	Biz          *string           `json:"biz,omitempty"`
	Set          *string           `json:"set,omitempty"`
	Module       *string           `json:"module,omitempty"`
	Region       *string           `json:"region,omitempty"`
	SecurityArea *string           `json:"securityArea,omitempty"`
	Owner        *string           `json:"owner,omitempty"`
	AssetTag     *string           `json:"assetTag,omitempty"`
	Labels       map[string]string `json:"labels,omitempty"`
	ReplaceLabel bool              `json:"replaceLabels,omitempty"`
}

// UpdateHost modifies a host's CMDB topology, labels, lifecycle state, or SSH configuration.
func (f *Facade) UpdateHost(ctx context.Context, actor domain.Actor, projectID, hostID string, patch HostUpdatePatch) (*domain.Host, error) {
	if err := authz.Authorize(actor, projectID, "host.write"); err != nil {
		return nil, err
	}
	h, err := f.store.Hosts().Get(ctx, hostID)
	if err != nil {
		return nil, err
	}
	if h.ProjectID != projectID {
		return nil, sdk.NewError(sdk.CodeHostNotFound, "HOST", http.StatusNotFound, sdk.CodeHostNotFound, false, fmt.Errorf("host %s not in project %s", hostID, projectID))
	}
	if patch.Address != nil && strings.TrimSpace(*patch.Address) != "" {
		h.Address = strings.TrimSpace(*patch.Address)
	}
	if patch.SSHPort != nil && *patch.SSHPort > 0 && *patch.SSHPort <= 65535 {
		h.SSHPort = *patch.SSHPort
	}
	if patch.SSHUser != nil && strings.TrimSpace(*patch.SSHUser) != "" {
		h.SSHUser = strings.TrimSpace(*patch.SSHUser)
	}
	if patch.CredentialID != nil && strings.TrimSpace(*patch.CredentialID) != "" {
		cid, cerr := f.resolveHostCredentialID(ctx, projectID, *patch.CredentialID)
		if cerr != nil {
			return nil, cerr
		}
		h.CredentialID = cid
	}
	if patch.State != nil && strings.TrimSpace(*patch.State) != "" {
		st := domain.HostState(strings.ToLower(strings.TrimSpace(*patch.State)))
		switch st {
		case domain.HostPending, domain.HostProbing, domain.HostReady, domain.HostInitializing, domain.HostActive, domain.HostDraining, domain.HostRetired:
			h.State = st
		default:
			return nil, sdk.NewError(sdk.CodeAPIInvalidRequest, "HOST", http.StatusBadRequest, sdk.CodeAPIInvalidRequest, false, fmt.Errorf("invalid host state %q", st))
		}
	}
	if patch.FaultZone != nil {
		h.FaultZone = strings.TrimSpace(*patch.FaultZone)
	}
	if patch.FaultRack != nil {
		h.FaultRack = strings.TrimSpace(*patch.FaultRack)
	}
	if patch.Arch != nil && strings.TrimSpace(*patch.Arch) != "" {
		h.Arch = strings.TrimSpace(*patch.Arch)
	}
	if patch.PageSize != nil && *patch.PageSize > 0 {
		h.PageSize = *patch.PageSize
	}
	if h.Labels == nil || patch.ReplaceLabel {
		prevCMDB := map[string]string{}
		for k, v := range h.Labels {
			if strings.HasPrefix(k, "cmdb.") {
				prevCMDB[k] = v
			}
		}
		h.Labels = prevCMDB
	}
	for k, v := range patch.Labels {
		tk := strings.TrimSpace(k)
		if tk == "" {
			continue
		}
		if strings.TrimSpace(v) == "" {
			delete(h.Labels, tk)
		} else {
			h.Labels[tk] = strings.TrimSpace(v)
		}
	}
	setTopo := func(k string, p *string) {
		if p == nil {
			return
		}
		tv := strings.TrimSpace(*p)
		if tv == "" {
			delete(h.Labels, k)
		} else {
			h.Labels[k] = tv
		}
	}
	setTopo(domain.LabelCMDBBiz, patch.Biz)
	setTopo(domain.LabelCMDBSet, patch.Set)
	setTopo(domain.LabelCMDBModule, patch.Module)
	setTopo(domain.LabelCMDBRegion, patch.Region)
	setTopo(domain.LabelCMDBSecurityArea, patch.SecurityArea)
	setTopo(domain.LabelCMDBOwner, patch.Owner)
	setTopo(domain.LabelCMDBAssetTag, patch.AssetTag)

	if err := f.store.Hosts().Update(ctx, h); err != nil {
		return nil, err
	}
	f.recordAudit(ctx, actor, projectID, "host.update", "host", h.ID, "success", "", map[string]any{"state": h.State, "faultZone": h.FaultZone})
	return h, nil
}

// BatchHostActionRequest specifies a batch CMDB operation across multiple host IDs.
type BatchHostActionRequest struct {
	HostIDs      []string          `json:"hostIds"`
	Action       string            `json:"action"` // update_topology | update_labels | bind_credential | set_state | probe | init | delete
	Biz          *string           `json:"biz,omitempty"`
	Set          *string           `json:"set,omitempty"`
	Module       *string           `json:"module,omitempty"`
	Region       *string           `json:"region,omitempty"`
	SecurityArea *string           `json:"securityArea,omitempty"`
	FaultZone    *string           `json:"faultZone,omitempty"`
	FaultRack    *string           `json:"faultRack,omitempty"`
	Owner        *string           `json:"owner,omitempty"`
	Labels       map[string]string `json:"labels,omitempty"`
	CredentialID string            `json:"credentialId,omitempty"`
	State        string            `json:"state,omitempty"`
	Profiles     []string          `json:"profiles,omitempty"`
}

// BatchHostActionResult summarizes per-host outcomes of a batch operation.
type BatchHostActionResult struct {
	Action       string            `json:"action"`
	Succeeded    []string          `json:"succeeded"`
	Failed       map[string]string `json:"failed"`
	OperationIDs []string          `json:"operationIds,omitempty"`
}

// BatchOperateHosts executes bulk topology updates, label edits, credential rebinds, state transitions, probes, HostInit, or deletes.
func (f *Facade) BatchOperateHosts(ctx context.Context, actor domain.Actor, projectID string, req BatchHostActionRequest) (*BatchHostActionResult, error) {
	if err := authz.Authorize(actor, projectID, "host.write"); err != nil {
		return nil, err
	}
	if len(req.HostIDs) == 0 {
		return nil, sdk.NewError(sdk.CodeAPIInvalidRequest, "HOST", http.StatusBadRequest, sdk.CodeAPIInvalidRequest, false, fmt.Errorf("hostIds must not be empty"))
	}
	out := &BatchHostActionResult{
		Action:    req.Action,
		Succeeded: make([]string, 0, len(req.HostIDs)),
		Failed:    make(map[string]string),
	}
	for _, hid := range req.HostIDs {
		switch req.Action {
		case "update_topology":
			_, err := f.UpdateHost(ctx, actor, projectID, hid, HostUpdatePatch{
				Biz: req.Biz, Set: req.Set, Module: req.Module,
				Region: req.Region, SecurityArea: req.SecurityArea,
				FaultZone: req.FaultZone, FaultRack: req.FaultRack, Owner: req.Owner,
			})
			if err != nil {
				out.Failed[hid] = err.Error()
			} else {
				out.Succeeded = append(out.Succeeded, hid)
			}
		case "update_labels":
			_, err := f.UpdateHost(ctx, actor, projectID, hid, HostUpdatePatch{Labels: req.Labels})
			if err != nil {
				out.Failed[hid] = err.Error()
			} else {
				out.Succeeded = append(out.Succeeded, hid)
			}
		case "bind_credential":
			cid := req.CredentialID
			_, err := f.UpdateHost(ctx, actor, projectID, hid, HostUpdatePatch{CredentialID: &cid})
			if err != nil {
				out.Failed[hid] = err.Error()
			} else {
				out.Succeeded = append(out.Succeeded, hid)
			}
		case "set_state":
			st := req.State
			_, err := f.UpdateHost(ctx, actor, projectID, hid, HostUpdatePatch{State: &st})
			if err != nil {
				out.Failed[hid] = err.Error()
			} else {
				out.Succeeded = append(out.Succeeded, hid)
			}
		case "probe":
			if _, err := f.ProbeHostFacts(ctx, actor, projectID, hid); err != nil {
				out.Failed[hid] = err.Error()
			} else {
				out.Succeeded = append(out.Succeeded, hid)
			}
		case "init":
			op, err := f.InitHost(ctx, actor, projectID, hid, req.Profiles)
			if err != nil {
				out.Failed[hid] = err.Error()
			} else {
				out.Succeeded = append(out.Succeeded, hid)
				out.OperationIDs = append(out.OperationIDs, op.ID)
			}
		case "delete":
			if err := f.DeleteHost(ctx, actor, projectID, hid); err != nil {
				out.Failed[hid] = err.Error()
			} else {
				out.Succeeded = append(out.Succeeded, hid)
			}
		default:
			return nil, sdk.NewError(sdk.CodeAPIInvalidRequest, "HOST", http.StatusBadRequest, sdk.CodeAPIInvalidRequest, false, fmt.Errorf("unsupported batch action %q", req.Action))
		}
	}
	f.recordAudit(ctx, actor, projectID, "host.batch."+req.Action, "project", projectID, "success", "", map[string]any{
		"total": len(req.HostIDs), "succeeded": len(out.Succeeded), "failed": len(out.Failed),
	})
	return out, nil
}

// GetCMDBOverview builds the Business Topology tree, Physical/Security tree, CI Impact Graph, and Process/Port Inventory.
func (f *Facade) GetCMDBOverview(ctx context.Context, actor domain.Actor, projectID string) (*inventory.CMDBOverview, error) {
	if err := authz.Authorize(actor, projectID, "resource.read"); err != nil {
		return nil, err
	}
	hosts, err := f.store.Hosts().ListByProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	instances, err := f.store.Instances().ListByProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	stacks, err := f.store.Stacks().ListByProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	claimsByHost := make(map[string][]domain.HostClaim, len(hosts))
	for _, h := range hosts {
		if cl, cerr := f.store.Instances().ListClaimsByHost(ctx, h.ID); cerr == nil {
			claimsByHost[h.ID] = cl
		}
	}
	return inventory.BuildCMDBOverview(hosts, instances, stacks, claimsByHost), nil
}

// GetHostImpact returns exact blast radius and live vs managed port audit for a single host.
func (f *Facade) GetHostImpact(ctx context.Context, actor domain.Actor, projectID, hostID string) (*inventory.HostImpactSummary, error) {
	if err := authz.Authorize(actor, projectID, "resource.read"); err != nil {
		return nil, err
	}
	h, err := f.store.Hosts().Get(ctx, hostID)
	if err != nil {
		return nil, err
	}
	claims, err := f.store.Instances().ListClaimsByHost(ctx, hostID)
	if err != nil {
		return nil, err
	}
	instances, _ := f.store.Instances().ListByProject(ctx, projectID)
	instByID := make(map[string]domain.Instance, len(instances))
	for _, inst := range instances {
		instByID[inst.ID] = inst
	}
	stacks, _ := f.store.Stacks().ListByProject(ctx, projectID)
	stackByID := make(map[string]domain.Stack, len(stacks))
	for _, st := range stacks {
		stackByID[st.ID] = st
	}
	imp := inventory.BuildHostImpact(*h, claims, instByID, stackByID)
	return &imp, nil
}

// --- CMDB Dynamic Groups ---

// EnrichedDynamicGroup attaches real-time matched host IDs & counts to a persisted CMDBDynamicGroup.
type EnrichedDynamicGroup struct {
	domain.CMDBDynamicGroup
	MatchedCount   int      `json:"matchedCount"`
	MatchedHostIDs []string `json:"matchedHostIds"`
}

func (f *Facade) ListDynamicGroups(ctx context.Context, actor domain.Actor, projectID string) ([]EnrichedDynamicGroup, error) {
	if err := authz.Authorize(actor, projectID, "resource.read"); err != nil {
		return nil, err
	}
	groups, err := f.store.CMDB().ListDynamicGroups(ctx, projectID)
	if err != nil {
		return nil, err
	}
	hosts, _ := f.store.Hosts().ListByProject(ctx, projectID)
	out := make([]EnrichedDynamicGroup, 0, len(groups))
	for _, g := range groups {
		matched := make([]string, 0)
		for _, h := range hosts {
			if inventory.MatchDynamicGroup(h, g.Rules) {
				matched = append(matched, h.ID)
			}
		}
		out = append(out, EnrichedDynamicGroup{
			CMDBDynamicGroup: g,
			MatchedCount:     len(matched),
			MatchedHostIDs:   matched,
		})
	}
	return out, nil
}

func (f *Facade) CreateDynamicGroup(ctx context.Context, actor domain.Actor, g *domain.CMDBDynamicGroup) (*domain.CMDBDynamicGroup, error) {
	if err := authz.Authorize(actor, g.ProjectID, "host.write"); err != nil {
		return nil, err
	}
	g.Name = strings.TrimSpace(g.Name)
	if g.Name == "" {
		return nil, sdk.NewError(sdk.CodeAPIInvalidRequest, "HOST", http.StatusBadRequest, sdk.CodeAPIInvalidRequest, false, fmt.Errorf("dynamic group name is required"))
	}
	g.CreatedBy = actor.Username
	if err := f.store.CMDB().CreateDynamicGroup(ctx, g); err != nil {
		return nil, err
	}
	f.recordAudit(ctx, actor, g.ProjectID, "cmdb.group.create", "cmdb_dynamic_group", g.ID, "success", "", map[string]any{"name": g.Name, "rules": len(g.Rules)})
	return g, nil
}

func (f *Facade) UpdateDynamicGroup(ctx context.Context, actor domain.Actor, g *domain.CMDBDynamicGroup) (*domain.CMDBDynamicGroup, error) {
	if err := authz.Authorize(actor, g.ProjectID, "host.write"); err != nil {
		return nil, err
	}
	if err := f.store.CMDB().UpdateDynamicGroup(ctx, g); err != nil {
		return nil, err
	}
	f.recordAudit(ctx, actor, g.ProjectID, "cmdb.group.update", "cmdb_dynamic_group", g.ID, "success", "", map[string]any{"name": g.Name, "rules": len(g.Rules)})
	return g, nil
}

func (f *Facade) DeleteDynamicGroup(ctx context.Context, actor domain.Actor, projectID, groupID string) error {
	if err := authz.Authorize(actor, projectID, "host.write"); err != nil {
		return err
	}
	if err := f.store.CMDB().DeleteDynamicGroup(ctx, groupID); err != nil {
		return err
	}
	f.recordAudit(ctx, actor, projectID, "cmdb.group.delete", "cmdb_dynamic_group", groupID, "success", "", nil)
	return nil
}

func (f *Facade) PreviewDynamicGroup(ctx context.Context, actor domain.Actor, projectID string, rules []domain.CMDBGroupRule) ([]domain.Host, error) {
	if err := authz.Authorize(actor, projectID, "resource.read"); err != nil {
		return nil, err
	}
	hosts, err := f.store.Hosts().ListByProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	matched := make([]domain.Host, 0)
	for _, h := range hosts {
		if inventory.MatchDynamicGroup(h, rules) {
			matched = append(matched, h)
		}
	}
	return matched, nil
}

// --- External CMDB Sync Sources ---

func (f *Facade) ListCMDBSyncSources(ctx context.Context, actor domain.Actor, projectID string) ([]domain.CMDBSyncSource, error) {
	if err := authz.Authorize(actor, projectID, "resource.read"); err != nil {
		return nil, err
	}
	return f.store.CMDB().ListSyncSources(ctx, projectID)
}

func (f *Facade) CreateCMDBSyncSource(ctx context.Context, actor domain.Actor, src *domain.CMDBSyncSource) (*domain.CMDBSyncSource, error) {
	if err := authz.Authorize(actor, src.ProjectID, "host.write"); err != nil {
		return nil, err
	}
	src.Name = strings.TrimSpace(src.Name)
	src.Kind = strings.ToLower(strings.TrimSpace(src.Kind))
	if src.Name == "" || src.Kind == "" {
		return nil, sdk.NewError(sdk.CodeAPIInvalidRequest, "HOST", http.StatusBadRequest, sdk.CodeAPIInvalidRequest, false, fmt.Errorf("sync source name and kind are required"))
	}
	if strings.TrimSpace(src.Endpoint) == "" && strings.TrimSpace(src.PayloadText) == "" {
		return nil, sdk.NewError(sdk.CodeAPIInvalidRequest, "HOST", http.StatusBadRequest, sdk.CodeAPIInvalidRequest, false, fmt.Errorf("either endpoint URL or offline payload snapshot is required"))
	}
	if err := f.store.CMDB().CreateSyncSource(ctx, src); err != nil {
		return nil, err
	}
	f.recordAudit(ctx, actor, src.ProjectID, "cmdb.sync_source.create", "cmdb_sync_source", src.ID, "success", "", map[string]any{"name": src.Name, "kind": src.Kind})
	return src, nil
}

func (f *Facade) DeleteCMDBSyncSource(ctx context.Context, actor domain.Actor, projectID, sourceID string) error {
	if err := authz.Authorize(actor, projectID, "host.write"); err != nil {
		return err
	}
	if err := f.store.CMDB().DeleteSyncSource(ctx, sourceID); err != nil {
		return err
	}
	f.recordAudit(ctx, actor, projectID, "cmdb.sync_source.delete", "cmdb_sync_source", sourceID, "success", "", nil)
	return nil
}

// RunCMDBSyncResult reports the outcome of running an external CMDB sync source.
type RunCMDBSyncResult struct {
	SourceID string                  `json:"sourceId"`
	DryRun   bool                    `json:"dryRun"`
	Imported int                     `json:"imported"`
	Updated  int                     `json:"updated"`
	Result   *inventory.ImportResult `json:"result"`
}

// RunCMDBSyncSource fetches/reads hosts from an external CMDB source (HTTP JSON/CSV, LiveOps JSON, Ansible INI, Huawei HCS) and upserts them.
func (f *Facade) RunCMDBSyncSource(ctx context.Context, actor domain.Actor, projectID, sourceID string, dryRun bool) (*RunCMDBSyncResult, error) {
	if err := authz.Authorize(actor, projectID, "host.write"); err != nil {
		return nil, err
	}
	src, err := f.store.CMDB().GetSyncSource(ctx, sourceID)
	if err != nil {
		return nil, err
	}

	var payload []byte
	if strings.TrimSpace(src.PayloadText) != "" {
		payload = []byte(src.PayloadText)
	} else {
		req, rerr := http.NewRequestWithContext(ctx, http.MethodGet, src.Endpoint, nil)
		if rerr != nil {
			_ = f.store.CMDB().UpdateSyncSourceStatus(ctx, src.ID, "failed", rerr.Error(), time.Now().UTC())
			return nil, sdk.NewError(sdk.CodeAPIInvalidRequest, "HOST", http.StatusBadRequest, sdk.CodeAPIInvalidRequest, false, rerr)
		}
		if src.Mapping.AuthHeader != "" {
			if k, v, ok := strings.Cut(src.Mapping.AuthHeader, ":"); ok {
				req.Header.Set(strings.TrimSpace(k), strings.TrimSpace(v))
			}
		}
		client := &http.Client{Timeout: 15 * time.Second}
		resp, derr := client.Do(req)
		if derr != nil {
			_ = f.store.CMDB().UpdateSyncSourceStatus(ctx, src.ID, "failed", derr.Error(), time.Now().UTC())
			return nil, sdk.NewError(sdk.CodeAPIInvalidRequest, "HOST", http.StatusBadGateway, sdk.CodeAPIInvalidRequest, true, derr)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode >= 400 {
			msg := fmt.Sprintf("upstream returned HTTP %d", resp.StatusCode)
			_ = f.store.CMDB().UpdateSyncSourceStatus(ctx, src.ID, "failed", msg, time.Now().UTC())
			return nil, sdk.NewError(sdk.CodeAPIInvalidRequest, "HOST", http.StatusBadGateway, sdk.CodeAPIInvalidRequest, true, fmt.Errorf("%s", msg))
		}
		payload, err = io.ReadAll(io.LimitReader(resp.Body, 16<<20))
		if err != nil {
			_ = f.store.CMDB().UpdateSyncSourceStatus(ctx, src.ID, "failed", err.Error(), time.Now().UTC())
			return nil, err
		}
	}

	parseFormat := "json"
	switch src.Kind {
	case "http_csv":
		parseFormat = "csv"
	case "ansible_ini":
		parseFormat = "ansible"
	case "liveops_json", "http_json", "huawei_hcs":
		parseFormat = "json"
	}

	defCred := ""
	if src.CredentialID != nil {
		defCred = *src.CredentialID
	}
	res, imported, updated, ierr := f.ImportHostsWithOptions(ctx, actor, projectID, HostImportOptions{
		Format:              parseFormat,
		Raw:                 payload,
		DryRun:              dryRun,
		ColumnMap:           src.Mapping.ColumnMap,
		DefaultCredentialID: defCred,
		DefaultBiz:          src.Mapping.DefaultBiz,
		DefaultSet:          src.Mapping.DefaultSet,
		DefaultModule:       src.Mapping.DefaultModule,
		DefaultRegion:       src.Mapping.DefaultRegion,
		DefaultSecurityArea: src.Mapping.DefaultSecurityArea,
		DefaultZone:         src.Mapping.DefaultZone,
		UpdateExisting:      true,
		SanitizeHostname:    true,
	})
	if ierr != nil {
		if !dryRun {
			_ = f.store.CMDB().UpdateSyncSourceStatus(ctx, src.ID, "failed", ierr.Error(), time.Now().UTC())
		}
		return nil, ierr
	}
	if !dryRun {
		status := "ok"
		if len(res.Errors) > 0 && imported == 0 && updated == 0 {
			status = "warn"
		}
		summary := fmt.Sprintf("新增 %d 台 / 更新 %d 台 / 异常 %d 行", imported, updated, len(res.Errors))
		_ = f.store.CMDB().UpdateSyncSourceStatus(ctx, src.ID, status, summary, time.Now().UTC())
		f.recordAudit(ctx, actor, projectID, "cmdb.sync_source.run", "cmdb_sync_source", src.ID, "success", "", map[string]any{
			"imported": imported, "updated": updated, "errors": len(res.Errors),
		})
	}
	return &RunCMDBSyncResult{
		SourceID: src.ID,
		DryRun:   dryRun,
		Imported: imported,
		Updated:  updated,
		Result:   res,
	}, nil
}
