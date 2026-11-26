package supportbundle_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kinstall/kinstall/internal/domain"
	"github.com/kinstall/kinstall/internal/store"
	"github.com/kinstall/kinstall/internal/supportbundle"
)

func TestSupportBundleRedactsSecretsAndRendersOperationReport(t *testing.T) {
	ctx := context.Background()
	st, err := store.OpenAndMigrate(ctx, store.Config{Driver: "sqlite", DSN: filepath.Join(t.TempDir(), "sb.db")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()

	const leak = "SuperSecretOBSKey#2026!"
	rawLog := "Connecting to OBS with secret=" + leak + " and X-Amz-Signature=deadbeef1234567890"
	tarGz, rep, err := supportbundle.Collect(ctx, st, "prj_1", rawLog, []string{leak})
	if err != nil || rep.SizeBytes == 0 {
		t.Fatalf("collect failed: %v %+v", err, rep)
	}

	gr, err := gzip.NewReader(bytes.NewReader(tarGz))
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		data, _ := io.ReadAll(tr)
		if strings.Contains(string(data), leak) || strings.Contains(string(data), "deadbeef1234567890") {
			t.Fatalf("CRITICAL REDLINE 6: secret leaked in support-bundle %s: %s", hdr.Name, data)
		}
	}

	hid := "hst_1"
	md, err := supportbundle.RenderOperationReport(&domain.Operation{
		ID: "op_01TEST", TargetType: "instance", TargetID: "ins_1", Action: "install", State: domain.OpSucceeded, FencingToken: 2,
	}, []domain.TaskRun{{PhaseIdx: 0, StepKey: "preflight", HostID: &hid, State: domain.TaskSucceeded}}, "markdown")
	if err != nil || !strings.Contains(string(md), "# Operation Report: `op_01TEST`") {
		t.Fatalf("unexpected report: %v %s", err, md)
	}
}
