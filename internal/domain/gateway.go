package domain

import "time"

type GatewayRouter struct {
	TenantID     string    `json:"tenant_id"`
	AppID        string    `json:"app_id"`
	RouterID     string    `json:"router_id"`
	Name         string    `json:"name"`
	ListenHost   string    `json:"listen_host"`
	RateLimitQPS int       `json:"rate_limit_qps"`
	Burst        int       `json:"burst"`
	Status       string    `json:"status"`
	RouteVersion int64     `json:"route_version,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type GatewayRoute struct {
	TenantID     string    `json:"tenant_id"`
	AppID        string    `json:"app_id"`
	RouteID      string    `json:"route_id"`
	RouterID     string    `json:"router_id"`
	Name         string    `json:"name"`
	PathPrefix   string    `json:"path_prefix"`
	UpstreamURL  string    `json:"upstream_url"`
	IsDefault    bool      `json:"is_default"`
	StripPrefix  bool      `json:"strip_prefix"`
	TimeoutMs    int       `json:"timeout_ms"`
	RateLimitQPS int       `json:"rate_limit_qps"`
	Status       string    `json:"status"`
	Version      int64     `json:"version"`
	RouteVersion int64     `json:"route_version,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type TelemetryBucket struct {
	TenantID       string    `json:"tenant_id"`
	AppID          string    `json:"app_id"`
	RouterID       string    `json:"router_id"`
	BucketKind     string    `json:"bucket_kind"` // day | month
	BucketKey      string    `json:"bucket_key"`
	TotalRequests  int64     `json:"total_requests"`
	ErrorRequests  int64     `json:"error_requests"`
	TotalLatencyMs int64     `json:"total_latency_ms"`
	AvgLatencyMs   float64   `json:"avg_latency_ms"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type ArtifactFile struct {
	TenantID           string     `json:"tenant_id"`
	AppID              string     `json:"app_id"`
	FileID             string     `json:"file_id"`
	Filename           string     `json:"filename"`
	ContentType        string     `json:"content_type"`
	SizeBytes          int64      `json:"size_bytes"`
	SHA256             string     `json:"sha256"`
	StoragePath        string     `json:"storage_path"`
	VerificationStatus string     `json:"verification_status"` // PENDING | VERIFIED | REJECTED
	VerifiedAt         *time.Time `json:"verified_at,omitempty"`
	CreatedBy          string     `json:"created_by"`
	CreatedAt          time.Time  `json:"created_at"`
}

type ArtifactRelease struct {
	TenantID   string     `json:"tenant_id"`
	AppID      string     `json:"app_id"`
	ReleaseID  string     `json:"release_id"`
	Name       string     `json:"name"`
	Version    string     `json:"version"`
	FileID     string     `json:"file_id"`
	Status     string     `json:"status"` // DRAFT | PROMOTED | ARCHIVED
	Notes      string     `json:"notes"`
	PromotedAt *time.Time `json:"promoted_at,omitempty"`
	CreatedBy  string     `json:"created_by"`
	CreatedAt  time.Time  `json:"created_at"`
}

type ArtifactAttachment struct {
	TenantID     string    `json:"tenant_id"`
	AppID        string    `json:"app_id"`
	AttachmentID string    `json:"attachment_id"`
	OwnerType    string    `json:"owner_type"`
	OwnerID      string    `json:"owner_id"`
	FileID       string    `json:"file_id"`
	CreatedAt    time.Time `json:"created_at"`
}
