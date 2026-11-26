package main

import (
	"fmt"

	"github.com/kinstall/kinstall/internal/registry"
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

// RegisterBuiltinPlugins is the SOLE place allowed to import plugins/* (ARCHITECTURE §4.3, ADR-022, R6).
// Zero init() registration anywhere in the repository!
func RegisterBuiltinPlugins(reg *registry.Registry) error {
	components := []sdk.Component{
		kubernetes.New(),
		valkey.New(),
		postgresql.New(),
		kafka.New(),
		opensearch.New(),
		elkpipeline.New(),
		mysql.New(),
		objectstorage.New(),
		skywalking.New(),
		devops.New(),
		messaging.New(),
		datastack.New(),
	}
	for _, c := range components {
		if err := reg.RegisterComponent(c); err != nil {
			return fmt.Errorf("register component %s: %w", c.Meta().Name, err)
		}
	}
	extensions := []sdk.Extension{
		kubernetes.NewContainerdExtension(),
		kubernetes.NewCalicoExtension(),
		kubernetes.NewLocalPathCSIExtension(),
	}
	for _, e := range extensions {
		if err := reg.RegisterExtension(e); err != nil {
			return fmt.Errorf("register extension %s: %w", e.Meta().Name, err)
		}
	}
	reg.Freeze()
	return nil
}
