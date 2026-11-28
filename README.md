# LiveOps Foundation × KubeSphere Console (`rewrite/liveops-foundation`)

企业级多租户云原生运维治理底座，完全按照 **LiveOps Go 底座架构规格** 与 **KubeSphere KubeDesign 三级控制台交互体系** 从零重写（无插件臃肿包袱，聚焦工业级核心底座与 40 项核心事务不变量）。

## 架构概览

### 1. 严格六边形分层（由 `.golangci.yml` 强制门禁）
- `cmd/liveops-api` — 嵌入式 Vue 3 SPA 控制面 HTTP/SSE API（`/startupz`, `/livez`, `/readyz`, `/healthy`）
- `cmd/liveops-worker` — 独立解耦后台 Worker（Outbox Relay、DAG 租约调度、租约超时接管、节点健康看门狗，独立探针 `:18085`）
- `cmd/liveops-node` — 零数据库外联边缘节点 Agent（一次性 15m 注册令牌、15s 心跳、双轨凭据轮换、分片断点确认）
- `cmd/liveopsctl` — 治理与加密 CLI（仅从 `stdin` 读取明文 `secret seal/open` 支持 AES-256-GCM & SM4-GCM、`db migrate`、`audit verify`）
- `pkg/platform/` — 零业务反向依赖公共底座 SDK（ULID+时钟、v2 KMS 信封+国密 SM4+ServerSign v2、Default-Deny RBAC+渐进锁定、SQLite WAL/PostgreSQL 16 `FORCE RLS` 双引擎、严格 SSE 游标、防 SSRF/DNS-Rebinding、防 ZipSlip、防篡改 HMAC 审计链、Kahn DAG 编译器、K8s 探针）
- `internal/domain/` — **严格零 I/O** 纯领域模型与收敛状态机
- `internal/adapter/store/` — 复合 `(tenant_id, app_id)` 多租户事务仓储与并发不变量实现
- `internal/usecase/` — 零 HTTP 传输依赖业务用例服务（含原生 Excelize `.xlsx` 导入导出）
- `internal/worker/` — 可插拔执行器与后台守护循环
- `internal/transport/http/` — LiveOps v1 REST & Strict `Last-Event-ID` SSE 传输层

### 2. 40 项 LiveOps 核心不变量全自动化证明（`test/invariants_test.go`）
- **D01–D19**: 部署作业 `Idempotency-Key` 幂等重放/409 冲突、原子物化、`lease_epoch`/`attempt_no` 并发互斥与超时 `RECONCILING` 接管围栏、重复/冲突回执隔离、Diamond DAG 汇聚唯一解锁、取消优先级、失败重试与下游 `SKIPPED` 剪枝、Repeatable-Read 快照一致性
- **S01–S07**: 复合 `(tenant_id, app_id)` 物理隔离、跨租户拓扑边拦截、业务/资源单父无环双树、CMDB `If-Match` 乐观锁与唯一字段索引、配置 `If-Match` 428/412 原子更新+不可变快照+KMS 密文强审计揭示、环境合并 `viewcheck` 冲突预检
- **G01–G05**: 网关单默认路由范围锁原子切换、`route_version` 单调递增、制品 `VERIFIED` 强晋升门禁、外键引用防误删
- **U01–U09**: 严格 SSE `Last-Event-ID` 游标校验（缺失 `409 SNAPSHOT_REQUIRED`、非法/负数/溢出 `400 INVALID_CURSOR`、窗口外 `410 CURSOR_RESET_REQUIRED`、无 ID 心跳帧）

### 3. KubeSphere KubeDesign 三级治理控制台 (`web/`)
- **平台管理 (Platform)**: 平台运行总览、企业空间/租户、账号角色与 Default-Deny 策略、零信任节点池、动态网关与制品门禁、防篡改审计链
- **企业空间 (Workspace)**: 空间概览、应用中心与 256-bit `lo_live_` 双轨 AppKey、项目治理与 CAS 角色授权
- **项目工作台 (Project)**: 项目概览、CMDB 资产台账 & `@xyflow/vue` 双视角拓扑 & `.xlsx`、KMS 配置中心与环境 ViewCheck、DAG 部署编排与 SSE 实时事件流

## 快速启动

```bash
# 全量 CI（前端 WCAG 对比度 + Stylelint + 单测 + 构建 + Go Lint + 40 项不变量 -race 测试 + 四二进制编译）
make all

# 启动控制面 API（默认监听 0.0.0.0:8080，默认账号 admin / LiveOps#2026!）
./bin/liveops-api --listen 0.0.0.0:8080

# 启动独立解耦 Worker（共享 SQLite WAL 或 PostgreSQL 16）
./bin/liveops-worker --probe-listen 127.0.0.1:18085
```
