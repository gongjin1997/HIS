# 09 插件设计：MySQL

> **本文定位**：kinstall MySQL 组件插件设计，覆盖 MySQL 8.4 LTS / 9.7 LTS（及可选 Percona Server）在主机模式（InnoDB Cluster / 异步半同步）与 Kubernetes 模式（Percona Operator for MySQL / Oracle MySQL Operator / MOCO）的完整部署与运维方案。通用约定遵循「06 中间件插件通用规范」。
>
> **版本**：v1.1（S10/V，2026-11-26；v1.1 将 9.7.2 BOM 默认源修正为官方 Oracle Generic tar.xz 并补齐 shell/router、Percona 移至可选项；将已移除的 `default_authentication_plugin` 替换为 `authentication_policy`）。版本号与生命周期信息截至 2026-11-26，引用前请复核。

## 1. 定位与结论摘要（F4、R-MW-03、ADR-048～050）

- **版本策略**：支持 **MySQL 8.4 LTS（默认生产稳定线）** 与 **MySQL 9.7 LTS（2026-04 发布，最新 LTS，Premier 支持至 2031-04-30）**；**禁止新建 MySQL 8.0（已于 2026-04 EOL）**；不向生产推荐短期创新版（26.x，如 26.10）。提供 **Percona Server for MySQL（8.4.11 / 9.7.2）** 作为同版本可选发行版（补齐社区版缺失的审计日志插件与线程池）。
- **高可用选型（ADR-048）**：
  - `host` 模式高可用默认采用 **MySQL InnoDB Cluster（Group Replication 单主模式 + MySQL Router + MySQL Shell）**；仅当业务表无法满足 MGR 约束（无主键/超大事务/非 InnoDB）时，允许显式切换 **半同步主从 + Keepalived VIP**；**否决 MHA（早已停维）**，**Orchestrator 不作为生产默认**（Percona 自身亦将其标为 tech preview）。
  - `kubernetes` 模式默认采用 **Percona Operator for MySQL（基于 Percona Server，1.3.0，Group Replication GA）**（06 §3），可选 Oracle MySQL Operator 或 Cybozu **MOCO v0.38.0**（无损半同步）。
- **备份选型（ADR-049）**：物理热备默认采用同主版本 **Percona XtraBackup**（`8.4.0-6` 对应 8.4 LTS；`9.7.2-2` 对应 9.7 LTS）+ binlog 连续归档实现 PITR；逻辑备份与跨大版本迁移采用 **MySQL Shell `util.dumpInstance` / `loadDump`**。
- **制品来源（ADR-050）**：优先采用 MySQL 官方 Generic Linux `tar.xz`（glibc 2.28，同时覆盖 `x86_64` 与 `aarch64`），跨 Rocky / Ubuntu / openEuler / 麒麟统一解包部署至 `/opt/kinstall/bin/mysql/<version>/`，规避各发行版自带 MariaDB/旧 MySQL 包冲突（ADR-026）。

## 2. 版本、许可与 BOM

### 2.1 版本生命周期（截至 2026-11-26）

| 系列 | 代表版本 | 发布日期 | 支持期限 | 定位 |
| --- | --- | --- | --- | --- |
| **MySQL 9.7 LTS** | `9.7.2`（Percona `9.7.2-3`，2026-11-12） | 2026-04-21 | Premier 至 2031-04-30 | 新一代 LTS，新项目可选 |
| **MySQL 8.4 LTS** | `8.4.11`（Percona `8.4.11-11`） | 2024-04-30 | Premier 至 2029-04；Extended 至 2032-04 | **默认生产推荐**（生态/驱动/工具链最稳） |
| MySQL 26.x 创新版 | `26.10`（2026-10-20） | 季度滚动 | 仅至下一创新版 | 不纳入生产 BOM |
| MySQL 8.0 | `8.0.46` | 2018-04 | **2026-04 已 EOL** | 禁止新建，仅提供升级向导 |

