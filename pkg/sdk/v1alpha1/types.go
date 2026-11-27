package v1alpha1

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

// SDKAPIVersion is the frozen contract version for v1alpha1 plugins.
const SDKAPIVersion = "v1alpha1"

// PluginType classifies a plugin registered in kinstall.
type PluginType string

const (
	PluginComponent  PluginType = "Component"
	PluginExtension  PluginType = "Extension"
	PluginOSAdapter  PluginType = "OSAdapter"
	PluginInitModule PluginType = "InitModule"
)

// DeployTarget enumerates the three first-class deployment targets (ADR-066).
type DeployTarget string

const (
	TargetHostBinary    DeployTarget = "host-binary"
	TargetHostContainer DeployTarget = "host-container"
	TargetKubernetes    DeployTarget = "kubernetes"
)

// ProductRange declares which product & version range a plugin supports.
type ProductRange struct {
	Product     string         `json:"product" yaml:"product"`
	Versions    string         `json:"versions" yaml:"versions"` // semver constraint e.g. ">=1.35.0, <1.39.0"
	Targets     []DeployTarget `json:"targets" yaml:"targets"`
	DefaultVer  string         `json:"defaultVersion,omitempty" yaml:"defaultVersion,omitempty"`
	LicenseTier string         `json:"licenseTier,omitempty" yaml:"licenseTier,omitempty"` // green | yellow | red
}

// Platforms declares supported OS families, architectures, and page sizes.
type Platforms struct {
	OSFamilies []string `json:"osFamilies,omitempty" yaml:"osFamilies,omitempty"`
	Arch       []string `json:"arch" yaml:"arch"`
	PageSizes  []int    `json:"pageSizes" yaml:"pageSizes"`
}

// PluginMeta describes static metadata exported by every plugin.
type PluginMeta struct {
	Name          string            `json:"name" yaml:"name"`
	Version       string            `json:"version" yaml:"version"`
	SDKVersion    string            `json:"sdkVersion" yaml:"sdkVersion"`
	Type          PluginType        `json:"type" yaml:"type"`
	Description   string            `json:"description,omitempty" yaml:"description,omitempty"`
	Products      []ProductRange    `json:"products,omitempty" yaml:"products,omitempty"`
	Platforms     Platforms         `json:"platforms" yaml:"platforms"`
	Operations    []string          `json:"operations,omitempty" yaml:"operations,omitempty"`     // supported Day-1/Day-2 lifecycle actions
	ParamSchema   map[string]any    `json:"paramSchema,omitempty" yaml:"paramSchema,omitempty"`   // JSON-Schema with x-kinstall-ui keywords (WEB_DESIGN §3.3)
	OutputSchema  map[string]string `json:"outputSchema,omitempty" yaml:"outputSchema,omitempty"` // key -> type description for Stack static validation
	DriftIgnore   []string          `json:"driftIgnore,omitempty" yaml:"driftIgnore,omitempty"`   // O-08: field/path whitelist ignored by 3-Way Diff
	SchemaVersion int               `json:"schemaVersion,omitempty" yaml:"schemaVersion,omitempty"`
}

// OSInfo holds normalized OS identity resolved via spec/osmatch.yaml.
type OSInfo struct {
	RuleID     string            `json:"ruleId" yaml:"ruleId"`
	Family     string            `json:"family" yaml:"family"`
	Parent     string            `json:"parent,omitempty" yaml:"parent,omitempty"`
	ID         string            `json:"id" yaml:"id"`
	VersionID  string            `json:"versionId" yaml:"versionId"`
	PrettyName string            `json:"prettyName,omitempty" yaml:"prettyName,omitempty"`
	Tier       string            `json:"tier,omitempty" yaml:"tier,omitempty"`
	Variants   map[string]string `json:"variants,omitempty" yaml:"variants,omitempty"`
}

// DiskView describes a block device discovered by Facts collection.
type DiskView struct {
	Name       string `json:"name" yaml:"name"`
	Type       string `json:"type" yaml:"type"`
	SizeBytes  int64  `json:"sizeBytes" yaml:"sizeBytes"`
	Rotational bool   `json:"rotational" yaml:"rotational"`
	FSType     string `json:"fsType,omitempty" yaml:"fsType,omitempty"`
	MountPoint string `json:"mountPoint,omitempty" yaml:"mountPoint,omitempty"`
	WWN        string `json:"wwn,omitempty" yaml:"wwn,omitempty"`
	Serial     string `json:"serial,omitempty" yaml:"serial,omitempty"`
}

