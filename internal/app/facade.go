package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/kinstall/kinstall/internal/audit"
	"github.com/kinstall/kinstall/internal/authz"
	"github.com/kinstall/kinstall/internal/compliance"
	"github.com/kinstall/kinstall/internal/domain"
	"github.com/kinstall/kinstall/internal/engine"
	"github.com/kinstall/kinstall/internal/gitops"
	"github.com/kinstall/kinstall/internal/hostinit"
	"github.com/kinstall/kinstall/internal/inventory"
	"github.com/kinstall/kinstall/internal/multicluster"
	"github.com/kinstall/kinstall/internal/osadapter"
	"github.com/kinstall/kinstall/internal/ports"
	"github.com/kinstall/kinstall/internal/registry"
	"github.com/kinstall/kinstall/internal/stack"
	"github.com/kinstall/kinstall/internal/supportbundle"
	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
)

// Facade is the SINGLE unified application service layer shared by CLI local mode and HA Server REST/SSE (G1 §1.2, ARCHITECTURE §4.3).
type Facade struct {
	store     ports.Store
	reg       *registry.Registry
	exec      ports.Executor
	secrets   ports.SecretStore
	media     ports.MediaResolver
	events    ports.EventSink
	auditRec  *audit.Recorder
	authSvc   *authz.Service
	worker    *engine.Worker
	osEngine  *osadapter.Engine
	kcMgr     *multicluster.KubeconfigManager
	enableGM  bool
	kekKind   string
	cipherAlg string
}

// NewFacade wires the unified Facade via explicit dependency injection (no global singletons).
func NewFacade(
	st ports.Store,
	reg *registry.Registry,
	exec ports.Executor,
	sec ports.SecretStore,
	media ports.MediaResolver,
	events ports.EventSink,
	auditRec *audit.Recorder,
	authSvc *authz.Service,
	worker *engine.Worker,
	osEng *osadapter.Engine,
	kcMgr *multicluster.KubeconfigManager,
	enableGM bool,
	kekKind, cipherAlg string,
) *Facade {
	return &Facade{
		store:     st,
		reg:       reg,
		exec:      exec,
		secrets:   sec,
		media:     media,
		events:    events,
		auditRec:  auditRec,
		authSvc:   authSvc,
		worker:    worker,
		osEngine:  osEng,
		kcMgr:     kcMgr,
		enableGM:  enableGM,
		kekKind:   kekKind,
		cipherAlg: cipherAlg,
	}
}

// IsDraining exposes Worker draining state to /readyz without letting internal/server import internal/engine (ARCHITECTURE §4.3).
func (f *Facade) IsDraining() bool {
	return f.worker != nil && f.worker.IsDraining()
}

// Idempotency exposes the HTTP idempotency store to server middleware without direct internal/store access.
func (f *Facade) Idempotency() ports.IdempotencyRepo {
	return f.store.Idempotency()
}

// --- 1. Projects ---

func (f *Facade) CreateProject(ctx context.Context, actor domain.Actor, name, desc string) (*domain.Project, error) {
	if err := authz.Authorize(actor, "*", "project.write"); err != nil {
		return nil, err
	}
	p := &domain.Project{ID: domain.NewID(domain.PrefixProject), Name: strings.TrimSpace(name), Description: desc}
	if err := f.store.Projects().Create(ctx, p); err != nil {
		return nil, err
	}
	f.recordAudit(ctx, actor, p.ID, "project.create", "project", p.ID, "success", "", map[string]any{"name": p.Name})
	return p, nil
}

func (f *Facade) ListProjects(ctx context.Context, _ domain.Actor) ([]domain.Project, error) {
	return f.store.Projects().List(ctx)
}

func (f *Facade) GetProject(ctx context.Context, _ domain.Actor, id string) (*domain.Project, error) {
	return f.store.Projects().Get(ctx, id)
}

// --- 2. Credentials ---

