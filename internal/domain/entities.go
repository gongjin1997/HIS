package domain

import (
	"encoding/json"
	"time"

	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
)

// Role enumerates the 5 platform RBAC roles (05 §4.3, G1 §4).
type Role string

const (
	RoleAdmin        Role = "admin"
	RoleProjectAdmin Role = "project-admin"
	RoleOperator     Role = "operator"
	RoleAuditor      Role = "auditor"
	RoleViewer       Role = "viewer"
)

// RoleBinding scopes a role to a project and optional environment.
type RoleBinding struct {
	ProjectID string `json:"projectId"`
	Env       string `json:"env,omitempty"`
	Role      Role   `json:"role"`
}

// Actor identifies the authenticated caller for RBAC, approvals, and audit logging.
type Actor struct {
	ID           string        `json:"id"`
	Username     string        `json:"username"`
	Type         string        `json:"type"` // user | token | system | cli
	SourceIP     string        `json:"sourceIp,omitempty"`
	RoleBindings []RoleBinding `json:"roleBindings,omitempty"`
	Scopes       []string      `json:"scopes,omitempty"`
}

// Project is the multi-tenant isolation root (table 1: projects).
type Project struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// Credential stores encrypted secret envelopes (table 2: credentials).
// Plaintext NEVER appears in this struct.
type Credential struct {
	ID           string          `json:"id"`
	ProjectID    string          `json:"projectId"`
	Name         string          `json:"name"`
	Kind         string          `json:"kind"` // ssh_key | ssh_password | sudo_password | api_token | ca_key | s3_aksk | registry_auth | webhook_secret
	State        CredentialState `json:"state"`
	KeyVersion   int             `json:"keyVersion"`
	EnvelopeJSON json.RawMessage `json:"-"`
	PrevEnvelope json.RawMessage `json:"-"`
	CreatedBy    string          `json:"createdBy"`
	Version      int64           `json:"version"`
	CreatedAt    time.Time       `json:"createdAt"`
	UpdatedAt    time.Time       `json:"updatedAt"`
}

// Host represents a CMDB managed host (table 3: hosts).
type Host struct {
	ID           string            `json:"id"`
	ProjectID    string            `json:"projectId"`
	Name         string            `json:"name"`
	Address      string            `json:"address"`
	SSHPort      int               `json:"sshPort"`
	SSHUser      string            `json:"sshUser"`
	CredentialID string            `json:"credentialId"`
	JumpHostID   *string           `json:"jumpHostId,omitempty"`
	LegacyRSA    bool              `json:"legacyRsa"`
	WorkDir      string            `json:"workDir"`
	HostKeyMode  string            `json:"hostkeyMode"` // pinned | tofu
	HostKeyFP    string            `json:"hostkeyFp"`
	State        HostState         `json:"state"`
	FaultZone    string            `json:"faultZone"`
	FaultRack    string            `json:"faultRack"`
	Labels       map[string]string `json:"labels"`
	Facts        *sdk.FactsView    `json:"facts,omitempty"`
	FactsAt      *time.Time        `json:"factsAt,omitempty"`
	OSFamily     string            `json:"osFamily"`
	OSID         string            `json:"osId"`
	OSVersion    string            `json:"osVersion"`
	Arch         string            `json:"arch"`
	PageSize     int               `json:"pageSize"`
	InitProfile  string            `json:"initProfile"`
	Version      int64             `json:"version"`
	CreatedAt    time.Time         `json:"createdAt"`
	UpdatedAt    time.Time         `json:"updatedAt"`
}

// ToHostView projects Host into the read-only SDK view passed to plugins.
func (h Host) ToHostView(roles []string) sdk.HostView {
	var facts sdk.FactsView
	if h.Facts != nil {
		facts = *h.Facts
	}
	privateIP := facts.PrimaryIP4
	if privateIP == "" {
		privateIP = h.Address
	}
	return sdk.HostView{
		ID:        h.ID,
		Name:      h.Name,
		Address:   h.Address,
		PrivateIP: privateIP,
		SSHPort:   h.SSHPort,
		FaultZone: h.FaultZone,
		FaultRack: h.FaultRack,
		Labels:    h.Labels,
		Roles:     roles,
		Facts:     facts,
	}
}

