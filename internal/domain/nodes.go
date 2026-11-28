package domain

import "time"

type NodeStatus string

const (
	NodeOnline   NodeStatus = "ONLINE"
	NodeDegraded NodeStatus = "DEGRADED"
	NodeOffline  NodeStatus = "OFFLINE"
	NodeIsolated NodeStatus = "ISOLATED"
	NodeRevoked  NodeStatus = "REVOKED"
)

const (
	NodeHeartbeatInterval  = 15 * time.Second
	NodeDegradedThreshold  = 30 * time.Second
	NodeOfflineThreshold   = 45 * time.Second
	NodeCredGraceWindow    = 5 * time.Minute
	NodeEnrollTokenDefault = 15 * time.Minute
)

type EnrollToken struct {
	TenantID     string            `json:"tenant_id"`
	AppID        string            `json:"app_id"`
	TokenID      string            `json:"token_id"`
	RawToken     string            `json:"token,omitempty"` // Only populated once at issuance
	TokenHash    string            `json:"-"`
	Labels       map[string]string `json:"labels"`
	ExpiresAt    time.Time         `json:"expires_at"`
	UsedAt       *time.Time        `json:"used_at,omitempty"`
	UsedByNodeID string            `json:"used_by_node_id,omitempty"`
	CreatedBy    string            `json:"created_by"`
	CreatedAt    time.Time         `json:"created_at"`
}

type Node struct {
	TenantID      string            `json:"tenant_id"`
	AppID         string            `json:"app_id"`
	NodeID        string            `json:"node_id"`
	Hostname      string            `json:"hostname"`
	IPAddress     string            `json:"ip_address"`
	OSArch        string            `json:"os_arch"`
	AgentVersion  string            `json:"agent_version"`
	Status        NodeStatus        `json:"status"`
	Labels        map[string]string `json:"labels"`
	TokenHash     string            `json:"-"`
	PrevTokenHash string            `json:"-"`
	PrevExpiresAt *time.Time        `json:"prev_expires_at,omitempty"`
	CredVersion   int64             `json:"cred_version"`
	LastSeenAt    time.Time         `json:"last_seen_at"`
	EnrolledAt    time.Time         `json:"enrolled_at"`
	UpdatedAt     time.Time         `json:"updated_at"`
}

type HostGroup struct {
	TenantID    string    `json:"tenant_id"`
	AppID       string    `json:"app_id"`
	GroupID     string    `json:"group_id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	NodeIDs     []string  `json:"node_ids,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

type Distribution struct {
	TenantID       string                   `json:"tenant_id"`
	AppID          string                   `json:"app_id"`
	DistID         string                   `json:"dist_id"`
	ArtifactFileID string                   `json:"artifact_file_id"`
	TargetNodeID   string                   `json:"target_node_id"`
	TotalBytes     int64                    `json:"total_bytes"`
	VerifiedBytes  int64                    `json:"verified_bytes"`
	SHA256         string                   `json:"sha256"`
	State          string                   `json:"state"` // PENDING | STREAMING | VERIFIED | FAILED
	Checkpoints    []DistributionCheckpoint `json:"checkpoints,omitempty"`
	CreatedAt      time.Time                `json:"created_at"`
	UpdatedAt      time.Time                `json:"updated_at"`
}

type DistributionCheckpoint struct {
	TenantID    string    `json:"tenant_id"`
	AppID       string    `json:"app_id"`
	DistID      string    `json:"dist_id"`
	ChunkIndex  int       `json:"chunk_index"`
	ChunkOffset int64     `json:"chunk_offset"`
	ChunkBytes  int64     `json:"chunk_bytes"`
	ChunkSHA256 string    `json:"chunk_sha256"`
	AckedAt     time.Time `json:"acked_at"`
}

// EvaluateNodeHealth computes LiveOps heartbeat-derived status (15s heartbeat / >30s DEGRADED / >45s OFFLINE)
// while never overriding explicit administrative ISOLATED or REVOKED states.
func EvaluateNodeHealth(current NodeStatus, lastSeen, now time.Time) NodeStatus {
	if current == NodeIsolated || current == NodeRevoked {
		return current
	}
	age := now.Sub(lastSeen)
	if age > NodeOfflineThreshold {
		return NodeOffline
	}
	if age > NodeDegradedThreshold {
		return NodeDegraded
	}
	return NodeOnline
}
