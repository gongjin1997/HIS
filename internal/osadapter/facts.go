package osadapter

import (
	"net/netip"
	"regexp"
	"strconv"
	"strings"
	"time"

	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
)

// FactsCollectorScript is the 1-RTT POSIX sh Facts probe (zero Python / zero jq, D1 §4.1).
// Uses @BEGIN:<SECTION> / @END:<SECTION> blocks so special characters can never break serialization.
const FactsCollectorScript = `#!/bin/sh
export LC_ALL=C PATH="/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"

kv() { printf '%s=%s\n' "$1" "$2"; }

echo '@BEGIN:KV'
kv hostname     "$(hostname 2>/dev/null || uname -n)"
kv fqdn         "$(hostname -f 2>/dev/null || hostname 2>/dev/null || uname -n)"
kv kernel       "$(uname -r)"
kv arch         "$(uname -m)"
kv page_size    "$(getconf PAGESIZE 2>/dev/null || echo 4096)"
kv glibc        "$(getconf GNU_LIBC_VERSION 2>/dev/null || (ldd --version 2>&1 | head -n1))"
kv machine_id   "$(cat /etc/machine-id 2>/dev/null || cat /var/lib/dbus/machine-id 2>/dev/null || echo '')"
kv cgroup_fs    "$(stat -fc %T /sys/fs/cgroup 2>/dev/null || echo unknown)"
kv systemd_ver  "$(systemd --version 2>/dev/null | head -n1 || systemctl --version 2>/dev/null | head -n1 || echo '')"
kv nft_ver      "$(nft --version 2>/dev/null | head -n1 || echo '')"
kv sudo_ver     "$(sudo --version 2>/dev/null | head -n1 || echo '')"
kv selinux      "$(getenforce 2>/dev/null || echo Disabled)"
kv apparmor     "$(cat /sys/module/apparmor/parameters/enabled 2>/dev/null || echo N)"
kv kysec        "$(command -v getstatus >/dev/null 2>&1 && getstatus 2>/dev/null | tr '\n' ';' || echo '')"
kv fapolicyd    "$(systemctl is-active fapolicyd 2>/dev/null || echo inactive)"
kv swap_kb      "$(awk '/^SwapTotal:/{print $2}' /proc/meminfo 2>/dev/null || echo 0)"
kv mem_kb       "$(awk '/^MemTotal:/{print $2}' /proc/meminfo 2>/dev/null || echo 0)"
kv hugepage_kb  "$(awk '/^Hugepagesize:/{print $2}' /proc/meminfo 2>/dev/null || echo 0)"
kv cpu_cores    "$(nproc 2>/dev/null || grep -c ^processor /proc/cpuinfo 2>/dev/null || echo 1)"
kv numa_nodes   "$(ls -d /sys/devices/system/node/node[0-9]* 2>/dev/null | wc -l | tr -d ' ')"
kv x86_flags    "$(awk -F: '/^flags/{print $2; exit}' /proc/cpuinfo 2>/dev/null | tr -s ' ')"
kv default_rt   "$(ip -o route show default 2>/dev/null | head -n1 || echo '')"
kv primary_ip4  "$(ip -4 -o route get 1.1.1.1 2>/dev/null | awk '{for(i=1;i<=NF;i++)if($i=="src"){print $(i+1);exit}}')"
kv primary_ip6  "$(ip -6 -o route get 2606:4700:4700::1111 2>/dev/null | awk '{for(i=1;i<=NF;i++)if($i=="src"){print $(i+1);exit}}')"
echo '@END:KV'

echo '@BEGIN:OS_RELEASE'
cat /etc/os-release 2>/dev/null || true
echo '@END:OS_RELEASE'

echo '@BEGIN:KYINFO'
cat /etc/.kyinfo 2>/dev/null || true
echo '@END:KYINFO'

echo '@BEGIN:NICS'
ip -o addr show 2>/dev/null || true
echo '@END:NICS'

echo '@BEGIN:DISKS'
lsblk -P -b -o NAME,TYPE,SIZE,ROTA,FSTYPE,MOUNTPOINT,WWN,SERIAL 2>/dev/null || true
echo '@END:DISKS'

echo '@BEGIN:MOUNTS'
df -P -B1 2>/dev/null || true
echo '@END:MOUNTS'
`

var (
	reVerNum   = regexp.MustCompile(`(\d+\.\d+(?:\.\d+)?)`)
	reFirstInt = regexp.MustCompile(`(\d+)`)
	reLSBLK    = regexp.MustCompile(`([A-Z]+)="([^"]*)"`)
)

