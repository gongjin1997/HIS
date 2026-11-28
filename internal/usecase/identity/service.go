package identity

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/kinstall/kinstall/internal/adapter/store"
	"github.com/kinstall/kinstall/internal/domain"
	"github.com/kinstall/kinstall/pkg/platform/auth"
	"github.com/kinstall/kinstall/pkg/platform/crypto"
)

type Service struct {
	repo   *store.Repo
	tokens *auth.TokenManager
}

func New(repo *store.Repo, tokens *auth.TokenManager) *Service {
	return &Service{repo: repo, tokens: tokens}
}

type LoginResult struct {
	Token     string              `json:"token"`
	ExpiresAt time.Time           `json:"expiresAt"`
	Context   auth.TrustedContext `json:"context"`
}

func (s *Service) Login(ctx context.Context, username, password, sourceIP, traceID string) (LoginResult, error) {
	username = strings.TrimSpace(username)
	if username == "" || password == "" {
		return LoginResult{}, domain.ValidationError("用户名和密码不能为空")
	}
	user, err := s.repo.GetUserByUsername(ctx, username)
	if err != nil {
		return LoginResult{}, domain.NewError(domain.ErrUnauthorized, "INVALID_CREDENTIALS", "用户名或密码错误", nil)
	}
	now := s.repo.Now()
	if user.Status == "DISABLED" {
		return LoginResult{}, domain.NewError(domain.ErrForbidden, "ACCOUNT_DISABLED", "账号已禁用，请联系平台管理员解封", nil)
	}
	if user.LockedUntil != nil && now.Before(*user.LockedUntil) {
		rem := int(time.Until(*user.LockedUntil).Seconds())
		if rem < 1 {
			rem = int(user.LockedUntil.Sub(now).Seconds())
		}
		return LoginResult{}, domain.NewError(domain.ErrRateLimited, "ACCOUNT_LOCKED", fmt.Sprintf("登录失败次数过多，请 %d 秒后重试", rem), map[string]any{"retryAfterSec": rem})
	}

	if !crypto.VerifyPassword(password, user.PasswordHash) {
		fails := user.FailedAttempts + 1
		lockStatus, lockUntil := auth.EvaluateProgressiveLockout(fails, now)
		nextDBStatus := user.Status
		var luPtr *time.Time
		if lockStatus == "DISABLED" {
			nextDBStatus = "DISABLED"
		} else if !lockUntil.IsZero() {
			luPtr = &lockUntil
		}
		_ = s.repo.UpdateUserLoginState(ctx, user.UserID, nextDBStatus, fails, luPtr, user.LastLoginAt)
		_ = s.repo.AppendAuditLog(ctx, domain.AuditLog{
			TenantID: user.TenantID, ActorID: user.UserID, ActorName: user.Username,
			Action: "AUTH_LOGIN_FAILED", ResourceType: "user", ResourceID: user.UserID,
			Outcome: "DENIED", RequestID: traceID, SourceIP: sourceIP,
		}, "iam", 401, fmt.Sprintf("用户 %s 登录密码校验失败 (第 %d 次)", user.Username, fails))
		if lockStatus == "DISABLED" {
			return LoginResult{}, domain.NewError(domain.ErrForbidden, "ACCOUNT_DISABLED", "连续登录失败达到上限，账号已自动禁用", nil)
		}
		if lockStatus == "LOCKED" {
			return LoginResult{}, domain.NewError(domain.ErrRateLimited, "ACCOUNT_LOCKED", "登录失败次数过多，账号已触发渐进锁定", nil)
		}
		return LoginResult{}, domain.NewError(domain.ErrUnauthorized, "INVALID_CREDENTIALS", "用户名或密码错误", nil)
	}

	// Reset failed attempts & update last_login_at
	_ = s.repo.UpdateUserLoginState(ctx, user.UserID, "ACTIVE", 0, nil, &now)

	tc, err := s.repo.ResolveScope(ctx, user.UserID, user.TenantID, "", "")
	if err != nil {
		return LoginResult{}, err
	}
	tc.TraceID = traceID
	tok, exp, err := s.tokens.Issue(*tc)
	if err != nil {
		return LoginResult{}, err
	}
	_ = s.repo.AppendAuditLog(ctx, domain.AuditLog{
		TenantID: tc.TenantID, AppID: tc.AppID, ActorID: tc.SubjectID, ActorName: tc.Username,
		Action: "AUTH_LOGIN_SUCCESS", ResourceType: "session", ResourceID: tc.SubjectID,
		Outcome: "SUCCESS", RequestID: traceID, SourceIP: sourceIP,
	}, "iam", 200, fmt.Sprintf("用户 %s 登录控制台成功", tc.Username))

	return LoginResult{Token: tok, ExpiresAt: exp, Context: *tc}, nil
}

