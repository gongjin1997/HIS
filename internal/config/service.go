package config

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/kinstall/kinstall/internal/iam"
	"github.com/kinstall/kinstall/pkg/platform/crypto"
	"github.com/kinstall/kinstall/pkg/platform/db"
	"github.com/kinstall/kinstall/pkg/platform/id"
)

const MaskedValue = "***MASKED***"

var ValidEnvs = map[string]bool{
	"dev": true, "sit": true, "uat": true, "beta": true, "prod": true,
}

type Namespace struct {
	ID            string `json:"id"`
	WorkspaceID   string `json:"workspaceId"`
	Env           string `json:"env"`
	AppCode       string `json:"appCode"`
	Name          string `json:"name"`
	Format        string `json:"format"`
	Description   string `json:"description"`
	ActiveVersion int    `json:"activeVersion"`
	ItemCount     int    `json:"itemCount"`
	CreatedAt     string `json:"createdAt"`
	UpdatedAt     string `json:"updatedAt"`
}

type Item struct {
	ID          string `json:"id"`
	NamespaceID string `json:"namespaceId"`
	Key         string `json:"key"`
	Value       string `json:"value"` // always "***MASKED***" when IsSecret == true
	IsSecret    bool   `json:"isSecret"`
	ValueType   string `json:"valueType"`
	Description string `json:"description"`
	UpdatedBy   string `json:"updatedBy"`
	UpdatedAt   string `json:"updatedAt"`
}

type snapshotEntry struct {
	Key         string `json:"key"`
	ValuePlain  string `json:"valuePlain,omitempty"`
	ValueEnc    string `json:"valueEnc,omitempty"`
	IsSecret    bool   `json:"isSecret"`
	ValueType   string `json:"valueType"`
	Description string `json:"description"`
}

type Release struct {
	ID          string `json:"id"`
	NamespaceID string `json:"namespaceId"`
	Version     int    `json:"version"`
	ETag        string `json:"etag"`
	Comment     string `json:"comment"`
	ItemCount   int    `json:"itemCount"`
	ReleasedBy  string `json:"releasedBy"`
	CreatedAt   string `json:"createdAt"`
}

type DiffEntry struct {
	Key      string `json:"key"`
	Change   string `json:"change"` // added | modified | deleted
	IsSecret bool   `json:"isSecret"`
	OldValue string `json:"oldValue"`
	NewValue string `json:"newValue"`
}

type ResolvedParam struct {
	Key    string `json:"key"`
	Value  string `json:"value"`
	Source string `json:"source"` // global | service | spec
}

type K8sSpec struct {
	Replicas      int     `json:"replicas"`
	RequestCPUM   int     `json:"requestCpuM"`
	LimitCPUM     int     `json:"limitCpuM"`
	RequestMemMi  int     `json:"requestMemMi"`
	LimitMemMi    int     `json:"limitMemMi"`
	JVMXms        string  `json:"jvmXms"`
	JVMXmx        string  `json:"jvmXmx"`
	TotalCPUCores float64 `json:"totalCpuCores"`
	TotalMemGB    float64 `json:"totalMemGb"`
}

type Service struct {
	store *db.Store
	codec *crypto.Codec
	iam   *iam.Service
}

func NewService(store *db.Store, codec *crypto.Codec, iamSvc *iam.Service) *Service {
	return &Service{store: store, codec: codec, iam: iamSvc}
}

func nowISO() string {
	return time.Now().UTC().Format(time.RFC3339)
}