// Stack represents a composite multi-instance application stack (table 4: stacks).
type Stack struct {
	ID              string          `json:"id"`
	ProjectID       string          `json:"projectId"`
	Env             string          `json:"env"`
	Name            string          `json:"name"`
	SpecYAML        string          `json:"specYaml"`
	ResolvedDAGJSON json.RawMessage `json:"resolvedDag"`
	State           string          `json:"state"`
	Version         int64           `json:"version"`
	CreatedAt       time.Time       `json:"createdAt"`
	UpdatedAt       time.Time       `json:"updatedAt"`
}

// Instance represents a managed product deployment (table 5: instances).
type Instance struct {
	ID              string           `json:"id"`
	ProjectID       string           `json:"projectId"`
	StackID         *string          `json:"stackId,omitempty"`
	Env             string           `json:"env"`
	Name            string           `json:"name"`
	Product         string           `json:"product"`
	DeployTarget    sdk.DeployTarget `json:"deployTarget"`
	ProductVersion  string           `json:"productVersion"`
	BOMRevision     string           `json:"bomRevision"`
	PluginSchemaVer int              `json:"pluginSchemaVer"`
	DesiredSpecJSON json.RawMessage  `json:"desiredSpec"`
	LastAppliedJSON json.RawMessage  `json:"lastApplied,omitempty"`
	StateJSON       json.RawMessage  `json:"stateJson"`
	OutputsJSON     json.RawMessage  `json:"outputs"`
	State           string           `json:"state"`
	Version         int64            `json:"version"`
	CreatedAt       time.Time        `json:"createdAt"`
	UpdatedAt       time.Time        `json:"updatedAt"`
}

// ToInstanceView projects Instance into the read-only SDK view.
func (i Instance) ToInstanceView() sdk.InstanceView {
	stackID := ""
	if i.StackID != nil {
		stackID = *i.StackID
	}
	return sdk.InstanceView{
		ID:              i.ID,
		ProjectID:       i.ProjectID,
		StackID:         stackID,
		Env:             i.Env,
		Name:            i.Name,
		Product:         i.Product,
		DeployTarget:    i.DeployTarget,
		ProductVersion:  i.ProductVersion,
		BOMRevision:     i.BOMRevision,
		PluginSchemaVer: i.PluginSchemaVer,
		Spec:            i.DesiredSpecJSON,
		LastApplied:     i.LastAppliedJSON,
		State:           i.StateJSON,
		Outputs:         i.OutputsJSON,
	}
}

// HostClaim tracks port, path, Docker subnet, and memory claims per host (table 6: instance_host_claims).
type HostClaim struct {
	ID         string          `json:"id"`
	InstanceID string          `json:"instanceId"`
	HostID     string          `json:"hostId"`
	Role       string          `json:"role"`
	Ports      []sdk.PortClaim `json:"ports"`
	Paths      []string        `json:"paths"`
	Subnets    []string        `json:"subnets"`
	MemMB      int             `json:"memMb"`
}

// Operation represents an asynchronous execution unit with lease & Fencing Token (table 7: operations).
type Operation struct {
	ID             string          `json:"id"`
	ProjectID      string          `json:"projectId"`
	TargetType     string          `json:"targetType"` // host | instance | stack
	TargetID       string          `json:"targetId"`
	Action         string          `json:"action"` // init | install | apply | scale | upgrade | backup | restore | uninstall
	State          OpState         `json:"state"`
	IdempotencyKey *string         `json:"idempotencyKey,omitempty"`
	RequestedBy    string          `json:"requestedBy"`
	ApprovedBy     *string         `json:"approvedBy,omitempty"`
	PipelineJSON   json.RawMessage `json:"pipeline"`
	LeaseOwner     *string         `json:"leaseOwner,omitempty"`
	LeaseExpiresAt *time.Time      `json:"leaseExpiresAt,omitempty"`
	FencingToken   int64           `json:"fencingToken"`
	ErrorCode      *string         `json:"errorCode,omitempty"`
	ErrorMessage   *string         `json:"errorMessage,omitempty"`
	StartedAt      *time.Time      `json:"startedAt,omitempty"`
	FinishedAt     *time.Time      `json:"finishedAt,omitempty"`
	CreatedAt      time.Time       `json:"createdAt"`
	UpdatedAt      time.Time       `json:"updatedAt"`
}

