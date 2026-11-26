package gitops_test

import (
	"encoding/json"
	"testing"

	"github.com/kinstall/kinstall/internal/domain"
	"github.com/kinstall/kinstall/internal/gitops"
	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
)

func TestGitOpsExportAndDiffExitCodes(t *testing.T) {
	live := []domain.Instance{{
		Name:            "valkey-prod",
		Env:             "prod",
		Product:         "valkey",
		ProductVersion:  "9.1.4",
		DeployTarget:    sdk.TargetHostBinary,
		DesiredSpecJSON: json.RawMessage(`{"topology":"cluster","port":6379}`),
	}}
	exported, err := gitops.ExportYAML("core-ops", live)
	if err != nil {
		t.Fatal(err)
	}
	entries, code, err := gitops.Diff(exported, live)
	if err != nil || code != gitops.ExitNoChanges || len(entries) != 1 || entries[0].Action != "unchanged" {
		t.Fatalf("expected ExitNoChanges (0), got code=%d entries=%+v err=%v", code, entries, err)
	}

	// Modify version in desired YAML -> ExitChangesPlanned (2)
	changed := []domain.Instance{{
		Name:            "valkey-prod",
		Env:             "prod",
		Product:         "valkey",
		ProductVersion:  "9.2.0",
		DeployTarget:    sdk.TargetHostBinary,
		DesiredSpecJSON: json.RawMessage(`{"topology":"cluster","port":6379}`),
	}}
	desired2, _ := gitops.ExportYAML("core-ops", changed)
	entries2, code2, err := gitops.Diff(desired2, live)
	if err != nil || code2 != gitops.ExitChangesPlanned || entries2[0].Action != "update" {
		t.Fatalf("expected ExitChangesPlanned (2) update, got code=%d entries=%+v err=%v", code2, entries2, err)
	}
}