- **许可证与再分发（A5）**：MySQL Community Server、Percona Server、Percona XtraBackup 均为 **GPLv2**——离线包中再分发二进制须在 `NOTICE` 中附带 GPLv2 文本与对应源码获取说明；`mysqld_exporter` 为 Apache-2.0；MOCO / Percona Operator 为 Apache-2.0。
- **内存分配器与 64K 页（ADR-028）**：标准 MySQL 8.4 / 9.7 默认链接 glibc malloc，支持 arm64 4K 与 64K 页；**若启用 Percona Server 的 `LD_PRELOAD=libjemalloc.so`**，在 arm64 64K 页主机上必须校验 jemalloc 页大小构建（不兼容时退回 glibc malloc，绝不静默崩溃）。

### 2.2 BOM 示例

```yaml
product: mysql
revision: "2026.11.1"
versions:
  "8.4.11":
    license: GPL-2.0-only
    pageSizes: [4096, 65536]
    components:
      server:     { source: "mysql-8.4.11-linux-glibc2.28-{arch}.tar.xz" }
      shell:      { version: "8.4.11" }
      router:     { version: "8.4.11" }
      xtrabackup: { version: "8.4.0-6", license: GPL-2.0-only }
      exporter:   { name: mysqld_exporter, version: "0.21.0", license: Apache-2.0 }
  "9.7.2":
    license: GPL-2.0-only
    pageSizes: [4096, 65536]
    components:
      server:         { source: "mysql-9.7.2-linux-glibc2.28-{arch}.tar.xz" } # 默认官方 Oracle Generic（ADR-050）
      server_percona: { source: "percona-server-9.7.2-3-linux-{arch}.tar.xz", optional: true } # 等保审计可选
      shell:          { version: "9.7.2", license: GPL-2.0-only }
      router:         { version: "9.7.2", license: GPL-2.0-only }
      xtrabackup:     { version: "9.7.2-2", license: GPL-2.0-only }
      exporter:       { name: mysqld_exporter, version: "0.21.0", license: Apache-2.0 }
```

## 3. 拓扑与角色

| 拓扑 | 节点与角色 | 适用场景 | 核心约束 |
| --- | --- | --- | --- |
| `standalone` | 1 台 `mysql` | 开发测试、非关键系统 | 无 HA |
| `innodb-cluster` | 3（或 5）台 `mysql`（MGR 单主模式）+ 2 台 `router`（可混布或独立，前置 VIP/LB） | **生产默认**；自动故障切换、Paxos 多数派防脑裂、零人工干预 | 所有表必须为 InnoDB 且有显式主键（启用 `sql_require_primary_key=ON`）；跨 3 故障域 |
| `semi-sync-replica` | 1 主 + 1~2 从 + Keepalived VIP（手动/审批切换） | 无法满足 MGR 约束的存量迁移业务 | 需开启 GTID + 无损半同步（`rpl_semi_sync_source_wait_point=AFTER_SYNC`）；禁止自动无仲裁切主防脑裂 |

## 4. 部署目标：`host` vs `kubernetes`

- **`host` 模式（默认）**：tar.xz 部署于 `/opt/kinstall/bin/mysql/<version>/`，数据 `/data/mysql/<instance>/data`，binlog `/data/mysql/<instance>/binlog`，unit `kinstall-mysql-<instance>.service`、`kinstall-mysql-<instance>-router.service`、`kinstall-mysql-<instance>-exporter.service`。
- **`kubernetes` 模式**：
  - 默认 **Percona Operator for MySQL（PS）`1.3.0`**（Group Replication 模式 + HAProxy/MySQL Router + XtraBackup 物理备份与 PITR）；
  - 可选 **Oracle MySQL Operator**（使用上游 MySQL Community，但免费版仅支持 MySQL Shell 逻辑备份）或 **Cybozu MOCO `v0.38.0`**（无损半同步主从，规避 MGR 限制）；
  - **不推荐** Percona PXC `1.20.0`（Galera 8.4 支持尚处 tech preview 且写放大较重），除非明确需要多主写入。

## 5. 主机要求与容量声明

