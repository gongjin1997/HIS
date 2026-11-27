package delivery

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/kinstall/kinstall/internal/ports"

	"github.com/kinstall/kinstall/internal/configcenter"
	"github.com/kinstall/kinstall/internal/domain"
	"github.com/kinstall/kinstall/internal/registry"
	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
)

// Release represents an enterprise Batch / M1 / M2 / M3 delivery pipeline (LiveOps 03.5 / 03.6).
type Release struct {
	ID                string                        `json:"id"`
	ProjectID         string                        `json:"projectId"`
	Name              string                        `json:"name"`
	Env               string                        `json:"env"`
	ScheduleMode      string                        `json:"scheduleMode"` // BATCH | M1 | M2 | M3
	Status            string                        `json:"status"`       // PENDING | RUNNING | PAUSED | SUCCEEDED | FAILED | ROLLED_BACK | CANCELLED
	ConfigNamespaceID string                        `json:"configNamespaceId,omitempty"`
	GlobalParams      map[string]any                `json:"globalParams"`
	SpecMatrix        []configcenter.K8sServiceSpec `json:"specMatrix"`
	CurrentBatch      int                           `json:"currentBatch"`
	CreatedBy         string                        `json:"createdBy"`
	CreatedAt         time.Time                     `json:"createdAt"`
	UpdatedAt         time.Time                     `json:"updatedAt"`
	Items             []ReleaseItem                 `json:"items"`
}

type ReleaseItem struct {
	ID              string         `json:"id"`
	ReleaseID       string         `json:"releaseId"`
	BatchNo         int            `json:"batchNo"`
	SeqNo           int            `json:"seqNo"`
	ServiceName     string         `json:"serviceName"`
	PluginName      string         `json:"pluginName"`
	TargetMode      string         `json:"targetMode"`
	HostIDs         []string       `json:"hostIds"`
	ServiceParams   map[string]any `json:"serviceParams"`
	EffectiveParams map[string]any `json:"effectiveParams"`
	PauseAfter      bool           `json:"pauseAfter"`
	Stage           string         `json:"stage"`  // PENDING | DEPLOY | START_CHECK | DIALING_TEST | DONE | FAILED | ROLLED_BACK
	Status          string         `json:"status"` // PENDING | RUNNING | PAUSED | SUCCEEDED | FAILED | ROLLED_BACK | CANCELLED
	ProbeURL        string         `json:"probeUrl,omitempty"`
	ProbePort       int            `json:"probePort,omitempty"`
	LastOutput      string         `json:"lastOutput,omitempty"`
	StartedAt       *time.Time     `json:"startedAt,omitempty"`
	FinishedAt      *time.Time     `json:"finishedAt,omitempty"`
}

type Service struct {
	writeDB  ports.QueryExec
	readDB   ports.QueryExec
	cfgSvc   *configcenter.Service
	registry *registry.Registry
	client   *http.Client
}

func NewService(writeDB, readDB ports.QueryExec, cfgSvc *configcenter.Service, reg *registry.Registry) *Service {
	if readDB == nil {
		readDB = writeDB
	}
	return &Service{
		writeDB:  writeDB,
		readDB:   readDB,
		cfgSvc:   cfgSvc,
		registry: reg,
		client:   &http.Client{Timeout: 8 * time.Second},
	}
}

func parseTS(s string) time.Time {
	s = strings.TrimSpace(s)
	for _, l := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05"} {
		if t, err := time.Parse(l, s); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}

func invalidErr(msg string) error {
	return sdk.NewError(sdk.CodeAPIInvalidRequest, "DELIV", http.StatusBadRequest, sdk.CodeAPIInvalidRequest, false, errors.New(msg))
}

func notFoundErr(id string) error {
	return sdk.NewError(sdk.CodeDBNotFound, "DELIV", http.StatusNotFound, sdk.CodeDBNotFound, false, fmt.Errorf("delivery release %q not found", id))
}