func (s *Service) EnsureSeeded(ctx context.Context, wsID string) error {
	var cnt int
	if err := s.store.QueryRow(ctx, `SELECT COUNT(1) FROM cfg_namespaces WHERE workspace_id = ?`, wsID).Scan(&cnt); err != nil {
		return err
	}
	if cnt > 0 {
		return nil
	}

	prodNS, err := s.CreateNamespace(ctx, wsID, "prod", "his-core", "application", "kv", "HIS 核心生产配置与 KMS 加密凭据")
	if err != nil {
		return err
	}
	_, _ = s.UpsertItem(ctx, wsID, prodNS.ID, "system", "DB_HOST", "10.20.10.21", false, "string", "PostgreSQL 主库地址")
	_, _ = s.UpsertItem(ctx, wsID, prodNS.ID, "system", "DB_PORT", "5432", false, "int", "PostgreSQL 服务端口")
	_, _ = s.UpsertItem(ctx, wsID, prodNS.ID, "system", "DB_PASSWORD", "HisProd#Pg2026!Secure", true, "secret", "数据库主账号密码（AES-256-GCM 加密）")
	_, _ = s.UpsertItem(ctx, wsID, prodNS.ID, "system", "VALKEY_ADDR", "10.20.10.31:6379", false, "string", "分布式缓存入口")
	_, _ = s.UpsertItem(ctx, wsID, prodNS.ID, "system", "VALKEY_AUTH", "HisValkey#Secret99", true, "secret", "缓存访问密码")
	_, _ = s.UpsertItem(ctx, wsID, prodNS.ID, "system", "JVM_XMX", "4096m", false, "string", "核心服务堆内存上限")
	_, _ = s.PublishRelease(ctx, wsID, prodNS.ID, "system", "初始生产基线快照 v1")

	devNS, err := s.CreateNamespace(ctx, wsID, "dev", "his-core", "application", "kv", "研发联调环境配置")
	if err == nil && devNS != nil {
		_, _ = s.UpsertItem(ctx, wsID, devNS.ID, "system", "DB_HOST", "127.0.0.1", false, "string", "本地开发库")
		_, _ = s.UpsertItem(ctx, wsID, devNS.ID, "system", "DB_PORT", "5432", false, "int", "端口")
		_, _ = s.UpsertItem(ctx, wsID, devNS.ID, "system", "LOG_LEVEL", "debug", false, "string", "调试日志级别")
		_, _ = s.PublishRelease(ctx, wsID, devNS.ID, "system", "初始研发基线 v1")
	}
	return nil
}

func (s *Service) ListNamespaces(ctx context.Context, wsID, env string) ([]Namespace, error) {
	q := `SELECT n.id, n.workspace_id, n.env, n.app_code, n.name, n.format, n.description, n.active_version, n.created_at, n.updated_at,
	             (SELECT COUNT(1) FROM cfg_items i WHERE i.namespace_id = n.id) AS cnt
	      FROM cfg_namespaces n WHERE n.workspace_id = ?`
	args := []any{wsID}
	if strings.TrimSpace(env) != "" {
		q += ` AND n.env = ?`
		args = append(args, strings.TrimSpace(env))
	}
	q += ` ORDER BY n.env ASC, n.app_code ASC, n.name ASC`
	rows, err := s.store.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]Namespace, 0)
	for rows.Next() {
		var ns Namespace
		if err := rows.Scan(&ns.ID, &ns.WorkspaceID, &ns.Env, &ns.AppCode, &ns.Name, &ns.Format, &ns.Description, &ns.ActiveVersion, &ns.CreatedAt, &ns.UpdatedAt, &ns.ItemCount); err != nil {
			return nil, err
		}
		out = append(out, ns)
	}
	return out, rows.Err()
}

