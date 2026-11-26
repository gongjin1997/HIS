# 07 插件设计：Valkey / Redis

> **本文定位**：kinstall 首个中间件插件，用于验证插件框架（`02 §3`、`06`）对有状态缓存/内存数据库的支持能力。默认产品为 **Valkey**（BSD-3-Clause，Linux Foundation 托管），**Redis 8** 作为可选（ADR-013 定稿）。
>
> **版本**：v1.1（S8/V，2026-11-26；v1.1 修正 §1 交付形态为内置强类型 Go 插件以对齐 ADR-069）。版本号与生命周期信息截至 2026-11-26，引用前请复核。

## 1. 定位与结论摘要（F6、R-MW-05、ADR-013）

- **默认选型（ADR-013 定稿）**：**默认 Valkey 9.1 LTS/稳定线或 9.2.0**（推荐生产基线 **Valkey 9.1.4** 或 **8.1.12**，最新特性线 **9.2.0** 于 2026-11-19 发布），**Redis 8.x** 仅作显式可选。
- **理由**：①Valkey 由 Linux Foundation 托管、AWS/Google/Oracle/Ericsson 等联合维护，BSD-3-Clause 许可无商业再分发与合规障碍；②Valkey 9.x 在多线程 I/O、内存效率（Path Hash、哈希槽每库等）与协议兼容性上持续领先，且官方自 9.x 起已在 `valkey.io` 提供预编译多架构二进制制品；③Redis 8 起虽增加 AGPLv3 与 RSALv2/SSPLv1 三选一，但仍带来企业法务审查与再分发源码义务；且 Redis 8 新增数据结构使 RDB 与 Valkey 不再双向兼容，新集群统一优先 Valkey。
- **交付形态**：M4 以**内置强类型 Go 插件（ADR-069）**交付，作为首个验证插件 SDK 的中间件（声明式插件包 `06 §6` 仅用于轻量组件）；`host-binary` / `host-container` 优先交付，`kubernetes` 模式按 `06 §3` 接入 `valkey-operator`。

## 2. 版本、许可与 BOM（A5、ADR-013、ADR-028）

### 2.1 版本矩阵（截至 2026-11-26）

| 产品与版本线 | 最新版本 | 发布日期 | 许可证 | 定位 |
| --- | --- | --- | --- | --- |
| **Valkey 9.2** | `9.2.0` | 2026-11-19 | BSD-3-Clause | 最新 GA（Path Hash 等新特性） |
| **Valkey 9.1** | `9.1.4` | 2026-11-18 | BSD-3-Clause | **默认推荐（生产稳定）** |
| **Valkey 9.0** | `9.0.8` | 2026-11-18 | BSD-3-Clause | 稳定维护线（零拷贝响应、MPTCP） |
| **Valkey 8.1** | `8.1.12` | 2026-11-18 | BSD-3-Clause | 兼容保守线（与 Redis 7.2 协议/RDB 高度兼容） |
| **Redis 8** | `8.x`（可选） | 持续维护 | RSALv2 / SSPLv1 / AGPLv3 | 仅当客户业务强依赖 Redis Stack 商业/专有模块（如特定 RedisSearch/JSON 特性）时显式开启 |

### 2.2 二进制构建、glibc 基线与 arm64 64K 页治理（ADR-026、ADR-028、ADR-068）

- **真实 64K 页崩溃实证（2026-11-19）**：`goharbor/harbor#24445` 实测确认，在 Ubuntu 24.04 arm64 `6.8.0-generic-64k` 64K 页内核上，默认以 4K jemalloc 编译的 `valkey` 容器启动直接以退出码 `139` 崩溃并报错 `<jemalloc>: Unsupported system page size`。
- **离线跨架构构建基线（ADR-068）**：
  - 优先采用 Valkey 官方发布二进制（amd64/arm64 4K 页）；为同时兼容 EL8/麒麟 V10（`glibc 2.28`）与 arm64 64K 页宿主机，kinstall 构建流水线在 EL8 基础容器中额外统一产出三份锁定二进制并经 `ldd` 检查：
    1. `valkey-9.1.4-linux-amd64`（`pageSizes: [4096]`）；
    2. `valkey-9.1.4-linux-arm64-4k`（`pageSizes: [4096]`）；
    3. `valkey-9.1.4-linux-arm64-64k`（编译参数 `make USE_SYSTEMD=yes BUILD_TLS=yes MALLOC=jemalloc JEMALLOC_CONFIGURE_OPTS="--with-lg-page=16"`，`pageSizes: [65536]`）；
  - 对 `glibc < 2.28` 的老旧 OS（如 CentOS 7 / Anolis 7），`Validate` 阶段直接提示切换 **`target: host-container`（Docker 容器部署）**，规避宿主机 glibc 限制；
  - `Plan` 阶段按主机 `Facts.PageSize` 自动匹配对应构建，缺失则以 `KI-PLAN-0012` 阻断。

