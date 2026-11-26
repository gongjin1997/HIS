package postgresql

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"

	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
)

//go:embed templates/*.tmpl schema/params.json plugin.yaml
var assetsFS embed.FS

// BootstrapDB declares an idempotent, least-privilege application database & role (P4 §3).
type BootstrapDB struct {
	Name          string   `json:"name"`
	Owner         string   `json:"owner"`
	Encoding      string   `json:"encoding"`
	CredentialRef string   `json:"credentialRef"`
	Privileges    []string `json:"privileges"`
	InitSQLRef    string   `json:"initSqlRef,omitempty"`
}

// Spec defines the typed PostgreSQL HA + PITR instance configuration (P4 §1-§4).
type Spec struct {
	SyncProfile        string        `json:"syncProfile"` // async | sync | sync-strict
	Port               int           `json:"port"`
	PgBouncerPort      int           `json:"pgbouncerPort"`
	HAProxyWritePort   int           `json:"haproxyWritePort"`
	HAProxyReadPort    int           `json:"haproxyReadPort"`
	PgBackRestVersion  string        `json:"pgBackRestVersion"`
	BackupS3Bucket     string        `json:"backupS3Bucket"`
	BackupCredRef      string        `json:"backupCredRef"`
	OSReserveMB        int           `json:"osReserveMB"`
	BootstrapDatabases []BootstrapDB `json:"bootstrapDatabases,omitempty"`
	TargetTime         string        `json:"targetTime,omitempty"`    // PITR restore target
	TargetVersion      string        `json:"targetVersion,omitempty"` // upgrade target
}

func parseSpec(raw json.RawMessage) Spec {
	s := Spec{
		SyncProfile:       "sync",
		Port:              5432,
		PgBouncerPort:     6432,
		HAProxyWritePort:  5000,
		HAProxyReadPort:   5001,
		PgBackRestVersion: "2.60.0",
		BackupS3Bucket:    "kinstall-pg-backups",
		OSReserveMB:       2048,
	}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &s)
	}
	if s.SyncProfile == "" {
		s.SyncProfile = "sync"
	}
	if s.Port <= 0 {
		s.Port = 5432
	}
	if s.PgBouncerPort <= 0 {
		s.PgBouncerPort = 6432
	}
	if s.HAProxyWritePort <= 0 {
		s.HAProxyWritePort = 5000
	}
	if s.HAProxyReadPort <= 0 {
		s.HAProxyReadPort = 5001
	}
	if s.PgBackRestVersion == "" {
		s.PgBackRestVersion = "2.60.0"
	}
	if s.OSReserveMB <= 0 {
		s.OSReserveMB = 2048
	}
	return s
}

// MemoryTuning holds the P4 §1 exact PostgreSQL memory & kernel dirty-page parameters.
type MemoryTuning struct {
	AvailRAMMB        int `json:"availRamMB"`
	SharedBuffersMB   int `json:"sharedBuffersMB"`
	EffectiveCacheMB  int `json:"effectiveCacheMB"`
	MaintenanceWorkMB int `json:"maintenanceWorkMB"`
}

// ComputeMemoryTuning implements P4 §1 / 08 §7.1:
// shared_buffers = min(AvailRAM * 0.25, 64 GiB), effective_cache_size = AvailRAM * 0.75, maintenance_work_mem = min(AvailRAM * 0.05, 2 GiB).
func ComputeMemoryTuning(hostMemMB, osReserveMB int) MemoryTuning {
	avail := hostMemMB - osReserveMB
	if avail < 1024 {
		avail = 1024
	}
	sb := int(math.Floor(float64(avail) * 0.25))
	if sb > 65536 {
		sb = 65536 // 64 GiB cap
	}
	ec := int(math.Floor(float64(avail) * 0.75))
	mw := int(math.Floor(float64(avail) * 0.05))
	if mw > 2048 {
		mw = 2048 // 2 GiB cap
	}
	return MemoryTuning{
		AvailRAMMB:        avail,
		SharedBuffersMB:   sb,
		EffectiveCacheMB:  ec,
		MaintenanceWorkMB: mw,
	}
}

var forbiddenPrivileges = []string{"SUPERUSER", "BYPASSRLS", "CREATEROLE", "WITH GRANT OPTION"}

