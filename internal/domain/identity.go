package domain

import "time"

type Tenant struct {
	TenantID    string    `json:"tenant_id"`
	Code        string    `json:"code"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type App struct {
	TenantID    string    `json:"tenant_id"`
	AppID       string    `json:"app_id"`
	Code        string    `json:"code"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type AppKey struct {
	TenantID   string     `json:"tenant_id"`
	AppID      string     `json:"app_id"`
	KeyID      string     `json:"key_id"`
	KeyPrefix  string     `json:"key_prefix"`
	KeyHash    string     `json:"-"`
	Status     string     `json:"status"` // ACTIVE | ROTATING | REVOKED
	GraceUntil *time.Time `json:"grace_until,omitempty"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

type Project struct {
	TenantID          string    `json:"tenant_id"`
	AppID             string    `json:"app_id"`
	ProjectID         string    `json:"project_id"`
	Code              string    `json:"code"`
	Name              string    `json:"name"`
	Description       string    `json:"description"`
	PermissionVersion int64     `json:"permission_version"`
	Status            string    `json:"status"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

type User struct {
	UserID         string     `json:"user_id"`
	TenantID       string     `json:"tenant_id"`
	Username       string     `json:"username"`
	DisplayName    string     `json:"display_name"`
	Email          string     `json:"email"`
	PasswordHash   string     `json:"-"`
	Status         string     `json:"status"` // ACTIVE | DISABLED
	FailedAttempts int        `json:"failed_attempts"`
	LockedUntil    *time.Time `json:"locked_until,omitempty"`
	LastLoginAt    *time.Time `json:"last_login_at,omitempty"`
	Roles          []string   `json:"roles,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

type Role struct {
	RoleID      string    `json:"role_id"`
	TenantID    string    `json:"tenant_id"`
	Code        string    `json:"code"`
	Name        string    `json:"name"`
	ScopeLevel  string    `json:"scope_level"` // PLATFORM | WORKSPACE | PROJECT
	Description string    `json:"description"`
	IsBuiltin   bool      `json:"is_builtin"`
	Permissions []string  `json:"permissions"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type Permission struct {
	PermCode    string `json:"perm_code"`
	Domain      string `json:"domain"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

type ProjectRole struct {
	TenantID    string    `json:"tenant_id"`
	AppID       string    `json:"app_id"`
	ProjectID   string    `json:"project_id"`
	RoleID      string    `json:"role_id"`
	RoleName    string    `json:"role_name"`
	Description string    `json:"description"`
	Permissions []string  `json:"permissions"`
	CreatedAt   time.Time `json:"created_at"`
}

type RoutePolicy struct {
	PolicyID     string    `json:"policy_id"`
	TenantID     string    `json:"tenant_id"`
	Method       string    `json:"method"`
	PathPattern  string    `json:"path_pattern"`
	RequiredPerm string    `json:"required_perm"`
	ScopeLevel   string    `json:"scope_level"`
	Enabled      bool      `json:"enabled"`
	Description  string    `json:"description"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type AuditLog struct {
	AuditID      string    `json:"audit_id"`
	Seq          int64     `json:"seq"`
	TenantID     string    `json:"tenant_id"`
	AppID        string    `json:"app_id"`
	ActorID      string    `json:"actor_id"`
	ActorName    string    `json:"actor_name"`
	Action       string    `json:"action"`
	ResourceType string    `json:"resource_type"`
	ResourceID   string    `json:"resource_id"`
	Outcome      string    `json:"outcome"`
	RequestID    string    `json:"request_id"`
	SourceIP     string    `json:"source_ip"`
	DetailJSON   string    `json:"detail_json"`
	PrevHash     string    `json:"prev_hash"`
	Signature    string    `json:"signature"`
	CreatedAt    time.Time `json:"created_at"`
}
