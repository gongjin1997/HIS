package osadapter

import "testing"

func TestParseFactsSysctlTHPAndListeningPorts(t *testing.T) {
	raw := `@BEGIN:KV
hostname=his-db-01
kernel=6.6.0
arch=x86_64
page_size=4096
cpu_cores=8
mem_kb=16777216
swap_kb=0
thp_enabled=always [madvise] never
@END:KV
@BEGIN:OS_RELEASE
ID=debian
VERSION_ID="12"
PRETTY_NAME="Debian GNU/Linux 12"
@END:OS_RELEASE
@BEGIN:SYSCTL
vm.overcommit_memory=0
vm.swappiness=60
net.core.somaxconn=4096
@END:SYSCTL
@BEGIN:LISTEN_PORTS
tcp LISTEN 0 128 0.0.0.0:5432 0.0.0.0:* users:(("postgres",pid=1024,fd=5))
tcp LISTEN 0 128 [::]:22 [::]:* users:(("sshd",pid=512,fd=3))
udp UNCONN 0 0 127.0.0.1:323 0.0.0.0:* users:(("chronyd",pid=400,fd=6))
@END:LISTEN_PORTS
`
	fv, err := ParseFactsOutput(raw, "10.10.1.10", nil)
	if err != nil {
		t.Fatalf("ParseFactsOutput failed: %v", err)
	}
	if fv.THPEnabled != "always [madvise] never" {
		t.Fatalf("unexpected THPEnabled: %q", fv.THPEnabled)
	}
	if fv.Sysctls["vm.overcommit_memory"] != "0" || fv.Sysctls["vm.swappiness"] != "60" {
		t.Fatalf("unexpected Sysctls: %#v", fv.Sysctls)
	}
	if len(fv.ListeningPorts) != 3 {
		t.Fatalf("expected 3 listening ports, got %d: %#v", len(fv.ListeningPorts), fv.ListeningPorts)
	}
	if fv.ListeningPorts[0].Port != 5432 || fv.ListeningPorts[0].Process != "postgres" || fv.ListeningPorts[0].Proto != "tcp" {
		t.Fatalf("unexpected first listening port: %#v", fv.ListeningPorts[0])
	}
}