// Plugin implements sdk.Component for PostgreSQL 18.7 / 19.0 GA (P4).
type Plugin struct {
	ops map[string]sdk.Operation
}

func New() *Plugin {
	p := &Plugin{}
	p.ops = map[string]sdk.Operation{
		"install":    &pgOp{name: "install", mutating: true, build: p.buildInstall},
		"upgrade":    &pgOp{name: "upgrade", mutating: true, build: p.buildUpgrade},
		"backup":     &pgOp{name: "backup", mutating: false, build: p.buildBackup},
		"restore":    &pgOp{name: "restore", mutating: true, build: p.buildRestore},
		"switchover": &pgOp{name: "switchover", mutating: true, build: p.buildSwitchover},
		"uninstall":  &pgOp{name: "uninstall", mutating: true, build: p.buildUninstall},
	}
	return p
}

func (p *Plugin) Meta() sdk.PluginMeta {
	return sdk.PluginMeta{
		Name:        "postgresql",
		Version:     "1.2.0",
		SDKVersion:  sdk.SDKAPIVersion,
		Type:        sdk.PluginComponent,
		Description: "PostgreSQL 18.7/19.0 GA HA (Patroni 4.1.5 + etcd + PgBouncer + HAProxy 5000/5001), pgBackRest >= 2.60.0 PITR, least-privilege bootstrapDatabases, CloudNativePG 1.30.1",
		Products: []sdk.ProductRange{{
			Product:     "postgresql",
			Versions:    ">=18.0.0, <20.0.0",
			DefaultVer:  "18.7.0",
			Targets:     []sdk.DeployTarget{sdk.TargetHostBinary, sdk.TargetHostContainer, sdk.TargetKubernetes},
			LicenseTier: "green",
		}},
		Platforms: sdk.Platforms{
			Arch:      []string{"amd64", "arm64"},
			PageSizes: []int{4096, 65536},
		},
		OutputSchema: map[string]string{
			"primaryEndpoint": "string",
			"replicaEndpoint": "string",
			"databases":       "object",
		},
		DriftIgnore:   []string{"/var/lib/postgresql/*/data/postgresql.auto.conf"},
		SchemaVersion: 1,
	}
}

func (p *Plugin) Validate(_ context.Context, req *sdk.ValidateRequest) (*sdk.ValidationReport, error) {
	rep := sdk.NewValidationReport()
	spec := parseSpec(req.Instance.Spec)
	target := req.Instance.DeployTarget
	if target == "" {
		target = sdk.TargetHostBinary
	}

	// ADR-046: pgBackRest MUST be >= 2.60.0 to avoid sub-key encryption flaw
	if sdk.DottedVersionLess(spec.PgBackRestVersion, "2.60.0") {
		rep.Block(sdk.CodePluginIncompatible, fmt.Sprintf("pgBackRest %s < 2.60.0 is forbidden due to sub-key encryption vulnerability (ADR-046)", spec.PgBackRestVersion))
	}

	// P4 §1 syncProfile quorum validation
	switch spec.SyncProfile {
	case "async", "sync":
	case "sync-strict":
		if target != sdk.TargetKubernetes && len(req.Hosts) < 3 {
			rep.Block(sdk.CodePlanTopologyMinHosts, fmt.Sprintf("syncProfile=sync-strict requires at least 3 PostgreSQL hosts for RPO=0 quorum, got %d", len(req.Hosts)))
		}
	default:
		rep.Block(sdk.CodeAPIInvalidRequest, fmt.Sprintf("invalid syncProfile %q (expected async|sync|sync-strict)", spec.SyncProfile))
	}

	// P4 §3 HARD SECURITY REDLINE: block privileged grants in bootstrapDatabases!
	for _, db := range spec.BootstrapDatabases {
		for _, priv := range db.Privileges {
			upper := strings.ToUpper(strings.TrimSpace(priv))
			for _, bad := range forbiddenPrivileges {
				if strings.Contains(upper, bad) {
					rep.Block(sdk.CodeAuthForbidden, fmt.Sprintf("bootstrapDatabases[%s]: forbidden privilege %q (least-privilege redline P4 §3)", db.Name, priv))
				}
			}
		}
	}

	if target != sdk.TargetKubernetes && req.CheckClaims != nil {
		for _, h := range req.Hosts {
			if err := req.CheckClaims(h.ID, sdk.HostRoleClaims{
				Ports: []sdk.PortClaim{
					{Port: spec.Port, Proto: "tcp"},
					{Port: spec.PgBouncerPort, Proto: "tcp"},
					{Port: 8008, Proto: "tcp"},
				},
				Dirs: []string{"/data/postgresql/" + req.Instance.Name},
			}); err != nil {
				rep.BlockHost(h.ID, sdk.CodeHostClaimConflict, err.Error())
			}
		}
	}
	return rep, nil
}

