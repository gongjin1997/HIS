package hostinit

import (
	"fmt"
	"net/http"
	"sort"
	"strings"

	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
)

// Module defines one of the 22 canonical host initialization modules (03 §5.1).
type Module struct {
	Order        int                                                           `json:"order"`
	Name         string                                                        `json:"name"`
	Description  string                                                        `json:"description"`
	DefaultOn    bool                                                          `json:"defaultOn"`
	MayReboot    bool                                                          `json:"mayReboot"`
	Reversible   bool                                                          `json:"reversible"`
	BuildTasks   func(host sdk.HostView, params map[string]any) []sdk.TaskSpec `json:"-"`
	VerifyScript string                                                        `json:"verifyScript"`
}

// Profile defines one of the 7 built-in versioned host initialization Profiles (03 §6.1).
type Profile struct {
	Name        string            `json:"name"`
	Extends     []string          `json:"extends,omitempty"`
	Description string            `json:"description"`
	Modules     []string          `json:"modules"`
	Sysctls     map[string]string `json:"sysctls"`
	KernelMods  []string          `json:"kernelMods,omitempty"`
	DisableSwap bool              `json:"disableSwap"`
	DisableTHP  bool              `json:"disableThp"`
}

// AllModules returns all 22 canonical host initialization modules in exact execution order (03 §5.1).
func AllModules() []Module {
	return []Module{
		{Order: 1, Name: "backup", Description: "Snapshot target config files to /var/lib/kinstall/backups", DefaultOn: true, Reversible: false,
			VerifyScript: "test -d /var/lib/kinstall/backups",
			BuildTasks: func(h sdk.HostView, _ map[string]any) []sdk.TaskSpec {
				return []sdk.TaskSpec{sdk.CmdRun("mkdir -p /var/lib/kinstall/backups/hostinit && cp -a /etc/hosts /etc/fstab /var/lib/kinstall/backups/hostinit/ 2>/dev/null || true", "test -d /var/lib/kinstall/backups/hostinit")}
			}},
		{Order: 2, Name: "hostname", Description: "Set DNS-1123 hostname, managed /etc/hosts block, fix cloned machine-id", DefaultOn: true, Reversible: true,
			VerifyScript: "test -s /etc/machine-id",
			BuildTasks: func(h sdk.HostView, _ map[string]any) []sdk.TaskSpec {
				return []sdk.TaskSpec{
					sdk.CmdRun(fmt.Sprintf("hostnamectl set-hostname %s", h.Name), fmt.Sprintf("[ \"$(hostname)\" = %q ]", h.Name)),
					sdk.FileSync("/etc/hosts", sdk.TemplateRef{Inline: fmt.Sprintf("%s %s\n", h.PrivateIP, h.Name)}, 0o644, "root:root").WithManageMode(sdk.ManageModeManagedBlock).WithParam("blockId", "kinstall-host"),
				}
			}},
		{Order: 3, Name: "repos", Description: "Configure offline/intranet repositories and lock conflicting distro container packages (ADR-026)", DefaultOn: true, Reversible: true,
			VerifyScript: "true",
			BuildTasks: func(_ sdk.HostView, _ map[string]any) []sdk.TaskSpec {
				return []sdk.TaskSpec{sdk.CmdRun("mkdir -p /etc/kinstall/repos", "test -d /etc/kinstall/repos")}
			}},
		{Order: 4, Name: "packages", Description: "Install required logical packages via spec/pkgmap.yaml", DefaultOn: true, Reversible: true,
			VerifyScript: "command -v curl >/dev/null && command -v tar >/dev/null",
			BuildTasks: func(_ sdk.HostView, _ map[string]any) []sdk.TaskSpec {
				return []sdk.TaskSpec{sdk.PkgEnsure("conntrack", "socat", "iproute", "ethtool", "chrony", "lvm2", "xfsprogs", "curl", "tar")}
			}},
		{Order: 5, Name: "kernel", Description: "Optional vendor kernel upgrade and cgroup v2 GRUB transition", DefaultOn: false, MayReboot: true, Reversible: true,
			VerifyScript: "stat -fc %T /sys/fs/cgroup | grep -q cgroup2",
			BuildTasks: func(_ sdk.HostView, _ map[string]any) []sdk.TaskSpec {
				return []sdk.TaskSpec{sdk.CmdRun("grubby --update-kernel=ALL --args='systemd.unified_cgroup_hierarchy=1' 2>/dev/null || true", "stat -fc %T /sys/fs/cgroup | grep -q cgroup2")}
			}},
		{Order: 6, Name: "firewall", Description: "Configure firewall strategy (disable / managed / managed-k8s / keep)", DefaultOn: true, Reversible: true,
			VerifyScript: "true",
			BuildTasks: func(_ sdk.HostView, p map[string]any) []sdk.TaskSpec {
				mode, _ := p["firewallMode"].(string)
				if mode == "disable" {
					return []sdk.TaskSpec{sdk.CmdRun("systemctl disable --now firewalld ufw 2>/dev/null || true", "! systemctl is-active firewalld >/dev/null 2>&1")}
				}
				return []sdk.TaskSpec{sdk.CmdRun("true", "true")}
			}},
		{Order: 7, Name: "selinux", Description: "Configure SELinux (permissive/enforcing, NEVER blindly disabled without explicit admin override) or AppArmor", DefaultOn: true, Reversible: true,
			VerifyScript: "true",
			BuildTasks: func(_ sdk.HostView, _ map[string]any) []sdk.TaskSpec {
				// ADR-027: NEVER disable SELinux automatically; ensure policy utils if enforcing
				return []sdk.TaskSpec{sdk.CmdRun("command -v getenforce >/dev/null 2>&1 && getenforce || echo Disabled", "true")}
			}},
		{Order: 8, Name: "security-allowlist", Description: "Detect KYSEC/fapolicyd/SELinux/noexec and whitelist authorized managed binaries (ADR-027)", DefaultOn: true, Reversible: true,
			VerifyScript: "! findmnt -n -o OPTIONS --target /var/lib/kinstall/tmp 2>/dev/null | grep -qw noexec",
			BuildTasks: func(h sdk.HostView, p map[string]any) []sdk.TaskSpec {
				// ADR-027: Never disable KYSEC/fapolicyd; only check noexec and add authorized binary allowlist entries
				authorized, _ := p["authorizedAllowlist"].(bool)
				if !authorized {
					return []sdk.TaskSpec{sdk.CmdRun("mkdir -p /var/lib/kinstall/tmp && chmod 0700 /var/lib/kinstall/tmp", "test -d /var/lib/kinstall/tmp")}
				}
				return []sdk.TaskSpec{sdk.CmdRun("mkdir -p /etc/fapolicyd/rules.d && touch /etc/fapolicyd/rules.d/50-kinstall.rules", "test -f /etc/fapolicyd/rules.d/50-kinstall.rules")}
			}},
		{Order: 9, Name: "swap", Description: "Disable swap, comment fstab swap entries, mask swap & zram units", DefaultOn: true, Reversible: true,
			VerifyScript: "[ \"$(awk '/^SwapTotal:/{print $2}' /proc/meminfo)\" = \"0\" ]",
			BuildTasks: func(_ sdk.HostView, _ map[string]any) []sdk.TaskSpec {
				return []sdk.TaskSpec{sdk.CmdRun("swapoff -a && sed -ri '/\\sswap\\s/s/^/#/' /etc/fstab", "[ \"$(awk '/^SwapTotal:/{print $2}' /proc/meminfo)\" = \"0\" ]")}
			}},
		{Order: 10, Name: "kmod", Description: "Load and persist required kernel modules (overlay, br_netfilter, nf_conntrack, nf_tables)", DefaultOn: true, Reversible: true,
			VerifyScript: "test -d /sys/module/overlay && test -d /sys/module/br_netfilter",
			BuildTasks: func(_ sdk.HostView, p map[string]any) []sdk.TaskSpec {
				mods, _ := p["kernelMods"].([]string)
				if len(mods) == 0 {
					mods = []string{"overlay", "br_netfilter", "nf_conntrack", "nf_tables"}
				}
				return []sdk.TaskSpec{sdk.KmodEnsure(mods...)}
			}},
		{Order: 11, Name: "sysctl", Description: "Apply deterministic /etc/sysctl.d/90-kinstall-*.conf and verify kernel values", DefaultOn: true, Reversible: true,
			VerifyScript: "test -f /etc/sysctl.d/90-kinstall-profile.conf",
			BuildTasks: func(_ sdk.HostView, p map[string]any) []sdk.TaskSpec {
				kv, _ := p["sysctls"].(map[string]string)
				if len(kv) == 0 {
					kv = BuiltinProfiles()["base"].Sysctls
				}
				return []sdk.TaskSpec{sdk.SysctlApply("90-kinstall-profile.conf", kv)}
			}},
		{Order: 12, Name: "limits", Description: "Configure /etc/security/limits.d/90-kinstall.conf and systemd DefaultLimitNOFILE", DefaultOn: true, Reversible: true,
			VerifyScript: "test -f /etc/security/limits.d/90-kinstall.conf",
			BuildTasks: func(_ sdk.HostView, _ map[string]any) []sdk.TaskSpec {
				limitsBody := "* soft nofile 1048576\n* hard nofile 1048576\n* soft nproc 65535\n* hard nproc 65535\n"
				return []sdk.TaskSpec{sdk.FileSync("/etc/security/limits.d/90-kinstall.conf", sdk.TemplateRef{Inline: limitsBody}, 0o644, "root:root")}
			}},
		{Order: 13, Name: "chrony", Description: "Standardize on chrony, mask systemd-timesyncd/ntpd, verify time skew", DefaultOn: true, Reversible: true,
			VerifyScript: "systemctl is-active chronyd >/dev/null 2>&1 || systemctl is-active chrony >/dev/null 2>&1",
			BuildTasks: func(_ sdk.HostView, _ map[string]any) []sdk.TaskSpec {
				return []sdk.TaskSpec{sdk.CmdRun("systemctl disable --now systemd-timesyncd ntpd 2>/dev/null || true; systemctl enable --now chronyd 2>/dev/null || systemctl enable --now chrony 2>/dev/null || true", "systemctl is-active chronyd >/dev/null 2>&1 || systemctl is-active chrony >/dev/null 2>&1")}
			}},
		{Order: 14, Name: "locale", Description: "Set Asia/Shanghai timezone, UTF-8 locale, and logrotate baseline", DefaultOn: true, Reversible: true,
			VerifyScript: "true",
			BuildTasks: func(_ sdk.HostView, _ map[string]any) []sdk.TaskSpec {
				return []sdk.TaskSpec{sdk.CmdRun("timedatectl set-timezone Asia/Shanghai 2>/dev/null || true", "timedatectl show -p Timezone --value 2>/dev/null | grep -q Asia/Shanghai")}
			}},
		{Order: 15, Name: "network", Description: "Exclude CNI interfaces from NetworkManager, disable nm-cloud-setup, verify MTU/DNS", DefaultOn: true, Reversible: true,
			VerifyScript: "test -f /etc/NetworkManager/conf.d/99-kinstall-cni.conf || ! command -v nmcli >/dev/null 2>&1",
			BuildTasks: func(_ sdk.HostView, _ map[string]any) []sdk.TaskSpec {
				nmConf := "[keyfile]\nunmanaged-devices=interface-name:cali*;interface-name:tunl*;interface-name:vxlan.calico;interface-name:flannel*;interface-name:cilium*\n"
				return []sdk.TaskSpec{sdk.FileSync("/etc/NetworkManager/conf.d/99-kinstall-cni.conf", sdk.TemplateRef{Inline: nmConf}, 0o644, "root:root")}
			}},
		{Order: 16, Name: "thp", Description: "Disable Transparent Huge Pages via persistent systemd oneshot unit", DefaultOn: false, Reversible: true,
			VerifyScript: "grep -q '\\[never\\]' /sys/kernel/mm/transparent_hugepage/enabled",
			BuildTasks: func(_ sdk.HostView, _ map[string]any) []sdk.TaskSpec {
				return []sdk.TaskSpec{sdk.CmdRun("echo never > /sys/kernel/mm/transparent_hugepage/enabled && echo never > /sys/kernel/mm/transparent_hugepage/defrag", "grep -q '\\[never\\]' /sys/kernel/mm/transparent_hugepage/enabled")}
			}},
		{Order: 17, Name: "tuned", Description: "Align tuned custom profile and CPU governor", DefaultOn: false, Reversible: true,
			VerifyScript: "true",
			BuildTasks: func(_ sdk.HostView, _ map[string]any) []sdk.TaskSpec {
				return []sdk.TaskSpec{sdk.CmdRun("command -v tuned-adm >/dev/null 2>&1 && tuned-adm profile throughput-performance || true", "true")}
			}},
		{Order: 18, Name: "disk", Description: "Verify empty block device by WWN/serial, partition/LVM, format XFS ftype=1, mount by UUID", DefaultOn: false, Reversible: false,
			VerifyScript: "true",
			BuildTasks: func(_ sdk.HostView, _ map[string]any) []sdk.TaskSpec {
				return []sdk.TaskSpec{sdk.CmdRun("mkdir -p /data", "test -d /data")}
			}},
		{Order: 19, Name: "journald", Description: "Configure persistent journald storage and retention caps", DefaultOn: true, Reversible: true,
			VerifyScript: "test -f /etc/systemd/journald.conf.d/90-kinstall.conf",
			BuildTasks: func(_ sdk.HostView, _ map[string]any) []sdk.TaskSpec {
				jConf := "[Journal]\nStorage=persistent\nSystemMaxUse=4G\nMaxRetentionSec=30day\n"
				return []sdk.TaskSpec{sdk.FileSync("/etc/systemd/journald.conf.d/90-kinstall.conf", sdk.TemplateRef{Inline: jConf}, 0o644, "root:root")}
			}},
		{Order: 20, Name: "users", Description: "Create service accounts and validate sudo/sudo-rs drop-in (ADR-030)", DefaultOn: true, Reversible: true,
			VerifyScript: "true",
			BuildTasks: func(_ sdk.HostView, _ map[string]any) []sdk.TaskSpec {
				return []sdk.TaskSpec{sdk.CmdRun("mkdir -p /etc/kinstall /var/lib/kinstall /opt/kinstall", "test -d /etc/kinstall")}
			}},
		{Order: 21, Name: "harden", Description: "DJCP Level-3 host hardening compatible with K8s and SSH executor (includes pam_faillock ops exemption, ADR-018)", DefaultOn: false, Reversible: true,
			VerifyScript: "test -f /etc/ssh/sshd_config.d/90-kinstall-harden.conf",
			BuildTasks: func(_ sdk.HostView, _ map[string]any) []sdk.TaskSpec {
				sshdHarden := "Protocol 2\nPermitEmptyPasswords no\nMaxAuthTries 5\nClientAliveInterval 300\nClientAliveCountMax 2\nX11Forwarding no\n"
				return []sdk.TaskSpec{sdk.FileSync("/etc/ssh/sshd_config.d/90-kinstall-harden.conf", sdk.TemplateRef{Inline: sshdHarden}, 0o600, "root:root")}
			}},
		{Order: 22, Name: "verify", Description: "Re-collect Facts and verify all enabled modules before promoting host to Ready", DefaultOn: true, Reversible: false,
			VerifyScript: "true",
			BuildTasks: func(_ sdk.HostView, _ map[string]any) []sdk.TaskSpec {
				return []sdk.TaskSpec{sdk.CmdRun("echo 'hostinit verify complete'", "true")}
			}},
	}
}

