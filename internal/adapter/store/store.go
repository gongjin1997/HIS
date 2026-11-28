package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/kinstall/kinstall/internal/domain"
	"github.com/kinstall/kinstall/pkg/platform/audit"
	"github.com/kinstall/kinstall/pkg/platform/auth"
	"github.com/kinstall/kinstall/pkg/platform/crypto"
	"github.com/kinstall/kinstall/pkg/platform/db"
	"github.com/kinstall/kinstall/pkg/platform/id"
)

// Repo is the transactional multi-tenant persistence adapter for LiveOps foundation domains.
type Repo struct {
	db          *db.Store
	clock       id.Clock
	codec       *crypto.EnvelopeCodec
	auditSecret []byte
}

func New(store *db.Store, clock id.Clock, codec *crypto.EnvelopeCodec, auditSecret []byte) *Repo {
	if clock == nil {
		clock = id.RealClock{}
	}
	if len(auditSecret) == 0 {
		auditSecret = []byte("liveops-foundation-audit-hmac-key-v1")
	}
	return &Repo{
		db:          store,
		clock:       clock,
		codec:       codec,
		auditSecret: append([]byte(nil), auditSecret...),
	}
}

func (r *Repo) DB() *db.Store       { return r.db }
func (r *Repo) Clock() id.Clock     { return r.clock }
func (r *Repo) AuditSecret() []byte { return r.auditSecret }
func (r *Repo) Now() time.Time      { return r.clock.Now().UTC() }
func (r *Repo) NowStr() string      { return formatTime(r.Now()) }

func formatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

func formatTimePtr(t *time.Time) string {
	if t == nil {
		return ""
	}
	return formatTime(*t)
}

func parseTime(s string) time.Time {
	if strings.TrimSpace(s) == "" {
		return time.Time{}
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t.UTC()
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC()
	}
	return time.Time{}
}

func parseTimePtr(s string) *time.Time {
	t := parseTime(s)
	if t.IsZero() {
		return nil
	}
	return &t
}

func marshalJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(b)
}

func unmarshalJSON(raw string, dst any) {
	if strings.TrimSpace(raw) == "" {
		return
	}
	_ = json.Unmarshal([]byte(raw), dst)
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique constraint") ||
		strings.Contains(msg, "duplicate key") ||
		strings.Contains(msg, "unique failed")
}

func isFKViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "foreign key constraint") ||
		strings.Contains(msg, "violates foreign key")
}

func dbScope(s domain.Scope) db.Scope {
	return db.Scope{TenantID: s.TenantID, AppID: s.AppID}
}