// TaskRun records one atomic Task execution on one Host within an Operation (table 8: task_runs).
type TaskRun struct {
	ID               string          `json:"id"`
	OperationID      string          `json:"operationId"`
	PhaseIdx         int             `json:"phaseIdx"`
	StepKey          string          `json:"stepKey"`
	HostID           *string         `json:"hostId,omitempty"`
	TaskType         string          `json:"taskType"`
	ParamsDigest     string          `json:"paramsDigest"`
	State            TaskState       `json:"state"`
	Attempt          int             `json:"attempt"`
	FencingToken     int64           `json:"fencingToken"`
	RemoteRunDir     *string         `json:"remoteRunDir,omitempty"`
	RemotePGID       *int            `json:"remotePgid,omitempty"`
	RemoteLogOffset  int64           `json:"remoteLogOffset"`
	DriftSummaryJSON json.RawMessage `json:"driftSummary,omitempty"`
	BackupRefJSON    json.RawMessage `json:"backupRef,omitempty"`
	ErrorCode        *string         `json:"errorCode,omitempty"`
	ErrorDetail      *string         `json:"errorDetail,omitempty"`
	StartedAt        *time.Time      `json:"startedAt,omitempty"`
	FinishedAt       *time.Time      `json:"finishedAt,omitempty"`
}

// OperationEvent is a monotonic SSE event entry (table 9: operation_events).
type OperationEvent struct {
	Seq         int64           `json:"seq"`
	OperationID string          `json:"operationId"`
	TaskRunID   *string         `json:"taskRunId,omitempty"`
	EventType   string          `json:"eventType"` // op.state | task.state | task.log | drift.warn | operation.approved
	PayloadJSON json.RawMessage `json:"payload"`
	CreatedAt   time.Time       `json:"createdAt"`
}

// ResourceLock represents a distributed Stack/Instance/Host lock (table 10: resource_locks).
type ResourceLock struct {
	ResourceType string    `json:"resourceType"` // stack | instance | host
	ResourceID   string    `json:"resourceId"`
	LockMode     string    `json:"lockMode"` // exclusive | shared
	HolderOpID   string    `json:"holderOpId"`
	FencingToken int64     `json:"fencingToken"`
	ExpiresAt    time.Time `json:"expiresAt"`
	AcquiredAt   time.Time `json:"acquiredAt"`
}

// AuditEntry carries caller-supplied fields for append-only hash-chain audit logging.
type AuditEntry struct {
	ActorID     string `json:"actorId"`
	ActorType   string `json:"actorType"`
	SourceIP    string `json:"sourceIp"`
	ProjectID   string `json:"projectId"`
	Action      string `json:"action"`
	TargetType  string `json:"targetType"`
	TargetID    string `json:"targetId"`
	Result      string `json:"result"` // success | denied | failure
	ErrCode     string `json:"errCode"`
	TraceID     string `json:"traceId"`
	PayloadJSON string `json:"payloadJson"`
}

// AuditRecord is a persisted row in audit_logs (table 12: audit_logs).
type AuditRecord struct {
	Seq          int64      `json:"seq"`
	OccurredAt   time.Time  `json:"occurredAt"`
	Entry        AuditEntry `json:"entry"`
	PrevSHA256   string     `json:"prevSha256"`
	RecordSHA256 string     `json:"recordSha256"`
	PrevSM3      string     `json:"prevSm3,omitempty"`
	RecordSM3    string     `json:"recordSm3,omitempty"`
}