// ParseFactsOutput converts the 1-RTT POSIX script stdout into a normalized sdk.FactsView (D1 §4.1).
func ParseFactsOutput(raw string, fallbackAddr string, engine *Engine) (*sdk.FactsView, error) {
	sections := splitSections(raw)
	kv := parseKVSection(sections["KV"])

	f := &sdk.FactsView{
		Hostname:    kv["hostname"],
		FQDN:        kv["fqdn"],
		Kernel:      kv["kernel"],
		Arch:        normalizeArch(kv["arch"]),
		PageSize:    parseIntDefault(kv["page_size"], 4096),
		MachineID:   strings.TrimSpace(kv["machine_id"]),
		SELinuxMode: strings.TrimSpace(kv["selinux"]),
		CPUCores:    parseIntDefault(kv["cpu_cores"], 1),
		MemMB:       parseIntDefault(kv["mem_kb"], 0) / 1024,
		SwapMB:      parseIntDefault(kv["swap_kb"], 0) / 1024,
		HugePageKB:  parseIntDefault(kv["hugepage_kb"], 0),
		NUMANodes:   parseIntDefault(kv["numa_nodes"], 1),
		PrimaryIP4:  strings.TrimSpace(kv["primary_ip4"]),
		PrimaryIP6:  strings.TrimSpace(kv["primary_ip6"]),
		CollectedAt: time.Now().UTC(),
	}

	// Cgroup version
	if strings.Contains(kv["cgroup_fs"], "cgroup2") {
		f.CgroupVer = 2
	} else {
		f.CgroupVer = 1
	}

	// glibc / musl parsing
	glibcRaw := strings.ToLower(kv["glibc"])
	if strings.Contains(glibcRaw, "musl") {
		f.Libc = "musl"
	} else {
		f.Libc = "glibc"
		if m := reVerNum.FindStringSubmatch(kv["glibc"]); m != nil {
			f.GlibcVersion = m[1]
		}
	}

	// systemd / nft / sudo-rs
	if m := reFirstInt.FindStringSubmatch(kv["systemd_ver"]); m != nil {
		f.SystemdVer, _ = strconv.Atoi(m[1])
	}
	if m := reVerNum.FindStringSubmatch(kv["nft_ver"]); m != nil {
		f.NFTVersion = m[1]
	}
	if strings.Contains(strings.ToLower(kv["sudo_ver"]), "sudo-rs") {
		f.SudoImpl = "sudo-rs"
	} else if kv["sudo_ver"] != "" {
		f.SudoImpl = "sudo"
	}

	// Security modules
	if strings.EqualFold(f.SELinuxMode, "Enforcing") || strings.EqualFold(f.SELinuxMode, "Permissive") {
		f.SecurityMod = append(f.SecurityMod, "selinux")
	}
	if strings.EqualFold(strings.TrimSpace(kv["apparmor"]), "Y") {
		f.SecurityMod = append(f.SecurityMod, "apparmor")
	}
	if strings.TrimSpace(kv["kysec"]) != "" {
		f.SecurityMod = append(f.SecurityMod, "kysec")
	}
	if strings.EqualFold(strings.TrimSpace(kv["fapolicyd"]), "active") {
		f.SecurityMod = append(f.SecurityMod, "fapolicyd")
	}

	// x86-64 microarchitecture level (D1 §4.1)
	if f.Arch == "amd64" {
		f.X86Level = detectX86Level(kv["x86_flags"])
	}

	// NICs and offline primary IP fallback
	f.NICs = parseNICs(sections["NICS"])
	if f.PrimaryIP4 == "" {
		f.PrimaryIP4 = selectFallbackIPv4(f.NICs, fallbackAddr)
	}

	// Disks
	f.Disks = parseDisks(sections["DISKS"])

	// OS match via spec/osmatch.yaml
	osRel := ParseOSRelease(sections["OS_RELEASE"])
	files := map[string]string{}
	if ky := strings.TrimSpace(sections["KYINFO"]); ky != "" {
		files["/etc/.kyinfo"] = sections["KYINFO"]
	}
	f.OS = sdk.OSInfo{
		ID:         osRel["ID"],
		VersionID:  osRel["VERSION_ID"],
		PrettyName: osRel["PRETTY_NAME"],
	}
	if engine != nil {
		res, err := engine.Match(MatchInput{OSRelease: osRel, Files: files})
		if err != nil {
			return nil, err
		}
		if res != nil && res.Rule != nil {
			f.OS.RuleID = res.Rule.ID
			f.OS.Family = res.Rule.Family
			f.OS.Parent = res.Rule.Parent
			f.OS.Tier = res.Rule.Support.Tier
			f.OS.Variants = res.Variants
		}
	}
	return f, nil
}

// FindDuplicateMachineIDs detects cloned VM hosts sharing the same /etc/machine-id (D1 §4.1).
func FindDuplicateMachineIDs(hosts []sdk.HostView) map[string][]string {
	byMID := make(map[string][]string)
	for _, h := range hosts {
		mid := strings.TrimSpace(h.Facts.MachineID)
		if mid == "" {
			continue
		}
		byMID[mid] = append(byMID[mid], h.Name)
	}
	dups := make(map[string][]string)
	for mid, names := range byMID {
		if len(names) > 1 {
			dups[mid] = names
		}
	}
	return dups
}

