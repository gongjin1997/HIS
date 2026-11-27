package v1alpha1

import (
	"io/fs"
	"time"
)

// ManageMode controls 3-Way Diff behavior for file.sync / template.render (ADR-070, D2 §3.2).
type ManageMode string

const (
	ManageModeManaged      ManageMode = "managed"
	ManageModeCreateOnly   ManageMode = "create-only"
	ManageModeManagedBlock ManageMode = "managed-block"
)

// HostLockMode declares whether a Phase requires shared or exclusive host locks (D2 §2.3).
type HostLockMode string

const (
	HostLockShared    HostLockMode = "shared"
	HostLockExclusive HostLockMode = "exclusive"
)

// FailPolicy controls how a Phase behaves when one or more hosts fail.
type FailPolicy struct {
	Mode         string  `json:"mode" yaml:"mode"` // fail-fast | continue-on-error
	MaxFailRatio float64 `json:"maxFailRatio,omitempty" yaml:"maxFailRatio,omitempty"`
}

// RollingPolicy configures rolling batch execution across hosts.
type RollingPolicy struct {
	MaxUnavailable int           `json:"maxUnavailable" yaml:"maxUnavailable"`
	PauseBetween   time.Duration `json:"pauseBetween,omitempty" yaml:"pauseBetween,omitempty"`
}

// HostSelector filters which hosts participate in a Phase.
type HostSelector struct {
	Roles   []string          `json:"roles,omitempty" yaml:"roles,omitempty"`
	HostIDs []string          `json:"hostIds,omitempty" yaml:"hostIds,omitempty"`
	Labels  map[string]string `json:"labels,omitempty" yaml:"labels,omitempty"`
}

// MountSpec declares an explicit bind-mount pre-creation target for compose.sync (D2 §5.1 #15).
// Explicitly declaring type=file|dir prevents Docker from creating directories for missing single files.
type MountSpec struct {
	Src  string      `json:"src" yaml:"src"`
	Dst  string      `json:"dst" yaml:"dst"`
	Type string      `json:"type" yaml:"type"` // file | dir
	UID  int         `json:"uid" yaml:"uid"`
	GID  int         `json:"gid" yaml:"gid"`
	Mode fs.FileMode `json:"mode" yaml:"mode"`
}

// UnitOpts configures systemd.unit behavior.
type UnitOpts struct {
	Enable          bool `json:"enable" yaml:"enable"`
	Start           bool `json:"start" yaml:"start"`
	RestartOnChange bool `json:"restartOnChange" yaml:"restartOnChange"`
	DaemonReload    bool `json:"daemonReload" yaml:"daemonReload"`
}

// CertIssueSpec configures cert.issue parameters.
type CertIssueSpec struct {
	CAScope    string        `json:"caScope" yaml:"caScope"`
	CommonName string        `json:"commonName" yaml:"commonName"`
	Orgs       []string      `json:"orgs,omitempty" yaml:"orgs,omitempty"`
	DNSNames   []string      `json:"dnsNames,omitempty" yaml:"dnsNames,omitempty"`
	IPs        []string      `json:"ips,omitempty" yaml:"ips,omitempty"`
	CertPath   string        `json:"certPath" yaml:"certPath"`
	KeyPath    string        `json:"keyPath" yaml:"keyPath"`
	CAPath     string        `json:"caPath,omitempty" yaml:"caPath,omitempty"`
	Owner      string        `json:"owner,omitempty" yaml:"owner,omitempty"`
	TTL        time.Duration `json:"ttl,omitempty" yaml:"ttl,omitempty"`
	KeyAlg     string        `json:"keyAlg,omitempty" yaml:"keyAlg,omitempty"` // ecdsa-p256 | rsa-2048
}

// HelmReleaseSpec configures helm.release parameters (Helm v4 SDK).
type HelmReleaseSpec struct {
	ReleaseName string         `json:"releaseName" yaml:"releaseName"`
	Namespace   string         `json:"namespace" yaml:"namespace"`
	ChartRef    string         `json:"chartRef" yaml:"chartRef"`
	Version     string         `json:"version,omitempty" yaml:"version,omitempty"`
	Values      map[string]any `json:"values,omitempty" yaml:"values,omitempty"`
	Atomic      bool           `json:"atomic" yaml:"atomic"`
	Wait        bool           `json:"wait" yaml:"wait"`
	Timeout     time.Duration  `json:"timeout,omitempty" yaml:"timeout,omitempty"`
}