func (s *Service) ResolveScope(ctx context.Context, subjectID, reqTenant, reqApp, reqProject string) (*auth.TrustedContext, error) {
	return s.repo.ResolveScope(ctx, subjectID, reqTenant, reqApp, reqProject)
}

func (s *Service) ResolveAppKey(ctx context.Context, rawKey string) (*auth.TrustedContext, error) {
	return s.repo.ResolveAppKey(ctx, rawKey)
}

func (s *Service) ListTenants(ctx context.Context) ([]domain.Tenant, error) {
	return s.repo.ListTenants(ctx)
}

func (s *Service) CreateTenant(ctx context.Context, tc auth.TrustedContext, code, name, desc string) (domain.Tenant, error) {
	code, name = strings.TrimSpace(code), strings.TrimSpace(name)
	if code == "" || name == "" {
		return domain.Tenant{}, domain.ValidationError("租户编码与名称不能为空")
	}
	t, err := s.repo.CreateTenant(ctx, code, name, desc)
	if err == nil {
		s.RecordAudit(ctx, tc, "iam", "TENANT_CREATE", "tenant", t.TenantID, 201, "创建企业空间/租户 "+t.Name)
	}
	return t, err
}

func (s *Service) ListApps(ctx context.Context, tenantID string) ([]domain.App, error) {
	return s.repo.ListApps(ctx, tenantID)
}

func (s *Service) CreateApp(ctx context.Context, tc auth.TrustedContext, tenantID, code, name, desc string) (domain.App, error) {
	code, name = strings.TrimSpace(code), strings.TrimSpace(name)
	if code == "" || name == "" {
		return domain.App{}, domain.ValidationError("应用编码与名称不能为空")
	}
	a, err := s.repo.CreateApp(ctx, tenantID, code, name, desc)
	if err == nil {
		s.RecordAudit(ctx, tc, "iam", "APP_CREATE", "app", a.AppID, 201, "创建应用 "+a.Name)
	}
	return a, err
}

func (s *Service) ListAppKeys(ctx context.Context, scope domain.Scope) ([]domain.AppKey, error) {
	return s.repo.ListAppKeys(ctx, scope)
}

func (s *Service) RotateAppKey(ctx context.Context, tc auth.TrustedContext, scope domain.Scope, graceWindow time.Duration) (domain.AppKey, string, error) {
	ak, raw, err := s.repo.RotateAppKey(ctx, scope, graceWindow)
	if err == nil {
		s.RecordAudit(ctx, tc, "iam", "APP_KEY_ROTATE", "app_key", ak.KeyID, 201, "执行双轨 AppKey 轮换 "+ak.KeyPrefix+"***")
	}
	return ak, raw, err
}

func (s *Service) RevokeAppKey(ctx context.Context, tc auth.TrustedContext, scope domain.Scope, keyID string) error {
	err := s.repo.RevokeAppKey(ctx, scope, keyID)
	if err == nil {
		s.RecordAudit(ctx, tc, "iam", "APP_KEY_REVOKE", "app_key", keyID, 200, "吊销 AppKey "+keyID)
	}
	return err
}

func (s *Service) ListProjects(ctx context.Context, scope domain.Scope) ([]domain.Project, error) {
	return s.repo.ListProjects(ctx, scope)
}

func (s *Service) CreateProject(ctx context.Context, tc auth.TrustedContext, scope domain.Scope, code, name, desc string) (domain.Project, error) {
	code, name = strings.TrimSpace(code), strings.TrimSpace(name)
	if code == "" || name == "" {
		return domain.Project{}, domain.ValidationError("项目编码与名称不能为空")
	}
	p, err := s.repo.CreateProject(ctx, scope, code, name, desc)
	if err == nil {
		s.RecordAudit(ctx, tc, "iam", "PROJECT_CREATE", "project", p.ProjectID, 201, "创建项目 "+p.Name)
	}
	return p, err
}

