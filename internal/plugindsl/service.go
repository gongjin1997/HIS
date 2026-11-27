package plugindsl

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"text/template"
	"time"

	"github.com/kinstall/kinstall/internal/ports"
	"go.yaml.in/yaml/v3"

	"github.com/kinstall/kinstall/internal/domain"
	"github.com/kinstall/kinstall/internal/registry"
	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
)

// PluginManifest defines a declarative, user-extensible deployment plugin package (1Panel / LiveOps IaC style).
type PluginManifest struct {
	APIVersion      string          `yaml:"apiVersion" json:"apiVersion"`
	Kind            string          `yaml:"kind" json:"kind"`
	Name            string          `yaml:"name" json:"name"`
	DisplayName     string          `yaml:"displayName" json:"displayName"`
	Category        string          `yaml:"category" json:"category"`
	Version         string          `yaml:"version" json:"version"`
	Icon            string          `yaml:"icon" json:"icon"`
	Description     string          `yaml:"description" json:"description"`
	Targets         []string        `yaml:"targets" json:"targets"` // host-container | host-binary | kubernetes
	Params          map[string]any  `yaml:"params" json:"params"`   // JSON Schema + x-kinstall-ui
	ComposeTemplate string          `yaml:"composeTemplate,omitempty" json:"composeTemplate,omitempty"`
	SystemdUnit     string          `yaml:"systemdUnit,omitempty" json:"systemdUnit,omitempty"`
	InstallScript   string          `yaml:"installScript,omitempty" json:"installScript,omitempty"`
	UninstallScript string          `yaml:"uninstallScript,omitempty" json:"uninstallScript,omitempty"`
	HealthCheck     HealthCheckSpec `yaml:"healthCheck,omitempty" json:"healthCheck,omitempty"`
	DefaultPorts    []int           `yaml:"defaultPorts,omitempty" json:"defaultPorts,omitempty"`
}

type HealthCheckSpec struct {
	TCPPort int    `yaml:"tcpPort,omitempty" json:"tcpPort,omitempty"`
	HTTPURL string `yaml:"httpUrl,omitempty" json:"httpUrl,omitempty"`
	Timeout string `yaml:"timeout,omitempty" json:"timeout,omitempty"`
}

type StoredPlugin struct {
	ID           string         `json:"id"`
	ProjectID    string         `json:"projectId"`
	Name         string         `json:"name"`
	DisplayName  string         `json:"displayName"`
	Category     string         `json:"category"`
	Version      string         `json:"version"`
	Icon         string         `json:"icon"`
	Description  string         `json:"description"`
	Targets      []string       `json:"targets"`
	ManifestYAML string         `json:"manifestYaml"`
	ParamSchema  map[string]any `json:"paramSchema"`
	IsEnabled    bool           `json:"isEnabled"`
	CreatedBy    string         `json:"createdBy"`
	CreatedAt    time.Time      `json:"createdAt"`
	UpdatedAt    time.Time      `json:"updatedAt"`
}

type Service struct {
	writeDB  ports.QueryExec
	readDB   ports.QueryExec
	registry *registry.Registry
}

func NewService(writeDB, readDB ports.QueryExec, reg *registry.Registry) *Service {
	if readDB == nil {
		readDB = writeDB
	}
	return &Service{writeDB: writeDB, readDB: readDB, registry: reg}
}

func parseTS(s string) time.Time {
	s = strings.TrimSpace(s)
	for _, l := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05"} {
		if t, err := time.Parse(l, s); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}

func ParseAndValidateManifest(rawYAML string) (*PluginManifest, error) {
	var m PluginManifest
	if err := yaml.Unmarshal([]byte(rawYAML), &m); err != nil {
		return nil, sdk.NewError(sdk.CodeAPIInvalidRequest, "PLGDSL", http.StatusBadRequest, sdk.CodeAPIInvalidRequest, false, fmt.Errorf("YAML 解析失败: %w", err))
	}
	m.Name = strings.TrimSpace(strings.ToLower(m.Name))
	if m.Name == "" {
		return nil, sdk.NewError(sdk.CodeAPIInvalidRequest, "PLGDSL", http.StatusBadRequest, sdk.CodeAPIInvalidRequest, false, errors.New("manifest.name 不能为空"))
	}
	if m.DisplayName == "" {
		m.DisplayName = m.Name
	}
	if m.Version == "" {
		m.Version = "1.0.0"
	}
	if m.Category == "" {
		m.Category = "custom"
	}
	if len(m.Targets) == 0 {
		m.Targets = []string{"host-container"}
	}
	if m.ComposeTemplate == "" && m.SystemdUnit == "" && m.InstallScript == "" {
		return nil, sdk.NewError(sdk.CodeAPIInvalidRequest, "PLGDSL", http.StatusBadRequest, sdk.CodeAPIInvalidRequest, false, errors.New("至少需定义 composeTemplate、systemdUnit 或 installScript 之一"))
	}
	return &m, nil
}

