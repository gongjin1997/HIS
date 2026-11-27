package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/kinstall/kinstall/internal/authz"
	"github.com/kinstall/kinstall/internal/domain"
	"github.com/kinstall/kinstall/internal/stack"
	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
)

// CreateHostWithInlineCred creates a CMDB host, optionally creating a Vault SSH credential inline so first-run users are never blocked.
func (f *Facade) CreateHostWithInlineCred(ctx context.Context, actor domain.Actor, h *domain.Host, inlineCred *InlineCredentialSpec) (*domain.Host, error) {
	if err := authz.Authorize(actor, h.ProjectID, "host.write"); err != nil {
		return nil, err
	}
	if inlineCred != nil && strings.TrimSpace(inlineCred.Plaintext) != "" {
		cName := strings.TrimSpace(inlineCred.Name)
		if cName == "" {
			cName = fmt.Sprintf("ssh-%s-%s", h.Name, time.Now().UTC().Format("150405"))
		}
		cKind := strings.TrimSpace(inlineCred.Kind)
		if cKind == "" {
			cKind = "ssh_password"
		}
		createdCred, err := f.CreateCredential(ctx, actor, h.ProjectID, cName, cKind, []byte(inlineCred.Plaintext))
		if err != nil {
			return nil, err
		}
		h.CredentialID = createdCred.ID
	}
	return f.CreateHost(ctx, actor, h)
}

// InstanceUpdatePatch captures editable fields for an existing managed Instance.
type InstanceUpdatePatch struct {
	Env            *string           `json:"env,omitempty"`
	ProductVersion *string           `json:"productVersion,omitempty"`
	DeployTarget   *sdk.DeployTarget `json:"deployTarget,omitempty"`
	DesiredSpec    json.RawMessage   `json:"desiredSpec,omitempty"`
}

// UpdateInstance modifies an existing instance's desiredSpec, target hosts, version, or deployTarget.
func (f *Facade) UpdateInstance(ctx context.Context, actor domain.Actor, projectID, instID string, patch InstanceUpdatePatch) (*domain.Instance, error) {
	if err := authz.Authorize(actor, projectID, "instance.write"); err != nil {
		return nil, err
	}
	inst, err := f.store.Instances().Get(ctx, instID)
	if err != nil {
		return nil, err
	}
	if inst.ProjectID != projectID {
		return nil, sdk.NewError(sdk.CodeDBNotFound, "DB", http.StatusNotFound, sdk.CodeDBNotFound, false, fmt.Errorf("instance %s not in project %s", instID, projectID))
	}
	if patch.Env != nil && strings.TrimSpace(*patch.Env) != "" {
		inst.Env = strings.TrimSpace(*patch.Env)
	}
	if patch.ProductVersion != nil && strings.TrimSpace(*patch.ProductVersion) != "" {
		inst.ProductVersion = strings.TrimSpace(*patch.ProductVersion)
	}
	if patch.DeployTarget != nil && strings.TrimSpace(string(*patch.DeployTarget)) != "" {
		inst.DeployTarget = *patch.DeployTarget
	}
	if len(patch.DesiredSpec) > 0 && string(patch.DesiredSpec) != "null" {
		inst.DesiredSpecJSON = patch.DesiredSpec
	}
	if f.reg != nil {
		if _, rerr := f.reg.Resolve(inst.Product, inst.DeployTarget, inst.ProductVersion); rerr != nil {
			return nil, rerr
		}
	}
	if err := f.store.Instances().Update(ctx, inst); err != nil {
		return nil, err
	}
	f.recordAudit(ctx, actor, projectID, "instance.update", "instance", inst.ID, "success", "", map[string]any{"version": inst.Version, "productVersion": inst.ProductVersion})
	return inst, nil
}

// InstanceAssignedHost summarizes a CMDB host bound to or claimed by an instance.
type InstanceAssignedHost struct {
	HostID    string          `json:"hostId"`
	HostName  string          `json:"hostName"`
	Address   string          `json:"address"`
	Arch      string          `json:"arch"`
	PageSize  int             `json:"pageSize"`
	FaultZone string          `json:"faultZone"`
	Role      string          `json:"role"`
	Ports     []sdk.PortClaim `json:"ports,omitempty"`
	Paths     []string        `json:"paths,omitempty"`
	MemMB     int             `json:"memMb,omitempty"`
	Claimed   bool            `json:"claimed"`
}