func (f *Facade) CreateCredential(ctx context.Context, actor domain.Actor, projectID, name, kind string, plaintext []byte) (*domain.Credential, error) {
	if err := authz.Authorize(actor, projectID, "host.write"); err != nil {
		return nil, err
	}
	id := domain.NewID(domain.PrefixCredential)
	envJSON := []byte(`{"v":1,"alg":"plaintext-dev"}`)
	if f.secrets != nil {
		var err error
		envJSON, err = f.secrets.Encrypt(ctx, "credentials", id, 1, plaintext)
		if err != nil {
			return nil, err
		}
	}
	c := &domain.Credential{
		ID:           id,
		ProjectID:    projectID,
		Name:         name,
		Kind:         kind,
		State:        domain.CredentialActive,
		KeyVersion:   1,
		EnvelopeJSON: envJSON,
		CreatedBy:    actor.ID,
	}
	if err := f.store.Credentials().Create(ctx, c); err != nil {
		return nil, err
	}
	f.recordAudit(ctx, actor, projectID, "credential.create", "credential", c.ID, "success", "", map[string]any{"name": name, "kind": kind})
	return c, nil
}

// --- 3. Hosts & CMDB ---

func (f *Facade) CreateHost(ctx context.Context, actor domain.Actor, h *domain.Host) (*domain.Host, error) {
	if err := authz.Authorize(actor, h.ProjectID, "host.write"); err != nil {
		return nil, err
	}
	if err := f.store.Hosts().Create(ctx, h); err != nil {
		return nil, err
	}
	f.recordAudit(ctx, actor, h.ProjectID, "host.create", "host", h.ID, "success", "", map[string]any{"name": h.Name, "address": h.Address})
	return h, nil
}

func (f *Facade) ListHosts(ctx context.Context, actor domain.Actor, projectID string) ([]domain.Host, error) {
	if err := authz.Authorize(actor, projectID, "resource.read"); err != nil {
		return nil, err
	}
	return f.store.Hosts().ListByProject(ctx, projectID)
}

func (f *Facade) GetHost(ctx context.Context, actor domain.Actor, projectID, hostID string) (*domain.Host, error) {
	if err := authz.Authorize(actor, projectID, "resource.read"); err != nil {
		return nil, err
	}
	return f.store.Hosts().Get(ctx, hostID)
}

func (f *Facade) ProbeHostFacts(ctx context.Context, actor domain.Actor, projectID, hostID string) (*sdk.FactsView, error) {
	if err := authz.Authorize(actor, projectID, "host.write"); err != nil {
		return nil, err
	}
	h, err := f.store.Hosts().Get(ctx, hostID)
	if err != nil {
		return nil, err
	}
	if f.exec == nil {
		return nil, fmt.Errorf("executor unavailable")
	}
	sess, err := f.exec.Connect(ctx, ports.Target{
		HostID:      h.ID,
		Addr:        h.Address,
		Port:        h.SSHPort,
		User:        h.SSHUser,
		HostKeyMode: ports.HostKeyMode(h.HostKeyMode),
		HostKeyFP:   h.HostKeyFP,
		LegacyRSA:   h.LegacyRSA,
		WorkDir:     h.WorkDir,
		OnHostKeyTOFU: func(tctx context.Context, hid, fp string) error {
			return f.store.Hosts().UpdateHostKey(tctx, hid, fp)
		},
	})
	if err != nil {
		return nil, err
	}
	defer func() { _ = sess.Close() }()

	res, err := sess.Run(ctx, ports.Command{Script: osadapter.FactsCollectorScript, Timeout: 30 * time.Second})
	if err != nil {
		return nil, err
	}
	facts, err := osadapter.ParseFactsOutput(res.Stdout, h.Address, f.osEngine)
	if err != nil {
		return nil, err
	}
	if err := f.store.Hosts().UpdateFacts(ctx, h.ID, *facts); err != nil {
		return nil, err
	}
	return facts, nil
}

func (f *Facade) ListHostInitProfiles() map[string]hostinit.Profile {
	return hostinit.BuiltinProfiles()
}

func (f *Facade) ListHostInitModules() []hostinit.Module {
	return hostinit.AllModules()
}

// --- 4. Media Policies (ADR-077 Local / OBS / Online) ---

func (f *Facade) ListMediaPolicies(ctx context.Context, actor domain.Actor, projectID string) ([]domain.MediaPolicy, error) {
	if err := authz.Authorize(actor, projectID, "resource.read"); err != nil {
		return nil, err
	}
	return f.store.MediaPolicies().List(ctx, projectID)
}

