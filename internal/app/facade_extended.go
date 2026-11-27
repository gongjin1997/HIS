package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/kinstall/kinstall/internal/authz"
	"github.com/kinstall/kinstall/internal/domain"
	"github.com/kinstall/kinstall/internal/hostinit"
	"github.com/kinstall/kinstall/internal/inventory"
	"github.com/kinstall/kinstall/internal/notify"
	"github.com/kinstall/kinstall/internal/obs"
	"github.com/kinstall/kinstall/internal/planner"
	"github.com/kinstall/kinstall/internal/stack"
	"github.com/kinstall/kinstall/internal/template"
	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
)

// ConfigDiffItem carries a real 3-Way Diff preview for a managed config file (T-M3-21, T-M4-04).
type ConfigDiffItem struct {
	Path         string `json:"path"`
	BaseText     string `json:"baseText"`
	LiveText     string `json:"liveText"`
	DesiredText  string `json:"desiredText"`
	HasDrift     bool   `json:"hasDrift"`
	ConflictCode string `json:"conflictCode,omitempty"`
	Outcome      string `json:"outcome"`
}

// PlanDiffEnvelope wraps the plugin PlanResult with real 3-Way Diff file hunks and host claims for UI & CLI review.
type PlanDiffEnvelope struct {
	Summary     string                        `json:"summary"`
	Outputs     map[string]any                `json:"outputs,omitempty"`
	Warnings    []sdk.Finding                 `json:"warnings,omitempty"`
	Claims      map[string]sdk.HostRoleClaims `json:"claims,omitempty"`
	ConfigDiffs []ConfigDiffItem              `json:"configDiffs"`
	HasDrift    bool                          `json:"hasDrift"`
}

// buildPlanConfigDiffs renders real file.sync/template.render configs (including embedded .tmpl templates),
// skips DriftIgnore globs (O-08), scrubs sensitive values, and evaluates 3-Way Diffs via planner.Evaluate3Way.
func buildPlanConfigDiffs(ctx context.Context, inst *domain.Instance, pl *sdk.Pipeline, driftIgnore []string, hosts []sdk.HostView) ([]ConfigDiffItem, bool) {
	lastApplied := map[string]string{}
	liveObserved := map[string]string{}
	if len(inst.StateJSON) > 0 {
		var st struct {
			LastAppliedFiles map[string]string `json:"lastAppliedFiles"`
			LiveObserved     map[string]string `json:"liveObservedFiles"`
		}
		if err := json.Unmarshal(inst.StateJSON, &st); err == nil {
			if st.LastAppliedFiles != nil {
				lastApplied = st.LastAppliedFiles
			}
			if st.LiveObserved != nil {
				liveObserved = st.LiveObserved
			}
		}
	}

	tplEng := template.New(func(_ context.Context, name string) (string, error) {
		return "[SECRET_REF:" + name + "]", nil
	})
	var sampleHost sdk.HostView
	if len(hosts) > 0 {
		sampleHost = hosts[0]
	}

	var diffs []ConfigDiffItem
	anyDrift := false
	if pl != nil {
		for _, ph := range pl.Phases {
			for _, t := range ph.Tasks {
				if t.Type != "file.sync" && t.Type != "template.render" {
					continue
				}
				dst := firstStringParam(t.Params, "dst", "dest", "path")
				if dst == "" {
					continue
				}
				// O-08: skip runtime-mutated files matching plugin DriftIgnore patterns (e.g. sentinel.conf, postgresql.auto.conf)
				if matchesDriftIgnore(dst, driftIgnore) {
					continue
				}
				desired := renderTaskPreview(ctx, tplEng, t, sampleHost)
				if desired == "" {
					continue
				}
				base := lastApplied[dst]
				live, hasLive := liveObserved[dst]
				if !hasLive {
					live = base
				}
				baseSHA := ""
				if base != "" {
					baseSHA = planner.SHA256Hex([]byte(base))
				}
				liveSHA := ""
				if live != "" {
					liveSHA = planner.SHA256Hex([]byte(live))
				}
				desiredSHA := planner.SHA256Hex([]byte(desired))
				res := planner.Evaluate3Way(dst, baseSHA, liveSHA, desiredSHA, live != "", planner.DiffOptions{
					ManageMode: t.ManageMode,
				})
				drifted := res.Decision == planner.DiffBlocked
				if drifted {
					anyDrift = true
				}
				conflictCode := ""
				if res.Err != nil {
					var se *sdk.Error
					if errors.As(res.Err, &se) {
						conflictCode = se.Code
					} else {
						conflictCode = sdk.CodeDiffExternalDrift
					}
				}
				diffs = append(diffs, ConfigDiffItem{
					Path:         dst,
					BaseText:     base,
					LiveText:     live,
					DesiredText:  desired,
					HasDrift:     drifted,
					ConflictCode: conflictCode,
					Outcome:      string(res.Decision),
				})
			}
		}
	}
	if len(diffs) == 0 {
		specPretty, _ := json.MarshalIndent(json.RawMessage(inst.DesiredSpecJSON), "", "  ")
		outcome := planner.DiffCreate
		if len(inst.LastAppliedJSON) > 0 {
			outcome = planner.DiffUpdate
		}
		diffs = append(diffs, ConfigDiffItem{
			Path:        fmt.Sprintf("/etc/kinstall/%s/%s/spec.json", inst.Product, inst.Name),
			BaseText:    string(inst.LastAppliedJSON),
			LiveText:    string(inst.LastAppliedJSON),
			DesiredText: string(specPretty),
			HasDrift:    false,
			Outcome:     string(outcome),
		})
	}
	return diffs, anyDrift
}

