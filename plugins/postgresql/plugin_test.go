package postgresql_test

import (
	"context"
	"encoding/json"
	"testing"

	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
	"github.com/kinstall/kinstall/plugins/postgresql"
)

func TestPostgreSQLMemoryCapLeastPrivilegeAndPgBackRestGuard(t *testing.T) {
	p := postgresql.New()
	ctx := context.Background()

	// 1. Memory tuning formula: 512 GiB host MUST cap shared_buffers at 64 GiB and maintenance_work_mem at 2 GiB
	m512 := postgresql.ComputeMemoryTuning(524288, 4096)
	if m512.SharedBuffersMB != 65536 || m512.MaintenanceWorkMB != 2048 {
		t.Fatalf("expected 64GiB/2GiB caps on 512GiB host, got %+v", m512)
	}

	// 2. bootstrapDatabases SUPERUSER / WITH GRANT OPTION MUST be blocked!
	badSpec, _ := json.Marshal(map[string]any{
		"syncProfile":       "sync",
		"pgBackRestVersion": "2.60.0",
		"bootstrapDatabases": []map[string]any{{
			"name":       "his_core",
			"owner":      "his_app",
			"privileges": []string{"CONNECT", "SUPERUSER"},
		}},
	})
	repBad, _ := p.Validate(ctx, &sdk.ValidateRequest{
		Instance: sdk.InstanceView{ID: "pg1", Name: "pg-main", Product: "postgresql", ProductVersion: "18.7.0", DeployTarget: sdk.TargetHostBinary, Spec: badSpec},
	})
	if !repBad.HasBlocking() || repBad.Findings[0].Code != sdk.CodeAuthForbidden {
		t.Fatalf("expected SUPERUSER bootstrapDB to be blocked with KI-AUTH-0002, got %+v", repBad.Findings)
	}

	// 3. pgBackRest < 2.60.0 MUST be rejected (ADR-046)
	oldBackrest, _ := json.Marshal(map[string]any{"syncProfile": "sync", "pgBackRestVersion": "2.59.3"})
	repBR, _ := p.Validate(ctx, &sdk.ValidateRequest{
		Instance: sdk.InstanceView{ID: "pg2", Name: "pg-main", Product: "postgresql", ProductVersion: "18.7.0", DeployTarget: sdk.TargetHostBinary, Spec: oldBackrest},
	})
	if !repBR.HasBlocking() {
		t.Fatalf("expected pgBackRest 2.59.3 to be rejected")
	}

	// 4. sync-strict with < 3 nodes MUST be rejected
	strictSpec, _ := json.Marshal(map[string]any{"syncProfile": "sync-strict", "pgBackRestVersion": "2.60.0"})
	repStrict, _ := p.Validate(ctx, &sdk.ValidateRequest{
		Instance: sdk.InstanceView{ID: "pg3", Name: "pg-fin", Product: "postgresql", ProductVersion: "18.7.0", DeployTarget: sdk.TargetHostBinary, Spec: strictSpec},
		Hosts:    []sdk.HostView{{ID: "h1", Address: "10.0.0.1"}, {ID: "h2", Address: "10.0.0.2"}},
	})
	if !repStrict.HasBlocking() || repStrict.Findings[0].Code != sdk.CodePlanTopologyMinHosts {
		t.Fatalf("expected sync-strict <3 hosts to fail, got %+v", repStrict.Findings)
	}

	// 5. Valid 3-node sync-strict + least-privilege bootstrapDatabases succeeds and exports dsnSecretRef
	goodSpec, _ := json.Marshal(map[string]any{
		"syncProfile":       "sync-strict",
		"pgBackRestVersion": "2.60.0",
		"bootstrapDatabases": []map[string]any{{
			"name":          "sonarqube",
			"owner":         "sonar_app",
			"credentialRef": "cred_01SONAR",
			"privileges":    []string{"CONNECT", "CREATE_SCHEMA_OWNED"},
		}},
	})
	hosts3 := []sdk.HostView{{ID: "h1", Address: "10.0.0.1"}, {ID: "h2", Address: "10.0.0.2"}, {ID: "h3", Address: "10.0.0.3"}}
	repGood, _ := p.Validate(ctx, &sdk.ValidateRequest{
		Instance: sdk.InstanceView{ID: "pg4", Name: "pg-main", Product: "postgresql", ProductVersion: "18.7.0", DeployTarget: sdk.TargetHostBinary, Spec: goodSpec},
		Hosts:    hosts3,
	})
	if repGood.HasBlocking() {
		t.Fatalf("valid PG spec failed: %+v", repGood.Findings)
	}
	plan, err := p.Plan(ctx, &sdk.PlanRequest{
		Instance: sdk.InstanceView{ID: "pg4", Name: "pg-main", Product: "postgresql", ProductVersion: "18.7.0", DeployTarget: sdk.TargetHostBinary, Spec: goodSpec},
		Hosts:    hosts3,
	})
	if err != nil || len(plan.Pipeline.Phases) != 3 {
		t.Fatalf("PG plan failed: err=%v phases=%d", err, len(plan.Pipeline.Phases))
	}
}