func (f *Facade) CreateMediaPolicy(ctx context.Context, actor domain.Actor, mp *domain.MediaPolicy) (*domain.MediaPolicy, error) {
	pid := "*"
	if mp.ProjectID != nil {
		pid = *mp.ProjectID
	}
	if err := authz.Authorize(actor, pid, "media.write"); err != nil {
		return nil, err
	}
	if err := f.store.MediaPolicies().Create(ctx, mp); err != nil {
		return nil, err
	}
	f.recordAudit(ctx, actor, pid, "media_policy.create", "media_policy", mp.ID, "success", "", map[string]any{"name": mp.Name, "offlineStrict": mp.OfflineStrict})
	return mp, nil
}

func (f *Facade) UpdateMediaPolicy(ctx context.Context, actor domain.Actor, mp *domain.MediaPolicy) (*domain.MediaPolicy, error) {
	pid := "*"
	if mp.ProjectID != nil {
		pid = *mp.ProjectID
	}
	if err := authz.Authorize(actor, pid, "media.write"); err != nil {
		return nil, err
	}
	if err := f.store.MediaPolicies().Update(ctx, mp); err != nil {
		return nil, err
	}
	f.recordAudit(ctx, actor, pid, "media_policy.update", "media_policy", mp.ID, "success", "", map[string]any{"version": mp.Version, "offlineStrict": mp.OfflineStrict})
	return mp, nil
}

func (f *Facade) TestMediaPolicy(ctx context.Context, actor domain.Actor, projectID, policyID string) (map[string]any, error) {
	if err := authz.Authorize(actor, projectID, "media.write"); err != nil {
		return nil, err
	}
	mp, err := f.store.MediaPolicies().Get(ctx, policyID)
	if err != nil {
		return nil, err
	}
	if f.media == nil {
		return map[string]any{"policyId": mp.ID, "status": "no_resolver"}, nil
	}
	report, err := f.media.ProbePolicy(ctx, *mp)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	mp.LastProbeJSON, _ = json.Marshal(report)
	mp.LastProbedAt = &now
	_ = f.store.MediaPolicies().Update(ctx, mp)
	return report, nil
}

// --- 5. Plugins, Instances, Plans & Apply ---

func (f *Facade) ListPlugins() []sdk.PluginMeta {
	if f.reg == nil {
		return nil
	}
	return f.reg.ListComponents()
}

func (f *Facade) CreateInstance(ctx context.Context, actor domain.Actor, inst *domain.Instance) (*domain.Instance, error) {
	if err := authz.Authorize(actor, inst.ProjectID, "instance.write"); err != nil {
		return nil, err
	}
	if _, err := f.reg.Resolve(inst.Product, inst.DeployTarget, inst.ProductVersion); err != nil {
		return nil, err
	}
	if err := f.store.Instances().Create(ctx, inst); err != nil {
		return nil, err
	}
	f.recordAudit(ctx, actor, inst.ProjectID, "instance.create", "instance", inst.ID, "success", "", map[string]any{"name": inst.Name, "product": inst.Product, "target": inst.DeployTarget})
	return inst, nil
}

func (f *Facade) ListInstances(ctx context.Context, actor domain.Actor, projectID string) ([]domain.Instance, error) {
	if err := authz.Authorize(actor, projectID, "resource.read"); err != nil {
		return nil, err
	}
	return f.store.Instances().ListByProject(ctx, projectID)
}

func (f *Facade) GetInstance(ctx context.Context, actor domain.Actor, projectID, instID string) (*domain.Instance, error) {
	if err := authz.Authorize(actor, projectID, "resource.read"); err != nil {
		return nil, err
	}
	return f.store.Instances().Get(ctx, instID)
}