func firstStringParam(params map[string]any, keys ...string) string {
	for _, k := range keys {
		if s, ok := params[k].(string); ok && s != "" {
			return s
		}
	}
	return ""
}

func matchesDriftIgnore(targetPath string, patterns []string) bool {
	for _, pat := range patterns {
		if pat == "" {
			continue
		}
		if matched, err := path.Match(pat, targetPath); err == nil && matched {
			return true
		}
		// Support trailing prefix wildcard such as /var/lib/kubelet/pki/*
		if strings.HasSuffix(pat, "/*") && strings.HasPrefix(targetPath, strings.TrimSuffix(pat, "*")) {
			return true
		}
	}
	return false
}

func renderTaskPreview(ctx context.Context, tplEng *template.Engine, t sdk.TaskSpec, host sdk.HostView) string {
	var raw string
	if t.Template != nil {
		if t.Type == "template.render" {
			vars := map[string]any{"Host": host}
			for k, v := range t.Vars {
				vars[k] = v
			}
			var secrets []string
			if rendered, err := tplEng.Render(ctx, *t.Template, vars, func(s string) { secrets = append(secrets, s) }); err == nil {
				return obs.RedactString(string(rendered), secrets...)
			}
		}
		switch {
		case len(t.Template.Content) > 0:
			raw = string(t.Template.Content)
		case t.Template.Inline != "":
			raw = t.Template.Inline
		case t.Template.FS != nil && t.Template.Path != "":
			if b, err := fs.ReadFile(t.Template.FS, t.Template.Path); err == nil {
				raw = string(b)
			}
		}
	}
	if raw == "" {
		raw = extractInlineTemplate(t.Params["template"])
	}
	if raw == "" {
		raw, _ = t.Params["content"].(string)
	}
	return obs.RedactString(raw)
}

func extractInlineTemplate(raw any) string {
	switch v := raw.(type) {
	case sdk.TemplateRef:
		return v.Inline
	case map[string]any:
		if s, ok := v["inline"].(string); ok {
			return s
		}
		if s, ok := v["Inline"].(string); ok {
			return s
		}
	}
	return ""
}

// --- Credentials List & Revoke (masked metadata only — never returns EnvelopeJSON) ---

func (f *Facade) ListCredentials(ctx context.Context, actor domain.Actor, projectID string) ([]domain.Credential, error) {
	if err := authz.Authorize(actor, projectID, "resource.read"); err != nil {
		return nil, err
	}
	items, err := f.store.Credentials().ListByProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	for i := range items {
		items[i].EnvelopeJSON = nil // never expose encrypted envelope over list API
	}
	return items, nil
}

func (f *Facade) DeleteCredential(ctx context.Context, actor domain.Actor, projectID, credID string) error {
	if err := authz.Authorize(actor, projectID, "project.write"); err != nil {
		return err
	}
	if err := f.store.Credentials().Delete(ctx, credID); err != nil {
		return err
	}
	f.recordAudit(ctx, actor, projectID, "credential.delete", "credential", credID, "success", "", nil)
	return nil
}

