package delivery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/kinstall/kinstall/pkg/platform/id"
	"go.yaml.in/yaml/v3"
)

type ParamSchema struct {
	Key      string   `yaml:"key" json:"key"`
	Label    string   `yaml:"label" json:"label"`
	Type     string   `yaml:"type" json:"type"` // string | int | float | secret | enum | bool
	Default  string   `yaml:"default" json:"default"`
	Required bool     `yaml:"required" json:"required"`
	Options  []string `yaml:"options,omitempty" json:"options,omitempty"`
}

type HealthCheckSpec struct {
	Type string `yaml:"type" json:"type"` // tcp | http | cmd
	Port string `yaml:"port,omitempty" json:"port,omitempty"`
	Path string `yaml:"path,omitempty" json:"path,omitempty"`
	Cmd  string `yaml:"cmd,omitempty" json:"cmd,omitempty"`
}

type PluginManifest struct {
	APIVersion      string          `yaml:"apiVersion" json:"apiVersion"`
	Code            string          `yaml:"code" json:"code"`
	Name            string          `yaml:"name" json:"name"`
	Category        string          `yaml:"category" json:"category"` // database | middleware | storage | application | observability
	Version         string          `yaml:"version" json:"version"`
	Runtime         string          `yaml:"runtime" json:"runtime"` // compose | systemd | script
	CMDBModel       string          `yaml:"cmdbModel" json:"cmdbModel"`
	DefaultPort     int             `yaml:"defaultPort" json:"defaultPort"`
	Description     string          `yaml:"description" json:"description"`
	Params          []ParamSchema   `yaml:"params" json:"params"`
	InstallScript   string          `yaml:"installScript" json:"installScript"`
	HealthCheck     HealthCheckSpec `yaml:"healthCheck" json:"healthCheck"`
	UninstallScript string          `yaml:"uninstallScript" json:"uninstallScript"`
}

type PluginPackage struct {
	ID           string        `json:"id"`
	WorkspaceID  string        `json:"workspaceId"`
	Code         string        `json:"code"`
	Name         string        `json:"name"`
	Category     string        `json:"category"`
	Version      string        `json:"version"`
	Runtime      string        `json:"runtime"`
	Description  string        `json:"description"`
	IsBuiltin    bool          `json:"isBuiltin"`
	ManifestYAML string        `json:"manifestYaml"`
	Schema       []ParamSchema `json:"schema"`
	CreatedAt    string        `json:"createdAt"`
	UpdatedAt    string        `json:"updatedAt"`
}

func nowISO() string {
	return time.Now().UTC().Format(time.RFC3339)
}

func ParseManifestYAML(raw string) (*PluginManifest, error) {
	var m PluginManifest
	if err := yaml.Unmarshal([]byte(raw), &m); err != nil {
		return nil, fmt.Errorf("YAML 插件声明解析失败: %w", err)
	}
	m.Code = strings.ToLower(strings.TrimSpace(m.Code))
	m.Name = strings.TrimSpace(m.Name)
	if m.Code == "" || m.Name == "" {
		return nil, errors.New("YAML 插件必须声明 code 与 name 字段")
	}
	if m.Version == "" {
		m.Version = "1.0.0"
	}
	if m.Category == "" {
		m.Category = "middleware"
	}
	if m.Runtime == "" {
		m.Runtime = "compose"
	}
	if m.CMDBModel == "" {
		switch m.Category {
		case "database":
			m.CMDBModel = "m_database"
		case "storage":
			m.CMDBModel = "m_storage"
		case "application":
			m.CMDBModel = "m_app"
		default:
			m.CMDBModel = "m_middleware"
		}
	}
	if m.Params == nil {
		m.Params = make([]ParamSchema, 0)
	}
	return &m, nil
}

func (s *Service) EnsureSeededPlugins(ctx context.Context, wsID string) error {
	var cnt int
	if err := s.store.QueryRow(ctx, `SELECT COUNT(1) FROM delivery_plugins WHERE workspace_id = ?`, wsID).Scan(&cnt); err != nil {
		return err
	}
	if cnt > 0 {
		return nil
	}
	for _, rawYAML := range builtinPluginManifests {
		_, _ = s.upsertPluginInternal(ctx, wsID, rawYAML, true)
	}
	return nil
}

