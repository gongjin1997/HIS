package v1alpha1

import (
	"encoding/json"
	"io/fs"
)

// ServiceReq declares desired systemd unit state for host requirements.
type ServiceReq struct {
	Name   string `json:"name" yaml:"name"`
	Action string `json:"action" yaml:"action"` // enable | disable | mask
}

// PortReq declares firewall port requirements.
type PortReq struct {
	Port  int    `json:"port" yaml:"port"`
	Proto string `json:"proto" yaml:"proto"` // tcp | udp
}

// HostRequirements declares OS, package, kernel, and security prerequisites for a plugin or extension.
type HostRequirements struct {
	Packages     []string          `json:"packages,omitempty" yaml:"packages,omitempty"`
	KernelMods   []string          `json:"kernelMods,omitempty" yaml:"kernelMods,omitempty"`
	Sysctls      map[string]string `json:"sysctls,omitempty" yaml:"sysctls,omitempty"`
	Services     []ServiceReq      `json:"services,omitempty" yaml:"services,omitempty"`
	Ports        []PortReq         `json:"ports,omitempty" yaml:"ports,omitempty"`
	Profiles     []string          `json:"profiles,omitempty" yaml:"profiles,omitempty"`
	MinKernel    string            `json:"minKernel,omitempty" yaml:"minKernel,omitempty"`
	PageSizes    []int             `json:"pageSizes,omitempty" yaml:"pageSizes,omitempty"`
	SecurityExec []string          `json:"securityExec,omitempty" yaml:"securityExec,omitempty"`
}

// PortClaim reserves a host port+protocol in instance_host_claims.
type PortClaim struct {
	Port  int    `json:"port" yaml:"port"`
	Proto string `json:"proto" yaml:"proto"` // tcp | udp
}

// DiskClaim reserves an exclusive block device on a host.
type DiskClaim struct {
	Device string `json:"device" yaml:"device"`
}

// HostRoleClaims reserves ports, directories, Docker subnets, disks, and memory on a host
// to block collisions at Plan time (D2 §1.3, R2).
type HostRoleClaims struct {
	Role      string      `json:"role,omitempty" yaml:"role,omitempty"`
	Ports     []PortClaim `json:"ports,omitempty" yaml:"ports,omitempty"`
	Dirs      []string    `json:"dirs,omitempty" yaml:"dirs,omitempty"`
	Subnets   []string    `json:"subnets,omitempty" yaml:"subnets,omitempty"`
	Disks     []DiskClaim `json:"disks,omitempty" yaml:"disks,omitempty"`
	MemoryMB  int         `json:"memoryMB,omitempty" yaml:"memoryMB,omitempty"`
	HugePages int         `json:"hugePages,omitempty" yaml:"hugePages,omitempty"`
}

// MediaRef identifies an artifact resolved through the ternary MediaResolver (ADR-077).
type MediaRef struct {
	Product       string `json:"product" yaml:"product"`
	Name          string `json:"name" yaml:"name"`
	Version       string `json:"version" yaml:"version"`
	Arch          string `json:"arch,omitempty" yaml:"arch,omitempty"`
	PageSize      int    `json:"pageSize,omitempty" yaml:"pageSize,omitempty"`
	MatchPageSize bool   `json:"matchPageSize,omitempty" yaml:"matchPageSize,omitempty"`
	ExpectedSHA   string `json:"expectedSha256,omitempty" yaml:"expectedSha256,omitempty"`
}

// ArtifactRef identifies a BOM artifact consumed by binary.install.
type ArtifactRef struct {
	Name          string `json:"name" yaml:"name"`
	MatchPageSize bool   `json:"matchPageSize,omitempty" yaml:"matchPageSize,omitempty"`
	SubPath       string `json:"subPath,omitempty" yaml:"subPath,omitempty"`
}

// TemplateRef points to an embedded or inline template rendered by internal/template.
type TemplateRef struct {
	FS      fs.FS  `json:"-" yaml:"-"`
	Path    string `json:"path,omitempty" yaml:"path,omitempty"`
	Inline  string `json:"inline,omitempty" yaml:"inline,omitempty"`
	Content []byte `json:"content,omitempty" yaml:"content,omitempty"`
}

// MarshalJSON materializes embedded fs.FS template files into Inline so frozen
// Plan & Operation PipelineJSON snapshots remain self-contained across restarts and HA replicas.
func (t TemplateRef) MarshalJSON() ([]byte, error) {
	type alias struct {
		Path    string `json:"path,omitempty"`
		Inline  string `json:"inline,omitempty"`
		Content []byte `json:"content,omitempty"`
	}
	out := alias{Path: t.Path, Inline: t.Inline, Content: t.Content}
	if out.Inline == "" && len(out.Content) == 0 && t.FS != nil && t.Path != "" {
		if raw, err := fs.ReadFile(t.FS, t.Path); err == nil {
			out.Inline = string(raw)
		}
	}
	return json.Marshal(out)
}
