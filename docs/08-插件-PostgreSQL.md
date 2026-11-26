# 08 插件设计：PostgreSQL

> **本文定位**：kinstall PostgreSQL 组件插件设计，覆盖主机模式高可用（Patroni + etcd + PgBouncer + HAProxy/VIP + pgBackRest）与 Kubernetes 模式（CloudNativePG），并严格联动 `03 §6` 的 `database-pg` Profile 与动态大页配置。通用约定遵循「06 中间件插件通用规范」。
>
> **版本**：v1.1（S9/V，2026-11-26；v1.1 统一国产 OS 构建口径为 `D3` EL8 `glibc 2.28` 容器与 `syncProfile: async|sync|sync-strict` 参数模板）。版本号与生命周期信息截至 2026-11-26，引用前请复核。

## 1. 定位与结论摘要（F5、R-MW-04、ADR-045～047）

- **版本策略**：支持 **PostgreSQL 18.7（默认生产稳定线）** 与 **PostgreSQL 19.0（2026-11-19 GA，最新线）**，兼容 17.12 / 16.16；**拒绝新建 PostgreSQL 14（已于 2026-11-12 EOL）**。
- **高可用与备份选型（ADR-045 / ADR-046）**：
  - `host` 模式 HA 默认采用 **Patroni 4.1.5 + 独立 3 节点 etcd v3.7.4 DCS + HAProxy/VIP（或 vip-manager）+ 可选 PgBouncer 1.24.x**；
  - `kubernetes` 模式采用 CNCF 孵化项目 **CloudNativePG（CNPG）1.30.1 + Barman Cloud Plugin 0.15.1**（06 §3）；
  - `host` 模式备份默认采用 **pgBackRest ≥ 2.60.0**（强制 ≥ 2.59.3 以规避加密子密钥漏洞）；
- **软件包策略（ADR-047）**：优先采用 **PGDG 官方源包**（RHEL/Rocky/Alma/OL、Debian/Ubuntu 均覆盖 amd64/arm64）；对无 PGDG 官方源的国产 OS（openEuler、麒麟、Anolis、UOS），由 kinstall 离线流水线在 `D3 §3` 统一 EL8 / `glibc 2.28` 构建容器内从官方源码编译打成统一 tarball 纳入 BOM（与 `P4` / ADR-047 对齐），不使用发行版自带陈旧 PG 包（延续 ADR-026 精神）。
- **范围边界**：不包含 openGauss / PolarDB 等衍生闭源或协议分叉分支。

## 2. 版本、许可与 BOM

### 2.1 支持矩阵（截至 2026-11-26）

| 主版本 | 最新小版本 | 发布日期 | 社区 EOL | 状态 |
| --- | --- | --- | --- | --- |
| **PostgreSQL 19** | `19.0` | 2026-11-19 | 2031-11 | 最新 GA（新特性线，建议完成 19.1 补丁后用于核心生产） |
| **PostgreSQL 18** | `18.7` | 2026-11-12 | 2030-11 | **默认推荐**（AIO `io_method`、`pg_upgrade` 保留统计信息） |
| **PostgreSQL 17** | `17.12` | 2026-11-12 | 2029-11 | 维护支持 |
| **PostgreSQL 16** | `16.16` | 2026-11-12 | 2028-11 | 兼容维护 |
| PostgreSQL 14 | `14.25` | 2026-11-12 | **2026-11-12（已 EOL）** | 禁止新建，仅提供升级引导 |

- **许可证**：PostgreSQL License（类 MIT/BSD，宽松无 Copyleft 义务）；Patroni（MIT）；pgBackRest（MIT）；PgBouncer（ISC）；HAProxy（GPLv2，仅原样系统包或附源码指针）；`postgres_exporter`（Apache-2.0）。
- **页大小（ADR-028）**：PostgreSQL 使用标准 glibc 内存分配器，同时支持 amd64 4K 与 arm64 4K/64K 页；需注意 `huge_pages` 静态大页在 arm64 64K 页系统下大页尺寸为 512 MiB（区别于 4K 页系统的 2 MiB），模板必须读取 `Facts.HugePageSizeKB` 动态计算 `vm.nr_hugepages`（`03 §6.2`）。

### 2.2 BOM 示例

