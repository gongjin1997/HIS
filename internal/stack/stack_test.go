package stack_test

import (
	"testing"

	"github.com/kinstall/kinstall/internal/stack"
	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
)

func TestStackKahnDAGExternalRefsAndCycleRejection(t *testing.T) {
	yamlSpec := `
apiVersion: kinstall.io/v1alpha1
kind: Stack
metadata:
  name: prod-logging
  project: core-ops
  env: prod
spec:
  externalRefs:
    - id: cloud-obs
      kind: obs
      endpoints: ["https://obs.cn-north-4.myhuaweicloud.com"]
      credentialRef: cred_01OBS
  instances:
    - id: s3
      product: objectstorage
      deployTarget: host-binary
      spec: {}
    - id: kafka
      product: kafka
      deployTarget: host-binary
      spec: {}
    - id: opensearch
      product: opensearch
      deployTarget: host-binary
      dependsOn: [s3]
      spec:
        s3Endpoint: "${instances.s3.outputs.s3Endpoint}"
        cloudBackup: "${externalRefs.cloud-obs.endpoints}"
    - id: dataprepper
      product: elkpipeline
      deployTarget: host-container
      dependsOn: [kafka, opensearch]
      spec:
        kafkaBrokers: "${instances.kafka.outputs.bootstrapServers}"
        sink: "${instances.opensearch.outputs.httpEndpoints}"
`
	doc, err := stack.Parse([]byte(yamlSpec))
	if err != nil {
		t.Fatal(err)
	}
	dag, err := stack.ResolveAndValidate(doc, nil, func(credID string) bool { return credID == "cred_01OBS" })
	if err != nil {
		t.Fatalf("ResolveAndValidate: %v", err)
	}
	// Layer 0: [kafka, s3], Layer 1: [opensearch], Layer 2: [dataprepper]
	if len(dag.InstallLayers) != 3 || len(dag.InstallLayers[0]) != 2 || dag.InstallLayers[2][0] != "dataprepper" {
		t.Fatalf("unexpected install layers: %+v", dag.InstallLayers)
	}
	// Uninstall layers MUST be reverse order and NEVER contain externalRef cloud-obs!
	if dag.UninstallLayers[0][0] != "dataprepper" {
		t.Fatalf("expected dataprepper uninstalled first, got %+v", dag.UninstallLayers)
	}
	for _, layer := range dag.UninstallLayers {
		for _, id := range layer {
			if id == "cloud-obs" {
				t.Fatal("externalRef cloud-obs must NEVER appear in uninstall layers!")
			}
		}
	}

	// Cycle detection -> KI-STACK-0001
	cycleYAML := `
apiVersion: kinstall.io/v1alpha1
kind: Stack
metadata: { name: bad-cycle, project: p, env: dev }
spec:
  instances:
    - id: a
      product: valkey
      dependsOn: [b]
      spec: {}
    - id: b
      product: valkey
      dependsOn: [a]
      spec: {}
`
	cdoc, _ := stack.Parse([]byte(cycleYAML))
	if _, err := stack.ResolveAndValidate(cdoc, nil, nil); !sdk.IsCode(err, sdk.CodeStackCycleDetected) {
		t.Fatalf("expected KI-STACK-0001 on cycle, got %v", err)
	}

	// Missing dependsOn closure for output reference -> KI-STACK-0003
	badRefYAML := `
apiVersion: kinstall.io/v1alpha1
kind: Stack
metadata: { name: bad-ref, project: p, env: dev }
spec:
  instances:
    - id: a
      product: kafka
      spec: {}
    - id: b
      product: elkpipeline
      spec:
        brokers: "${instances.a.outputs.bootstrapServers}"
`
	bdoc, _ := stack.Parse([]byte(badRefYAML))
	if _, err := stack.ResolveAndValidate(bdoc, nil, nil); !sdk.IsCode(err, sdk.CodeStackOutputRefInvalid) {
		t.Fatalf("expected KI-STACK-0003 on undeclared dependsOn ref, got %v", err)
	}

	// Uninstall blocked when referenced -> KI-STACK-0002
	if err := stack.ValidateSafeUninstall("s3", []string{"stack_other/inst_x"}); !sdk.IsCode(err, sdk.CodeStackReferencedInUse) {
		t.Fatalf("expected KI-STACK-0002, got %v", err)
	}
}
