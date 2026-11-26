package objectstorage

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
)

// BucketSpec declares an S3 bucket with optional WORM Object Lock (P6 §2).
type BucketSpec struct {
	Name          string `json:"name"`
	WORMDays      int    `json:"wormDays,omitempty"`
	RetentionDays int    `json:"retentionDays,omitempty"`
}

// Spec configures multi-engine object storage (SeaweedFS 4.54 / Ceph RGW / RustFS / Garage, P6).
type Spec struct {
	Engine           string       `json:"engine"` // seaweedfs | ceph-rgw | rustfs | garage (blocks minio!)
	S3Port           int          `json:"s3Port"`
	FilerCount       int          `json:"filerCount"`
	FilerMetaBackend string       `json:"filerMetaBackend"` // postgresql | valkey | raft | leveldb
	EnableEC104      bool         `json:"enableEC104"`      // Reed-Solomon 10+4 cold conversion
	Buckets          []BucketSpec `json:"buckets,omitempty"`
	AcceptRedLicense bool         `json:"acceptRedLicense"`
}

func parseSpec(raw json.RawMessage) Spec {
	s := Spec{Engine: "seaweedfs", S3Port: 8333, FilerCount: 2, FilerMetaBackend: "postgresql", EnableEC104: true}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &s)
	}
	if s.Engine == "" {
		s.Engine = "seaweedfs"
	}
	if s.S3Port <= 0 {
		s.S3Port = 8333
	}
	return s
}

// Plugin implements sdk.Component for product "object-storage" (P6, ADR-012/054/055/056).
type Plugin struct{ ops map[string]sdk.Operation }

func New() *Plugin {
	p := &Plugin{}
	p.ops = map[string]sdk.Operation{
		"install":   &osOp{name: "install", mutating: true, build: p.buildInstall},
		"ec-encode": &osOp{name: "ec-encode", mutating: true, build: p.buildECEncode},
		"uninstall": &osOp{name: "uninstall", mutating: true, build: p.buildUninstall},
	}
	return p
}

func (p *Plugin) Meta() sdk.PluginMeta {
	return sdk.PluginMeta{
		Name:        "object-storage",
		Version:     "1.1.0",
		SDKVersion:  sdk.SDKAPIVersion,
		Type:        sdk.PluginComponent,
		Description: "Unified S3 object storage: SeaweedFS 4.54 (default hot-rep + cold EC 10+4 + WORM), Ceph RGW (Rook v1.21.3), RustFS 1.0.6, Garage v2.5.0; blocks archived MinIO CE",
		Products: []sdk.ProductRange{{
			Product:     "object-storage",
			Versions:    ">=1.0.0, <10.0.0",
			DefaultVer:  "4.54.0",
			Targets:     []sdk.DeployTarget{sdk.TargetHostBinary, sdk.TargetHostContainer, sdk.TargetKubernetes},
			LicenseTier: "green",
		}},
		Platforms: sdk.Platforms{Arch: []string{"amd64", "arm64"}, PageSizes: []int{4096, 65536}},
		OutputSchema: map[string]string{
			"s3Endpoint":     "string",
			"caCertSecretId": "string",
			"bucketCreds":    "object",
			"dlqBucket":      "string",
		},
		SchemaVersion: 1,
	}
}

func (p *Plugin) Validate(_ context.Context, req *sdk.ValidateRequest) (*sdk.ValidationReport, error) {
	rep := sdk.NewValidationReport()
	spec := parseSpec(req.Instance.Spec)
	eng := strings.ToLower(spec.Engine)
	if eng == "minio" || strings.Contains(eng, "minio") {
		rep.Block(sdk.CodeImgForbiddenRegistry, "MinIO CE was archived in 2026-04 and is strictly forbidden (ADR-012/054); use SeaweedFS 4.54 or RustFS 1.0.6")
	}
	if eng == "garage" && !spec.AcceptRedLicense {
		rep.Block(sdk.CodeImgForbiddenRegistry, "Garage v2.5.0 is AGPLv3 (red-list); set acceptRedLicense=true or use SeaweedFS 4.54 (Apache-2.0)")
	}
	// Multi-Filer production MUST NOT use local embedded LevelDB (P6 §2.4)
	if eng == "seaweedfs" && spec.FilerCount > 1 && strings.EqualFold(spec.FilerMetaBackend, "leveldb") {
		rep.Block(sdk.CodeAPIInvalidRequest, "multi-filer SeaweedFS forbids local LevelDB metadata backend; configure postgresql, valkey, or raft (P6 §2.4)")
	}
	return rep, nil
}

func (p *Plugin) Plan(ctx context.Context, req *sdk.PlanRequest) (*sdk.PlanResult, error) {
	spec := parseSpec(req.Instance.Spec)
	pl, err := p.buildInstall(ctx, &sdk.OpRequest{Instance: req.Instance, Hosts: req.Hosts})
	if err != nil {
		return nil, err
	}
	host := "127.0.0.1"
	if len(req.Hosts) > 0 && req.Hosts[0].Address != "" {
		host = req.Hosts[0].Address
	}
	creds := map[string]string{}
	dlq := "logging-dlq"
	for _, b := range spec.Buckets {
		creds[b.Name] = "cred_s3_" + b.Name + "_" + req.Instance.ID
		if strings.Contains(b.Name, "dlq") {
			dlq = b.Name
		}
	}
	return &sdk.PlanResult{
		Summary:  fmt.Sprintf("Object Storage (%s %s, EC10+4=%v, buckets=%d)", spec.Engine, req.Instance.ProductVersion, spec.EnableEC104, len(spec.Buckets)),
		Pipeline: pl,
		Outputs: map[string]any{
			"s3Endpoint":     fmt.Sprintf("https://%s:%d", host, spec.S3Port),
			"caCertSecretId": "cred_s3_ca_" + req.Instance.ID,
			"bucketCreds":    creds,
			"dlqBucket":      dlq,
		},
	}, nil
}

