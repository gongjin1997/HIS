package ports

import (
	"context"
	"crypto/x509"
	"io"
	"io/fs"
	"net"
	"time"

	"github.com/kinstall/kinstall/internal/domain"
	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
)

// --- Executor Port (D1 §1.2, 05 §3.2) ---

type HostKeyMode string

const (
	HostKeyTOFU   HostKeyMode = "tofu"
	HostKeyPinned HostKeyMode = "pinned"
)

type SudoMode string

const (
	SudoNone     SudoMode = "none"
	SudoNoPasswd SudoMode = "nopasswd"
	SudoPassword SudoMode = "password"
)

type AuthMaterial struct {
	PrivateKey   []byte
	Passphrase   []byte
	Password     []byte
	SudoPassword []byte
}

// Zero wipes in-memory secret material (D1 §3.3).
func (a *AuthMaterial) Zero() {
	clear(a.PrivateKey)
	clear(a.Passphrase)
	clear(a.Password)
	clear(a.SudoPassword)
	a.PrivateKey = nil
	a.Passphrase = nil
	a.Password = nil
	a.SudoPassword = nil
}

type Target struct {
	HostID        string
	Addr          string
	Port          int
	User          string
	Auth          AuthMaterial
	Jumps         []Target
	HostKeyMode   HostKeyMode
	HostKeyFP     string
	LegacyRSA     bool
	SudoMode      SudoMode
	WorkDir       string
	OnHostKeyTOFU func(ctx context.Context, hostID, fp string) error
}

type Command struct {
	Script    string
	Sudo      bool
	Stdin     io.Reader
	Stdout    io.Writer
	Stderr    io.Writer
	Timeout   time.Duration
	Setsid    bool
	Async     bool
	TaskRunID string
	WorkDir   string
}

type Result struct {
	ExitCode   int
	Stdout     string
	Stderr     string
	Duration   time.Duration
	RunDir     string
	RemotePGID int
	LogOffset  int64
}

type RemoteFileInfo struct {
	Exists bool
	IsDir  bool
	Size   int64
	Mode   fs.FileMode
	SHA256 string
}

type Session interface {
	Run(ctx context.Context, cmd Command) (Result, error)
	Upload(ctx context.Context, r io.Reader, dst string, mode fs.FileMode, owner string) (sha256 string, err error)
	Download(ctx context.Context, src string, w io.Writer) error
	Stat(ctx context.Context, path string) (*RemoteFileInfo, error)
	Close() error
}

type Executor interface {
	Connect(ctx context.Context, target Target) (Session, error)
	Close() error
}

// --- Secrets Port (D1 §3, 05 §3.7) ---

type KEKProvider interface {
	ID() string
	Version() int
	Wrap(ctx context.Context, dek []byte, contextStr string) (wrapped string, kekVer int, err error)
	Unwrap(ctx context.Context, wrapped string, contextStr string) (dek []byte, err error)
	Rewrap(ctx context.Context, wrapped string, contextStr string) (newWrapped string, newVer int, err error)
}

type SecretStore interface {
	Encrypt(ctx context.Context, table, recordID string, keyVersion int, plaintext []byte) ([]byte, error)
	Decrypt(ctx context.Context, table, recordID string, keyVersion int, envelopeJSON []byte) ([]byte, error)
	Rewrap(ctx context.Context, table, recordID string, envelopeJSON []byte) ([]byte, error)
}

// --- OS Adapter Port (D1 §4, 05 §3.5) ---

type ServiceOp string

const (
	ServiceStart   ServiceOp = "start"
	ServiceStop    ServiceOp = "stop"
	ServiceRestart ServiceOp = "restart"
	ServiceReload  ServiceOp = "reload"
	ServiceEnable  ServiceOp = "enable"
	ServiceDisable ServiceOp = "disable"
)

type OSAdapter interface {
	Family() string
	RuleID() string
	PkgType() string
	PkgManager() string
	ResolvePkg(logical string) (pkgName string, missing bool, err error)
	PkgInstallCmd(pkgs []string) string
	PkgQueryCmd(pkgs []string) string
	PkgLockCmd(pkgs []string) string
	ServiceCmd(op ServiceOp, unit string) string
	FirewallBackend() string
	Bootloader() string
}

// --- Template & Expression Port (05 §3.9) ---

type Renderer interface {
	Render(ctx context.Context, tpl sdk.TemplateRef, vars map[string]any, registerSecret func(string)) ([]byte, error)
}

type Evaluator interface {
	EvalBool(expr string, env map[string]any) (bool, error)
}

// --- Media Resolver Port (D3 §7, ADR-077) ---

type ResolvedMedia struct {
	SourceType   string // local | obs | s3 | online
	DeliveryMode string // relay-cache | direct-presigned
	LocalPath    string
	URL          string
	SHA256       string
	SizeBytes    int64
	Presigned    bool
}

type MediaResolver interface {
	Resolve(ctx context.Context, policy domain.MediaPolicy, ref sdk.MediaRef) (*ResolvedMedia, error)
	ProbePolicy(ctx context.Context, policy domain.MediaPolicy) (map[string]any, error)
}