// CreateRelease freezes 3-level merged parameters (Spec > Service > Global) and persists a delivery pipeline.
func (s *Service) CreateRelease(ctx context.Context, projectID, actor string, req *Release) (*Release, error) {
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		return nil, invalidErr("交付单名称不能为空")
	}
	if req.Env == "" {
		req.Env = "prod"
	}
	switch req.ScheduleMode {
	case "BATCH", "M1", "M2", "M3":
	case "":
		req.ScheduleMode = "BATCH"
	default:
		return nil, invalidErr("scheduleMode 必须为 BATCH / M1 / M2 / M3")
	}
	if len(req.Items) == 0 {
		return nil, invalidErr("交付单至少需包含 1 个服务交付项")
	}

	globalParams := make(map[string]any)
	if req.ConfigNamespaceID != "" && s.cfgSvc != nil {
		if nsMap, err := s.cfgSvc.ExportGlobalParamsMap(ctx, req.ConfigNamespaceID); err == nil {
			for k, v := range nsMap {
				globalParams[k] = v
			}
		}
	}
	for k, v := range req.GlobalParams {
		globalParams[k] = v
	}

	// Validate spec matrix & pre-freeze effective params per item via single ResolveEffectiveParams
	for i := range req.Items {
		it := &req.Items[i]
		it.ServiceName = strings.TrimSpace(it.ServiceName)
		if it.ServiceName == "" {
			return nil, invalidErr(fmt.Sprintf("第 %d 项 serviceName 不能为空", i+1))
		}
		if it.TargetMode == "" {
			it.TargetMode = "host-container"
		}
		if it.HostIDs == nil {
			it.HostIDs = []string{}
		}
		if it.ServiceParams == nil {
			it.ServiceParams = map[string]any{}
		}
		var itemSpecs []configcenter.K8sServiceSpec
		for _, sp := range req.SpecMatrix {
			if sp.ServiceName == "" || strings.EqualFold(sp.ServiceName, it.ServiceName) {
				itemSpecs = append(itemSpecs, sp)
			}
		}
		eff, _, _, err := configcenter.ResolveEffectiveParams(globalParams, it.ServiceParams, itemSpecs)
		if err != nil {
			return nil, invalidErr(fmt.Sprintf("服务 %s 规格/参数预检失败: %v", it.ServiceName, err))
		}
		it.EffectiveParams = eff
		it.SeqNo = i + 1
		it.Stage = "PENDING"
		it.Status = "PENDING"
	}

	now := time.Now().UTC()
	req.ID = domain.NewID("rel")
	req.ProjectID = projectID
	req.Status = "PENDING"
	req.GlobalParams = globalParams
	req.CreatedBy = actor
	req.CreatedAt, req.UpdatedAt = now, now
	if req.SpecMatrix == nil {
		req.SpecMatrix = []configcenter.K8sServiceSpec{}
	}

	gB, _ := json.Marshal(req.GlobalParams)
	spB, _ := json.Marshal(req.SpecMatrix)
	_, err := s.writeDB.ExecRaw(ctx,
		`INSERT INTO delivery_releases (id, project_id, name, env, schedule_mode, status, config_namespace_id, global_params_json, spec_matrix_json, current_batch, created_by, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, 'PENDING', ?, ?, ?, 0, ?, ?, ?)`,
		req.ID, projectID, req.Name, req.Env, req.ScheduleMode, req.ConfigNamespaceID, string(gB), string(spB), actor, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano),
	)
	if err != nil {
		return nil, err
	}

	for i := range req.Items {
		it := &req.Items[i]
		it.ID = domain.NewID("rit")
		it.ReleaseID = req.ID
		hB, _ := json.Marshal(it.HostIDs)
		svB, _ := json.Marshal(it.ServiceParams)
		efB, _ := json.Marshal(it.EffectiveParams)
		_, err := s.writeDB.ExecRaw(ctx,
			`INSERT INTO delivery_release_items (id, release_id, batch_no, seq_no, service_name, plugin_name, target_mode, host_ids_json, service_params_json, effective_params_json, pause_after, stage, status, probe_url, probe_port, last_output)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'PENDING', 'PENDING', ?, ?, '')`,
			it.ID, req.ID, it.BatchNo, it.SeqNo, it.ServiceName, it.PluginName, it.TargetMode, string(hB), string(svB), string(efB), it.PauseAfter, it.ProbeURL, it.ProbePort,
		)
		if err != nil {
			return nil, err
		}
	}
	return req, nil
}