### 2.3 BOM 示例（`artifacts.yaml` 节选）

```yaml
product: valkey
revision: "2026.11.1"
versions:
  "9.1.4":
    license: BSD-3-Clause
    artifacts:
      - name: valkey-tarball
        arch: amd64
        pageSizes: [4096]
        url: "https://download.valkey.io/releases/valkey-9.1.4-linux-amd64.tar.gz"
        sha256: "<由构建流水线实测锁定>"
      - name: valkey-tarball
        arch: arm64
        pageSizes: [4096]
        url: "https://download.valkey.io/releases/valkey-9.1.4-linux-arm64.tar.gz"
        sha256: "<由构建流水线实测锁定>"
      - name: valkey-tarball-64k
        arch: arm64
        pageSizes: [65536]
        url: "https://artifacts.kinstall.internal/valkey/valkey-9.1.4-linux-arm64-64k.tar.gz"
        sha256: "<EL8 容器 --with-lg-page=16 自建>"
  exporter:
    name: redis_exporter
    version: "1.99.0" # 2026-11-24，支持 Valkey 9.x/8.x 与 Redis
    license: MIT
```

## 3. 拓扑与规格建议

| 拓扑 | 节点数与角色 | 适用场景 | 限制与约束 |
| --- | --- | --- | --- |
| `standalone` | 1 台 `data` | 开发、测试、可容忍重启的纯缓存 | 无 HA |
| `replication-sentinel` | 1 主 + 1~2 从 `data` + 3（奇数）`sentinel`（可与 data 混布 3 节点） | **中小规模生产默认**（单分片 ≤ 32 GiB 内存）；兼容多键事务与 Lua | 需跨 3 故障域（zone/rack）；`quorum = 2`；`sentinel.conf` 须 `manageMode: create-only`（ADR-070） |
| `cluster` | ≥ 3 主 + 3 从（6 实例，3 宿主机每机 2 端口强制环状跨机主从反亲和，或 6 宿主机） | 大容量（> 32 GiB）或高吞吐水平扩展 | 16384 槽位；客户端需支持 Cluster 协议；先建 3 主再显式跨机挂 3 从（详见 `18 §3.4`），禁止主从落同一宿主机 |

**规格建议**：单实例 `maxmemory` 建议 ≤ 16~24 GiB（避免 RDB `fork()` 耗时与 COW 内存翻倍）；大内存场景优先通过 Cluster 多分片拆分。

## 4. 三种部署目标：`host-binary` / `host-container`（Docker）/ `kubernetes`

> **完整端到端代码级参考实现**（3 种拓扑 Spec YAML、Go 插件编排源码、`valkey.conf` / `sentinel.conf` / `docker-compose.yml` 完整模板与跨主机反亲和集群引导算法）见 **[18 架构深度评估与端到端开发落地指南 §3](./18-架构深度评估与端到端开发落地指南.md)**。

- **`host-binary`（生产核心默认）**：二进制部署到 `/opt/kinstall/bin/valkey/<version>/`，unit `kinstall-valkey-<instance>.service` 与 `kinstall-valkey-<instance>-sentinel.service`；单主机可按端口隔离多实例；遵循 ADR-069 以强类型 Go 插件实现集群引导逻辑。
- **`host-container`（Docker Engine 29.10 + Compose v5.8，ADR-066）**：
  - 适用场景：无 K8s 但希望快速标准化交付、或目标主机为老旧 glibc OS（如 CentOS 7）；
  - 编排方式：平台通过 `docker.ensure` + `image.load`（离线 Zot 导入多架构镜像；arm64 64K 页主机须选用 64K 兼容镜像）+ `compose.sync` 渲染 `docker-compose.yml` 与 `kinstall-compose-<instance>.service`；
  - 关键约束：宿主机仍须先应用 `cache` Profile（关 THP + `vm.overcommit_memory=1`）；Cluster 拓扑优先 `network_mode: host` 避免 NAT 错乱；数据目录挂载宿主机 XFS `/data/valkey/<instance>`；配置 `DOCKER-USER` 白名单链防绕过防火墙。