// --- PKI Issuer Port (05 §3.7) ---

type CertRequest struct {
	CAScope    string
	CommonName string
	Orgs       []string
	DNSNames   []string
	IPs        []net.IP
	Usages     []x509.ExtKeyUsage
	TTL        time.Duration
	KeyAlg     string // ecdsa-p256 | rsa-2048
}

type IssuedCert struct {
	CertPEM  []byte
	KeyPEM   []byte
	CAPEM    []byte
	Serial   string
	NotAfter time.Time
}

type PKIIssuer interface {
	EnsureCA(ctx context.Context, scope, commonName string) (certPEM, keyPEM []byte, err error)
	Issue(ctx context.Context, req CertRequest) (*IssuedCert, error)
}

// --- EventSink & AuditRecorder Ports (ARCHITECTURE §4) ---

type EventSink interface {
	Publish(ctx context.Context, ev domain.OperationEvent) (int64, error)
	SubscribeWakeup(opID string) (ch <-chan struct{}, unsubscribe func())
}

type AuditRecorder interface {
	Append(ctx context.Context, entry domain.AuditEntry) (int64, error)
}

// --- Locker Port (D2 §2.3) ---

type LockKey struct {
	Kind string // stack | instance | host
	ID   string
	Mode string // exclusive | shared
}

type Locker interface {
	AcquireAll(ctx context.Context, opID string, fencing int64, keys []LockKey, ttl time.Duration) (release func(context.Context) error, err error)
}

// --- Store Port (24 Tables, 05 §3.8, D2 §1.3, G1 §6.3) ---

type Store interface {
	Dialect() string // sqlite | postgres
	SchemaVersion(ctx context.Context) (int64, error)
	Close() error

	Projects() ProjectRepo
	Credentials() CredentialRepo
	Hosts() HostRepo
	Stacks() StackRepo
	Instances() InstanceRepo
	Operations() OperationRepo
	TaskRuns() TaskRunRepo
	OperationEvents() OperationEventRepo
	Locks() LockRepo
	Audit() AuditRepo
	Approvals() ApprovalRepo
	MediaPolicies() MediaPolicyRepo
	Auth() AuthRepo
	Idempotency() IdempotencyRepo
	Plans() PlanRepo
	StateBackups() StateBackupRepo
	Notifications() NotificationRepo
	Kubeconfigs() KubeconfigRepo
}

type ProjectRepo interface {
	Create(ctx context.Context, p *domain.Project) error
	Get(ctx context.Context, id string) (*domain.Project, error)
	GetByName(ctx context.Context, name string) (*domain.Project, error)
	List(ctx context.Context) ([]domain.Project, error)
	Delete(ctx context.Context, id string) error
}

type CredentialRepo interface {
	Create(ctx context.Context, c *domain.Credential) error
	Get(ctx context.Context, id string) (*domain.Credential, error)
	ListByProject(ctx context.Context, projectID string) ([]domain.Credential, error)
	UpdateEnvelope(ctx context.Context, id string, expectedVer int64, envelopeJSON []byte, keyVersion int, state domain.CredentialState) error
	Delete(ctx context.Context, id string) error
}

type HostRepo interface {
	Create(ctx context.Context, h *domain.Host) error
	Get(ctx context.Context, id string) (*domain.Host, error)
	GetByName(ctx context.Context, projectID, name string) (*domain.Host, error)
	ListByProject(ctx context.Context, projectID string) ([]domain.Host, error)
	Update(ctx context.Context, h *domain.Host) error
	UpdateFacts(ctx context.Context, id string, facts sdk.FactsView) error
	UpdateHostKey(ctx context.Context, id, fp string) error
	Delete(ctx context.Context, id string) error
}

type StackRepo interface {
	Create(ctx context.Context, s *domain.Stack) error
	Get(ctx context.Context, id string) (*domain.Stack, error)
	ListByProject(ctx context.Context, projectID string) ([]domain.Stack, error)
	Update(ctx context.Context, s *domain.Stack) error
	Delete(ctx context.Context, id string) error
}

type InstanceRepo interface {
	Create(ctx context.Context, inst *domain.Instance) error
	Get(ctx context.Context, id string) (*domain.Instance, error)
	ListByProject(ctx context.Context, projectID string) ([]domain.Instance, error)
	ListByStack(ctx context.Context, stackID string) ([]domain.Instance, error)
	Update(ctx context.Context, inst *domain.Instance) error
	Delete(ctx context.Context, id string) error
	ReplaceClaims(ctx context.Context, instanceID string, claims []domain.HostClaim) error
	ListClaimsByHost(ctx context.Context, hostID string) ([]domain.HostClaim, error)
}

type OperationRepo interface {
	Create(ctx context.Context, op *domain.Operation) error
	Get(ctx context.Context, id string) (*domain.Operation, error)
	ListByProject(ctx context.Context, projectID string, limit int) ([]domain.Operation, error)
	ClaimNext(ctx context.Context, workerID string, leaseTTL time.Duration) (*domain.Operation, error)
	RenewLease(ctx context.Context, opID, workerID string, fencing int64, leaseTTL time.Duration) (bool, error)
	Finish(ctx context.Context, opID, workerID string, fencing int64, state domain.OpState, errCode, errMsg string) (bool, error)
	TransitionState(ctx context.Context, opID string, from, to domain.OpState, approvedBy *string) (bool, error)
	ReleaseLease(ctx context.Context, opID, workerID string, fencing int64) error
}

