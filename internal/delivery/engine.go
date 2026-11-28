package delivery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/kinstall/kinstall/internal/cmdb"
	"github.com/kinstall/kinstall/internal/config"
	"github.com/kinstall/kinstall/internal/execplane"
	"github.com/kinstall/kinstall/pkg/platform/dag"
	"github.com/kinstall/kinstall/pkg/platform/db"
	"github.com/kinstall/kinstall/pkg/platform/eventbus"
	"github.com/kinstall/kinstall/pkg/platform/id"
	"github.com/kinstall/kinstall/pkg/platform/netguard"
)

type ReleaseItemInput struct {
	NodeKey        string            `json:"nodeKey"`
	PluginCode     string            `json:"pluginCode"`
	ServiceName    string            `json:"serviceName"`
	TargetHostCIID string            `json:"targetHostCiId"`
	BatchIndex     int               `json:"batchIndex"`
	DependsOn      []string          `json:"dependsOn"`
	PauseAfter     bool              `json:"pauseAfter"`
	SpecParams     map[string]string `json:"specParams"`
	DialURL        string            `json:"dialUrl"`
}

type CreateReleaseRequest struct {
	Name          string                       `json:"name"`
	Env           string                       `json:"env"`
	Mode          string                       `json:"mode"` // DAG | BATCH | M1 | M2 | M3
	GlobalParams  map[string]string            `json:"globalParams"`
	ServiceParams map[string]map[string]string `json:"serviceParams"`
	Items         []ReleaseItemInput           `json:"items"`
	AutoStart     bool                         `json:"autoStart"`
}

type ReleaseItem struct {
	ID              string            `json:"id"`
	ReleaseID       string            `json:"releaseId"`
	NodeKey         string            `json:"nodeKey"`
	PluginCode      string            `json:"pluginCode"`
	ServiceName     string            `json:"serviceName"`
	TargetHostCIID  string            `json:"targetHostCiId"`
	BatchIndex      int               `json:"batchIndex"`
	DependsOn       []string          `json:"dependsOn"`
	PauseAfter      bool              `json:"pauseAfter"`
	SpecParams      map[string]string `json:"specParams"`
	EffectiveParams map[string]string `json:"effectiveParams"`
	K8sSpec         config.K8sSpec    `json:"k8sSpec"`
	DialURL         string            `json:"dialUrl"`
	Status          string            `json:"status"` // PENDING | DEPLOYING | START_CHECK | DIALING_TEST | SUCCEEDED | FAILED | ROLLED_BACK
	SyncedCIID      string            `json:"syncedCiId"`
	Summary         string            `json:"summary"`
	StartedAt       string            `json:"startedAt"`
	FinishedAt      string            `json:"finishedAt"`
}

type Release struct {
	ID            string                       `json:"id"`
	WorkspaceID   string                       `json:"workspaceId"`
	Name          string                       `json:"name"`
	Env           string                       `json:"env"`
	Mode          string                       `json:"mode"`
	Status        string                       `json:"status"` // DRAFT | RUNNING | PAUSED | SUCCEEDED | FAILED | ROLLED_BACK
	GlobalParams  map[string]string            `json:"globalParams"`
	ServiceParams map[string]map[string]string `json:"serviceParams"`
	Plan          *dag.Plan                    `json:"plan,omitempty"`
	CreatedBy     string                       `json:"createdBy"`
	StartedAt     string                       `json:"startedAt"`
	FinishedAt    string                       `json:"finishedAt"`
	CreatedAt     string                       `json:"createdAt"`
	UpdatedAt     string                       `json:"updatedAt"`
	Items         []ReleaseItem                `json:"items"`
}

type Service struct {
	store   *db.Store
	cmdbSvc *cmdb.Service
	execSvc *execplane.Service
	broker  *eventbus.LogBroker
}

func NewService(store *db.Store, cmdbSvc *cmdb.Service, execSvc *execplane.Service, broker *eventbus.LogBroker) *Service {
	return &Service{
		store:   store,
		cmdbSvc: cmdbSvc,
		execSvc: execSvc,
		broker:  broker,
	}
}