// EnsureDefaults seeds 3 rich declarative starter plugin packages and registers all enabled DB plugins into Registry.
func (s *Service) EnsureDefaults(ctx context.Context, projectID string) error {
	if projectID == "" {
		return nil
	}
	var cnt int
	if err := s.readDB.ScanRaw(ctx, `SELECT COUNT(1) FROM declarative_plugins WHERE project_id = ?`, []any{projectID}, &cnt); err == nil && cnt > 0 {
		return s.SyncRegistry(ctx, projectID)
	}

	starters := []string{
		`apiVersion: kinstall.io/v1alpha1
kind: PluginPackage
name: his-java-service
displayName: HIS 核心 Java 微服务标准包
category: 业务应用
version: 2.1.0
icon: Boxes
description: 声明式 SpringBoot/Java 业务服务容器化交付模板，支持自定义 JVM 堆内存、端口、配置中心挂载与 /actuator/health 探针
targets:
  - host-container
defaultPorts:
  - 8088
params:
  title: HIS Java 业务微服务部署参数
  properties:
    image:
      type: string
      default: harbor.internal.corp/his/his-outpatient:2.1.0
      x-kinstall-ui: { group: basic, order: 1, label: 离线业务镜像地址 }
    port:
      type: integer
      default: 8088
      minimum: 1024
      maximum: 65535
      x-kinstall-ui: { group: network, order: 2, label: 服务监听端口 }
    jvmOpts:
      type: string
      default: "-Xms1024m -Xmx2048m -XX:+UseG1GC"
      x-kinstall-ui: { group: advanced, order: 3, label: JVM 启动参数 }
    springProfile:
      type: string
      enum: ["prod", "uat", "sit", "dev"]
      default: prod
      x-kinstall-ui: { group: basic, order: 4, label: 运行环境 Profile }
composeTemplate: |
  services:
    app:
      image: "{{ .image }}"
      container_name: "kinstall-{{ .InstanceName }}"
      restart: unless-stopped
      ports:
        - "{{ .port }}:{{ .port }}"
      environment:
        JAVA_OPTS: "{{ .jvmOpts }}"
        SPRING_PROFILES_ACTIVE: "{{ .springProfile }}"
      volumes:
        - "/data/apps/{{ .InstanceName }}/logs:/app/logs"
healthCheck:
  tcpPort: 8088
  timeout: 45s
`,
		`apiVersion: kinstall.io/v1alpha1
kind: PluginPackage
name: openresty-edge-waf
displayName: OpenResty 边缘网关与限流包
category: 网关与网络
version: 1.25.3
icon: Globe
description: 高性能 OpenResty + Lua 边缘接入网关声明式插件包，适用于零信任前置路由与静态资源加速
targets:
  - host-container
defaultPorts:
  - 8080
params:
  title: OpenResty 边缘网关参数
  properties:
    image:
      type: string
      default: openresty/openresty:1.25.3.2-alpine
      x-kinstall-ui: { group: basic, order: 1, label: 镜像地址 }
    httpPort:
      type: integer
      default: 8080
      x-kinstall-ui: { group: network, order: 2, label: HTTP 端口 }
composeTemplate: |
  services:
    openresty:
      image: "{{ .image }}"
      container_name: "kinstall-{{ .InstanceName }}"
      restart: unless-stopped
      ports:
        - "{{ .httpPort }}:80"
healthCheck:
  tcpPort: 8080
  timeout: 30s
`,
		`apiVersion: kinstall.io/v1alpha1
kind: PluginPackage
name: node-exporter-agent
displayName: Prometheus Node Exporter 采集插件
category: 可观测与监控
version: 1.8.2
icon: Activity
description: 主机级硬件与内核指标暴露插件（9100 端口），一键挂载至 CMDB 主机节点
targets:
  - host-container
  - host-binary
defaultPorts:
  - 9100
params:
  title: Node Exporter 采集参数
  properties:
    port:
      type: integer
      default: 9100
      x-kinstall-ui: { group: network, order: 1, label: 指标暴露端口 }
composeTemplate: |
  services:
    node-exporter:
      image: prom/node-exporter:v1.8.2
      container_name: "kinstall-{{ .InstanceName }}"
      restart: unless-stopped
      network_mode: host
      pid: host
      command:
        - "--web.listen-address=:{{ .port }}"
installScript: |
  mkdir -p /opt/kinstall/node-exporter
  echo "node-exporter configured on port {{ .port }}"
healthCheck:
  tcpPort: 9100
  timeout: 20s
`,
	}

	for _, y := range starters {
		_, _ = s.Upsert(ctx, projectID, "system", y, true)
	}
	return s.SyncRegistry(ctx, projectID)
}