// NICView describes a network interface discovered by Facts collection.
type NICView struct {
	Name      string   `json:"name" yaml:"name"`
	Addresses []string `json:"addresses" yaml:"addresses"`
	IsVirtual bool     `json:"isVirtual,omitempty" yaml:"isVirtual,omitempty"`
}

// ListeningPort describes a live listening TCP/UDP socket observed on a managed host during 1-RTT Facts probe.
type ListeningPort struct {
	Proto   string `json:"proto" yaml:"proto"`     // tcp | udp
	Address string `json:"address" yaml:"address"` // 0.0.0.0 | 127.0.0.1 | ::
	Port    int    `json:"port" yaml:"port"`
	Process string `json:"process,omitempty" yaml:"process,omitempty"`
}

// FactsView is the read-only host facts snapshot exposed to plugins.
type FactsView struct {
	Hostname       string            `json:"hostname" yaml:"hostname"`
	FQDN           string            `json:"fqdn,omitempty" yaml:"fqdn,omitempty"`
	OS             OSInfo            `json:"os" yaml:"os"`
	Kernel         string            `json:"kernel" yaml:"kernel"`
	Arch           string            `json:"arch" yaml:"arch"`
	PageSize       int               `json:"pageSize" yaml:"pageSize"`
	GlibcVersion   string            `json:"glibcVersion,omitempty" yaml:"glibcVersion,omitempty"`
	Libc           string            `json:"libc,omitempty" yaml:"libc,omitempty"` // glibc | musl
	MachineID      string            `json:"machineId,omitempty" yaml:"machineId,omitempty"`
	CgroupVer      int               `json:"cgroupVer" yaml:"cgroupVer"`
	SystemdVer     int               `json:"systemdVer,omitempty" yaml:"systemdVer,omitempty"`
	NFTVersion     string            `json:"nftVersion,omitempty" yaml:"nftVersion,omitempty"`
	X86Level       string            `json:"x86Level,omitempty" yaml:"x86Level,omitempty"`
	SecurityMod    []string          `json:"securityMod,omitempty" yaml:"securityMod,omitempty"`
	SudoImpl       string            `json:"sudoImpl,omitempty" yaml:"sudoImpl,omitempty"` // sudo | sudo-rs
	SELinuxMode    string            `json:"selinuxMode,omitempty" yaml:"selinuxMode,omitempty"`
	CPUCores       int               `json:"cpuCores" yaml:"cpuCores"`
	MemMB          int               `json:"memMB" yaml:"memMB"`
	SwapMB         int               `json:"swapMB" yaml:"swapMB"`
	HugePageKB     int               `json:"hugePageKB,omitempty" yaml:"hugePageKB,omitempty"`
	THPEnabled     string            `json:"thpEnabled,omitempty" yaml:"thpEnabled,omitempty"`
	Sysctls        map[string]string `json:"sysctls,omitempty" yaml:"sysctls,omitempty"`
	ListeningPorts []ListeningPort   `json:"listeningPorts,omitempty" yaml:"listeningPorts,omitempty"`
	NUMANodes      int               `json:"numaNodes,omitempty" yaml:"numaNodes,omitempty"`
	PrimaryIP4     string            `json:"primaryIP4,omitempty" yaml:"primaryIP4,omitempty"`
	PrimaryIP6     string            `json:"primaryIP6,omitempty" yaml:"primaryIP6,omitempty"`
	Disks          []DiskView        `json:"disks,omitempty" yaml:"disks,omitempty"`
	NICs           []NICView         `json:"nics,omitempty" yaml:"nics,omitempty"`
	CollectedAt    time.Time         `json:"collectedAt,omitempty" yaml:"collectedAt,omitempty"`
}