```yaml
product: postgresql
revision: "2026.11.1"
components:
  postgresql: { version: "18.7", license: PostgreSQL, pageSizes: [4096, 65536] }
  patroni:    { version: "4.1.5", license: MIT }
  etcd:       { version: "3.7.4", license: Apache-2.0 }   # 与 04 BOM 统一
  pgbouncer:  { version: "1.24.1", license: ISC }
  pgbackrest: { version: "2.60.0", license: MIT }          # 强制 >= 2.59.3
  exporter:   { name: postgres_exporter, version: "0.21.1", license: Apache-2.0 }
```

## 3. 拓扑与高可用方案对比

### 3.1 高可用方案选型（ADR-045）

| 方案 | 一致性 / 防脑裂 | 运维成熟度 | 离线友好 | 结论 |
| --- | --- | --- | --- | --- |
| **Patroni + etcd DCS** | 强（etcd Raft 租约 + Watchdog 可选） | 行业事实标准 | 高（复用 K8s 插件同款 etcd v3.7.4） | **host 模式默认** |
| repmgr | 弱（缺少强分布式锁，易脑裂） | 中 | 高 | 否决（不用于生产 HA） |
| pg_auto_failover | 依赖独立 monitor 节点（单点风险） | 中（社区放缓） | 中 | 否决 |
| **CloudNativePG（CNPG）** | K8s API Lease 原生强一致 | CNCF Incubating，极高 | 中（镜像离线） | **kubernetes 模式默认** |

### 3.2 支持拓扑与规格建议

| 拓扑 | 节点组成 | 适用场景 | 最低建议规格 |
| --- | --- | --- | --- |
| `standalone` | 1 台 `pg` | 开发测试、非关键内部工具 | 2C / 4 GiB / 50 GiB SSD |
| `ha-patroni` | 3 台 `pg`（1 主 2 从，含 Patroni + 可选 PgBouncer）+ 3 台 `etcd`（中小规模可混布于同 3 节点，大规模独立）+ 入口（HAProxy+VIP 或 `vip-manager`） | **生产默认**；支持同步复制（`synchronous_mode: true`）实现 RPO=0 | 每节点 8C / 32 GiB / 独立数据盘 + 独立 WAL NVMe |
| `ha-patroni-readonly` | 3 主备 + N 台异步只读扩展节点 + 独立 HAProxy 读写分离端口（5000 写 / 5001 读） | 读多写少报表/分析型业务 | 按查询并发扩展 |

## 4. 部署目标：`host` vs `kubernetes`

- **`host` 模式**：
  - 数据盘挂载于 `/data/postgresql/<instance>/pgdata`，WAL 目录可独立挂载 `/data/postgresql/<instance>/wal`；
  - systemd unit：`kinstall-postgresql-<instance>-patroni.service`（由 Patroni 托管 `postgres` 生命周期，不单独启用系统 `postgresql.service`）、`kinstall-postgresql-<instance>-pgbouncer.service`、`kinstall-postgresql-<instance>-exporter.service`；
  - 连接入口：HAProxy 检查 Patroni REST API（`:8008/primary` 返回 200 路由写端口 `5000`，`:8008/replica?lag=10MB` 路由读端口 `5001`），前端配 Keepalived VIP 或外部硬件 LB。
- **`kubernetes` 模式**：
  - 通过 Helm v4 SDK 部署 **CloudNativePG `1.30.1`** 与 **Barman Cloud Plugin `0.15.1`**（注意 CNPG 1.26 已弃用内置 `.spec.backup.barmanObjectStore` 并将于 1.32 移除，kinstall 仅生成基于 `ObjectStore` CRD 的新架构）；
  - 镜像固定使用 CNPG 官方 `standard` / `minimal` 镜像（不使用已弃用的 `system` 镜像），CNPG 1.30 支持 PG 15–18（PG 19 待 CNPG 1.31 2026-12 发布后纳管）。

## 5. 主机要求与容量声明

- **Profile**：`base` + `database-pg`（`03 §6.1–§6.2`）。
- **`HostRequirements`**：
  - `sysctl`：`vm.swappiness = 1`、`vm.dirty_background_bytes = 67108864`、`vm.dirty_bytes = 536870912`、`vm.overcommit_memory = 2` + `vm.overcommit_ratio = 80`（防 OOM Killer 杀 postmaster）、动态计算 `vm.nr_hugepages`（`03 §6.2`）；
  - 关闭 THP（`never`），I/O 调度器 NVMe 设 `none`、SATA SSD 设 `mq-deadline`（`03 §6.10`）；
  - `limits`：`nofile >= 65536`、`nproc >= 65536`、`memlock unlimited`；
  - 端口放行：PG `5432`、Patroni REST `8008`（带 mTLS）、PgBouncer `6432`、HAProxy `5000/5001`、etcd `2379/2380`、Exporter `9187`。