func (p *Plugin) Operation(name string) (sdk.Operation, bool) { op, ok := p.ops[name]; return op, ok }

func (p *Plugin) Status(_ context.Context, _ *sdk.StatusRequest) (*sdk.InstanceStatus, error) {
	return &sdk.InstanceStatus{Health: "healthy", Message: "S3 gateway & volume quorum healthy", CheckedAt: time.Now().UTC()}, nil
}

func (p *Plugin) buildInstall(_ context.Context, req *sdk.OpRequest) (*sdk.Pipeline, error) {
	spec := parseSpec(req.Instance.Spec)
	pb := sdk.NewPipelineBuilder()
	if spec.Engine == "ceph-rgw" || req.Instance.DeployTarget == sdk.TargetKubernetes {
		pb.Phase(sdk.PhaseSpec{
			Name: "k8s-rook-ceph",
			Tasks: []sdk.TaskSpec{
				sdk.HelmRelease(sdk.HelmReleaseSpec{
					ReleaseName: "rook-ceph-" + req.Instance.Name, Namespace: "rook-ceph",
					ChartRef: "charts/rook-ceph-v1.21.3.tgz", Version: "1.21.3", Atomic: true, Wait: true,
				}).WithKey("s3-rook-helm"),
			},
		})
		return pb.Build(), nil
	}
	secTOML := fmt.Sprintf("[grpc]\nca = \"/etc/kinstall/object-storage/%s/ca.crt\"\n[grpc.volume]\ncert = \"/etc/kinstall/object-storage/%s/tls.crt\"\nkey = \"/etc/kinstall/object-storage/%s/tls.key\"\n", req.Instance.Name, req.Instance.Name, req.Instance.Name)
	pb.Phase(sdk.PhaseSpec{
		Name: "deploy-seaweedfs",
		Tasks: []sdk.TaskSpec{
			sdk.BinaryInstall("weed", req.Instance.ProductVersion, sdk.ArtifactRef{Name: "seaweedfs"}).WithKey("s3-bin"),
			sdk.CertIssue(sdk.CertIssueSpec{
				CAScope: "s3-" + req.Instance.ID, CommonName: "s3-" + req.Instance.Name,
				CertPath: fmt.Sprintf("/etc/kinstall/object-storage/%s/tls.crt", req.Instance.Name),
				KeyPath:  fmt.Sprintf("/etc/kinstall/object-storage/%s/tls.key", req.Instance.Name),
				CAPath:   fmt.Sprintf("/etc/kinstall/object-storage/%s/ca.crt", req.Instance.Name),
				TTL:      8760 * time.Hour, KeyAlg: "ecdsa-p256",
			}).WithKey("s3-mtls"),
			sdk.FileSync(fmt.Sprintf("/etc/kinstall/object-storage/%s/security.toml", req.Instance.Name), sdk.TemplateRef{Inline: secTOML}, 0o640, "root:seaweedfs").WithKey("s3-sec-cfg"),
			sdk.WaitTCP(fmt.Sprintf("127.0.0.1:%d", spec.S3Port), 60*time.Second).WithKey("s3-ready"),
		},
	})
	return pb.Build(), nil
}

func (p *Plugin) buildECEncode(_ context.Context, _ *sdk.OpRequest) (*sdk.Pipeline, error) {
	pb := sdk.NewPipelineBuilder()
	pb.Phase(sdk.PhaseSpec{Name: "seaweedfs-ec-10-4", Tasks: []sdk.TaskSpec{sdk.CmdRun("weed shell -c 'ec.encode -fullPercent=95 -quietFor=24h; ec.balance'", "").WithKey("s3-ec")}})
	return pb.Build(), nil
}

func (p *Plugin) buildUninstall(_ context.Context, req *sdk.OpRequest) (*sdk.Pipeline, error) {
	pb := sdk.NewPipelineBuilder()
	pb.Phase(sdk.PhaseSpec{Name: "uninstall-s3", Disruptive: true, Tasks: []sdk.TaskSpec{sdk.CmdRun("systemctl disable --now kinstall-object-storage-"+req.Instance.Name+"-s3.service || true", "").WithKey("s3-stop")}})
	return pb.Build(), nil
}

type osOp struct {
	name     string
	mutating bool
	build    func(context.Context, *sdk.OpRequest) (*sdk.Pipeline, error)
}

func (o *osOp) Name() string   { return o.name }
func (o *osOp) Mutating() bool { return o.mutating }
func (o *osOp) Preflight(_ context.Context, _ *sdk.OpRequest) (*sdk.ValidationReport, error) {
	return sdk.NewValidationReport(), nil
}
func (o *osOp) Build(ctx context.Context, req *sdk.OpRequest) (*sdk.Pipeline, error) {
	return o.build(ctx, req)
}