// AuditChainHead is the single-row chain anchor (table 11: audit_chain_head).
type AuditChainHead struct {
	ChainID    int16
	LastSeq    int64
	LastSHA256 string
	LastSM3    string
}

// Approval represents a four-eyes approval record (table 14: operation_approvals).
type Approval struct {
	OperationID  string         `json:"operationId"`
	ProjectID    string         `json:"projectId"`
	EnvName      string         `json:"envName"`
	RequestedBy  string         `json:"requestedBy"`
	Reason       string         `json:"reason"`
	Status       ApprovalStatus `json:"status"`
	DecidedBy    *string        `json:"decidedBy,omitempty"`
	DecisionNote string         `json:"decisionNote"`
	ExpiresAt    time.Time      `json:"expiresAt"`
	CreatedAt    time.Time      `json:"createdAt"`
	DecidedAt    *time.Time     `json:"decidedAt,omitempty"`
}

// MediaSourceConfig describes one entry inside MediaPolicy.Sources (D3 §7, ADR-077).
type MediaSourceConfig struct {
	Type          string   `json:"type"`     // local | obs | s3 | online
	Priority      int      `json:"priority"` // lower executes first
	Internal      bool     `json:"internal"` // allowed under offlineStrict when true
	Roots         []string `json:"roots,omitempty"`
	Endpoint      string   `json:"endpoint,omitempty"`
	Region        string   `json:"region,omitempty"`
	Bucket        string   `json:"bucket,omitempty"`
	Prefix        string   `json:"prefix,omitempty"`
	Driver        string   `json:"driver,omitempty"` // s3v4 | obs-native
	CredentialRef string   `json:"credentialRef,omitempty"`
	VerifyTLS     bool     `json:"verifyTls"`
	BaseURL       string   `json:"baseUrl,omitempty"`
	ProxyURL      string   `json:"proxyUrl,omitempty"`
}

// MediaPolicy controls Local / OBS·S3 / Online ternary resolution (table 15: media_policies).
type MediaPolicy struct {
	ID            string              `json:"id"`
	Scope         string              `json:"scope"` // global | project | instance
	ProjectID     *string             `json:"projectId,omitempty"`
	InstanceID    *string             `json:"instanceId,omitempty"`
	Name          string              `json:"name"`
	IsDefault     bool                `json:"isDefault"`
	DeliveryMode  string              `json:"deliveryMode"` // auto | relay-cache | direct-presigned
	OfflineStrict bool                `json:"offlineStrict"`
	StrictLocal   bool                `json:"strictLocal"`
	Sources       []MediaSourceConfig `json:"sources"`
	LastProbeJSON json.RawMessage     `json:"lastProbe,omitempty"`
	LastProbedAt  *time.Time          `json:"lastProbedAt,omitempty"`
	Version       int64               `json:"version"`
	CreatedAt     time.Time           `json:"createdAt"`
	UpdatedAt     time.Time           `json:"updatedAt"`
}

// User models both local Argon2id and OIDC accounts (table 16: users).
type User struct {
	ID                  string        `json:"id"`
	Username            string        `json:"username"`
	DisplayName         string        `json:"displayName"`
	AuthSource          string        `json:"authSource"` // local | oidc
	OIDCIssuer          string        `json:"oidcIssuer,omitempty"`
	OIDCSubject         string        `json:"oidcSubject,omitempty"`
	PasswordHash        string        `json:"-"`
	RoleBindings        []RoleBinding `json:"roleBindings"`
	FailedLoginAttempts int           `json:"failedLoginAttempts"`
	LockedUntil         *time.Time    `json:"lockedUntil,omitempty"`
	Disabled            bool          `json:"disabled"`
	CreatedAt           time.Time     `json:"createdAt"`
	UpdatedAt           time.Time     `json:"updatedAt"`
}

// UserSession models a revocable server-side session (table 17: user_sessions).
type UserSession struct {
	ID        string     `json:"id"`
	UserID    string     `json:"userId"`
	TokenHash string     `json:"-"`
	SourceIP  string     `json:"sourceIp"`
	UserAgent string     `json:"userAgent"`
	ExpiresAt time.Time  `json:"expiresAt"`
	RevokedAt *time.Time `json:"revokedAt,omitempty"`
	CreatedAt time.Time  `json:"createdAt"`
}