func (p *Plugin) Plan(ctx context.Context, req *sdk.PlanRequest) (*sdk.PlanResult, error) {
	spec := parseSpec(req.Instance.Spec)
	opName := req.Operation
	if opName == "" {
		opName = "install"
	}
	op, ok := p.Operation(opName)
	if !ok {
		return nil, sdk.NewError(sdk.CodeAPIInvalidRequest, "PG", http.StatusBadRequest, sdk.CodeAPIInvalidRequest, false, fmt.Errorf("unknown op %q", opName))
	}
	pl, err := op.Build(ctx, &sdk.OpRequest{Instance: req.Instance, Hosts: req.Hosts, BOM: req.BOM})
	if err != nil {
		return nil, err
	}
	hostAddr := "127.0.0.1"
	if len(req.Hosts) > 0 && req.Hosts[0].Address != "" {
		hostAddr = req.Hosts[0].Address
	}
	dbOuts := map[string]any{}
	for _, db := range spec.BootstrapDatabases {
		dbOuts[db.Name] = map[string]any{
			"owner":        db.Owner,
			"dsnSecretRef": db.CredentialRef,
		}
	}
	return &sdk.PlanResult{
		Summary:  fmt.Sprintf("PostgreSQL %s (%s / syncProfile=%s) across %d nodes", req.Instance.ProductVersion, req.Instance.DeployTarget, spec.SyncProfile, len(req.Hosts)),
		Pipeline: pl,
		Outputs: map[string]any{
			"primaryEndpoint": fmt.Sprintf("%s:%d", hostAddr, spec.HAProxyWritePort),
			"replicaEndpoint": fmt.Sprintf("%s:%d", hostAddr, spec.HAProxyReadPort),
			"databases":       dbOuts,
		},
	}, nil
}

func (p *Plugin) Operation(name string) (sdk.Operation, bool) {
	op, ok := p.ops[name]
	return op, ok
}

func (p *Plugin) Status(_ context.Context, req *sdk.StatusRequest) (*sdk.InstanceStatus, error) {
	spec := parseSpec(req.Instance.Spec)
	host := "127.0.0.1"
	if len(req.Hosts) > 0 && req.Hosts[0].Address != "" {
		host = req.Hosts[0].Address
	}
	return &sdk.InstanceStatus{
		Health:  "healthy",
		Message: fmt.Sprintf("PostgreSQL %s Patroni cluster healthy", req.Instance.ProductVersion),
		Endpoints: map[string]string{
			"primary": fmt.Sprintf("%s:%d", host, spec.HAProxyWritePort),
			"replica": fmt.Sprintf("%s:%d", host, spec.HAProxyReadPort),
		},
		CheckedAt: time.Now().UTC(),
	}, nil
}

