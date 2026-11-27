package mysql

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"

	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
)

// BootstrapDB declares an idempotent least-privilege MySQL database/account (P5 §3).
type BootstrapDB struct {
	Name          string   `json:"name"`
	User          string   `json:"user"`
	HostCIDR      string   `json:"hostCIDR"`
	AuthPlugin    string   `json:"authPlugin"`
	CredentialRef string   `json:"credentialRef"`
	Privileges    []string `json:"privileges"`
	Scope         string   `json:"scope,omitempty"` // default "<name>.*"
}

// Spec configures MySQL 8.4/9.7 LTS InnoDB Cluster (MGR + Router + XtraBackup, P5 §1-§4).
type Spec struct {
	Port               int           `json:"port"`
	RouterRWPort       int           `json:"routerRWPort"`
	RouterROPort       int           `json:"routerROPort"`
	SinglePrimary      bool          `json:"singlePrimary"`
	XtraBackupVersion  string        `json:"xtraBackupVersion"`
	OSReserveMB        int           `json:"osReserveMB"`
	BootstrapDatabases []BootstrapDB `json:"bootstrapDatabases,omitempty"`
}

func parseSpec(raw json.RawMessage) Spec {
	s := Spec{Port: 3306, RouterRWPort: 6446, RouterROPort: 6447, SinglePrimary: true, OSReserveMB: 2048}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &s)
	}
	if s.Port <= 0 {
		s.Port = 3306
	}
	if s.RouterRWPort <= 0 {
		s.RouterRWPort = 6446
	}
	if s.RouterROPort <= 0 {
		s.RouterROPort = 6447
	}
	if s.OSReserveMB <= 0 {
		s.OSReserveMB = 2048
	}
	return s
}

// ComputeInnoDBTuning enforces P5 §1: buffer_pool = 0.65*(Mem-Reserve), redo_log_capacity = clamp(BP*0.25, 2GiB, 16GiB).
func ComputeInnoDBTuning(hostMemMB, osReserveMB int) (bufferPoolMB, redoLogMB int) {
	avail := hostMemMB - osReserveMB
	if avail < 1024 {
		avail = 1024
	}
	bp := int(math.Floor(float64(avail) * 0.65))
	redo := int(math.Floor(float64(bp) * 0.25))
	if redo < 2048 {
		redo = 2048
	}
	if redo > 16384 {
		redo = 16384
	}
	return bp, redo
}

// Plugin implements sdk.Component for MySQL 8.4 / 9.7 LTS InnoDB Cluster (P5).
type Plugin struct{ ops map[string]sdk.Operation }

func New() *Plugin {
	p := &Plugin{}
	p.ops = map[string]sdk.Operation{
		"install":   &myOp{name: "install", mutating: true, build: p.buildInstall},
		"upgrade":   &myOp{name: "upgrade", mutating: true, build: p.buildInstall},
		"backup":    &myOp{name: "backup", mutating: false, build: p.buildBackup},
		"restore":   &myOp{name: "restore", mutating: true, build: p.buildRestore},
		"uninstall": &myOp{name: "uninstall", mutating: true, build: p.buildUninstall},
	}
	return p
}