func (s *Service) List(ctx context.Context, projectID string) ([]StoredPlugin, error) {
	if err := s.EnsureDefaults(ctx, projectID); err != nil {
		return nil, err
	}
	rows, err := s.readDB.QueryRaw(ctx,
		`SELECT id, project_id, name, display_name, category, version, icon, description, targets_json, manifest_yaml, param_schema_json, is_enabled, created_by, created_at, updated_at
		 FROM declarative_plugins WHERE project_id = ? ORDER BY updated_at DESC, name ASC`, projectID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]StoredPlugin, 0)
	for rows.Next() {
		var p StoredPlugin
		var tStr, psStr, cAt, uAt string
		if err := rows.Scan(&p.ID, &p.ProjectID, &p.Name, &p.DisplayName, &p.Category, &p.Version, &p.Icon, &p.Description, &tStr, &p.ManifestYAML, &psStr, &p.IsEnabled, &p.CreatedBy, &cAt, &uAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(tStr), &p.Targets)
		_ = json.Unmarshal([]byte(psStr), &p.ParamSchema)
		if p.Targets == nil {
			p.Targets = []string{}
		}
		if p.ParamSchema == nil {
			p.ParamSchema = map[string]any{}
		}
		p.CreatedAt, p.UpdatedAt = parseTS(cAt), parseTS(uAt)
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Service) Upsert(ctx context.Context, projectID, actor, manifestYAML string, enabled bool) (*StoredPlugin, error) {
	m, err := ParseAndValidateManifest(manifestYAML)
	if err != nil {
		return nil, err
	}
	tB, _ := json.Marshal(m.Targets)
	psB, _ := json.Marshal(m.Params)
	if m.Params == nil {
		psB = []byte("{}")
	}
	now := time.Now().UTC()
	var existingID string
	_ = s.readDB.ScanRaw(ctx, `SELECT id FROM declarative_plugins WHERE project_id = ? AND name = ?`, []any{projectID, m.Name}, &existingID)
	id := existingID
	if id == "" {
		id = domain.NewID("dplg")
	}
	_, err = s.writeDB.ExecRaw(ctx,
		`INSERT INTO declarative_plugins (id, project_id, name, display_name, category, version, icon, description, targets_json, manifest_yaml, param_schema_json, is_enabled, created_by, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT (project_id, name) DO UPDATE SET
		   display_name = excluded.display_name,
		   category = excluded.category,
		   version = excluded.version,
		   icon = excluded.icon,
		   description = excluded.description,
		   targets_json = excluded.targets_json,
		   manifest_yaml = excluded.manifest_yaml,
		   param_schema_json = excluded.param_schema_json,
		   is_enabled = excluded.is_enabled,
		   updated_at = excluded.updated_at`,
		id, projectID, m.Name, m.DisplayName, m.Category, m.Version, m.Icon, m.Description, string(tB), manifestYAML, string(psB), enabled, actor, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano),
	)
	if err != nil {
		return nil, err
	}

	if s.registry != nil {
		if enabled {
			s.registry.UpsertDynamicComponent(&DeclarativeAdapter{manifest: m})
		} else {
			s.registry.RemoveDynamicComponent(m.Name)
		}
	}

	return &StoredPlugin{
		ID:           id,
		ProjectID:    projectID,
		Name:         m.Name,
		DisplayName:  m.DisplayName,
		Category:     m.Category,
		Version:      m.Version,
		Icon:         m.Icon,
		Description:  m.Description,
		Targets:      m.Targets,
		ManifestYAML: manifestYAML,
		ParamSchema:  m.Params,
		IsEnabled:    enabled,
		CreatedBy:    actor,
		CreatedAt:    now,
		UpdatedAt:    now,
	}, nil
}