- **Profile**：`base` + `database-mysql`（`03 §6.1–§6.2`）。
- **`HostRequirements`**：
  - `sysctl`：`vm.swappiness = 1`、`vm.dirty_background_ratio = 5`、`vm.dirty_ratio = 15`、`net.core.somaxconn = 65535`；
  - 关闭 THP（`never`）；I/O 调度器 SSD/NVMe 设为 `none` / `mq-deadline`；
  - 逻辑包：`libaio`（或 EL9/Ubuntu24 对应 `libaio1t64` 经 `pkgmap.yaml` 映射）、`numactl`、`zstd`；
  - 端口放行：MySQL `3306`、X Protocol `33060`、MGR 组通信 `33061`、MySQL Router 写 `6446` / 读 `6447`、Exporter `9104`。
- **`HostRole.Claims`**：端口、数据盘、binlog 盘、内存占用 = `innodb_buffer_pool_size × 1.25`。
- **数据盘**：独立 SSD/NVMe，XFS `noatime`（**不要**对 MySQL 开启 `nobarrier`，保持崩溃一致性）。

## 6. 安装流程（`install` Pipeline）

| 顺序 | Phase | 目标角色 | 核心 Task | 并发 | 失败策略 | 幂等判据 |
| :---: | --- | --- | --- | :---: | --- | --- |
| 1 | `preflight` | 全部 | Profile `database-mysql`、冲突包检测（mariadb/mysql-server 排斥锁 ADR-026）、Claims、时钟同步 | 并行 10 | `failFast` | 只读 |
| 2 | `pki` | 本地 | 每实例独立 CA；签发 MySQL server、MGR 节点证书、Router 与客户端证书 | 本地 | `failFast` | 证书指纹 |
| 3 | `install-bin` | 全部 | `user.ensure(mysql)`、`dir.ensure`、`binary.install`（mysql/mysqlsh/mysqlrouter/xtrabackup）、`exec.allowlist` | 并行 10 | `failFast` | 版本软链 |
| 4 | `init-datadir` | `mysql` | `file.sync(my.cnf, pki/*)`、`cmd.run(mysqld --initialize-insecure)`（`creates: data/auto.cnf`）、启动 systemd、设置 `root` 随机强口令（stdin）与 MGR 复制账号 | 并行 10 | `failFast` | `auto.cnf` + `SELECT 1` |
| 5 | `bootstrap-cluster` | `mysql` | 在首节点通过 `mysqlsh` 执行 `dba.createCluster()`；其余节点逐台 `cluster.addInstance()`（自动 clone 同步） | `serial: true` | `failFast` | `cluster.status().status == "OK"` |
| 6 | `deploy-router-exp` | `router` / `mysql` | `mysqlrouter --bootstrap` 生成路由配置并启动 systemd；安装配置 `mysqld_exporter`（专用只读账号） | 并行 10 | `failFast` | 6446/6447 连通 + `/metrics` 200 |
| 7 | `verify` | 全部 | 创建探针库写入并在从节点读回、校验 TLS 强制生效、登记 `InstalledFile` | 本地 | `failFast` | 校验通过 |

## 7. 配置模板与参数调优

### 7.1 `my.cnf` 自动计算公式

设 `AvailRAM = HostMemMB - OSReserveMB`（与 `03 §6.2` 一致）：

| 参数 | 默认公式 / 值 | 说明 |
| --- | --- | --- |
| `innodb_buffer_pool_size` | `AvailRAM × 0.65`（独占宿主机），按 `128 MiB × instances` 对齐 | 留 35% 给连接线程、排序缓冲、OS 与备份进程 |
| `innodb_buffer_pool_instances` | `clamp(BufferPoolGB, 1, 16)` | 降低全局锁竞争 |
| `innodb_redo_log_capacity` | `clamp(BufferPoolGB × 0.25, 2 GiB, 16 GiB)` | MySQL 8.4+ 统一取代 `innodb_log_file_size` |
| `innodb_flush_method` | `O_DIRECT` | 避免双缓冲 |
| `innodb_io_capacity` / `_max` | NVMe `4000 / 8000`；SATA SSD `1000 / 2000`；HDD `200 / 400` | 按磁盘类型分档 |
| `innodb_flush_log_at_trx_commit` | `1`（默认金融级强一致）；`sync_binlog = 1` | “双 1”保证崩溃零丢事务 |
| `max_connections` | `clamp(AvailRAM_GB × 50, 300, 3000)` | 配合应用连接池控制 |
| `gtid_mode` / `enforce_gtid_consistency` | `ON` / `ON` | 全部拓扑强制开启 GTID |
| `sql_require_primary_key` | `ON`（`innodb-cluster` 强制） | 防止无主键表阻断 MGR |
| `binlog_expire_logs_seconds` | `604800`（7 天） | 配合全量 + binlog PITR |
| `authentication_policy` | `'caching_sha2_password,,'`（MySQL 8.4/9.x 已移除旧 `default_authentication_plugin` 变量，配置该旧变量会导致启动失败） | 强口令认证 |