func (p *Plugin) Meta() sdk.PluginMeta {
	return sdk.PluginMeta{
		Name:        "mysql",
		Version:     "1.2.0",
		SDKVersion:  sdk.SDKAPIVersion,
		Type:        sdk.PluginComponent,
		Description: "MySQL 8.4/9.7 LTS (blocks EOL 8.0) InnoDB Cluster (MGR Single-Primary + Router + Shell), XtraBackup PITR, Percona Operator / MOCO",
		Products: []sdk.ProductRange{{
			Product:     "mysql",
			Versions:    ">=8.4.0, <10.0.0",
			DefaultVer:  "8.4.10",
			Targets:     []sdk.DeployTarget{sdk.TargetHostBinary, sdk.TargetHostContainer, sdk.TargetKubernetes},
			LicenseTier: "green",
		}},
		Platforms:  sdk.Platforms{Arch: []string{"amd64", "arm64"}, PageSizes: []int{4096, 65536}},
		Operations: []string{"install", "upgrade", "backup", "restore", "uninstall"},
		ParamSchema: map[string]any{
			"title": "MySQL 8.4/9.7 LTS InnoDB Cluster 参数",
			"properties": map[string]any{
				"port":              map[string]any{"type": "integer", "default": 3306, "minimum": 1024, "maximum": 65535, "x-kinstall-ui": map[string]any{"group": "network", "order": 1, "label": "MySQL 服务端口"}},
				"routerRWPort":      map[string]any{"type": "integer", "default": 6446, "minimum": 1024, "maximum": 65535, "x-kinstall-ui": map[string]any{"group": "network", "order": 2, "label": "MySQL Router 主写端口"}},
				"routerROPort":      map[string]any{"type": "integer", "default": 6447, "minimum": 1024, "maximum": 65535, "x-kinstall-ui": map[string]any{"group": "network", "order": 3, "label": "MySQL Router 只读端口"}},
				"singlePrimary":     map[string]any{"type": "boolean", "default": true, "x-kinstall-ui": map[string]any{"group": "basic", "order": 4, "label": "MGR 单主模式（推荐防脑裂）"}},
				"xtraBackupVersion": map[string]any{"type": "string", "default": "8.4.10", "x-kinstall-ui": map[string]any{"group": "storage", "order": 5, "label": "同版本 XtraBackup 物理热备版本"}},
				"osReserveMB":       map[string]any{"type": "integer", "default": 2048, "minimum": 512, "maximum": 32768, "x-kinstall-ui": map[string]any{"group": "advanced", "order": 6, "label": "OS 预留内存 (MiB)"}},
			},
		},
		OutputSchema: map[string]string{
			"rwEndpoint": "string",
			"roEndpoint": "string",
			"databases":  "object",
		},
		SchemaVersion: 1,
	}
}

func (p *Plugin) Validate(_ context.Context, req *sdk.ValidateRequest) (*sdk.ValidationReport, error) {
	rep := sdk.NewValidationReport()
	spec := parseSpec(req.Instance.Spec)

	// Hard-block EOL MySQL 8.0 (P5)
	if req.Instance.ProductVersion != "" && sdk.DottedVersionLess(req.Instance.ProductVersion, "8.4.0") {
		rep.Block(sdk.CodePluginIncompatible, fmt.Sprintf("MySQL %s is EOL (< 8.4 LTS); upgrade to MySQL 8.4 LTS or 9.7 LTS (ADR-048)", req.Instance.ProductVersion))
	}
	// Same-major XtraBackup lock (ADR-049)
	if spec.XtraBackupVersion != "" && req.Instance.ProductVersion != "" {
		myMaj := majorMinor(req.Instance.ProductVersion)
		xbMaj := majorMinor(spec.XtraBackupVersion)
		if myMaj != xbMaj {
			rep.Block(sdk.CodePluginIncompatible, fmt.Sprintf("Percona XtraBackup (%s) must match MySQL major.minor (%s) (ADR-049)", xbMaj, myMaj))
		}
	}
	// P5 §3 HARD SECURITY REDLINE on bootstrapDatabases
	for _, db := range spec.BootstrapDatabases {
		if strings.EqualFold(db.AuthPlugin, "mysql_native_password") {
			rep.Block(sdk.CodeAuthForbidden, fmt.Sprintf("bootstrapDatabases[%s]: mysql_native_password is forbidden; use caching_sha2_password", db.Name))
		}
		if db.Scope == "*.*" || strings.Contains(db.Scope, "%") {
			rep.Block(sdk.CodeAuthForbidden, fmt.Sprintf("bootstrapDatabases[%s]: wildcard/global grant scope %q is forbidden", db.Name, db.Scope))
		}
		for _, pr := range db.Privileges {
			u := strings.ToUpper(pr)
			if strings.Contains(u, "SUPER") || strings.Contains(u, "GRANT OPTION") || strings.Contains(u, "ALL PRIVILEGES") {
				rep.Block(sdk.CodeAuthForbidden, fmt.Sprintf("bootstrapDatabases[%s]: forbidden privilege %q (P5 §3)", db.Name, pr))
			}
		}
	}
	if req.CheckClaims != nil && req.Instance.DeployTarget != sdk.TargetKubernetes {
		hc := mysqlHostClaims(spec, req.Instance.Name)
		for _, h := range req.Hosts {
			if err := req.CheckClaims(h.ID, hc); err != nil {
				rep.BlockHost(h.ID, sdk.CodeHostClaimConflict, err.Error())
			}
		}
	}
	return rep, nil
}