func (s *Service) Delete(ctx context.Context, projectID, pluginID string) error {
	var name string
	err := s.readDB.ScanRaw(ctx, `SELECT name FROM declarative_plugins WHERE project_id = ? AND id = ?`, []any{projectID, pluginID}, &name)
	if errors.Is(err, sql.ErrNoRows) {
		return sdk.NewError(sdk.CodeDBNotFound, "PLGDSL", http.StatusNotFound, sdk.CodeDBNotFound, false, fmt.Errorf("plugin %q not found", pluginID))
	}
	if err != nil {
		return err
	}
	if _, err := s.writeDB.ExecRaw(ctx, `DELETE FROM declarative_plugins WHERE project_id = ? AND id = ?`, projectID, pluginID); err != nil {
		return err
	}
	if s.registry != nil && name != "" {
		s.registry.RemoveDynamicComponent(name)
	}
	return nil
}

func (s *Service) SyncRegistry(ctx context.Context, projectID string) error {
	if s.registry == nil {
		return nil
	}
	rows, err := s.readDB.QueryRaw(ctx,
		`SELECT manifest_yaml FROM declarative_plugins WHERE project_id = ? AND is_enabled = ?`, projectID, true)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var y string
		if rows.Scan(&y) == nil {
			if m, perr := ParseAndValidateManifest(y); perr == nil {
				s.registry.UpsertDynamicComponent(&DeclarativeAdapter{manifest: m})
			}
		}
	}
	return nil
}

// --- DeclarativeAdapter implements sdk.Component so YAML plugins seamlessly run on the real engine ---

type DeclarativeAdapter struct {
	manifest *PluginManifest
}

func (d *DeclarativeAdapter) Meta() sdk.PluginMeta {
	targets := make([]sdk.DeployTarget, 0, len(d.manifest.Targets))
	for _, t := range d.manifest.Targets {
		targets = append(targets, sdk.DeployTarget(t))
	}
	return sdk.PluginMeta{
		Name:        d.manifest.Name,
		Version:     d.manifest.Version,
		SDKVersion:  sdk.SDKAPIVersion,
		Type:        sdk.PluginComponent,
		Description: fmt.Sprintf("[声明式插件] %s", d.manifest.Description),
		Products: []sdk.ProductRange{{
			Product:     d.manifest.Name,
			Versions:    ">=0.0.1",
			DefaultVer:  d.manifest.Version,
			Targets:     targets,
			LicenseTier: "green",
		}},
		Platforms: sdk.Platforms{
			Arch:      []string{"amd64", "arm64"},
			PageSizes: []int{4096, 65536},
		},
		Operations:    []string{"install", "upgrade", "uninstall"},
		ParamSchema:   d.manifest.Params,
		SchemaVersion: 1,
	}
}

func (d *DeclarativeAdapter) Validate(_ context.Context, _ *sdk.ValidateRequest) (*sdk.ValidationReport, error) {
	return sdk.NewValidationReport(), nil
}

func (d *DeclarativeAdapter) Status(_ context.Context, _ *sdk.StatusRequest) (*sdk.InstanceStatus, error) {
	return &sdk.InstanceStatus{Health: "healthy", Message: d.manifest.DisplayName + " ready", CheckedAt: time.Now().UTC()}, nil
}

func (d *DeclarativeAdapter) Operation(name string) (sdk.Operation, bool) {
	switch name {
	case "install", "upgrade", "uninstall":
		return &declOp{adapter: d, opName: name}, true
	default:
		return nil, false
	}
}

func (d *DeclarativeAdapter) Plan(ctx context.Context, req *sdk.PlanRequest) (*sdk.PlanResult, error) {
	opName := req.Operation
	if opName == "" {
		opName = "install"
	}
	op, ok := d.Operation(opName)
	if !ok {
		return nil, sdk.NewError(sdk.CodeAPIInvalidRequest, "PLGDSL", http.StatusBadRequest, sdk.CodeAPIInvalidRequest, false, fmt.Errorf("unknown op %q", opName))
	}
	pl, err := op.Build(ctx, &sdk.OpRequest{Instance: req.Instance, Hosts: req.Hosts, BOM: req.BOM})
	if err != nil {
		return nil, err
	}
	return &sdk.PlanResult{
		Summary:  fmt.Sprintf("%s 声明式插件 %s v%s (%s)", opName, d.manifest.DisplayName, d.manifest.Version, req.Instance.DeployTarget),
		Pipeline: pl,
	}, nil
}