// APIToken models a scoped short-lived API token (table 18: api_tokens).
type APIToken struct {
	ID          string     `json:"id"`
	UserID      string     `json:"userId"`
	ProjectID   *string    `json:"projectId,omitempty"`
	Name        string     `json:"name"`
	TokenPrefix string     `json:"tokenPrefix"`
	TokenHash   string     `json:"-"`
	Scopes      []string   `json:"scopes"`
	ExpiresAt   time.Time  `json:"expiresAt"`
	LastUsedAt  *time.Time `json:"lastUsedAt,omitempty"`
	RevokedAt   *time.Time `json:"revokedAt,omitempty"`
	CreatedAt   time.Time  `json:"createdAt"`
}

// IdempotencyRecord persists 24h HTTP write idempotency (table 19: idempotency_keys).
type IdempotencyRecord struct {
	ProjectID    string    `json:"projectId"`
	ActorID      string    `json:"actorId"`
	KeyHash      string    `json:"keyHash"`
	Endpoint     string    `json:"endpoint"`
	RequestHash  string    `json:"requestHash"`
	Status       string    `json:"status"` // in_progress | completed
	ResponseCode int       `json:"responseCode"`
	ResponseBody string    `json:"responseBody"`
	ExpiresAt    time.Time `json:"expiresAt"`
	CreatedAt    time.Time `json:"createdAt"`
}

// Plan persists a frozen two-phase plan snapshot (table 20: plans).
type Plan struct {
	ID             string          `json:"id"`
	ProjectID      string          `json:"projectId"`
	TargetType     string          `json:"targetType"` // host | instance | stack
	TargetID       string          `json:"targetId"`
	InputHash      string          `json:"inputHash"`
	DiffJSON       json.RawMessage `json:"diff"`
	WarningsJSON   json.RawMessage `json:"warnings"`
	PipelineJSON   json.RawMessage `json:"pipeline"`
	CreatedBy      string          `json:"createdBy"`
	ExpiresAt      time.Time       `json:"expiresAt"`
	ConsumedByOpID *string         `json:"consumedByOpId,omitempty"`
	CreatedAt      time.Time       `json:"createdAt"`
}

// StateBackup preserves instance state before StateMigrator runs (table 21: instance_state_backups).
type StateBackup struct {
	ID            string          `json:"id"`
	InstanceID    string          `json:"instanceId"`
	FromSchemaVer int             `json:"fromSchemaVer"`
	ToSchemaVer   int             `json:"toSchemaVer"`
	RawStateJSON  json.RawMessage `json:"rawState"`
	CreatedAt     time.Time       `json:"createdAt"`
}

// NotificationChannel configures webhook/email/IM destinations (table 22: notification_channels).
type NotificationChannel struct {
	ID            string          `json:"id"`
	ProjectID     string          `json:"projectId"`
	Name          string          `json:"name"`
	Kind          string          `json:"kind"` // webhook | email | wecom | dingtalk | feishu
	ConfigJSON    json.RawMessage `json:"config"`
	CredentialRef *string         `json:"credentialRef,omitempty"`
	Events        []string        `json:"events"`
	Enabled       bool            `json:"enabled"`
	CreatedAt     time.Time       `json:"createdAt"`
	UpdatedAt     time.Time       `json:"updatedAt"`
}

// NotificationDelivery tracks storm-folded deliveries (table 23: notification_deliveries).
type NotificationDelivery struct {
	ID          string     `json:"id"`
	ChannelID   string     `json:"channelId"`
	DedupKey    string     `json:"dedupKey"`
	EventCount  int        `json:"eventCount"`
	Status      string     `json:"status"` // pending | sent | failed
	Attempt     int        `json:"attempt"`
	LastError   string     `json:"lastError"`
	WindowEndAt time.Time  `json:"windowEndAt"`
	SentAt      *time.Time `json:"sentAt,omitempty"`
	CreatedAt   time.Time  `json:"createdAt"`
}