- **`HostRole.Claims`**：登记上述端口、数据/WAL 目录、独占数据盘、内存占用 = `shared_buffers + work_mem 预算 + 系统缓冲`（计入 CMDB 容量视图，`03 §8.5`）。

## 6. 安装流程（`install` Pipeline）

| 顺序 | Phase | 目标角色 | 核心 Task | 并发 | 失败策略 | 幂等判据 |
| :---: | --- | --- | --- | :---: | --- | --- |
| 1 | `preflight` | 全部 | Profile `database-pg` 校验、磁盘 XFS `noatime` 检查、时钟偏移 < 50 ms、Claims 端口检查 | 并行 10 | `failFast` | 只读 |
| 2 | `pki` | 本地 | `Issuer.EnsureCA`（PG 实例 CA + etcd CA）；签发 PG server、Patroni REST mTLS、客户端复制与 etcd 证书 | 本地 | `failFast` | 证书与 SAN 一致 |
| 3 | `deploy-etcd` | `etcd` | 复用 etcd v3.7.4 安装编排（TLS + Raft 3 节点） | `serial: true` | `failFast` | `endpoint health` |
| 4 | `install-pg-stack` | `pg` | `user.ensure(postgres)`、`pkg.ensure`/自编包安装 PG、Patroni、pgBackRest、PgBouncer；创建数据/WAL 目录 | 并行 10 | `failFast` | 版本与路径校验 |
| 5 | `bootstrap-patroni` | `pg` | `file.sync`（`patroni.yml`、`pgbackrest.conf`、PKI）；先启动首节点引导集群初始化（`initdb --data-checksums`），再启动其余从节点自动 `pg_basebackup` | 首节点串行，其余并行 | `failFast` | `patronictl list` 1 Leader + N Streaming |
| 6 | `init-users-backup` | Leader | 创建业务/只读/监控账号（`scram-sha-256`）；`pgbackrest stanza-create` + 首次全量备份校验 | `serial: true` | `failFast` | `pgbackrest check` |
| 7 | `deploy-proxy-exp` | `pg` / `lb` | 配置 PgBouncer、HAProxy + Keepalived VIP、`postgres_exporter` | 并行 10 | `failFast` | 写端口读写探针 + `/metrics` 200 |

## 7. 配置模板与参数调优

### 7.1 `params` 关键字段与自动计算公式

```yaml
version: "18.7"
topology: ha-patroni
syncProfile: sync                # async（纯异步低延迟）| sync（默认：1 同步备 + 1 异步备，无备时降级保写）| sync-strict（金融核心严格 RPO=0，至少 3 节点）
dataChecksums: true              # 强制 initdb 开启页校验和（与 pgBackRest 2.60 联动）
pgbouncer:
  enabled: true
  poolMode: transaction
  maxClientConn: 2000
  defaultPoolSize: 50
backup:
  tool: pgbackrest
  repoType: s3                   # local | nfs | s3
  retentionFull: 4
  cipherType: aes-256-cbc        # 启用仓库加密，口令来自 SecretStore
```

**按宿主机硬件自动计算的 `postgresql.conf` 基线公式**（与 `03 §6.2` `OSReserveMB` 联动，设 `AvailRAM = HostMemMB - OSReserveMB`）：