// PlanInstance runs plugin Validate + Claims collision check + Operation.Build and stores an immutable Plan snapshot (24h TTL).
func (f *Facade) PlanInstance(ctx context.Context, actor domain.Actor, projectID, instID, action string, hostRoles map[string][]string, bom sdk.BOMView) (*domain.Plan, *sdk.ValidationReport, error) {
	if err := authz.Authorize(actor, projectID, "instance.apply"); err != nil {
		return nil, nil, err
	}
	inst, err := f.store.Instances().Get(ctx, instID)
	if err != nil {
		return nil, nil, err
	}
	comp, err := f.reg.Resolve(inst.Product, inst.DeployTarget, inst.ProductVersion)
	if err != nil {
		return nil, nil, err
	}
	if action == "" {
		action = "install"
	}

	allHosts, err := f.store.Hosts().ListByProject(ctx, projectID)
	if err != nil {
		return nil, nil, err
	}
	hostByID := make(map[string]domain.Host, len(allHosts))
	var views []sdk.HostView
	for _, h := range allHosts {
		hostByID[h.ID] = h
		if len(hostRoles) == 0 {
			views = append(views, h.ToHostView(nil))
		} else if roles, ok := hostRoles[h.ID]; ok {
			views = append(views, h.ToHostView(roles))
		}
	}

	claimsChecker := func(hostID string, proposed sdk.HostRoleClaims) error {
		h, ok := hostByID[hostID]
		if !ok {
			return fmt.Errorf("host %s not found", hostID)
		}
		existing, err := f.store.Instances().ListClaimsByHost(ctx, hostID)
		if err != nil {
			return err
		}
		return inventory.CheckClaimConflicts(h, existing, proposed, inst.ID)
	}

	valRep, err := comp.Validate(ctx, &sdk.ValidateRequest{
		Instance:    inst.ToInstanceView(),
		Hosts:       views,
		BOM:         bom,
		CheckClaims: claimsChecker,
	})
	if err != nil {
		return nil, valRep, err
	}
	if valRep != nil && valRep.HasBlocking() {
		return nil, valRep, sdk.NewError(sdk.CodeAPIInvalidRequest, "PLAN", http.StatusUnprocessableEntity, valRep.Findings[0].Code, false,
			fmt.Errorf("plan validation blocked: %s", valRep.Findings[0].Message))
	}

	planRes, err := comp.Plan(ctx, &sdk.PlanRequest{
		Instance:    inst.ToInstanceView(),
		Hosts:       views,
		BOM:         bom,
		Operation:   action,
		CheckClaims: claimsChecker,
	})
	if err != nil {
		return nil, valRep, err
	}

	var pl *sdk.Pipeline
	if op, ok := comp.Operation(action); ok {
		pl, err = op.Build(ctx, &sdk.OpRequest{Instance: inst.ToInstanceView(), Hosts: views, BOM: bom})
		if err != nil {
			return nil, valRep, err
		}
	}
	if pl == nil && planRes.Pipeline != nil {
		pl = planRes.Pipeline
	}
	if pl == nil {
		pl = &sdk.Pipeline{}
	}

	inputHash := computeInstanceInputHash(inst, action)
	diffJSON, _ := json.Marshal(planRes)
	warnJSON, _ := json.Marshal(planRes.Warnings)
	pipeJSON, _ := json.Marshal(pl)
	plan := &domain.Plan{
		ID:           domain.NewID(domain.PrefixPlan),
		ProjectID:    projectID,
		TargetType:   "instance",
		TargetID:     inst.ID,
		InputHash:    inputHash,
		DiffJSON:     diffJSON,
		WarningsJSON: warnJSON,
		PipelineJSON: pipeJSON,
		CreatedBy:    actor.ID,
		ExpiresAt:    time.Now().UTC().Add(24 * time.Hour),
	}
	if err := f.store.Plans().Create(ctx, plan); err != nil {
		return nil, valRep, err
	}
	f.recordAudit(ctx, actor, projectID, "plan.created", "instance", inst.ID, "success", "", map[string]any{"planId": plan.ID, "action": action})
	return plan, valRep, nil
}