// InstanceDetailView bundles an instance with its live host claims and assigned target hosts.
type InstanceDetailView struct {
	Instance      *domain.Instance       `json:"instance"`
	AssignedHosts []InstanceAssignedHost `json:"assignedHosts"`
	StackName     string                 `json:"stackName,omitempty"`
}

// GetInstanceDetail loads full metadata, host bindings, claims, and stack ownership for an instance.
func (f *Facade) GetInstanceDetail(ctx context.Context, actor domain.Actor, projectID, instID string) (*InstanceDetailView, error) {
	if err := authz.Authorize(actor, projectID, "resource.read"); err != nil {
		return nil, err
	}
	inst, err := f.store.Instances().Get(ctx, instID)
	if err != nil {
		return nil, err
	}
	hosts, _ := f.store.Hosts().ListByProject(ctx, projectID)
	hostByID := make(map[string]domain.Host, len(hosts))
	for _, h := range hosts {
		hostByID[h.ID] = h
	}

	assignedMap := make(map[string]InstanceAssignedHost)
	for _, h := range hosts {
		claims, _ := f.store.Instances().ListClaimsByHost(ctx, h.ID)
		for _, c := range claims {
			if c.InstanceID != inst.ID {
				continue
			}
			assignedMap[h.ID] = InstanceAssignedHost{
				HostID: h.ID, HostName: h.Name, Address: h.Address, Arch: h.Arch, PageSize: h.PageSize, FaultZone: h.FaultZone,
				Role: c.Role, Ports: c.Ports, Paths: c.Paths, MemMB: c.MemMB, Claimed: true,
			}
		}
	}
	// Also include targetHosts / hostRoles configured in desiredSpec even before Apply
	preferredIDs, specRoles := extractInstanceHostHints(inst)
	for _, hid := range preferredIDs {
		if _, ok := assignedMap[hid]; ok {
			continue
		}
		if h, ok := hostByID[hid]; ok {
			role := strings.Join(specRoles[hid], ",")
			if role == "" {
				role = "target"
			}
			assignedMap[h.ID] = InstanceAssignedHost{
				HostID: h.ID, HostName: h.Name, Address: h.Address, Arch: h.Arch, PageSize: h.PageSize, FaultZone: h.FaultZone,
				Role: role, Claimed: false,
			}
		}
	}

	assigned := make([]InstanceAssignedHost, 0, len(assignedMap))
	for _, v := range assignedMap {
		assigned = append(assigned, v)
	}
	stackName := ""
	if inst.StackID != nil && *inst.StackID != "" {
		if st, serr := f.store.Stacks().Get(ctx, *inst.StackID); serr == nil && st != nil {
			stackName = st.Name
		}
	}
	return &InstanceDetailView{
		Instance:      inst,
		AssignedHosts: assigned,
		StackName:     stackName,
	}, nil
}

// UpdateStack re-validates DAG & updates an existing Stack's YAML, name, or env.
func (f *Facade) UpdateStack(ctx context.Context, actor domain.Actor, projectID, stackID, env, name, specYAML string) (*domain.Stack, *stack.ResolvedDAG, error) {
	if err := authz.Authorize(actor, projectID, "stack.write"); err != nil {
		return nil, nil, err
	}
	st, err := f.store.Stacks().Get(ctx, stackID)
	if err != nil {
		return nil, nil, err
	}
	doc, err := stack.Parse([]byte(specYAML))
	if err != nil {
		return nil, nil, err
	}
	dag, err := stack.ResolveAndValidate(doc, f.reg, func(credID string) bool {
		_, cerr := f.store.Credentials().Get(ctx, credID)
		return cerr == nil
	})
	if err != nil {
		return nil, nil, err
	}
	dagJSON, _ := json.Marshal(dag)
	if strings.TrimSpace(env) != "" {
		st.Env = strings.TrimSpace(env)
	}
	if strings.TrimSpace(name) != "" {
		st.Name = strings.TrimSpace(name)
	}
	st.SpecYAML = specYAML
	st.ResolvedDAGJSON = dagJSON
	if err := f.store.Stacks().Update(ctx, st); err != nil {
		return nil, nil, err
	}
	f.recordAudit(ctx, actor, projectID, "stack.update", "stack", st.ID, "success", "", map[string]any{"name": st.Name, "layers": len(dag.InstallLayers)})
	return st, dag, nil
}

