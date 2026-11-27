package osadapter

import (
	"fmt"
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
kv mem_avail_kb "$(awk '/^MemAvailable:/{print $2}' /proc/meminfo 2>/dev/null || awk '/^MemFree:/{print $2}' /proc/meminfo 2>/dev/null || echo 0)"
kv hugepage_kb  "$(awk '/^Hugepagesize:/{print $2}' /proc/meminfo 2>/dev/null || echo 0)"
kv thp_enabled  "$(cat /sys/kernel/mm/transparent_hugepage/enabled 2>/dev/null || echo '')"
kv cpu_cores    "$(nproc 2>/dev/null || grep -c ^processor /proc/cpuinfo 2>/dev/null || echo 1)"
kv cpu_model    "$(awk -F: '/^model name/{gsub(/^[ \t]+/,"",$2); print $2; exit}' /proc/cpuinfo 2>/dev/null || echo '')"
kv uptime_sec   "$(awk '{printf "%d", $1}' /proc/uptime 2>/dev/null || echo 0)"
kv load_avg     "$(awk '{print $1" "$2" "$3}' /proc/loadavg 2>/dev/null || echo '')"
kv docker_ver   "$(docker version --format '{{.Server.Version}}' 2>/dev/null || docker --version 2>/dev/null | awk '{print $3}' | tr -d ',' || echo '')"
kv failed_units "$(systemctl list-units --failed --no-legend --plain 2>/dev/null | awk '{print $1}' | head -n 20 | paste -sd, - 2>/dev/null || echo '')"
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

echo '@BEGIN:SYSCTL'
for k in fs.file-max fs.inotify.max_user_watches fs.inotify.max_user_instances kernel.pid_max net.core.somaxconn net.core.netdev_max_backlog net.ipv4.tcp_max_syn_backlog net.ipv4.tcp_fin_timeout net.ipv4.ip_forward net.bridge.bridge-nf-call-iptables net.netfilter.nf_conntrack_max vm.swappiness vm.overcommit_memory vm.overcommit_ratio vm.max_map_count vm.dirty_background_bytes vm.dirty_bytes vm.zone_reclaim_mode; do
  v="$(sysctl -n "$k" 2>/dev/null || true)"
  if [ -n "$v" ]; then printf '%s=%s\n' "$k" "$v"; fi
done
echo '@END:SYSCTL'

echo '@BEGIN:LISTEN_PORTS'
(ss -lntupH 2>/dev/null || netstat -lntup 2>/dev/null || true) | head -n 200
echo '@END:LISTEN_PORTS'
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
		Hostname:      kv["hostname"],
		FQDN:          kv["fqdn"],
		Kernel:        kv["kernel"],
		Arch:          normalizeArch(kv["arch"]),
		PageSize:      parseIntDefault(kv["page_size"], 4096),
		MachineID:     strings.TrimSpace(kv["machine_id"]),
		SELinuxMode:   strings.TrimSpace(kv["selinux"]),
		CPUCores:      parseIntDefault(kv["cpu_cores"], 1),
		CPUModel:      strings.TrimSpace(kv["cpu_model"]),
		MemMB:         parseIntDefault(kv["mem_kb"], 0) / 1024,
		MemAvailMB:    parseIntDefault(kv["mem_avail_kb"], 0) / 1024,
		SwapMB:        parseIntDefault(kv["swap_kb"], 0) / 1024,
		UptimeSec:     parseInt64Default(kv["uptime_sec"], 0),
		LoadAvg:       strings.TrimSpace(kv["load_avg"]),
		DockerVersion: strings.TrimSpace(kv["docker_ver"]),
		HugePageKB:    parseIntDefault(kv["hugepage_kb"], 0),
		THPEnabled:    strings.TrimSpace(kv["thp_enabled"]),
		NUMANodes:     parseIntDefault(kv["numa_nodes"], 1),
		PrimaryIP4:    strings.TrimSpace(kv["primary_ip4"]),
		PrimaryIP6:    strings.TrimSpace(kv["primary_ip6"]),
		CollectedAt:   time.Now().UTC(),
	}
	if fu := strings.TrimSpace(kv["failed_units"]); fu != "" {
		for _, u := range strings.Split(fu, ",") {
			if u = strings.TrimSpace(u); u != "" {
				f.FailedUnits = append(f.FailedUnits, u)
			}
		}
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

	// Disks, live mounts, live Sysctls, and listening ports (0 extra SSH RTTs)
	f.Disks = parseDisks(sections["DISKS"])
	f.Mounts = parseMounts(sections["MOUNTS"])
	if sysctls := parseKVSection(sections["SYSCTL"]); len(sysctls) > 0 {
		f.Sysctls = sysctls
	}
	f.ListeningPorts = parseListeningPorts(sections["LISTEN_PORTS"])

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

var reSSProc = regexp.MustCompile(`"([^"]+)"`)

// parseListeningPorts parses `ss -lntupH` or `netstat -lntup` output into normalized ListeningPort entries.
func parseListeningPorts(s string) []sdk.ListeningPort {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	seen := make(map[string]bool)
	var out []sdk.ListeningPort
	for _, line := range strings.Split(s, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}
		protoRaw := strings.ToLower(fields[0])
		var proto string
		switch {
		case strings.HasPrefix(protoRaw, "tcp"):
			proto = "tcp"
		case strings.HasPrefix(protoRaw, "udp"):
			proto = "udp"
		default:
			continue
		}
		// ss -lntupH: tcp LISTEN 0 128 0.0.0.0:5432 0.0.0.0:* users:(("postgres",pid=123,fd=5))
		// netstat -lntup: tcp 0 0 0.0.0.0:5432 0.0.0.0:* LISTEN 123/postgres
		localAddr := fields[4]
		if fields[1] == "0" && len(fields) >= 4 {
			localAddr = fields[3]
		}
		idx := strings.LastIndex(localAddr, ":")
		if idx < 0 || idx == len(localAddr)-1 {
			continue
		}
		addr := strings.Trim(localAddr[:idx], "[]")
		port, err := strconv.Atoi(localAddr[idx+1:])
		if err != nil || port <= 0 || port > 65535 {
			continue
		}
		proc := ""
		if m := reSSProc.FindStringSubmatch(line); len(m) > 1 {
			proc = m[1]
		} else if len(fields) >= 7 && strings.Contains(fields[6], "/") {
			_, proc, _ = strings.Cut(fields[6], "/")
		}
		key := fmt.Sprintf("%s|%s|%d", proto, addr, port)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, sdk.ListeningPort{
			Proto:   proto,
			Address: addr,
			Port:    port,
			Process: proc,
		})
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

func parseInt64Default(s string, def int64) int64 {
	v, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return def
	}
	return v
}

func parseMounts(section string) []sdk.MountView {
	var out []sdk.MountView
	for i, line := range strings.Split(section, "\n") {
		fields := strings.Fields(line)
		if i == 0 || len(fields) < 6 {
			continue
		}
		dev := fields[0]
		mp := fields[5]
		if !strings.HasPrefix(dev, "/") && dev != "overlay" {
			continue
		}
		if strings.HasPrefix(mp, "/snap") || strings.HasPrefix(mp, "/run") || strings.HasPrefix(mp, "/sys") || strings.HasPrefix(mp, "/proc") {
			continue
		}
		total, _ := strconv.ParseInt(fields[1], 10, 64)
		used, _ := strconv.ParseInt(fields[2], 10, 64)
		avail, _ := strconv.ParseInt(fields[3], 10, 64)
		pct, _ := strconv.Atoi(strings.TrimSuffix(fields[4], "%"))
		if total <= 0 {
			continue
		}
		out = append(out, sdk.MountView{
			Device:     dev,
			MountPoint: mp,
			TotalBytes: total,
			UsedBytes:  used,
			AvailBytes: avail,
			UsedPct:    pct,
		})
	}
	return out
}