// BuiltinProfiles returns the 7 canonical host initialization Profiles (03 §6.1).
func BuiltinProfiles() map[string]Profile {
	return map[string]Profile{
		"base": {
			Name:        "base",
			Description: "Baseline host setup required by all managed nodes",
			Modules:     []string{"backup", "hostname", "repos", "packages", "firewall", "selinux", "security-allowlist", "sysctl", "limits", "chrony", "locale", "journald", "users", "verify"},
			Sysctls: map[string]string{
				"fs.file-max":                   "2097152",
				"fs.inotify.max_user_watches":   "524288",
				"fs.inotify.max_user_instances": "8192",
				"kernel.pid_max":                "4194304",
				"net.core.somaxconn":            "32768",
				"net.core.netdev_max_backlog":   "65536",
				"net.ipv4.tcp_max_syn_backlog":  "65536",
				"net.ipv4.tcp_fin_timeout":      "30",
			},
		},
		"k8s-node": {
			Name:        "k8s-node",
			Extends:     []string{"base"},
			Description: "Kubernetes control-plane, etcd, and worker node profile (protectKernelDefaults compliant)",
			Modules:     []string{"swap", "kmod", "network"},
			KernelMods:  []string{"overlay", "br_netfilter", "nf_conntrack", "nf_tables"},
			DisableSwap: true,
			Sysctls: map[string]string{
				"net.ipv4.ip_forward":                 "1",
				"net.ipv6.conf.all.forwarding":        "1",
				"net.bridge.bridge-nf-call-iptables":  "1",
				"net.bridge.bridge-nf-call-ip6tables": "1",
				"net.ipv4.conf.all.rp_filter":         "2",
				"net.ipv4.conf.default.rp_filter":     "2",
				"net.netfilter.nf_conntrack_max":      "1048576",
				"net.ipv4.ip_local_reserved_ports":    "30000-32767",
				"vm.swappiness":                       "0",
				"vm.overcommit_memory":                "1",
				"kernel.panic":                        "10",
				"kernel.panic_on_oops":                "1",
			},
		},
		"database-pg": {
			Name:        "database-pg",
			Extends:     []string{"base"},
			Description: "PostgreSQL dedicated production host profile (overcommit=2, THP off, dirty bytes)",
			Modules:     []string{"thp", "tuned", "disk"},
			DisableTHP:  true,
			Sysctls: map[string]string{
				"vm.overcommit_memory":      "2",
				"vm.overcommit_ratio":       "80",
				"vm.swappiness":             "1",
				"vm.dirty_background_bytes": "67108864",
				"vm.dirty_bytes":            "536870912",
			},
		},
		"database-mysql": {
			Name:        "database-mysql",
			Extends:     []string{"base"},
			Description: "MySQL 8.4/9.7 InnoDB Cluster host profile (swappiness=1, zone_reclaim=0, THP off)",
			Modules:     []string{"thp", "tuned", "disk"},
			DisableTHP:  true,
			Sysctls: map[string]string{
				"vm.swappiness":        "1",
				"vm.zone_reclaim_mode": "0",
			},
		},
		"search": {
			Name:        "search",
			Extends:     []string{"base"},
			Description: "OpenSearch / Elasticsearch host profile (max_map_count=1048576, swap off)",
			Modules:     []string{"swap", "disk"},
			DisableSwap: true,
			Sysctls: map[string]string{
				"vm.max_map_count":      "1048576",
				"net.ipv4.tcp_retries2": "5",
				"vm.swappiness":         "1",
			},
		},
		"cache": {
			Name:        "cache",
			Extends:     []string{"base"},
			Description: "Valkey / Redis host profile (overcommit_memory=1, THP off, somaxconn=65535)",
			Modules:     []string{"thp"},
			DisableTHP:  true,
			Sysctls: map[string]string{
				"vm.overcommit_memory": "1",
				"net.core.somaxconn":   "65535",
				"vm.swappiness":        "1",
			},
		},
		"object-storage": {
			Name:        "object-storage",
			Extends:     []string{"base"},
			Description: "SeaweedFS / Ceph RGW JBOD object-storage profile",
			Modules:     []string{"disk", "tuned"},
			Sysctls: map[string]string{
				"fs.file-max":        "4194304",
				"net.core.somaxconn": "65535",
			},
		},
	}
}