type declOp struct {
	adapter *DeclarativeAdapter
	opName  string
}

func (o *declOp) Name() string   { return o.opName }
func (o *declOp) Mutating() bool { return true }
func (o *declOp) Preflight(_ context.Context, _ *sdk.OpRequest) (*sdk.ValidationReport, error) {
	return sdk.NewValidationReport(), nil
}

func renderDeclTemplate(tmplStr string, inst sdk.InstanceView, params map[string]any) (string, error) {
	data := make(map[string]any, len(params)+4)
	for k, v := range params {
		data[k] = v
	}
	data["InstanceName"] = inst.Name
	data["Version"] = inst.ProductVersion
	data["Target"] = string(inst.DeployTarget)

	t, err := template.New("decl").Option("missingkey=zero").Parse(tmplStr)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func (o *declOp) Build(_ context.Context, req *sdk.OpRequest) (*sdk.Pipeline, error) {
	m := o.adapter.manifest
	params := make(map[string]any)
	if props, ok := m.Params["properties"].(map[string]any); ok {
		for k, raw := range props {
			if pmap, ok := raw.(map[string]any); ok {
				if def, has := pmap["default"]; has {
					params[k] = def
				}
			}
		}
	}
	if len(req.Instance.Spec) > 0 {
		var userParams map[string]any
		if json.Unmarshal(req.Instance.Spec, &userParams) == nil {
			for k, v := range userParams {
				params[k] = v
			}
		}
	}

	instName := req.Instance.Name
	if instName == "" {
		instName = m.Name + "-main"
	}
	target := req.Instance.DeployTarget
	if target == "" {
		target = sdk.TargetHostContainer
	}
	workDir := fmt.Sprintf("/opt/kinstall/plugins/%s/%s", m.Name, instName)
	composePath := workDir + "/docker-compose.yml"
	pb := sdk.NewPipelineBuilder()

	if o.opName == "uninstall" {
		script := m.UninstallScript
		if script == "" {
			script = fmt.Sprintf(`if [ -f %s ]; then (docker compose -f %s down --remove-orphans || docker-compose -f %s down || true); fi; rm -rf %s`, composePath, composePath, composePath, workDir)
		} else {
			var err error
			script, err = renderDeclTemplate(script, req.Instance, params)
			if err != nil {
				return nil, err
			}
		}
		pb.Phase(sdk.PhaseSpec{
			Name:       "uninstall-" + m.Name,
			Disruptive: true,
			Tasks:      []sdk.TaskSpec{sdk.CmdRun(script, "").WithKey("decl-uninstall")},
		})
		return pb.Build(), nil
	}

	if target == sdk.TargetHostContainer && m.ComposeTemplate != "" {
		composeRendered, err := renderDeclTemplate(m.ComposeTemplate, req.Instance, params)
		if err != nil {
			return nil, err
		}
		tasks := []sdk.TaskSpec{
			sdk.DockerEnsure(nil).WithKey("decl-docker"),
			sdk.ComposeSync("kinstall-"+m.Name+"-"+instName, composePath).
				WithParam("composeContent", composeRendered).
				WithKey("decl-compose"),
		}
		if m.HealthCheck.TCPPort > 0 {
			port := m.HealthCheck.TCPPort
			if p, ok := params["port"]; ok {
				if pf, ok := p.(float64); ok && pf > 0 {
					port = int(pf)
				} else if pi, ok := p.(int); ok && pi > 0 {
					port = pi
				}
			}
			tasks = append(tasks, sdk.WaitTCP(fmt.Sprintf("127.0.0.1:%d", port), 60*time.Second).WithKey("decl-ready"))
		}
		pb.Phase(sdk.PhaseSpec{Name: "deploy-" + m.Name + "-container", Tasks: tasks})
		return pb.Build(), nil
	}

	if m.InstallScript != "" {
		script, err := renderDeclTemplate(m.InstallScript, req.Instance, params)
		if err != nil {
			return nil, err
		}
		tasks := []sdk.TaskSpec{sdk.CmdRun(script, "").WithKey("decl-install")}
		if m.HealthCheck.TCPPort > 0 {
			tasks = append(tasks, sdk.WaitTCP(fmt.Sprintf("127.0.0.1:%d", m.HealthCheck.TCPPort), 60*time.Second).WithKey("decl-ready"))
		}
		pb.Phase(sdk.PhaseSpec{Name: "deploy-" + m.Name + "-script", Tasks: tasks})
	}
	return pb.Build(), nil
}