// ApplyInstance validates plan expiry & inputHash, triggers four-eyes approval if high-risk, or queues Operation as pending.
func (f *Facade) ApplyInstance(ctx context.Context, actor domain.Actor, projectID, instID, planID, action string, forceOverride, skipVerify bool, idempotencyKey *string) (*domain.Operation, error) {
	if err := authz.Authorize(actor, projectID, "instance.apply"); err != nil {
		return nil, err
	}
	inst, err := f.store.Instances().Get(ctx, instID)
	if err != nil {
		return nil, err
	}
	plan, err := f.store.Plans().Get(ctx, planID)
	if err != nil {
		return nil, err
	}
	if plan.TargetID != inst.ID || plan.ExpiresAt.Before(time.Now().UTC()) || plan.ConsumedByOpID != nil {
		return nil, sdk.NewError(sdk.CodePlanExpiredOrDrifted, "PLAN", http.StatusConflict, sdk.CodePlanExpiredOrDrifted, false,
			fmt.Errorf("plan %s expired or already consumed", planID))
	}
	if action == "" {
		action = "install"
	}
	if plan.InputHash != computeInstanceInputHash(inst, action) {
		return nil, sdk.NewError(sdk.CodePlanExpiredOrDrifted, "PLAN", http.StatusConflict, sdk.CodePlanExpiredOrDrifted, false,
			fmt.Errorf("instance spec changed since plan %s was created", planID))
	}

	needsApproval := authz.RequiresApproval(inst.Env, action, forceOverride, skipVerify)
	initState := domain.OpPending
	if needsApproval {
		initState = domain.OpAwaitingApproval
	}

	op := &domain.Operation{
		ID:             domain.NewID(domain.PrefixOperation),
		ProjectID:      projectID,
		TargetType:     "instance",
		TargetID:       inst.ID,
		Action:         action,
		State:          initState,
		IdempotencyKey: idempotencyKey,
		RequestedBy:    actor.ID,
		PipelineJSON:   plan.PipelineJSON,
	}
	if err := f.store.Operations().Create(ctx, op); err != nil {
		return nil, err
	}
	_ = f.store.Plans().MarkConsumed(ctx, plan.ID, op.ID)

	if needsApproval {
		ap := &domain.Approval{
			OperationID: op.ID,
			ProjectID:   projectID,
			EnvName:     inst.Env,
			RequestedBy: actor.ID,
			Reason:      fmt.Sprintf("%s on %s/%s (%s)", action, inst.Env, inst.Name, inst.Product),
			Status:      domain.ApprovalPending,
			ExpiresAt:   time.Now().UTC().Add(24 * time.Hour),
		}
		if err := f.store.Approvals().Create(ctx, ap); err != nil {
			return nil, err
		}
	}
	f.recordAudit(ctx, actor, projectID, "instance.apply", "instance", inst.ID, "success", "", map[string]any{"opId": op.ID, "state": op.State})
	return op, nil
}

// --- 6. Stacks ---

func (f *Facade) CreateStack(ctx context.Context, actor domain.Actor, projectID, env, name, specYAML string) (*domain.Stack, *stack.ResolvedDAG, error) {
	if err := authz.Authorize(actor, projectID, "stack.write"); err != nil {
		return nil, nil, err
	}
	doc, err := stack.Parse([]byte(specYAML))
	if err != nil {
		return nil, nil, err
	}
	dag, err := stack.ResolveAndValidate(doc, f.reg, func(credID string) bool {
		_, err := f.store.Credentials().Get(ctx, credID)
		return err == nil
	})
	if err != nil {
		return nil, nil, err
	}
	dagJSON, _ := json.Marshal(dag)
	st := &domain.Stack{
		ID:              domain.NewID(domain.PrefixStack),
		ProjectID:       projectID,
		Env:             env,
		Name:            name,
		SpecYAML:        specYAML,
		ResolvedDAGJSON: dagJSON,
		State:           "planned",
	}
	if err := f.store.Stacks().Create(ctx, st); err != nil {
		return nil, nil, err
	}
	f.recordAudit(ctx, actor, projectID, "stack.create", "stack", st.ID, "success", "", map[string]any{"name": name, "layers": len(dag.InstallLayers)})
	return st, dag, nil
}

func (f *Facade) ListStacks(ctx context.Context, actor domain.Actor, projectID string) ([]domain.Stack, error) {
	if err := authz.Authorize(actor, projectID, "resource.read"); err != nil {
		return nil, err
	}
	return f.store.Stacks().ListByProject(ctx, projectID)
}

// --- 7. Operations, SSE Events & Four-Eyes Approvals ---