| 参数 | 默认公式 | 说明 |
| --- | --- | --- |
| `shared_buffers` | `min(AvailRAM × 0.25, 64 GiB)` | 避免过大导致 Checkpoint 刷盘风暴 |
| `effective_cache_size` | `AvailRAM × 0.75` | 优化器估算 OS 页缓存 + 共享缓冲 |
| `work_mem` | `clamp((AvailRAM × 0.25) / max_connections, 4 MiB, 64 MiB)` | 开启 PgBouncer 时按真实后端连接数算 |
| `maintenance_work_mem` | `min(AvailRAM × 0.05, 2 GiB)` | 加速 `VACUUM` / 建索引 |
| `huge_pages` | `try`（已算好 `nr_hugepages` 时为 `on`） | 联动 `03 §6.2` 预留 |
| `max_wal_size` / `min_wal_size` | `clamp(DataDiskGB × 0.05, 4 GiB, 32 GiB)` / `1/4` | 配合 `checkpoint_completion_target = 0.9` |
| `io_method`（PG 18+） | `worker`（内核 ≥ 6.6 可选 `io_uring`） | PG 18 原生异步 I/O 加速顺序扫描与预读 |
| `random_page_cost` | `1.1`（SSD/NVMe）/ `4.0`（HDD） | 按 Facts 磁盘 `Rotational` 自动判定 |
| `max_connections` | 无 PgBouncer `min(500, AvailRAM_GB × 15)`；有 PgBouncer 后端 `200` | 防止连接数爆炸耗尽内存 |

### 7.2 Patroni 关键配置片段（`patroni.yml.tmpl`）

```yaml
scope: {{ .Instance.Name }}
namespace: /kinstall/pg/
name: {{ .Host.Name }}
restapi:
  listen: "{{ .Host.PrivateIP }}:8008"
  connect_address: "{{ .Host.PrivateIP }}:8008"
  certfile: "/etc/kinstall/postgresql/{{ .Instance.Name }}/pki/patroni.crt"
  keyfile: "/etc/kinstall/postgresql/{{ .Instance.Name }}/pki/patroni.key"
  cafile: "/etc/kinstall/postgresql/{{ .Instance.Name }}/pki/ca.crt"
  verify_client: required
bootstrap:
  dcs:
    ttl: 30
    loop_wait: 10
    retry_timeout: 10
    synchronous_mode: {{ ne .Params.SyncProfile "async" }}
    synchronous_mode_strict: {{ eq .Params.SyncProfile "sync-strict" }}
    postgresql:
      use_pg_rewind: true
      use_slots: true
      parameters:
        wal_level: replica
        archive_mode: "on"
        archive_command: "pgbackrest --stanza={{ .Instance.Name }} archive-push %p"
        password_encryption: scram-sha-256
        ssl: "on"
        ssl_min_protocol_version: TLSv1.3
```

## 8. 安全设计

- **认证与加密**：强制 `scram-sha-256`（禁用 `md5` / `trust`）；`pg_hba.conf` 仅对允许网段开放 `hostssl`，本地管理走 `local ... peer`；强制 `ssl_min_protocol_version = TLSv1.3`（或兼容 TLSv1.2）；Patroni REST API 启用双向 mTLS（防止未授权调用 `/switchover` 或 `/reinitialize`）。
- **权限分离**：`postgres` 超级用户仅供 Patroni / 运维维护；`replicator` 仅授 `REPLICATION`；`monitor` 仅授 `pg_monitor` 角色；业务账号仅授业务库权限，回收 `public` schema 创建权（PG 15+ 默认行为）。
- **审计**：可选启用 `pgaudit` 扩展记录 DDL/ROLE/登录事件，配合等保三级（`13 §3`）。

## 9. 运维操作

- **备份与 PITR 恢复（ADR-046）**：选用 **pgBackRest ≥ 2.60.0**（2026-05 由 AWS / Supabase / Percona / pgEdge 等多厂商联合资助续维；**强制 ≥ 2.59.3** 修复 2026-10 披露的加密子密钥弱随机缺陷；2.60.0 新增恢复期页校验和强拦截，坏页立即失败避免静默数据损坏）。支持全量 + 差异 + 增量 + 连续 WAL 归档，PITR 恢复自动新建临时集群校验后再按审批切流。
- **小版本升级**（如 `18.6 → 18.7`）：滚动升级全部从节点 → `patronictl switchover` 优雅切主 → 升级原主节点（秒级中断）。
- **大版本升级**（如 `17 → 18` 或 `18 → 19`）：
  1. 停写窗口方案：先备份 → `pg_upgrade --link --check`（PG 18+ 自动保留优化器统计信息，免去升级后长时间 `ANALYZE` 抖动）→ 升级主节点 → 同步升级/重建从节点；
  2. 近零停机方案：基于原生逻辑复制（`CREATE PUBLICATION / SUBSCRIPTION`）构建新版本双活集群并割接。