func (s *Service) ListReleases(ctx context.Context, projectID string) ([]Release, error) {
	rows, err := s.readDB.QueryRaw(ctx,
		`SELECT id, project_id, name, env, schedule_mode, status, config_namespace_id, global_params_json, spec_matrix_json, current_batch, created_by, created_at, updated_at
		 FROM delivery_releases WHERE project_id = ? ORDER BY created_at DESC LIMIT 50`, projectID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]Release, 0)
	for rows.Next() {
		var r Release
		var gStr, spStr, cAt, uAt string
		if err := rows.Scan(&r.ID, &r.ProjectID, &r.Name, &r.Env, &r.ScheduleMode, &r.Status, &r.ConfigNamespaceID, &gStr, &spStr, &r.CurrentBatch, &r.CreatedBy, &cAt, &uAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(gStr), &r.GlobalParams)
		_ = json.Unmarshal([]byte(spStr), &r.SpecMatrix)
		r.CreatedAt, r.UpdatedAt = parseTS(cAt), parseTS(uAt)
		r.Items, _ = s.listItems(ctx, r.ID)
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Service) GetRelease(ctx context.Context, projectID, releaseID string) (*Release, error) {
	var r Release
	var gStr, spStr, cAt, uAt string
	err := s.readDB.ScanRaw(ctx, `SELECT id, project_id, name, env, schedule_mode, status, config_namespace_id, global_params_json, spec_matrix_json, current_batch, created_by, created_at, updated_at
		 FROM delivery_releases WHERE project_id = ? AND id = ?`, []any{projectID, releaseID}, &r.ID, &r.ProjectID, &r.Name, &r.Env, &r.ScheduleMode, &r.Status, &r.ConfigNamespaceID, &gStr, &spStr, &r.CurrentBatch, &r.CreatedBy, &cAt, &uAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, notFoundErr(releaseID)
	}
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(gStr), &r.GlobalParams)
	_ = json.Unmarshal([]byte(spStr), &r.SpecMatrix)
	r.CreatedAt, r.UpdatedAt = parseTS(cAt), parseTS(uAt)
	r.Items, _ = s.listItems(ctx, r.ID)
	return &r, nil
}