type TaskRunRepo interface {
	Upsert(ctx context.Context, tr *domain.TaskRun) error
	ListByOperation(ctx context.Context, opID string) ([]domain.TaskRun, error)
	UpdateWithFencing(ctx context.Context, tr *domain.TaskRun, workerID string) (bool, error)
}

type OperationEventRepo interface {
	Append(ctx context.Context, ev *domain.OperationEvent) (int64, error)
	ListAfter(ctx context.Context, opID string, afterSeq int64, limit int) ([]domain.OperationEvent, error)
}

type LockRepo interface {
	AcquireBatch(ctx context.Context, opID string, fencing int64, keys []LockKey, ttl time.Duration) error
	RenewByOp(ctx context.Context, opID string, fencing int64, ttl time.Duration) error
	ReleaseByOp(ctx context.Context, opID string) error
}

type AuditRepo interface {
	AppendChained(ctx context.Context, entry domain.AuditEntry, computeHash func(seq int64, ts time.Time, prevSHA, prevSM3 string) (shaHex, sm3Hex string)) (int64, error)
	List(ctx context.Context, projectID string, afterSeq int64, limit int) ([]domain.AuditRecord, error)
	GetSyslogCursor(ctx context.Context, sinkID string) (int64, error)
	UpdateSyslogCursor(ctx context.Context, sinkID string, seq int64) error
}

type ApprovalRepo interface {
	Create(ctx context.Context, ap *domain.Approval) error
	Get(ctx context.Context, opID string) (*domain.Approval, error)
	ListPending(ctx context.Context, projectID string) ([]domain.Approval, error)
	Decide(ctx context.Context, opID, decidedBy string, approve bool, note string) error
}

type MediaPolicyRepo interface {
	Create(ctx context.Context, mp *domain.MediaPolicy) error
	Get(ctx context.Context, id string) (*domain.MediaPolicy, error)
	List(ctx context.Context, projectID string) ([]domain.MediaPolicy, error)
	ResolveEffective(ctx context.Context, projectID, instanceID string) (*domain.MediaPolicy, error)
	Update(ctx context.Context, mp *domain.MediaPolicy) error
	Delete(ctx context.Context, id string) error
}

type AuthRepo interface {
	CreateUser(ctx context.Context, u *domain.User) error
	GetUserByID(ctx context.Context, id string) (*domain.User, error)
	GetUserByUsername(ctx context.Context, username string) (*domain.User, error)
	GetUserByOIDC(ctx context.Context, issuer, subject string) (*domain.User, error)
	RecordLoginFailure(ctx context.Context, userID string, maxAttempts int, lockDuration time.Duration) (attempts int, lockedUntil *time.Time, err error)
	ResetLoginFailures(ctx context.Context, userID string) error
	CreateSession(ctx context.Context, s *domain.UserSession) error
	GetSessionByTokenHash(ctx context.Context, hash string) (*domain.UserSession, error)
	RevokeSession(ctx context.Context, id string) error
	CreateAPIToken(ctx context.Context, t *domain.APIToken) error
	GetAPITokenByHash(ctx context.Context, hash string) (*domain.APIToken, error)
	ListAPITokens(ctx context.Context, userID string) ([]domain.APIToken, error)
	RevokeAPIToken(ctx context.Context, id string) error
}

type IdempotencyRepo interface {
	Acquire(ctx context.Context, rec domain.IdempotencyRecord) (existing *domain.IdempotencyRecord, created bool, err error)
	Complete(ctx context.Context, projectID, actorID, keyHash string, status int, body string) error
	Abort(ctx context.Context, projectID, actorID, keyHash string) error
}

type PlanRepo interface {
	Create(ctx context.Context, p *domain.Plan) error
	Get(ctx context.Context, id string) (*domain.Plan, error)
	MarkConsumed(ctx context.Context, planID, opID string) error
}

type StateBackupRepo interface {
	Create(ctx context.Context, bk *domain.StateBackup) error
	ListByInstance(ctx context.Context, instanceID string) ([]domain.StateBackup, error)
}

type NotificationRepo interface {
	CreateChannel(ctx context.Context, ch *domain.NotificationChannel) error
	ListChannels(ctx context.Context, projectID string) ([]domain.NotificationChannel, error)
	UpsertDelivery(ctx context.Context, d *domain.NotificationDelivery) (folded bool, err error)
	MarkDelivery(ctx context.Context, id, status, lastErr string) error
}

type KubeconfigRepo interface {
	Create(ctx context.Context, kc *domain.ClusterKubeconfig) error
	Get(ctx context.Context, id string) (*domain.ClusterKubeconfig, error)
	ListByInstance(ctx context.Context, instanceID string) ([]domain.ClusterKubeconfig, error)
	Revoke(ctx context.Context, id, revokedBy string) error
}