- **动态改参（`reconfigure`）**：统一经 `patronictl edit-config` 写入 DCS，所有节点自动同步；`pending_restart=true` 的参数按先从后主在维护窗口滚动重启。

## 10. 监控、日志与告警规则

使用 `prometheus-community/postgres_exporter v0.21.1`（仅授 `pg_monitor` 角色），关键告警模板：

```yaml
groups:
  - name: kinstall.postgresql
    rules:
      - alert: PostgreSQLDown
        expr: pg_up == 0
        for: 1m
        labels: { severity: critical }
      - alert: PostgreSQLReplicationLagHigh
        expr: pg_replication_lag > 30
        for: 2m
        labels: { severity: warning }
      - alert: PostgreSQLTooManyConnections
        expr: sum by (kinstall_instance) (pg_stat_activity_count) / pg_settings_max_connections > 0.85
        for: 5m
        labels: { severity: warning }
      - alert: PostgreSQLDeadlocks
        expr: increase(pg_stat_database_deadlocks[5m]) > 0
        for: 1m
        labels: { severity: warning }
      - alert: PostgreSQLXidWraparoundRisk
        expr: pg_database_age_datfrozenxid > 1500000000
        for: 15m
        labels: { severity: critical }
      - alert: PostgreSQLArchiveFailed
        expr: increase(pg_stat_archiver_failed_count[10m]) > 0
        for: 5m
        labels: { severity: critical }
```

## 11. 测试与验收

1. **三节点 HA 演练**：强杀 Leader 进程 / 断主节点网口，验证 ≤ 30 s 完成新主选举、VIP/HAProxy 切换成功、`synchronous_mode=true` 下零数据丢失，原主恢复后通过 `pg_rewind` 自动回流；
2. **PITR 演练**：写入带时间戳标记数据 → 故意误删表 → 使用 pgBackRest 恢复至误删前 1 秒并校验数据完整；
3. **压测基线**：`pgbench -i -s 100` 初始化后跑 10 分钟读写，记录 TPS 与 P99 延迟留存验收报告；
4. **升级回归**：`17.12 → 18.7` 验证 `pg_upgrade` 与统计信息保留。

## 12. 风险与待办

- 国产 OS（openEuler / 麒麟 / UOS）无 PGDG 官方源，M4/M5 需在 CI 中建立源码打包流水线（与 `12 §4` OS 包闭包流水线集成）；
- CloudNativePG 对 PostgreSQL 19 的正式支持需等待 2026-12 发布的 CNPG 1.31.0。

## 13. 参考来源

- [PostgreSQL 19 发布公告（2026-11-19）与版本策略](https://www.postgresql.org/support/versioning/)
- [PostgreSQL 18.7 / 17.12 / 14.25 发布说明](https://www.postgresql.org/about/news/postgresql-187-1712-1616-1520-1425-and-19-rc-2-released-3442/)
- [pgBackRest 2.60.0 Release Notes & 2.59.3 安全公告](https://pgbackrest.org/release.html)
- [CloudNativePG（CNCF Incubating）](https://github.com/cloudnative-pg/cloudnative-pg)
- [Patroni 4.1.5 文档](https://github.com/patroni/patroni)
- [prometheus-community/postgres_exporter v0.21.1](https://github.com/prometheus-community/postgres_exporter/releases)

## 附 A. 本文涉及的决策与清单映射

- **新增 ADR**：
  - **ADR-045**：PostgreSQL `host` 模式高可用默认采用 **Patroni 4.1.5 + etcd v3.7.4 DCS + HAProxy/VIP**（可选 PgBouncer），否决 repmgr 与 pg_auto_failover；`kubernetes` 模式默认 **CloudNativePG 1.30.1 + Barman Cloud Plugin 0.15.1**（08 §3–§4）。
  - **ADR-046**：PostgreSQL `host` 模式备份默认采用 **pgBackRest ≥ 2.60.0**（强制 ≥ 2.59.3 修复加密子密钥缺陷），并默认开启 `initdb --data-checksums` 配合 2.60 页校验和拦截（08 §9）。
  - **ADR-047**：PostgreSQL 制品在有 PGDG 官方源的 OS 使用上游 PGDG 包，在无 PGDG 源的国产 OS 使用平台统一编译的同版本包，不使用发行版陈旧 PG 包（08 §1）。
- **关闭设计项**：F5。
- **关闭调研项**：R-MW-04。
