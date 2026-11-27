package configcenter

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/kinstall/kinstall/internal/domain"
	"github.com/kinstall/kinstall/internal/ports"
	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
)

const MaskedSecretPlaceholder = "***MASKED***"

type Namespace struct {
	ID                string    `json:"id"`
	ProjectID         string    `json:"projectId"`
	Env               string    `json:"env"`        // dev | sit | uat | beta | prod
	Name              string    `json:"name"`       // e.g. application, global-infra, database
	FormatKind        string    `json:"formatKind"` // kv | yaml | properties
	Description       string    `json:"description"`
	CurrentReleaseVer int       `json:"currentReleaseVer"`
	ItemCount         int       `json:"itemCount"`
	CreatedAt         time.Time `json:"createdAt"`
	UpdatedAt         time.Time `json:"updatedAt"`
}

type ConfigItem struct {
	ID          string    `json:"id"`
	NamespaceID string    `json:"namespaceId"`
	ItemKey     string    `json:"itemKey"`
	ItemType    string    `json:"itemType"` // string | json | yaml | secret
	Value       string    `json:"value"`    // always "***MASKED***" on list when itemType=="secret"
	HasSecret   bool      `json:"hasSecret"`
	Description string    `json:"description"`
	Version     int       `json:"version"`
	UpdatedBy   string    `json:"updatedBy"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type SnapshotEntry struct {
	Key         string `json:"key"`
	Type        string `json:"type"`
	ValuePlain  string `json:"valuePlain,omitempty"`
	ValueCipher string `json:"valueCipher,omitempty"`
	Description string `json:"description,omitempty"`
}

type Release struct {
	ID          string          `json:"id"`
	NamespaceID string          `json:"namespaceId"`
	ReleaseVer  int             `json:"releaseVer"`
	ETag        string          `json:"etag"`
	Items       []SnapshotEntry `json:"items"`
	Comment     string          `json:"comment"`
	ORCTicketNo string          `json:"orcTicketNo"`
	PublishedBy string          `json:"publishedBy"`
	PublishedAt time.Time       `json:"publishedAt"`
}

type DiffEntry struct {
	Key        string `json:"key"`
	ChangeType string `json:"changeType"` // added | modified | removed | unchanged
	OldType    string `json:"oldType,omitempty"`
	NewType    string `json:"newType,omitempty"`
	OldValue   string `json:"oldValue,omitempty"`
	NewValue   string `json:"newValue,omitempty"`
}

type Service struct {
	writeDB ports.QueryExec
	readDB  ports.QueryExec
	secrets ports.SecretStore
}

func NewService(writeDB, readDB ports.QueryExec, sec ports.SecretStore) *Service {
	if readDB == nil {
		readDB = writeDB
	}
	return &Service{writeDB: writeDB, readDB: readDB, secrets: sec}
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
	return sdk.NewError(sdk.CodeAPIInvalidRequest, "CFG", http.StatusBadRequest, sdk.CodeAPIInvalidRequest, false, errors.New(msg))
}

func notFoundErr(res, id string) error {
	return sdk.NewError(sdk.CodeDBNotFound, "CFG", http.StatusNotFound, sdk.CodeDBNotFound, false, fmt.Errorf("%s %q not found", res, id))
}

func validEnv(env string) bool {
	switch env {
	case "dev", "sit", "uat", "beta", "prod":
		return true
	default:
		return false
	}
}

// EnsureDefaults seeds starter multi-environment global namespaces if empty.
func (s *Service) EnsureDefaults(ctx context.Context, projectID string) error {
	if projectID == "" {
		return nil
	}
	var cnt int
	if err := s.readDB.ScanRaw(ctx, `SELECT COUNT(1) FROM cfg_namespaces WHERE project_id = ?`, []any{projectID}, &cnt); err == nil && cnt > 0 {
		return nil
	}
	nsProd, err := s.CreateNamespace(ctx, projectID, &Namespace{
		Env:         "prod",
		Name:        "global-platform",
		FormatKind:  "kv",
		Description: "生产环境全局基础设施与部署共享参数（支持 Spec > Service > Global 三级合并）",
	})
	if err == nil {
		_, _ = s.UpsertItem(ctx, nsProd.ID, "system", &ConfigItem{
			ItemKey: "NTP_SERVERS", ItemType: "string", Value: "ntp1.internal.corp,ntp2.internal.corp", Description: "内网统一时钟同步源",
		})
		_, _ = s.UpsertItem(ctx, nsProd.ID, "system", &ConfigItem{
			ItemKey: "IMAGE_REGISTRY", ItemType: "string", Value: "harbor.internal.corp/library", Description: "离线制品镜像库地址",
		})
		_, _ = s.UpsertItem(ctx, nsProd.ID, "system", &ConfigItem{
			ItemKey: "DB_ADMIN_PASSWORD", ItemType: "secret", Value: "ProdInit#2026!Safe", Description: "数据库初始化高权限口令（KMS 信封密文落库）",
		})
		_, _ = s.PublishRelease(ctx, nsProd.ID, "system", "初始化生产基线配置快照", "INIT-001")
	}
	_, _ = s.CreateNamespace(ctx, projectID, &Namespace{
		Env:         "uat",
		Name:        "global-platform",
		FormatKind:  "kv",
		Description: "UAT 预发环境全局配置命名空间",
	})
	return nil
}

func (s *Service) ListNamespaces(ctx context.Context, projectID, env string) ([]Namespace, error) {
	if err := s.EnsureDefaults(ctx, projectID); err != nil {
		return nil, err
	}
	counts := make(map[string]int)
	cRows, err := s.readDB.QueryRaw(ctx,
		`SELECT i.namespace_id, COUNT(1) FROM cfg_items i JOIN cfg_namespaces n ON n.id = i.namespace_id WHERE n.project_id = ? GROUP BY i.namespace_id`, projectID)
	if err == nil {
		defer func() { _ = cRows.Close() }()
		for cRows.Next() {
			var nid string
			var cnt int
			if cRows.Scan(&nid, &cnt) == nil {
				counts[nid] = cnt
			}
		}
	}

	q := `SELECT id, project_id, env, name, format_kind, description, current_release_ver, created_at, updated_at
	      FROM cfg_namespaces WHERE project_id = ?`
	args := []any{projectID}
	if env != "" && env != "all" {
		q += ` AND env = ?`
		args = append(args, env)
	}
	q += ` ORDER BY env ASC, name ASC`
	rows, err := s.readDB.QueryRaw(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]Namespace, 0)
	for rows.Next() {
		var ns Namespace
		var cAt, uAt string
		if err := rows.Scan(&ns.ID, &ns.ProjectID, &ns.Env, &ns.Name, &ns.FormatKind, &ns.Description, &ns.CurrentReleaseVer, &cAt, &uAt); err != nil {
			return nil, err
		}
		ns.ItemCount = counts[ns.ID]
		ns.CreatedAt, ns.UpdatedAt = parseTS(cAt), parseTS(uAt)
		out = append(out, ns)
	}
	return out, rows.Err()
}

func (s *Service) GetNamespace(ctx context.Context, nsID string) (*Namespace, error) {
	var ns Namespace
	var cAt, uAt string
	err := s.readDB.ScanRaw(ctx, `SELECT id, project_id, env, name, format_kind, description, current_release_ver, created_at, updated_at
		 FROM cfg_namespaces WHERE id = ?`, []any{nsID}, &ns.ID, &ns.ProjectID, &ns.Env, &ns.Name, &ns.FormatKind, &ns.Description, &ns.CurrentReleaseVer, &cAt, &uAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, notFoundErr("cfg_namespace", nsID)
	}
	if err != nil {
		return nil, err
	}
	ns.CreatedAt, ns.UpdatedAt = parseTS(cAt), parseTS(uAt)
	return &ns, nil
}

func (s *Service) CreateNamespace(ctx context.Context, projectID string, ns *Namespace) (*Namespace, error) {
	ns.Env = strings.ToLower(strings.TrimSpace(ns.Env))
	ns.Name = strings.TrimSpace(ns.Name)
	if !validEnv(ns.Env) {
		return nil, invalidErr("环境必须为 dev / sit / uat / beta / prod 之一")
	}
	if ns.Name == "" {
		return nil, invalidErr("命名空间名称不能为空")
	}
	if ns.FormatKind == "" {
		ns.FormatKind = "kv"
	}
	now := time.Now().UTC()
	ns.ID = domain.NewID("cfgns")
	ns.ProjectID = projectID
	ns.CurrentReleaseVer = 0
	ns.CreatedAt, ns.UpdatedAt = now, now
	_, err := s.writeDB.ExecRaw(ctx,
		`INSERT INTO cfg_namespaces (id, project_id, env, name, format_kind, description, current_release_ver, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, 0, ?, ?)`,
		ns.ID, projectID, ns.Env, ns.Name, ns.FormatKind, ns.Description, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano),
	)
	if err != nil {
		return nil, sdk.NewError(sdk.CodeLockConflict, "CFG", http.StatusConflict, sdk.CodeLockConflict, false, err)
	}
	return ns, nil
}

func (s *Service) DeleteNamespace(ctx context.Context, projectID, nsID string) error {
	_, err := s.writeDB.ExecRaw(ctx, `DELETE FROM cfg_namespaces WHERE project_id = ? AND id = ?`, projectID, nsID)
	return err
}

func (s *Service) ListItems(ctx context.Context, nsID string) ([]ConfigItem, error) {
	rows, err := s.readDB.QueryRaw(ctx,
		`SELECT id, namespace_id, item_key, item_type, value_plain, value_cipher, description, version, updated_by, updated_at
		 FROM cfg_items WHERE namespace_id = ? ORDER BY item_key ASC`, nsID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]ConfigItem, 0)
	for rows.Next() {
		var it ConfigItem
		var plain, cipher, uAt string
		if err := rows.Scan(&it.ID, &it.NamespaceID, &it.ItemKey, &it.ItemType, &plain, &cipher, &it.Description, &it.Version, &it.UpdatedBy, &uAt); err != nil {
			return nil, err
		}
		if it.ItemType == "secret" {
			it.HasSecret = cipher != ""
			if it.HasSecret {
				it.Value = MaskedSecretPlaceholder
			} else {
				it.Value = ""
			}
		} else {
			it.Value = plain
		}
		it.UpdatedAt = parseTS(uAt)
		out = append(out, it)
	}
	return out, rows.Err()
}

// UpsertItem writes a draft config item; secrets are strictly encrypted via KMS EnvelopeStore with value_plain cleared.
func (s *Service) UpsertItem(ctx context.Context, nsID, actor string, item *ConfigItem) (*ConfigItem, error) {
	if _, err := s.GetNamespace(ctx, nsID); err != nil {
		return nil, err
	}
	item.ItemKey = strings.TrimSpace(item.ItemKey)
	if item.ItemKey == "" {
		return nil, invalidErr("配置项键名 itemKey 不能为空")
	}
	if item.ItemType == "" {
		item.ItemType = "string"
	}
	switch item.ItemType {
	case "yaml":
		var tmp any
		if err := yaml.Unmarshal([]byte(item.Value), &tmp); err != nil {
			return nil, invalidErr("YAML 语法校验失败: " + err.Error())
		}
	case "json":
		var tmp any
		if err := json.Unmarshal([]byte(item.Value), &tmp); err != nil {
			return nil, invalidErr("JSON 语法校验失败: " + err.Error())
		}
	}

	var existingID, existingCipher string
	var existingVer int
	err := s.readDB.ScanRaw(ctx, `SELECT id, value_cipher, version FROM cfg_items WHERE namespace_id = ? AND item_key = ?`, []any{nsID, item.ItemKey}, &existingID, &existingCipher, &existingVer)
	isUpdate := err == nil && existingID != ""

	recID := existingID
	ver := existingVer + 1
	if !isUpdate {
		recID = domain.NewID("cfgit")
		ver = 1
	}

	valuePlain := item.Value
	valueCipher := ""
	if item.ItemType == "secret" {
		valuePlain = ""
		if item.Value == MaskedSecretPlaceholder && isUpdate {
			// Keep existing ciphertext unchanged when operator only updated description
			valueCipher = existingCipher
		} else if item.Value != "" {
			if s.secrets == nil {
				return nil, invalidErr("secret store unavailable")
			}
			enc, encErr := s.secrets.Encrypt(ctx, "cfg_items", recID, 1, []byte(item.Value))
			if encErr != nil {
				return nil, encErr
			}
			valueCipher = string(enc)
		}
	}

	now := time.Now().UTC()
	if isUpdate {
		_, err = s.writeDB.ExecRaw(ctx,
			`UPDATE cfg_items SET item_type = ?, value_plain = ?, value_cipher = ?, description = ?, version = ?, updated_by = ?, updated_at = ?
			 WHERE id = ?`,
			item.ItemType, valuePlain, valueCipher, item.Description, ver, actor, now.Format(time.RFC3339Nano), recID,
		)
	} else {
		_, err = s.writeDB.ExecRaw(ctx,
			`INSERT INTO cfg_items (id, namespace_id, item_key, item_type, value_plain, value_cipher, description, version, updated_by, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, 1, ?, ?)`,
			recID, nsID, item.ItemKey, item.ItemType, valuePlain, valueCipher, item.Description, actor, now.Format(time.RFC3339Nano),
		)
	}
	if err != nil {
		return nil, err
	}

	out := &ConfigItem{
		ID:          recID,
		NamespaceID: nsID,
		ItemKey:     item.ItemKey,
		ItemType:    item.ItemType,
		Value:       valuePlain,
		HasSecret:   item.ItemType == "secret" && valueCipher != "",
		Description: item.Description,
		Version:     ver,
		UpdatedBy:   actor,
		UpdatedAt:   now,
	}
	if out.ItemType == "secret" && out.HasSecret {
		out.Value = MaskedSecretPlaceholder
	}
	return out, nil
}

func (s *Service) DeleteItem(ctx context.Context, nsID, itemKey string) error {
	_, err := s.writeDB.ExecRaw(ctx, `DELETE FROM cfg_items WHERE namespace_id = ? AND item_key = ?`, nsID, itemKey)
	return err
}

// RevealSecret decrypts a single secret key strictly on demand; callers MUST record an audit trail (LiveOps 03.4 §4.2).
func (s *Service) RevealSecret(ctx context.Context, nsID, itemKey string) (string, error) {
	var id, itype, plain, cipher string
	err := s.readDB.ScanRaw(ctx, `SELECT id, item_type, value_plain, value_cipher FROM cfg_items WHERE namespace_id = ? AND item_key = ?`, []any{nsID, itemKey}, &id, &itype, &plain, &cipher)
	if errors.Is(err, sql.ErrNoRows) {
		return "", notFoundErr("cfg_item", itemKey)
	}
	if err != nil {
		return "", err
	}
	if itype != "secret" {
		return plain, nil
	}
	if cipher == "" {
		return "", nil
	}
	if s.secrets == nil {
		return "", invalidErr("secret store unavailable")
	}
	b, err := s.secrets.Decrypt(ctx, "cfg_items", id, 1, []byte(cipher))
	if err != nil {
		return "", err
	}
	defer clear(b)
	return string(b), nil
}

func (s *Service) rawDraftSnapshot(ctx context.Context, nsID string) ([]SnapshotEntry, error) {
	rows, err := s.readDB.QueryRaw(ctx,
		`SELECT item_key, item_type, value_plain, value_cipher, description FROM cfg_items WHERE namespace_id = ? ORDER BY item_key ASC`, nsID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	entries := make([]SnapshotEntry, 0)
	for rows.Next() {
		var e SnapshotEntry
		if err := rows.Scan(&e.Key, &e.Type, &e.ValuePlain, &e.ValueCipher, &e.Description); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

func computeCanonicalETag(entries []SnapshotEntry) string {
	sorted := make([]SnapshotEntry, len(entries))
	copy(sorted, entries)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Key < sorted[j].Key })
	b, _ := json.Marshal(sorted)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// PublishRelease freezes current draft items into an immutable monotonic release snapshot with canonical SHA-256 ETag.
func (s *Service) PublishRelease(ctx context.Context, nsID, actor, comment, orcTicketNo string) (*Release, error) {
	ns, err := s.GetNamespace(ctx, nsID)
	if err != nil {
		return nil, err
	}
	entries, err := s.rawDraftSnapshot(ctx, nsID)
	if err != nil {
		return nil, err
	}
	nextVer := ns.CurrentReleaseVer + 1
	etag := computeCanonicalETag(entries)
	snapB, _ := json.Marshal(entries)
	now := time.Now().UTC()

	rel := &Release{
		ID:          domain.NewID("cfgrel"),
		NamespaceID: nsID,
		ReleaseVer:  nextVer,
		ETag:        etag,
		Items:       maskSnapshotEntries(entries),
		Comment:     strings.TrimSpace(comment),
		ORCTicketNo: strings.TrimSpace(orcTicketNo),
		PublishedBy: actor,
		PublishedAt: now,
	}
	_, err = s.writeDB.ExecRaw(ctx,
		`INSERT INTO cfg_releases (id, namespace_id, release_ver, etag, snapshot_json, comment, orc_ticket_no, published_by, published_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		rel.ID, nsID, rel.ReleaseVer, rel.ETag, string(snapB), rel.Comment, rel.ORCTicketNo, actor, now.Format(time.RFC3339Nano),
	)
	if err != nil {
		return nil, err
	}
	_, err = s.writeDB.ExecRaw(ctx,
		`UPDATE cfg_namespaces SET current_release_ver = ?, updated_at = ? WHERE id = ?`,
		nextVer, now.Format(time.RFC3339Nano), nsID,
	)
	if err != nil {
		return nil, err
	}
	return rel, nil
}