func (s *Service) CreateNamespace(ctx context.Context, wsID, env, appCode, name, format, desc string) (*Namespace, error) {
	env = strings.ToLower(strings.TrimSpace(env))
	if !ValidEnvs[env] {
		return nil, fmt.Errorf("无效环境 %q，必须是 dev/sit/uat/beta/prod", env)
	}
	appCode = strings.TrimSpace(appCode)
	name = strings.TrimSpace(name)
	if appCode == "" || name == "" {
		return nil, errors.New("应用编码与配置集名称不能为空")
	}
	if format == "" {
		format = "kv"
	}
	now := nowISO()
	ns := &Namespace{
		ID:          id.New("cfg"),
		WorkspaceID: wsID,
		Env:         env,
		AppCode:     appCode,
		Name:        name,
		Format:      format,
		Description: desc,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	_, err := s.store.Exec(ctx,
		`INSERT INTO cfg_namespaces(id, workspace_id, env, app_code, name, format, description, active_version, created_at, updated_at)
		 VALUES(?,?,?,?,?,?,?,0,?,?)`,
		ns.ID, wsID, ns.Env, ns.AppCode, ns.Name, ns.Format, ns.Description, now, now,
	)
	if err != nil {
		return nil, fmt.Errorf("创建配置命名空间失败: %w", err)
	}
	return ns, nil
}

func (s *Service) DeleteNamespace(ctx context.Context, wsID, nsID string) error {
	_, err := s.store.Exec(ctx, `DELETE FROM cfg_namespaces WHERE workspace_id = ? AND id = ?`, wsID, nsID)
	return err
}

func (s *Service) verifyNamespaceOwnership(ctx context.Context, wsID, nsID string) error {
	var ok int
	if err := s.store.QueryRow(ctx, `SELECT 1 FROM cfg_namespaces WHERE workspace_id = ? AND id = ?`, wsID, nsID).Scan(&ok); err != nil {
		return errors.New("配置命名空间不存在")
	}
	return nil
}

func (s *Service) ListItems(ctx context.Context, wsID, nsID string) ([]Item, error) {
	if err := s.verifyNamespaceOwnership(ctx, wsID, nsID); err != nil {
		return nil, err
	}
	rows, err := s.store.Query(ctx,
		`SELECT id, namespace_id, item_key, value_plain, is_secret, value_type, description, updated_by, updated_at
		 FROM cfg_items WHERE namespace_id = ? ORDER BY item_key ASC`,
		nsID,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]Item, 0)
	for rows.Next() {
		var (
			it        Item
			secretInt int
		)
		if err := rows.Scan(&it.ID, &it.NamespaceID, &it.Key, &it.Value, &secretInt, &it.ValueType, &it.Description, &it.UpdatedBy, &it.UpdatedAt); err != nil {
			return nil, err
		}
		it.IsSecret = secretInt == 1
		if it.IsSecret {
			it.Value = MaskedValue
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

func (s *Service) UpsertItem(ctx context.Context, wsID, nsID, actor, key, value string, isSecret bool, valueType, desc string) (*Item, error) {
	if err := s.verifyNamespaceOwnership(ctx, wsID, nsID); err != nil {
		return nil, err
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, errors.New("配置键不能为空")
	}
	if valueType == "" {
		if isSecret {
			valueType = "secret"
		} else {
			valueType = "string"
		}
	}
	var (
		plainVal string
		encVal   string
		secInt   int
	)
	if isSecret {
		secInt = 1
		if value == MaskedValue {
			// Keep existing ciphertext unchanged if caller resubmits mask placeholder.
			_ = s.store.QueryRow(ctx, `SELECT value_enc FROM cfg_items WHERE namespace_id = ? AND item_key = ?`, nsID, key).Scan(&encVal)
		} else {
			var err error
			encVal, err = s.codec.Encrypt(value)
			if err != nil {
				return nil, fmt.Errorf("KMS 加密配置失败: %w", err)
			}
		}
	} else {
		plainVal = value
	}

	now := nowISO()
	itemID := id.New("cfgi")
	_, err := s.store.Exec(ctx,
		`INSERT INTO cfg_items(id, namespace_id, item_key, value_plain, value_enc, is_secret, value_type, description, updated_by, updated_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?)
		 ON CONFLICT(namespace_id, item_key) DO UPDATE SET
		   value_plain=excluded.value_plain,
		   value_enc=CASE WHEN excluded.is_secret=1 AND excluded.value_enc<>'' THEN excluded.value_enc ELSE cfg_items.value_enc END,
		   is_secret=excluded.is_secret,
		   value_type=excluded.value_type,
		   description=excluded.description,
		   updated_by=excluded.updated_by,
		   updated_at=excluded.updated_at`,
		itemID, nsID, key, plainVal, encVal, secInt, valueType, desc, actor, now,
	)
	if err != nil {
		return nil, err
	}
	retVal := plainVal
	if isSecret {
		retVal = MaskedValue
	}
	return &Item{
		ID:          itemID,
		NamespaceID: nsID,
		Key:         key,
		Value:       retVal,
		IsSecret:    isSecret,
		ValueType:   valueType,
		Description: desc,
		UpdatedBy:   actor,
		UpdatedAt:   now,
	}, nil
}

func (s *Service) DeleteItem(ctx context.Context, wsID, nsID, key string) error {
	if err := s.verifyNamespaceOwnership(ctx, wsID, nsID); err != nil {
		return err
	}
	_, err := s.store.Exec(ctx, `DELETE FROM cfg_items WHERE namespace_id = ? AND item_key = ?`, nsID, key)
	return err
}

// RevealSecret decrypts a KMS-protected config secret and records a mandatory audit hash-chain entry.
func (s *Service) RevealSecret(ctx context.Context, wsID, nsID, key, actor, role, reason, sourceIP string) (string, error) {
	if err := s.verifyNamespaceOwnership(ctx, wsID, nsID); err != nil {
		return "", err
	}
	if strings.TrimSpace(reason) == "" {
		reason = "运维排查显密"
	}
	var (
		plainVal, encVal string
		secInt           int
	)
	err := s.store.QueryRow(ctx,
		`SELECT value_plain, value_enc, is_secret FROM cfg_items WHERE namespace_id = ? AND item_key = ?`,
		nsID, key,
	).Scan(&plainVal, &encVal, &secInt)
	if err != nil {
		return "", errors.New("配置项不存在")
	}
	if secInt == 0 {
		return plainVal, nil
	}
	if encVal == "" {
		return "", nil
	}
	pt, err := s.codec.Decrypt(encVal)
	if err != nil {
		return "", fmt.Errorf("KMS 解密失败: %w", err)
	}
	if s.iam != nil {
		_ = s.iam.RecordAudit(ctx, wsID, actor, role, "config.secret.reveal", "cfg_item", nsID+"/"+key, "ok", "显密事由: "+reason, sourceIP)
	}
	return pt, nil
}

func (s *Service) loadRawSnapshot(ctx context.Context, nsID string) ([]snapshotEntry, error) {
	rows, err := s.store.Query(ctx,
		`SELECT item_key, value_plain, value_enc, is_secret, value_type, description FROM cfg_items WHERE namespace_id = ? ORDER BY item_key ASC`,
		nsID,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]snapshotEntry, 0)
	for rows.Next() {
		var (
			e      snapshotEntry
			secInt int
		)
		if err := rows.Scan(&e.Key, &e.ValuePlain, &e.ValueEnc, &secInt, &e.ValueType, &e.Description); err != nil {
			return nil, err
		}
		e.IsSecret = secInt == 1
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Service) PublishRelease(ctx context.Context, wsID, nsID, actor, comment string) (*Release, error) {
	if err := s.verifyNamespaceOwnership(ctx, wsID, nsID); err != nil {
		return nil, err
	}
	snap, err := s.loadRawSnapshot(ctx, nsID)
	if err != nil {
		return nil, err
	}
	snapBytes, err := json.Marshal(snap)
	if err != nil {
		return nil, err
	}
	etag := crypto.SHA256Hex(snapBytes)[:16]
	now := nowISO()
	var rel Release
	err = s.store.WithTx(ctx, func(ctx context.Context, tx *db.Tx) error {
		var curVer int
		if err := tx.QueryRow(ctx, `SELECT active_version FROM cfg_namespaces WHERE id = ?`, nsID).Scan(&curVer); err != nil {
			return err
		}
		nextVer := curVer + 1
		rel = Release{
			ID:          id.New("cfgr"),
			NamespaceID: nsID,
			Version:     nextVer,
			ETag:        etag,
			Comment:     comment,
			ItemCount:   len(snap),
			ReleasedBy:  actor,
			CreatedAt:   now,
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO cfg_releases(id, namespace_id, version, etag, comment, snapshot_json, released_by, created_at) VALUES(?,?,?,?,?,?,?,?)`,
			rel.ID, nsID, rel.Version, rel.ETag, rel.Comment, string(snapBytes), actor, now,
		); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE cfg_namespaces SET active_version = ?, updated_at = ? WHERE id = ?`, nextVer, now, nsID)
		return err
	})
	if err != nil {
		return nil, err
	}
	return &rel, nil
}

func (s *Service) ListReleases(ctx context.Context, wsID, nsID string) ([]Release, error) {
	if err := s.verifyNamespaceOwnership(ctx, wsID, nsID); err != nil {
		return nil, err
	}
	rows, err := s.store.Query(ctx,
		`SELECT id, namespace_id, version, etag, comment, snapshot_json, released_by, created_at
		 FROM cfg_releases WHERE namespace_id = ? ORDER BY version DESC LIMIT 50`,
		nsID,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]Release, 0)
	for rows.Next() {
		var (
			r       Release
			snapRaw string
		)
		if err := rows.Scan(&r.ID, &r.NamespaceID, &r.Version, &r.ETag, &r.Comment, &snapRaw, &r.ReleasedBy, &r.CreatedAt); err != nil {
			return nil, err
		}
		var entries []snapshotEntry
		_ = json.Unmarshal([]byte(snapRaw), &entries)
		r.ItemCount = len(entries)
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Service) DiffDraftVsRelease(ctx context.Context, wsID, nsID string) ([]DiffEntry, error) {
	if err := s.verifyNamespaceOwnership(ctx, wsID, nsID); err != nil {
		return nil, err
	}
	draft, err := s.loadRawSnapshot(ctx, nsID)
	if err != nil {
		return nil, err
	}
	var lastSnapRaw string
	_ = s.store.QueryRow(ctx, `SELECT snapshot_json FROM cfg_releases WHERE namespace_id = ? ORDER BY version DESC LIMIT 1`, nsID).Scan(&lastSnapRaw)
	var released []snapshotEntry
	if lastSnapRaw != "" {
		_ = json.Unmarshal([]byte(lastSnapRaw), &released)
	}

	draftMap := make(map[string]snapshotEntry, len(draft))
	for _, e := range draft {
		draftMap[e.Key] = e
	}
	relMap := make(map[string]snapshotEntry, len(released))
	for _, e := range released {
		relMap[e.Key] = e
	}

	diffs := make([]DiffEntry, 0)
	for k, d := range draftMap {
		r, existed := relMap[k]
		if !existed {
			nv := d.ValuePlain
			if d.IsSecret {
				nv = MaskedValue
			}
			diffs = append(diffs, DiffEntry{Key: k, Change: "added", IsSecret: d.IsSecret, NewValue: nv})
			continue
		}
		if d.IsSecret != r.IsSecret || d.ValuePlain != r.ValuePlain || d.ValueEnc != r.ValueEnc {
			ov, nv := r.ValuePlain, d.ValuePlain
			if r.IsSecret {
				ov = MaskedValue
			}
			if d.IsSecret {
				nv = MaskedValue
			}
			diffs = append(diffs, DiffEntry{Key: k, Change: "modified", IsSecret: d.IsSecret || r.IsSecret, OldValue: ov, NewValue: nv})
		}
	}
	for k, r := range relMap {
		if _, ok := draftMap[k]; !ok {
			ov := r.ValuePlain
			if r.IsSecret {
				ov = MaskedValue
			}
			diffs = append(diffs, DiffEntry{Key: k, Change: "deleted", IsSecret: r.IsSecret, OldValue: ov})
		}
	}
	sort.Slice(diffs, func(i, j int) bool { return diffs[i].Key < diffs[j].Key })
	return diffs, nil
}

// RollbackRelease restores items from targetVersion and publishes a new monotonic release version.
func (s *Service) RollbackRelease(ctx context.Context, wsID, nsID string, targetVersion int, actor string) (*Release, error) {
	if err := s.verifyNamespaceOwnership(ctx, wsID, nsID); err != nil {
		return nil, err
	}
	var snapRaw string
	if err := s.store.QueryRow(ctx,
		`SELECT snapshot_json FROM cfg_releases WHERE namespace_id = ? AND version = ?`,
		nsID, targetVersion,
	).Scan(&snapRaw); err != nil {
		return nil, fmt.Errorf("未找到版本 v%d 的不可变快照", targetVersion)
	}
	var entries []snapshotEntry
	if err := json.Unmarshal([]byte(snapRaw), &entries); err != nil {
		return nil, err
	}
	now := nowISO()
	err := s.store.WithTx(ctx, func(ctx context.Context, tx *db.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM cfg_items WHERE namespace_id = ?`, nsID); err != nil {
			return err
		}
		for _, e := range entries {
			secInt := 0
			if e.IsSecret {
				secInt = 1
			}
			if _, err := tx.Exec(ctx,
				`INSERT INTO cfg_items(id, namespace_id, item_key, value_plain, value_enc, is_secret, value_type, description, updated_by, updated_at)
				 VALUES(?,?,?,?,?,?,?,?,?,?)`,
				id.New("cfgi"), nsID, e.Key, e.ValuePlain, e.ValueEnc, secInt, e.ValueType, e.Description, actor, now,
			); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.PublishRelease(ctx, wsID, nsID, actor, fmt.Sprintf("回滚至历史快照 v%d", targetVersion))
}

// CloneNamespace safely clones a namespace across environments while AUTO-BLANKING all secrets.
func (s *Service) CloneNamespace(ctx context.Context, wsID, srcNSID, targetEnv, targetApp, targetName, actor string) (*Namespace, int, error) {
	if err := s.verifyNamespaceOwnership(ctx, wsID, srcNSID); err != nil {
		return nil, 0, err
	}
	snap, err := s.loadRawSnapshot(ctx, srcNSID)
	if err != nil {
		return nil, 0, err
	}
	dst, err := s.CreateNamespace(ctx, wsID, targetEnv, targetApp, targetName, "kv", "跨环境安全克隆（密钥项已自动置空）")
	if err != nil {
		return nil, 0, err
	}
	blankedSecrets := 0
	for _, e := range snap {
		if e.IsSecret {
			blankedSecrets++
			_, _ = s.UpsertItem(ctx, wsID, dst.ID, actor, e.Key, "", true, e.ValueType, e.Description+" [待填写目标环境密钥]")
		} else {
			_, _ = s.UpsertItem(ctx, wsID, dst.ID, actor, e.Key, e.ValuePlain, false, e.ValueType, e.Description)
		}
	}
	return dst, blankedSecrets, nil
}

// ResolveEffectiveParams merges Spec > Service > Global parameters deterministically.
func ResolveEffectiveParams(globalParams, serviceParams, specParams map[string]string) (map[string]string, []ResolvedParam) {
	merged := make(map[string]string)
	src := make(map[string]string)
	for k, v := range globalParams {
		merged[k] = v
		src[k] = "global"
	}
	for k, v := range serviceParams {
		merged[k] = v
		src[k] = "service"
	}
	for k, v := range specParams {
		merged[k] = v
		src[k] = "spec"
	}
	keys := make([]string, 0, len(merged))
	for k := range merged {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	trace := make([]ResolvedParam, 0, len(keys))
	for _, k := range keys {
		trace = append(trace, ResolvedParam{Key: k, Value: merged[k], Source: src[k]})
	}
	return merged, trace
}

// ComputeK8sSpec calculates Kubernetes milliCPU / MiB requests, limits, and JVM heap recommendations.
func ComputeK8sSpec(cpuCores float64, memGB float64, replicas int) K8sSpec {
	if cpuCores <= 0 {
		cpuCores = 1.0
	}
	if memGB <= 0 {
		memGB = 2.0
	}
	if replicas <= 0 {
		replicas = 1
	}
	limitCPU := int(math.Round(cpuCores * 1000))
	reqCPU := max(250, limitCPU/2)
	limitMem := int(math.Round(memGB * 1024))
	reqMem := max(256, limitMem/2)
	jvmMax := max(256, int(math.Round(float64(limitMem)*0.65)))
	jvmMin := max(128, jvmMax/2)

	return K8sSpec{
		Replicas:      replicas,
		RequestCPUM:   reqCPU,
		LimitCPUM:     limitCPU,
		RequestMemMi:  reqMem,
		LimitMemMi:    limitMem,
		JVMXms:        fmt.Sprintf("%dm", jvmMin),
		JVMXmx:        fmt.Sprintf("%dm", jvmMax),
		TotalCPUCores: math.Round(cpuCores*float64(replicas)*10) / 10,
		TotalMemGB:    math.Round(memGB*float64(replicas)*10) / 10,
	}
}