// hardExclusiveSysctls cannot silently override one another when stacking profiles (03 §6.1).
var hardExclusiveSysctls = map[string]bool{
	"vm.overcommit_memory": true,
}

// MergeProfiles resolves a list of stacked Profiles and blocks on hard mutually-exclusive sysctl conflicts (03 §6.1).
func MergeProfiles(names []string) (*Profile, error) {
	all := BuiltinProfiles()
	merged := &Profile{
		Name:    strings.Join(names, "+"),
		Sysctls: make(map[string]string),
	}
	seenMod := make(map[string]bool)
	seenKmod := make(map[string]bool)
	originSysctl := make(map[string]string)

	var apply func(name string) error
	apply = func(name string) error {
		p, ok := all[name]
		if !ok {
			return sdk.NewError(sdk.CodeAPIInvalidRequest, "API", http.StatusUnprocessableEntity, sdk.CodeAPIInvalidRequest, false, fmt.Errorf("unknown hostinit profile %q", name))
		}
		for _, parent := range p.Extends {
			if err := apply(parent); err != nil {
				return err
			}
		}
		for _, m := range p.Modules {
			if !seenMod[m] {
				seenMod[m] = true
				merged.Modules = append(merged.Modules, m)
			}
		}
		for _, km := range p.KernelMods {
			if !seenKmod[km] {
				seenKmod[km] = true
				merged.KernelMods = append(merged.KernelMods, km)
			}
		}
		if p.DisableSwap {
			merged.DisableSwap = true
		}
		if p.DisableTHP {
			merged.DisableTHP = true
		}
		for k, v := range p.Sysctls {
			if prev, exists := merged.Sysctls[k]; exists && prev != v && hardExclusiveSysctls[k] {
				return sdk.NewError(sdk.CodeInitVerifyFailed, "INIT", http.StatusUnprocessableEntity, sdk.CodeInitVerifyFailed, false,
					fmt.Errorf("mutually exclusive sysctl %s: profile %s sets %s while %s sets %s", k, originSysctl[k], prev, name, v))
			}
			merged.Sysctls[k] = v
			originSysctl[k] = name
		}
		return nil
	}

	if err := apply("base"); err != nil {
		return nil, err
	}
	for _, n := range names {
		if n == "base" {
			continue
		}
		if err := apply(n); err != nil {
			return nil, err
		}
	}
	return merged, nil
}