// HostView is the read-only host representation supplied to plugins.
type HostView struct {
	ID        string            `json:"id" yaml:"id"`
	Name      string            `json:"name" yaml:"name"`
	Address   string            `json:"address" yaml:"address"`
	PrivateIP string            `json:"privateIP" yaml:"privateIP"`
	SSHPort   int               `json:"sshPort" yaml:"sshPort"`
	FaultZone string            `json:"faultZone,omitempty" yaml:"faultZone,omitempty"`
	FaultRack string            `json:"faultRack,omitempty" yaml:"faultRack,omitempty"`
	Labels    map[string]string `json:"labels,omitempty" yaml:"labels,omitempty"`
	Roles     []string          `json:"roles,omitempty" yaml:"roles,omitempty"`
	Facts     FactsView         `json:"facts" yaml:"facts"`
}

// HasRole reports whether this host carries the specified role in the current instance.
func (h HostView) HasRole(role string) bool {
	for _, r := range h.Roles {
		if r == role {
			return true
		}
	}
	return false
}

// InstanceView is the read-only instance representation exposed to plugins.
type InstanceView struct {
	ID              string          `json:"id" yaml:"id"`
	ProjectID       string          `json:"projectId" yaml:"projectId"`
	StackID         string          `json:"stackId,omitempty" yaml:"stackId,omitempty"`
	Env             string          `json:"env" yaml:"env"`
	Name            string          `json:"name" yaml:"name"`
	Product         string          `json:"product" yaml:"product"`
	DeployTarget    DeployTarget    `json:"deployTarget" yaml:"deployTarget"`
	ProductVersion  string          `json:"productVersion" yaml:"productVersion"`
	BOMRevision     string          `json:"bomRevision,omitempty" yaml:"bomRevision,omitempty"`
	PluginSchemaVer int             `json:"pluginSchemaVer,omitempty" yaml:"pluginSchemaVer,omitempty"`
	Spec            json.RawMessage `json:"spec" yaml:"spec"`
	LastApplied     json.RawMessage `json:"lastApplied,omitempty" yaml:"lastApplied,omitempty"`
	State           json.RawMessage `json:"state,omitempty" yaml:"state,omitempty"`
	Outputs         json.RawMessage `json:"outputs,omitempty" yaml:"outputs,omitempty"`
}

// BOMArtifact describes a resolved artifact entry inside a BOM.
type BOMArtifact struct {
	Name      string `json:"name" yaml:"name"`
	Version   string `json:"version" yaml:"version"`
	Arch      string `json:"arch" yaml:"arch"`
	PageSizes []int  `json:"pageSizes,omitempty" yaml:"pageSizes,omitempty"`
	Kind      string `json:"kind" yaml:"kind"` // binary | tarball | image | chart
	URL       string `json:"url,omitempty" yaml:"url,omitempty"`
	SHA256    string `json:"sha256,omitempty" yaml:"sha256,omitempty"`
	Digest    string `json:"digest,omitempty" yaml:"digest,omitempty"`
	License   string `json:"license,omitempty" yaml:"license,omitempty"`
}

// BOMView gives plugins read-only access to the active BOM for artifact & page-size checks.
type BOMView interface {
	Revision() string
	HasArtifact(name, arch string, pageSize int) bool
	GetArtifact(name, arch string, pageSize int) (BOMArtifact, bool)
	ResolveImage(logical string) (string, bool)
}

// Severity indicates whether a validation finding blocks execution or merely warns.
type Severity string

const (
	SeverityBlock Severity = "block"
	SeverityWarn  Severity = "warn"
	SeverityInfo  Severity = "info"
)

// Finding is a single preflight or validation report item.
type Finding struct {
	Severity Severity `json:"severity" yaml:"severity"`
	Code     string   `json:"code" yaml:"code"`
	HostID   string   `json:"hostId,omitempty" yaml:"hostId,omitempty"`
	Message  string   `json:"message" yaml:"message"`
}

// ValidationReport aggregates blocking errors and non-blocking warnings.
type ValidationReport struct {
	Findings []Finding `json:"findings" yaml:"findings"`
}

// NewValidationReport allocates an empty ValidationReport.
func NewValidationReport() *ValidationReport {
	return &ValidationReport{Findings: make([]Finding, 0)}
}

