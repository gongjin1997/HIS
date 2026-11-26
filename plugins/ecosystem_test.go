package plugins_test

import (
	"context"
	"encoding/json"
	"testing"

	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
	"github.com/kinstall/kinstall/plugins/datastack"
	"github.com/kinstall/kinstall/plugins/devops"
	"github.com/kinstall/kinstall/plugins/elkpipeline"
	"github.com/kinstall/kinstall/plugins/kafka"
	"github.com/kinstall/kinstall/plugins/messaging"
	"github.com/kinstall/kinstall/plugins/mysql"
	"github.com/kinstall/kinstall/plugins/objectstorage"
	"github.com/kinstall/kinstall/plugins/opensearch"
	"github.com/kinstall/kinstall/plugins/skywalking"
)

func TestM5EcosystemPluginsGuardrails(t *testing.T) {
	ctx := context.Background()

	// P3: Kafka KRaft blocks ZK mode & >8G heap
	kf := kafka.New()
	badKF, _ := json.Marshal(map[string]any{"mode": "zookeeper", "jvmHeapGB": 16})
	repKF, _ := kf.Validate(ctx, &sdk.ValidateRequest{Instance: sdk.InstanceView{Product: "kafka", ProductVersion: "4.3.2", Spec: badKF}})
	if !repKF.HasBlocking() || len(repKF.Findings) < 2 {
		t.Fatalf("expected Kafka ZK & >8G heap to be blocked, got %+v", repKF.Findings)
	}

	// P3: OpenSearch blocks >31G heap & NFS path.data & unaccepted ES license
	osPlg := opensearch.New()
	if got := opensearch.ComputeSafeHeapGB(131072); got != 31 {
		t.Fatalf("expected 31G CompressedOops cap on 128G RAM host, got %d", got)
	}
	badOS, _ := json.Marshal(map[string]any{"heapGB": 40, "dataPath": "/mnt/nfs/os", "dataFsType": "nfs4"})
	repOS, _ := osPlg.Validate(ctx, &sdk.ValidateRequest{Instance: sdk.InstanceView{Product: "elasticsearch", ProductVersion: "9.5.7", Spec: badOS}})
	if !repOS.HasBlocking() || len(repOS.Findings) < 3 {
		t.Fatalf("expected ES license, >31G heap, and NFS to be blocked, got %+v", repOS.Findings)
	}

	// P3: ELK Pipeline enforces partition >= nodes*workers & acknowledgments=true & DLQ
	ep := elkpipeline.New()
	badEP, _ := json.Marshal(map[string]any{"workersPerNode": 8, "topicPartitions": 12, "acknowledgments": false, "autoCommit": true, "dlqBucket": ""})
	repEP, _ := ep.Validate(ctx, &sdk.ValidateRequest{
		Instance: sdk.InstanceView{Product: "elk-pipeline", ProductVersion: "2.16.0", Spec: badEP},
		Hosts:    []sdk.HostView{{ID: "h1"}, {ID: "h2"}}, // 2*8 = 16 > 12 partitions!
	})
	if !repEP.HasBlocking() || len(repEP.Findings) < 3 {
		t.Fatalf("expected ELK alignment/ACK/DLQ violations, got %+v", repEP.Findings)
	}

	// P5: MySQL blocks EOL 8.0, mismatched XtraBackup major, and *.* / mysql_native_password
	my := mysql.New()
	badMy, _ := json.Marshal(map[string]any{
		"xtraBackupVersion": "9.7.0",
		"bootstrapDatabases": []map[string]any{{
			"name": "app", "user": "appu", "scope": "*.*", "authPlugin": "mysql_native_password", "privileges": []string{"ALL PRIVILEGES"},
		}},
	})
	repMy, _ := my.Validate(ctx, &sdk.ValidateRequest{Instance: sdk.InstanceView{Product: "mysql", ProductVersion: "8.0.40", Spec: badMy}})
	if !repMy.HasBlocking() || len(repMy.Findings) < 4 {
		t.Fatalf("expected MySQL 8.0/XtraBackup/bootstrapDB guardrails, got %+v", repMy.Findings)
	}

	// P6: Object storage blocks archived MinIO CE & multi-filer LevelDB
	s3 := objectstorage.New()
	badMinio, _ := json.Marshal(map[string]any{"engine": "minio"})
	repMinio, _ := s3.Validate(ctx, &sdk.ValidateRequest{Instance: sdk.InstanceView{Product: "object-storage", Spec: badMinio}})
	if !repMinio.HasBlocking() {
		t.Fatalf("expected archived MinIO CE to be blocked")
	}
	badFiler, _ := json.Marshal(map[string]any{"engine": "seaweedfs", "filerCount": 2, "filerMetaBackend": "leveldb"})
	repFiler, _ := s3.Validate(ctx, &sdk.ValidateRequest{Instance: sdk.InstanceView{Product: "object-storage", Spec: badFiler}})
	if !repFiler.HasBlocking() {
		t.Fatalf("expected multi-filer LevelDB to be blocked")
	}

	// P7: SkyWalking blocks SQL param tracing & disabled OAP auth
	sw := skywalking.New()
	badSW, _ := json.Marshal(map[string]any{"traceSqlParameters": true, "enableOapAuth": false})
	repSW, _ := sw.Validate(ctx, &sdk.ValidateRequest{Instance: sdk.InstanceView{Product: "skywalking", ProductVersion: "11.1.0", Spec: badSW}})
	if !repSW.HasBlocking() || len(repSW.Findings) < 2 {
		t.Fatalf("expected SkyWalking privacy/auth redlines to block, got %+v", repSW.Findings)
	}

	// P8: Harbor on 64K host MUST externalize Valkey (harbor#24445); Jenkins requires JDK >= 21
	dev := devops.New()
	hbSpec, _ := json.Marshal(map[string]any{"externalValkey": false})
	repHB, _ := dev.Validate(ctx, &sdk.ValidateRequest{
		Instance: sdk.InstanceView{Product: "harbor", ProductVersion: "2.16.0", DeployTarget: sdk.TargetHostContainer, Spec: hbSpec},
		Hosts:    []sdk.HostView{{ID: "h64", Facts: sdk.FactsView{PageSize: 65536}}},
	})
	if !repHB.HasBlocking() || repHB.Findings[0].Code != sdk.CodePlanArchPageMismatch {
		t.Fatalf("expected Harbor 64K without externalValkey to block, got %+v", repHB.Findings)
	}
	jksSpec, _ := json.Marshal(map[string]any{"jdkVersion": 17})
	repJKS, _ := dev.Validate(ctx, &sdk.ValidateRequest{
		Instance: sdk.InstanceView{Product: "jenkins", ProductVersion: "2.580.3", Spec: jksSpec},
		Hosts:    []sdk.HostView{{ID: "h1"}},
	})
	if !repJKS.HasBlocking() {
		t.Fatalf("expected Jenkins JDK 17 to block")
	}

	// P9: Seata blocks default password 'seata' & even Raft; ZK blocks 2-node & shared WAL/snap dir
	msg := messaging.New()
	badSeata, _ := json.Marshal(map[string]any{"consolePassword": "seata", "storeMode": "raft"})
	repSeata, _ := msg.Validate(ctx, &sdk.ValidateRequest{
		Instance: sdk.InstanceView{Product: "seata", ProductVersion: "2.6.0", Spec: badSeata},
		Hosts:    []sdk.HostView{{ID: "h1"}, {ID: "h2"}},
	})
	if !repSeata.HasBlocking() || len(repSeata.Findings) < 2 {
		t.Fatalf("expected Seata weak password & 2-node Raft to block, got %+v", repSeata.Findings)
	}
	badZK, _ := json.Marshal(map[string]any{"dataDir": "/data/zk", "dataLogDir": "/data/zk"})
	repZK, _ := msg.Validate(ctx, &sdk.ValidateRequest{
		Instance: sdk.InstanceView{Product: "zookeeper", ProductVersion: "3.9.7", Spec: badZK},
		Hosts:    []sdk.HostView{{ID: "h1"}, {ID: "h2"}},
	})
	if !repZK.HasBlocking() || len(repZK.Findings) < 2 {
		t.Fatalf("expected ZK 2-node & shared dataDir/dataLogDir to block, got %+v", repZK.Findings)
	}

	// P10: Milvus CVE-2026-26190 (< 2.6.26), Neo4j online plugins, OnlyOffice legacy privileged, generic-app 0.0.0.0 & missing mount type
	ds := datastack.New()
	repMil, _ := ds.Validate(ctx, &sdk.ValidateRequest{Instance: sdk.InstanceView{Product: "milvus", ProductVersion: "2.6.25"}})
	if !repMil.HasBlocking() {
		t.Fatalf("expected Milvus < 2.6.26 (CVE-2026-26190) to block")
	}
	badGA, _ := json.Marshal(map[string]any{
		"services": []map[string]any{{
			"name": "hkms-api", "image": "corp/hkms:1.0@sha256:abc", "ports": []string{"0.0.0.0:8080:8080"},
			"privileged": true, "mounts": []map[string]any{{"src": "/data/app.log", "dst": "/app/app.log"}},
		}},
	})
	repGA, _ := ds.Validate(ctx, &sdk.ValidateRequest{Instance: sdk.InstanceView{Product: "generic-app", ProductVersion: "1.0.0", DeployTarget: sdk.TargetHostContainer, Spec: badGA}})
	if !repGA.HasBlocking() || len(repGA.Findings) < 3 {
		t.Fatalf("expected generic-app privileged/0.0.0.0/mount-type violations, got %+v", repGA.Findings)
	}
}
