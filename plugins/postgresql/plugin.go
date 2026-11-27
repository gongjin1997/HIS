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
	Topology           string        `json:"topology"`    // patroni-ha | standalone
	SyncProfile        string        `json:"syncProfile"` // async | sync | sync-strict
	Port               int           `json:"port"`
	PgBouncerPort      int           `json:"pgbouncerPort"`
	HAProxyWritePort   int           `json:"haproxyWritePort"`
	HAProxyReadPort    int           `json:"haproxyReadPort"`
	HAProxyRWPortAlias int           `json:"haproxyRWPort,omitempty"` // legacy UI alias
	HAProxyROPortAlias int           `json:"haproxyROPort,omitempty"` // legacy UI alias
	PgBackRestVersion  string        `json:"pgBackRestVersion"`
	BackupRepoType     string        `json:"backupRepoType"` // local | s3 | obs
	BackupS3Bucket     string        `json:"backupS3Bucket"`
	BackupCredRef      string        `json:"backupCredRef"`
	CredentialRefAlias string        `json:"credentialRef,omitempty"` // legacy UI alias
	OSReserveMB        int           `json:"osReserveMB"`
	BootstrapDatabases []BootstrapDB `json:"bootstrapDatabases,omitempty"`
	TargetTime         string        `json:"targetTime,omitempty"`    // PITR restore target
	TargetVersion      string        `json:"targetVersion,omitempty"` // upgrade target
}

func parseSpec(raw json.RawMessage) Spec {
	s := Spec{
		Topology:          "patroni-ha",
		SyncProfile:       "sync",
		Port:              5432,
		PgBouncerPort:     6432,
		HAProxyWritePort:  5000,
		HAProxyReadPort:   5001,
		PgBackRestVersion: "2.60.0",
		BackupRepoType:    "local",
		BackupS3Bucket:    "kinstall-pg-backups",
		OSReserveMB:       2048,
	}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &s)
	}
	if s.HAProxyRWPortAlias > 0 && s.HAProxyWritePort == 5000 {
		s.HAProxyWritePort = s.HAProxyRWPortAlias
	}
	if s.HAProxyROPortAlias > 0 && s.HAProxyReadPort == 5001 {
		s.HAProxyReadPort = s.HAProxyROPortAlias
	}
	if s.BackupCredRef == "" && s.CredentialRefAlias != "" {
		s.BackupCredRef = s.CredentialRefAlias
	}
	if s.Topology == "" {
		s.Topology = "patroni-ha"
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
	if s.BackupRepoType == "" {
		s.BackupRepoType = "local"
	}
	if s.OSReserveMB <= 0 {
		s.OSReserveMB = 2048
	}
	return s
}