func (f *Facade) resolveHostCredentialID(ctx context.Context, projectID, ref string) (string, error) {
	creds, err := f.store.Credentials().ListByProject(ctx, projectID)
	if err != nil {
		return "", err
	}
	trimmed := strings.TrimSpace(ref)
	if trimmed != "" {
		for _, c := range creds {
			if c.ID == trimmed || strings.EqualFold(c.Name, trimmed) {
				return c.ID, nil
			}
		}
		return "", sdk.NewError(sdk.CodeAPIInvalidRequest, "HOST", http.StatusUnprocessableEntity, sdk.CodeAPIInvalidRequest, false,
			fmt.Errorf("credential %q not found in project %s", trimmed, projectID))
	}
	if len(creds) > 0 {
		return creds[0].ID, nil
	}
	return "", sdk.NewError(sdk.CodeAPIInvalidRequest, "HOST", http.StatusUnprocessableEntity, sdk.CodeAPIInvalidRequest, false,
		fmt.Errorf("project %s has no credentials in vault; create an SSH credential first", projectID))
}

// --- Hosts Bulk Import (.xlsx / CSV / YAML), HostInit Execution & Delete (T-M1-04, T-M1-06) ---

func (f *Facade) ImportHosts(ctx context.Context, actor domain.Actor, projectID, format string, raw []byte, dryRun bool) (*inventory.ImportResult, int, error) {
	if err := authz.Authorize(actor, projectID, "host.write"); err != nil {
		return nil, 0, err
	}
	var (
		res *inventory.ImportResult
		err error
	)
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "xlsx":
		decoded := raw
		if dec, derr := base64.StdEncoding.DecodeString(strings.TrimSpace(string(raw))); derr == nil && len(dec) > 4 {
			decoded = dec
		}
		res, err = inventory.ParseXLSX(bytes.NewReader(decoded))
	case "yaml", "yml":
		res, err = inventory.ParseYAML(raw)
	default:
		res, err = inventory.ParseCSV(bytes.NewReader(raw))
	}
	if err != nil {
		return nil, 0, sdk.NewError(sdk.CodeAPIInvalidRequest, "HOST", http.StatusBadRequest, sdk.CodeAPIInvalidRequest, false, err)
	}

	// Pre-validate against existing project hosts (name + address:port) and explicit credential references even during Dry-Run!
	existingHosts, _ := f.store.Hosts().ListByProject(ctx, projectID)
	existingByName := make(map[string]bool, len(existingHosts))
	existingByAddrPort := make(map[string]string, len(existingHosts))
	for _, eh := range existingHosts {
		existingByName[eh.Name] = true
		p := eh.SSHPort
		if p <= 0 {
			p = 22
		}
		existingByAddrPort[fmt.Sprintf("%s:%d", eh.Address, p)] = eh.Name
	}
	validRows := make([]inventory.HostImportRow, 0, len(res.Rows))
	for _, r := range res.Rows {
		rowOK := true
		if existingByName[r.Name] {
			res.Errors = append(res.Errors, inventory.ImportRowError{
				RowNumber: r.RowNumber, Field: "name", Code: sdk.CodeHostClaimConflict,
				Message: fmt.Sprintf("host %q already exists in project", r.Name),
			})
			rowOK = false
		}
		apKey := fmt.Sprintf("%s:%d", r.Address, r.Port)
		if owner, dupAddr := existingByAddrPort[apKey]; dupAddr {
			res.Errors = append(res.Errors, inventory.ImportRowError{
				RowNumber: r.RowNumber, Field: "address", Code: sdk.CodeHostClaimConflict,
				Message: fmt.Sprintf("SSH endpoint %s is already registered by host %q", apKey, owner),
			})
			rowOK = false
		}
		if strings.TrimSpace(r.Credential) != "" {
			if _, cerr := f.resolveHostCredentialID(ctx, projectID, r.Credential); cerr != nil {
				res.Errors = append(res.Errors, inventory.ImportRowError{
					RowNumber: r.RowNumber, Field: "credential", Code: sdk.CodeAPIInvalidRequest, Message: cerr.Error(),
				})
				rowOK = false
			}
		}
		if rowOK {
			validRows = append(validRows, r)
		}
	}
	res.Rows = validRows

	if dryRun || len(res.Errors) > 0 {
		return res, 0, nil
	}
	imported := 0
	for _, r := range res.Rows {
		credID, cerr := f.resolveHostCredentialID(ctx, projectID, r.Credential)
		if cerr != nil {
			res.Errors = append(res.Errors, inventory.ImportRowError{
				RowNumber: r.RowNumber, Field: "credential", Code: sdk.CodeAPIInvalidRequest, Message: cerr.Error(),
			})
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
			Labels:       r.Labels,
			HostKeyFP:    r.HostKeyFP,
			Arch:         "amd64",
			PageSize:     4096,
		}
		if cerr := f.store.Hosts().Create(ctx, h); cerr != nil {
			res.Errors = append(res.Errors, inventory.ImportRowError{
				RowNumber: r.RowNumber, Field: "name", Code: sdk.CodeAPIInvalidRequest, Message: cerr.Error(),
			})
			continue
		}
		imported++
	}
	f.recordAudit(ctx, actor, projectID, "host.import", "project", projectID, "success", "", map[string]any{"format": format, "imported": imported, "errors": len(res.Errors)})
	return res, imported, nil
}