// Block appends a blocking finding that halts Plan/Apply.
func (r *ValidationReport) Block(code, msg string) {
	r.Findings = append(r.Findings, Finding{Severity: SeverityBlock, Code: code, Message: msg})
}

// BlockHost appends a host-scoped blocking finding.
func (r *ValidationReport) BlockHost(hostID, code, msg string) {
	r.Findings = append(r.Findings, Finding{Severity: SeverityBlock, Code: code, HostID: hostID, Message: msg})
}

// Warn appends a non-blocking warning finding.
func (r *ValidationReport) Warn(code, msg string) {
	r.Findings = append(r.Findings, Finding{Severity: SeverityWarn, Code: code, Message: msg})
}

// HasBlocking returns true if any finding has SeverityBlock.
func (r *ValidationReport) HasBlocking() bool {
	if r == nil {
		return false
	}
	for _, f := range r.Findings {
		if f.Severity == SeverityBlock {
			return true
		}
	}
	return false
}

// Merge appends all findings from another report.
func (r *ValidationReport) Merge(other *ValidationReport) {
	if other == nil {
		return
	}
	r.Findings = append(r.Findings, other.Findings...)
}

// HostChange summarizes a planned mutation on a single host.
type HostChange struct {
	HostID   string `json:"hostId" yaml:"hostId"`
	HostName string `json:"hostName,omitempty" yaml:"hostName,omitempty"`
	Kind     string `json:"kind" yaml:"kind"` // file | package | binary | service | container | helm | cmd
	Target   string `json:"target" yaml:"target"`
	Action   string `json:"action" yaml:"action"` // create | update | delete | skip | drift
	Diff     string `json:"diff,omitempty" yaml:"diff,omitempty"`
}

// PlanResult is returned by Component.Plan.
type PlanResult struct {
	Summary  string                    `json:"summary" yaml:"summary"`
	Changes  []HostChange              `json:"changes,omitempty" yaml:"changes,omitempty"`
	Claims   map[string]HostRoleClaims `json:"claims,omitempty" yaml:"claims,omitempty"` // hostID -> claims
	Pipeline *Pipeline                 `json:"pipeline,omitempty" yaml:"pipeline,omitempty"`
	Warnings []Finding                 `json:"warnings,omitempty" yaml:"warnings,omitempty"`
	Outputs  map[string]any            `json:"outputs,omitempty" yaml:"outputs,omitempty"`
}

// InstanceStatus represents the live health & endpoint summary of an instance.
type InstanceStatus struct {
	Health    string            `json:"health" yaml:"health"` // healthy | degraded | unhealthy | unknown
	Message   string            `json:"message,omitempty" yaml:"message,omitempty"`
	Endpoints map[string]string `json:"endpoints,omitempty" yaml:"endpoints,omitempty"`
	Details   map[string]any    `json:"details,omitempty" yaml:"details,omitempty"`
	CheckedAt time.Time         `json:"checkedAt,omitempty" yaml:"checkedAt,omitempty"`
}

// DottedVersionLess performs segment-by-segment numeric version comparison so
// "2.9" < "2.28" and "5.8" < "5.13" (18 §3.3, 05 §3.4).
// Plugins must NEVER compare version strings with < or >.
func DottedVersionLess(a, b string) bool {
	pa := parseNumericSegments(a)
	pb := parseNumericSegments(b)
	n := len(pa)
	if len(pb) > n {
		n = len(pb)
	}
	for i := 0; i < n; i++ {
		var va, vb int
		if i < len(pa) {
			va = pa[i]
		}
		if i < len(pb) {
			vb = pb[i]
		}
		if va != vb {
			return va < vb
		}
	}
	return false
}

func parseNumericSegments(v string) []int {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "v")
	if v == "" {
		return nil
	}
	parts := strings.Split(v, ".")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		// Take leading digits in case segment has suffix like "0-1160" or "28-el8".
		end := 0
		for end < len(p) && p[end] >= '0' && p[end] <= '9' {
			end++
		}
		if end == 0 {
			out = append(out, 0)
			continue
		}
		num, err := strconv.Atoi(p[:end])
		if err != nil {
			out = append(out, 0)
			continue
		}
		out = append(out, num)
	}
	return out
}