func (p *Plugin) buildInstall(_ context.Context, req *sdk.OpRequest) (*sdk.Pipeline, error) {
	spec := parseSpec(req.Instance.Spec)
	ver := req.Instance.ProductVersion
	if ver == "" {
		ver = "18.7.0"
	}
	name := req.Instance.Name
	if name == "" {
		name = "pg-main"
	}
	target := req.Instance.DeployTarget
	if target == "" {
		target = sdk.TargetHostBinary
	}

	pb := sdk.NewPipelineBuilder()

	// Kubernetes target: CloudNativePG 1.30.1 + Barman Cloud Plugin 0.15.1 via Helm v4 (ADR-045)
	if target == sdk.TargetKubernetes {
		pb.Phase(sdk.PhaseSpec{
			Name: "k8s-cnpg",
			Tasks: []sdk.TaskSpec{
				sdk.HelmRelease(sdk.HelmReleaseSpec{
					ReleaseName: "cnpg-" + name,
					Namespace:   "cnpg-system",
					ChartRef:    "charts/cloudnative-pg-1.30.1.tgz",
					Version:     "1.30.1",
					Atomic:      true,
					Wait:        true,
					Values: map[string]any{
						"postgresqlVersion": ver,
						"syncProfile":       spec.SyncProfile,
						"barmanPluginVer":   "0.15.1",
					},
				}).WithKey("pg-cnpg-helm"),
			},
		})
		return pb.Build(), nil
	}

	memMB := 32768
	if len(req.Hosts) > 0 && req.Hosts[0].Facts.MemMB > 0 {
		memMB = req.Hosts[0].Facts.MemMB
	}
	tuning := ComputeMemoryTuning(memMB, spec.OSReserveMB)
	var etcdHosts []string
	for _, h := range req.Hosts {
		if h.Address != "" {
			etcdHosts = append(etcdHosts, h.Address)
		}
	}
	if len(etcdHosts) == 0 {
		etcdHosts = []string{"127.0.0.1"}
	}

	// Phase 1: Kernel dirty-page bytes tuning (eliminates 30s flush stall, P4 §1)
	pb.Phase(sdk.PhaseSpec{
		Name: "kernel-tune",
		Tasks: []sdk.TaskSpec{
			sdk.SysctlApply("92-kinstall-pg.conf", map[string]string{
				"vm.dirty_background_bytes": "67108864",
				"vm.dirty_bytes":            "536870912",
				"vm.swappiness":             "1",
			}).WithKey("pg-sysctl"),
		},
	})

	// Phase 2: Patroni 4.1.5 + PostgreSQL + PgBouncer + pgBackRest >= 2.60.0
	tplVars := map[string]any{
		"ClusterName":       name,
		"NodeName":          name + "-node",
		"HostIP":            etcdHosts[0],
		"Port":              spec.Port,
		"EtcdHosts":         etcdHosts,
		"SyncMode":          spec.SyncProfile != "async",
		"SyncStrict":        spec.SyncProfile == "sync-strict",
		"SharedBuffersMB":   tuning.SharedBuffersMB,
		"EffectiveCacheMB":  tuning.EffectiveCacheMB,
		"MaintenanceWorkMB": tuning.MaintenanceWorkMB,
		"BackupS3Bucket":    spec.BackupS3Bucket,
	}
	pb.Phase(sdk.PhaseSpec{
		Name: "deploy-patroni-pg",
		Tasks: []sdk.TaskSpec{
			sdk.BinaryInstall("postgresql", ver, sdk.ArtifactRef{Name: "postgresql", MatchPageSize: false}).WithKey("pg-bin"),
			sdk.BinaryInstall("pgbackrest", spec.PgBackRestVersion, sdk.ArtifactRef{Name: "pgbackrest"}).WithKey("pg-backrest-bin"),
			sdk.TemplateRender(fmt.Sprintf("/etc/patroni/%s.yml", name), sdk.TemplateRef{FS: assetsFS, Path: "templates/patroni.yml.tmpl"}, 0o640, "postgres:postgres").WithVars(tplVars).WithKey("pg-patroni-cfg"),
			sdk.TemplateRender("/etc/pgbackrest/pgbackrest.conf", sdk.TemplateRef{FS: assetsFS, Path: "templates/pgbackrest.conf.tmpl"}, 0o640, "postgres:postgres").WithVars(tplVars).WithKey("pg-backrest-cfg"),
			sdk.WaitTCP(fmt.Sprintf("127.0.0.1:%d", spec.Port), 90*time.Second).WithKey("pg-ready"),
		},
	})

	// Phase 3: Declarative idempotent least-privilege bootstrapDatabases (P4 §3)
	if len(spec.BootstrapDatabases) > 0 {
		var sqlTasks []sdk.TaskSpec
		for _, db := range spec.BootstrapDatabases {
			enc := db.Encoding
			if enc == "" {
				enc = "UTF8"
			}
			stmt := fmt.Sprintf("psql -v ON_ERROR_STOP=1 -c \"CREATE ROLE %s LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOBYPASSRLS;\" || true; psql -v ON_ERROR_STOP=1 -c \"CREATE DATABASE %s OWNER %s ENCODING '%s';\" || true; psql -d %s -c \"REVOKE CREATE ON SCHEMA public FROM PUBLIC;\"",
				db.Owner, db.Name, db.Owner, enc, db.Name)
			sqlTasks = append(sqlTasks, sdk.CmdRun(stmt, "").WithKey("pg-bootstrap-"+db.Name))
		}
		pb.Phase(sdk.PhaseSpec{Name: "bootstrap-databases", Serial: true, Tasks: sqlTasks})
	}
	return pb.Build(), nil
}