func (f *Facade) DeleteHost(ctx context.Context, actor domain.Actor, projectID, hostID string) error {
	if err := authz.Authorize(actor, projectID, "host.write"); err != nil {
		return err
	}
	claims, err := f.store.Instances().ListClaimsByHost(ctx, hostID)
	if err != nil {
		return err
	}
	if len(claims) > 0 {
		return sdk.NewError(sdk.CodeHostClaimConflict, "HOST", http.StatusConflict, sdk.CodeHostClaimConflict, false,
			fmt.Errorf("host %s is still claimed by %d active instance(s); uninstall instances first", hostID, len(claims)))
	}
	if err := f.store.Hosts().Delete(ctx, hostID); err != nil {
		return err
	}
	f.recordAudit(ctx, actor, projectID, "host.delete", "host", hostID, "success", "", nil)
	return nil
}

// InitHost schedules a 22-module hostinit Operation for a CMDB host under the merged profiles (T-M1-06).
func (f *Facade) InitHost(ctx context.Context, actor domain.Actor, projectID, hostID string, profiles []string) (*domain.Operation, error) {
	if err := authz.Authorize(actor, projectID, "host.write"); err != nil {
		return nil, err
	}
	h, err := f.store.Hosts().Get(ctx, hostID)
	if err != nil {
		return nil, err
	}
	if len(profiles) == 0 {
		profiles = []string{"base"}
	}
	prof, err := hostinit.MergeProfiles(profiles)
	if err != nil {
		return nil, sdk.NewError(sdk.CodeAPIInvalidRequest, "HOST", http.StatusBadRequest, sdk.CodeAPIInvalidRequest, false, err)
	}
	mods := hostinit.AllModules()
	enabled := make(map[string]bool, len(prof.Modules))
	for _, mn := range prof.Modules {
		enabled[mn] = true
	}
	hv := h.ToHostView(nil)
	modParams := map[string]any{
		"sysctls":    prof.Sysctls,
		"kernelMods": prof.KernelMods,
	}
	pb := sdk.NewPipelineBuilder()
	var tasks []sdk.TaskSpec
	for _, m := range mods {
		if !enabled[m.Name] {
			continue
		}
		if m.BuildTasks != nil {
			for idx, ts := range m.BuildTasks(hv, modParams) {
				tasks = append(tasks, ts.WithKey(fmt.Sprintf("%02d-%s-%d", m.Order, m.Name, idx)))
			}
		}
	}
	pb.Phase(sdk.PhaseSpec{Name: "hostinit-22-modules", Tasks: tasks})
	pipeJSON, _ := json.Marshal(pb.Build())

	h.InitProfile = strings.Join(profiles, ",")
	_ = f.store.Hosts().Update(ctx, h)

	op := &domain.Operation{
		ID:           domain.NewID(domain.PrefixOperation),
		ProjectID:    projectID,
		TargetType:   "host",
		TargetID:     h.ID,
		Action:       "hostinit",
		State:        domain.OpPending,
		RequestedBy:  actor.ID,
		PipelineJSON: pipeJSON,
	}
	if err := f.store.Operations().Create(ctx, op); err != nil {
		return nil, err
	}
	f.recordAudit(ctx, actor, projectID, "host.init", "host", h.ID, "success", "", map[string]any{"profiles": profiles, "opId": op.ID})
	return op, nil
}

// --- Instance Delete & Plugin Schema Lookup ---

func (f *Facade) DeleteInstance(ctx context.Context, actor domain.Actor, projectID, instID string) error {
	if err := authz.Authorize(actor, projectID, "instance.write"); err != nil {
		return err
	}
	_ = f.store.Instances().ReplaceClaims(ctx, instID, nil)
	if err := f.store.Instances().Delete(ctx, instID); err != nil {
		return err
	}
	f.recordAudit(ctx, actor, projectID, "instance.delete", "instance", instID, "success", "", nil)
	return nil
}