- **`kubernetes`（遵循 `06 §2–§3`）**：
  - Valkey Cluster 选用 Linux Foundation 官方 `valkey-io/valkey-operator`（`v0.7.1`，BSD-3-Clause）；
  - 若需 Standalone / Replication-Sentinel 拓扑，采用 `OT-Container-Kit/redis-operator`（Apache-2.0，配置 Valkey 官方镜像）或继续使用 `host-binary` / `host-container`；
  - 严禁使用已归档的 `Spotahome/redis-operator` 及冻结的 Bitnami Chart（ADR-042）。

## 5. 主机要求与容量声明

- **主机 Profile**：`base` + `cache`（`03 §6.1`）。
- **`HostRequirements`**：
  - `sysctl`：`vm.overcommit_memory = 1`（防止 RDB/AOF `fork()` 失败）、`net.core.somaxconn = 65535`、`net.ipv4.tcp_max_syn_backlog = 65535`；
  - 关闭透明大页 THP（`03 §6.5`：`never`，避免 fork 后延迟尖刺）；
  - `limits`：`nofile >= 65535`（`03 §6.3`）；
  - 端口放行：客户端端口（默认 `6379`，TLS 模式下仅监听加密端口）、Sentinel 端口（`26379`）、Cluster 总线端口（`clientPort + 10000` 即 `16379`）、Exporter 端口（`9121`）。
- **`HostRole.Claims`**：登记上述端口、数据目录 `/data/valkey/<instance>`、内存占用 = `maxmemory × 1.5`（预留 COW 与碎片缓冲；纯缓存无持久化取 `1.25×`）。
- **数据盘**：启用 RDB/AOF 持久化时要求独立 SSD/NVMe（XFS `noatime`），盘容量 ≥ `maxmemory × 3`（容纳 RDB + AOF 重写临时文件）。

## 6. 安装流程（`install` Pipeline）

| 顺序 | Phase | 目标角色 | 核心 Task（`02 §3.4`） | 并发 | 失败策略 | 幂等判据 |
| :---: | --- | --- | --- | :---: | --- | --- |
| 1 | `preflight` | 全部 | 检查 Profile `cache`、THP=never、`overcommit_memory=1`、页大小（ADR-028）、端口 Claims | 并行 10 | `failFast` | 只读检查 |
| 2 | `pki` | 本地（平台） | `Issuer.EnsureCA`（实例独立 CA）+ 为每节点签发 server/client 证书（含双栈 IP SAN） | 本地 | `failFast` | 已存在且 SAN 一致则复用 |
| 3 | `install-bin` | 全部 | `user.ensure(valkey)`、`dir.ensure`、`binary.install(valkey)`、`exec.allowlist`（如 KYSEC） | 并行 10 | `failFast` | 版本软链与 sha256 |
| 4 | `configure-data` | `data` | `file.sync`（`valkey.conf`、`users.acl`、`pki/*`、systemd unit）、`systemd.unit(enable+start)`、`wait.port` | 并行 10 | `failFast` | 模板 sha256 + `is-active` |
| 5 | `init-topology` | `data` / `sentinel` | `replication-sentinel`：配置 `replicaof` 并启动 3 个 Sentinel；`cluster`：执行 `valkey-cli --cluster create --cluster-replicas 1`（通过 `check` 判 `cluster_state:ok`） | `serial: true` | `failFast` | `INFO replication` / `CLUSTER INFO` |
| 6 | `deploy-exporter` | `data` | `binary.install(redis_exporter)`、`file.sync`（unit + 凭据文件 `0600`）、`systemd.unit`、`wait.http` | 并行 10 | `continueOnError` | 版本与 `/metrics` 200 |
| 7 | `verify` | 全部 | 写/读/删探针键、验证复制偏移量、TLS 握手、ACL 权限拒绝测试、记录 `InstalledFile` | 本地 | `failFast` | 探针通过 |