// DeleteStack removes a stack metadata record once no active stack-owned instances still hold host claims.
func (f *Facade) DeleteStack(ctx context.Context, actor domain.Actor, projectID, stackID string) error {
	if err := authz.Authorize(actor, projectID, "stack.write"); err != nil {
		return err
	}
	st, err := f.store.Stacks().Get(ctx, stackID)
	if err != nil {
		return err
	}
	members, _ := f.store.Instances().ListByStack(ctx, stackID)
	for _, m := range members {
		if m.State == "ready" || m.State == "running" {
			return sdk.NewError(sdk.CodeHostClaimConflict, "STACK", http.StatusConflict, sdk.CodeHostClaimConflict, false,
				fmt.Errorf("stack %s still has active member instance %s (%s); run reverse uninstall first", st.Name, m.Name, m.State))
		}
		_ = f.store.Instances().ReplaceClaims(ctx, m.ID, nil)
		_ = f.store.Instances().Delete(ctx, m.ID)
	}
	if err := f.store.Stacks().Delete(ctx, stackID); err != nil {
		return err
	}
	f.recordAudit(ctx, actor, projectID, "stack.delete", "stack", stackID, "success", "", map[string]any{"name": st.Name})
	return nil
}

// CancelOperation cancels an awaiting_approval or pending operation, marks pending approvals cancelled, and releases provisional claims.
func (f *Facade) CancelOperation(ctx context.Context, actor domain.Actor, projectID, opID, reason string) error {
	if err := authz.Authorize(actor, projectID, "instance.apply"); err != nil {
		return err
	}
	op, err := f.store.Operations().Get(ctx, opID)
	if err != nil {
		return err
	}
	if op.ProjectID != projectID {
		return sdk.NewError(sdk.CodeDBNotFound, "DB", http.StatusNotFound, sdk.CodeDBNotFound, false, fmt.Errorf("operation %s not in project %s", opID, projectID))
	}
	if op.State != domain.OpAwaitingApproval && op.State != domain.OpPending {
		return sdk.NewError(sdk.CodeEngInvalidTransition, "ENG", http.StatusConflict, sdk.CodeEngInvalidTransition, false,
			fmt.Errorf("only awaiting_approval or pending operations can be cancelled (current state=%s)", op.State))
	}
	ok, err := f.store.Operations().TransitionState(ctx, opID, op.State, domain.OpCancelled, nil)
	if err != nil {
		return err
	}
	if !ok {
		return sdk.NewError(sdk.CodeEngInvalidTransition, "ENG", http.StatusConflict, sdk.CodeEngInvalidTransition, true, fmt.Errorf("operation %s state changed concurrently", opID))
	}
	if reason == "" {
		reason = "Cancelled by " + actor.Username
	}
	_ = f.store.Approvals().Cancel(ctx, opID, reason)
	if op.TargetType == "instance" && op.Action == "install" {
		if inst, ierr := f.store.Instances().Get(ctx, op.TargetID); ierr == nil && inst != nil && len(inst.LastAppliedJSON) == 0 {
			_ = f.store.Instances().ReplaceClaims(ctx, inst.ID, nil)
		}
	}
	f.recordAudit(ctx, actor, projectID, "operation.cancel", "operation", opID, "success", "", map[string]any{"reason": reason, "prevState": op.State})
	return nil
}

// DeleteProject removes an empty project (blocking if hosts, instances, stacks, or credentials remain).
func (f *Facade) DeleteProject(ctx context.Context, actor domain.Actor, projectID string) error {
	if err := authz.Authorize(actor, "*", "project.write"); err != nil {
		return err
	}
	hosts, _ := f.store.Hosts().ListByProject(ctx, projectID)
	insts, _ := f.store.Instances().ListByProject(ctx, projectID)
	stacks, _ := f.store.Stacks().ListByProject(ctx, projectID)
	if len(hosts) > 0 || len(insts) > 0 || len(stacks) > 0 {
		return sdk.NewError(sdk.CodeHostClaimConflict, "PROJECT", http.StatusConflict, sdk.CodeHostClaimConflict, false,
			fmt.Errorf("project still contains %d hosts, %d instances, %d stacks; remove resources first", len(hosts), len(insts), len(stacks)))
	}
	if err := f.store.Projects().Delete(ctx, projectID); err != nil {
		return err
	}
	f.recordAudit(ctx, actor, projectID, "project.delete", "project", projectID, "success", "", nil)
	return nil
}