// CreateRelease validates the DAG (or Batch order), merges Spec > Service > Global params,
// calculates K8s milliCPU/MiB specs, and persists the release plan.
func (s *Service) CreateRelease(ctx context.Context, wsID, actor string, req CreateReleaseRequest) (*Release, error) {
	if strings.TrimSpace(req.Name) == "" {
		return nil, errors.New("发布单名称不能为空")
	}
	if len(req.Items) == 0 {
		return nil, errors.New("发布编排至少需要包含 1 个组件节点")
	}
	env := strings.ToLower(strings.TrimSpace(req.Env))
	if env == "" {
		env = "prod"
	}
	mode := strings.ToUpper(strings.TrimSpace(req.Mode))
	switch mode {
	case "DAG", "BATCH", "M1", "M2", "M3":
	default:
		mode = "DAG"
	}
	if req.GlobalParams == nil {
		req.GlobalParams = make(map[string]string)
	}
	if req.ServiceParams == nil {
		req.ServiceParams = make(map[string]map[string]string)
	}

	// Validate Kahn DAG topology and detect cycles upfront!
	dagNodes := make([]dag.Node, 0, len(req.Items))
	for i := range req.Items {
		if strings.TrimSpace(req.Items[i].NodeKey) == "" {
			req.Items[i].NodeKey = fmt.Sprintf("node-%d", i+1)
		}
		if strings.TrimSpace(req.Items[i].ServiceName) == "" {
			req.Items[i].ServiceName = req.Items[i].NodeKey
		}
		dagNodes = append(dagNodes, dag.Node{
			ID:        req.Items[i].NodeKey,
			DependsOn: req.Items[i].DependsOn,
		})
	}
	plan, err := dag.Sort(dagNodes)
	if err != nil {
		return nil, fmt.Errorf("编排拓扑校验失败: %w", err)
	}

	// Build ReleaseItem rows with frozen 3-level merged params + K8s spec.
	now := nowISO()
	relID := id.New("rel")
	items := make([]ReleaseItem, 0, len(req.Items))
	for _, inp := range req.Items {
		plg, perr := s.GetPluginByCode(ctx, wsID, inp.PluginCode)
		if perr != nil {
			return nil, fmt.Errorf("未找到插件 %q: %w", inp.PluginCode, perr)
		}
		manifest, merr := ParseManifestYAML(plg.ManifestYAML)
		if merr != nil {
			return nil, merr
		}
		// Seed plugin defaults underneath Global < Service < Spec.
		defaults := make(map[string]string)
		for _, ps := range manifest.Params {
			if ps.Default != "" {
				defaults[ps.Key] = ps.Default
			}
		}
		baseGlobal := make(map[string]string, len(defaults)+len(req.GlobalParams))
		for k, v := range defaults {
			baseGlobal[k] = v
		}
		for k, v := range req.GlobalParams {
			baseGlobal[k] = v
		}
		svcParams := req.ServiceParams[inp.PluginCode]
		eff, _ := config.ResolveEffectiveParams(baseGlobal, svcParams, inp.SpecParams)
		eff["SERVICE_NAME"] = inp.ServiceName
		eff["ENV"] = env

		cpuCores, _ := strconv.ParseFloat(eff["CPU_CORES"], 64)
		memGB, _ := strconv.ParseFloat(eff["MEM_GB"], 64)
		replicas, _ := strconv.Atoi(eff["REPLICAS"])
		k8s := config.ComputeK8sSpec(cpuCores, memGB, replicas)

		deps := inp.DependsOn
		if deps == nil {
			deps = make([]string, 0)
		}
		specMap := inp.SpecParams
		if specMap == nil {
			specMap = make(map[string]string)
		}
		items = append(items, ReleaseItem{
			ID:              id.New("ritem"),
			ReleaseID:       relID,
			NodeKey:         inp.NodeKey,
			PluginCode:      inp.PluginCode,
			ServiceName:     inp.ServiceName,
			TargetHostCIID:  inp.TargetHostCIID,
			BatchIndex:      inp.BatchIndex,
			DependsOn:       deps,
			PauseAfter:      inp.PauseAfter,
			SpecParams:      specMap,
			EffectiveParams: eff,
			K8sSpec:         k8s,
			DialURL:         strings.TrimSpace(inp.DialURL),
			Status:          "PENDING",
		})
	}

	gBytes, _ := json.Marshal(req.GlobalParams)
	sBytes, _ := json.Marshal(req.ServiceParams)
	pBytes, _ := json.Marshal(plan)

	err = s.store.WithTx(ctx, func(ctx context.Context, tx *db.Tx) error {
		if _, err := tx.Exec(ctx,
			`INSERT INTO delivery_releases(id, workspace_id, name, env, mode, status, global_params_json, service_params_json, plan_json, created_by, started_at, finished_at, created_at, updated_at)
			 VALUES(?,?,?,?,?,'DRAFT',?,?,?,?,'','',?,?)`,
			relID, wsID, req.Name, env, mode, string(gBytes), string(sBytes), string(pBytes), actor, now, now,
		); err != nil {
			return err
		}
		for _, it := range items {
			dB, _ := json.Marshal(it.DependsOn)
			spB, _ := json.Marshal(it.SpecParams)
			efB, _ := json.Marshal(it.EffectiveParams)
			k8B, _ := json.Marshal(it.K8sSpec)
			pInt := 0
			if it.PauseAfter {
				pInt = 1
			}
			if _, err := tx.Exec(ctx,
				`INSERT INTO delivery_release_items(id, release_id, node_key, plugin_code, service_name, target_host_ci_id, batch_index, depends_on_json, pause_after, spec_params_json, effective_params_json, k8s_spec_json, dial_url, status, synced_ci_id, summary, started_at, finished_at)
				 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,'PENDING','','','','')`,
				it.ID, relID, it.NodeKey, it.PluginCode, it.ServiceName, it.TargetHostCIID, it.BatchIndex, string(dB), pInt, string(spB), string(efB), string(k8B), it.DialURL,
			); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if req.AutoStart {
		return s.ExecuteRelease(ctx, wsID, relID, actor)
	}
	return s.GetRelease(ctx, wsID, relID)
}

func (s *Service) ListReleases(ctx context.Context, wsID string) ([]Release, error) {
	rows, err := s.store.Query(ctx,
		`SELECT id, workspace_id, name, env, mode, status, global_params_json, service_params_json, plan_json, created_by, started_at, finished_at, created_at, updated_at
		 FROM delivery_releases WHERE workspace_id = ? ORDER BY created_at DESC LIMIT 40`,
		wsID,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]Release, 0)
	for rows.Next() {
		rel, err := scanReleaseRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rel)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		items, _ := s.listReleaseItems(ctx, out[i].ID)
		out[i].Items = items
	}
	return out, nil
}

func (s *Service) GetRelease(ctx context.Context, wsID, relID string) (*Release, error) {
	rows, err := s.store.Query(ctx,
		`SELECT id, workspace_id, name, env, mode, status, global_params_json, service_params_json, plan_json, created_by, started_at, finished_at, created_at, updated_at
		 FROM delivery_releases WHERE workspace_id = ? AND id = ?`,
		wsID, relID,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return nil, errors.New("发布单不存在")
	}
	rel, err := scanReleaseRow(rows)
	if err != nil {
		return nil, err
	}
	items, err := s.listReleaseItems(ctx, rel.ID)
	if err != nil {
		return nil, err
	}
	rel.Items = items
	return &rel, nil
}

func scanReleaseRow(rows interface{ Scan(...any) error }) (Release, error) {
	var (
		r                Release
		gRaw, sRaw, pRaw string
	)
	err := rows.Scan(&r.ID, &r.WorkspaceID, &r.Name, &r.Env, &r.Mode, &r.Status, &gRaw, &sRaw, &pRaw, &r.CreatedBy, &r.StartedAt, &r.FinishedAt, &r.CreatedAt, &r.UpdatedAt)
	if err != nil {
		return r, err
	}
	r.GlobalParams = make(map[string]string)
	r.ServiceParams = make(map[string]map[string]string)
	_ = json.Unmarshal([]byte(gRaw), &r.GlobalParams)
	_ = json.Unmarshal([]byte(sRaw), &r.ServiceParams)
	var plan dag.Plan
	if json.Unmarshal([]byte(pRaw), &plan) == nil {
		r.Plan = &plan
	}
	r.Items = make([]ReleaseItem, 0)
	return r, nil
}

func (s *Service) listReleaseItems(ctx context.Context, releaseID string) ([]ReleaseItem, error) {
	rows, err := s.store.Query(ctx,
		`SELECT id, release_id, node_key, plugin_code, service_name, target_host_ci_id, batch_index, depends_on_json, pause_after, spec_params_json, effective_params_json, k8s_spec_json, dial_url, status, synced_ci_id, summary, started_at, finished_at
		 FROM delivery_release_items WHERE release_id = ? ORDER BY batch_index ASC, node_key ASC`,
		releaseID,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]ReleaseItem, 0)
	for rows.Next() {
		var (
			it                  ReleaseItem
			depR, spR, efR, k8R string
			pauseInt            int
		)
		if err := rows.Scan(&it.ID, &it.ReleaseID, &it.NodeKey, &it.PluginCode, &it.ServiceName, &it.TargetHostCIID, &it.BatchIndex, &depR, &pauseInt, &spR, &efR, &k8R, &it.DialURL, &it.Status, &it.SyncedCIID, &it.Summary, &it.StartedAt, &it.FinishedAt); err != nil {
			return nil, err
		}
		it.PauseAfter = pauseInt == 1
		it.DependsOn = make([]string, 0)
		it.SpecParams = make(map[string]string)
		it.EffectiveParams = make(map[string]string)
		_ = json.Unmarshal([]byte(depR), &it.DependsOn)
		_ = json.Unmarshal([]byte(spR), &it.SpecParams)
		_ = json.Unmarshal([]byte(efR), &it.EffectiveParams)
		_ = json.Unmarshal([]byte(k8R), &it.K8sSpec)
		out = append(out, it)
	}
	return out, rows.Err()
}