func (f *Facade) GetPluginByProduct(product string) (*sdk.PluginMeta, error) {
	if f.reg == nil {
		return nil, fmt.Errorf("plugin registry unavailable")
	}
	for _, meta := range f.reg.ListComponents() {
		if strings.EqualFold(meta.Name, product) {
			cp := meta
			return &cp, nil
		}
		for _, pr := range meta.Products {
			if strings.EqualFold(pr.Product, product) {
				cp := meta
				return &cp, nil
			}
		}
	}
	return nil, sdk.NewError(sdk.CodeAPIInvalidRequest, "PLUG", http.StatusNotFound, sdk.CodeAPIInvalidRequest, false, fmt.Errorf("plugin for product %q not found", product))
}

// --- Stack Live Validation & Multi-Layer Apply / Reverse Uninstall (T-M3-23, T-M4-05) ---

// StackNodeVisual describes a resolved Stack node with real plugin OutputSchema handles for the DAG UI.
type StackNodeVisual struct {
	ID        string              `json:"id"`
	Product   string              `json:"product"`
	Version   string              `json:"version"`
	Target    string              `json:"target"`
	DependsOn []string            `json:"dependsOn"`
	Layer     int                 `json:"layer"`
	Outputs   []StackOutputHandle `json:"outputs"`
}

// StackOutputHandle describes one typed/secret output key declared by a plugin's OutputSchema.
type StackOutputHandle struct {
	Name   string `json:"name"`
	Secret bool   `json:"secret,omitempty"`
}

// StackValidationResult carries both the canonical ResolvedDAG and enriched visual nodes from real plugin OutputSchemas.
type StackValidationResult struct {
	DAG   *stack.ResolvedDAG `json:"dag"`
	Nodes []StackNodeVisual  `json:"nodes"`
}

func (f *Facade) ValidateStackYAML(ctx context.Context, actor domain.Actor, projectID, specYAML string) (*StackValidationResult, error) {
	if err := authz.Authorize(actor, projectID, "resource.read"); err != nil {
		return nil, err
	}
	doc, err := stack.Parse([]byte(specYAML))
	if err != nil {
		return nil, err
	}
	dag, err := stack.ResolveAndValidate(doc, f.reg, func(credID string) bool {
		_, cerr := f.store.Credentials().Get(ctx, credID)
		return cerr == nil
	})
	if err != nil {
		return nil, err
	}
	layerOf := map[string]int{}
	for idx, layer := range dag.InstallLayers {
		for _, id := range layer {
			layerOf[id] = idx
		}
	}
	nodes := make([]StackNodeVisual, 0, len(doc.Spec.Instances))
	for _, inst := range doc.Spec.Instances {
		ver := inst.ProductVersion
		var outs []StackOutputHandle
		if f.reg != nil {
			if comp, rerr := f.reg.Resolve(inst.Product, "", ""); rerr == nil {
				meta := comp.Meta()
				if ver == "" {
					for _, pr := range meta.Products {
						if strings.EqualFold(pr.Product, inst.Product) && pr.DefaultVer != "" {
							ver = pr.DefaultVer
							break
						}
					}
				}
				for k, v := range meta.OutputSchema {
					low := strings.ToLower(k + " " + v)
					isSec := strings.Contains(low, "secret") || strings.Contains(low, "pass") || strings.Contains(low, "key") || strings.Contains(low, "token")
					outs = append(outs, StackOutputHandle{Name: k, Secret: isSec})
				}
				sort.Slice(outs, func(i, j int) bool { return outs[i].Name < outs[j].Name })
			}
		}
		deps := inst.DependsOn
		if deps == nil {
			deps = []string{}
		}
		nodes = append(nodes, StackNodeVisual{
			ID:        inst.ID,
			Product:   inst.Product,
			Version:   ver,
			Target:    string(inst.DeployTarget),
			DependsOn: deps,
			Layer:     layerOf[inst.ID],
			Outputs:   outs,
		})
	}
	return &StackValidationResult{DAG: dag, Nodes: nodes}, nil
}