func maskSnapshotEntries(in []SnapshotEntry) []SnapshotEntry {
	out := make([]SnapshotEntry, len(in))
	for i, e := range in {
		out[i] = SnapshotEntry{
			Key:         e.Key,
			Type:        e.Type,
			Description: e.Description,
		}
		if e.Type == "secret" {
			if e.ValueCipher != "" {
				out[i].ValuePlain = MaskedSecretPlaceholder
			}
		} else {
			out[i].ValuePlain = e.ValuePlain
		}
	}
	return out
}

func (s *Service) ListReleases(ctx context.Context, nsID string) ([]Release, error) {
	rows, err := s.readDB.QueryRaw(ctx,
		`SELECT id, namespace_id, release_ver, etag, snapshot_json, comment, orc_ticket_no, published_by, published_at
		 FROM cfg_releases WHERE namespace_id = ? ORDER BY release_ver DESC LIMIT 50`, nsID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]Release, 0)
	for rows.Next() {
		var r Release
		var snapStr, pAt string
		if err := rows.Scan(&r.ID, &r.NamespaceID, &r.ReleaseVer, &r.ETag, &snapStr, &r.Comment, &r.ORCTicketNo, &r.PublishedBy, &pAt); err != nil {
			return nil, err
		}
		var raw []SnapshotEntry
		_ = json.Unmarshal([]byte(snapStr), &raw)
		r.Items = maskSnapshotEntries(raw)
		r.PublishedAt = parseTS(pAt)
		out = append(out, r)
	}
	return out, rows.Err()
}