### 7.2 关键 `my.cnf.tmpl` 片段

```ini
[mysqld]
server_id = {{ .Host.NumericID }}
datadir = /data/mysql/{{ .Instance.Name }}/data
log_bin = /data/mysql/{{ .Instance.Name }}/binlog/mysql-bin
gtid_mode = ON
enforce_gtid_consistency = ON
binlog_format = ROW
binlog_transaction_dependency_tracking = WRITESET
require_secure_transport = {{ ternary "ON" "OFF" .Params.TLS.Enabled }}
tls_version = TLSv1.2,TLSv1.3
ssl_ca = /etc/kinstall/mysql/{{ .Instance.Name }}/pki/ca.crt
ssl_cert = /etc/kinstall/mysql/{{ .Instance.Name }}/pki/server.crt
ssl_key = /etc/kinstall/mysql/{{ .Instance.Name }}/pki/server.key
loose_group_replication_ssl_mode = VERIFY_IDENTITY
loose_group_replication_consistency = BEFORE_ON_PRIMARY_FAILOVER
```

## 8. 安全设计

- **认证与密码策略**：强制 `caching_sha2_password` + 加载 `component_validate_password`（长度 ≥ 12、大小写数字特殊字符）；强制 `require_secure_transport=ON` 且 MGR 内部启用 `VERIFY_IDENTITY` mTLS。
- **审计合规**：MySQL 社区版不含企业审计插件，等保三级（`13 §3`）场景下推荐勾选 **Percona Server** 发行版启用内置开源 `audit_log_filter` 组件，并配置日志落盘转送集中审计系统（留存 ≥ 180 天）。
- **最小权限账号**：`root` 仅限本机 socket / 部署机 IP；业务账号禁止 `SUPER`/`SYSTEM_VARIABLES_ADMIN`/`FILE`；监控账号仅授 `PROCESS, REPLICATION CLIENT, SELECT ON performance_schema.*`。

## 9. 运维操作

- **备份与恢复（ADR-049）**：
  - 在**从节点**执行 `xtrabackup --backup --compress=zstd --stream=xbstream` 流式直传 S3 / NFS，避免占满本地盘，同时记录 `xtrabackup_binlog_info`（GTID 集合）；
  - 定时归档 binlog 配合 `mysqlbinlog` 实现任意秒级 PITR；
  - **版本强匹配**：8.4 必须用 XtraBackup 8.4，9.7 必须用 XtraBackup 9.7，由 BOM 绑定保证不混用。
- **升级路径**：
  - 同 LTS 小版本（如 `8.4.10 → 8.4.11`）：先升级全部从节点 → `cluster.setPrimaryInstance()` 优雅切主 → 升级原主节点 → 升级 MySQL Router；
  - 跨 LTS（`8.4 LTS → 9.7 LTS`）：强制先跑 `mysqlsh util.checkForServerUpgrade()` 零 Error 门禁 → 先备份 → 滚动升级从节点再切主（或构建 9.7 新集群经 MySQL Shell 并行 `dumpInstance/loadDump` + 异步复制追平后割接，零回退风险）；
  - 8.0 → 8.4：仅通过“迁移向导”支持，不支持 8.0 直跳 9.7。
- **动态改参**：`SET PERSIST` 在全部节点下发并回写 `my.cnf`；只读变量走滚动重启。

## 10. 监控、日志与告警规则

使用 `prometheus/mysqld_exporter v0.21.0`（2026-10-22），关键告警模板：