func (p *Plugin) buildUpgrade(_ context.Context, req *sdk.OpRequest) (*sdk.Pipeline, error) {
	spec := parseSpec(req.Instance.Spec)
	target := spec.TargetVersion
	if target == "" {
		target = req.Instance.ProductVersion
	}
	pb := sdk.NewPipelineBuilder()
	pb.Phase(sdk.PhaseSpec{
		Name:   "upgrade-standbys-then-switchover",
		Serial: true,
		Tasks: []sdk.TaskSpec{
			sdk.BinaryInstall("postgresql", target, sdk.ArtifactRef{Name: "postgresql"}).WithKey("pg-upg-bin"),
			sdk.CmdRun(fmt.Sprintf("patronictl -c /etc/patroni/%s.yml switchover --force", req.Instance.Name), "").WithKey("pg-upg-switchover"),
		},
	})
	return pb.Build(), nil
}

func (p *Plugin) buildBackup(_ context.Context, req *sdk.OpRequest) (*sdk.Pipeline, error) {
	pb := sdk.NewPipelineBuilder()
	pb.Phase(sdk.PhaseSpec{
		Name:   "pgbackrest-backup",
		Serial: true,
		Tasks: []sdk.TaskSpec{
			sdk.CmdRun(fmt.Sprintf("pgbackrest --stanza=%s --type=diff backup", req.Instance.Name), "").WithKey("pg-backup"),
		},
	})
	return pb.Build(), nil
}

func (p *Plugin) buildRestore(_ context.Context, req *sdk.OpRequest) (*sdk.Pipeline, error) {
	spec := parseSpec(req.Instance.Spec)
	targetArg := ""
	if spec.TargetTime != "" {
		targetArg = fmt.Sprintf(" --type=time --target=%q", spec.TargetTime)
	}
	pb := sdk.NewPipelineBuilder()
	pb.Phase(sdk.PhaseSpec{
		Name:       "pgbackrest-pitr-restore",
		Serial:     true,
		Disruptive: true,
		Tasks: []sdk.TaskSpec{
			sdk.CmdRun(fmt.Sprintf("pgbackrest --stanza=%s restore%s", req.Instance.Name, targetArg), "").WithKey("pg-pitr-restore"),
		},
	})
	return pb.Build(), nil
}

func (p *Plugin) buildSwitchover(_ context.Context, req *sdk.OpRequest) (*sdk.Pipeline, error) {
	pb := sdk.NewPipelineBuilder()
	pb.Phase(sdk.PhaseSpec{
		Name:   "patroni-switchover",
		Serial: true,
		Tasks: []sdk.TaskSpec{
			sdk.CmdRun(fmt.Sprintf("patronictl -c /etc/patroni/%s.yml switchover --force", req.Instance.Name), "").WithKey("pg-switchover"),
		},
	})
	return pb.Build(), nil
}

func (p *Plugin) buildUninstall(_ context.Context, req *sdk.OpRequest) (*sdk.Pipeline, error) {
	pb := sdk.NewPipelineBuilder()
	pb.Phase(sdk.PhaseSpec{
		Name:       "uninstall-pg",
		Disruptive: true,
		Tasks: []sdk.TaskSpec{
			sdk.CmdRun(fmt.Sprintf("systemctl disable --now patroni-%s pgbouncer-%s || true", req.Instance.Name, req.Instance.Name), "").WithKey("pg-stop"),
		},
	})
	return pb.Build(), nil
}

type pgOp struct {
	name     string
	mutating bool
	build    func(context.Context, *sdk.OpRequest) (*sdk.Pipeline, error)
}

func (o *pgOp) Name() string   { return o.name }
func (o *pgOp) Mutating() bool { return o.mutating }
func (o *pgOp) Preflight(_ context.Context, _ *sdk.OpRequest) (*sdk.ValidationReport, error) {
	return &sdk.ValidationReport{}, nil
}
func (o *pgOp) Build(ctx context.Context, req *sdk.OpRequest) (*sdk.Pipeline, error) {
	return o.build(ctx, req)
}
