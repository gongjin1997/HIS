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
	"github.com/kinstall/kinstall/internal/notify"
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
	notifier  *notify.Dispatcher
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
	var disp *notify.Dispatcher
	if st != nil {
		disp = notify.NewDispatcher(st.Notifications(), 5*time.Minute)
		if sec != nil {
			disp.WithSecretResolver(func(ctx context.Context, credID string) ([]byte, error) {
				c, err := st.Credentials().Get(ctx, credID)
				if err != nil {
					return nil, err
				}
				return sec.Decrypt(ctx, "credentials", c.ID, c.KeyVersion, c.EnvelopeJSON)
			})
		}
	}
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
		notifier:  disp,
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
	credID, err := f.resolveHostCredentialID(ctx, h.ProjectID, h.CredentialID)
	if err != nil {
		return nil, err
	}
	h.CredentialID = credID
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
	target, err := BuildHostTarget(ctx, f.store, f.secrets, h)
	if err != nil {
		return nil, err
	}
	sess, err := f.exec.Connect(ctx, target)
	ZeroTargetAuth(&target)
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
	return f.PlanInstanceWithParams(ctx, actor, projectID, instID, action, hostRoles, nil, bom)
}

// PlanInstanceWithParams runs plugin Validate + Claims collision check + real template 3-Way Diff + Operation.Build,
// supporting optional per-operation parameter overrides (e.g. PITR targetTime, upgrade targetVersion).
func (f *Facade) PlanInstanceWithParams(
	ctx context.Context,
	actor domain.Actor,
	projectID, instID, action string,
	hostRoles map[string][]string,
	opParams map[string]any,
	bom sdk.BOMView,
) (*domain.Plan, *sdk.ValidationReport, error) {
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
	// Honor explicit hostRoles, or fallback to desiredSpec.targetHosts, or all project hosts
	effectiveRoles := hostRoles
	if len(effectiveRoles) == 0 {
		preferredIDs, specRoles := extractInstanceHostHints(inst)
		if len(specRoles) > 0 {
			effectiveRoles = specRoles
		} else if len(preferredIDs) > 0 {
			effectiveRoles = make(map[string][]string, len(preferredIDs))
			for _, id := range preferredIDs {
				effectiveRoles[id] = nil
			}
		}
	}
	for _, h := range allHosts {
		hostByID[h.ID] = h
		if len(effectiveRoles) == 0 {
			views = append(views, h.ToHostView(nil))
		} else if roles, ok := effectiveRoles[h.ID]; ok {
			views = append(views, h.ToHostView(roles))
		} else if roles, ok := effectiveRoles[h.Name]; ok {
			views = append(views, h.ToHostView(roles))
		}
	}

	instView := inst.ToInstanceView()
	if len(opParams) > 0 {
		merged := map[string]any{}
		if len(instView.Spec) > 0 {
			_ = json.Unmarshal(instView.Spec, &merged)
		}
		for k, v := range opParams {
			if v != nil && fmt.Sprint(v) != "" {
				merged[k] = v
			}
		}
		if b, merr := json.Marshal(merged); merr == nil {
			instView.Spec = b
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
		Instance:    instView,
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
		Instance:    instView,
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
		pl, err = op.Build(ctx, &sdk.OpRequest{Instance: instView, Hosts: views, BOM: bom})
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

	// Pre-flight readiness checks inspired by LiveOps (unprobed hosts, cloned machine-id, live unmanaged port occupancy)
	if planRes == nil {
		planRes = &sdk.PlanResult{}
	}
	for _, h := range allHosts {
		if len(effectiveRoles) > 0 {
			if _, byID := effectiveRoles[h.ID]; !byID {
				if _, byName := effectiveRoles[h.Name]; !byName {
					continue
				}
			}
		}
		if h.Facts == nil {
			planRes.Warnings = append(planRes.Warnings, sdk.Finding{
				Severity: sdk.SeverityWarn,
				Code:     "KI-PREFLIGHT-HOST-UNPROBED",
				HostID:   h.ID,
				Message:  fmt.Sprintf("host %s (%s) has not been probed yet; run Facts probe before Apply", h.Name, h.Address),
			})
			continue
		}
		if proposed, ok := planRes.Claims[h.ID]; ok && len(h.Facts.ListeningPorts) > 0 {
			existingClaims, _ := f.store.Instances().ListClaimsByHost(ctx, h.ID)
			ownedPort := func(p int, proto string) bool {
				for _, ec := range existingClaims {
					if ec.InstanceID == inst.ID {
						for _, ep := range ec.Ports {
							if ep.Port == p && (ep.Proto == "" || strings.EqualFold(ep.Proto, proto)) {
								return true
							}
						}
					}
				}
				return false
			}
			for _, pp := range proposed.Ports {
				wantProto := pp.Proto
				if wantProto == "" {
					wantProto = "tcp"
				}
				if ownedPort(pp.Port, wantProto) {
					continue
				}
				for _, lp := range h.Facts.ListeningPorts {
					if lp.Port == pp.Port && strings.EqualFold(lp.Proto, wantProto) {
						procInfo := lp.Process
						if procInfo == "" {
							procInfo = "unknown process"
						}
						planRes.Warnings = append(planRes.Warnings, sdk.Finding{
							Severity: sdk.SeverityWarn,
							Code:     "KI-PREFLIGHT-PORT-IN-USE",
							HostID:   h.ID,
							Message:  fmt.Sprintf("host %s port %d/%s is already bound by live process %q (%s:%d)", h.Name, pp.Port, wantProto, procInfo, lp.Address, lp.Port),
						})
						break
					}
				}
			}
		}
	}
	for mid, dupNames := range osadapter.FindDuplicateMachineIDs(views) {
		planRes.Warnings = append(planRes.Warnings, sdk.Finding{
			Severity: sdk.SeverityWarn,
			Code:     "KI-PREFLIGHT-DUPLICATE-MACHINE-ID",
			Message:  fmt.Sprintf("cloned VM machine-id %s shared by hosts: %s; run hostinit hostname module first", mid, strings.Join(dupNames, ", ")),
		})
	}

	configDiffs, hasDrift := buildPlanConfigDiffs(ctx, inst, pl, comp.Meta().DriftIgnore, views)
	diffEnv := PlanDiffEnvelope{
		Summary:     planRes.Summary,
		Outputs:     planRes.Outputs,
		Warnings:    planRes.Warnings,
		Claims:      planRes.Claims,
		ConfigDiffs: configDiffs,
		HasDrift:    hasDrift,
	}
	inputHash := computeInstanceInputHash(inst, action)
	diffJSON, _ := json.Marshal(diffEnv)
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

	// Propagate forceOverride flag into Operation PipelineJSON so Worker RunContext honors approved overrides!
	opPipeJSON := plan.PipelineJSON
	if forceOverride && len(opPipeJSON) > 0 {
		var pl sdk.Pipeline
		if uerr := json.Unmarshal(opPipeJSON, &pl); uerr == nil {
			pl.ForceOverride = true
			if b, merr := json.Marshal(&pl); merr == nil {
				opPipeJSON = b
			}
		}
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
		PipelineJSON:   opPipeJSON,
	}
	if err := f.store.Operations().Create(ctx, op); err != nil {
		return nil, err
	}
	_ = f.store.Plans().MarkConsumed(ctx, plan.ID, op.ID)

	// Claim & output persistence safety:
	// - NEVER clear claims prematurely on uninstall (if four-eyes approval rejects or uninstall fails, production claims MUST stay intact;
	//   Worker.persistOutcome atomically clears claims ONLY once uninstall reaches OpSucceeded).
	// - For non-uninstall actions, reserve claims immediately (so concurrent prod operations awaiting approval cannot collide on the same port/dir);
	//   if a brand-new install is later rejected in DecideApproval, its provisional claims are cleanly released.
	if action != "uninstall" && len(plan.DiffJSON) > 0 {
		var diffEnv PlanDiffEnvelope
		if uerr := json.Unmarshal(plan.DiffJSON, &diffEnv); uerr == nil {
			if len(diffEnv.Claims) > 0 {
				claims := make([]domain.HostClaim, 0, len(diffEnv.Claims))
				for hostID, c := range diffEnv.Claims {
					if hostID == "" || hostID == "local" {
						continue
					}
					claims = append(claims, domain.HostClaim{
						InstanceID: inst.ID,
						HostID:     hostID,
						Role:       "member",
						Ports:      c.Ports,
						Paths:      c.Dirs,
						Subnets:    c.Subnets,
						MemMB:      c.MemoryMB,
					})
				}
				if len(claims) > 0 {
					_ = f.store.Instances().ReplaceClaims(ctx, inst.ID, claims)
				}
			}
			if len(diffEnv.Outputs) > 0 {
				if ob, merr := json.Marshal(diffEnv.Outputs); merr == nil {
					inst.OutputsJSON = ob
					_ = f.store.Instances().Update(ctx, inst)
				}
			}
		}
	}

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
		f.dispatchNotification(ctx, projectID, "approval.requested", "warn", "approval:"+op.ID, ap.Reason)
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
	// If a brand-new install (never previously applied) is rejected, release its provisionally reserved host claims.
	if !approve {
		if op, oerr := f.store.Operations().Get(ctx, opID); oerr == nil && op != nil && op.TargetType == "instance" && op.Action == "install" {
			if inst, ierr := f.store.Instances().Get(ctx, op.TargetID); ierr == nil && inst != nil && len(inst.LastAppliedJSON) == 0 {
				_ = f.store.Instances().ReplaceClaims(ctx, inst.ID, nil)
			}
		}
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
			if h.Address != "" {
				existing.Address = h.Address
			}
			if h.SSHPort > 0 {
				existing.SSHPort = h.SSHPort
			}
			if h.SSHUser != "" {
				existing.SSHUser = h.SSHUser
			}
			if h.FaultZone != "" {
				existing.FaultZone = h.FaultZone
			}
			if h.FaultRack != "" {
				existing.FaultRack = h.FaultRack
			}
			if len(h.Labels) > 0 {
				if existing.Labels == nil {
					existing.Labels = map[string]string{}
				}
				for k, v := range h.Labels {
					existing.Labels[k] = v
				}
			}
			if h.CredentialID != "" {
				if cid, cerr := f.resolveHostCredentialID(ctx, projectID, h.CredentialID); cerr == nil {
					existing.CredentialID = cid
				}
			}
			if uerr := f.store.Hosts().Update(ctx, existing); uerr != nil {
				return created, updated, uerr
			}
			updated++
			continue
		}
		credID, cerr := f.resolveHostCredentialID(ctx, projectID, h.CredentialID)
		if cerr != nil {
			return created, updated, cerr
		}
		h.CredentialID = credID
		if h.SSHPort <= 0 {
			h.SSHPort = 22
		}
		if h.SSHUser == "" {
			h.SSHUser = "root"
		}
		if cerr := f.store.Hosts().Create(ctx, &h); cerr != nil {
			return created, updated, cerr
		}
		created++
	}
	f.recordAudit(ctx, actor, projectID, "cmdb.sync", "project", projectID, "success", "", map[string]any{"created": created, "updated": updated})
	return created, updated, nil
}

// CheckHostBaseline inspects a CMDB host against its configured (possibly comma-separated multi-profile) hostinit baseline (T-M1-08).
func (f *Facade) CheckHostBaseline(ctx context.Context, actor domain.Actor, projectID, hostID string) ([]hostinit.DriftFinding, error) {
	if err := authz.Authorize(actor, projectID, "resource.read"); err != nil {
		return nil, err
	}
	h, err := f.store.Hosts().Get(ctx, hostID)
	if err != nil {
		return nil, err
	}
	var profiles []string
	seen := map[string]bool{}
	for _, part := range strings.Split(h.InitProfile, ",") {
		p := strings.TrimSpace(part)
		if p != "" && !seen[p] {
			seen[p] = true
			profiles = append(profiles, p)
		}
	}
	if len(profiles) == 0 {
		profiles = []string{"base"}
	}
	prof, err := hostinit.MergeProfiles(profiles)
	if err != nil {
		return nil, err
	}
	if h.Facts == nil {
		return []hostinit.DriftFinding{
			{
				Category: "facts",
				Key:      "facts_probe",
				Expected: "probed",
				Observed: "unprobed",
				Severity: "warn",
			},
		}, nil
	}
	fv := *h.Facts
	return hostinit.CheckHostDrift(prof, fv, fv.Sysctls, fv.THPEnabled), nil
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