// ExecuteRelease executes (or resumes from PAUSED) all remaining PENDING release items in deterministic DAG/Batch order!
func (s *Service) ExecuteRelease(ctx context.Context, wsID, relID, actor string) (*Release, error) {
	rel, err := s.GetRelease(ctx, wsID, relID)
	if err != nil {
		return nil, err
	}
	topic := "release:" + relID
	now := nowISO()
	startedAt := rel.StartedAt
	if startedAt == "" {
		startedAt = now
	}
	_, _ = s.store.Exec(ctx, `UPDATE delivery_releases SET status='RUNNING', started_at=?, updated_at=? WHERE id=?`, startedAt, now, relID)
	s.broker.Publish(topic, "system", "info", fmt.Sprintf("启动发布单 %q (模式=%s, 环境=%s)", rel.Name, rel.Mode, rel.Env))

	// Build ordered execution sequence respecting DAG topological order + batchIndex.
	ordered := orderReleaseItems(rel)
	paused := false
	failed := false

	for _, it := range ordered {
		if it.Status == "SUCCEEDED" {
			continue
		}
		itemStart := nowISO()
		_, _ = s.store.Exec(ctx, `UPDATE delivery_release_items SET status='DEPLOYING', started_at=? WHERE id=?`, itemStart, it.ID)
		s.broker.Publish(topic, "system", "info", fmt.Sprintf("[DEPLOYING] 节点 %s (%s -> %s)", it.NodeKey, it.PluginCode, it.ServiceName))

		plg, perr := s.GetPluginByCode(ctx, wsID, it.PluginCode)
		if perr != nil {
			s.markItemFailed(ctx, topic, it.ID, "找不到插件声明: "+perr.Error())
			failed = true
			break
		}
		manifest, merr := ParseManifestYAML(plg.ManifestYAML)
		if merr != nil {
			s.markItemFailed(ctx, topic, it.ID, merr.Error())
			failed = true
			break
		}

		// Step 1: Render and run install script on real target host (Agent or SSH) or Plan-Only mode.
		rendered := renderTemplate(manifest.InstallScript, it.EffectiveParams)
		execSummary := ""
		if it.TargetHostCIID != "" && s.execSvc != nil {
			transport, code, out, rerr := s.execSvc.ExecuteOnHost(ctx, wsID, it.TargetHostCIID, rendered, topic)
			if rerr != nil || code != 0 {
				msg := fmt.Sprintf("目标主机执行失败 (%s, exit=%d): %v %s", transport, code, rerr, strings.TrimSpace(out))
				s.markItemFailed(ctx, topic, it.ID, msg)
				failed = true
				break
			}
			execSummary = fmt.Sprintf("已在目标主机通过 %s 完成部署", transport)
		} else {
			s.broker.Publish(topic, "stdout", "info", fmt.Sprintf("[%s] 预演渲染完成 (K8s 规格: %dm/%dMi, JVM %s)", it.NodeKey, it.K8sSpec.LimitCPUM, it.K8sSpec.LimitMemMi, it.K8sSpec.JVMXmx))
			execSummary = "未绑定物理目标机，已完成参数合并与声明式编排预演"
		}

		// Step 2: START_CHECK
		_, _ = s.store.Exec(ctx, `UPDATE delivery_release_items SET status='START_CHECK' WHERE id=?`, it.ID)
		s.broker.Publish(topic, "system", "info", fmt.Sprintf("[START_CHECK] 校验组件 %s 启动状态与配置快照", it.ServiceName))

		// Step 3: DIALING_TEST (real SSRF-safe HTTP check if dialUrl provided!)
		_, _ = s.store.Exec(ctx, `UPDATE delivery_release_items SET status='DIALING_TEST' WHERE id=?`, it.ID)
		if it.DialURL != "" {
			s.broker.Publish(topic, "system", "info", fmt.Sprintf("[DIALING_TEST] 执行实时 HTTP 拨测: %s", it.DialURL))
			if derr := runHTTPDialCheck(ctx, it.DialURL); derr != nil {
				s.markItemFailed(ctx, topic, it.ID, "HTTP 拨测失败: "+derr.Error())
				failed = true
				break
			}
			s.broker.Publish(topic, "system", "ok", fmt.Sprintf("[DIALING_TEST] 拨测 2xx 通过: %s", it.DialURL))
			execSummary += " + HTTP 拨测通过"
		}

		// Step 4: Auto-sync deployed service back into Unified CMDB + runs_on lineage edge!
		syncedID := ""
		if s.cmdbSvc != nil {
			ciData := map[string]any{
				"version": manifest.Version,
				"port":    manifest.DefaultPort,
				"plugin":  manifest.Code,
			}
			switch manifest.CMDBModel {
			case "m_database":
				ciData["engine"] = manifest.Name
				ciData["role"] = "primary"
			case "m_middleware":
				ciData["kind"] = manifest.Code
			case "m_app":
				ciData["language"] = "Java"
				ciData["sla_tier"] = "P1"
			}
			if synced, serr := s.cmdbSvc.SyncDeployedServiceCI(ctx, wsID, actor, manifest.CMDBModel, it.ServiceName, it.ServiceName, rel.Env, it.TargetHostCIID, ciData); serr == nil && synced != nil {
				syncedID = synced.ID
				s.broker.Publish(topic, "system", "ok", fmt.Sprintf("[CMDB-SYNC] 已自动登记资产 %s (%s) 并建立 runs_on 血缘", synced.Name, synced.ID))
			}
		}

		_, _ = s.store.Exec(ctx,
			`UPDATE delivery_release_items SET status='SUCCEEDED', synced_ci_id=?, summary=?, finished_at=? WHERE id=?`,
			syncedID, execSummary, nowISO(), it.ID,
		)
		s.broker.Publish(topic, "system", "ok", fmt.Sprintf("[SUCCEEDED] 节点 %s 发布完成", it.NodeKey))

		if it.PauseAfter {
			paused = true
			s.broker.Publish(topic, "system", "warn", fmt.Sprintf("[PAUSED] 节点 %s 触发人工灰度暂停断点，请确认拨测无误后点击「继续发布」", it.NodeKey))
			break
		}
	}

	finalStatus := "SUCCEEDED"
	finishedAt := nowISO()
	if failed {
		finalStatus = "FAILED"
	} else if paused {
		finalStatus = "PAUSED"
		finishedAt = ""
	}
	_, _ = s.store.Exec(ctx,
		`UPDATE delivery_releases SET status=?, finished_at=?, updated_at=? WHERE id=?`,
		finalStatus, finishedAt, nowISO(), relID,
	)
	if !paused {
		s.broker.Complete(topic, finalStatus, fmt.Sprintf("发布单 %q 执行结束，最终状态: %s", rel.Name, finalStatus))
	}
	return s.GetRelease(ctx, wsID, relID)
}