func (f *Facade) ApplyStack(ctx context.Context, actor domain.Actor, projectID, stackID, action string) (*domain.Operation, error) {
	if err := authz.Authorize(actor, projectID, "stack.write"); err != nil {
		return nil, err
	}
	st, err := f.store.Stacks().Get(ctx, stackID)
	if err != nil {
		return nil, err
	}
	if action == "" {
		action = "install"
	}

	doc, parseErr := stack.Parse([]byte(st.SpecYAML))
	var dag stack.ResolvedDAG
	if len(st.ResolvedDAGJSON) > 0 {
		_ = json.Unmarshal(st.ResolvedDAGJSON, &dag)
	}
	if len(dag.InstallLayers) == 0 && parseErr == nil && doc != nil {
		if resolved, rerr := stack.ResolveAndValidate(doc, f.reg, nil); rerr == nil && resolved != nil {
			dag = *resolved
		}
	}
	layers := dag.InstallLayers
	if action == "uninstall" {
		layers = dag.UninstallLayers
	}

	// Build node spec lookup & existing stack member instances for real layered plugin pipeline compilation
	nodeSpecs := make(map[string]stack.InstanceNode)
	var extRefs []stack.ExternalRef
	if parseErr == nil && doc != nil {
		extRefs = doc.Spec.ExternalRefs
		for _, n := range doc.Spec.Instances {
			nodeSpecs[n.ID] = n
		}
	}
	existingMembers, _ := f.store.Instances().ListByStack(ctx, st.ID)
	memberByName := make(map[string]*domain.Instance, len(existingMembers))
	for i := range existingMembers {
		memberByName[existingMembers[i].Name] = &existingMembers[i]
	}
	allHosts, _ := f.store.Hosts().ListByProject(ctx, projectID)
	hostViews := make([]sdk.HostView, 0, len(allHosts))
	for _, h := range allHosts {
		hostViews = append(hostViews, h.ToHostView(nil))
	}

	needsApproval := authz.RequiresApproval(st.Env, action, false, false)
	accumulatedOutputs := make(map[string]map[string]any)
	pb := sdk.NewPipelineBuilder()

	for i, layer := range layers {
		for _, nodeID := range layer {
			nodeSpec, hasSpec := nodeSpecs[nodeID]
			if !hasSpec {
				pb.Phase(sdk.PhaseSpec{
					Name:       fmt.Sprintf("L%d/%s/%s", i, nodeID, action),
					Disruptive: action == "uninstall",
					Tasks: []sdk.TaskSpec{
						sdk.CmdRun(fmt.Sprintf("echo '[stack:%s][L%d] %s %s (externalRefs untouched)'", st.Name, i, action, nodeID), "").WithKey(fmt.Sprintf("l%d-%s", i, nodeID)),
					},
				})
				continue
			}

			// Interpolate ${instances.<upstream>.outputs.<key>} & ${externalRefs.<id>.<field>}
			resolvedSpec := stack.InterpolateSpec(nodeSpec.Spec, accumulatedOutputs, extRefs)
			specJSON, _ := json.Marshal(resolvedSpec)
			target := nodeSpec.DeployTarget
			if target == "" {
				target = sdk.TargetHostBinary
			}
			memberName := fmt.Sprintf("%s-%s", st.Name, nodeSpec.ID)

			// Reconcile member Instance record bound to StackID (never mutates externalRefs!)
			member := memberByName[memberName]
			if member == nil && action != "uninstall" {
				stID := st.ID
				member = &domain.Instance{
					ID:              domain.NewID(domain.PrefixInstance),
					ProjectID:       projectID,
					StackID:         &stID,
					Env:             st.Env,
					Name:            memberName,
					Product:         nodeSpec.Product,
					DeployTarget:    target,
					ProductVersion:  nodeSpec.ProductVersion,
					DesiredSpecJSON: specJSON,
					State:           "planned",
				}
				if cerr := f.store.Instances().Create(ctx, member); cerr == nil {
					memberByName[memberName] = member
				}
			} else if member != nil && action != "uninstall" {
				member.DesiredSpecJSON = specJSON
				if nodeSpec.ProductVersion != "" {
					member.ProductVersion = nodeSpec.ProductVersion
				}
				_ = f.store.Instances().Update(ctx, member)
			}

			// Compile real plugin phases for this member node
			compiledPhases := 0
			if f.reg != nil {
				if comp, rerr := f.reg.Resolve(nodeSpec.Product, target, nodeSpec.ProductVersion); rerr == nil && comp != nil {
					var instView sdk.InstanceView
					if member != nil {
						instView = member.ToInstanceView()
					} else {
						instView = sdk.InstanceView{
							ID:             nodeSpec.ID,
							ProjectID:      projectID,
							StackID:        st.ID,
							Env:            st.Env,
							Name:           memberName,
							Product:        nodeSpec.Product,
							DeployTarget:   target,
							ProductVersion: nodeSpec.ProductVersion,
							Spec:           specJSON,
						}
					}
					if planRes, perr := comp.Plan(ctx, &sdk.PlanRequest{
						Instance:  instView,
						Hosts:     hostViews,
						Operation: action,
					}); perr == nil && planRes != nil {
						if len(planRes.Outputs) > 0 {
							accumulatedOutputs[nodeSpec.ID] = planRes.Outputs
							if member != nil && action != "uninstall" && !needsApproval {
								if ob, merr := json.Marshal(planRes.Outputs); merr == nil {
									member.OutputsJSON = ob
									_ = f.store.Instances().Update(ctx, member)
								}
							}
						}
					}
					var childPipe *sdk.Pipeline
					if opBuilder, ok := comp.Operation(action); ok {
						if cp, berr := opBuilder.Build(ctx, &sdk.OpRequest{Instance: instView, Hosts: hostViews}); berr == nil && cp != nil {
							childPipe = cp
						}
					}
					if childPipe != nil {
						for pIdx, ph := range childPipe.Phases {
							scPh := ph
							scPh.Name = fmt.Sprintf("L%d/%s/%s", i, nodeID, ph.Name)
							if action == "uninstall" {
								scPh.Disruptive = true
							}
							scTasks := make([]sdk.TaskSpec, len(ph.Tasks))
							for tIdx, t := range ph.Tasks {
								tk := t.Key
								if tk == "" {
									tk = fmt.Sprintf("%d-%s", tIdx, t.Type)
								}
								scTasks[tIdx] = t.WithKey(fmt.Sprintf("l%d-%s-p%d-%s", i, nodeID, pIdx, tk))
							}
							scPh.Tasks = scTasks
							pb.Phase(scPh)
							compiledPhases++
						}
					}
				}
			}

			if compiledPhases == 0 {
				pb.Phase(sdk.PhaseSpec{
					Name:       fmt.Sprintf("L%d/%s/%s", i, nodeID, action),
					Disruptive: action == "uninstall",
					Tasks: []sdk.TaskSpec{
						sdk.CmdRun(fmt.Sprintf("echo '[stack:%s][L%d] %s %s (%s)'", st.Name, i, action, nodeID, nodeSpec.Product), "").WithKey(fmt.Sprintf("l%d-%s", i, nodeID)),
					},
				})
			}
		}
	}
	pipeJSON, _ := json.Marshal(pb.Build())

	initState := domain.OpPending
	if needsApproval {
		initState = domain.OpAwaitingApproval
	}
	op := &domain.Operation{
		ID:           domain.NewID(domain.PrefixOperation),
		ProjectID:    projectID,
		TargetType:   "stack",
		TargetID:     st.ID,
		Action:       action,
		State:        initState,
		RequestedBy:  actor.ID,
		PipelineJSON: pipeJSON,
	}
	if err := f.store.Operations().Create(ctx, op); err != nil {
		return nil, err
	}
	if needsApproval {
		ap := &domain.Approval{
			OperationID: op.ID,
			ProjectID:   projectID,
			EnvName:     st.Env,
			RequestedBy: actor.ID,
			Reason:      fmt.Sprintf("Stack %s on %s/%s (%d layers)", action, st.Env, st.Name, len(layers)),
			Status:      domain.ApprovalPending,
			ExpiresAt:   time.Now().UTC().Add(24 * time.Hour),
		}
		_ = f.store.Approvals().Create(ctx, ap)
		f.dispatchNotification(ctx, projectID, "approval.requested", "warn", "approval:"+op.ID, ap.Reason)
	}
	f.recordAudit(ctx, actor, projectID, "stack."+action, "stack", st.ID, "success", "", map[string]any{"opId": op.ID, "state": op.State, "layers": len(layers)})
	return op, nil
}