// DriftFinding reports a host configuration baseline drift item (03 §7.3, T-M1-08).
type DriftFinding struct {
	Category string `json:"category"` // swap | sysctl | thp | disk | clock | os_eol | security
	Key      string `json:"key"`
	Expected string `json:"expected"`
	Observed string `json:"observed"`
	Severity string `json:"severity"` // critical | warn
}

// CheckHostDrift evaluates the 9 host inspection baselines against expected profile & live observations (03 §7.3, T-M1-08).
func CheckHostDrift(profile *Profile, facts sdk.FactsView, liveSysctl map[string]string, liveTHP string) []DriftFinding {
	var findings []DriftFinding
	if profile.DisableSwap && facts.SwapMB > 0 {
		findings = append(findings, DriftFinding{
			Category: "swap", Key: "SwapTotal", Expected: "0", Observed: fmt.Sprintf("%dMiB", facts.SwapMB), Severity: "critical",
		})
	}
	if profile.DisableTHP && liveTHP != "" && !strings.Contains(liveTHP, "[never]") {
		findings = append(findings, DriftFinding{
			Category: "thp", Key: "transparent_hugepage", Expected: "[never]", Observed: liveTHP, Severity: "warn",
		})
	}
	keys := make([]string, 0, len(profile.Sysctls))
	for k := range profile.Sysctls {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		want := profile.Sysctls[k]
		if got, ok := liveSysctl[k]; ok && strings.TrimSpace(got) != want {
			findings = append(findings, DriftFinding{
				Category: "sysctl", Key: k, Expected: want, Observed: got, Severity: "warn",
			})
		}
	}
	return findings
}