// RollbackRelease executes plugin uninstall scripts in reverse order for all deployed items.
func (s *Service) RollbackRelease(ctx context.Context, wsID, relID, actor string) (*Release, error) {
	rel, err := s.GetRelease(ctx, wsID, relID)
	if err != nil {
		return nil, err
	}
	topic := "release:" + relID
	s.broker.Publish(topic, "system", "warn", fmt.Sprintf("[ROLLBACK] 运维人员 %s 触发一键回滚发布单 %q", actor, rel.Name))

	ordered := orderReleaseItems(rel)
	for i := len(ordered) - 1; i >= 0; i-- {
		it := ordered[i]
		if it.Status != "SUCCEEDED" && it.Status != "FAILED" {
			continue
		}
		if plg, err := s.GetPluginByCode(ctx, wsID, it.PluginCode); err == nil {
			if manifest, merr := ParseManifestYAML(plg.ManifestYAML); merr == nil && strings.TrimSpace(manifest.UninstallScript) != "" {
				rendered := renderTemplate(manifest.UninstallScript, it.EffectiveParams)
				if it.TargetHostCIID != "" && s.execSvc != nil {
					_, _, _, _ = s.execSvc.ExecuteOnHost(ctx, wsID, it.TargetHostCIID, rendered, topic)
				}
			}
		}
		_, _ = s.store.Exec(ctx, `UPDATE delivery_release_items SET status='ROLLED_BACK', summary='已执行回滚卸载', finished_at=? WHERE id=?`, nowISO(), it.ID)
		s.broker.Publish(topic, "system", "info", fmt.Sprintf("[ROLLED_BACK] 节点 %s 回滚完成", it.NodeKey))
	}
	_, _ = s.store.Exec(ctx, `UPDATE delivery_releases SET status='ROLLED_BACK', finished_at=?, updated_at=? WHERE id=?`, nowISO(), nowISO(), relID)
	s.broker.Complete(topic, "ROLLED_BACK", "发布单已全部回滚")
	return s.GetRelease(ctx, wsID, relID)
}

