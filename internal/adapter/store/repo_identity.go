package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/kinstall/kinstall/internal/domain"
	"github.com/kinstall/kinstall/pkg/platform/audit"
	"github.com/kinstall/kinstall/pkg/platform/auth"
	"github.com/kinstall/kinstall/pkg/platform/crypto"
	"github.com/kinstall/kinstall/pkg/platform/db"
	"github.com/kinstall/kinstall/pkg/platform/id"
)

// ============================================================================
// Tenants, Apps, AppKeys & Projects
// ============================================================================

func (r *Repo) ListTenants(ctx context.Context) ([]domain.Tenant, error) {
	rows, err := r.db.Query(ctx, `SELECT tenant_id, code, name, description, status, created_at, updated_at FROM iam_tenants ORDER BY created_at ASC`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []domain.Tenant
	for rows.Next() {
		var t domain.Tenant
		var ca, ua string
		if err := rows.Scan(&t.TenantID, &t.Code, &t.Name, &t.Description, &t.Status, &ca, &ua); err != nil {
			return nil, err
		}
		t.CreatedAt, t.UpdatedAt = parseTime(ca), parseTime(ua)
		out = append(out, t)
	}
	return out, rows.Err()
}

func (r *Repo) CreateTenant(ctx context.Context, code, name, desc string) (domain.Tenant, error) {
	now := r.Now()
	nowStr := formatTime(now)
	tid := id.New("ten")
	t := domain.Tenant{
		TenantID: tid, Code: code, Name: name, Description: desc,
		Status: "ACTIVE", CreatedAt: now, UpdatedAt: now,
	}
	err := r.db.WithTx(ctx, func(ctx context.Context, tx *db.Tx) error {
		if _, err := tx.Exec(ctx,
			`INSERT INTO iam_tenants (tenant_id, code, name, description, status, created_at, updated_at) VALUES (?, ?, ?, ?, 'ACTIVE', ?, ?)`,
			tid, code, name, desc, nowStr, nowStr,
		); err != nil {
			if isUniqueViolation(err) {
				return domain.NewError(domain.ErrAlreadyExists, "TENANT_EXISTS", "租户编码已存在", nil)
			}
			return err
		}
		// Automatically create a default workspace app so the new tenant is immediately operable.
		defaultAppID := "app-" + code
		if _, err := tx.Exec(ctx,
			`INSERT INTO iam_apps (tenant_id, app_id, code, name, description, status, created_at, updated_at) VALUES (?, ?, 'default', '默认核心应用', '企业空间自动初始化应用', 'ACTIVE', ?, ?)`,
			tid, defaultAppID, nowStr, nowStr,
		); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO iam_scope_versions (tenant_id, app_id, scope_version, updated_at) VALUES (?, ?, 1, ?)`, tid, defaultAppID, nowStr); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO gw_revisions (tenant_id, app_id, route_version, updated_at) VALUES (?, ?, 1, ?)`, tid, defaultAppID, nowStr); err != nil {
			return err
		}
		return r.seedCMDB(ctx, tx, tid, defaultAppID, nowStr)
	})
	return t, err
}

