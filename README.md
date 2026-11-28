# Kinstall v2 — 统一动态元模型 CMDB 资产平台与声明式交付运维引擎

Kinstall v2 从系统底座重新架构，以**全能动态元模型 CMDB 为单一真实来源（SSOT）**，向上无缝打通**多环境配置与国密 KMS 密钥中心**、**声明式 YAML 插件库与 DAG/分批灰度发布流水线**、**零信任单向出站 Agent + SSH 双模执行面**以及 **CAS 内容寻址制品仓与纯 Go 原生 OCI 镜像引擎**。

全系统采用 `CGO_ENABLED=0` 模块化单体单二进制交付，默认内嵌 SQLite WAL 开箱即用，同时完整兼容 PostgreSQL (`pgx/v5`)。

## 核心架构

1. **Layer 1 — 系统公共底座 (`pkg/platform/`)**
   - `id`: 带业务前缀的单调递增 ULID 生成器
   - `crypto`: `v2:{ver}:{alg}:{nonce}:{ct}` KMS 信封加密（AES-256-GCM + 国密 SM4-GCM `emmansun/gmsm`）、Argon2id 密码哈希、Ed25519 数字签名与验签
   - `db`: SQLite WAL 物理读写分离连接池（1 Writer / 4 Reader）+ PostgreSQL `pgx/v5` 双栈自动占位符重写 + Goose 嵌入式前向迁移
   - `auth`: 无状态 HMAC-SHA256 会话令牌 + Personal API Token (`ki_pat_...`) + Default-Deny RBAC 中间件
   - `httpx`: 统一响应与错误信封、严格 JSON 解码、严格无外链 CSP 安全头、SSE 实时事件流推送
   - `netguard`: 防 SSRF / DNS Rebinding HTTP Client 与阻断云元数据 `169.254.0.0/16` 的 CIDR 护栏
   - `safefs`: 防路径穿越 `SafeJoin`、防 ZipSlip / ZipBomb 的 `SafeExtractTar`、fsync 原子落盘
   - `audit`: 自动敏感键脱敏 `slog.Handler` + 防篡改 SHA-256 哈希链审计校验器
   - `eventbus`: 内存实时日志环缓冲与 SSE 扇出广播中心 (`LogBroker`)
   - `dag`: 确定性 Kahn 拓扑排序、波次分层与精准环路检测
   - `safety`: 高危 Shell 命令拦截护栏

2. **Layer 2 — 统一 SSOT 数据库模型 (`migrations/sqlite/` & `migrations/postgres/`)**
   - **Host = `m_host` CI + 1:1 `cmdb_host_endpoints`**：主机录入/导入/Agent 自动纳管一次后，即全局打通 CMDB、双拓扑、42U 机柜、IPAM、WebSSH、白名单采集、动态分组批量作业与 DAG 真实发布。

3. **Layer 3 — 核心领域引擎 (`internal/`)**
   - `internal/iam`: 工作空间、用户 RBAC、PAT、KMS 凭据保险箱、SHA-256 审计哈希链
   - `internal/cmdb`: 动态元模型与属性 Schema 校验、多跳血缘影响面、蓝鲸双拓扑树与服务模板、动态分组、原生 Excelize (`.xlsx`) 导入导出、DCIM 42U 机柜立面防重叠、IPAM CIDR 与自动 IP 分配
   - `internal/config`: `dev/sit/uat/beta/prod` 五环境隔离、KMS 脱敏与强制审计显密、Draft Diff、不可变快照与单调回滚、跨环境安全克隆（自动置空密钥）、`Spec > Service > Global` 三级合并与 K8s 毫核/MiB 计算器
   - `internal/catalog`: 本地 CAS 内容寻址去重仓、Ed25519 Fail-Closed 验签、BOM 导出、纯 Go `google/go-containerregistry` 零 docker CLI 镜像推送与架构防混推 (`ARCH_MISMATCH`)
   - `internal/execplane`: Agentless SSH / SFTP 原子落盘 / WebSocket `xterm-256color` PTY + 零信任单向出站纯 Go Agent（`Setpgid` 进程组隔离）+ 白名单采集 + CIDR TCP 批量探测 + 动态分组并发批量作业
   - `internal/delivery`: 10 套预置声明式 YAML 插件包 + 自定义 YAML 插件 CRUD + DAG / BATCH / M1 / M2 / M3 真实发布执行 + `PAUSED -> RESUME` 灰度暂停断点 + SSRF 安全 HTTP 拨测 + 自动回写 CMDB `runs_on` 血缘 + 一键回滚

## 快速启动

```bash
# 1. 构建前端并编译静态单二进制
make web-build && make build

# 2. 启动控制面服务 (默认 SQLite WAL: ~/.kinstall/kinstall.db)
./bin/kinstall server start --listen 0.0.0.0:8080
# 默认管理员账号: admin / Kinstall#2026!

# 3. (可选) 在边缘主机启动零信任单向出站纯 Go Agent
./bin/kinstall agent --server http://<SERVER_IP>:8080 --token <ENROLL_TOKEN> --zone prod-core
```
