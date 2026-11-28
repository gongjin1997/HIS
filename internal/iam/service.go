package iam

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/kinstall/kinstall/pkg/platform/audit"
	"github.com/kinstall/kinstall/pkg/platform/auth"
	"github.com/kinstall/kinstall/pkg/platform/crypto"
	"github.com/kinstall/kinstall/pkg/platform/db"
	"github.com/kinstall/kinstall/pkg/platform/id"
)

const (
	DefaultAdminUser     = "admin"
	DefaultAdminPassword = "Kinstall#2026!"
	DefaultWorkspaceCode = "core-ops"
)

type Workspace struct {
	ID          string `json:"id"`
	Code        string `json:"code"`
	Name        string `json:"name"`
	Description string `json:"description"`
	CreatedAt   string `json:"createdAt"`
	UpdatedAt   string `json:"updatedAt"`
}

type User struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
	Email       string `json:"email"`
	Role        string `json:"role"`
	Enabled     bool   `json:"enabled"`
	CreatedAt   string `json:"createdAt"`
	UpdatedAt   string `json:"updatedAt"`
}

type APIToken struct {
	ID          string `json:"id"`
	UserID      string `json:"userId"`
	WorkspaceID string `json:"workspaceId"`
	Name        string `json:"name"`
	TokenPrefix string `json:"tokenPrefix"`
	Role        string `json:"role"`
	ExpiresAt   string `json:"expiresAt"`
	LastUsedAt  string `json:"lastUsedAt"`
	Revoked     bool   `json:"revoked"`
	CreatedAt   string `json:"createdAt"`
	PlainToken  string `json:"plainToken,omitempty"` // returned ONCE on creation
}

type Credential struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspaceId"`
	Name        string `json:"name"`
	Kind        string `json:"kind"` // ssh_password | ssh_key | registry_basic | api_secret
	Username    string `json:"username"`
	Description string `json:"description"`
	CreatedBy   string `json:"createdBy"`
	CreatedAt   string `json:"createdAt"`
	UpdatedAt   string `json:"updatedAt"`
}

// DecryptedCredential is NEVER serialized to HTTP responses.
type DecryptedCredential struct {
	Credential
	Secret     string
	Passphrase string
}

type LoginResult struct {
	Token      string      `json:"token"`
	ExpiresAt  string      `json:"expiresAt"`
	User       User        `json:"user"`
	Workspaces []Workspace `json:"workspaces"`
}

type AuditVerifyResult struct {
	Valid      bool   `json:"valid"`
	TotalCount int    `json:"totalCount"`
	BrokenAt   int    `json:"brokenAt"`
	Reason     string `json:"reason"`
	VerifiedAt string `json:"verifiedAt"`
}

type Service struct {
	store   *db.Store
	codec   *crypto.Codec
	authMgr *auth.Manager
	auditMu sync.Mutex
}

func NewService(store *db.Store, codec *crypto.Codec, authMgr *auth.Manager) *Service {
	return &Service{
		store:   store,
		codec:   codec,
		authMgr: authMgr,
	}
}

func nowISO() string {
	return time.Now().UTC().Format(time.RFC3339)
}

// Bootstrap idempotently seeds the default workspace and admin account.
func (s *Service) Bootstrap(ctx context.Context) (string, error) {
	now := nowISO()
	var wsID string
	err := s.store.QueryRow(ctx, `SELECT id FROM workspaces WHERE code = ?`, DefaultWorkspaceCode).Scan(&wsID)
	if errors.Is(err, sql.ErrNoRows) {
		wsID = id.New("ws")
		if _, err := s.store.Exec(ctx,
			`INSERT INTO workspaces(id, code, name, description, created_at, updated_at) VALUES(?,?,?,?,?,?)`,
			wsID, DefaultWorkspaceCode, "核心运维空间", "统一 CMDB 资产、多环境配置与自动化发布默认工作空间", now, now,
		); err != nil {
			return "", fmt.Errorf("seed workspace: %w", err)
		}
	} else if err != nil {
		return "", fmt.Errorf("lookup default workspace: %w", err)
	}

	var adminID string
	err = s.store.QueryRow(ctx, `SELECT id FROM users WHERE username = ?`, DefaultAdminUser).Scan(&adminID)
	if errors.Is(err, sql.ErrNoRows) {
		hash, herr := crypto.HashPassword(DefaultAdminPassword)
		if herr != nil {
			return "", herr
		}
		adminID = id.New("usr")
		if _, err := s.store.Exec(ctx,
			`INSERT INTO users(id, username, display_name, email, role, password_hash, enabled, created_at, updated_at) VALUES(?,?,?,?,?,?,1,?,?)`,
			adminID, DefaultAdminUser, "系统管理员", "admin@kinstall.local", auth.RoleAdmin, hash, now, now,
		); err != nil {
			return "", fmt.Errorf("seed admin user: %w", err)
		}
	} else if err != nil {
		return "", fmt.Errorf("lookup admin user: %w", err)
	}

	// Seed an initial audit genesis entry if empty.
	var auditCount int
	_ = s.store.QueryRow(ctx, `SELECT COUNT(1) FROM audit_logs WHERE workspace_id = ?`, wsID).Scan(&auditCount)
	if auditCount == 0 {
		_ = s.RecordAudit(ctx, wsID, "system", auth.RoleAdmin, "platform.bootstrap", "workspace", wsID, "ok", "Kinstall v2 底座初始化完成，启用防篡改审计哈希链", "127.0.0.1")
	}
	return wsID, nil
}