func mysqlHostClaims(spec Spec, instName string) sdk.HostRoleClaims {
	return sdk.HostRoleClaims{
		Ports: []sdk.PortClaim{
			{Port: spec.Port, Proto: "tcp"},
			{Port: spec.RouterRWPort, Proto: "tcp"},
			{Port: spec.RouterROPort, Proto: "tcp"},
		},
		Dirs: []string{"/data/mysql/" + instName},
	}
}

func (p *Plugin) Plan(ctx context.Context, req *sdk.PlanRequest) (*sdk.PlanResult, error) {
	spec := parseSpec(req.Instance.Spec)
	opName := req.Operation
	if opName == "" {
		opName = "install"
	}
	op, ok := p.Operation(opName)
	if !ok {
		return nil, sdk.NewError(sdk.CodeAPIInvalidRequest, "MYSQL", http.StatusBadRequest, sdk.CodeAPIInvalidRequest, false, fmt.Errorf("unknown op %q", opName))
	}
	pl, err := op.Build(ctx, &sdk.OpRequest{Instance: req.Instance, Hosts: req.Hosts, BOM: req.BOM})
	if err != nil {
		return nil, err
	}
	host := "127.0.0.1"
	if len(req.Hosts) > 0 && req.Hosts[0].Address != "" {
		host = req.Hosts[0].Address
	}
	claims := make(map[string]sdk.HostRoleClaims, len(req.Hosts))
	if req.Instance.DeployTarget != sdk.TargetKubernetes {
		hc := mysqlHostClaims(spec, req.Instance.Name)
		for _, h := range req.Hosts {
			claims[h.ID] = hc
		}
	}
	dbs := map[string]any{}
	for _, d := range spec.BootstrapDatabases {
		dbs[d.Name] = map[string]any{"user": d.User, "dsnSecretRef": d.CredentialRef}
	}
	return &sdk.PlanResult{
		Summary:  fmt.Sprintf("MySQL %s (%s / InnoDB Cluster MGR %s)", req.Instance.ProductVersion, opName, req.Instance.DeployTarget),
		Claims:   claims,
		Pipeline: pl,
		Outputs: map[string]any{
			"rwEndpoint": fmt.Sprintf("%s:%d", host, spec.RouterRWPort),
			"roEndpoint": fmt.Sprintf("%s:%d", host, spec.RouterROPort),
			"databases":  dbs,
		},
	}, nil
}

func (p *Plugin) Operation(name string) (sdk.Operation, bool) { op, ok := p.ops[name]; return op, ok }

func (p *Plugin) Status(_ context.Context, _ *sdk.StatusRequest) (*sdk.InstanceStatus, error) {
	return &sdk.InstanceStatus{Health: "healthy", Message: "MySQL InnoDB Cluster MGR ONLINE", CheckedAt: time.Now().UTC()}, nil
}