func (f *Facade) ListOperations(ctx context.Context, actor domain.Actor, projectID string, limit int) ([]domain.Operation, error) {
	if err := authz.Authorize(actor, projectID, "resource.read"); err != nil {
		return nil, err
	}
	return f.store.Operations().ListByProject(ctx, projectID, limit)
}

func (f *Facade) GetOperation(ctx context.Context, actor domain.Actor, opID string) (*domain.Operation, error) {
	op, err := f.store.Operations().Get(ctx, opID)
	if err != nil {
		return nil, err
	}
	if err := authz.Authorize(actor, op.ProjectID, "resource.read"); err != nil {
		return nil, err
	}
	return op, nil
}

func (f *Facade) GetOperationState(ctx context.Context, opID string) (domain.OpState, error) {
	op, err := f.store.Operations().Get(ctx, opID)
	if err != nil {
		return "", err
	}
	return op.State, nil
}

func (f *Facade) ListOperationEventsAfter(ctx context.Context, opID string, afterSeq int64, limit int) ([]domain.OperationEvent, error) {
	return f.store.OperationEvents().ListAfter(ctx, opID, afterSeq, limit)
}

func (f *Facade) SubscribeOperationWakeup(opID string) (<-chan struct{}, func()) {
	if f.events == nil {
		ch := make(chan struct{})
		return ch, func() {}
	}
	return f.events.SubscribeWakeup(opID)
}

func (f *Facade) ListPendingApprovals(ctx context.Context, actor domain.Actor, projectID string) ([]domain.Approval, error) {
	if err := authz.Authorize(actor, projectID, "resource.read"); err != nil {
		return nil, err
	}
	return f.store.Approvals().ListPending(ctx, projectID)
}

func (f *Facade) DecideApproval(ctx context.Context, actor domain.Actor, projectID, opID string, approve bool, note string) error {
	if err := authz.Authorize(actor, projectID, "operation.approve"); err != nil {
		return err
	}
	if err := f.store.Approvals().Decide(ctx, opID, actor.ID, approve, note); err != nil {
		return err
	}
	action := "approval.reject"
	evType := "operation.rejected"
	if approve {
		action = "approval.approve"
		evType = "operation.approved"
	}
	if f.events != nil {
		payload, _ := json.Marshal(map[string]any{"opId": opID, "approved": approve, "decidedBy": actor.ID, "note": note})
		_, _ = f.events.Publish(ctx, domain.OperationEvent{OperationID: opID, EventType: evType, PayloadJSON: payload})
	}
	f.recordAudit(ctx, actor, projectID, action, "operation", opID, "success", "", map[string]any{"note": note})
	return nil
}

func (f *Facade) ResumeOperation(ctx context.Context, actor domain.Actor, opID string) error {
	op, err := f.store.Operations().Get(ctx, opID)
	if err != nil {
		return err
	}
	if err := authz.Authorize(actor, op.ProjectID, "instance.apply"); err != nil {
		return err
	}
	ok, err := f.store.Operations().TransitionState(ctx, opID, domain.OpFailed, domain.OpPending, nil)
	if err != nil {
		return err
	}
	if !ok {
		return sdk.NewError(sdk.CodeEngInvalidTransition, "ENG", http.StatusConflict, sdk.CodeEngInvalidTransition, false, fmt.Errorf("operation %s is not in failed state", opID))
	}
	f.recordAudit(ctx, actor, op.ProjectID, "operation.resume", "operation", opID, "success", "", nil)
	return nil
}

// --- 8. Audit, Multi-Cluster & Compliance ---

func (f *Facade) ListAuditLogs(ctx context.Context, actor domain.Actor, projectID string, afterSeq int64, limit int) ([]domain.AuditRecord, error) {
	if err := authz.Authorize(actor, projectID, "audit.read"); err != nil {
		return nil, err
	}
	return f.store.Audit().List(ctx, projectID, afterSeq, limit)
}

func (f *Facade) VerifyAuditChain(ctx context.Context, actor domain.Actor) (*audit.VerifyResult, error) {
	if err := authz.Authorize(actor, "*", "audit.read"); err != nil {
		return nil, err
	}
	return audit.VerifyChain(ctx, f.store.Audit(), f.enableGM)
}