func splitSections(raw string) map[string]string {
	out := make(map[string]string)
	var cur string
	var buf strings.Builder
	for _, line := range strings.Split(raw, "\n") {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "@BEGIN:") {
			cur = strings.TrimPrefix(trim, "@BEGIN:")
			buf.Reset()
			continue
		}
		if strings.HasPrefix(trim, "@END:") && cur != "" {
			out[cur] = buf.String()
			cur = ""
			buf.Reset()
			continue
		}
		if cur != "" {
			buf.WriteString(line)
			buf.WriteByte('\n')
		}
	}
	return out
}

func parseKVSection(s string) map[string]string {
	out := make(map[string]string)
	for _, line := range strings.Split(s, "\n") {
		if line == "" {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if ok {
			out[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	return out
}

func normalizeArch(a string) string {
	switch strings.ToLower(strings.TrimSpace(a)) {
	case "x86_64", "amd64":
		return "amd64"
	case "aarch64", "arm64":
		return "arm64"
	case "loongarch64", "loong64":
		return "loong64"
	default:
		return strings.TrimSpace(a)
	}
}

func detectX86Level(flagsLine string) string {
	set := make(map[string]bool)
	for _, tok := range strings.Fields(strings.ToLower(flagsLine)) {
		set[tok] = true
	}
	v3 := []string{"avx2", "bmi1", "bmi2", "fma", "movbe", "f16c"}
	allV3 := true
	for _, f := range v3 {
		if !set[f] {
			allV3 = false
			break
		}
	}
	if allV3 {
		return "x86-64-v3"
	}
	v2 := []string{"cx16", "popcnt", "sse4_2", "ssse3"}
	allV2 := true
	for _, f := range v2 {
		if !set[f] {
			allV2 = false
			break
		}
	}
	if allV2 {
		return "x86-64-v2"
	}
	return "x86-64-v1"
}

var virtualNICPrefixes = []string{"lo", "docker", "br-", "veth", "cali", "flannel", "cilium", "cni", "kube-ipvs", "virbr", "tunl"}

func isVirtualNIC(name string) bool {
	for _, p := range virtualNICPrefixes {
		if name == p || strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

func parseNICs(section string) []sdk.NICView {
	byName := make(map[string]*sdk.NICView)
	var order []string
	for _, line := range strings.Split(section, "\n") {
		fields := strings.Fields(line)
		// Format: "2: eth0    inet 10.0.1.11/24 ..."
		if len(fields) < 4 {
			continue
		}
		iface := strings.TrimSuffix(fields[1], ":")
		family := fields[2]
		if family != "inet" && family != "inet6" {
			continue
		}
		addr := fields[3]
		nic, ok := byName[iface]
		if !ok {
			nic = &sdk.NICView{Name: iface, IsVirtual: isVirtualNIC(iface)}
			byName[iface] = nic
			order = append(order, iface)
		}
		nic.Addresses = append(nic.Addresses, addr)
	}
	out := make([]sdk.NICView, 0, len(order))
	for _, name := range order {
		out = append(out, *byName[name])
	}
	return out
}

func selectFallbackIPv4(nics []sdk.NICView, sshAddr string) string {
	targetIP, targetErr := netip.ParseAddr(sshAddr)
	var firstNonVirtual string
	for _, nic := range nics {
		if nic.IsVirtual {
			continue
		}
		for _, cidr := range nic.Addresses {
			pfx, err := netip.ParsePrefix(cidr)
			if err != nil || !pfx.Addr().Is4() {
				continue
			}
			if firstNonVirtual == "" {
				firstNonVirtual = pfx.Addr().String()
			}
			if targetErr == nil && pfx.Contains(targetIP) {
				return pfx.Addr().String()
			}
		}
	}
	if firstNonVirtual != "" {
		return firstNonVirtual
	}
	return sshAddr
}

func parseDisks(section string) []sdk.DiskView {
	var out []sdk.DiskView
	for _, line := range strings.Split(section, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		m := make(map[string]string)
		for _, pair := range reLSBLK.FindAllStringSubmatch(line, -1) {
			m[pair[1]] = pair[2]
		}
		if m["NAME"] == "" {
			continue
		}
		sz, _ := strconv.ParseInt(m["SIZE"], 10, 64)
		out = append(out, sdk.DiskView{
			Name:       m["NAME"],
			Type:       m["TYPE"],
			SizeBytes:  sz,
			Rotational: m["ROTA"] == "1",
			FSType:     m["FSTYPE"],
			MountPoint: m["MOUNTPOINT"],
			WWN:        m["WWN"],
			Serial:     m["SERIAL"],
		})
	}
	return out
}

func parseIntDefault(s string, def int) int {
	v, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return def
	}
	return v
}