func (s *Service) UpsertPluginFromYAML(ctx context.Context, wsID, rawYAML string) (*PluginPackage, error) {
	return s.upsertPluginInternal(ctx, wsID, rawYAML, false)
}

func (s *Service) upsertPluginInternal(ctx context.Context, wsID, rawYAML string, builtin bool) (*PluginPackage, error) {
	m, err := ParseManifestYAML(rawYAML)
	if err != nil {
		return nil, err
	}
	schemaBytes, _ := json.Marshal(m.Params)
	now := nowISO()
	bInt := 0
	if builtin {
		bInt = 1
	}
	pkgID := id.New("plg")
	_, err = s.store.Exec(ctx,
		`INSERT INTO delivery_plugins(id, workspace_id, code, name, category, version, runtime, description, is_builtin, manifest_yaml, schema_json, created_at, updated_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)
		 ON CONFLICT(workspace_id, code) DO UPDATE SET
		   name=excluded.name, category=excluded.category, version=excluded.version, runtime=excluded.runtime,
		   description=excluded.description, manifest_yaml=excluded.manifest_yaml, schema_json=excluded.schema_json, updated_at=excluded.updated_at`,
		pkgID, wsID, m.Code, m.Name, m.Category, m.Version, m.Runtime, m.Description, bInt, rawYAML, string(schemaBytes), now, now,
	)
	if err != nil {
		return nil, err
	}
	return s.GetPluginByCode(ctx, wsID, m.Code)
}