## 7. 配置模板与参数调优

### 7.1 `params` 核心字段示例

```yaml
topology: replication-sentinel   # standalone | replication-sentinel | cluster
port: 6379
tls:
  enabled: true                  # 生产默认开启
  requireClientCert: false       # 双向 mTLS 可选
memory:
  maxmemory: auto                # auto = 按宿主机可用内存计算，或显式 "8gb"
  maxmemoryPolicy: allkeys-lru   # 缓存场景默认；持久化业务改为 noeviction
persistence:
  mode: aof-rdb                  # none | rdb | aof | aof-rdb（默认混合持久化）
  appendfsync: everysec
ioThreads: auto                  # auto = min(4, max(1, vCPU/2))
```

### 7.2 参数计算公式与模板要点

- **`maxmemory` 自动计算**：独占宿主机时取 `floor((HostMemMB - OSReserveMB) × 0.65)`（启用持久化留 35% 给操作系统页缓存与 fork COW；`persistence.mode=none` 取 `0.80`），单实例上限默认提示 24 GiB。
- **关键配置片段（`valkey.conf.tmpl`）**：
  - `bind {{ .Host.PrivateIP }} 127.0.0.1`、`protected-mode yes`；
  - 启用 TLS 时设置 `port 0`、`tls-port {{ .Params.Port }}`、`tls-replication yes`、`tls-cluster yes`、`tls-protocols "TLSv1.2 TLSv1.3"`；
  - `aclfile /etc/kinstall/valkey/{{ .Instance.Name }}/users.acl`（密码不写主配置文件）；
  - `aof-use-rdb-preamble yes`、`auto-aof-rewrite-percentage 100`、`auto-aof-rewrite-min-size 256mb`；
  - 禁用/重命名高危命令通过 ACL 精细控制（`default off`，禁止 `FLUSHALL`/`FLUSHDB`/`DEBUG`/`CONFIG` 给应用账号）。

## 8. 安全设计

- **ACL 最小权限分角色**：
  - `kinstall-admin`：平台运维账号（全权限，仅平台 `SecretStore` 持有）；
  - `repl`：复制与 Sentinel 专用账号（`+psync +replconf +ping`）；
  - `exporter`：监控只读账号（`-@all +info +config|get +client|list +slowlog|get +cluster|info +latency|latest`）；
  - `app`：业务账号（禁 `@dangerous` 与 `@admin`，按需限定键前缀 `~app:*`）。
- **传输加密**：生产默认启用 TLS 1.2/1.3（独立 CA 签发，证书 1 年期，30 天前告警并通过 `rotate-certs` 无停机轮换）。
- **静态文件权限**：`valkey.conf` 与 `users.acl` 权限 `0640` 属主 `root:valkey`；密钥不出现在命令行（`valkey-cli` 统一通过 `REDISCLI_AUTH` 环境变量或临时 `0600` 文件传参）。

## 9. 运维操作（`06 §4`）

| 操作 | 实现要点 |
| --- | --- |
| `scale` | Sentinel 模式：增删从节点（自动 `REPLICAOF` 并同步 Sentinel）；Cluster 模式：扩容先加空主从节点再执行 `valkey-cli --cluster rebalance`；缩容先 `reshard` 迁空槽位再 `del-node` |
| `upgrade` | 允许路径：Valkey `8.1.x → 9.0.x → 9.1.x → 9.2.x`（禁止跨两代跳升）；先逐台升级从节点并验证 `master_link_status:up` → 触发 `SENTINEL FAILOVER` / `CLUSTER FAILOVER` 优雅切主 → 升级原主节点；**迁移提示**：Redis `7.2.x` 可通过复制/RDB 平滑迁至 Valkey `8.1/9.x`；Redis `8.x` 的新 RDB 格式与 Valkey 不保证兼容，须走应用双写或逻辑迁移 |
| `backup` / `restore` | 优先在**从节点**触发 `BGSAVE`，校验 `LASTSAVE` 推进与 RDB `valkey-check-rdb` 完整性，压缩并加密上传至 NFS / S3，写入 `BackupRecord`；恢复时停机替换 `dump.rdb`（关闭 AOF 或恢复后 `BGREWRITEAOF`） |
| `reconfigure` | 支持动态生效的参数（`maxmemory`、`maxmemory-policy`、`slowlog-*`）先 `CONFIG SET` 再同步落盘 `valkey.conf`（零重启）；不可热改参数（`port`、`io-threads`、TLS）按先从后主滚动重启 |
| `failover` | 校验目标从节点 `master_link_status=up` 且 `repl_offset` 差值 < 1 MiB 后执行优雅切换 |

