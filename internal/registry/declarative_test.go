package registry_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/kinstall/kinstall/internal/registry"
	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
)

func TestDeclarativeAndExternalPluginSandbox(t *testing.T) {
	// 1. Valid declarative YAML plugin restricted to 15 built-in Tasks
	validYAML := []byte(`
apiVersion: kinstall.io/v1alpha1
kind: DeclarativePlugin
meta:
  name: custom-nginx-proxy
  version: 1.0.0
  sdkVersion: v1alpha1
  type: Component
  products:
    - product: custom-proxy
      versions: ">=1.0.0"
      targets: [host-binary]
operations:
  install:
    phases:
      - name: deploy
        tasks:
          - key: write-conf
            type: file.sync
            params: { dst: /etc/custom-proxy.conf }
          - key: wait-port
            type: wait.tcp
            params: { hostPort: "127.0.0.1:8088" }
outputs:
  endpoint: "http://127.0.0.1:8088"
`)
	comp, err := registry.LoadDeclarativeYAML(validYAML)
	if err != nil {
		t.Fatalf("LoadDeclarativeYAML valid failed: %v", err)
	}
	res, err := comp.Plan(context.Background(), &sdk.PlanRequest{Operation: "install"})
	if err != nil || len(res.Pipeline.Phases) != 1 || len(res.Pipeline.Phases[0].Tasks) != 2 {
		t.Fatalf("declarative plan failed: %v %+v", err, res)
	}

	// 2. Disallowed task type in declarative YAML MUST be rejected!
	evilYAML := []byte(`
apiVersion: kinstall.io/v1alpha1
kind: DeclarativePlugin
meta:
  name: evil-plugin
  version: 1.0.0
  products: [{ product: evil, versions: ">=1.0.0", targets: [host-binary] }]
operations:
  install:
    phases:
      - name: pwn
        tasks:
          - key: raw-ssh
            type: ssh.raw_exec
`)
	if _, err := registry.LoadDeclarativeYAML(evilYAML); err == nil {
		t.Fatalf("expected disallowed task type in declarative plugin to be rejected")
	}

	// 3. External JSON-RPC process plugin proxy (Linux shell script mock)
	if runtime.GOOS == "linux" {
		script := filepath.Join(t.TempDir(), "mock-ext-plugin.sh")
		content := `#!/bin/sh
read req
case "$req" in
  *'"method":"meta"'*)
    echo '{"result":{"name":"ext-demo","version":"1.0.0","sdkVersion":"v1alpha1","type":"Component","products":[{"product":"ext-demo","versions":">=1.0.0","targets":["host-binary"]}]}}'
    ;;
  *'"method":"plan"'*)
    echo '{"result":{"summary":"ext plan","pipeline":{"phases":[{"name":"ext-phase","tasks":[{"key":"t1","type":"cmd.run","params":{"script":"echo ok"}}]}]}}}'
    ;;
  *)
    echo '{"result":{"findings":[]}}'
    ;;
esac
`
		if err := os.WriteFile(script, []byte(content), 0o700); err != nil {
			t.Fatalf("write mock ext plugin: %v", err)
		}
		ext, err := registry.LoadExternalPlugin(context.Background(), script, 5*time.Second)
		if err != nil {
			t.Fatalf("LoadExternalPlugin failed: %v", err)
		}
		if ext.Meta().Name != "ext-demo" {
			t.Fatalf("unexpected ext meta: %+v", ext.Meta())
		}
		plan, err := ext.Plan(context.Background(), &sdk.PlanRequest{Operation: "install"})
		if err != nil || len(plan.Pipeline.Phases) != 1 {
			t.Fatalf("external plan failed: %v %+v", err, plan)
		}
	}
}