// TaskSpec is a pure declarative specification of one of the 15 built-in platform Tasks.
// Plugins only construct TaskSpecs; execution is exclusively performed by internal/engine.
type TaskSpec struct {
	Key         string         `json:"key,omitempty" yaml:"key,omitempty"`
	Type        string         `json:"type" yaml:"type"`
	Params      map[string]any `json:"params,omitempty" yaml:"params,omitempty"`
	Template    *TemplateRef   `json:"template,omitempty" yaml:"template,omitempty"`
	Vars        map[string]any `json:"vars,omitempty" yaml:"vars,omitempty"`
	When        string         `json:"when,omitempty" yaml:"when,omitempty"`
	Async       bool           `json:"async,omitempty" yaml:"async,omitempty"`
	Timeout     time.Duration  `json:"timeout,omitempty" yaml:"timeout,omitempty"`
	Notify      string         `json:"notify,omitempty" yaml:"notify,omitempty"`
	Disruptive  bool           `json:"disruptive,omitempty" yaml:"disruptive,omitempty"`
	ManageMode  ManageMode     `json:"manageMode,omitempty" yaml:"manageMode,omitempty"`
	BlockID     string         `json:"blockId,omitempty" yaml:"blockId,omitempty"`
	RollbackCmd string         `json:"rollbackCmd,omitempty" yaml:"rollbackCmd,omitempty"`
}

// WithKey assigns an explicit stable step key to the task.
func (t TaskSpec) WithKey(key string) TaskSpec {
	t.Key = key
	return t
}

// WithManageMode sets managed / create-only / managed-block mode.
func (t TaskSpec) WithManageMode(mode ManageMode) TaskSpec {
	t.ManageMode = mode
	return t
}

// WithVars attaches extra template rendering variables.
func (t TaskSpec) WithVars(vars map[string]any) TaskSpec {
	t.Vars = vars
	return t
}

// WithAsync marks a long-running task for Async Detached execution (D1 §2.3).
func (t TaskSpec) WithAsync(timeout time.Duration) TaskSpec {
	t.Async = true
	t.Timeout = timeout
	return t
}

// WithWhen attaches a precompiled expr condition.
func (t TaskSpec) WithWhen(expr string) TaskSpec {
	t.When = expr
	return t
}

// WithParam sets a parameter value on the task spec.
func (t TaskSpec) WithParam(k string, v any) TaskSpec {
	if t.Params == nil {
		t.Params = make(map[string]any)
	}
	t.Params[k] = v
	return t
}

// PhaseSpec groups ordered Tasks executed across selected hosts.
type PhaseSpec struct {
	Name        string         `json:"name" yaml:"name"`
	DependsOn   []string       `json:"dependsOn,omitempty" yaml:"dependsOn,omitempty"`
	Hosts       HostSelector   `json:"hosts,omitempty" yaml:"hosts,omitempty"`
	Tasks       []TaskSpec     `json:"tasks" yaml:"tasks"`
	Concurrency int            `json:"concurrency,omitempty" yaml:"concurrency,omitempty"`
	Serial      bool           `json:"serial,omitempty" yaml:"serial,omitempty"`
	Rolling     *RollingPolicy `json:"rolling,omitempty" yaml:"rolling,omitempty"`
	FailPolicy  FailPolicy     `json:"failPolicy,omitempty" yaml:"failPolicy,omitempty"`
	Lock        HostLockMode   `json:"lock,omitempty" yaml:"lock,omitempty"`
	Disruptive  bool           `json:"disruptive,omitempty" yaml:"disruptive,omitempty"`
}

// Pipeline is the immutable declarative DAG produced by Operation.Build.
type Pipeline struct {
	Phases []PhaseSpec `json:"phases" yaml:"phases"`
}

// AddPhase appends a PhaseSpec to the Pipeline.
func (p *Pipeline) AddPhase(phase PhaseSpec) {
	if phase.Concurrency <= 0 && !phase.Serial {
		phase.Concurrency = 10
	}
	if phase.Lock == "" {
		phase.Lock = HostLockShared
	}
	if phase.FailPolicy.Mode == "" {
		phase.FailPolicy.Mode = "fail-fast"
	}
	p.Phases = append(p.Phases, phase)
}

// PipelineBuilder provides a fluent Builder pattern for immutable Pipeline specs (D1 §1.2 #6).
type PipelineBuilder struct {
	pipeline Pipeline
}

// NewPipelineBuilder starts constructing a Pipeline.
func NewPipelineBuilder() *PipelineBuilder {
	return &PipelineBuilder{}
}

// Phase adds a PhaseSpec and returns the builder.
func (b *PipelineBuilder) Phase(p PhaseSpec) *PipelineBuilder {
	b.pipeline.AddPhase(p)
	return b
}

// Build returns a copy of the constructed Pipeline.
func (b *PipelineBuilder) Build() *Pipeline {
	out := &Pipeline{Phases: make([]PhaseSpec, len(b.pipeline.Phases))}
	copy(out.Phases, b.pipeline.Phases)
	return out
}

// --- All 15 Built-in Platform Task Constructors (D2 §5.1, 05 §3.4) ---