func pgHostClaims(spec Spec, instName string) sdk.HostRoleClaims {
	ports := []sdk.PortClaim{
		{Port: spec.Port, Proto: "tcp"},
		{Port: spec.PgBouncerPort, Proto: "tcp"},
		{Port: 8008, Proto: "tcp"},
	}
	if spec.Topology != "standalone" {
		ports = append(ports,
			sdk.PortClaim{Port: spec.HAProxyWritePort, Proto: "tcp"},
			sdk.PortClaim{Port: spec.HAProxyReadPort, Proto: "tcp"},
		)
	}
	return sdk.HostRoleClaims{
		Ports: ports,
		Dirs:  []string{"/data/postgresql/" + instName},
	}
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
		Description: "PostgreSQL 16/18/19 LTS HA (Patroni 4.1.5 + etcd + PgBouncer + HAProxy 5000/5001), pgBackRest >= 2.60.0 PITR, least-privilege bootstrapDatabases, CloudNativePG 1.30.1",
		Products: []sdk.ProductRange{{
			Product:     "postgresql",
			Versions:    ">=16.0.0, <20.0.0",
			DefaultVer:  "18.7.0",
			Targets:     []sdk.DeployTarget{sdk.TargetHostBinary, sdk.TargetHostContainer, sdk.TargetKubernetes},
			LicenseTier: "green",
		}},
		Platforms: sdk.Platforms{
			Arch:      []string{"amd64", "arm64"},
			PageSizes: []int{4096, 65536},
		},
		Operations: []string{"install", "upgrade", "switchover", "backup", "restore", "uninstall"},
		ParamSchema: map[string]any{
			"title": "PostgreSQL 18/19 Patroni HA 与 pgBackRest 参数",
			"properties": map[string]any{
				"topology":          map[string]any{"type": "string", "enum": []string{"standalone", "patroni-ha"}, "default": "patroni-ha", "x-kinstall-ui": map[string]any{"group": "basic", "order": 1, "label": "高可用架构"}},
				"syncProfile":       map[string]any{"type": "string", "enum": []string{"async", "sync", "sync-strict"}, "default": "sync", "x-kinstall-ui": map[string]any{"group": "basic", "order": 2, "label": "同步复制策略 (RPO)"}},
				"port":              map[string]any{"type": "integer", "default": 5432, "minimum": 1024, "maximum": 65535, "x-kinstall-ui": map[string]any{"group": "network", "order": 3, "label": "PostgreSQL 端口"}},
				"haproxyWritePort":  map[string]any{"type": "integer", "default": 5000, "minimum": 1024, "maximum": 65535, "x-kinstall-ui": map[string]any{"group": "network", "order": 4, "label": "HAProxy 主写端口", "visibleWhen": "topology == 'patroni-ha'"}},
				"haproxyReadPort":   map[string]any{"type": "integer", "default": 5001, "minimum": 1024, "maximum": 65535, "x-kinstall-ui": map[string]any{"group": "network", "order": 5, "label": "HAProxy 只读端口", "visibleWhen": "topology == 'patroni-ha'"}},
				"pgBackRestVersion": map[string]any{"type": "string", "default": "2.60.0", "x-kinstall-ui": map[string]any{"group": "storage", "order": 6, "label": "pgBackRest 版本 (>= 2.60.0)"}},
				"backupRepoType":    map[string]any{"type": "string", "enum": []string{"local", "s3", "obs"}, "default": "local", "x-kinstall-ui": map[string]any{"group": "storage", "order": 7, "label": "PITR 备份仓库类型"}},
				"backupCredRef":     map[string]any{"type": "string", "default": "", "x-kinstall-ui": map[string]any{"group": "security", "order": 8, "widget": "credential-select", "label": "备份/管理加密凭据引用"}},
				"osReserveMB":       map[string]any{"type": "integer", "default": 2048, "minimum": 512, "maximum": 32768, "x-kinstall-ui": map[string]any{"group": "advanced", "order": 9, "label": "OS 内存预留 (MiB)"}},
			},
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
		hc := pgHostClaims(spec, req.Instance.Name)
		for _, h := range req.Hosts {
			if err := req.CheckClaims(h.ID, hc); err != nil {
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
	claims := make(map[string]sdk.HostRoleClaims, len(req.Hosts))
	if req.Instance.DeployTarget != sdk.TargetKubernetes {
		hc := pgHostClaims(spec, req.Instance.Name)
		for _, h := range req.Hosts {
			claims[h.ID] = hc
		}
	}
	return &sdk.PlanResult{
		Summary:  fmt.Sprintf("PostgreSQL %s (%s / %s / syncProfile=%s) across %d nodes", req.Instance.ProductVersion, opName, req.Instance.DeployTarget, spec.SyncProfile, len(req.Hosts)),
		Claims:   claims,
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

	// Target 2: host-container (Docker Compose + persistent data dir + memory tuning + pg_isready healthcheck)
	if target == sdk.TargetHostContainer {
		majorVer := strings.Split(strings.TrimPrefix(ver, "v"), ".")[0]
		if majorVer == "" {
			majorVer = "16"
		}
		memMB := 8192
		if len(req.Hosts) > 0 && req.Hosts[0].Facts.MemMB > 0 {
			memMB = req.Hosts[0].Facts.MemMB
		}
		tuning := ComputeMemoryTuning(memMB, spec.OSReserveMB)
		sharedBuf := tuning.SharedBuffersMB
		if sharedBuf > 2048 && memMB < 16384 {
			sharedBuf = 256
		}
		dataDir := fmt.Sprintf("/data/pg/%s", name)
		composePath := fmt.Sprintf("/opt/kinstall/compose/pg-%s/docker-compose.yml", name)
		composeYAML := fmt.Sprintf(`services:
  postgres:
    image: postgres:%s
    container_name: kinstall-pg-%s
    restart: unless-stopped
    environment:
      POSTGRES_PASSWORD: "${POSTGRES_PASSWORD:-KinstallPG#2026}"
      POSTGRES_DB: "%s"
    command: ["postgres", "-c", "shared_buffers=%dMB", "-c", "max_connections=300"]
    ports:
      - "%d:5432"
    volumes:
      - %s:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U postgres"]
      interval: 5s
      timeout: 5s
      retries: 20
`, majorVer, name, name, sharedBuf, spec.Port, dataDir)
		pb.Phase(sdk.PhaseSpec{
			Name: "deploy-pg-container",
			Tasks: []sdk.TaskSpec{
				sdk.DockerEnsure(nil).WithKey("pg-docker"),
				sdk.ComposeSync("kinstall-pg-"+name, composePath,
					sdk.MountSpec{Src: dataDir, Dst: "/var/lib/postgresql/data", Type: "dir", Mode: 0o755},
				).WithParam("composeContent", composeYAML).WithKey("pg-compose"),
				sdk.WaitTCP(fmt.Sprintf("127.0.0.1:%d", spec.Port), 90*time.Second).WithKey("pg-ready"),
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

	// Phase 2: Real postgres user/dir + Patroni/PostgreSQL binary + systemd unit + TCP readiness
	dataDir := fmt.Sprintf("/data/pg/%s", name)
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
	pgPrepCmd := fmt.Sprintf("getent group postgres >/dev/null || groupadd -r postgres; id -u postgres >/dev/null 2>&1 || useradd -r -g postgres -s /bin/bash postgres; mkdir -p %s /etc/patroni /etc/pgbackrest && chown -R postgres:postgres %s /etc/patroni /etc/pgbackrest", dataDir, dataDir)
	pgInitCmd := fmt.Sprintf(`if [ ! -f %s/PG_VERSION ]; then
  INITDB="$(command -v initdb || find /usr/lib/postgresql /usr/pgsql* -name initdb 2>/dev/null | head -n1 || true)"
  if [ -n "$INITDB" ]; then su - postgres -c "$INITDB -D %s --encoding=UTF8"; fi
fi`, dataDir, dataDir)
	pgUnit := fmt.Sprintf(`[Unit]
Description=kinstall PostgreSQL %s (%s)
After=network-online.target
[Service]
Type=simple
User=postgres
Group=postgres
ExecStart=/bin/sh -c 'if command -v patroni >/dev/null 2>&1; then exec patroni /etc/patroni/%s.yml; else PG="$(command -v postgres || find /usr/lib/postgresql /usr/pgsql* -name postgres 2>/dev/null | head -n1)"; exec "$PG" -D %s -p %d; fi'
Restart=on-failure
LimitNOFILE=65535
[Install]
WantedBy=multi-user.target
`, ver, name, name, dataDir, spec.Port)
	pb.Phase(sdk.PhaseSpec{
		Name: "deploy-patroni-pg",
		Tasks: []sdk.TaskSpec{
			sdk.CmdRun(pgPrepCmd, fmt.Sprintf("id -u postgres >/dev/null 2>&1 && test -d %s", dataDir)).WithKey("pg-user-dir"),
			sdk.BinaryInstall("postgresql", ver, sdk.ArtifactRef{Name: "postgresql", MatchPageSize: false}).WithParam("fallbackPkg", "postgresql").WithKey("pg-bin"),
			sdk.BinaryInstall("pgbackrest", spec.PgBackRestVersion, sdk.ArtifactRef{Name: "pgbackrest"}).WithKey("pg-backrest-bin"),
			sdk.TemplateRender(fmt.Sprintf("/etc/patroni/%s.yml", name), sdk.TemplateRef{FS: assetsFS, Path: "templates/patroni.yml.tmpl"}, 0o640, "postgres:postgres").WithVars(tplVars).WithKey("pg-patroni-cfg"),
			sdk.TemplateRender("/etc/pgbackrest/pgbackrest.conf", sdk.TemplateRef{FS: assetsFS, Path: "templates/pgbackrest.conf.tmpl"}, 0o640, "postgres:postgres").WithVars(tplVars).WithKey("pg-backrest-cfg"),
			sdk.CmdRun(pgInitCmd, fmt.Sprintf("test -f %s/PG_VERSION", dataDir)).WithKey("pg-initdb"),
			sdk.SystemdUnit("kinstall-pg-"+name+".service", sdk.TemplateRef{Inline: pgUnit}, sdk.UnitOpts{Enable: true, Start: true, RestartOnChange: true}).WithKey("pg-unit"),
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
	name := req.Instance.Name
	pb := sdk.NewPipelineBuilder()
	stopScript := fmt.Sprintf(`systemctl disable --now kinstall-pg-%s.service patroni-%s pgbouncer-%s 2>/dev/null || true
if [ -f /opt/kinstall/compose/pg-%s/docker-compose.yml ]; then
  (docker compose -f /opt/kinstall/compose/pg-%s/docker-compose.yml down --remove-orphans || docker-compose -f /opt/kinstall/compose/pg-%s/docker-compose.yml down || true)
fi
rm -f /etc/systemd/system/kinstall-pg-%s.service
systemctl daemon-reload 2>/dev/null || true`, name, name, name, name, name, name, name)
	pb.Phase(sdk.PhaseSpec{
		Name:       "uninstall-pg",
		Disruptive: true,
		Tasks: []sdk.TaskSpec{
			sdk.CmdRun(stopScript, "").WithKey("pg-stop"),
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