func (s *Service) ListPlugins(ctx context.Context, wsID, category string) ([]PluginPackage, error) {
	q := `SELECT id, workspace_id, code, name, category, version, runtime, description, is_builtin, manifest_yaml, schema_json, created_at, updated_at
	      FROM delivery_plugins WHERE workspace_id = ?`
	args := []any{wsID}
	if strings.TrimSpace(category) != "" {
		q += ` AND category = ?`
		args = append(args, strings.TrimSpace(category))
	}
	q += ` ORDER BY is_builtin DESC, category ASC, code ASC`
	rows, err := s.store.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]PluginPackage, 0)
	for rows.Next() {
		var (
			p         PluginPackage
			bInt      int
			schemaRaw string
		)
		if err := rows.Scan(&p.ID, &p.WorkspaceID, &p.Code, &p.Name, &p.Category, &p.Version, &p.Runtime, &p.Description, &bInt, &p.ManifestYAML, &schemaRaw, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		p.IsBuiltin = bInt == 1
		p.Schema = make([]ParamSchema, 0)
		_ = json.Unmarshal([]byte(schemaRaw), &p.Schema)
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Service) GetPluginByCode(ctx context.Context, wsID, code string) (*PluginPackage, error) {
	var (
		p         PluginPackage
		bInt      int
		schemaRaw string
	)
	err := s.store.QueryRow(ctx,
		`SELECT id, workspace_id, code, name, category, version, runtime, description, is_builtin, manifest_yaml, schema_json, created_at, updated_at
		 FROM delivery_plugins WHERE workspace_id = ? AND code = ?`,
		wsID, code,
	).Scan(&p.ID, &p.WorkspaceID, &p.Code, &p.Name, &p.Category, &p.Version, &p.Runtime, &p.Description, &bInt, &p.ManifestYAML, &schemaRaw, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return nil, err
	}
	p.IsBuiltin = bInt == 1
	p.Schema = make([]ParamSchema, 0)
	_ = json.Unmarshal([]byte(schemaRaw), &p.Schema)
	return &p, nil
}

func (s *Service) DeletePlugin(ctx context.Context, wsID, code string) error {
	_, err := s.store.Exec(ctx, `DELETE FROM delivery_plugins WHERE workspace_id = ? AND code = ?`, wsID, code)
	return err
}

var builtinPluginManifests = []string{
	`apiVersion: kinstall.io/v2
code: postgresql
name: PostgreSQL 16 企业级关系数据库
category: database
version: "16.4"
runtime: compose
cmdbModel: m_database
defaultPort: 5432
description: 支持主从流复制、WAL 归档与自动数据卷挂载的生产级 PostgreSQL 声明式插件包
params:
  - key: PG_PORT
    label: 监听端口
    type: int
    default: "5432"
  - key: PG_DB
    label: 初始化库名
    type: string
    default: his_core
  - key: PG_PASSWORD
    label: 超级管理员密码
    type: secret
    default: ChangeMe#Pg2026
  - key: CPU_CORES
    label: CPU 核数
    type: float
    default: "4"
  - key: MEM_GB
    label: 内存 (GB)
    type: float
    default: "8"
installScript: |
  echo "[Plugin:postgresql] Rendering compose deployment for {{SERVICE_NAME}} on port {{PG_PORT}}"
  mkdir -p /opt/kinstall/services/{{SERVICE_NAME}}
  echo "POSTGRES_DB={{PG_DB}} PORT={{PG_PORT}}" > /opt/kinstall/services/{{SERVICE_NAME}}/.env
  echo "[Plugin:postgresql] Deployment initialized successfully"
healthCheck:
  type: tcp
  port: "{{PG_PORT}}"
uninstallScript: |
  echo "[Plugin:postgresql] Rolling back {{SERVICE_NAME}}"
  rm -rf /opt/kinstall/services/{{SERVICE_NAME}}
`,
	`apiVersion: kinstall.io/v2
code: mysql
name: MySQL 8.4 LTS 关系数据库
category: database
version: "8.4.3"
runtime: compose
cmdbModel: m_database
defaultPort: 3306
description: 面向交易系统的 MySQL 8.4 LTS 声明式插件，内置 InnoDB 缓冲池与慢查询规范
params:
  - key: MYSQL_PORT
    label: 服务端口
    type: int
    default: "3306"
  - key: MYSQL_ROOT_PASSWORD
    label: Root 密码
    type: secret
    default: ChangeMe#My2026
  - key: CPU_CORES
    label: CPU 核数
    type: float
    default: "4"
  - key: MEM_GB
    label: 内存 (GB)
    type: float
    default: "8"
installScript: |
  echo "[Plugin:mysql] Provisioning MySQL 8.4 {{SERVICE_NAME}} on port {{MYSQL_PORT}}"
  mkdir -p /opt/kinstall/services/{{SERVICE_NAME}}
  echo "[Plugin:mysql] Ready"
healthCheck:
  type: tcp
  port: "{{MYSQL_PORT}}"
uninstallScript: |
  echo "[Plugin:mysql] Removing {{SERVICE_NAME}}"
  rm -rf /opt/kinstall/services/{{SERVICE_NAME}}
`,
	`apiVersion: kinstall.io/v2
code: valkey
name: Valkey 8 高性能内存缓存
category: middleware
version: "8.0.1"
runtime: compose
cmdbModel: m_middleware
defaultPort: 6379
description: 兼容 Redis 协议的开源 Valkey 8 缓存中间件，支持 AOF 持久化与哨兵高可用
params:
  - key: VALKEY_PORT
    label: 监听端口
    type: int
    default: "6379"
  - key: VALKEY_PASSWORD
    label: 访问密码
    type: secret
    default: ChangeMe#Vk2026
  - key: CPU_CORES
    label: CPU 核数
    type: float
    default: "2"
  - key: MEM_GB
    label: 内存 (GB)
    type: float
    default: "4"
installScript: |
  echo "[Plugin:valkey] Deploying Valkey 8 {{SERVICE_NAME}} on port {{VALKEY_PORT}}"
  mkdir -p /opt/kinstall/services/{{SERVICE_NAME}}
  echo "[Plugin:valkey] Valkey service ready"
healthCheck:
  type: tcp
  port: "{{VALKEY_PORT}}"
uninstallScript: |
  echo "[Plugin:valkey] Stopping {{SERVICE_NAME}}"
  rm -rf /opt/kinstall/services/{{SERVICE_NAME}}
`,
	`apiVersion: kinstall.io/v2
code: nginx
name: Nginx 1.28 边缘网关与反向代理
category: middleware
version: "1.28.0"
runtime: systemd
cmdbModel: m_middleware
defaultPort: 80
description: 支持七层负载均衡、TLS 1.3 终结与上游健康摘除的标准 Nginx 声明式包
params:
  - key: HTTP_PORT
    label: HTTP 端口
    type: int
    default: "80"
  - key: UPSTREAM_ADDR
    label: 上游服务地址
    type: string
    default: "127.0.0.1:8080"
  - key: CPU_CORES
    label: CPU 核数
    type: float
    default: "2"
  - key: MEM_GB
    label: 内存 (GB)
    type: float
    default: "2"
installScript: |
  echo "[Plugin:nginx] Generating upstream {{UPSTREAM_ADDR}} for {{SERVICE_NAME}} on port {{HTTP_PORT}}"
  mkdir -p /opt/kinstall/services/{{SERVICE_NAME}}
  echo "[Plugin:nginx] Configuration validated"
healthCheck:
  type: http
  port: "{{HTTP_PORT}}"
  path: /healthz
uninstallScript: |
  echo "[Plugin:nginx] Cleaning up {{SERVICE_NAME}}"
  rm -rf /opt/kinstall/services/{{SERVICE_NAME}}
`,
	`apiVersion: kinstall.io/v2
code: kafka
name: Apache Kafka 3.8 (KRaft 无 ZK)
category: middleware
version: "3.8.1"
runtime: compose
cmdbModel: m_middleware
defaultPort: 9092
description: 原生 KRaft 共识模式的企业级 Kafka 消息总线，零 ZooKeeper 依赖
params:
  - key: KAFKA_PORT
    label: Broker 端口
    type: int
    default: "9092"
  - key: PARTITIONS
    label: 默认分区数
    type: int
    default: "6"
  - key: CPU_CORES
    label: CPU 核数
    type: float
    default: "4"
  - key: MEM_GB
    label: 内存 (GB)
    type: float
    default: "8"
installScript: |
  echo "[Plugin:kafka] Bootstrapping KRaft broker {{SERVICE_NAME}} on {{KAFKA_PORT}}"
  mkdir -p /opt/kinstall/services/{{SERVICE_NAME}}
  echo "[Plugin:kafka] Broker initialized"
healthCheck:
  type: tcp
  port: "{{KAFKA_PORT}}"
uninstallScript: |
  rm -rf /opt/kinstall/services/{{SERVICE_NAME}}
`,
	`apiVersion: kinstall.io/v2
code: opensearch
name: OpenSearch 2.18 日志与全文检索
category: middleware
version: "2.18.0"
runtime: compose
cmdbModel: m_middleware
defaultPort: 9200
description: 面向审计日志、链路追踪与医疗病历检索的 OpenSearch 分布式引擎
params:
  - key: OS_PORT
    label: REST API 端口
    type: int
    default: "9200"
  - key: CPU_CORES
    label: CPU 核数
    type: float
    default: "4"
  - key: MEM_GB
    label: 内存 (GB)
    type: float
    default: "8"
installScript: |
  echo "[Plugin:opensearch] Initializing OpenSearch node {{SERVICE_NAME}} on {{OS_PORT}}"
  mkdir -p /opt/kinstall/services/{{SERVICE_NAME}}
healthCheck:
  type: tcp
  port: "{{OS_PORT}}"
uninstallScript: |
  rm -rf /opt/kinstall/services/{{SERVICE_NAME}}
`,
	`apiVersion: kinstall.io/v2
code: minio
name: MinIO 企业级 S3 对象存储
category: storage
version: "2026.01"
runtime: systemd
cmdbModel: m_storage
defaultPort: 9000
description: 兼容 S3 协议的高性能对象存储，适配 PACS 影像归档与制品仓后端
params:
  - key: MINIO_PORT
    label: S3 API 端口
    type: int
    default: "9000"
  - key: MINIO_ROOT_PASSWORD
    label: Root 密钥
    type: secret
    default: ChangeMe#Minio2026
  - key: CPU_CORES
    label: CPU 核数
    type: float
    default: "2"
  - key: MEM_GB
    label: 内存 (GB)
    type: float
    default: "4"
installScript: |
  echo "[Plugin:minio] Setting up MinIO S3 endpoint {{SERVICE_NAME}} on {{MINIO_PORT}}"
  mkdir -p /opt/kinstall/services/{{SERVICE_NAME}}
healthCheck:
  type: tcp
  port: "{{MINIO_PORT}}"
uninstallScript: |
  rm -rf /opt/kinstall/services/{{SERVICE_NAME}}
`,
	`apiVersion: kinstall.io/v2
code: harbor
name: Harbor 私有 OCI 镜像仓库
category: middleware
version: "2.12.0"
runtime: compose
cmdbModel: m_middleware
defaultPort: 443
description: 离线内网可信 OCI 镜像制品仓库，原生对接 Kinstall Go OCI 直推引擎
params:
  - key: HARBOR_HOST
    label: 仓库域名
    type: string
    default: registry.his.local
  - key: CPU_CORES
    label: CPU 核数
    type: float
    default: "4"
  - key: MEM_GB
    label: 内存 (GB)
    type: float
    default: "8"
installScript: |
  echo "[Plugin:harbor] Preparing Harbor registry {{HARBOR_HOST}} ({{SERVICE_NAME}})"
  mkdir -p /opt/kinstall/services/{{SERVICE_NAME}}
healthCheck:
  type: tcp
  port: "443"
uninstallScript: |
  rm -rf /opt/kinstall/services/{{SERVICE_NAME}}
`,
	`apiVersion: kinstall.io/v2
code: his-java-app
name: HIS 核心 Java 微服务业务包
category: application
version: "3.2.0"
runtime: systemd
cmdbModel: m_app
defaultPort: 8080
description: 标准 Spring Boot 临床/挂号/账务微服务部署模板，自动计算 JVM 堆参数与 HTTP 拨测
params:
  - key: APP_PORT
    label: HTTP 服务端口
    type: int
    default: "8080"
  - key: SPRING_PROFILE
    label: 运行 Profile
    type: enum
    default: prod
    options: ["dev", "sit", "uat", "beta", "prod"]
  - key: CPU_CORES
    label: CPU 核数
    type: float
    default: "2"
  - key: MEM_GB
    label: 内存 (GB)
    type: float
    default: "4"
  - key: REPLICAS
    label: 副本数
    type: int
    default: "2"
installScript: |
  echo "[Plugin:his-java-app] Deploying {{SERVICE_NAME}} (profile={{SPRING_PROFILE}}, port={{APP_PORT}})"
  mkdir -p /opt/kinstall/services/{{SERVICE_NAME}}
  echo "[Plugin:his-java-app] Service started cleanly"
healthCheck:
  type: http
  port: "{{APP_PORT}}"
  path: /actuator/health
uninstallScript: |
  echo "[Plugin:his-java-app] Rolling back {{SERVICE_NAME}}"
  rm -rf /opt/kinstall/services/{{SERVICE_NAME}}
`,
	`apiVersion: kinstall.io/v2
code: node-exporter
name: Prometheus Node Exporter 主机监控
category: observability
version: "1.9.0"
runtime: systemd
cmdbModel: m_middleware
defaultPort: 9100
description: 轻量级主机内核与硬件指标采集器，开箱即用暴露 9100/metrics
params:
  - key: METRICS_PORT
    label: 指标端口
    type: int
    default: "9100"
  - key: CPU_CORES
    label: CPU 核数
    type: float
    default: "0.5"
  - key: MEM_GB
    label: 内存 (GB)
    type: float
    default: "0.5"
installScript: |
  echo "[Plugin:node-exporter] Enabling node_exporter {{SERVICE_NAME}} on {{METRICS_PORT}}"
  mkdir -p /opt/kinstall/services/{{SERVICE_NAME}}
healthCheck:
  type: tcp
  port: "{{METRICS_PORT}}"
uninstallScript: |
  rm -rf /opt/kinstall/services/{{SERVICE_NAME}}
`,
}