## 10. 监控、日志与告警规则（F10）

采用 `oliver006/redis_exporter v1.99.0`，关键指标与告警模板：

```yaml
groups:
  - name: kinstall.valkey
    rules:
      - alert: ValkeyDown
        expr: redis_up == 0
        for: 1m
        labels: { severity: critical }
        annotations:
          summary: "Valkey 实例 {{ $labels.kinstall_instance }} 节点不可达"
      - alert: ValkeyMemoryHigh
        expr: redis_memory_used_bytes / redis_memory_max_bytes > 0.85
        for: 5m
        labels: { severity: warning }
      - alert: ValkeyMemoryFragmentationHigh
        expr: redis_mem_fragmentation_ratio > 1.5 and redis_memory_used_bytes > 1073741824
        for: 15m
        labels: { severity: warning }
      - alert: ValkeyReplicationBroken
        expr: redis_master_link_up == 0
        for: 1m
        labels: { severity: critical }
      - alert: ValkeyReplicationLagHigh
        expr: redis_master_last_io_seconds_ago > 15
        for: 2m
        labels: { severity: warning }
      - alert: ValkeyRdbSaveFailed
        expr: redis_rdb_last_bgsave_status == 0
        for: 1m
        labels: { severity: critical }
      - alert: ValkeyClusterStateNotOk
        expr: redis_cluster_state != 1
        for: 1m
        labels: { severity: critical }
      - alert: ValkeyRejectedConnections
        expr: increase(redis_rejected_connections_total[5m]) > 0
        for: 1m
        labels: { severity: critical }
```

## 11. 测试与验收

1. **功能与幂等**：3 种拓扑离线安装 + 连续两次 `apply` 第二次全 Skipped；
2. **HA 故障注入**：`replication-sentinel` 强杀主进程，验证 15 s 内完成自动选主且原主恢复后自动降从；`cluster` 强杀 1 个主节点，验证其从节点接管槽位、数据零丢失；
3. **64K 页门禁**：在 arm64 64K 页主机上用 4K BOM 触发 `KI-PLAN-0012` 阻断，换 64K BOM 成功启动；
4. **备份与恢复**：写入 10 万键 + `backup` + 恢复到新实例校验全量 SHA-256 一致；
5. **升级回归**：`8.1.12 → 9.1.4` 在持续压测（`valkey-benchmark`）下滚动升级，错误率 < 0.1%。

## 12. 风险与待办

- Valkey 官方二进制对旧 glibc（如 CentOS 7 / EL8 glibc 2.28）的兼容性需在 M4 构建流水线实测，必要时在 EL8 容器内自编统一静态/低 glibc 构建；
- `valkey-io/valkey-operator` 尚处 `v0.7.x`，达到 1.0 GA 后再将 K8s 模式列为 P0 推荐。

## 13. 参考来源

- [Valkey 官方下载与版本（9.2.0 / 9.1.4 / 9.0.8 / 8.1.12）](https://valkey.io/download/)
- [Valkey GitHub Releases](https://github.com/valkey-io/valkey/releases)
- [Redis Licensing](https://redis.io/legal/licenses)
- [valkey-io/valkey-operator](https://github.com/valkey-io/valkey-operator)
- [oliver006/redis_exporter v1.99.0](https://github.com/oliver006/redis_exporter/releases)

## 附 A. 本文涉及的决策与清单映射

- **ADR-013 定稿（状态改为“已采纳”）**：缓存/键值插件默认使用 **Valkey**（BSD-3-Clause，推荐 9.1.x 稳定线 / 9.2.x 最新线），**Redis 8.x** 仅作显式可选；针对 arm64 64K 页主机通过 BOM `pageSizes` 匹配专用 jemalloc 64K 构建，阻断 4K-only 构建（ADR-028）。
- **关闭设计项**：F6。
- **关闭调研项**：R-MW-05。