// 1. file.sync
func FileSync(dst string, src TemplateRef, mode fs.FileMode, owner string) TaskSpec {
	t := src
	return TaskSpec{
		Type:       "file.sync",
		ManageMode: ManageModeManaged,
		Template:   &t,
		Params: map[string]any{
			"dst":   dst,
			"mode":  uint32(mode),
			"owner": owner,
		},
	}
}

// 2. template.render
func TemplateRender(dst string, src TemplateRef, mode fs.FileMode, owner string) TaskSpec {
	t := src
	return TaskSpec{
		Type:       "template.render",
		ManageMode: ManageModeManaged,
		Template:   &t,
		Params: map[string]any{
			"dst":   dst,
			"mode":  uint32(mode),
			"owner": owner,
		},
	}
}

// 3. pkg.ensure
func PkgEnsure(logical ...string) TaskSpec {
	return TaskSpec{
		Type:  "pkg.ensure",
		Async: true,
		Params: map[string]any{
			"packages": logical,
		},
	}
}

// 4. binary.install
func BinaryInstall(name, version string, artifact ArtifactRef) TaskSpec {
	return TaskSpec{
		Type:  "binary.install",
		Async: true,
		Params: map[string]any{
			"name":          name,
			"version":       version,
			"artifactName":  artifact.Name,
			"matchPageSize": artifact.MatchPageSize,
			"subPath":       artifact.SubPath,
		},
	}
}

// 5. systemd.unit
func SystemdUnit(name string, unit TemplateRef, opts UnitOpts) TaskSpec {
	t := unit
	if !opts.Start && opts.Enable {
		opts.Start = true
	}
	return TaskSpec{
		Type:     "systemd.unit",
		Template: &t,
		Params: map[string]any{
			"name":            name,
			"enable":          opts.Enable,
			"start":           opts.Start,
			"restartOnChange": opts.RestartOnChange,
			"daemonReload":    true,
		},
	}
}

// 6. sysctl.apply
func SysctlApply(dropIn string, params map[string]string) TaskSpec {
	return TaskSpec{
		Type: "sysctl.apply",
		Params: map[string]any{
			"dropIn": dropIn,
			"params": params,
		},
	}
}

// 7. kmod.ensure
func KmodEnsure(modules ...string) TaskSpec {
	return TaskSpec{
		Type: "kmod.ensure",
		Params: map[string]any{
			"modules": modules,
		},
	}
}

// 8. cmd.run
func CmdRun(script string, check string) TaskSpec {
	return TaskSpec{
		Type: "cmd.run",
		Params: map[string]any{
			"script": script,
			"check":  check,
		},
	}
}

// 9. wait.tcp
func WaitTCP(hostPort string, timeout time.Duration) TaskSpec {
	return TaskSpec{
		Type:    "wait.tcp",
		Timeout: timeout,
		Params: map[string]any{
			"hostPort":  hostPort,
			"timeoutMs": timeout.Milliseconds(),
		},
	}
}

// 10. wait.http
func WaitHTTP(url string, timeout time.Duration) TaskSpec {
	return TaskSpec{
		Type:    "wait.http",
		Timeout: timeout,
		Params: map[string]any{
			"url":       url,
			"timeoutMs": timeout.Milliseconds(),
		},
	}
}

// 11. cert.issue
func CertIssue(spec CertIssueSpec) TaskSpec {
	return TaskSpec{
		Type: "cert.issue",
		Params: map[string]any{
			"spec": spec,
		},
	}
}

// 12. helm.release
func HelmRelease(rel HelmReleaseSpec) TaskSpec {
	return TaskSpec{
		Type:  "helm.release",
		Async: true,
		Params: map[string]any{
			"release": rel,
		},
	}
}

// 13. docker.ensure (Docker 29.10 + Compose v5.8 + LimitNOFILE=1048576 + data-root + DOCKER-USER/nft guard)
func DockerEnsure(allowCIDRs []string) TaskSpec {
	return TaskSpec{
		Type:  "docker.ensure",
		Async: true,
		Params: map[string]any{
			"dockerVersion":   "29.10.0",
			"composeVersion":  "5.8.0",
			"limitNOFILE":     1048576,
			"dataRoot":        "/data/docker",
			"firewallBackend": "iptables",
			"allowCIDRs":      allowCIDRs,
		},
	}
}

// 14. image.load (registryMirror rewrite + Zot pull or streaming zstd tar load)
func ImageLoad(imageRef string) TaskSpec {
	return TaskSpec{
		Type:  "image.load",
		Async: true,
		Params: map[string]any{
			"imageRef": imageRef,
		},
	}
}

// 15. compose.sync (explicit mounts file|dir pre-creation, 0600 .env, legal Type=oneshot systemd unit)
func ComposeSync(serviceName, composePath string, mounts ...MountSpec) TaskSpec {
	return TaskSpec{
		Type:  "compose.sync",
		Async: true,
		Params: map[string]any{
			"serviceName": serviceName,
			"composePath": composePath,
			"mounts":      mounts,
		},
	}
}