func (f *Facade) IssueClusterKubeconfig(ctx context.Context, actor domain.Actor, projectID, instID, role string, namespaces []string, ttl time.Duration) (*multicluster.IssuedKubeconfig, error) {
	if err := authz.Authorize(actor, projectID, "resource.read"); err != nil {
		return nil, err
	}
	inst, err := f.store.Instances().Get(ctx, instID)
	if err != nil {
		return nil, err
	}
	return f.kcMgr.Issue(ctx, multicluster.IssueRequest{
		InstanceID:  inst.ID,
		ClusterName: inst.Name,
		UserID:      actor.ID,
		Username:    actor.Username,
		K8sRole:     role,
		Namespaces:  namespaces,
		TTL:         ttl,
	})
}

func (f *Facade) ListClusterKubeconfigs(ctx context.Context, actor domain.Actor, projectID, instID string) ([]domain.ClusterKubeconfig, error) {
	if err := authz.Authorize(actor, projectID, "resource.read"); err != nil {
		return nil, err
	}
	return f.store.Kubeconfigs().ListByInstance(ctx, instID)
}

func (f *Facade) RevokeClusterKubeconfig(ctx context.Context, actor domain.Actor, projectID, kcID string) error {
	if err := authz.Authorize(actor, projectID, "project.write"); err != nil {
		return err
	}
	return f.kcMgr.Revoke(ctx, kcID, actor.ID)
}

func (f *Facade) GenerateComplianceReport(ctx context.Context, actor domain.Actor, projectID string) (*compliance.Report, []byte, string, error) {
	if err := authz.Authorize(actor, projectID, "audit.read"); err != nil {
		return nil, nil, "", err
	}
	return compliance.BuildEvidencePackage(ctx, f.store, projectID, f.cipherAlg, f.kekKind, f.enableGM)
}

// RewrapAllCredentials rotates every credential DEK under the latest KEK version (T-M1-02, O-11).
func (f *Facade) RewrapAllCredentials(ctx context.Context, actor domain.Actor, projectID string) (int, error) {
	if err := authz.Authorize(actor, projectID, "project.write"); err != nil {
		return 0, err
	}
	if f.secrets == nil {
		return 0, fmt.Errorf("secret store unavailable")
	}
	creds, err := f.store.Credentials().ListByProject(ctx, projectID)
	if err != nil {
		return 0, err
	}
	rewrapped := 0
	for _, c := range creds {
		plain, err := f.secrets.Decrypt(ctx, "credentials", c.ID, c.KeyVersion, c.EnvelopeJSON)
		if err != nil {
			return rewrapped, err
		}
		nextVer := c.KeyVersion + 1
		nextEnv, err := f.secrets.Encrypt(ctx, "credentials", c.ID, nextVer, plain)
		clear(plain)
		if err != nil {
			return rewrapped, err
		}
		if err := f.store.Credentials().UpdateEnvelope(ctx, c.ID, c.Version, nextEnv, nextVer, domain.CredentialActive); err != nil {
			return rewrapped, err
		}
		rewrapped++
	}
	f.recordAudit(ctx, actor, projectID, "secrets.rewrap", "project", projectID, "success", "", map[string]any{"count": rewrapped})
	return rewrapped, nil
}

// CollectSupportBundle produces a redacted diagnostic .tar.gz archive (T-M2-05).
func (f *Facade) CollectSupportBundle(ctx context.Context, actor domain.Actor, projectID string) ([]byte, *supportbundle.BundleReport, error) {
	if err := authz.Authorize(actor, projectID, "audit.read"); err != nil {
		return nil, nil, err
	}
	return supportbundle.Collect(ctx, f.store, projectID, "", nil)
}

// ExportOperationReport renders a Markdown or JSON summary for an Operation (T-M2-05).
func (f *Facade) ExportOperationReport(ctx context.Context, actor domain.Actor, opID, format string) ([]byte, error) {
	op, err := f.GetOperation(ctx, actor, opID)
	if err != nil {
		return nil, err
	}
	runs, err := f.store.TaskRuns().ListByOperation(ctx, opID)
	if err != nil {
		return nil, err
	}
	return supportbundle.RenderOperationReport(op, runs, format)
}