// --- Multi-Channel Notifications (G2 §3, T-M3-12) ---

func (f *Facade) ListNotificationChannels(ctx context.Context, actor domain.Actor, projectID string) ([]domain.NotificationChannel, error) {
	if err := authz.Authorize(actor, projectID, "resource.read"); err != nil {
		return nil, err
	}
	return f.store.Notifications().ListChannels(ctx, projectID)
}

func (f *Facade) CreateNotificationChannel(ctx context.Context, actor domain.Actor, ch *domain.NotificationChannel) (*domain.NotificationChannel, error) {
	if err := authz.Authorize(actor, ch.ProjectID, "project.write"); err != nil {
		return nil, err
	}
	if ch.ID == "" {
		ch.ID = domain.NewID(domain.PrefixNotifyChan)
	}
	if err := f.store.Notifications().CreateChannel(ctx, ch); err != nil {
		return nil, err
	}
	f.recordAudit(ctx, actor, ch.ProjectID, "notify_channel.create", "notification_channel", ch.ID, "success", "", map[string]any{"kind": ch.Kind, "name": ch.Name})
	return ch, nil
}

func (f *Facade) TestNotificationDispatch(ctx context.Context, actor domain.Actor, projectID, summary string) (sent, folded int, err error) {
	if err := authz.Authorize(actor, projectID, "project.write"); err != nil {
		return 0, 0, err
	}
	if f.notifier == nil {
		return 0, 0, nil
	}
	if summary == "" {
		summary = "kinstall 通知通道连通性与防风暴测试"
	}
	return f.notifier.Dispatch(ctx, notify.EventMessage{
		EventType:  "system.test",
		ProjectID:  projectID,
		Severity:   "info",
		DedupKey:   "test:" + projectID + ":" + time.Now().UTC().Format("20060102150405"),
		Summary:    summary,
		OccurredAt: time.Now().UTC(),
	})
}