// Seed initializes an idempotent, realistic multi-tenant KubeSphere/LiveOps baseline on first boot.
func (r *Repo) Seed(ctx context.Context) error {
	var count int
	if err := r.db.QueryRow(ctx, `SELECT COUNT(*) FROM iam_tenants`).Scan(&count); err != nil {
		return fmt.Errorf("check seed status: %w", err)
	}
	if count > 0 {
		return nil
	}

	now := r.NowStr()
	adminHash, err := crypto.HashPassword("LiveOps#2026!")
	if err != nil {
		return fmt.Errorf("hash admin password: %w", err)
	}
	opHash, err := crypto.HashPassword("Operator#2026!")
	if err != nil {
		return fmt.Errorf("hash operator password: %w", err)
	}
	audHash, err := crypto.HashPassword("Auditor#2026!")
	if err != nil {
		return fmt.Errorf("hash auditor password: %w", err)
	}

	return r.db.WithTx(ctx, func(ctx context.Context, tx *db.Tx) error {
		// 1. Seed Permissions Catalog
		perms := []domain.Permission{
			{PermCode: "*", Domain: "platform", Name: "平台超级权限", Description: "全平台所有资源完全控制权限"},
			{PermCode: auth.PermPlatformAdmin, Domain: "platform", Name: "平台管理", Description: "租户/企业空间、节点池与全局策略治理"},
			{PermCode: auth.PermIAMRead, Domain: "iam", Name: "身份只读", Description: "查看租户、应用、项目、用户与角色"},
			{PermCode: auth.PermIAMWrite, Domain: "iam", Name: "RBAC 管理", Description: "管理用户、角色授权与路由鉴权策略"},
			{PermCode: auth.PermIAMAppAdmin, Domain: "iam", Name: "应用与密钥管理", Description: "管理应用及双轨 AppKey 轮换"},
			{PermCode: auth.PermIAMAuditRead, Domain: "audit", Name: "审计只读", Description: "查询防篡改审计链日志"},
			{PermCode: auth.PermIAMAuditVerify, Domain: "audit", Name: "审计链验签", Description: "执行全链 HMAC-SHA256 完整性校验"},
			{PermCode: auth.PermAssetsRead, Domain: "assets", Name: "CMDB 只读", Description: "查看模型、实例与双视角拓扑"},
			{PermCode: auth.PermAssetsWrite, Domain: "assets", Name: "CMDB 维护", Description: "变更模型、实例、拓扑关系与 Excel 批导"},
			{PermCode: auth.PermConfigRead, Domain: "config", Name: "配置只读", Description: "查看配置集、历史快照与环境合集"},
			{PermCode: auth.PermConfigWrite, Domain: "config", Name: "配置变更", Description: "创建/更新配置版本与环境编排"},
			{PermCode: auth.PermConfigReveal, Domain: "config", Name: "密文明文揭示", Description: "经强审计揭示 KMS 加密配置明文"},
			{PermCode: auth.PermDeployJobSubmit, Domain: "deploy", Name: "部署作业提交", Description: "提交 DAG 部署作业（强制 Idempotency-Key）"},
			{PermCode: auth.PermDeployJobRead, Domain: "deploy", Name: "部署作业只读", Description: "查看作业快照、任务租约与尝试记录"},
			{PermCode: auth.PermDeployJobObserve, Domain: "deploy", Name: "时间线订阅", Description: "订阅部署作业 SSE 实时事件流"},
			{PermCode: auth.PermDeployJobCancel, Domain: "deploy", Name: "部署作业取消", Description: "通过 If-Match 乐观锁发起作业取消"},
			{PermCode: auth.PermNodesRead, Domain: "nodes", Name: "节点只读", Description: "查看零信任节点心跳、主机组与分发进度"},
			{PermCode: auth.PermNodesAdmin, Domain: "nodes", Name: "节点管控", Description: "签发注册令牌、轮换凭据、隔离或吊销节点"},
			{PermCode: auth.PermGatewayRead, Domain: "gateway", Name: "网关路由只读", Description: "查看动态路由与默认路由版本"},
			{PermCode: auth.PermGatewayWrite, Domain: "gateway", Name: "网关路由变更", Description: "管理路由与原子切换默认路由"},
			{PermCode: auth.PermGatewayMonitor, Domain: "gateway", Name: "网关指标监控", Description: "查看网关按日/月统计延迟与错率"},
		}
		for _, p := range perms {
			if _, err := tx.Exec(ctx,
				`INSERT INTO iam_perms (perm_code, domain, name, description) VALUES (?, ?, ?, ?)`,
				p.PermCode, p.Domain, p.Name, p.Description,
			); err != nil {
				return err
			}
		}

		// 2. Seed Two Multi-Tenant Workspaces (KubeSphere Enterprise Workspaces)
		tenants := []struct{ id, code, name, desc string }{
			{"default-tenant", "core-hq", "总部核心企业空间", "承载院区核心临床业务与统一基础设施治理的主企业空间"},
			{"clinical-tenant", "clinical-rd", "临床研发企业空间", "面向临床研发与预发布演练团队的独立业务隔离空间"},
		}
		for _, t := range tenants {
			if _, err := tx.Exec(ctx,
				`INSERT INTO iam_tenants (tenant_id, code, name, description, status, created_at, updated_at) VALUES (?, ?, ?, ?, 'ACTIVE', ?, ?)`,
				t.id, t.code, t.name, t.desc, now, now,
			); err != nil {
				return err
			}
		}

		// 3. Seed Applications, Scope Versions & Gateway Revisions
		apps := []struct{ tid, aid, code, name, desc string }{
			{"default-tenant", "his-core", "his-core", "HIS 核心业务集群", "住院/门诊/医嘱核心微服务集群底座"},
			{"default-tenant", "platform-ops", "platform-ops", "平台运维控制面", "统一 CMDB、配置中心与发布编排底座"},
			{"clinical-tenant", "emr-suite", "emr-suite", "电子病历套件", "临床研发空间隔离应用"},
		}
		for _, a := range apps {
			if _, err := tx.Exec(ctx,
				`INSERT INTO iam_apps (tenant_id, app_id, code, name, description, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, 'ACTIVE', ?, ?)`,
				a.tid, a.aid, a.code, a.name, a.desc, now, now,
			); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx,
				`INSERT INTO iam_scope_versions (tenant_id, app_id, scope_version, updated_at) VALUES (?, ?, 1, ?)`,
				a.tid, a.aid, now,
			); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx,
				`INSERT INTO gw_revisions (tenant_id, app_id, route_version, updated_at) VALUES (?, ?, 1, ?)`,
				a.tid, a.aid, now,
			); err != nil {
				return err
			}
		}

		// 4. Seed Projects under Apps
		projects := []struct{ tid, aid, pid, code, name, desc string }{
			{"default-tenant", "his-core", "prod-clinical", "prod-clinical", "生产临床区", "三甲医院核心生产环境项目"},
			{"default-tenant", "his-core", "staging-lab", "staging-lab", "预发布演练区", "全链路压测与灰度发布项目"},
			{"default-tenant", "platform-ops", "ops-control", "ops-control", "平台控制面项目", "底座资产、配置与节点管控项目"},
			{"clinical-tenant", "emr-suite", "emr-dev", "emr-dev", "病历研发项目", "隔离租户研发项目"},
		}
		for _, p := range projects {
			if _, err := tx.Exec(ctx,
				`INSERT INTO iam_projects (tenant_id, app_id, project_id, code, name, description, permission_version, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, 1, 'ACTIVE', ?, ?)`,
				p.tid, p.aid, p.pid, p.code, p.name, p.desc, now, now,
			); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx,
				`INSERT INTO iam_project_roles (tenant_id, app_id, project_id, role_id, role_name, description, created_at) VALUES (?, ?, ?, 'proj-admin', '项目管理员', '管理该项目下的资产、配置与部署作业', ?)`,
				p.tid, p.aid, p.pid, now,
			); err != nil {
				return err
			}
			for _, perm := range []string{auth.PermAssetsRead, auth.PermAssetsWrite, auth.PermConfigRead, auth.PermConfigWrite, auth.PermDeployJobRead, auth.PermDeployJobSubmit, auth.PermDeployJobObserve, auth.PermDeployJobCancel} {
				if _, err := tx.Exec(ctx,
					`INSERT INTO iam_project_role_perms (tenant_id, app_id, project_id, role_id, perm_code) VALUES (?, ?, ?, 'proj-admin', ?)`,
					p.tid, p.aid, p.pid, perm,
				); err != nil {
					return err
				}
			}
		}

		// 5. Seed Roles, Role Permissions & Users
		roles := []struct {
			id, tid, code, name, level, desc string
			perms                            []string
		}{
			{"role-platform-admin", "default-tenant", "platform-admin", "平台超级管理员", "PLATFORM", "全平台治理与安全策略控制", []string{"*"}},
			{"role-workspace-op", "default-tenant", "workspace-operator", "企业空间运维员", "WORKSPACE", "日常 CMDB、配置发布与作业执行权限", []string{
				auth.PermIAMRead, auth.PermAssetsRead, auth.PermAssetsWrite,
				auth.PermConfigRead, auth.PermConfigWrite,
				auth.PermDeployJobRead, auth.PermDeployJobSubmit, auth.PermDeployJobObserve, auth.PermDeployJobCancel,
				auth.PermNodesRead, auth.PermGatewayRead, auth.PermGatewayMonitor,
			}},
			{"role-auditor", "default-tenant", "security-auditor", "安全审计员", "PLATFORM", "只读查看全平台资源并校验防篡改审计链", []string{
				auth.PermIAMRead, auth.PermIAMAuditRead, auth.PermIAMAuditVerify,
				auth.PermAssetsRead, auth.PermConfigRead, auth.PermDeployJobRead, auth.PermDeployJobObserve,
				auth.PermNodesRead, auth.PermGatewayRead, auth.PermGatewayMonitor,
			}},
		}
		for _, ro := range roles {
			if _, err := tx.Exec(ctx,
				`INSERT INTO iam_roles (role_id, tenant_id, code, name, scope_level, description, is_builtin, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, 1, ?, ?)`,
				ro.id, ro.tid, ro.code, ro.name, ro.level, ro.desc, now, now,
			); err != nil {
				return err
			}
			for _, p := range ro.perms {
				if _, err := tx.Exec(ctx, `INSERT INTO iam_role_perms (role_id, perm_code) VALUES (?, ?)`, ro.id, p); err != nil {
					return err
				}
			}
		}

		users := []struct{ uid, tid, username, display, email, hash, roleID string }{
			{"usr-admin", "default-tenant", "admin", "平台管理员", "admin@liveops.local", adminHash, "role-platform-admin"},
			{"usr-operator", "default-tenant", "operator", "发布运维师", "ops@liveops.local", opHash, "role-workspace-op"},
			{"usr-auditor", "default-tenant", "auditor", "合规审计官", "audit@liveops.local", audHash, "role-auditor"},
		}
		for _, u := range users {
			if _, err := tx.Exec(ctx,
				`INSERT INTO iam_users (user_id, tenant_id, username, display_name, email, password_hash, status, failed_attempts, locked_until, last_login_at, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, 'ACTIVE', 0, '', '', ?, ?)`,
				u.uid, u.tid, u.username, u.display, u.email, u.hash, now, now,
			); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx,
				`INSERT INTO iam_user_roles (user_id, role_id, tenant_id, app_id, project_id, created_at) VALUES (?, ?, ?, '', '', ?)`,
				u.uid, u.roleID, u.tid, now,
			); err != nil {
				return err
			}
		}

		// 6. Seed Default-Deny Route Policies
		routePolicies := []struct{ id, method, pattern, perm, scope, desc string }{
			{"pol-iam-read", "GET", "/api/v1/iam/*", auth.PermIAMRead, "PLATFORM", "身份与企业空间查询"},
			{"pol-iam-admin", "POST", "/api/v1/iam/*", auth.PermIAMWrite, "PLATFORM", "租户、用户、角色变更"},
			{"pol-audit-verify", "POST", "/api/v1/iam/audit/verify", auth.PermIAMAuditVerify, "PLATFORM", "防篡改审计链校验"},
			{"pol-assets-write", "POST", "/api/v1/assets/*", auth.PermAssetsWrite, "PROJECT", "CMDB 模型/实例/拓扑写入"},
			{"pol-config-reveal", "POST", "/api/v1/config/configurations/*/reveal", auth.PermConfigReveal, "PROJECT", "KMS 加密配置明文揭示"},
			{"pol-deploy-submit", "POST", "/api/v1/deploy/jobs", auth.PermDeployJobSubmit, "PROJECT", "提交 DAG 部署作业"},
			{"pol-deploy-cancel", "POST", "/api/v1/deploy/jobs/*/cancel", auth.PermDeployJobCancel, "PROJECT", "取消部署作业"},
			{"pol-nodes-admin", "POST", "/api/v1/nodes/*", auth.PermNodesAdmin, "PLATFORM", "零信任节点纳管与轮换"},
			{"pol-gw-write", "POST", "/api/v1/gateway/*", auth.PermGatewayWrite, "PLATFORM", "动态网关路由与默认切换"},
		}
		for _, rp := range routePolicies {
			if _, err := tx.Exec(ctx,
				`INSERT INTO iam_route_policies (policy_id, tenant_id, method, path_pattern, required_perm, scope_level, enabled, description, created_at, updated_at) VALUES (?, 'default-tenant', ?, ?, ?, ?, 1, ?, ?, ?)`,
				rp.id, rp.method, rp.pattern, rp.perm, rp.scope, rp.desc, now, now,
			); err != nil {
				return err
			}
		}

		// 7. Seed CMDB Meta-Models (8 LiveOps Standard Models), Relation Types & Dual Topology for (default-tenant, his-core)
		if err := r.seedCMDB(ctx, tx, "default-tenant", "his-core", now); err != nil {
			return err
		}
		if err := r.seedCMDB(ctx, tx, "default-tenant", "platform-ops", now); err != nil {
			return err
		}

		// 8. Seed Configs (with KMS-sealed secret) & Environment for (default-tenant, his-core)
		secretVal := "DbPass#Clinical2026!"
		sealed := ""
		if r.codec != nil {
			if ct, sealErr := r.codec.Seal([]byte(secretVal), []byte("default-tenant:his-core:cfg-his-db:DB_PASSWORD")); sealErr == nil {
				sealed = ct
			}
		}
		dbEntries := []domain.ConfigEntry{
			{Key: "DB_HOST", Value: "pg-primary.clinical.svc.local", IsSecret: false, Comment: "PostgreSQL 16 主库地址"},
			{Key: "DB_PORT", Value: "5432", IsSecret: false, Comment: "数据库端口"},
			{Key: "DB_MAX_CONNS", Value: "64", IsSecret: false, Comment: "连接池上限"},
			{Key: "DB_PASSWORD", Value: "******", IsSecret: true, Ciphertext: sealed, Comment: "KMS 信封加密凭证"},
		}
		appEntries := []domain.ConfigEntry{
			{Key: "LOG_LEVEL", Value: "info", IsSecret: false, Comment: "结构化 slog 级别"},
			{Key: "DB_MAX_CONNS", Value: "128", IsSecret: false, Comment: "高并发门诊覆写连接池"},
			{Key: "SSE_HEARTBEAT_SEC", Value: "15", IsSecret: false, Comment: "SSE 心跳间隔"},
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO cfg_configurations (tenant_id, app_id, cfg_id, project_id, name, format, description, entries_json, version, deleted, created_by, updated_by, created_at, updated_at)
			 VALUES ('default-tenant','his-core','cfg-his-db','prod-clinical','his-postgres-base','kv','HIS 核心数据库基础连接与加密凭证配置',?,1,0,'usr-admin','usr-admin',?,?)`,
			marshalJSON(dbEntries), now, now,
		); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO cfg_history (tenant_id, app_id, cfg_id, version, entries_json, change_summary, changed_by, created_at)
			 VALUES ('default-tenant','his-core','cfg-his-db',1,?,'初始基线版本','usr-admin',?)`,
			marshalJSON(dbEntries), now,
		); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO cfg_configurations (tenant_id, app_id, cfg_id, project_id, name, format, description, entries_json, version, deleted, created_by, updated_by, created_at, updated_at)
			 VALUES ('default-tenant','his-core','cfg-his-runtime','prod-clinical','his-runtime-override','kv','生产高并发运行参数覆写',?,1,0,'usr-admin','usr-admin',?,?)`,
			marshalJSON(appEntries), now, now,
		); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO cfg_history (tenant_id, app_id, cfg_id, version, entries_json, change_summary, changed_by, created_at)
			 VALUES ('default-tenant','his-core','cfg-his-runtime',1,?,'初始基线版本','usr-admin',?)`,
			marshalJSON(appEntries), now,
		); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO cfg_environments (tenant_id, app_id, env_id, project_id, code, name, description, version, created_by, updated_by, created_at, updated_at)
			 VALUES ('default-tenant','his-core','env-prod-clinical','prod-clinical','prod','生产临床环境','合并基础数据库配置与门诊高并发覆写',1,'usr-admin','usr-admin',?,?)`,
			now, now,
		); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO cfg_env_members (tenant_id, app_id, env_id, cfg_id, priority) VALUES
			 ('default-tenant','his-core','env-prod-clinical','cfg-his-db',10),
			 ('default-tenant','his-core','env-prod-clinical','cfg-his-runtime',20)`,
		); err != nil {
			return err
		}

		// 9. Seed Dynamic Gateway Router, Routes & Telemetry
		if _, err := tx.Exec(ctx,
			`INSERT INTO gw_routers (tenant_id, app_id, router_id, name, listen_host, rate_limit_qps, burst, status, created_at, updated_at)
			 VALUES ('default-tenant','his-core','rtr-edge-main','核心临床入口网关','gateway.his.local',2000,400,'ACTIVE',?,?)`,
			now, now,
		); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO gw_routes (tenant_id, app_id, route_id, router_id, name, path_prefix, upstream_url, is_default, strip_prefix, timeout_ms, rate_limit_qps, status, version, created_at, updated_at) VALUES
			 ('default-tenant','his-core','rt-outpatient','rtr-edge-main','门诊主路由','/api/outpatient','http://10.20.1.11:8081',1,0,15000,1000,'ACTIVE',1,?,?),
			 ('default-tenant','his-core','rt-inpatient','rtr-edge-main','住院医嘱路由','/api/inpatient','http://10.20.1.12:8082',0,0,20000,800,'ACTIVE',1,?,?)`,
			now, now, now, now,
		); err != nil {
			return err
		}
		dayKey := r.Now().Format("2006-01-02")
		monthKey := r.Now().Format("2006-01")
		if _, err := tx.Exec(ctx,
			`INSERT INTO gw_telemetry_samples (tenant_id, app_id, router_id, bucket_kind, bucket_key, total_requests, error_requests, total_latency_ms, updated_at) VALUES
			 ('default-tenant','his-core','rtr-edge-main','day',?,48210,14,915990,?),
			 ('default-tenant','his-core','rtr-edge-main','month',?,1240890,312,24817800,?)`,
			dayKey, now, monthKey, now,
		); err != nil {
			return err
		}

		// 10. Seed One Completed Baseline DAG Deploy Job & Timeline (demonstrating D03/D04/D08/D19)
		if _, err := tx.Exec(ctx,
			`INSERT INTO deploy_jobs (tenant_id, app_id, job_id, project_id, name, trigger_type, state, cancel_requested, version, last_seq, min_retained_seq, spec_json, summary_json, created_by, created_at, updated_at, finished_at)
			 VALUES ('default-tenant','his-core','job-baseline-001','prod-clinical','HIS 核心基线配置校验与热切换','MANUAL','SUCCEEDED',0,4,4,1,'{"graph":"explicit"}','{"succeeded":3,"failed":0,"skipped":0}','usr-admin',?,?,?)`,
			now, now, now,
		); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO deploy_tasks (tenant_id, app_id, job_id, task_id, task_key, name, task_type, node_id, payload_json, state, remaining, lease_epoch, attempt_no, owner, lease_until, action_key, max_retries, last_error, result_json, started_at, finished_at, updated_at) VALUES
			 ('default-tenant','his-core','job-baseline-001','tsk-01','preflight','环境配置冲突预检','CHECK','','{"env":"env-prod-clinical"}','SUCCEEDED',0,1,1,'worker-seed','','default-tenant:job-baseline-001:tsk-01:a1',1,'','{"conflicts":1,"ok":true}',?,?,?),
			 ('default-tenant','his-core','job-baseline-001','tsk-02','sync-cfg','下发 KMS 加密配置快照','SYNC','','{"cfg":"cfg-his-db"}','SUCCEEDED',0,1,1,'worker-seed','','default-tenant:job-baseline-001:tsk-02:a1',1,'','{"version":1,"ok":true}',?,?,?),
			 ('default-tenant','his-core','job-baseline-001','tsk-03','verify-gw','探活校验网关路由版本','VERIFY','','{"router":"rtr-edge-main"}','SUCCEEDED',0,1,1,'worker-seed','','default-tenant:job-baseline-001:tsk-03:a1',0,'','{"routeVersion":1,"ok":true}',?,?,?)`,
			now, now, now, now, now, now, now, now, now,
		); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO deploy_edges (tenant_id, app_id, job_id, from_task_id, to_task_id, satisfied) VALUES
			 ('default-tenant','his-core','job-baseline-001','tsk-01','tsk-02',1),
			 ('default-tenant','his-core','job-baseline-001','tsk-02','tsk-03',1)`,
		); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO deploy_timeline (tenant_id, app_id, job_id, seq, event_type, task_id, attempt_no, lease_epoch, message, payload_json, created_at) VALUES
			 ('default-tenant','his-core','job-baseline-001',1,'job.created','',0,0,'作业已通过 Kahn 拓扑校验并原子入库','{}',?),
			 ('default-tenant','his-core','job-baseline-001',2,'task.succeeded','tsk-01',1,1,'前置环境预检完成，解锁后继配置下发任务','{}',?),
			 ('default-tenant','his-core','job-baseline-001',3,'task.succeeded','tsk-02',1,1,'配置快照 v1 下发成功，解锁网关校验任务','{}',?),
			 ('default-tenant','his-core','job-baseline-001',4,'job.completed','',0,0,'全部 3 个 DAG 任务收敛为 SUCCEEDED','{}',?)`,
			now, now, now, now,
		); err != nil {
			return err
		}

		// 11. Seed Genesis Tamper-Evident Audit Entry
		auditRec := audit.Record{
			ID:            "aud-genesis-001",
			TenantID:      "default-tenant",
			AppID:         "his-core",
			ActorID:       "system",
			ActorUsername: "bootstrap",
			ClientIP:      "127.0.0.1",
			TraceID:       "bootstrap-genesis",
			Module:        "platform",
			Action:        "BOOTSTRAP_SEED",
			ResourceType:  "tenant",
			ResourceID:    "default-tenant",
			HTTPStatus:    200,
			Summary:       "LiveOps 多租户底座与 KubeSphere 治理基线初始化完成",
			PrevHash:      "GENESIS",
			OccurredAt:    now,
		}
		auditRec.HMACSignature = audit.ComputeSignature(r.auditSecret, auditRec)
		if _, err := tx.Exec(ctx,
			`INSERT INTO iam_audit_logs (audit_id, seq, tenant_id, app_id, actor_id, actor_name, action, resource_type, resource_id, outcome, request_id, source_ip, detail_json, prev_hash, signature, created_at)
			 VALUES (?, 1, ?, ?, ?, ?, ?, ?, ?, 'SUCCESS', ?, ?, ?, ?, ?, ?)`,
			auditRec.ID, auditRec.TenantID, auditRec.AppID, auditRec.ActorID, auditRec.ActorUsername,
			auditRec.Action, auditRec.ResourceType, auditRec.ResourceID, auditRec.TraceID, auditRec.ClientIP,
			marshalJSON(map[string]any{"summary": auditRec.Summary, "module": auditRec.Module, "httpStatus": auditRec.HTTPStatus}),
			auditRec.PrevHash, auditRec.HMACSignature, auditRec.OccurredAt,
		); err != nil {
			return err
		}

		return nil
	})
}