// ClusterKubeconfig records issued short-lived RBAC kubeconfigs (table 24: cluster_kubeconfigs).
type ClusterKubeconfig struct {
	ID         string     `json:"id"`
	InstanceID string     `json:"instanceId"`
	UserID     string     `json:"userId"`
	K8sRole    string     `json:"k8sRole"` // cluster-admin | ns-admin | edit | view
	Namespaces []string   `json:"namespaces"`
	CertSerial string     `json:"certSerial"`
	ExpiresAt  time.Time  `json:"expiresAt"`
	RevokedAt  *time.Time `json:"revokedAt,omitempty"`
	RevokedBy  *string    `json:"revokedBy,omitempty"`
	CreatedAt  time.Time  `json:"createdAt"`
}

// Canonical CMDB topology label keys stored on Host.Labels for unified Biz/Set/Module & Region/SecurityArea trees.
const (
	LabelCMDBBiz          = "cmdb.biz"
	LabelCMDBSet          = "cmdb.set"
	LabelCMDBModule       = "cmdb.module"
	LabelCMDBRegion       = "cmdb.region"
	LabelCMDBSecurityArea = "cmdb.security_area"
	LabelCMDBOwner        = "cmdb.owner"
	LabelCMDBAssetTag     = "cmdb.asset_tag"
)

// CMDBGroupRule defines one predicate in a CMDB dynamic host group.
type CMDBGroupRule struct {
	Field string `json:"field"` // biz | set | module | region | security_area | fault_zone | fault_rack | arch | os_family | state | page_size | min_cpu | min_mem_mb | label.<key>
	Op    string `json:"op"`    // eq | neq | contains | in | prefix | gte | lte | exists
	Value string `json:"value"`
}

// CMDBDynamicGroup represents a persisted rule-based dynamic host group (table 25: cmdb_dynamic_groups).
type CMDBDynamicGroup struct {
	ID          string          `json:"id"`
	ProjectID   string          `json:"projectId"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Rules       []CMDBGroupRule `json:"rules"`
	CreatedBy   string          `json:"createdBy"`
	CreatedAt   time.Time       `json:"createdAt"`
	UpdatedAt   time.Time       `json:"updatedAt"`
}

// CMDBSyncMapping configures external CMDB sync field mappings & defaults.
type CMDBSyncMapping struct {
	ColumnMap           map[string]string `json:"columnMap,omitempty"`
	DefaultBiz          string            `json:"defaultBiz,omitempty"`
	DefaultSet          string            `json:"defaultSet,omitempty"`
	DefaultModule       string            `json:"defaultModule,omitempty"`
	DefaultRegion       string            `json:"defaultRegion,omitempty"`
	DefaultSecurityArea string            `json:"defaultSecurityArea,omitempty"`
	DefaultZone         string            `json:"defaultZone,omitempty"`
	DefaultUser         string            `json:"defaultUser,omitempty"`
	UpdateExisting      bool              `json:"updateExisting"`
	AuthHeader          string            `json:"authHeader,omitempty"`
}

// CMDBSyncSource configures an external CMDB / HCS / LiveOps / Ansible synchronization source (table 26: cmdb_sync_sources).
type CMDBSyncSource struct {
	ID           string          `json:"id"`
	ProjectID    string          `json:"projectId"`
	Name         string          `json:"name"`
	Kind         string          `json:"kind"` // http_json | http_csv | liveops_json | ansible_ini | huawei_hcs
	Endpoint     string          `json:"endpoint"`
	CredentialID *string         `json:"credentialId,omitempty"`
	Mapping      CMDBSyncMapping `json:"mapping"`
	PayloadText  string          `json:"payloadText,omitempty"`
	LastSyncAt   *time.Time      `json:"lastSyncAt,omitempty"`
	LastStatus   string          `json:"lastStatus"`
	LastSummary  string          `json:"lastSummary"`
	CreatedAt    time.Time       `json:"createdAt"`
	UpdatedAt    time.Time       `json:"updatedAt"`
}
