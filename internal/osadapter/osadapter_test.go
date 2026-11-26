package osadapter_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"go.yaml.in/yaml/v3"

	"github.com/kinstall/kinstall/internal/osadapter"
	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
	"github.com/kinstall/kinstall/spec"
)

type goldenFile struct {
	Measured map[string]map[string]string `yaml:"measured"`
	Cases    []struct {
		Name      string            `yaml:"name"`
		Kind      string            `yaml:"kind"`
		Expect    *string           `yaml:"expect"`
		Variants  map[string]string `yaml:"variants"`
		OSRelease string            `yaml:"osrelease"`
		Files     map[string]string `yaml:"files"`
	} `yaml:"cases"`
}

// TestGolden71SamplesAnd2880PkgMapReplay verifies T-M0-03 DoD:
// 100% replay of all 45 measured samples + 26 documented/constructed samples (71 total)
// and all 2,880 logical package resolutions against research fixtures.
func TestGolden71SamplesAnd2880PkgMapReplay(t *testing.T) {
	eng, err := osadapter.LoadEmbedded()
	if err != nil {
		t.Fatalf("LoadEmbedded: %v", err)
	}

	// Load spec/osmatch_golden.yaml from embedded FS
	gBytes, err := spec.FS.ReadFile("osmatch_golden.yaml")
	if err != nil {
		t.Fatalf("read osmatch_golden.yaml: %v", err)
	}
	var gf goldenFile
	if err := yaml.Unmarshal(gBytes, &gf); err != nil {
		t.Fatalf("parse osmatch_golden.yaml: %v", err)
	}

	// 1. Replay all 26 documented/constructed golden cases
	for _, c := range gf.Cases {
		res, merr := eng.Match(osadapter.MatchInput{
			OSRelease: osadapter.ParseOSRelease(c.OSRelease),
			Files:     c.Files,
		})
		if merr != nil {
			t.Fatalf("case %q error: %v", c.Name, merr)
		}
		var got string
		if res != nil && res.Rule != nil {
			got = res.Rule.ID
		}
		want := ""
		if c.Expect != nil {
			want = *c.Expect
		}
		if got != want {
			t.Errorf("case %q: got rule %q, want %q", c.Name, got, want)
			continue
		}
		for vk, vv := range c.Variants {
			if res == nil || res.Variants[vk] != vv {
				t.Errorf("case %q variant %s: got %q, want %q", c.Name, vk, res.Variants[vk], vv)
			}
		}
	}

	// 2. Replay all 45 measured samples from docs/research/repo_scan.json
	scanRaw, err := os.ReadFile(filepath.Join("..", "..", "docs", "research", "repo_scan.json"))
	if err != nil {
		t.Fatalf("read repo_scan.json: %v", err)
	}
	var scan map[string]struct {
		OSRelease string `json:"osrelease"`
	}
	if err := json.Unmarshal(scanRaw, &scan); err != nil {
		t.Fatalf("parse repo_scan.json: %v", err)
	}
	owner := make(map[string]*osadapter.RuleSpec)
	for i := range eng.OSMatch.Rules {
		r := &eng.OSMatch.Rules[i]
		for _, s := range r.Support.Samples {
			owner[s] = r
		}
	}
	measuredCount := 0
	for sampleKey, entry := range scan {
		expectedRule, ok := owner[sampleKey]
		if !ok {
			t.Fatalf("measured sample %q has no owner rule", sampleKey)
		}
		res, merr := eng.Match(osadapter.MatchInput{
			OSRelease: osadapter.ParseOSRelease(entry.OSRelease),
		})
		if merr != nil {
			t.Fatalf("measured %q error: %v", sampleKey, merr)
		}
		if res == nil || res.Rule.ID != expectedRule.ID {
			got := "<nil>"
			if res != nil {
				got = res.Rule.ID
			}
			t.Errorf("measured %q: got %s, want %s", sampleKey, got, expectedRule.ID)
			continue
		}
		for vk, vv := range gf.Measured[sampleKey] {
			if res.Variants[vk] != vv {
				t.Errorf("measured %q variant %s: got %q, want %q", sampleKey, vk, res.Variants[vk], vv)
			}
		}
		measuredCount++
	}
	if measuredCount+len(gf.Cases) != 71 {
		t.Fatalf("expected 71 golden samples, replayed %d (%d measured + %d cases)", measuredCount+len(gf.Cases), measuredCount, len(gf.Cases))
	}

	// 3. Replay all 2,880 pkgmap entries from docs/research/pkgmap.json
	pmRaw, err := os.ReadFile(filepath.Join("..", "..", "docs", "research", "pkgmap.json"))
	if err != nil {
		t.Fatalf("read pkgmap.json: %v", err)
	}
	var pm map[string]struct {
		Map map[string]struct {
			Pkg *string `json:"pkg"`
		} `json:"map"`
	}
	if err := json.Unmarshal(pmRaw, &pm); err != nil {
		t.Fatalf("parse pkgmap.json: %v", err)
	}
	replayed := 0
	for sampleKey, rule := range owner {
		entry, ok := pm[sampleKey]
		if !ok || len(entry.Map) == 0 {
			continue
		}
		for _, lp := range eng.PkgMap.Packages {
			got, missing, rerr := eng.ResolvePkg(lp.Name, rule)
			if rerr != nil {
				t.Fatalf("ResolvePkg(%s, %s): %v", lp.Name, rule.ID, rerr)
			}
			wantEntry := entry.Map[lp.Name]
			want := ""
			if wantEntry.Pkg != nil {
				want = *wantEntry.Pkg
			}
			if want == "" && !missing {
				t.Errorf("pkgmap %s %s: got %q, want missing(null)", sampleKey, lp.Name, got)
			}
			if want != "" && got != want {
				t.Errorf("pkgmap %s %s: got %q, want %q", sampleKey, lp.Name, got, want)
			}
			replayed++
		}
	}
	if replayed != 2880 {
		t.Fatalf("expected 2880 pkgmap replays, got %d", replayed)
	}
}

