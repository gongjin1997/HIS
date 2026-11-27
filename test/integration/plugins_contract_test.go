package integration_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
	"github.com/kinstall/kinstall/plugins/datastack"
	"github.com/kinstall/kinstall/plugins/devops"
	"github.com/kinstall/kinstall/plugins/elkpipeline"
	"github.com/kinstall/kinstall/plugins/kafka"
	"github.com/kinstall/kinstall/plugins/kubernetes"
	"github.com/kinstall/kinstall/plugins/messaging"
	"github.com/kinstall/kinstall/plugins/mysql"
	"github.com/kinstall/kinstall/plugins/objectstorage"
	"github.com/kinstall/kinstall/plugins/opensearch"
	"github.com/kinstall/kinstall/plugins/postgresql"
	"github.com/kinstall/kinstall/plugins/skywalking"
	"github.com/kinstall/kinstall/plugins/valkey"
)

func TestAllTwelvePluginsUniversalContract(t *testing.T) {
	ctx := context.Background()
	plugins := []sdk.Component{
		kubernetes.New(),
		valkey.New(),
		postgresql.New(),
		kafka.New(),
		mysql.New(),
		opensearch.New(),
		elkpipeline.New(),
		objectstorage.New(),
		skywalking.New(),
		devops.New(),
		messaging.New(),
		datastack.New(),
	}

	hosts := []sdk.HostView{
		{ID: "hst_1", Name: "node-1", Address: "10.20.1.11", FaultZone: "az-1", Roles: []string{"control-plane", "worker"}, Facts: sdk.FactsView{Arch: "amd64", PageSize: 4096, MemMB: 16384, Kernel: "6.6.0", NFTVersion: "1.0.9", CgroupVer: 2}},
		{ID: "hst_2", Name: "node-2", Address: "10.20.1.12", FaultZone: "az-2", Roles: []string{"control-plane", "worker"}, Facts: sdk.FactsView{Arch: "amd64", PageSize: 4096, MemMB: 16384, Kernel: "6.6.0", NFTVersion: "1.0.9", CgroupVer: 2}},
		{ID: "hst_3", Name: "node-3", Address: "10.20.1.13", FaultZone: "az-3", Roles: []string{"control-plane", "worker"}, Facts: sdk.FactsView{Arch: "amd64", PageSize: 4096, MemMB: 16384, Kernel: "6.6.0", NFTVersion: "1.0.9", CgroupVer: 2}},
	}

	for _, comp := range plugins {
		meta := comp.Meta()
		t.Run(meta.Name, func(t *testing.T) {
			if len(meta.Products) == 0 {
				t.Fatalf("plugin %s declares no products", meta.Name)
			}
			prod := meta.Products[0]
			target := prod.Targets[0]

			// Build default spec from ParamSchema defaults (proving UI -> Plugin binding round-trip!)
			specMap := map[string]any{}
			if props, ok := meta.ParamSchema["properties"].(map[string]any); ok {
				for k, rawProp := range props {
					if pm, ok := rawProp.(map[string]any); ok {
						if def, hasDef := pm["default"]; hasDef {
							specMap[k] = def
						}
					}
				}
			}
			specBytes, _ := json.Marshal(specMap)
			inst := sdk.InstanceView{
				ID:             "inst_" + meta.Name,
				Name:           "demo-" + meta.Name,
				Product:        prod.Product,
				ProductVersion: prod.DefaultVer,
				DeployTarget:   target,
				Spec:           specBytes,
			}

			// 1. Validate with ParamSchema defaults must have zero blocking findings!
			valRep, err := comp.Validate(ctx, &sdk.ValidateRequest{Instance: inst, Hosts: hosts})
			if err != nil {
				t.Fatalf("Validate error: %v", err)
			}
			if valRep != nil && valRep.HasBlocking() {
				t.Fatalf("Validate blocked on ParamSchema defaults: %+v", valRep.Findings)
			}

			// 2. Plan(install) must return non-empty pipeline, Claims for host targets, and Outputs matching OutputSchema
			installPlan, err := comp.Plan(ctx, &sdk.PlanRequest{Instance: inst, Hosts: hosts, Operation: "install"})
			if err != nil {
				t.Fatalf("Plan(install) error: %v", err)
			}
			if installPlan.Pipeline == nil || len(installPlan.Pipeline.Phases) == 0 {
				t.Fatalf("Plan(install) returned empty pipeline")
			}
			if target != sdk.TargetKubernetes && len(installPlan.Claims) == 0 {
				t.Fatalf("Plan(install) on %s returned empty Claims (breaks instance_host_claims protection!)", target)
			}
			for outKey := range meta.OutputSchema {
				if _, ok := installPlan.Outputs[outKey]; !ok {
					t.Fatalf("Plan(install) missing declared OutputSchema key %q", outKey)
				}
			}

			// 3. Plan(uninstall) must dispatch uninstall op and mark phase Disruptive=true!
			unPlan, err := comp.Plan(ctx, &sdk.PlanRequest{Instance: inst, Hosts: hosts, Operation: "uninstall"})
			if err != nil {
				t.Fatalf("Plan(uninstall) error: %v", err)
			}
			if unPlan.Pipeline == nil || len(unPlan.Pipeline.Phases) == 0 {
				t.Fatalf("Plan(uninstall) returned empty pipeline")
			}
			hasDisruptive := false
			for _, ph := range unPlan.Pipeline.Phases {
				if ph.Disruptive {
					hasDisruptive = true
					break
				}
			}
			if !hasDisruptive {
				t.Fatalf("Plan(uninstall) did not dispatch disruptive uninstall pipeline (phases=%+v)", unPlan.Pipeline.Phases)
			}

			// 4. Every declared Operation in Meta().Operations must exist and build a non-empty pipeline!
			for _, opName := range meta.Operations {
				op, ok := comp.Operation(opName)
				if !ok {
					t.Fatalf("declared operation %q not registered in Operation()", opName)
				}
				pl, berr := op.Build(ctx, &sdk.OpRequest{Instance: inst, Hosts: hosts})
				if berr != nil {
					t.Fatalf("Operation(%q).Build error: %v", opName, berr)
				}
				if pl == nil || len(pl.Phases) == 0 {
					t.Fatalf("Operation(%q).Build returned empty pipeline", opName)
				}
			}
		})
	}

	// Verify UI alias & PITR bindings explicitly
	t.Run("SchemaAliasesAndPITR", func(t *testing.T) {
		k8s := kubernetes.New()
		kPlan, err := k8s.Plan(ctx, &sdk.PlanRequest{
			Instance: sdk.InstanceView{ID: "i1", Name: "k1", Product: "kubernetes", ProductVersion: "1.37.2", DeployTarget: sdk.TargetHostBinary, Spec: []byte(`{"apiServerVIP":"10.88.0.10"}`)},
			Hosts:    hosts, Operation: "install",
		})
		if err != nil || kPlan.Outputs["apiServerVIP"] != "https://10.88.0.10:6443" {
			t.Fatalf("k8s apiServerVIP alias failed: out=%v err=%v", kPlan, err)
		}

		vk := valkey.New()
		vPlan, err := vk.Plan(ctx, &sdk.PlanRequest{
			Instance: sdk.InstanceView{ID: "i2", Name: "v1", Product: "valkey", ProductVersion: "9.1.4", DeployTarget: sdk.TargetHostContainer, Spec: []byte(`{"topology":"standalone","credentialRef":"crd_vk_1"}`)},
			Hosts:    hosts[:1], Operation: "install",
		})
		if err != nil || vPlan.Outputs["adminAclSecretRef"] != "crd_vk_1" {
			t.Fatalf("valkey credentialRef alias failed: out=%v err=%v", vPlan, err)
		}
		// Ensure fake all-zero digest was removed from valkey container compose YAML
		rawPL, _ := json.Marshal(vPlan.Pipeline)
		if strings.Contains(string(rawPL), "0000000000000000") {
			t.Fatalf("valkey compose YAML still contains dummy all-zero sha256 digest")
		}

		pg := postgresql.New()
		pgPlan, err := pg.Plan(ctx, &sdk.PlanRequest{
			Instance: sdk.InstanceView{ID: "i3", Name: "pg1", Product: "postgresql", ProductVersion: "18.7.0", DeployTarget: sdk.TargetHostBinary, Spec: []byte(`{"haproxyRWPort":5500,"haproxyROPort":5501,"targetTime":"2026-11-27T12:00:00Z"}`)},
			Hosts:    hosts, Operation: "restore",
		})
		if err != nil || pgPlan.Outputs["primaryEndpoint"] != "10.20.1.11:5500" {
			t.Fatalf("pg haproxyRWPort alias failed: out=%v err=%v", pgPlan, err)
		}
		rawPG, _ := json.Marshal(pgPlan.Pipeline)
		if !strings.Contains(string(rawPG), `2026-11-27T12:00:00Z`) {
			t.Fatalf("pg restore pipeline missing PITR targetTime: %s", string(rawPG))
		}
	})
}