func (s *Service) Login(ctx context.Context, username, password, sourceIP string) (*LoginResult, error) {
	u := strings.TrimSpace(username)
	if u == "" || password == "" {
		return nil, errors.New("用户名和密码不能为空")
	}
	var (
		user    User
		pwHash  string
		enabled int
	)
	err := s.store.QueryRow(ctx,
		`SELECT id, username, display_name, email, role, password_hash, enabled, created_at, updated_at FROM users WHERE username = ?`,
		u,
	).Scan(&user.ID, &user.Username, &user.DisplayName, &user.Email, &user.Role, &pwHash, &enabled, &user.CreatedAt, &user.UpdatedAt)
	if err != nil {
		return nil, errors.New("用户名或密码错误")
	}
	user.Enabled = enabled == 1
	if !user.Enabled {
		return nil, errors.New("该账号已被禁用")
	}
	if !crypto.VerifyPassword(password, pwHash) {
		return nil, errors.New("用户名或密码错误")
	}
	workspaces, err := s.ListWorkspaces(ctx)
	if err != nil {
		return nil, err
	}
	defaultWS := ""
	if len(workspaces) > 0 {
		defaultWS = workspaces[0].ID
	}
	tok, exp, err := s.authMgr.Issue(auth.Principal{
		UserID:      user.ID,
		Username:    user.Username,
		DisplayName: user.DisplayName,
		Role:        user.Role,
		WorkspaceID: defaultWS,
		TokenType:   "session",
	})
	if err != nil {
		return nil, err
	}
	if defaultWS != "" {
		_ = s.RecordAudit(ctx, defaultWS, user.Username, user.Role, "auth.login", "user", user.ID, "ok", "管理员登录控制台成功", sourceIP)
	}
	return &LoginResult{
		Token:      tok,
		ExpiresAt:  exp.Format(time.RFC3339),
		User:       user,
		Workspaces: workspaces,
	}, nil
}