// ExportGitOpsYAML exports a project's instances into deterministic GitOps YAML (T-M3-11, G2 §2).
func (f *Facade) ExportGitOpsYAML(ctx context.Context, actor domain.Actor, projectID string) ([]byte, error) {
	if err := authz.Authorize(actor, projectID, "resource.read"); err != nil {
		return nil, err
	}
	p, err := f.store.Projects().Get(ctx, projectID)
	if err != nil {
		return nil, err
	}
	insts, err := f.store.Instances().ListByProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	return gitops.ExportYAML(p.Name, insts)
}

// DiffGitOpsYAML diffs a desired GitOps YAML bundle against live project instances (T-M3-11).
func (f *Facade) DiffGitOpsYAML(ctx context.Context, actor domain.Actor, projectID string, desiredYAML []byte) ([]gitops.DiffEntry, int, error) {
	if err := authz.Authorize(actor, projectID, "resource.read"); err != nil {
		return nil, 1, err
	}
	insts, err := f.store.Instances().ListByProject(ctx, projectID)
	if err != nil {
		return nil, 1, err
	}
	return gitops.Diff(desiredYAML, insts)
}

// SyncExternalCMDBHosts upserts hosts pushed from an external CMDB webhook (T-M3-11, G2 §2).
func (f *Facade) SyncExternalCMDBHosts(ctx context.Context, actor domain.Actor, projectID string, incoming []domain.Host) (created, updated int, err error) {
	if err := authz.Authorize(actor, projectID, "host.write"); err != nil {
		return 0, 0, err
	}
	for i := range incoming {
		h := incoming[i]
		h.ProjectID = projectID
		if existing, gerr := f.store.Hosts().GetByName(ctx, projectID, h.Name); gerr == nil && existing != nil {
			updated++
			continue
		}
		if cerr := f.store.Hosts().Create(ctx, &h); cerr != nil {
			return created, updated, cerr
		}
		created++
	}
	f.recordAudit(ctx, actor, projectID, "cmdb.sync", "project", projectID, "success", "", map[string]any{"created": created, "updated": updated})
	return created, updated, nil
}

// CheckHostBaseline inspects a CMDB host against its configured hostinit profile (T-M1-08).
func (f *Facade) CheckHostBaseline(ctx context.Context, actor domain.Actor, projectID, hostID string) ([]hostinit.DriftFinding, error) {
	if err := authz.Authorize(actor, projectID, "resource.read"); err != nil {
		return nil, err
	}
	h, err := f.store.Hosts().Get(ctx, hostID)
	if err != nil {
		return nil, err
	}
	profileName := h.InitProfile
	if profileName == "" {
		profileName = "base"
	}
	prof, err := hostinit.MergeProfiles([]string{profileName})
	if err != nil {
		return nil, err
	}
	var fv sdk.FactsView
	if h.Facts != nil {
		fv = *h.Facts
	}
	return hostinit.CheckHostDrift(prof, fv, nil, ""), nil
}

func (f *Facade) recordAudit(ctx context.Context, actor domain.Actor, projectID, action, targetType, targetID, result, errCode string, payload map[string]any) {
	if f.auditRec == nil {
		return
	}
	pStr := "{}"
	if payload != nil {
		if b, err := json.Marshal(payload); err == nil {
			pStr = string(b)
		}
	}
	_, _ = f.auditRec.Append(ctx, domain.AuditEntry{
		ActorID: actor.ID, ActorType: actor.Type, SourceIP: actor.SourceIP,
		ProjectID: projectID, Action: action, TargetType: targetType, TargetID: targetID,
		Result: result, ErrCode: errCode, PayloadJSON: pStr,
	})
}

func computeInstanceInputHash(inst *domain.Instance, action string) string {
	h := sha256.New()
	_, _ = fmt.Fprintf(h, "%s|%s|%s|%s|%d|%s", inst.ID, inst.ProductVersion, inst.BOMRevision, string(inst.DeployTarget), inst.Version, action)
	_, _ = h.Write(inst.DesiredSpecJSON)
	return hex.EncodeToString(h.Sum(nil))
}