// DeleteNotificationChannel removes a configured notification channel.
func (f *Facade) DeleteNotificationChannel(ctx context.Context, actor domain.Actor, projectID, channelID string) error {
	if err := authz.Authorize(actor, projectID, "project.write"); err != nil {
		return err
	}
	if err := f.store.Notifications().DeleteChannel(ctx, channelID); err != nil {
		return err
	}
	f.recordAudit(ctx, actor, projectID, "notify_channel.delete", "notification_channel", channelID, "success", "", nil)
	return nil
}

// EnrichedOperation adds human-readable TargetName and RequestedByName to domain.Operation for UI consumption.
type EnrichedOperation struct {
	domain.Operation
	TargetName      string `json:"targetName,omitempty"`
	RequestedByName string `json:"requestedByName,omitempty"`
}

// EnrichedApproval adds human-readable TargetType/TargetName/Action/RequestedByName to domain.Approval.
type EnrichedApproval struct {
	domain.Approval
	TargetType      string `json:"targetType,omitempty"`
	TargetName      string `json:"targetName,omitempty"`
	Action          string `json:"action,omitempty"`
	RequestedByName string `json:"requestedByName,omitempty"`
}

func (f *Facade) buildNameMaps(ctx context.Context, projectID string) (targets map[string]string, users map[string]string) {
	targets = make(map[string]string)
	users = make(map[string]string)
	if hosts, err := f.store.Hosts().ListByProject(ctx, projectID); err == nil {
		for _, h := range hosts {
			targets["host:"+h.ID] = h.Name
		}
	}
	if insts, err := f.store.Instances().ListByProject(ctx, projectID); err == nil {
		for _, inst := range insts {
			targets["instance:"+inst.ID] = fmt.Sprintf("%s (%s)", inst.Name, inst.Product)
		}
	}
	if stacks, err := f.store.Stacks().ListByProject(ctx, projectID); err == nil {
		for _, st := range stacks {
			targets["stack:"+st.ID] = st.Name
		}
	}
	if us, err := f.store.Auth().ListUsers(ctx); err == nil {
		for _, u := range us {
			label := u.Username
			if u.DisplayName != "" && u.DisplayName != u.Username {
				label = fmt.Sprintf("%s (%s)", u.DisplayName, u.Username)
			}
			users[u.ID] = label
		}
	}
	return targets, users
}

// ListOperationsEnriched returns operations with human-readable target and requester names.
func (f *Facade) ListOperationsEnriched(ctx context.Context, actor domain.Actor, projectID string, limit int) ([]EnrichedOperation, error) {
	ops, err := f.ListOperations(ctx, actor, projectID, limit)
	if err != nil {
		return nil, err
	}
	targets, users := f.buildNameMaps(ctx, projectID)
	out := make([]EnrichedOperation, 0, len(ops))
	for _, op := range ops {
		tName := targets[op.TargetType+":"+op.TargetID]
		if tName == "" {
			tName = op.TargetID
		}
		uName := users[op.RequestedBy]
		if uName == "" {
			uName = op.RequestedBy
		}
		out = append(out, EnrichedOperation{
			Operation:       op,
			TargetName:      tName,
			RequestedByName: uName,
		})
	}
	return out, nil
}

// ListPendingApprovalsEnriched returns pending approvals with human-readable target, action, and requester names.
func (f *Facade) ListPendingApprovalsEnriched(ctx context.Context, actor domain.Actor, projectID string) ([]EnrichedApproval, error) {
	aps, err := f.ListPendingApprovals(ctx, actor, projectID)
	if err != nil {
		return nil, err
	}
	targets, users := f.buildNameMaps(ctx, projectID)
	out := make([]EnrichedApproval, 0, len(aps))
	for _, ap := range aps {
		ea := EnrichedApproval{
			Approval:        ap,
			RequestedByName: users[ap.RequestedBy],
		}
		if ea.RequestedByName == "" {
			ea.RequestedByName = ap.RequestedBy
		}
		if op, oerr := f.store.Operations().Get(ctx, ap.OperationID); oerr == nil && op != nil {
			ea.TargetType = op.TargetType
			ea.Action = op.Action
			ea.TargetName = targets[op.TargetType+":"+op.TargetID]
			if ea.TargetName == "" {
				ea.TargetName = op.TargetID
			}
		}
		out = append(out, ea)
	}
	return out, nil
}