// DiffDraftVsLatest compares current draft items against the latest published release (secret values masked).
func (s *Service) DiffDraftVsLatest(ctx context.Context, nsID string) ([]DiffEntry, error) {
	draft, err := s.rawDraftSnapshot(ctx, nsID)
	if err != nil {
		return nil, err
	}
	var latestSnapStr string
	_ = s.readDB.ScanRaw(ctx, `SELECT snapshot_json FROM cfg_releases WHERE namespace_id = ? ORDER BY release_ver DESC LIMIT 1`, []any{nsID}, &latestSnapStr)
	var published []SnapshotEntry
	if latestSnapStr != "" {
		_ = json.Unmarshal([]byte(latestSnapStr), &published)
	}

	pubMap := make(map[string]SnapshotEntry, len(published))
	for _, p := range published {
		pubMap[p.Key] = p
	}
	draftMap := make(map[string]SnapshotEntry, len(draft))
	for _, d := range draft {
		draftMap[d.Key] = d
	}

	keySet := make(map[string]bool)
	for k := range pubMap {
		keySet[k] = true
	}
	for k := range draftMap {
		keySet[k] = true
	}
	keys := make([]string, 0, len(keySet))
	for k := range keySet {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	diffs := make([]DiffEntry, 0, len(keys))
	displayVal := func(e SnapshotEntry) string {
		if e.Type == "secret" {
			if e.ValueCipher == "" {
				return "(空密钥)"
			}
			h := sha256.Sum256([]byte(e.ValueCipher))
			return fmt.Sprintf("%s [cipher:%x]", MaskedSecretPlaceholder, h[:4])
		}
		return e.ValuePlain
	}

	for _, k := range keys {
		p, inPub := pubMap[k]
		d, inDraft := draftMap[k]
		switch {
		case !inPub && inDraft:
			diffs = append(diffs, DiffEntry{Key: k, ChangeType: "added", NewType: d.Type, NewValue: displayVal(d)})
		case inPub && !inDraft:
			diffs = append(diffs, DiffEntry{Key: k, ChangeType: "removed", OldType: p.Type, OldValue: displayVal(p)})
		default:
			changed := p.Type != d.Type || p.ValuePlain != d.ValuePlain || p.ValueCipher != d.ValueCipher
			ct := "unchanged"
			if changed {
				ct = "modified"
			}
			diffs = append(diffs, DiffEntry{
				Key: k, ChangeType: ct, OldType: p.Type, NewType: d.Type, OldValue: displayVal(p), NewValue: displayVal(d),
			})
		}
	}
	return diffs, nil
}

// RollbackRelease restores draft items from a historical release and publishes a NEW monotonic version (immutable audit history).
func (s *Service) RollbackRelease(ctx context.Context, nsID, actor string, targetVer int, comment string) (*Release, error) {
	var snapStr string
	err := s.readDB.ScanRaw(ctx, `SELECT snapshot_json FROM cfg_releases WHERE namespace_id = ? AND release_ver = ?`, []any{nsID, targetVer}, &snapStr)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, notFoundErr("cfg_release", fmt.Sprintf("v%d", targetVer))
	}
	if err != nil {
		return nil, err
	}
	var entries []SnapshotEntry
	_ = json.Unmarshal([]byte(snapStr), &entries)

	// Replace draft items atomically with target snapshot (preserving encrypted ciphertexts)
	if _, err := s.writeDB.ExecRaw(ctx, `DELETE FROM cfg_items WHERE namespace_id = ?`, nsID); err != nil {
		return nil, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, e := range entries {
		_, err := s.writeDB.ExecRaw(ctx,
			`INSERT INTO cfg_items (id, namespace_id, item_key, item_type, value_plain, value_cipher, description, version, updated_by, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, 1, ?, ?)`,
			domain.NewID("cfgit"), nsID, e.Key, e.Type, e.ValuePlain, e.ValueCipher, e.Description, actor, now,
		)
		if err != nil {
			return nil, err
		}
	}
	if comment == "" {
		comment = fmt.Sprintf("rollback to v%d", targetVer)
	}
	return s.PublishRelease(ctx, nsID, actor, comment, "")
}

// CloneNamespace copies a namespace into another environment; secret items are intentionally blanked so dev/uat secrets NEVER leak into prod.
func (s *Service) CloneNamespace(ctx context.Context, projectID, sourceNsID, targetEnv, targetName, actor string) (*Namespace, error) {
	src, err := s.GetNamespace(ctx, sourceNsID)
	if err != nil {
		return nil, err
	}
	if targetName == "" {
		targetName = src.Name
	}
	dst, err := s.CreateNamespace(ctx, projectID, &Namespace{
		Env:         targetEnv,
		Name:        targetName,
		FormatKind:  src.FormatKind,
		Description: fmt.Sprintf("克隆自 %s/%s（密钥项已安全置空待目标环境注入）", src.Env, src.Name),
	})
	if err != nil {
		return nil, err
	}
	entries, err := s.rawDraftSnapshot(ctx, sourceNsID)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		val := e.ValuePlain
		if e.Type == "secret" {
			val = "" // Strict cross-env secret isolation (LiveOps 03.4 §4.2 #4)
		}
		_, _ = s.UpsertItem(ctx, dst.ID, actor, &ConfigItem{
			ItemKey:     e.Key,
			ItemType:    e.Type,
			Value:       val,
			Description: e.Description,
		})
	}
	return dst, nil
}

// ExportGlobalParamsMap returns latest published non-secret key-values for a namespace (used by Delivery 3-level merge).
func (s *Service) ExportGlobalParamsMap(ctx context.Context, nsID string) (map[string]any, error) {
	out := make(map[string]any)
	if nsID == "" {
		return out, nil
	}
	items, err := s.ListItems(ctx, nsID)
	if err != nil {
		return nil, err
	}
	for _, it := range items {
		out[it.ItemKey] = it.Value
	}
	return out, nil
}