func TestParseFacts1RTTAndDuplicateMachineID(t *testing.T) {
	eng, err := osadapter.LoadEmbedded()
	if err != nil {
		t.Fatalf("LoadEmbedded: %v", err)
	}
	sample := `@BEGIN:KV
hostname=kylin-arm-01
fqdn=kylin-arm-01.corp.local
kernel=4.19.90-52.22.v2207.ky10.aarch64
arch=aarch64
page_size=65536
glibc=glibc 2.28
machine_id=abcd1234567890
cgroup_fs=cgroup2fs
systemd_ver=systemd 243 (243)
nft_ver=nftables v0.9.3 (Topsy)
sudo_ver=sudo-rs 0.2.8
selinux=Enforcing
apparmor=N
kysec=KySec status: Normal
fapolicyd=inactive
swap_kb=0
mem_kb=33554432
hugepage_kb=2048
cpu_cores=16
numa_nodes=2
x86_flags=
default_rt=
primary_ip4=
primary_ip6=
@END:KV
@BEGIN:OS_RELEASE
NAME="Kylin Linux Advanced Server"
VERSION="V10 (Lance)"
ID="kylin"
VERSION_ID="V10"
PRETTY_NAME="Kylin Linux Advanced Server V10 (Lance)"
@END:OS_RELEASE
@BEGIN:KYINFO
milestone=V10-SP3-General-Release-2403
@END:KYINFO
@BEGIN:NICS
1: lo    inet 127.0.0.1/8 scope host lo
2: docker0    inet 172.17.0.1/16 scope global docker0
3: ens192    inet 10.20.30.11/24 scope global ens192
@END:NICS
@BEGIN:DISKS
NAME="nvme0n1" TYPE="disk" SIZE="500107862016" ROTA="0" FSTYPE="xfs" MOUNTPOINT="/data" WWN="eui.001" SERIAL="SN001"
@END:DISKS
@BEGIN:MOUNTS
@END:MOUNTS
`
	f, err := osadapter.ParseFactsOutput(sample, "10.20.30.11", eng)
	if err != nil {
		t.Fatalf("ParseFactsOutput: %v", err)
	}
	if f.Arch != "arm64" || f.PageSize != 65536 {
		t.Fatalf("unexpected arch/pageSize: %s / %d", f.Arch, f.PageSize)
	}
	if f.GlibcVersion != "2.28" || f.SudoImpl != "sudo-rs" || f.CgroupVer != 2 {
		t.Fatalf("unexpected facts: glibc=%s sudo=%s cgroup=%d", f.GlibcVersion, f.SudoImpl, f.CgroupVer)
	}
	if f.PrimaryIP4 != "10.20.30.11" {
		t.Fatalf("fallback IP selection failed: got %q", f.PrimaryIP4)
	}
	if f.OS.RuleID != "kylin-v10" || f.OS.Family != "kylin" {
		t.Fatalf("unexpected OS match: %+v", f.OS)
	}
	if len(f.Disks) != 1 || f.Disks[0].MountPoint != "/data" {
		t.Fatalf("unexpected disks: %+v", f.Disks)
	}

	dups := osadapter.FindDuplicateMachineIDs([]sdk.HostView{
		{Name: "h1", Facts: sdk.FactsView{MachineID: "same-id"}},
		{Name: "h2", Facts: sdk.FactsView{MachineID: "same-id"}},
		{Name: "h3", Facts: sdk.FactsView{MachineID: "unique-id"}},
	})
	if len(dups["same-id"]) != 2 {
		t.Fatalf("expected duplicate machine-id detection, got %+v", dups)
	}
}
