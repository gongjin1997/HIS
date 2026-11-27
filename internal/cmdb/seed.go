package cmdb

import (
	"context"
	"encoding/json"
	"time"

	"github.com/kinstall/kinstall/internal/domain"
)

// EnsureDefaults idempotently seeds enterprise bk-cmdb / VeOps / NetBox built-in meta-models,
// attribute schemas, model relations, service templates, dual topology roots, sample rack & IPAM prefix.
func (s *Service) EnsureDefaults(ctx context.Context, projectID string) error {
	if projectID == "" {
		return nil
	}
	var existing int
	if err := s.readDB.ScanRaw(ctx, `SELECT COUNT(1) FROM cmdb_models WHERE project_id = ?`, []any{projectID}, &existing); err == nil && existing > 0 {
		return nil
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)

	groups := []struct {
		code, name string
		sort       int
	}{
		{"t_biz", "业务与组织拓扑", 1},
		{"t_host", "计算与主机资产", 2},
		{"t_mid", "数据库与中间件", 3},
		{"t_net", "网络设备与域名", 4},
		{"t_dcim", "机房与基础设施", 5},
		{"t_app", "应用与进程实例", 6},
	}
	for _, g := range groups {
		_, _ = s.writeDB.ExecRaw(ctx,
			`INSERT INTO cmdb_model_groups (id, project_id, code, name, sort_order, is_builtin, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?) ON CONFLICT (project_id, code) DO NOTHING`,
			domain.NewID("mgrp"), projectID, g.code, g.name, g.sort, true, now,
		)
	}

	models := []struct {
		group, code, name, icon, desc string
		x, y                          float64
		mainline                      bool
		parent                        string
	}{
		{"t_biz", "m_biz", "业务系统", "Building2", "企业级核心业务域（如 HIS 门诊、急诊、支付核心）", 60, 40, true, ""},
		{"t_biz", "m_set", "集群环境", "Layers", "业务下属集群/环境划分（如 prod-az1、uat-core）", 320, 40, true, "m_biz"},
		{"t_biz", "m_module", "服务模块", "Boxes", "微服务或组件逻辑模块，支持绑定标准化服务模板", 580, 40, true, "m_set"},
		{"t_host", "m_host", "主机节点", "Server", "物理机/虚拟机/国产信创主机，支持 SSH 与 Agent 双模采集", 580, 230, false, ""},
		{"t_app", "m_process", "服务进程", "Cpu", "运行在主机上的守护进程、监听端口与启停定义", 860, 230, false, ""},
		{"t_mid", "m_database", "数据库实例", "Database", "PostgreSQL / MySQL / 达梦 / OpenGauss 等核心数据库 CI", 320, 410, false, ""},
		{"t_mid", "m_middleware", "中间件服务", "Workflow", "Valkey / Kafka / Nginx / OpenSearch 等中间件资产", 580, 410, false, ""},
		{"t_net", "m_domain", "域名与证书", "Globe", "外部/内网业务域名、TLS 证书到期日与上游映射", 860, 40, false, ""},
		{"t_dcim", "m_site", "机房数据中心", "Landmark", "IDC 数据中心物理站点与地区归属", 60, 230, false, ""},
		{"t_dcim", "m_rack", "物理机柜", "HardDrive", "标准 42U 物理机柜，支持 U 位立面图与功率监控", 320, 230, false, ""},
		{"t_net", "m_switch", "网络交换机", "Network", "核心/接入交换机、防火墙与负载均衡网络设备", 60, 410, false, ""},
	}
	for _, m := range models {
		_, _ = s.writeDB.ExecRaw(ctx,
			`INSERT INTO cmdb_models (id, project_id, group_code, code, name, icon, description, pos_x, pos_y, is_builtin, is_mainline, mainline_parent_code, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT (project_id, code) DO NOTHING`,
			domain.NewID("mdl"), projectID, m.group, m.code, m.name, m.icon, m.desc, m.x, m.y, true, m.mainline, m.parent, now, now,
		)
	}

	type attrSeed struct {
		model, group, propID, propName string
		ft                             FieldType
		unit                           string
		req, ro, uniq                  bool
		enums                          []string
		source, collector              string
	}
	attrs := []attrSeed{
		// m_biz
		{"m_biz", "基础属性", "biz_code", "业务编码", FieldString, "", true, false, true, nil, "manual", ""},
		{"m_biz", "基础属性", "sla_tier", "SLA 等级", FieldEnum, "", true, false, false, []string{"Tier-0 核心", "Tier-1 重要", "Tier-2 常规"}, "manual", ""},
		{"m_biz", "运维属性", "dept", "归属部门", FieldString, "", false, false, false, nil, "manual", ""},
		// m_host
		{"m_host", "网络与定位", "inner_ip", "内网 IP", FieldIP, "", true, false, true, nil, "collect", "os_info"},
		{"m_host", "硬件规格", "os_distro", "操作系统", FieldString, "", false, false, false, nil, "collect", "os_info"},
		{"m_host", "硬件规格", "cpu_cores", "CPU 核数", FieldInt, "vCPU", false, false, false, nil, "collect", "cpu_info"},
		{"m_host", "硬件规格", "mem_gb", "内存容量", FieldDouble, "GiB", false, false, false, nil, "collect", "mem_info"},
		{"m_host", "硬件规格", "disk_gb", "系统盘容量", FieldInt, "GiB", false, false, false, nil, "collect", "disk_info"},
		{"m_host", "物理位置", "arch", "CPU 架构", FieldEnum, "", false, false, false, []string{"amd64", "arm64", "loong64"}, "collect", "os_info"},
		{"m_host", "物理位置", "rack_slot", "机柜 U 位", FieldString, "", false, false, false, nil, "manual", ""},
		// m_database
		{"m_database", "基础属性", "engine", "数据库引擎", FieldEnum, "", true, false, false, []string{"PostgreSQL", "MySQL", "MariaDB", "OpenGauss", "MongoDB", "DM8"}, "manual", ""},
		{"m_database", "基础属性", "version", "版本号", FieldString, "", true, false, false, nil, "manual", ""},
		{"m_database", "网络属性", "endpoint", "连接地址", FieldString, "", true, false, true, nil, "manual", ""},
		{"m_database", "网络属性", "port", "监听端口", FieldInt, "", true, false, false, nil, "manual", ""},
		{"m_database", "高可用", "ha_mode", "部署架构", FieldEnum, "", false, false, false, []string{"standalone", "primary-replica", "patroni-ha", "cluster"}, "manual", ""},
		// m_middleware
		{"m_middleware", "基础属性", "product", "组件类型", FieldEnum, "", true, false, false, []string{"Nginx", "Valkey/Redis", "Kafka", "RabbitMQ", "OpenSearch", "MinIO", "Harbor"}, "manual", ""},
		{"m_middleware", "基础属性", "version", "版本号", FieldString, "", false, false, false, nil, "manual", ""},
		{"m_middleware", "网络属性", "endpoint", "访问端点", FieldString, "", true, false, false, nil, "manual", ""},
		{"m_middleware", "高可用", "deploy_target", "部署形态", FieldEnum, "", false, false, false, []string{"host-binary", "host-container", "kubernetes"}, "manual", ""},
		// m_domain
		{"m_domain", "基础属性", "fqdn", "完整域名", FieldString, "", true, false, true, nil, "manual", ""},
		{"m_domain", "证书治理", "tls_enabled", "启用 HTTPS", FieldBool, "", false, false, false, nil, "manual", ""},
		{"m_domain", "证书治理", "cert_expire_date", "证书到期日", FieldDate, "", false, false, false, nil, "manual", ""},
		{"m_domain", "网络属性", "upstream_vip", "上游 VIP/网关", FieldIP, "", false, false, false, nil, "manual", ""},
		// m_switch
		{"m_switch", "基础属性", "mgmt_ip", "管理 IP", FieldIP, "", true, false, true, nil, "manual", ""},
		{"m_switch", "基础属性", "vendor", "设备厂商", FieldEnum, "", false, false, false, []string{"Huawei", "H3C", "Cisco", "Ruijie", "Arista"}, "manual", ""},
		{"m_switch", "物理属性", "ports_count", "端口数量", FieldInt, "口", false, false, false, nil, "manual", ""},
		// m_process
		{"m_process", "运行属性", "binary_name", "进程名", FieldString, "", true, false, false, nil, "scan", "process_scan"},
		{"m_process", "运行属性", "listen_port", "监听端口", FieldInt, "", false, false, false, nil, "scan", "ports_scan"},
		{"m_process", "运行属性", "run_user", "运行账号", FieldString, "", false, false, false, nil, "scan", "os_users"},
		// m_site / m_rack
		{"m_site", "位置属性", "city", "所在城市", FieldString, "", true, false, false, nil, "manual", ""},
		{"m_site", "位置属性", "tier", "机房等级", FieldEnum, "", false, false, false, []string{"T3+", "T3", "T2", "边缘机房"}, "manual", ""},
		{"m_rack", "物理属性", "site_name", "所属机房", FieldString, "", true, false, false, nil, "manual", ""},
		{"m_rack", "物理属性", "u_height", "总 U 高", FieldInt, "U", false, false, false, nil, "manual", ""},
	}
	for i, a := range attrs {
		enumsB, _ := json.Marshal(a.enums)
		if len(a.enums) == 0 {
			enumsB = []byte("[]")
		}
		_, _ = s.writeDB.ExecRaw(ctx,
			`INSERT INTO cmdb_model_attributes (id, project_id, model_code, attr_group, prop_id, prop_name, field_type, unit, required, readonly, is_unique, display, enum_options_json, default_value, source_kind, collector_plugin_id, sort_order)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, '', ?, ?, ?) ON CONFLICT (project_id, model_code, prop_id) DO NOTHING`,
			domain.NewID("mattr"), projectID, a.model, a.group, a.propID, a.propName, string(a.ft), a.unit, a.req, a.ro, a.uniq, true, string(enumsB), a.source, a.collector, i+1,
		)
	}

	rels := []struct{ src, tgt, rtype, label, kind string }{
		{"m_biz", "m_set", "contains", "包含集群", "1:N"},
		{"m_set", "m_module", "contains", "包含模块", "1:N"},
		{"m_module", "m_host", "runs_on", "部署主机", "N:N"},
		{"m_host", "m_process", "hosts", "运行进程", "1:N"},
		{"m_site", "m_rack", "contains", "容纳机柜", "1:N"},
		{"m_rack", "m_host", "mounted_in", "上架主机", "1:N"},
		{"m_rack", "m_switch", "mounted_in", "上架网元", "1:N"},
		{"m_database", "m_host", "runs_on", "运行于", "N:N"},
		{"m_middleware", "m_host", "runs_on", "运行于", "N:N"},
		{"m_domain", "m_module", "routes_to", "路由至", "N:N"},
	}
	for _, r := range rels {
		_, _ = s.writeDB.ExecRaw(ctx,
			`INSERT INTO cmdb_model_relations (id, project_id, source_model, target_model, relation_type, label, mapping_kind, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT (project_id, source_model, target_model, relation_type) DO NOTHING`,
			domain.NewID("mrel"), projectID, r.src, r.tgt, r.rtype, r.label, r.kind, now,
		)
	}

	// Default Service Templates (bk-cmdb style)
	tpl1Proc, _ := json.Marshal([]ServiceTemplateProcess{
		{Name: "nginx-gateway", Binary: "/usr/sbin/nginx", User: "www-data", BindPort: 80, Protocol: "TCP", HealthCheck: "http://127.0.0.1/healthz"},
	})
	tpl2Proc, _ := json.Marshal([]ServiceTemplateProcess{
		{Name: "postgresql-primary", Binary: "/usr/lib/postgresql/bin/postgres", User: "postgres", BindPort: 5432, Protocol: "TCP", HealthCheck: "pg_isready -p 5432"},
		{Name: "pgbouncer-pool", Binary: "/usr/bin/pgbouncer", User: "postgres", BindPort: 6432, Protocol: "TCP"},
	})
	_, _ = s.writeDB.ExecRaw(ctx,
		`INSERT INTO cmdb_service_templates (id, project_id, name, category, description, processes_json, ports_json, default_params_json, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT (project_id, name) DO NOTHING`,
		domain.NewID("stpl"), projectID, "标准 Web 接入网关模板", "接入层", "统一 Nginx / OpenResty 80/443 接入进程与健康探针基线", string(tpl1Proc), `[80,443]`, `{"worker_processes":"auto"}`, now, now,
	)
	_, _ = s.writeDB.ExecRaw(ctx,
		`INSERT INTO cmdb_service_templates (id, project_id, name, category, description, processes_json, ports_json, default_params_json, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT (project_id, name) DO NOTHING`,
		domain.NewID("stpl"), projectID, "核心 PostgreSQL 高可用服务模板", "数据层", "PostgreSQL 主库 + PgBouncer 连接池进程标准模板", string(tpl2Proc), `[5432,6432]`, `{"max_connections":500}`, now, now,
	)

	// Default Dual Topology starter tree (Business Mainline + Resource Infrastructure)
	var topoCnt int
	_ = s.readDB.ScanRaw(ctx, `SELECT COUNT(1) FROM cmdb_topology_nodes WHERE project_id = ?`, []any{projectID}, &topoCnt)
	if topoCnt == 0 {
		bizID := domain.NewID("topo")
		setProdID := domain.NewID("topo")
		modWebID := domain.NewID("topo")
		modDBID := domain.NewID("topo")
		siteID := domain.NewID("topo")
		zoneCoreID := domain.NewID("topo")
		zoneDMZID := domain.NewID("topo")

		nodes := []struct {
			id, kind, parent, model, name string
			sort                          int
		}{
			{bizID, "business", "", "m_biz", "核心业务域 (Core)", 1},
			{setProdID, "business", bizID, "m_set", "生产主集群 (prod-az1)", 1},
			{modWebID, "business", setProdID, "m_module", "前端网关与接入模块", 1},
			{modDBID, "business", setProdID, "m_module", "核心数据库与缓存模块", 2},
			{siteID, "resource", "", "m_site", "北京亦庄主数据中心 (BJ-IDC-01)", 1},
			{zoneCoreID, "resource", siteID, "m_rack", "核心数据区 · A01 机柜", 1},
			{zoneDMZID, "resource", siteID, "m_rack", "DMZ 接入区 · B02 机柜", 2},
		}
		for _, n := range nodes {
			_, _ = s.writeDB.ExecRaw(ctx,
				`INSERT INTO cmdb_topology_nodes (id, project_id, topo_kind, parent_id, model_code, name, service_template_id, metadata_json, sort_order, created_at)
				 VALUES (?, ?, ?, ?, ?, ?, '', '{}', ?, ?)`,
				n.id, projectID, n.kind, n.parent, n.model, n.name, n.sort, now,
			)
		}
	}

	// Default Starter Rack (42U) & IPAM Prefix so DCIM/IPAM view renders richly out of the box
	var rackCnt int
	_ = s.readDB.ScanRaw(ctx, `SELECT COUNT(1) FROM dcim_racks WHERE project_id = ?`, []any{projectID}, &rackCnt)
	if rackCnt == 0 {
		rackID := domain.NewID("rack")
		_, _ = s.writeDB.ExecRaw(ctx,
			`INSERT INTO dcim_racks (id, project_id, site_name, room_name, name, u_height, power_kw, status, notes, created_at)
			 VALUES (?, ?, ?, ?, ?, 42, 10.0, 'active', '默认核心业务机柜（支持点击空 U 位直接上架设备）', ?)`,
			rackID, projectID, "BJ-IDC-01", "A区主机房", "A01-CORE", now,
		)
		starterUnits := []struct {
			name, role, color     string
			startU, uSize, powerW int
		}{
			{"TOR-SW-A01-01 (万兆核心)", "switch", "accent", 41, 2, 320},
			{"FW-DMZ-01 (下一代防火墙)", "firewall", "warn", 38, 2, 280},
			{"k8s-ctrl-01 (管理控制面)", "server", "primary", 30, 2, 550},
			{"pg-ha-primary-01 (核心数据库)", "server", "ok", 26, 4, 920},
			{"san-storage-01 (全闪存阵列)", "storage", "info", 10, 4, 1200},
		}
		for _, u := range starterUnits {
			_, _ = s.writeDB.ExecRaw(ctx,
				`INSERT INTO dcim_rack_units (id, project_id, rack_id, ci_id, device_name, device_role, start_u, u_size, power_w, status, color_tag, created_at)
				 VALUES (?, ?, ?, '', ?, ?, ?, ?, ?, 'active', ?, ?)`,
				domain.NewID("ru"), projectID, rackID, u.name, u.role, u.startU, u.uSize, u.powerW, u.color, now,
			)
		}
	}

	var pfxCnt int
	_ = s.readDB.ScanRaw(ctx, `SELECT COUNT(1) FROM ipam_prefixes WHERE project_id = ?`, []any{projectID}, &pfxCnt)
	if pfxCnt == 0 {
		pfxID := domain.NewID("pfx")
		_, _ = s.writeDB.ExecRaw(ctx,
			`INSERT INTO ipam_prefixes (id, project_id, cidr, vlan_id, vrf, site_name, gateway, role, description, created_at)
			 VALUES (?, ?, '10.20.10.0/24', 110, 'prod-core', 'BJ-IDC-01', '10.20.10.1', 'business', '核心业务内网生产网段', ?)`,
			pfxID, projectID, now,
		)
		starterIPs := []struct{ ip, st, host, desc string }{
			{"10.20.10.1", "reserved", "gw-vlan110", "核心网关 VIP"},
			{"10.20.10.10", "active", "k8s-ctrl-01", "控制面节点 01"},
			{"10.20.10.20", "active", "pg-ha-primary-01", "核心 PostgreSQL 主库"},
		}
		for _, ip := range starterIPs {
			_, _ = s.writeDB.ExecRaw(ctx,
				`INSERT INTO ipam_addresses (id, project_id, prefix_id, ip_address, status, ci_id, host_id, hostname, mac_address, description, updated_at)
				 VALUES (?, ?, ?, ?, ?, '', '', ?, '', ?, ?)`,
				domain.NewID("ip"), projectID, pfxID, ip.ip, ip.st, ip.host, ip.desc, now,
			)
		}
	}

	// Default sample CI instances + lineage relations so CI graph works immediately
	var ciCnt int
	_ = s.readDB.ScanRaw(ctx, `SELECT COUNT(1) FROM cmdb_ci_instances WHERE project_id = ?`, []any{projectID}, &ciCnt)
	if ciCnt == 0 {
		bizCI := domain.NewID("ci")
		hostCI := domain.NewID("ci")
		dbCI := domain.NewID("ci")
		domCI := domain.NewID("ci")
		samples := []struct {
			id, model, name, ukey, env, owner, attrs string
		}{
			{bizCI, "m_biz", "核心交易与门户系统", "biz-core-01", "prod", "platform-sre", `{"biz_code":"BIZ-CORE-01","sla_tier":"Tier-0 核心","dept":"基础架构部"}`},
			{hostCI, "m_host", "prod-node-01", "10.20.10.10", "prod", "infra-ops", `{"inner_ip":"10.20.10.10","os_distro":"Ubuntu 24.04 LTS","cpu_cores":16,"mem_gb":64,"disk_gb":500,"arch":"amd64","rack_slot":"A01-U30"}`},
			{dbCI, "m_database", "pg-core-prod", "10.20.10.20:5432", "prod", "dba-team", `{"engine":"PostgreSQL","version":"18.7","endpoint":"10.20.10.20:5432","port":5432,"ha_mode":"patroni-ha"}`},
			{domCI, "m_domain", "portal.internal.corp", "portal.internal.corp", "prod", "net-ops", `{"fqdn":"portal.internal.corp","tls_enabled":true,"cert_expire_date":"2027-06-30","upstream_vip":"10.20.10.10"}`},
		}
		for _, c := range samples {
			_, _ = s.writeDB.ExecRaw(ctx,
				`INSERT INTO cmdb_ci_instances (id, project_id, model_code, name, unique_key, status, env, owner, attrs_json, version, created_by, created_at, updated_at)
				 VALUES (?, ?, ?, ?, ?, 'active', ?, ?, ?, 1, 'system', ?, ?)`,
				c.id, projectID, c.model, c.name, c.ukey, c.env, c.owner, c.attrs, now, now,
			)
		}
		_, _ = s.writeDB.ExecRaw(ctx,
			`INSERT INTO cmdb_ci_relations (id, project_id, source_ci_id, target_ci_id, relation_type, label, created_at) VALUES (?, ?, ?, ?, 'depends_on', '依赖数据库', ?)`,
			domain.NewID("cirel"), projectID, bizCI, dbCI, now,
		)
		_, _ = s.writeDB.ExecRaw(ctx,
			`INSERT INTO cmdb_ci_relations (id, project_id, source_ci_id, target_ci_id, relation_type, label, created_at) VALUES (?, ?, ?, ?, 'runs_on', '运行于主机', ?)`,
			domain.NewID("cirel"), projectID, dbCI, hostCI, now,
		)
		_, _ = s.writeDB.ExecRaw(ctx,
			`INSERT INTO cmdb_ci_relations (id, project_id, source_ci_id, target_ci_id, relation_type, label, created_at) VALUES (?, ?, ?, ?, 'routes_to', '入口流量路由', ?)`,
			domain.NewID("cirel"), projectID, domCI, bizCI, now,
		)
	}
	return nil
}
