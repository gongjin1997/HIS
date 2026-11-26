package scale_test

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/kinstall/kinstall/internal/audit"
	"github.com/kinstall/kinstall/internal/domain"
	"github.com/kinstall/kinstall/internal/store"
)

// TestScale500HostsControlPlaneSLO validates G3 §4 / T-M2-11 control-plane performance SLO
// by provisioning 500 CMDB hosts (mixed amd64/arm64-4K/arm64-64K) and verifying query + chained audit latency.
func TestScale500HostsControlPlaneSLO(t *testing.T) {
	ctx := context.Background()
	st, err := store.OpenAndMigrate(ctx, store.Config{Driver: "sqlite", DSN: filepath.Join(t.TempDir(), "scale500.db")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()

	prj := &domain.Project{ID: domain.NewID(domain.PrefixProject), Name: "scale-500"}
	if err := st.Projects().Create(ctx, prj); err != nil {
		t.Fatal(err)
	}
	cred := &domain.Credential{
		ID: domain.NewID(domain.PrefixCredential), ProjectID: prj.ID, Name: "ops-ssh", Kind: "ssh_key",
		EnvelopeJSON: []byte(`{"v":1}`), CreatedBy: "usr_scale",
	}
	if err := st.Credentials().Create(ctx, cred); err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	for i := 0; i < 500; i++ {
		arch := "amd64"
		ps := 4096
		if i%5 == 0 {
			arch = "arm64"
		}
		if i%20 == 0 {
			arch = "arm64"
			ps = 65536
		}
		h := &domain.Host{
			ProjectID:    prj.ID,
			Name:         fmt.Sprintf("node-%03d", i),
			Address:      fmt.Sprintf("10.20.%d.%d", i/250, (i%250)+1),
			SSHPort:      22,
			SSHUser:      "ops",
			CredentialID: cred.ID,
			FaultZone:    fmt.Sprintf("az-%d", (i%3)+1),
			Arch:         arch,
			PageSize:     ps,
		}
		if err := st.Hosts().Create(ctx, h); err != nil {
			t.Fatalf("create host %d: %v", i, err)
		}
	}
	insertDur := time.Since(start)

	listStart := time.Now()
	hosts, err := st.Hosts().ListByProject(ctx, prj.ID)
	listDur := time.Since(listStart)
	if err != nil || len(hosts) != 500 {
		t.Fatalf("expected 500 hosts, got %d err=%v", len(hosts), err)
	}
	if listDur > 500*time.Millisecond {
		t.Fatalf("500-host LIST exceeded 500ms SLO: %v", listDur)
	}

	aud := audit.NewRecorder(st.Audit(), true)
	for i := 0; i < 200; i++ {
		if _, err := aud.Append(ctx, domain.AuditEntry{
			ActorID: "usr_scale", ActorType: "user", ProjectID: prj.ID,
			Action: "host.probe", TargetType: "host", TargetID: hosts[i].ID, Result: "success", PayloadJSON: "{}",
		}); err != nil {
			t.Fatalf("audit %d: %v", i, err)
		}
	}
	vr, err := audit.VerifyChain(ctx, st.Audit(), true)
	if err != nil || !vr.Valid || vr.TotalChecked != 200 {
		t.Fatalf("scale audit verify failed: %v %+v", err, vr)
	}
	t.Logf("500-host scale SLO PASS: insert=%v list=%v audit200=valid", insertDur, listDur)
}