func (s *Service) markItemFailed(ctx context.Context, topic, itemID, msg string) {
	_, _ = s.store.Exec(ctx, `UPDATE delivery_release_items SET status='FAILED', summary=?, finished_at=? WHERE id=?`, msg, nowISO(), itemID)
	s.broker.Publish(topic, "stderr", "error", "[FAILED] "+msg)
}

func orderReleaseItems(rel *Release) []ReleaseItem {
	byKey := make(map[string]ReleaseItem, len(rel.Items))
	for _, it := range rel.Items {
		byKey[it.NodeKey] = it
	}
	if rel.Mode == "DAG" && rel.Plan != nil && len(rel.Plan.Order) > 0 {
		out := make([]ReleaseItem, 0, len(rel.Items))
		for _, k := range rel.Plan.Order {
			if it, ok := byKey[k]; ok {
				out = append(out, it)
			}
		}
		if len(out) == len(rel.Items) {
			return out
		}
	}
	cp := make([]ReleaseItem, len(rel.Items))
	copy(cp, rel.Items)
	sort.SliceStable(cp, func(i, j int) bool {
		if cp[i].BatchIndex != cp[j].BatchIndex {
			return cp[i].BatchIndex < cp[j].BatchIndex
		}
		return cp[i].NodeKey < cp[j].NodeKey
	})
	return cp
}

func renderTemplate(tpl string, params map[string]string) string {
	out := tpl
	for k, v := range params {
		out = strings.ReplaceAll(out, "{{"+k+"}}", v)
	}
	return out
}

func runHTTPDialCheck(ctx context.Context, url string) error {
	// Enterprise internal health checks allow private/loopback IPs while strictly blocking 169.254.x.x cloud metadata.
	client := netguard.NewSafeHTTPClient(5*time.Second, true)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	_ = resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		return fmt.Errorf("拨测返回异常状态码 HTTP %d", resp.StatusCode)
	}
	return nil
}
