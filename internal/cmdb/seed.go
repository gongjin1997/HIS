package cmdb

import (
	"context"
	"encoding/json"
	"time"

	"github.com/kinstall/kinstall/pkg/platform/id"
)

func nowISO() string {
	return time.Now().UTC().Format(time.RFC3339)
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(b)
}

// EnsureSeeded populates the built-in dynamic CMDB meta-models, dual topology, starter CIs, 42U rack, and IPAM prefix.
func (s *Service) EnsureSeeded(ctx context.Context, wsID string) error {
	var cnt int
	if err := s.store.QueryRow(ctx, `SELECT COUNT(1) FROM cmdb_models WHERE workspace_id = ?`, wsID).Scan(&cnt); err != nil {
		return err
	}
	if cnt > 0 {
		return nil
	}
	now := nowISO()

	groups := []struct {
		code, name string
		order      int
	}{
		{"compute", "计算与主机资源", 10},
		{"database", "数据库与存储", 20},
		{"middleware", "中间件与缓存", 30},
		{"application", "业务应用与服务", 40},
		{"network", "机房与网络设施", 50},
		{"security", "域名与安全证书", 60},
	}
	for _, g := range groups {
		_, _ = s.store.Exec(ctx,
			`INSERT INTO cmdb_model_groups(id, workspace_id, code, name, sort_order, created_at) VALUES(?,?,?,?,?,?)`,
			id.New("mgrp"), wsID, g.code, g.name, g.order, now,
		)
	}

	models := []struct {
		group, code, name, icon, desc string
		x, y                          float64
	}{
		{"network", "m_datacenter", "数据中心/机房", "building", "物理机房与园区设施", 60, 40},
		{"network", "m_rack", "机柜 (42U)", "server", "机房物理机柜与供电单元", 300, 40},
		{"network", "m_network_dev", "网络设备", "network", "核心交换机/防火墙/负载均衡器", 560, 40},
		{"compute", "m_host", "主机节点", "cpu", "统一计算主机资产（支持 Agent/SSH 双模执行与遥测）", 300, 210},
		{"compute", "m_k8s_cluster", "K8s 集群", "layers", "容器编排集群与工作节点池", 580, 210},
		{"database", "m_database", "数据库实例", "database", "PostgreSQL / MySQL / Oracle 等关系型数据库", 120, 390},
		{"middleware", "m_middleware", "中间件服务", "boxes", "Valkey / Kafka / OpenSearch / Nginx", 360, 390},
		{"database", "m_storage", "对象/块存储", "hard-drive", "MinIO / Ceph / SAN 存储池", 610, 390},
		{"application", "m_app", "业务应用服务", "app-window", "核心微服务与业务应用系统", 240, 560},
		{"security", "m_domain", "域名/入口路由", "globe", "DNS 域名与 Ingress 网关入口", 500, 560},
		{"security", "m_cert", "TLS 证书", "shield-check", "HTTPS 证书与过期巡检台账", 750, 560},
	}
	for _, m := range models {
		_, _ = s.store.Exec(ctx,
			`INSERT INTO cmdb_models(id, workspace_id, group_code, code, name, icon, description, is_builtin, pos_x, pos_y, created_at, updated_at)
			 VALUES(?,?,?,?,?,?,?,1,?,?,?,?)`,
			id.New("mdl"), wsID, m.group, m.code, m.name, m.icon, m.desc, m.x, m.y, now, now,
		)
	}

	attrs := []struct {
		model, key, name, ftype, grp, def string
		req, uniq                         int
		opts                              []string
		order                             int
	}{
		// m_host
		{"m_host", "ip", "管理 IP", "ip", "网络与连接", "", 1, 1, nil, 10},
		{"m_host", "zone", "可用区/安全域", "enum", "网络与连接", "prod-core", 0, 0, []string{"prod-core", "dmz-edge", "staging-zone", "dr-backup"}, 20},
		{"m_host", "os", "操作系统", "string", "系统规格", "Ubuntu 24.04 LTS", 0, 0, nil, 30},
		{"m_host", "arch", "CPU 架构", "enum", "系统规格", "amd64", 0, 0, []string{"amd64", "arm64"}, 40},
		{"m_host", "cpu_cores", "CPU 核数", "int", "系统规格", "8", 0, 0, nil, 50},
		{"m_host", "mem_gb", "内存 (GB)", "int", "系统规格", "32", 0, 0, nil, 60},
		{"m_host", "sn", "资产序列号", "string", "维保资产", "", 0, 0, nil, 70},
		// m_database
		{"m_database", "engine", "数据库引擎", "enum", "运行规格", "PostgreSQL", 1, 0, []string{"PostgreSQL", "MySQL", "Oracle", "TiDB", "MongoDB"}, 10},
		{"m_database", "version", "版本号", "string", "运行规格", "16.4", 0, 0, nil, 20},
		{"m_database", "port", "监听端口", "int", "运行规格", "5432", 1, 0, nil, 30},
		{"m_database", "role", "主从角色", "enum", "高可用", "primary", 0, 0, []string{"primary", "replica", "standalone"}, 40},
		{"m_database", "endpoint", "连接地址", "string", "运行规格", "", 0, 0, nil, 50},
		// m_middleware
		{"m_middleware", "kind", "组件类型", "enum", "运行规格", "Valkey", 1, 0, []string{"Valkey", "Redis", "Kafka", "Nginx", "OpenSearch", "RabbitMQ"}, 10},
		{"m_middleware", "version", "版本号", "string", "运行规格", "8.0", 0, 0, nil, 20},
		{"m_middleware", "port", "服务端口", "int", "运行规格", "6379", 1, 0, nil, 30},
		{"m_middleware", "cluster_mode", "集群形态", "enum", "高可用", "sentinel", 0, 0, []string{"standalone", "sentinel", "cluster", "kraft"}, 40},
		// m_app
		{"m_app", "language", "技术栈", "enum", "应用属性", "Java", 0, 0, []string{"Java", "Go", "Node.js", "Python", ".NET"}, 10},
		{"m_app", "version", "发布版本", "string", "应用属性", "v2.4.0", 0, 0, nil, 20},
		{"m_app", "port", "服务端口", "int", "应用属性", "8080", 0, 0, nil, 30},
		{"m_app", "sla_tier", "SLA 等级", "enum", "业务属性", "P0", 0, 0, []string{"P0", "P1", "P2"}, 40},
		// m_network_dev
		{"m_network_dev", "vendor", "厂商", "enum", "设备规格", "Huawei", 0, 0, []string{"Huawei", "H3C", "Cisco", "Ruijie", "F5"}, 10},
		{"m_network_dev", "mgmt_ip", "管理 IP", "ip", "网络属性", "", 1, 1, nil, 20},
		{"m_network_dev", "role", "设备角色", "enum", "设备规格", "ToR 接入交换机", 0, 0, []string{"核心交换机", "ToR 接入交换机", "下一代防火墙", "负载均衡器"}, 30},
		// m_domain
		{"m_domain", "fqdn", "完整域名", "string", "域名配置", "", 1, 1, nil, 10},
		{"m_domain", "target_vip", "解析 VIP", "ip", "域名配置", "", 0, 0, nil, 20},
		{"m_domain", "tls_enabled", "启用 HTTPS", "bool", "安全属性", "true", 0, 0, nil, 30},
	}
	for _, a := range attrs {
		opts := a.opts
		if opts == nil {
			opts = make([]string, 0)
		}
		_, _ = s.store.Exec(ctx,
			`INSERT INTO cmdb_model_attrs(id, workspace_id, model_code, attr_key, name, field_type, group_name, required, readonly, is_unique, default_value, options_json, sort_order, created_at)
			 VALUES(?,?,?,?,?,?,?,?,0,?,?,?,?,?)`,
			id.New("attr"), wsID, a.model, a.key, a.name, a.ftype, a.grp, a.req, a.uniq, a.def, mustJSON(opts), a.order, now,
		)
	}

	rels := []struct {
		src, dst, code, name, card string
	}{
		{"m_rack", "m_datacenter", "located_in", "部署于机房", "N:1"},
		{"m_host", "m_rack", "mounted_in", "上架于机柜", "N:1"},
		{"m_host", "m_network_dev", "uplink_to", "上联至网络设备", "N:M"},
		{"m_database", "m_host", "runs_on", "运行于主机", "N:1"},
		{"m_middleware", "m_host", "runs_on", "运行于主机", "N:1"},
		{"m_storage", "m_host", "runs_on", "运行于主机", "N:1"},
		{"m_app", "m_host", "runs_on", "运行于主机", "N:M"},
		{"m_app", "m_database", "depends_on", "依赖数据库", "N:M"},
		{"m_app", "m_middleware", "depends_on", "依赖中间件", "N:M"},
		{"m_domain", "m_app", "routes_to", "路由至应用", "N:M"},
		{"m_domain", "m_cert", "protected_by", "绑定证书", "N:1"},
	}
	for _, r := range rels {
		_, _ = s.store.Exec(ctx,
			`INSERT INTO cmdb_model_relations(id, workspace_id, source_model, target_model, relation_code, relation_name, cardinality, created_at)
			 VALUES(?,?,?,?,?,?,?,?)`,
			id.New("mrel"), wsID, r.src, r.dst, r.code, r.name, r.card, now,
		)
	}

	// Seed 2 Service Templates
	tplJavaID := id.New("stpl")
	tplPgID := id.New("stpl")
	_, _ = s.store.Exec(ctx,
		`INSERT INTO cmdb_service_templates(id, workspace_id, code, name, category, ports_json, processes_json, env_defaults_json, description, created_at, updated_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		tplJavaID, wsID, "his-java-svc", "HIS 核心 Java 微服务模板", "application",
		mustJSON([]int{8080, 9100}), mustJSON([]string{"java -jar his-core.jar", "node_exporter"}),
		mustJSON(map[string]string{"SPRING_PROFILES_ACTIVE": "prod", "JVM_XMX": "4g"}),
		"标准 Spring Boot 业务微服务进程与端口规范", now, now,
	)
	_, _ = s.store.Exec(ctx,
		`INSERT INTO cmdb_service_templates(id, workspace_id, code, name, category, ports_json, processes_json, env_defaults_json, description, created_at, updated_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		tplPgID, wsID, "pg-ha-cluster", "PostgreSQL 高可用数据库模板", "database",
		mustJSON([]int{5432, 9187}), mustJSON([]string{"postgres", "patroni", "postgres_exporter"}),
		mustJSON(map[string]string{"PG_MAX_CONNECTIONS": "500", "PG_SHARED_BUFFERS": "4GB"}),
		"主从流复制 + Patroni 自动故障切换标准模板", now, now,
	)

	// Seed BlueKing Dual Topology Trees
	bizID := id.New("topo")
	setCoreID := id.New("topo")
	modHisID := id.New("topo")
	modDbID := id.New("topo")
	modCacheID := id.New("topo")

	orgID := id.New("topo")
	regEastID := id.New("topo")
	zoneProdID := id.New("topo")
	zoneDmzID := id.New("topo")

	topoNodes := []struct {
		id, tree, parent, ntype, name, code, tpl string
		order                                    int
	}{
		{bizID, "business", "", "biz", "智慧医院核心平台 (HIS)", "biz-his", "", 10},
		{setCoreID, "business", bizID, "set", "门急诊与临床核心集群", "set-clinical", "", 20},
		{modHisID, "business", setCoreID, "module", "核心业务交易模块", "mod-his-app", tplJavaID, 30},
		{modDbID, "business", setCoreID, "module", "核心数据库主从模块", "mod-his-db", tplPgID, 40},
		{modCacheID, "business", setCoreID, "module", "分布式缓存与消息模块", "mod-his-cache", "", 50},

		{orgID, "resource", "", "org", "总院基础设施部", "org-infra", "", 10},
		{regEastID, "resource", orgID, "region", "华东主数据中心", "reg-east-01", "", 20},
		{zoneProdID, "resource", regEastID, "zone", "生产核心安全域 (Core)", "zone-prod-core", "", 30},
		{zoneDmzID, "resource", regEastID, "zone", "边界接入区 (DMZ)", "zone-dmz-edge", "", 40},
	}
	for _, n := range topoNodes {
		_, _ = s.store.Exec(ctx,
			`INSERT INTO cmdb_topology_nodes(id, workspace_id, tree_type, parent_id, node_type, name, code, service_template_id, labels_json, sort_order, created_at, updated_at)
			 VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
			n.id, wsID, n.tree, n.parent, n.ntype, n.name, n.code, n.tpl, "{}", n.order, now, now,
		)
	}

	// Seed Starter Unified CIs (Host = m_host CI + cmdb_host_endpoints!)
	hostApp1, _ := s.UpsertHostCI(ctx, wsID, "system", UpsertCIRequest{
		ModelCode: "m_host",
		UniqueKey: "10.20.10.11",
		Name:      "his-app-prod-01",
		Env:       "prod",
		Status:    "active",
		Owner:     "SRE 核心组",
		Tags:      []string{"prod", "his-app", "core"},
		Data: map[string]any{
			"ip": "10.20.10.11", "zone": "prod-core", "os": "Ubuntu 24.04 LTS",
			"arch": "amd64", "cpu_cores": 16, "mem_gb": 64, "sn": "SN-DELL-R760-0011",
		},
		Host: &HostEndpoint{
			IP: "10.20.10.11", SSHPort: 22, SSHUser: "root", ExecMode: "auto",
			AgentStatus: "OFFLINE", OSFamily: "ubuntu", OSVersion: "24.04 LTS", Kernel: "6.8.0-generic",
			Arch: "amd64", CPUCores: 16, MemMB: 65536, DiskGB: 1000, LoadAvg: "0.42 0.38 0.35",
			CPUUsagePct: 24.5, MemUsagePct: 48.2,
		},
	})
	hostDb1, _ := s.UpsertHostCI(ctx, wsID, "system", UpsertCIRequest{
		ModelCode: "m_host",
		UniqueKey: "10.20.10.21",
		Name:      "his-pg-prod-01",
		Env:       "prod",
		Status:    "active",
		Owner:     "DBA 团队",
		Tags:      []string{"prod", "database", "ssd"},
		Data: map[string]any{
			"ip": "10.20.10.21", "zone": "prod-core", "os": "Rocky Linux 9.4",
			"arch": "amd64", "cpu_cores": 32, "mem_gb": 128, "sn": "SN-H3C-R4900-0021",
		},
		Host: &HostEndpoint{
			IP: "10.20.10.21", SSHPort: 22, SSHUser: "root", ExecMode: "auto",
			AgentStatus: "OFFLINE", OSFamily: "rocky", OSVersion: "9.4", Kernel: "5.14.0-427",
			Arch: "amd64", CPUCores: 32, MemMB: 131072, DiskGB: 4000, LoadAvg: "1.15 1.02 0.96",
			CPUUsagePct: 36.8, MemUsagePct: 62.4,
		},
	})
	hostEdge1, _ := s.UpsertHostCI(ctx, wsID, "system", UpsertCIRequest{
		ModelCode: "m_host",
		UniqueKey: "10.20.10.31",
		Name:      "his-gw-dmz-01",
		Env:       "prod",
		Status:    "active",
		Owner:     "网络与安全组",
		Tags:      []string{"prod", "dmz", "arm64"},
		Data: map[string]any{
			"ip": "10.20.10.31", "zone": "dmz-edge", "os": "Kylin V10 SP3",
			"arch": "arm64", "cpu_cores": 8, "mem_gb": 32, "sn": "SN-KP920-0031",
		},
		Host: &HostEndpoint{
			IP: "10.20.10.31", SSHPort: 22, SSHUser: "root", ExecMode: "auto",
			AgentStatus: "OFFLINE", OSFamily: "kylin", OSVersion: "V10 SP3", Kernel: "4.19.90-52",
			Arch: "arm64", CPUCores: 8, MemMB: 32768, DiskGB: 500, LoadAvg: "0.21 0.19 0.18",
			CPUUsagePct: 14.2, MemUsagePct: 31.0,
		},
	})

	dbCI, _ := s.CreateCI(ctx, wsID, "system", UpsertCIRequest{
		ModelCode: "m_database",
		UniqueKey: "pg-his-core-primary",
		Name:      "HIS 主账务 PostgreSQL 16",
		Env:       "prod",
		Status:    "active",
		Owner:     "DBA 团队",
		Tags:      []string{"p0", "ha-primary"},
		Data:      map[string]any{"engine": "PostgreSQL", "version": "16.4", "port": 5432, "role": "primary", "endpoint": "10.20.10.21:5432"},
	})
	cacheCI, _ := s.CreateCI(ctx, wsID, "system", UpsertCIRequest{
		ModelCode: "m_middleware",
		UniqueKey: "valkey-his-session",
		Name:      "HIS 分布式会话 Valkey 8",
		Env:       "prod",
		Status:    "active",
		Owner:     "中间件组",
		Tags:      []string{"cache", "sentinel"},
		Data:      map[string]any{"kind": "Valkey", "version": "8.0", "port": 6379, "cluster_mode": "sentinel"},
	})
	appCI, _ := s.CreateCI(ctx, wsID, "system", UpsertCIRequest{
		ModelCode: "m_app",
		UniqueKey: "his-clinical-gateway",
		Name:      "门急诊核心交易网关",
		Env:       "prod",
		Status:    "active",
		Owner:     "临床研发中心",
		Tags:      []string{"p0", "spring-boot"},
		Data:      map[string]any{"language": "Java", "version": "v3.2.1", "port": 8080, "sla_tier": "P0"},
	})
	netCI, _ := s.CreateCI(ctx, wsID, "system", UpsertCIRequest{
		ModelCode: "m_network_dev",
		UniqueKey: "sw-core-ce6865-01",
		Name:      "华东核心交换机 CE6865-01",
		Env:       "prod",
		Status:    "active",
		Owner:     "网络与安全组",
		Tags:      []string{"core-switch", "100g"},
		Data:      map[string]any{"vendor": "Huawei", "mgmt_ip": "10.20.10.1", "role": "核心交换机"},
	})

	// Bind starter hosts into both Business & Resource topology trees
	if hostApp1 != nil {
		_ = s.BindCIToTopoNode(ctx, wsID, hostApp1.ID, modHisID)
		_ = s.BindCIToTopoNode(ctx, wsID, hostApp1.ID, zoneProdID)
	}
	if hostDb1 != nil {
		_ = s.BindCIToTopoNode(ctx, wsID, hostDb1.ID, modDbID)
		_ = s.BindCIToTopoNode(ctx, wsID, hostDb1.ID, zoneProdID)
	}
	if hostEdge1 != nil {
		_ = s.BindCIToTopoNode(ctx, wsID, hostEdge1.ID, modCacheID)
		_ = s.BindCIToTopoNode(ctx, wsID, hostEdge1.ID, zoneDmzID)
	}

	// Build real multi-hop CI lineage edges
	if appCI != nil && hostApp1 != nil {
		_, _ = s.CreateCIRelation(ctx, wsID, appCI.ID, hostApp1.ID, "runs_on", "部署运行于生产应用主机")
	}
	if dbCI != nil && hostDb1 != nil {
		_, _ = s.CreateCIRelation(ctx, wsID, dbCI.ID, hostDb1.ID, "runs_on", "运行于数据库物理机")
	}
	if cacheCI != nil && hostEdge1 != nil {
		_, _ = s.CreateCIRelation(ctx, wsID, cacheCI.ID, hostEdge1.ID, "runs_on", "运行于边缘缓存节点")
	}
	if appCI != nil && dbCI != nil {
		_, _ = s.CreateCIRelation(ctx, wsID, appCI.ID, dbCI.ID, "depends_on", "核心账务强依赖")
	}
	if appCI != nil && cacheCI != nil {
		_, _ = s.CreateCIRelation(ctx, wsID, appCI.ID, cacheCI.ID, "depends_on", "会话与号源缓存依赖")
	}
	if hostApp1 != nil && netCI != nil {
		_, _ = s.CreateCIRelation(ctx, wsID, hostApp1.ID, netCI.ID, "uplink_to", "双 25GE 上联")
	}

	// Seed Starter Dynamic Group
	_, _ = s.CreateDynamicGroup(ctx, wsID, "生产核心 Linux 主机池", "m_host", []DynamicCondition{
		{Field: "env", Op: "eq", Value: "prod"},
		{Field: "zone", Op: "eq", Value: "prod-core"},
	}, "自动匹配生产核心安全域下的全部主机资产，可直接作为批量作业目标")

	// Seed Starter 42U DCIM Rack & Mounted Devices
	rack, err := s.CreateRack(ctx, wsID, "华东主数据中心 (IDC-SH01)", "A 区 302 机房", "RACK-A01", 42, 12.0, "核心 HIS 生产机柜（双路 UPS）")
	if err == nil && rack != nil {
		if netCI != nil {
			_, _ = s.MountRackUnit(ctx, wsID, rack.ID, netCI.ID, netCI.Name, "m_network_dev", 41, 2, "front", 320, "info")
		}
		if hostApp1 != nil {
			_, _ = s.MountRackUnit(ctx, wsID, rack.ID, hostApp1.ID, hostApp1.Name, "m_host", 28, 2, "front", 550, "primary")
		}
		if hostDb1 != nil {
			_, _ = s.MountRackUnit(ctx, wsID, rack.ID, hostDb1.ID, hostDb1.Name, "m_host", 22, 4, "front", 850, "warn")
		}
		if hostEdge1 != nil {
			_, _ = s.MountRackUnit(ctx, wsID, rack.ID, hostEdge1.ID, hostEdge1.Name, "m_host", 16, 2, "front", 380, "ok")
		}
	}

	// Seed Starter IPAM CIDR Prefix & Allocated IPs
	pfx, err := s.CreatePrefix(ctx, wsID, "10.20.10.0/24", 1020, "IDC-SH01", "vrf-prod", "10.20.10.1", "生产核心业务与数据库网段")
	if err == nil && pfx != nil {
		if netCI != nil {
			_, _ = s.AllocateIP(ctx, wsID, pfx.ID, "10.20.10.1", "reserved", netCI.ID, "gw.prod.his.local", "网络与安全组", "核心网关 VIP")
		}
		if hostApp1 != nil {
			_, _ = s.AllocateIP(ctx, wsID, pfx.ID, "10.20.10.11", "active", hostApp1.ID, "his-app-prod-01.his.local", "SRE 核心组", "核心应用主机")
		}
		if hostDb1 != nil {
			_, _ = s.AllocateIP(ctx, wsID, pfx.ID, "10.20.10.21", "active", hostDb1.ID, "his-pg-prod-01.his.local", "DBA 团队", "PostgreSQL 主库")
		}
		if hostEdge1 != nil {
			_, _ = s.AllocateIP(ctx, wsID, pfx.ID, "10.20.10.31", "active", hostEdge1.ID, "his-gw-dmz-01.his.local", "网络与安全组", "DMZ 接入网关")
		}
	}

	return nil
}
