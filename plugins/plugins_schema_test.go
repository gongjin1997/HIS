package plugins_test

import (
	"context"
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

func TestAll12PluginsHaveParamSchemasAndValidOperations(t *testing.T) {
	all := []sdk.Component{
		kubernetes.New(),
		valkey.New(),
		postgresql.New(),
		mysql.New(),
		kafka.New(),
		opensearch.New(),
		elkpipeline.New(),
		objectstorage.New(),
		skywalking.New(),
		devops.New(),
		messaging.New(),
		datastack.New(),
	}
	if len(all) != 12 {
		t.Fatalf("expected 12 built-in plugins, got %d", len(all))
	}
	for _, comp := range all {
		meta := comp.Meta()
		if meta.Name == "" {
			t.Fatalf("plugin has empty name")
		}
		if len(meta.Operations) == 0 {
			t.Fatalf("plugin %s must declare Operations", meta.Name)
		}
		for _, opName := range meta.Operations {
			op, ok := comp.Operation(opName)
			if !ok || op == nil {
				t.Fatalf("plugin %s declares operation %q in Meta().Operations but Operation(%q) returned false", meta.Name, opName, opName)
			}
			// Verify Build produces a non-nil pipeline for default product
			if len(meta.Products) > 0 {
				pr := meta.Products[0]
				pl, err := op.Build(context.Background(), &sdk.OpRequest{
					Instance: sdk.InstanceView{
						ID:             "inst_test",
						Name:           "test-" + pr.Product,
						Product:        pr.Product,
						ProductVersion: pr.DefaultVer,
						DeployTarget:   pr.Targets[0],
					},
					Hosts: []sdk.HostView{{ID: "host_1", Name: "node-01", Address: "10.0.0.11", FaultZone: "az-1", Facts: sdk.FactsView{Arch: "amd64", PageSize: 4096, MemMB: 16384, CgroupVer: 2}}},
				})
				if err != nil {
					t.Fatalf("plugin %s op %s Build failed: %v", meta.Name, opName, err)
				}
				if pl == nil || len(pl.Phases) == 0 {
					t.Fatalf("plugin %s op %s returned empty pipeline", meta.Name, opName)
				}
			}
		}
		if len(meta.ParamSchema) == 0 {
			t.Fatalf("plugin %s must expose ParamSchema with x-kinstall-ui", meta.Name)
		}
		props, ok := meta.ParamSchema["properties"].(map[string]any)
		if !ok || len(props) == 0 {
			t.Fatalf("plugin %s ParamSchema missing properties", meta.Name)
		}
	}
}