func (f *Facade) dispatchNotification(ctx context.Context, projectID, eventType, severity, dedupKey, summary string) {
	if f.notifier == nil {
		return
	}
	_, _, _ = f.notifier.Dispatch(ctx, notify.EventMessage{
		EventType:  eventType,
		ProjectID:  projectID,
		Severity:   severity,
		DedupKey:   dedupKey,
		Summary:    summary,
		OccurredAt: time.Now().UTC(),
	})
}

// --- Users & Scoped API Tokens (G1 §4.1, T-M3-06/16) ---

func (f *Facade) ListUsers(ctx context.Context, actor domain.Actor) ([]domain.User, error) {
	if err := authz.Authorize(actor, "*", "project.write"); err != nil {
		return nil, err
	}
	return f.store.Auth().ListUsers(ctx)
}

func (f *Facade) CreateUser(ctx context.Context, actor domain.Actor, username, displayName, password string, bindings []domain.RoleBinding) (*domain.User, error) {
	if err := authz.Authorize(actor, "*", "project.write"); err != nil {
		return nil, err
	}
	u, err := f.authSvc.CreateLocalUser(ctx, username, displayName, password, bindings)
	if err != nil {
		return nil, err
	}
	u.PasswordHash = ""
	f.recordAudit(ctx, actor, "*", "user.create", "user", u.ID, "success", "", map[string]any{"username": u.Username})
	return u, nil
}

func (f *Facade) ListAPITokens(ctx context.Context, actor domain.Actor) ([]domain.APIToken, error) {
	return f.store.Auth().ListAPITokens(ctx, actor.ID)
}

func (f *Facade) RevokeAPIToken(ctx context.Context, actor domain.Actor, tokenID string) error {
	if err := f.store.Auth().RevokeAPIToken(ctx, tokenID); err != nil {
		return err
	}
	f.recordAudit(ctx, actor, "*", "api_token.revoke", "api_token", tokenID, "success", "", nil)
	return nil
}

// DiscoverHostsSubnet scans an operator-specified CIDR for active SSH endpoints (T-M1-04).
func (f *Facade) DiscoverHostsSubnet(ctx context.Context, actor domain.Actor, projectID, cidr string, port int) ([]inventory.DiscoveredHost, error) {
	if err := authz.Authorize(actor, projectID, "host.write"); err != nil {
		return nil, err
	}
	items, err := inventory.DiscoverSubnet(ctx, cidr, port, 1200*time.Millisecond, 32)
	if err != nil {
		return nil, sdk.NewError(sdk.CodeAPIInvalidRequest, "HOST", http.StatusBadRequest, sdk.CodeAPIInvalidRequest, false, err)
	}
	f.recordAudit(ctx, actor, projectID, "host.discover", "project", projectID, "success", "", map[string]any{"cidr": cidr, "found": len(items)})
	return items, nil
}

// RefreshAllHostsBaseline runs periodic background baseline checks across all projects (invoked by gocron/v2, T-M1-04).
func (f *Facade) RefreshAllHostsBaseline(ctx context.Context) int {
	sysActor := domain.Actor{ID: "system_scheduler", Username: "gocron", Type: "system"}
	projects, err := f.store.Projects().List(ctx)
	if err != nil {
		return 0
	}
	checked := 0
	for _, prj := range projects {
		hosts, herr := f.store.Hosts().ListByProject(ctx, prj.ID)
		if herr != nil {
			continue
		}
		for _, h := range hosts {
			if _, derr := f.CheckHostBaseline(ctx, sysActor, prj.ID, h.ID); derr == nil {
				checked++
			}
		}
	}
	return checked
}