func (s *Service) ListWorkspaces(ctx context.Context) ([]Workspace, error) {
	rows, err := s.store.Query(ctx, `SELECT id, code, name, description, created_at, updated_at FROM workspaces ORDER BY created_at ASC`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]Workspace, 0)
	for rows.Next() {
		var w Workspace
		if err := rows.Scan(&w.ID, &w.Code, &w.Name, &w.Description, &w.CreatedAt, &w.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

func (s *Service) CreateWorkspace(ctx context.Context, code, name, description string) (*Workspace, error) {
	code = strings.ToLower(strings.TrimSpace(code))
	name = strings.TrimSpace(name)
	if code == "" || name == "" {
		return nil, errors.New("空间标识和名称不能为空")
	}
	now := nowISO()
	ws := &Workspace{
		ID:          id.New("ws"),
		Code:        code,
		Name:        name,
		Description: strings.TrimSpace(description),
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if _, err := s.store.Exec(ctx,
		`INSERT INTO workspaces(id, code, name, description, created_at, updated_at) VALUES(?,?,?,?,?,?)`,
		ws.ID, ws.Code, ws.Name, ws.Description, ws.CreatedAt, ws.UpdatedAt,
	); err != nil {
		return nil, fmt.Errorf("创建工作空间失败: %w", err)
	}
	return ws, nil
}

func (s *Service) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := s.store.Query(ctx, `SELECT id, username, display_name, email, role, enabled, created_at, updated_at FROM users ORDER BY created_at ASC`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]User, 0)
	for rows.Next() {
		var (
			u       User
			enabled int
		)
		if err := rows.Scan(&u.ID, &u.Username, &u.DisplayName, &u.Email, &u.Role, &enabled, &u.CreatedAt, &u.UpdatedAt); err != nil {
			return nil, err
		}
		u.Enabled = enabled == 1
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *Service) CreateUser(ctx context.Context, username, displayName, email, role, password string) (*User, error) {
	username = strings.TrimSpace(username)
	if username == "" || len(password) < 8 {
		return nil, errors.New("用户名不能为空且密码长度不少于 8 位")
	}
	role = normalizeRole(role)
	if displayName == "" {
		displayName = username
	}
	hash, err := crypto.HashPassword(password)
	if err != nil {
		return nil, err
	}
	now := nowISO()
	u := &User{
		ID:          id.New("usr"),
		Username:    username,
		DisplayName: strings.TrimSpace(displayName),
		Email:       strings.TrimSpace(email),
		Role:        role,
		Enabled:     true,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if _, err := s.store.Exec(ctx,
		`INSERT INTO users(id, username, display_name, email, role, password_hash, enabled, created_at, updated_at) VALUES(?,?,?,?,?,?,1,?,?)`,
		u.ID, u.Username, u.DisplayName, u.Email, u.Role, hash, now, now,
	); err != nil {
		return nil, fmt.Errorf("创建用户失败: %w", err)
	}
	return u, nil
}

func (s *Service) UpdateUserRole(ctx context.Context, userID, role string, enabled bool) error {
	en := 0
	if enabled {
		en = 1
	}
	_, err := s.store.Exec(ctx,
		`UPDATE users SET role = ?, enabled = ?, updated_at = ? WHERE id = ?`,
		normalizeRole(role), en, nowISO(), userID,
	)
	return err
}

func normalizeRole(r string) string {
	switch strings.ToLower(strings.TrimSpace(r)) {
	case auth.RoleAdmin, auth.RoleOperator, auth.RoleAuditor, auth.RoleViewer:
		return strings.ToLower(strings.TrimSpace(r))
	default:
		return auth.RoleViewer
	}
}

func (s *Service) CreateAPIToken(ctx context.Context, userID, wsID, name, role string, ttlDays int) (*APIToken, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("令牌名称不能为空")
	}
	randPart, err := crypto.RandomToken(24)
	if err != nil {
		return nil, err
	}
	plain := "ki_pat_" + randPart
	prefix := plain[:14]
	hash := crypto.SHA256Hex([]byte(plain))
	now := time.Now().UTC()
	exp := ""
	if ttlDays > 0 {
		exp = now.Add(time.Duration(ttlDays) * 24 * time.Hour).Format(time.RFC3339)
	}
	tok := &APIToken{
		ID:          id.New("pat"),
		UserID:      userID,
		WorkspaceID: wsID,
		Name:        name,
		TokenPrefix: prefix,
		Role:        normalizeRole(role),
		ExpiresAt:   exp,
		CreatedAt:   now.Format(time.RFC3339),
		PlainToken:  plain,
	}
	if _, err := s.store.Exec(ctx,
		`INSERT INTO api_tokens(id, user_id, workspace_id, name, token_prefix, token_hash, role, expires_at, last_used_at, revoked, created_at) VALUES(?,?,?,?,?,?,?,?,'',0,?)`,
		tok.ID, tok.UserID, tok.WorkspaceID, tok.Name, tok.TokenPrefix, hash, tok.Role, tok.ExpiresAt, tok.CreatedAt,
	); err != nil {
		return nil, err
	}
	return tok, nil
}

func (s *Service) ListAPITokens(ctx context.Context, wsID string) ([]APIToken, error) {
	rows, err := s.store.Query(ctx,
		`SELECT id, user_id, workspace_id, name, token_prefix, role, expires_at, last_used_at, revoked, created_at FROM api_tokens WHERE workspace_id = ? ORDER BY created_at DESC`,
		wsID,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]APIToken, 0)
	for rows.Next() {
		var (
			t       APIToken
			revoked int
		)
		if err := rows.Scan(&t.ID, &t.UserID, &t.WorkspaceID, &t.Name, &t.TokenPrefix, &t.Role, &t.ExpiresAt, &t.LastUsedAt, &revoked, &t.CreatedAt); err != nil {
			return nil, err
		}
		t.Revoked = revoked == 1
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Service) RevokeAPIToken(ctx context.Context, wsID, tokenID string) error {
	_, err := s.store.Exec(ctx, `UPDATE api_tokens SET revoked = 1 WHERE id = ? AND workspace_id = ?`, tokenID, wsID)
	return err
}

func (s *Service) LookupPAT(ctx context.Context, rawToken string) (*auth.Principal, error) {
	hash := crypto.SHA256Hex([]byte(rawToken))
	var (
		tokID, userID, wsID, role, expiresAt, username, displayName string
		revoked, enabled                                            int
	)
	err := s.store.QueryRow(ctx,
		`SELECT t.id, t.user_id, t.workspace_id, t.role, t.expires_at, t.revoked, u.username, u.display_name, u.enabled
		 FROM api_tokens t JOIN users u ON u.id = t.user_id WHERE t.token_hash = ?`,
		hash,
	).Scan(&tokID, &userID, &wsID, &role, &expiresAt, &revoked, &username, &displayName, &enabled)
	if err != nil {
		return nil, errors.New("invalid api token")
	}
	if revoked == 1 || enabled == 0 {
		return nil, errors.New("api token revoked or user disabled")
	}
	if expiresAt != "" {
		if exp, perr := time.Parse(time.RFC3339, expiresAt); perr == nil && time.Now().UTC().After(exp) {
			return nil, errors.New("api token expired")
		}
	}
	_, _ = s.store.Exec(ctx, `UPDATE api_tokens SET last_used_at = ? WHERE id = ?`, nowISO(), tokID)
	return &auth.Principal{
		UserID:      userID,
		Username:    username,
		DisplayName: displayName,
		Role:        role,
		WorkspaceID: wsID,
		TokenType:   "pat",
	}, nil
}

func (s *Service) ListCredentials(ctx context.Context, wsID string) ([]Credential, error) {
	rows, err := s.store.Query(ctx,
		`SELECT id, workspace_id, name, kind, username, description, created_by, created_at, updated_at FROM credentials WHERE workspace_id = ? ORDER BY created_at DESC`,
		wsID,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]Credential, 0)
	for rows.Next() {
		var c Credential
		if err := rows.Scan(&c.ID, &c.WorkspaceID, &c.Name, &c.Kind, &c.Username, &c.Description, &c.CreatedBy, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Service) CreateCredential(ctx context.Context, wsID, actor, name, kind, username, secret, passphrase, description string) (*Credential, error) {
	name = strings.TrimSpace(name)
	if name == "" || secret == "" {
		return nil, errors.New("凭据名称和密钥内容不能为空")
	}
	if kind == "" {
		kind = "ssh_password"
	}
	secEnc, err := s.codec.Encrypt(secret)
	if err != nil {
		return nil, fmt.Errorf("encrypt credential secret: %w", err)
	}
	ppEnc := ""
	if passphrase != "" {
		ppEnc, err = s.codec.Encrypt(passphrase)
		if err != nil {
			return nil, fmt.Errorf("encrypt passphrase: %w", err)
		}
	}
	now := nowISO()
	c := &Credential{
		ID:          id.New("cred"),
		WorkspaceID: wsID,
		Name:        name,
		Kind:        kind,
		Username:    strings.TrimSpace(username),
		Description: strings.TrimSpace(description),
		CreatedBy:   actor,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if _, err := s.store.Exec(ctx,
		`INSERT INTO credentials(id, workspace_id, name, kind, username, secret_enc, passphrase_enc, description, created_by, created_at, updated_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		c.ID, c.WorkspaceID, c.Name, c.Kind, c.Username, secEnc, ppEnc, c.Description, c.CreatedBy, c.CreatedAt, c.UpdatedAt,
	); err != nil {
		return nil, fmt.Errorf("保存凭据失败: %w", err)
	}
	return c, nil
}

func (s *Service) DeleteCredential(ctx context.Context, wsID, credID string) error {
	_, err := s.store.Exec(ctx, `DELETE FROM credentials WHERE id = ? AND workspace_id = ?`, credID, wsID)
	return err
}

func (s *Service) ResolveCredential(ctx context.Context, wsID, credID string) (*DecryptedCredential, error) {
	var (
		c             Credential
		secEnc, ppEnc string
	)
	err := s.store.QueryRow(ctx,
		`SELECT id, workspace_id, name, kind, username, secret_enc, passphrase_enc, description, created_by, created_at, updated_at
		 FROM credentials WHERE id = ? AND workspace_id = ?`,
		credID, wsID,
	).Scan(&c.ID, &c.WorkspaceID, &c.Name, &c.Kind, &c.Username, &secEnc, &ppEnc, &c.Description, &c.CreatedBy, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("credential %s not found: %w", credID, err)
	}
	sec, err := s.codec.Decrypt(secEnc)
	if err != nil {
		return nil, fmt.Errorf("decrypt credential: %w", err)
	}
	pp := ""
	if ppEnc != "" {
		pp, err = s.codec.Decrypt(ppEnc)
		if err != nil {
			return nil, fmt.Errorf("decrypt passphrase: %w", err)
		}
	}
	return &DecryptedCredential{Credential: c, Secret: sec, Passphrase: pp}, nil
}

// RecordAudit appends a tamper-evident entry linked to the previous SHA-256 chain hash.
func (s *Service) RecordAudit(ctx context.Context, wsID, actor, actorRole, action, targetType, targetID, status, detail, sourceIP string) error {
	s.auditMu.Lock()
	defer s.auditMu.Unlock()

	if status == "" {
		status = "ok"
	}
	return s.store.WithTx(ctx, func(ctx context.Context, tx *db.Tx) error {
		prevHash := "GENESIS"
		var lastHash string
		err := tx.QueryRow(ctx,
			`SELECT hash FROM audit_logs WHERE workspace_id = ? ORDER BY created_at DESC, id DESC LIMIT 1`,
			wsID,
		).Scan(&lastHash)
		if err == nil && lastHash != "" {
			prevHash = lastHash
		}
		e := audit.Entry{
			ID:          id.New("aud"),
			WorkspaceID: wsID,
			Actor:       actor,
			ActorRole:   actorRole,
			Action:      action,
			TargetType:  targetType,
			TargetID:    targetID,
			Status:      status,
			Detail:      detail,
			SourceIP:    sourceIP,
			PrevHash:    prevHash,
			CreatedAt:   nowISO(),
		}
		e.Hash = audit.ComputeHash(e)
		_, err = tx.Exec(ctx,
			`INSERT INTO audit_logs(id, workspace_id, actor, actor_role, action, target_type, target_id, status, detail, source_ip, prev_hash, hash, created_at)
			 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			e.ID, e.WorkspaceID, e.Actor, e.ActorRole, e.Action, e.TargetType, e.TargetID, e.Status, e.Detail, e.SourceIP, e.PrevHash, e.Hash, e.CreatedAt,
		)
		return err
	})
}

func (s *Service) ListAuditLogs(ctx context.Context, wsID string, limit int) ([]audit.Entry, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.store.Query(ctx,
		`SELECT id, workspace_id, actor, actor_role, action, target_type, target_id, status, detail, source_ip, prev_hash, hash, created_at
		 FROM audit_logs WHERE workspace_id = ? ORDER BY created_at DESC, id DESC LIMIT ?`,
		wsID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]audit.Entry, 0)
	for rows.Next() {
		var e audit.Entry
		if err := rows.Scan(&e.ID, &e.WorkspaceID, &e.Actor, &e.ActorRole, &e.Action, &e.TargetType, &e.TargetID, &e.Status, &e.Detail, &e.SourceIP, &e.PrevHash, &e.Hash, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Service) VerifyAuditChain(ctx context.Context, wsID string) (*AuditVerifyResult, error) {
	rows, err := s.store.Query(ctx,
		`SELECT id, workspace_id, actor, actor_role, action, target_type, target_id, status, detail, source_ip, prev_hash, hash, created_at
		 FROM audit_logs WHERE workspace_id = ? ORDER BY created_at ASC, id ASC`,
		wsID,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	entries := make([]audit.Entry, 0)
	for rows.Next() {
		var e audit.Entry
		if err := rows.Scan(&e.ID, &e.WorkspaceID, &e.Actor, &e.ActorRole, &e.Action, &e.TargetType, &e.TargetID, &e.Status, &e.Detail, &e.SourceIP, &e.PrevHash, &e.Hash, &e.CreatedAt); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	valid, brokenAt, reason := audit.VerifyChain(entries)
	return &AuditVerifyResult{
		Valid:      valid,
		TotalCount: len(entries),
		BrokenAt:   brokenAt,
		Reason:     reason,
		VerifiedAt: nowISO(),
	}, nil
}