```yaml
groups:
  - name: kinstall.mysql
    rules:
      - alert: MySQLDown
        expr: mysql_up == 0
        for: 1m
        labels: { severity: critical }
      - alert: MySQLGroupReplicationMemberNotOnline
        expr: mysql_perf_schema_replication_group_member_info{member_state!="ONLINE"} == 1
        for: 1m
        labels: { severity: critical }
      - alert: MySQLReplicationLagHigh
        expr: mysql_slave_status_seconds_behind_master > 30
        for: 2m
        labels: { severity: warning }
      - alert: MySQLConnectionsHigh
        expr: mysql_global_status_threads_connected / mysql_global_variables_max_connections > 0.85
        for: 5m
        labels: { severity: warning }
      - alert: MySQLInnoDBBufferPoolHitRateLow
        expr: (1 - (rate(mysql_global_status_innodb_buffer_pool_reads[5m]) / clamp_min(rate(mysql_global_status_innodb_buffer_pool_read_requests[5m]), 1))) < 0.95
        for: 15m
        labels: { severity: warning }
      - alert: MySQLSlowQueriesSpike
        expr: rate(mysql_global_status_slow_queries[5m]) > 5
        for: 5m
        labels: { severity: warning }
```

## 11. 测试与验收

1. **MGR 故障注入**：强杀 Primary 节点 / 注入网络隔离，验证 ≤ 15 s 自动选主、Router 自动重连且新写无报错、原主恢复后自动重新入组；
2. **XtraBackup 物理恢复 + PITR**：`sysbench oltp_read_write` 写入数据 → 热备 → 误删表 → 物理恢复 + binlog 重放校验一致；
3. **升级回归**：`8.4.11` 滚动升级小版本及 `8.4 → 9.7` `checkForServerUpgrade` + 升级验证；
4. **双架构**：amd64 与 arm64（含 64K 页主机禁用 4K jemalloc 检查）均通过。

## 12. 风险与待办

- MySQL 9.7 LTS 刚发布半年余，部分老驱动（旧 JDBC 5.x）在认证/系统表细节上有兼容差异，默认仍保持 8.4 LTS，待 9.7 生态完全成熟后再评估切换默认；
- `tar.xz` 离线分发需在 `NOTICE` 中完整履行 GPLv2 源码指引义务（`12 §9`）。

## 13. 参考来源

- [MySQL 生命周期（endoflife.date/mysql）](https://endoflife.date/mysql)
- [Percona Server for MySQL 8.4.11 / 9.7.2 Release Notes](https://docs.percona.com/percona-server/9.7/upgrade.html)
- [Percona XtraBackup 8.4 / 9.7 Documentation](https://docs.percona.com/percona-xtrabackup/9.7/index.html)
- [Percona Operator for MySQL（PS）1.3.0 与 MySQL Operators 对比](https://www.osseva.io/blog/mysql-kubernetes-operator-comparison)
- [Cybozu MOCO v0.38.0](https://github.com/cybozu-go/moco)
- [prometheus/mysqld_exporter v0.21.0](https://github.com/prometheus/mysqld_exporter)

## 附 A. 本文涉及的决策与清单映射

- **新增 ADR**：
  - **ADR-048**：MySQL `host` 模式高可用默认 **InnoDB Cluster（Group Replication 单主 + MySQL Router + MySQL Shell）**，可选无损半同步主从，否决 MHA 与 Orchestrator 默认；`kubernetes` 模式默认 **Percona Operator for MySQL（PS）1.3.0**（可选 Oracle Operator / Cybozu MOCO v0.38.0）（09 §1、§3–§4）。
  - **ADR-049**：MySQL 物理热备默认同主版本 **Percona XtraBackup**（8.4 / 9.7 严格绑定）+ binlog 归档实现 PITR，逻辑迁移用 **MySQL Shell dump/load**（09 §1、§9）。
  - **ADR-050**：MySQL `host` 模式默认采用官方 Generic Linux `tar.xz`（glibc 2.28，amd64/arm64）装入 `/opt/kinstall/bin/mysql/<version>/`，等保审计场景可选 Percona Server 启用开源 `audit_log_filter`（09 §1、§8）。
- **关闭设计项**：F4。
- **关闭调研项**：R-MW-03。