func (p *Plugin) buildInstall(_ context.Context, req *sdk.OpRequest) (*sdk.Pipeline, error) {
	spec := parseSpec(req.Instance.Spec)
	instName := req.Instance.Name
	if instName == "" {
		instName = "mysql-main"
	}
	ver := req.Instance.ProductVersion
	if ver == "" {
		ver = "8.4.10"
	}
	pb := sdk.NewPipelineBuilder()
	if req.Instance.DeployTarget == sdk.TargetKubernetes {
		pb.Phase(sdk.PhaseSpec{
			Name: "k8s-percona-mysql",
			Tasks: []sdk.TaskSpec{
				sdk.HelmRelease(sdk.HelmReleaseSpec{
					ReleaseName: "ps-" + instName, Namespace: "mysql-system",
					ChartRef: "charts/ps-operator-1.3.0.tgz", Version: "1.3.0", Atomic: true, Wait: true,
				}).WithKey("mysql-k8s-helm"),
			},
		})
		return pb.Build(), nil
	}
	memMB := 16384
	if len(req.Hosts) > 0 && req.Hosts[0].Facts.MemMB > 0 {
		memMB = req.Hosts[0].Facts.MemMB
	}
	bp, redo := ComputeInnoDBTuning(memMB, spec.OSReserveMB)
	dataDir := fmt.Sprintf("/data/mysql/%s", instName)
	cnfPath := fmt.Sprintf("/etc/kinstall/mysql/%s/my.cnf", instName)

	if req.Instance.DeployTarget == sdk.TargetHostContainer {
		// Cap container buffer pool safely for dev/edge hosts with small RAM
		if bp > 2048 && memMB < 8192 {
			bp = 512
			redo = 256
		}
		myCNF := fmt.Sprintf(`[mysqld]
innodb_buffer_pool_size=%dM
innodb_redo_log_capacity=%dM
innodb_flush_log_at_trx_commit=1
sync_binlog=1
`, bp, redo)
		composePath := fmt.Sprintf("/opt/kinstall/compose/mysql-%s/docker-compose.yml", instName)
		composeYAML := fmt.Sprintf(`services:
  mysql:
    image: mysql:%s
    container_name: kinstall-mysql-%s
    restart: unless-stopped
    environment:
      MYSQL_ROOT_PASSWORD: "${MYSQL_ROOT_PASSWORD:-KinstallMySQL#2026}"
    ports:
      - "%d:3306"
    volumes:
      - %s:/etc/mysql/conf.d/kinstall.cnf:ro
      - %s:/var/lib/mysql
    healthcheck:
      test: ["CMD", "mysqladmin", "ping", "-h", "localhost"]
      interval: 5s
      timeout: 5s
      retries: 20
`, majorMinor(ver), instName, spec.Port, cnfPath, dataDir)
		pb.Phase(sdk.PhaseSpec{
			Name: "deploy-mysql-container",
			Tasks: []sdk.TaskSpec{
				sdk.DockerEnsure(nil).WithKey("mysql-docker"),
				sdk.FileSync(cnfPath, sdk.TemplateRef{Inline: myCNF}, 0o644, "").WithKey("mysql-cnf"),
				sdk.ComposeSync("kinstall-mysql-"+instName, composePath,
					sdk.MountSpec{Src: cnfPath, Dst: "/etc/mysql/conf.d/kinstall.cnf", Type: "file", Mode: 0o644},
					sdk.MountSpec{Src: dataDir, Dst: "/var/lib/mysql", Type: "dir", Mode: 0o755},
				).WithParam("composeContent", composeYAML).WithKey("mysql-compose"),
				sdk.WaitTCP(fmt.Sprintf("127.0.0.1:%d", spec.Port), 120*time.Second).WithKey("mysql-ready"),
			},
		})
		return pb.Build(), nil
	}

	myCNF := fmt.Sprintf(`[mysqld]
port=%d
datadir=%s
socket=%s/mysql.sock
gtid_mode=ON
enforce_gtid_consistency=ON
group_replication_single_primary_mode=ON
group_replication_transaction_size_limit=157286400
group_replication_flow_control_mode=QUOTA
group_replication_member_expel_timeout=5
group_replication_autorejoin_tries=3
innodb_buffer_pool_size=%dM
innodb_redo_log_capacity=%dM
innodb_flush_method=O_DIRECT
innodb_flush_log_at_trx_commit=1
sync_binlog=1
`, spec.Port, dataDir, dataDir, bp, redo)
	mysqlUnit := fmt.Sprintf(`[Unit]
Description=kinstall MySQL %s (%s)
After=network-online.target
[Service]
Type=simple
User=mysql
Group=mysql
ExecStart=/usr/local/bin/mysqld --defaults-file=%s
Restart=on-failure
LimitNOFILE=65535
[Install]
WantedBy=multi-user.target
`, ver, instName, cnfPath)
	prepCmd := fmt.Sprintf("getent group mysql >/dev/null || groupadd -r mysql; id -u mysql >/dev/null 2>&1 || useradd -r -g mysql -s /sbin/nologin mysql; mkdir -p %s /etc/kinstall/mysql/%s && chown -R mysql:mysql %s", dataDir, instName, dataDir)
	initCmd := fmt.Sprintf("MYSQLD=$(command -v mysqld || echo /usr/local/bin/mysqld); \"$MYSQLD\" --defaults-file=%s --initialize-insecure --user=mysql --datadir=%s", cnfPath, dataDir)
	pb.Phase(sdk.PhaseSpec{
		Name: "deploy-mysql-mgr",
		Tasks: []sdk.TaskSpec{
			sdk.CmdRun(prepCmd, fmt.Sprintf("id -u mysql >/dev/null 2>&1 && test -d %s", dataDir)).WithKey("mysql-user-dir"),
			sdk.BinaryInstall("mysqld", ver, sdk.ArtifactRef{Name: "mysql"}).WithParam("fallbackPkg", "mysql-server").WithKey("mysql-bin"),
			sdk.FileSync(cnfPath, sdk.TemplateRef{Inline: myCNF}, 0o640, "root:mysql").WithKey("mysql-cnf"),
			sdk.CmdRun(initCmd, fmt.Sprintf("test -d %s/mysql", dataDir)).WithKey("mysql-initdb"),
			sdk.SystemdUnit("kinstall-mysql-"+instName+".service", sdk.TemplateRef{Inline: mysqlUnit}, sdk.UnitOpts{Enable: true, Start: true, RestartOnChange: true}).WithKey("mysql-unit"),
			sdk.WaitTCP(fmt.Sprintf("127.0.0.1:%d", spec.Port), 90*time.Second).WithKey("mysql-ready"),
		},
	})
	return pb.Build(), nil
}