func (r *Repo) ListApps(ctx context.Context, tenantID string) ([]domain.App, error) {
	rows, err := r.db.Query(ctx,
		`SELECT tenant_id, app_id, code, name, description, status, created_at, updated_at FROM iam_apps WHERE tenant_id=? ORDER BY created_at ASC`,
		tenantID,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []domain.App
	for rows.Next() {
		var a domain.App
		var ca, ua string
		if err := rows.Scan(&a.TenantID, &a.AppID, &a.Code, &a.Name, &a.Description, &a.Status, &ca, &ua); err != nil {
			return nil, err
		}
		a.CreatedAt, a.UpdatedAt = parseTime(ca), parseTime(ua)
		out = append(out, a)
	}
	return out, rows.Err()
}

func (r *Repo) CreateApp(ctx context.Context, tenantID, code, name, desc string) (domain.App, error) {
	now := r.Now()
	nowStr := formatTime(now)
	aid := id.New("app")
	app := domain.App{
		TenantID: tenantID, AppID: aid, Code: code, Name: name, Description: desc,
		Status: "ACTIVE", CreatedAt: now, UpdatedAt: now,
	}
	err := r.db.WithScopeTx(ctx, db.Scope{TenantID: tenantID}, func(ctx context.Context, tx *db.Tx) error {
		if _, err := tx.Exec(ctx,
			`INSERT INTO iam_apps (tenant_id, app_id, code, name, description, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, 'ACTIVE', ?, ?)`,
			tenantID, aid, code, name, desc, nowStr, nowStr,
		); err != nil {
			if isUniqueViolation(err) {
				return domain.NewError(domain.ErrAlreadyExists, "APP_EXISTS", "应用编码已存在", nil)
			}
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO iam_scope_versions (tenant_id, app_id, scope_version, updated_at) VALUES (?, ?, 1, ?)`, tenantID, aid, nowStr); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO gw_revisions (tenant_id, app_id, route_version, updated_at) VALUES (?, ?, 1, ?)`, tenantID, aid, nowStr); err != nil {
			return err
		}
		return r.seedCMDB(ctx, tx, tenantID, aid, nowStr)
	})
	return app, err
}

func (r *Repo) ListAppKeys(ctx context.Context, scope domain.Scope) ([]domain.AppKey, error) {
	rows, err := r.db.Query(ctx,
		`SELECT tenant_id, app_id, key_id, key_prefix, status, grace_until, last_used_at, created_at
		 FROM iam_app_keys WHERE tenant_id=? AND app_id=? ORDER BY created_at DESC`,
		scope.TenantID, scope.AppID,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []domain.AppKey
	for rows.Next() {
		var k domain.AppKey
		var gu, lu, ca string
		if err := rows.Scan(&k.TenantID, &k.AppID, &k.KeyID, &k.KeyPrefix, &k.Status, &gu, &lu, &ca); err != nil {
			return nil, err
		}
		k.GraceUntil, k.LastUsedAt, k.CreatedAt = parseTimePtr(gu), parseTimePtr(lu), parseTime(ca)
		out = append(out, k)
	}
	return out, rows.Err()
}

// RotateAppKey implements LiveOps Dual-Track 256-bit AppKey Rotation: existing ACTIVE keys enter ROTATING with a grace window.
func (r *Repo) RotateAppKey(ctx context.Context, scope domain.Scope, graceWindow time.Duration) (domain.AppKey, string, error) {
	if graceWindow <= 0 {
		graceWindow = 24 * time.Hour
	}
	rawKey, prefix, keyHash, err := crypto.GenerateAppKey()
	if err != nil {
		return domain.AppKey{}, "", err
	}
	now := r.Now()
	nowStr := formatTime(now)
	graceStr := formatTime(now.Add(graceWindow))
	keyID := id.New("akey")

	ak := domain.AppKey{
		TenantID: scope.TenantID, AppID: scope.AppID, KeyID: keyID,
		KeyPrefix: prefix, KeyHash: keyHash, Status: "ACTIVE", CreatedAt: now,
	}
	err = r.db.WithScopeTx(ctx, dbScope(scope), func(ctx context.Context, tx *db.Tx) error {
		if _, err := tx.Exec(ctx,
			`UPDATE iam_app_keys SET status='ROTATING', grace_until=? WHERE tenant_id=? AND app_id=? AND status='ACTIVE'`,
			graceStr, scope.TenantID, scope.AppID,
		); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO iam_app_keys (tenant_id, app_id, key_id, key_prefix, key_hash, status, grace_until, last_used_at, created_at)
			 VALUES (?, ?, ?, ?, ?, 'ACTIVE', '', '', ?)`,
			scope.TenantID, scope.AppID, keyID, prefix, keyHash, nowStr,
		); err != nil {
			return err
		}
		_, err := tx.Exec(ctx,
			`UPDATE iam_scope_versions SET scope_version = scope_version + 1, updated_at=? WHERE tenant_id=? AND app_id=?`,
			nowStr, scope.TenantID, scope.AppID,
		)
		return err
	})
	return ak, rawKey, err
}

func (r *Repo) RevokeAppKey(ctx context.Context, scope domain.Scope, keyID string) error {
	return r.db.WithScopeTx(ctx, dbScope(scope), func(ctx context.Context, tx *db.Tx) error {
		res, err := tx.Exec(ctx,
			`UPDATE iam_app_keys SET status='REVOKED', grace_until='' WHERE tenant_id=? AND app_id=? AND key_id=?`,
			scope.TenantID, scope.AppID, keyID,
		)
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			return domain.NotFoundError("app key not found")
		}
		return nil
	})
}

// ResolveAppKey satisfies pkg/platform/auth.AppKeyResolver.
func (r *Repo) ResolveAppKey(ctx context.Context, rawKey string) (*auth.TrustedContext, error) {
	hash := crypto.SHA256Hex([]byte(rawKey))
	var tid, aid, keyID, status, graceStr string
	err := r.db.QueryRow(ctx,
		`SELECT tenant_id, app_id, key_id, status, grace_until FROM iam_app_keys WHERE key_hash=?`,
		hash,
	).Scan(&tid, &aid, &keyID, &status, &graceStr)
	if err != nil {
		return nil, domain.ErrUnauthorized
	}
	if status == "REVOKED" {
		return nil, domain.ErrUnauthorized
	}
	if status == "ROTATING" {
		gu := parseTime(graceStr)
		if !gu.IsZero() && r.Now().After(gu) {
			return nil, domain.ErrUnauthorized
		}
	}
	_, _ = r.db.Exec(ctx, `UPDATE iam_app_keys SET last_used_at=? WHERE key_hash=?`, r.NowStr(), hash)
	perms := map[string]struct{}{
		auth.PermAssetsRead: {}, auth.PermAssetsWrite: {},
		auth.PermConfigRead: {}, auth.PermConfigWrite: {},
		auth.PermDeployJobRead: {}, auth.PermDeployJobSubmit: {}, auth.PermDeployJobObserve: {},
		auth.PermNodesRead: {}, auth.PermGatewayRead: {},
	}
	plist := make([]string, 0, len(perms))
	for p := range perms {
		plist = append(plist, p)
	}
	return &auth.TrustedContext{
		TenantID:    tid,
		AppID:       aid,
		SubjectID:   "appkey:" + keyID,
		Username:    "appkey:" + keyID,
		DisplayName: "AppKey (" + keyID + ")",
		Roles:       []string{"app-service"},
		Perms:       perms,
		PermList:    plist,
		AuthKind:    "appkey",
	}, nil
}

func (r *Repo) ListProjects(ctx context.Context, scope domain.Scope) ([]domain.Project, error) {
	rows, err := r.db.Query(ctx,
		`SELECT tenant_id, app_id, project_id, code, name, description, permission_version, status, created_at, updated_at
		 FROM iam_projects WHERE tenant_id=? AND app_id=? ORDER BY created_at ASC`,
		scope.TenantID, scope.AppID,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []domain.Project
	for rows.Next() {
		var p domain.Project
		var ca, ua string
		if err := rows.Scan(&p.TenantID, &p.AppID, &p.ProjectID, &p.Code, &p.Name, &p.Description, &p.PermissionVersion, &p.Status, &ca, &ua); err != nil {
			return nil, err
		}
		p.CreatedAt, p.UpdatedAt = parseTime(ca), parseTime(ua)
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r *Repo) CreateProject(ctx context.Context, scope domain.Scope, code, name, desc string) (domain.Project, error) {
	now := r.Now()
	nowStr := formatTime(now)
	pid := id.New("prj")
	p := domain.Project{
		TenantID: scope.TenantID, AppID: scope.AppID, ProjectID: pid,
		Code: code, Name: name, Description: desc, PermissionVersion: 1,
		Status: "ACTIVE", CreatedAt: now, UpdatedAt: now,
	}
	err := r.db.WithScopeTx(ctx, dbScope(scope), func(ctx context.Context, tx *db.Tx) error {
		if _, err := tx.Exec(ctx,
			`INSERT INTO iam_projects (tenant_id, app_id, project_id, code, name, description, permission_version, status, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, 1, 'ACTIVE', ?, ?)`,
			scope.TenantID, scope.AppID, pid, code, name, desc, nowStr, nowStr,
		); err != nil {
			if isUniqueViolation(err) {
				return domain.NewError(domain.ErrAlreadyExists, "PROJECT_EXISTS", "项目编码已存在", nil)
			}
			return err
		}
		_, err := tx.Exec(ctx,
			`INSERT INTO iam_project_roles (tenant_id, app_id, project_id, role_id, role_name, description, created_at)
			 VALUES (?, ?, ?, 'proj-admin', '项目管理员', '默认项目治理角色', ?)`,
			scope.TenantID, scope.AppID, pid, nowStr,
		)
		return err
	})
	return p, err
}

func (r *Repo) ListProjectRoles(ctx context.Context, scope domain.Scope, projectID string) ([]domain.ProjectRole, int64, error) {
	var permVer int64
	if err := r.db.QueryRow(ctx,
		`SELECT permission_version FROM iam_projects WHERE tenant_id=? AND app_id=? AND project_id=?`,
		scope.TenantID, scope.AppID, projectID,
	).Scan(&permVer); err != nil {
		return nil, 0, mapNotFound(err, "project not found")
	}
	rows, err := r.db.Query(ctx,
		`SELECT role_id, role_name, description, created_at FROM iam_project_roles WHERE tenant_id=? AND app_id=? AND project_id=? ORDER BY created_at ASC`,
		scope.TenantID, scope.AppID, projectID,
	)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()
	var out []domain.ProjectRole
	for rows.Next() {
		var pr domain.ProjectRole
		var ca string
		pr.TenantID, pr.AppID, pr.ProjectID = scope.TenantID, scope.AppID, projectID
		if err := rows.Scan(&pr.RoleID, &pr.RoleName, &pr.Description, &ca); err != nil {
			return nil, 0, err
		}
		pr.CreatedAt = parseTime(ca)
		out = append(out, pr)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	for i := range out {
		perms, err := r.queryStrings(ctx,
			`SELECT perm_code FROM iam_project_role_perms WHERE tenant_id=? AND app_id=? AND project_id=? AND role_id=? ORDER BY perm_code ASC`,
			scope.TenantID, scope.AppID, projectID, out[i].RoleID,
		)
		if err != nil {
			return nil, 0, err
		}
		out[i].Permissions = perms
	}
	return out, permVer, nil
}

// ReplaceProjectRolePerms atomically replaces a project role's permissions under If-Match permission_version CAS (LiveOps 领域/01).
func (r *Repo) ReplaceProjectRolePerms(ctx context.Context, scope domain.Scope, projectID, roleID string, expectedVer int64, perms []string) (int64, error) {
	var newVer int64
	nowStr := r.NowStr()
	err := r.db.WithScopeTx(ctx, dbScope(scope), func(ctx context.Context, tx *db.Tx) error {
		res, err := tx.Exec(ctx,
			`UPDATE iam_projects SET permission_version = permission_version + 1, updated_at=?
			 WHERE tenant_id=? AND app_id=? AND project_id=? AND permission_version=?`,
			nowStr, scope.TenantID, scope.AppID, projectID, expectedVer,
		)
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			return domain.NewError(domain.ErrVersionMismatch, "VERSION_MISMATCH", "项目权限版本已变更，请刷新后重试", nil)
		}
		newVer = expectedVer + 1
		if _, err := tx.Exec(ctx,
			`DELETE FROM iam_project_role_perms WHERE tenant_id=? AND app_id=? AND project_id=? AND role_id=?`,
			scope.TenantID, scope.AppID, projectID, roleID,
		); err != nil {
			return err
		}
		for _, pc := range perms {
			pc = strings.TrimSpace(pc)
			if pc == "" {
				continue
			}
			if _, err := tx.Exec(ctx,
				`INSERT INTO iam_project_role_perms (tenant_id, app_id, project_id, role_id, perm_code) VALUES (?, ?, ?, ?, ?)`,
				scope.TenantID, scope.AppID, projectID, roleID, pc,
			); err != nil {
				return err
			}
		}
		_, err = tx.Exec(ctx,
			`UPDATE iam_scope_versions SET scope_version = scope_version + 1, updated_at=? WHERE tenant_id=? AND app_id=?`,
			nowStr, scope.TenantID, scope.AppID,
		)
		return err
	})
	return newVer, err
}

// ============================================================================
// Users, Roles, Route Policies & Live Scope Resolution
// ============================================================================

func (r *Repo) GetUserByUsername(ctx context.Context, username string) (domain.User, error) {
	var u domain.User
	var lu, ll, ca, ua string
	err := r.db.QueryRow(ctx,
		`SELECT user_id, tenant_id, username, display_name, email, password_hash, status, failed_attempts, locked_until, last_login_at, created_at, updated_at
		 FROM iam_users WHERE username=?`,
		username,
	).Scan(&u.UserID, &u.TenantID, &u.Username, &u.DisplayName, &u.Email, &u.PasswordHash, &u.Status, &u.FailedAttempts, &lu, &ll, &ca, &ua)
	if err != nil {
		return domain.User{}, mapNotFound(err, "user not found")
	}
	u.LockedUntil, u.LastLoginAt, u.CreatedAt, u.UpdatedAt = parseTimePtr(lu), parseTimePtr(ll), parseTime(ca), parseTime(ua)
	return u, nil
}

func (r *Repo) UpdateUserLoginState(ctx context.Context, userID, status string, failedAttempts int, lockedUntil, lastLogin *time.Time) error {
	_, err := r.db.Exec(ctx,
		`UPDATE iam_users SET status=?, failed_attempts=?, locked_until=?, last_login_at=?, updated_at=? WHERE user_id=?`,
		status, failedAttempts, formatTimePtr(lockedUntil), formatTimePtr(lastLogin), r.NowStr(), userID,
	)
	return err
}

func (r *Repo) ListUsers(ctx context.Context, tenantID string) ([]domain.User, error) {
	q := `SELECT user_id, tenant_id, username, display_name, email, status, failed_attempts, locked_until, last_login_at, created_at, updated_at FROM iam_users`
	var args []any
	if tenantID != "" {
		q += ` WHERE tenant_id=?`
		args = append(args, tenantID)
	}
	q += ` ORDER BY created_at ASC`
	rows, err := r.db.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []domain.User
	for rows.Next() {
		var u domain.User
		var lu, ll, ca, ua string
		if err := rows.Scan(&u.UserID, &u.TenantID, &u.Username, &u.DisplayName, &u.Email, &u.Status, &u.FailedAttempts, &lu, &ll, &ca, &ua); err != nil {
			return nil, err
		}
		u.LockedUntil, u.LastLoginAt, u.CreatedAt, u.UpdatedAt = parseTimePtr(lu), parseTimePtr(ll), parseTime(ca), parseTime(ua)
		out = append(out, u)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		roles, err := r.queryStrings(ctx,
			`SELECT r.code FROM iam_user_roles ur JOIN iam_roles r ON ur.role_id = r.role_id WHERE ur.user_id=?`,
			out[i].UserID,
		)
		if err != nil {
			return nil, err
		}
		out[i].Roles = roles
	}
	return out, nil
}

func (r *Repo) CreateUser(ctx context.Context, tenantID, username, displayName, email, passwordHash, roleID string) (domain.User, error) {
	now := r.Now()
	nowStr := formatTime(now)
	uid := id.New("usr")
	u := domain.User{
		UserID: uid, TenantID: tenantID, Username: username, DisplayName: displayName,
		Email: email, Status: "ACTIVE", CreatedAt: now, UpdatedAt: now,
	}
	err := r.db.WithTx(ctx, func(ctx context.Context, tx *db.Tx) error {
		if _, err := tx.Exec(ctx,
			`INSERT INTO iam_users (user_id, tenant_id, username, display_name, email, password_hash, status, failed_attempts, locked_until, last_login_at, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, 'ACTIVE', 0, '', '', ?, ?)`,
			uid, tenantID, username, displayName, email, passwordHash, nowStr, nowStr,
		); err != nil {
			if isUniqueViolation(err) {
				return domain.NewError(domain.ErrAlreadyExists, "USER_EXISTS", "用户名已存在", nil)
			}
			return err
		}
		if roleID != "" {
			if _, err := tx.Exec(ctx,
				`INSERT INTO iam_user_roles (user_id, role_id, tenant_id, app_id, project_id, created_at) VALUES (?, ?, ?, '', '', ?)`,
				uid, roleID, tenantID, nowStr,
			); err != nil {
				return err
			}
		}
		return nil
	})
	return u, err
}

func (r *Repo) ListRoles(ctx context.Context, tenantID string) ([]domain.Role, error) {
	q := `SELECT role_id, tenant_id, code, name, scope_level, description, is_builtin, created_at, updated_at FROM iam_roles`
	var args []any
	if tenantID != "" {
		q += ` WHERE tenant_id=?`
		args = append(args, tenantID)
	}
	q += ` ORDER BY created_at ASC`
	rows, err := r.db.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []domain.Role
	for rows.Next() {
		var ro domain.Role
		var builtin int
		var ca, ua string
		if err := rows.Scan(&ro.RoleID, &ro.TenantID, &ro.Code, &ro.Name, &ro.ScopeLevel, &ro.Description, &builtin, &ca, &ua); err != nil {
			return nil, err
		}
		ro.IsBuiltin = builtin != 0
		ro.CreatedAt, ro.UpdatedAt = parseTime(ca), parseTime(ua)
		out = append(out, ro)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		perms, err := r.queryStrings(ctx, `SELECT perm_code FROM iam_role_perms WHERE role_id=? ORDER BY perm_code ASC`, out[i].RoleID)
		if err != nil {
			return nil, err
		}
		out[i].Permissions = perms
	}
	return out, nil
}

func (r *Repo) queryStrings(ctx context.Context, query string, args ...any) ([]string, error) {
	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (r *Repo) ListRoutePolicies(ctx context.Context, tenantID string) ([]domain.RoutePolicy, error) {
	rows, err := r.db.Query(ctx,
		`SELECT policy_id, tenant_id, method, path_pattern, required_perm, scope_level, enabled, description, created_at, updated_at
		 FROM iam_route_policies WHERE tenant_id=? ORDER BY path_pattern ASC`,
		tenantID,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []domain.RoutePolicy
	for rows.Next() {
		var p domain.RoutePolicy
		var en int
		var ca, ua string
		if err := rows.Scan(&p.PolicyID, &p.TenantID, &p.Method, &p.PathPattern, &p.RequiredPerm, &p.ScopeLevel, &en, &p.Description, &ca, &ua); err != nil {
			return nil, err
		}
		p.Enabled = en != 0
		p.CreatedAt, p.UpdatedAt = parseTime(ca), parseTime(ua)
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r *Repo) UpsertRoutePolicy(ctx context.Context, tenantID, method, pathPattern, requiredPerm, scopeLevel, desc string, enabled bool) (domain.RoutePolicy, error) {
	now := r.Now()
	nowStr := formatTime(now)
	pid := id.New("pol")
	p := domain.RoutePolicy{
		PolicyID: pid, TenantID: tenantID, Method: strings.ToUpper(method), PathPattern: pathPattern,
		RequiredPerm: requiredPerm, ScopeLevel: scopeLevel, Enabled: enabled, Description: desc,
		CreatedAt: now, UpdatedAt: now,
	}
	_, err := r.db.Exec(ctx,
		`INSERT INTO iam_route_policies (policy_id, tenant_id, method, path_pattern, required_perm, scope_level, enabled, description, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(tenant_id, method, path_pattern) DO UPDATE SET required_perm=excluded.required_perm, scope_level=excluded.scope_level, enabled=excluded.enabled, description=excluded.description, updated_at=excluded.updated_at`,
		pid, tenantID, p.Method, pathPattern, requiredPerm, scopeLevel, boolInt(enabled), desc, nowStr, nowStr,
	)
	return p, err
}

// ResolveScope satisfies pkg/platform/auth.ScopeResolver — ALWAYS checks live user status, roles, perms & scopeVersion!
func (r *Repo) ResolveScope(ctx context.Context, subjectID, requestedTenant, requestedApp, requestedProject string) (*auth.TrustedContext, error) {
	var uid, userTenant, username, displayName, status string
	err := r.db.QueryRow(ctx,
		`SELECT user_id, tenant_id, username, display_name, status FROM iam_users WHERE user_id=?`,
		subjectID,
	).Scan(&uid, &userTenant, &username, &displayName, &status)
	if err != nil {
		return nil, domain.ErrUnauthorized
	}
	if status != "ACTIVE" {
		return nil, domain.ErrForbidden
	}

	rows, err := r.db.Query(ctx,
		`SELECT DISTINCT r.code, rp.perm_code
		 FROM iam_user_roles ur
		 JOIN iam_roles r ON ur.role_id = r.role_id
		 JOIN iam_role_perms rp ON rp.role_id = r.role_id
		 WHERE ur.user_id=?`,
		uid,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	roleSet := make(map[string]struct{})
	permSet := make(map[string]struct{})
	for rows.Next() {
		var rc, pc string
		if err := rows.Scan(&rc, &pc); err != nil {
			return nil, err
		}
		roleSet[rc] = struct{}{}
		permSet[pc] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	_, isWildcard := permSet["*"]
	_, isPlatformAdmin := permSet[auth.PermPlatformAdmin]

	tid := userTenant
	if requestedTenant != "" {
		if requestedTenant == userTenant || isWildcard || isPlatformAdmin {
			var exists int
			if err := r.db.QueryRow(ctx, `SELECT COUNT(*) FROM iam_tenants WHERE tenant_id=?`, requestedTenant).Scan(&exists); err == nil && exists > 0 {
				tid = requestedTenant
			} else {
				return nil, domain.ErrForbidden
			}
		} else {
			return nil, domain.ErrForbidden
		}
	}

	aid := requestedApp
	if aid == "" {
		_ = r.db.QueryRow(ctx, `SELECT app_id FROM iam_apps WHERE tenant_id=? ORDER BY created_at ASC LIMIT 1`, tid).Scan(&aid)
	} else {
		var exists int
		if err := r.db.QueryRow(ctx, `SELECT COUNT(*) FROM iam_apps WHERE tenant_id=? AND app_id=?`, tid, aid).Scan(&exists); err != nil || exists == 0 {
			return nil, domain.ErrForbidden
		}
	}

	pid := requestedProject
	if pid == "" && aid != "" {
		_ = r.db.QueryRow(ctx, `SELECT project_id FROM iam_projects WHERE tenant_id=? AND app_id=? ORDER BY created_at ASC LIMIT 1`, tid, aid).Scan(&pid)
	}

	var scopeVer int64 = 1
	if aid != "" {
		_ = r.db.QueryRow(ctx, `SELECT scope_version FROM iam_scope_versions WHERE tenant_id=? AND app_id=?`, tid, aid).Scan(&scopeVer)
	}

	roles := make([]string, 0, len(roleSet))
	for rc := range roleSet {
		roles = append(roles, rc)
	}
	permList := make([]string, 0, len(permSet))
	for pc := range permSet {
		permList = append(permList, pc)
	}

	return &auth.TrustedContext{
		TenantID:     tid,
		AppID:        aid,
		ProjectID:    pid,
		SubjectID:    uid,
		Username:     username,
		DisplayName:  displayName,
		Roles:        roles,
		Perms:        permSet,
		PermList:     permList,
		ScopeVersion: scopeVer,
		AuthKind:     "session",
	}, nil
}

// ============================================================================
// Tamper-Evident HMAC-SHA256 Audit Chain
// ============================================================================

func (r *Repo) AppendAuditLog(ctx context.Context, entry domain.AuditLog, module string, httpStatus int, summary string) error {
	nowStr := r.NowStr()
	if entry.AuditID == "" {
		entry.AuditID = id.New("aud")
	}
	if entry.Outcome == "" {
		entry.Outcome = "SUCCESS"
	}
	if httpStatus == 0 {
		httpStatus = 200
	}
	return r.db.WithTx(ctx, func(ctx context.Context, tx *db.Tx) error {
		var prevSeq int64
		var prevHash string
		err := tx.QueryRow(ctx, `SELECT seq, signature FROM iam_audit_logs ORDER BY seq DESC LIMIT 1`).Scan(&prevSeq, &prevHash)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				prevSeq = 0
				prevHash = "GENESIS"
			} else {
				return err
			}
		}
		nextSeq := prevSeq + 1
		rec := audit.Record{
			ID:            entry.AuditID,
			TenantID:      entry.TenantID,
			AppID:         entry.AppID,
			ActorID:       entry.ActorID,
			ActorUsername: entry.ActorName,
			ClientIP:      entry.SourceIP,
			TraceID:       entry.RequestID,
			Module:        module,
			Action:        entry.Action,
			ResourceType:  entry.ResourceType,
			ResourceID:    entry.ResourceID,
			HTTPStatus:    httpStatus,
			Summary:       summary,
			PrevHash:      prevHash,
			OccurredAt:    nowStr,
		}
		sig := audit.ComputeSignature(r.auditSecret, rec)
		detail := marshalJSON(map[string]any{
			"summary":    summary,
			"module":     module,
			"httpStatus": httpStatus,
			"extra":      entry.DetailJSON,
		})
		_, err = tx.Exec(ctx,
			`INSERT INTO iam_audit_logs (audit_id, seq, tenant_id, app_id, actor_id, actor_name, action, resource_type, resource_id, outcome, request_id, source_ip, detail_json, prev_hash, signature, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			entry.AuditID, nextSeq, entry.TenantID, entry.AppID, entry.ActorID, entry.ActorName,
			entry.Action, entry.ResourceType, entry.ResourceID, entry.Outcome, entry.RequestID,
			entry.SourceIP, detail, prevHash, sig, nowStr,
		)
		return err
	})
}

func (r *Repo) ListAuditLogs(ctx context.Context, tenantID string, limit int) ([]domain.AuditLog, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	q := `SELECT audit_id, seq, tenant_id, app_id, actor_id, actor_name, action, resource_type, resource_id, outcome, request_id, source_ip, detail_json, prev_hash, signature, created_at FROM iam_audit_logs`
	var args []any
	if tenantID != "" {
		q += ` WHERE tenant_id=?`
		args = append(args, tenantID)
	}
	q += fmt.Sprintf(` ORDER BY seq DESC LIMIT %d`, limit)
	rows, err := r.db.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []domain.AuditLog
	for rows.Next() {
		var a domain.AuditLog
		var ca string
		if err := rows.Scan(&a.AuditID, &a.Seq, &a.TenantID, &a.AppID, &a.ActorID, &a.ActorName, &a.Action, &a.ResourceType, &a.ResourceID, &a.Outcome, &a.RequestID, &a.SourceIP, &a.DetailJSON, &a.PrevHash, &a.Signature, &ca); err != nil {
			return nil, err
		}
		a.CreatedAt = parseTime(ca)
		out = append(out, a)
	}
	return out, rows.Err()
}

func (r *Repo) VerifyAuditChain(ctx context.Context) (bool, int, int, string, error) {
	rows, err := r.db.Query(ctx,
		`SELECT audit_id, tenant_id, app_id, actor_id, actor_name, action, resource_type, resource_id, request_id, source_ip, detail_json, prev_hash, signature, created_at
		 FROM iam_audit_logs ORDER BY seq ASC`,
	)
	if err != nil {
		return false, 0, -1, "", err
	}
	defer func() { _ = rows.Close() }()
	var recs []audit.Record
	for rows.Next() {
		var aid, tid, appid, actorID, actorName, action, rtype, rid, reqID, ip, detailRaw, prevHash, sig, ca string
		if err := rows.Scan(&aid, &tid, &appid, &actorID, &actorName, &action, &rtype, &rid, &reqID, &ip, &detailRaw, &prevHash, &sig, &ca); err != nil {
			return false, 0, -1, "", err
		}
		var dm struct {
			Summary    string `json:"summary"`
			Module     string `json:"module"`
			HTTPStatus int    `json:"httpStatus"`
		}
		unmarshalJSON(detailRaw, &dm)
		recs = append(recs, audit.Record{
			ID:            aid,
			TenantID:      tid,
			AppID:         appid,
			ActorID:       actorID,
			ActorUsername: actorName,
			ClientIP:      ip,
			TraceID:       reqID,
			Module:        dm.Module,
			Action:        action,
			ResourceType:  rtype,
			ResourceID:    rid,
			HTTPStatus:    dm.HTTPStatus,
			Summary:       dm.Summary,
			PrevHash:      prevHash,
			HMACSignature: sig,
			OccurredAt:    ca,
		})
	}
	if err := rows.Err(); err != nil {
		return false, 0, -1, "", err
	}
	ok, brokenAt, reason := audit.VerifyChain(r.auditSecret, recs)
	return ok, len(recs), brokenAt, reason, nil
}