func (s *Service) ListProjectRoles(ctx context.Context, scope domain.Scope, projectID string) ([]domain.ProjectRole, int64, error) {
	return s.repo.ListProjectRoles(ctx, scope, projectID)
}

func (s *Service) ReplaceProjectRolePerms(ctx context.Context, tc auth.TrustedContext, scope domain.Scope, projectID, roleID string, expectedVer int64, perms []string) (int64, error) {
	v, err := s.repo.ReplaceProjectRolePerms(ctx, scope, projectID, roleID, expectedVer, perms)
	if err == nil {
		s.RecordAudit(ctx, tc, "iam", "PROJECT_ROLE_PERMS_REPLACE", "project_role", projectID+":"+roleID, 200, fmt.Sprintf("替换项目角色权限，新版本 v%d", v))
	}
	return v, err
}

func (s *Service) ListUsers(ctx context.Context, tenantID string) ([]domain.User, error) {
	return s.repo.ListUsers(ctx, tenantID)
}

func (s *Service) CreateUser(ctx context.Context, tc auth.TrustedContext, tenantID, username, displayName, email, password, roleID string) (domain.User, error) {
	username = strings.TrimSpace(username)
	if username == "" {
		return domain.User{}, domain.ValidationError("用户名不能为空")
	}
	if err := crypto.ValidatePasswordPolicy(username, password); err != nil {
		return domain.User{}, domain.ValidationError(err.Error())
	}
	hash, err := crypto.HashPassword(password)
	if err != nil {
		return domain.User{}, err
	}
	if displayName == "" {
		displayName = username
	}
	u, err := s.repo.CreateUser(ctx, tenantID, username, displayName, email, hash, roleID)
	if err == nil {
		s.RecordAudit(ctx, tc, "iam", "USER_CREATE", "user", u.UserID, 201, "创建平台用户 "+u.Username)
	}
	return u, err
}

func (s *Service) ListRoles(ctx context.Context, tenantID string) ([]domain.Role, error) {
	return s.repo.ListRoles(ctx, tenantID)
}

func (s *Service) ListRoutePolicies(ctx context.Context, tenantID string) ([]domain.RoutePolicy, error) {
	return s.repo.ListRoutePolicies(ctx, tenantID)
}

func (s *Service) UpsertRoutePolicy(ctx context.Context, tc auth.TrustedContext, tenantID, method, pathPattern, requiredPerm, scopeLevel, desc string, enabled bool) (domain.RoutePolicy, error) {
	if strings.TrimSpace(method) == "" || strings.TrimSpace(pathPattern) == "" || strings.TrimSpace(requiredPerm) == "" {
		return domain.RoutePolicy{}, domain.ValidationError("方法、路径模式与权限码均不能为空")
	}
	p, err := s.repo.UpsertRoutePolicy(ctx, tenantID, method, pathPattern, requiredPerm, scopeLevel, desc, enabled)
	if err == nil {
		s.RecordAudit(ctx, tc, "iam", "ROUTE_POLICY_UPSERT", "route_policy", p.Method+" "+p.PathPattern, 200, "更新动态鉴权路由策略")
	}
	return p, err
}

func (s *Service) ListAuditLogs(ctx context.Context, tenantID string, limit int) ([]domain.AuditLog, error) {
	return s.repo.ListAuditLogs(ctx, tenantID, limit)
}

type AuditVerifyResult struct {
	Valid    bool   `json:"valid"`
	Total    int    `json:"total"`
	BrokenAt int    `json:"brokenAt"`
	Reason   string `json:"reason,omitempty"`
}

func (s *Service) VerifyAuditChain(ctx context.Context) (AuditVerifyResult, error) {
	ok, total, brokenAt, reason, err := s.repo.VerifyAuditChain(ctx)
	return AuditVerifyResult{Valid: ok, Total: total, BrokenAt: brokenAt, Reason: reason}, err
}

func (s *Service) RecordAudit(ctx context.Context, tc auth.TrustedContext, module, action, resType, resID string, status int, summary string) {
	_ = s.repo.AppendAuditLog(ctx, domain.AuditLog{
		TenantID: tc.TenantID, AppID: tc.AppID, ActorID: tc.SubjectID, ActorName: tc.Username,
		Action: action, ResourceType: resType, ResourceID: resID, Outcome: "SUCCESS", RequestID: tc.TraceID,
	}, module, status, summary)
}