func (r *Repo) seedCMDB(ctx context.Context, tx *db.Tx, tid, aid, now string) error {
	groups := []struct {
		id, name string
		order    int
	}{
		{"grp-biz", "业务拓扑模型", 10},
		{"grp-res", "基础设施资源模型", 20},
	}
	for _, g := range groups {
		if _, err := tx.Exec(ctx,
			`INSERT INTO assets_model_groups (tenant_id, app_id, group_id, name, sort_order, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
			tid, aid, g.id, g.name, g.order, now,
		); err != nil {
			return err
		}
	}

	models := []struct {
		id, gid, code, name, icon, desc string
		fields                          []domain.ModelField
	}{
		{"m_biz", "grp-biz", "biz", "业务线", "briefcase", "医院一级业务域（业务树根）", []domain.ModelField{
			{FieldKey: "owner", Name: "业务负责人", FieldType: "string", Required: true, SortOrder: 1},
			{FieldKey: "tier", Name: "SLA 等级", FieldType: "enum", Required: true, EnumValues: []string{"T0", "T1", "T2"}, DefaultValue: "T0", SortOrder: 2},
		}},
		{"m_set", "grp-biz", "set", "集群 (Set)", "layers", "业务下属高可用集群", []domain.ModelField{
			{FieldKey: "env", Name: "环境", FieldType: "enum", Required: true, EnumValues: []string{"prod", "staging", "dev"}, DefaultValue: "prod", SortOrder: 1},
		}},
		{"m_module", "grp-biz", "module", "服务模块", "cpu", "具体微服务或数据库部署单元", []domain.ModelField{
			{FieldKey: "port", Name: "服务端口", FieldType: "number", Required: false, SortOrder: 1},
			{FieldKey: "version", Name: "当前版本", FieldType: "string", Required: false, SortOrder: 2},
		}},
		{"m_region", "grp-res", "region", "机房/区域", "globe", "物理机房或可用区（资源树根）", []domain.ModelField{
			{FieldKey: "datacenter", Name: "机房编号", FieldType: "string", Required: true, SortOrder: 1},
		}},
		{"m_security_area", "grp-res", "security_area", "安全域", "shield", "DMZ / 内网核心 / 数据库隔离区", []domain.ModelField{
			{FieldKey: "vlan", Name: "VLAN ID", FieldType: "string", Required: false, SortOrder: 1},
		}},
		{"m_host", "grp-res", "host", "主机节点", "server", "物理机或虚拟机计算资源", []domain.ModelField{
			{FieldKey: "ip", Name: "内网 IP", FieldType: "string", Required: true, IsUnique: true, SortOrder: 1},
			{FieldKey: "os", Name: "操作系统", FieldType: "string", Required: false, DefaultValue: "Rocky Linux 9", SortOrder: 2},
			{FieldKey: "cpu_cores", Name: "CPU 核数", FieldType: "number", Required: false, DefaultValue: "16", SortOrder: 3},
			{FieldKey: "mem_gb", Name: "内存 (GB)", FieldType: "number", Required: false, DefaultValue: "64", SortOrder: 4},
		}},
		{"m_process", "grp-res", "process", "运行进程", "activity", "主机上托管的常驻进程", []domain.ModelField{
			{FieldKey: "binary", Name: "进程名", FieldType: "string", Required: true, SortOrder: 1},
			{FieldKey: "listen", Name: "监听地址", FieldType: "string", Required: false, SortOrder: 2},
		}},
		{"m_db", "grp-res", "database", "数据库实例", "database", "PostgreSQL / Redis 等持久化存储实例", []domain.ModelField{
			{FieldKey: "engine", Name: "存储引擎", FieldType: "enum", Required: true, EnumValues: []string{"PostgreSQL", "Redis", "Kingbase"}, DefaultValue: "PostgreSQL", SortOrder: 1},
			{FieldKey: "endpoint", Name: "连接端点", FieldType: "string", Required: true, IsUnique: true, SortOrder: 2},
		}},
	}
	for _, m := range models {
		if _, err := tx.Exec(ctx,
			`INSERT INTO assets_models (tenant_id, app_id, model_id, group_id, code, name, icon, is_builtin, description, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, 1, ?, ?, ?)`,
			tid, aid, m.id, m.gid, m.code, m.name, m.icon, m.desc, now, now,
		); err != nil {
			return err
		}
		for _, f := range m.fields {
			if _, err := tx.Exec(ctx,
				`INSERT INTO assets_model_fields (tenant_id, app_id, model_id, field_key, name, field_type, required, is_unique, enum_values_json, default_value, sort_order) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				tid, aid, m.id, f.FieldKey, f.Name, f.FieldType, boolInt(f.Required), boolInt(f.IsUnique), marshalJSON(f.EnumValues), f.DefaultValue, f.SortOrder,
			); err != nil {
				return err
			}
		}
	}

	relTypes := []struct{ id, code, name, src, dst, card string }{
		{"rel-contains", "contains", "包含", "", "", "1:N"},
		{"rel-runs-on", "runs_on", "运行于", "m_module", "m_host", "N:N"},
		{"rel-depends-on", "depends_on", "依赖", "m_module", "m_db", "N:N"},
	}
	for _, rt := range relTypes {
		if _, err := tx.Exec(ctx,
			`INSERT INTO assets_relation_types (tenant_id, app_id, rel_type_id, code, name, source_model_id, target_model_id, cardinality, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			tid, aid, rt.id, rt.code, rt.name, rt.src, rt.dst, rt.card, now,
		); err != nil {
			return err
		}
	}

	if aid != "his-core" {
		return nil
	}

	// Seed rich starter dual-topology instances & edges for his-core
	instances := []struct {
		id, mid, name, ukey string
		attrs               map[string]any
		uniques             map[string]string
	}{
		{"inst-biz-his", "m_biz", "三甲 HIS 核心业务", "biz-his-core", map[string]any{"owner": "信息科核心组", "tier": "T0"}, nil},
		{"inst-set-outp", "m_set", "门诊核心集群", "set-outpatient", map[string]any{"env": "prod"}, nil},
		{"inst-set-inp", "m_set", "住院医嘱集群", "set-inpatient", map[string]any{"env": "prod"}, nil},
		{"inst-mod-reg", "m_module", "挂号结算服务", "mod-registration", map[string]any{"port": 8081, "version": "v4.2.0"}, nil},
		{"inst-mod-ord", "m_module", "电子医嘱引擎", "mod-order-engine", map[string]any{"port": 8082, "version": "v4.2.1"}, nil},
		{"inst-reg-dc1", "m_region", "主院区中心机房 DC-A", "dc-a", map[string]any{"datacenter": "HZ-DC-01"}, nil},
		{"inst-sec-core", "m_security_area", "核心业务内网区", "sec-core-net", map[string]any{"vlan": "VLAN-100"}, nil},
		{"inst-host-01", "m_host", "his-app-node-01", "10.20.1.11", map[string]any{"ip": "10.20.1.11", "os": "Rocky Linux 9.4", "cpu_cores": 32, "mem_gb": 128}, map[string]string{"ip": "10.20.1.11"}},
		{"inst-host-02", "m_host", "his-app-node-02", "10.20.1.12", map[string]any{"ip": "10.20.1.12", "os": "Rocky Linux 9.4", "cpu_cores": 32, "mem_gb": 128}, map[string]string{"ip": "10.20.1.12"}},
		{"inst-db-pg", "m_db", "his-pg16-primary", "pg-primary.clinical.svc:5432", map[string]any{"engine": "PostgreSQL", "endpoint": "pg-primary.clinical.svc:5432"}, map[string]string{"endpoint": "pg-primary.clinical.svc:5432"}},
	}
	for _, inst := range instances {
		if _, err := tx.Exec(ctx,
			`INSERT INTO assets_instances (tenant_id, app_id, instance_id, model_id, name, unique_key, status, attrs_json, version, created_by, updated_by, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, 'ACTIVE', ?, 1, 'usr-admin', 'usr-admin', ?, ?)`,
			tid, aid, inst.id, inst.mid, inst.name, inst.ukey, marshalJSON(inst.attrs), now, now,
		); err != nil {
			return err
		}
		for fk, fv := range inst.uniques {
			if _, err := tx.Exec(ctx,
				`INSERT INTO assets_field_uniques (tenant_id, app_id, model_id, field_key, field_value, instance_id) VALUES (?, ?, ?, ?, ?, ?)`,
				tid, aid, inst.mid, fk, fv, inst.id,
			); err != nil {
				return err
			}
		}
	}

	edges := []struct{ id, kind, rel, src, dst string }{
		// Business tree (strictly single-parent)
		{"edg-biz-1", "business", "rel-contains", "inst-biz-his", "inst-set-outp"},
		{"edg-biz-2", "business", "rel-contains", "inst-biz-his", "inst-set-inp"},
		{"edg-biz-3", "business", "rel-contains", "inst-set-outp", "inst-mod-reg"},
		{"edg-biz-4", "business", "rel-contains", "inst-set-inp", "inst-mod-ord"},
		// Resource tree (strictly single-parent)
		{"edg-res-1", "resource", "rel-contains", "inst-reg-dc1", "inst-sec-core"},
		{"edg-res-2", "resource", "rel-contains", "inst-sec-core", "inst-host-01"},
		{"edg-res-3", "resource", "rel-contains", "inst-sec-core", "inst-host-02"},
		{"edg-res-4", "resource", "rel-contains", "inst-sec-core", "inst-db-pg"},
		// Cross-domain custom links
		{"edg-cus-1", "custom", "rel-runs-on", "inst-mod-reg", "inst-host-01"},
		{"edg-cus-2", "custom", "rel-runs-on", "inst-mod-ord", "inst-host-02"},
		{"edg-cus-3", "custom", "rel-depends-on", "inst-mod-reg", "inst-db-pg"},
	}
	for _, e := range edges {
		if _, err := tx.Exec(ctx,
			`INSERT INTO assets_edges (tenant_id, app_id, edge_id, tree_kind, rel_type_id, source_id, target_id, metadata_json, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, '{}', ?)`,
			tid, aid, e.id, e.kind, e.rel, e.src, e.dst, now,
		); err != nil {
			return err
		}
	}
	return nil
}

// Helper for checking sql.ErrNoRows cleanly.
func mapNotFound(err error, msg string) error {
	if errors.Is(err, sql.ErrNoRows) {
		return domain.NotFoundError(msg)
	}
	return err
}