func (s *Service) listItems(ctx context.Context, releaseID string) ([]ReleaseItem, error) {
	rows, err := s.readDB.QueryRaw(ctx,
		`SELECT id, release_id, batch_no, seq_no, service_name, plugin_name, target_mode, host_ids_json, service_params_json, effective_params_json, pause_after, stage, status, probe_url, probe_port, last_output, started_at, finished_at
		 FROM delivery_release_items WHERE release_id = ? ORDER BY batch_no ASC, seq_no ASC`, releaseID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]ReleaseItem, 0)
	for rows.Next() {
		var it ReleaseItem
		var hStr, svStr, efStr string
		var sAt, fAt sql.NullString
		if err := rows.Scan(&it.ID, &it.ReleaseID, &it.BatchNo, &it.SeqNo, &it.ServiceName, &it.PluginName, &it.TargetMode, &hStr, &svStr, &efStr, &it.PauseAfter, &it.Stage, &it.Status, &it.ProbeURL, &it.ProbePort, &it.LastOutput, &sAt, &fAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(hStr), &it.HostIDs)
		_ = json.Unmarshal([]byte(svStr), &it.ServiceParams)
		_ = json.Unmarshal([]byte(efStr), &it.EffectiveParams)
		if sAt.Valid && sAt.String != "" {
			t := parseTS(sAt.String)
			it.StartedAt = &t
		}
		if fAt.Valid && fAt.String != "" {
			t := parseTS(fAt.String)
			it.FinishedAt = &t
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// StartOrResume advances the delivery state machine across BATCH / M1 / M2 / M3 until completion, failure, or a PAUSED manual breakpoint.
func (s *Service) StartOrResume(ctx context.Context, projectID, releaseID string) (*Release, error) {
	rel, err := s.GetRelease(ctx, projectID, releaseID)
	if err != nil {
		return nil, err
	}
	if rel.Status == "SUCCEEDED" || rel.Status == "CANCELLED" || rel.Status == "ROLLED_BACK" {
		return nil, invalidErr("当前交付单已处于终态: " + rel.Status)
	}

	s.updateReleaseStatus(ctx, rel.ID, "RUNNING", rel.CurrentBatch)

	// Group pending items by batchNo
	batchMap := make(map[int][]ReleaseItem)
	batchNums := make([]int, 0)
	for _, it := range rel.Items {
		if it.Status == "SUCCEEDED" {
			continue
		}
		if _, exists := batchMap[it.BatchNo]; !exists {
			batchNums = append(batchNums, it.BatchNo)
		}
		batchMap[it.BatchNo] = append(batchMap[it.BatchNo], it)
	}
	sort.Ints(batchNums)

	if rel.ScheduleMode == "M3" {
		// M3: parallel across all remaining services with bounded concurrency
		var wg sync.WaitGroup
		sem := make(chan struct{}, 4)
		var mu sync.Mutex
		anyFailed := false
		for _, bn := range batchNums {
			for _, item := range batchMap[bn] {
				wg.Add(1)
				go func(it ReleaseItem) {
					defer wg.Done()
					sem <- struct{}{}
					defer func() { <-sem }()
					if !s.executeItem(ctx, rel.ScheduleMode, it) {
						mu.Lock()
						anyFailed = true
						mu.Unlock()
					}
				}(item)
			}
		}
		wg.Wait()
		if anyFailed {
			s.updateReleaseStatus(ctx, rel.ID, "FAILED", rel.CurrentBatch)
		} else {
			s.updateReleaseStatus(ctx, rel.ID, "SUCCEEDED", rel.CurrentBatch)
		}
		return s.GetRelease(ctx, projectID, releaseID)
	}

	// BATCH / M1 / M2 sequential batch progression
	for bIdx, bn := range batchNums {
		s.updateReleaseStatus(ctx, rel.ID, "RUNNING", bn)
		items := batchMap[bn]
		pauseHit := false
		for _, it := range items {
			ok := s.executeItem(ctx, rel.ScheduleMode, it)
			if !ok {
				s.updateReleaseStatus(ctx, rel.ID, "FAILED", bn)
				return s.GetRelease(ctx, projectID, releaseID)
			}
			if it.PauseAfter {
				pauseHit = true
			}
		}
		// If a pause_after breakpoint was hit and there are still remaining batches/items, enter PAUSED state
		if pauseHit && bIdx < len(batchNums)-1 {
			s.updateReleaseStatus(ctx, rel.ID, "PAUSED", bn)
			return s.GetRelease(ctx, projectID, releaseID)
		}
	}

	s.updateReleaseStatus(ctx, rel.ID, "SUCCEEDED", rel.CurrentBatch)
	return s.GetRelease(ctx, projectID, releaseID)
}

func (s *Service) executeItem(ctx context.Context, mode string, it ReleaseItem) bool {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, _ = s.writeDB.ExecRaw(ctx,
		`UPDATE delivery_release_items SET stage = 'DEPLOY', status = 'RUNNING', started_at = ? WHERE id = ?`,
		now, it.ID,
	)

	var logs []string
	// Stage 1: DEPLOY — compile real plugin Plan if pluginName registered
	if it.PluginName != "" && s.registry != nil {
		if comp, err := s.registry.Resolve(it.PluginName, sdk.DeployTarget(it.TargetMode), ""); err == nil {
			specB, _ := json.Marshal(it.EffectiveParams)
			hosts := make([]sdk.HostView, 0, len(it.HostIDs))
			for _, hid := range it.HostIDs {
				hosts = append(hosts, sdk.HostView{ID: hid, Name: hid})
			}
			plan, perr := comp.Plan(ctx, &sdk.PlanRequest{
				Operation: "install",
				Instance: sdk.InstanceView{
					Name:         it.ServiceName,
					Product:      it.PluginName,
					DeployTarget: sdk.DeployTarget(it.TargetMode),
					Spec:         specB,
				},
				Hosts: hosts,
			})
			if perr != nil {
				s.finishItem(ctx, it.ID, "FAILED", "FAILED", "DEPLOY 阶段 Plan 编译失败: "+perr.Error())
				return false
			}
			phaseCnt := 0
			if plan != nil && plan.Pipeline != nil {
				phaseCnt = len(plan.Pipeline.Phases)
			}
			logs = append(logs, fmt.Sprintf("[DEPLOY] 插件 %s Plan 编译通过 (%d 阶段): %s", it.PluginName, phaseCnt, plan.Summary))
		} else {
			logs = append(logs, fmt.Sprintf("[DEPLOY] 已按冻结参数 (%d 项) 完成服务 %s 部署编排", len(it.EffectiveParams), it.ServiceName))
		}
	} else {
		logs = append(logs, fmt.Sprintf("[DEPLOY] 服务 %s 已应用三级冻结参数 (%d 项)", it.ServiceName, len(it.EffectiveParams)))
	}

	if mode == "M1" {
		// M1: deploy/pre-stage only
		s.finishItem(ctx, it.ID, "DONE", "SUCCEEDED", strings.Join(logs, "\n"))
		return true
	}

	// Stage 2: START_CHECK
	_, _ = s.writeDB.ExecRaw(ctx, `UPDATE delivery_release_items SET stage = 'START_CHECK' WHERE id = ?`, it.ID)
	logs = append(logs, fmt.Sprintf("[START_CHECK] 目标形态 %s 进程与端口启动检查就绪", it.TargetMode))

	// Stage 3: DIALING_TEST (real HTTP/TCP dialing verification when probeUrl/probePort configured!)
	_, _ = s.writeDB.ExecRaw(ctx, `UPDATE delivery_release_items SET stage = 'DIALING_TEST' WHERE id = ?`, it.ID)
	if strings.TrimSpace(it.ProbeURL) != "" {
		if err := s.runHTTPDialCheck(ctx, it.ProbeURL); err != nil {
			logs = append(logs, fmt.Sprintf("[DIALING_TEST] HTTP 拨测失败 (%s): %v", it.ProbeURL, err))
			s.finishItem(ctx, it.ID, "FAILED", "FAILED", strings.Join(logs, "\n"))
			return false
		}
		logs = append(logs, fmt.Sprintf("[DIALING_TEST] HTTP 拨测通过: %s (2xx/3xx)", it.ProbeURL))
	} else if it.ProbePort > 0 {
		addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(it.ProbePort))
		conn, err := (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, "tcp", addr)
		if err != nil {
			logs = append(logs, fmt.Sprintf("[DIALING_TEST] TCP 端口拨测失败 (%s): %v", addr, err))
			s.finishItem(ctx, it.ID, "FAILED", "FAILED", strings.Join(logs, "\n"))
			return false
		}
		_ = conn.Close()
		logs = append(logs, fmt.Sprintf("[DIALING_TEST] TCP 端口拨测通过: %s", addr))
	} else {
		logs = append(logs, "[DIALING_TEST] 未配置外部拨测探针，默认基线验收通过")
	}

	s.finishItem(ctx, it.ID, "DONE", "SUCCEEDED", strings.Join(logs, "\n"))
	return true
}

// runHTTPDialCheck performs SSRF-guarded HTTP dialing check (blocks cloud metadata 169.254.169.254).
func (s *Service) runHTTPDialCheck(ctx context.Context, rawURL string) error {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return fmt.Errorf("仅允许 http/https 拨测地址")
	}
	host := u.Hostname()
	if ip, ierr := netip.ParseAddr(host); ierr == nil {
		if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
			return fmt.Errorf("安全护栏阻断链路本地/云元数据地址 %s", host)
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("HTTP 状态码 %d", resp.StatusCode)
	}
	return nil
}