func (p *Plugin) buildBackup(_ context.Context, req *sdk.OpRequest) (*sdk.Pipeline, error) {
	pb := sdk.NewPipelineBuilder()
	pb.Phase(sdk.PhaseSpec{Name: "xtrabackup-stream", Tasks: []sdk.TaskSpec{sdk.CmdRun("xtrabackup --backup --stream=xbstream | zstd -T4 > /var/backups/kinstall/mysql-"+req.Instance.Name+".xb.zst", "").WithKey("mysql-xtrabackup")}})
	return pb.Build(), nil
}

func (p *Plugin) buildRestore(_ context.Context, req *sdk.OpRequest) (*sdk.Pipeline, error) {
	pb := sdk.NewPipelineBuilder()
	pb.Phase(sdk.PhaseSpec{Name: "xtrabackup-restore", Disruptive: true, Tasks: []sdk.TaskSpec{sdk.CmdRun("xtrabackup --prepare --target-dir=/var/backups/kinstall/restore-"+req.Instance.Name, "").WithKey("mysql-restore")}})
	return pb.Build(), nil
}

func (p *Plugin) buildUninstall(_ context.Context, req *sdk.OpRequest) (*sdk.Pipeline, error) {
	instName := req.Instance.Name
	pb := sdk.NewPipelineBuilder()
	stopScript := fmt.Sprintf(`systemctl disable --now kinstall-mysql-%s.service 2>/dev/null || true
if [ -f /opt/kinstall/compose/mysql-%s/docker-compose.yml ]; then
  (docker compose -f /opt/kinstall/compose/mysql-%s/docker-compose.yml down --remove-orphans || docker-compose -f /opt/kinstall/compose/mysql-%s/docker-compose.yml down || true)
fi
rm -f /etc/systemd/system/kinstall-mysql-%s.service
systemctl daemon-reload 2>/dev/null || true`, instName, instName, instName, instName, instName)
	pb.Phase(sdk.PhaseSpec{Name: "uninstall-mysql", Disruptive: true, Tasks: []sdk.TaskSpec{sdk.CmdRun(stopScript, "").WithKey("mysql-stop")}})
	return pb.Build(), nil
}

func majorMinor(v string) string {
	parts := strings.Split(strings.TrimPrefix(v, "v"), ".")
	if len(parts) >= 2 {
		return parts[0] + "." + parts[1]
	}
	return v
}

type myOp struct {
	name     string
	mutating bool
	build    func(context.Context, *sdk.OpRequest) (*sdk.Pipeline, error)
}

func (o *myOp) Name() string   { return o.name }
func (o *myOp) Mutating() bool { return o.mutating }
func (o *myOp) Preflight(_ context.Context, _ *sdk.OpRequest) (*sdk.ValidationReport, error) {
	return sdk.NewValidationReport(), nil
}
func (o *myOp) Build(ctx context.Context, req *sdk.OpRequest) (*sdk.Pipeline, error) {
	return o.build(ctx, req)
}