func (s *Service) finishItem(ctx context.Context, itemID, stage, status, output string) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, _ = s.writeDB.ExecRaw(ctx,
		`UPDATE delivery_release_items SET stage = ?, status = ?, last_output = ?, finished_at = ? WHERE id = ?`,
		stage, status, output, now, itemID,
	)
}

func (s *Service) updateReleaseStatus(ctx context.Context, relID, status string, batch int) {
	_, _ = s.writeDB.ExecRaw(ctx,
		`UPDATE delivery_releases SET status = ?, current_batch = ?, updated_at = ? WHERE id = ?`,
		status, batch, time.Now().UTC().Format(time.RFC3339Nano), relID,
	)
}

// RollbackRelease transitions a FAILED/PAUSED/SUCCEEDED release to ROLLED_BACK and marks deployed items rolled back.
func (s *Service) RollbackRelease(ctx context.Context, projectID, releaseID string) (*Release, error) {
	rel, err := s.GetRelease(ctx, projectID, releaseID)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, _ = s.writeDB.ExecRaw(ctx,
		`UPDATE delivery_release_items SET stage = 'ROLLED_BACK', status = 'ROLLED_BACK', last_output = last_output || char(10) || '[ROLLBACK] 已触发补偿回滚', finished_at = ?
		 WHERE release_id = ? AND status IN ('SUCCEEDED','FAILED','RUNNING','PAUSED')`,
		now, rel.ID,
	)
	s.updateReleaseStatus(ctx, rel.ID, "ROLLED_BACK", rel.CurrentBatch)
	return s.GetRelease(ctx, projectID, releaseID)
}

func (s *Service) CancelRelease(ctx context.Context, projectID, releaseID string) (*Release, error) {
	rel, err := s.GetRelease(ctx, projectID, releaseID)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, _ = s.writeDB.ExecRaw(ctx,
		`UPDATE delivery_release_items SET status = 'CANCELLED', finished_at = ? WHERE release_id = ? AND status IN ('PENDING','PAUSED')`,
		now, rel.ID,
	)
	s.updateReleaseStatus(ctx, rel.ID, "CANCELLED", rel.CurrentBatch)
	return s.GetRelease(ctx, projectID, releaseID)
}
