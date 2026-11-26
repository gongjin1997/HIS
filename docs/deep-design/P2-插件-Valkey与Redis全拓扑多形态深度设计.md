# P2 核心插件深度设计（2/7）：Valkey / Redis 全拓扑与多形态深度设计

> **本文定位**：kinstall 第三阶段缓存/键值核心插件深度设计（`plugins/valkey/`，**ADR-013 / ADR-028 / ADR-066 / ADR-068 / ADR-070**），结合 `07-插件-Valkey-Redis.md` 与 `18 §3` 代码样板，沉淀**单节点 / 主从+哨兵 / 3主3从+ Cluster × `host-binary` / `host-container` / `kubernetes`** 全矩阵生产级细节（含 64K 页 jemalloc、COW 内存爆炸防护、环状机架反亲和与无损在线扩缩容迁槽）。
>
> **版本**：v1.1（核心插件 2/7，2026-11-26；v1.1 对齐 `06`/`07` 拓扑枚举、最小节点数、文件属主/权限、`LimitNOFILE=65535`、`OOMScoreAdjust=-900`、`maxmemory` 0.65/0.80、ACL 用户名、BOM 制品名、Operator、Exporter 监听及麒麟 V10 SP3=64K）。默认 **Valkey `9.1.4 / 9.2.0`**（BSD-3-Clause，入公共离线包），**Redis `8.x`** 仅显式勾选且遵循红灯许可门禁（客户侧按需构建，ADR-059）。

---

## 1. 全矩阵选型与架构拓扑设计

| 拓扑形态 | 最小节点数 | 适用规模与 SLA | 高可用与反亲和布局规则 |
| --- | :---: | --- | --- |
| **`standalone`（单节点）** | 1 | 开发/测试、非核心缓存、Harbor 轻量单机缓存 | 单进程 + systemd / Compose 托管；强制开启 `ACL` + 禁用高危命令（`FLUSHALL`/`CONFIG`/`DEBUG` 仅限管理 ACL 用户） |
| **`replication-sentinel`（主从 + 哨兵）** | ≥ 3（1 主 + 1–2 从 + 3 哨兵；与 `07 §3` 一致） | 内存 ≤ 32 GiB、要求自动 Failover（RTO ≤ 10s）且业务不兼容 Cluster 多键限制 | 哨兵数强制奇数（3/5）；**`sentinel.conf` 必须使用 D2 §3 `create-only` 模式**（因 Sentinel 运行期会自改写 `sentinel.conf` 持久化 epoch 与新主 IP，覆盖将导致脑裂！后续调参走 `SENTINEL SET` 动态命令） |
| **`cluster`（分布式分片集群）** | 最少 6 实例（可 3 台独立宿主机 × 2 实例实现主从反亲和，或 6 节点起） | 大容量（> 32 GiB）、高吞吐（> 15 万 QPS）、水平在线扩缩容 | **★ 环状机架反亲和算法（Ring Anti-Affinity）**：假设跨 3 个机架/可用区（AZ1/AZ2/AZ3），Master-1 在 AZ1 → 其 Replica-1 强制落在 AZ2；Master-2 在 AZ2 → Replica-2 落在 AZ3；Master-3 在 AZ3 → Replica-3 落在 AZ1，确保**任意整机架/整可用区断电 0 分片不可用**！ |

### 三元部署目标落地差异（ADR-066）

- **`host-binary`（生产默认，最高性能与极低抖动）**：
  - 二进制落盘 `/opt/kinstall/bin/valkey/9.1.4/valkey-server`（由 `D3 §3` EL8 `glibc 2.28` 分别编译 `amd64` / `arm64-4k` / `arm64-64k(--with-lg-page=16)`）；
  - 配置 `/etc/kinstall/valkey/<instance>/valkey.conf`（`0640`，属主 `root:valkey`）、数据 `/data/valkey/<instance>/`、systemd unit `kinstall-valkey-<instance>.service`（`Type=notify` + `LimitNOFILE=65535` + `OOMScoreAdjust=-900`，遵循 `06 §1.1` / `07`）。
- **`host-container`（Docker Engine 29 + Compose v5 + systemd）**：
  - 适用于 CentOS 7 (`glibc 2.17`) 或要求容器化隔离的宿主机；镜像经 `D3 §2` `registryMirror` 强锁定 `@sha256:`；**64K 页主机预检强拦截上游 4K Alpine 镜像**，自动切换为 kinstall 自建 64K 专用容器镜像；
  - **Cluster 容器网络铁律**：必须配 `network_mode: host` 或显式配置 `cluster-announce-ip <宿主机私网IP>` + `cluster-announce-port` + `cluster-announce-bus-port`（`port + 10000`），严禁将容器内 `172.x` Bridge IP 广播给外部客户端导致 `MOVED` 重定向死循环！
- **`kubernetes`（Helm v4 + 开源 Valkey/Redis Operator）**：
  - 严禁 Bitnami Chart（ADR-042）；Cluster 使用 `valkey-io/valkey-operator`（BSD-3），备选 OT-Container-Kit（Apache-2.0）（与 `06 §3`/`07 §4` 一致）+ PodAntiAffinity（按 `topology.kubernetes.io/zone` 强反亲和）+ Local PV / 高 IOPS NVMe StorageClass。

---

## 2. 四大生产级稳定性攻坚治理

### 2.1 `arm64-64K` 页 jemalloc 致命崩溃治理（ADR-028 / Harbor#24445）

- 在鲲鹏 920 + 麒麟 V10 SP3 / RHEL 8 系（`getconf PAGESIZE == 65536`；注：`03-A §5` 实测麒麟 V11 为 4K）上运行默认 4K 页编译的 jemalloc 会立即触发 `Unsupported system page size` 崩溃。
- `plugins/valkey` 在 `Validate()` 阶段自动读取每台目标主机 `Facts.PageSize`：
  - `4096` → 自动从 BOM 解析官方上游 `valkey-9.1.4-linux-arm64.tar.gz`（x86_64 为 `valkey-9.1.4-linux-amd64.tar.gz`，与 `07 §2.3` 一致）；
  - `65536` → 自动解析平台 EL8 容器 `--with-lg-page=16` 自建的 `valkey-9.1.4-linux-arm64-64k.tar.gz`（`07 §2.3`）；
  - 若目标离线包缺失 64K 变体，直接阻断 Plan 并返回 `KI-PLAN-0012`（绝不带病安装）。

### 2.2 `BGSAVE` / `AOF Rewrite` 写时复制（COW）OOM 内存公式

很多生产故障源于盲目把 `maxmemory` 配成宿主机内存的 80%–90%，一旦触发 `BGSAVE` 或 AOF 重写 `fork()`，高写负载引发 Linux Copy-on-Write 内存翻倍瞬间触发宿主机 OOM Killer 杀进程！
- **kinstall 强制内存公式（`Validate()` 校验并自动计算推荐值）**：
  - `maxmemory = floor((MemTotal − OSReserveMB) × 0.65)`；`persistence.mode=none` 纯缓存模式上限 `× 0.80`（与 `07 §7` / `18 §3` 一致）；
  - 主机 Profile `cache` 强制落地：`vm.overcommit_memory = 1`（防 `fork()` 返回 ENOMEM）、`transparent_hugepage = never`（杜绝 THP 导致 2MB 巨页 COW 延迟毛刺与内存放大 10×！）、`net.core.somaxconn = 65535`。

### 2.3 全链路 TLS + ACL 最小权限 + 无明文密码执行

- 默认开启实例独立 CA 签发的 TLS（`tls-port`、`tls-replication yes`、`tls-cluster yes`、`tls-protocols "TLSv1.2 TLSv1.3"`）；
- 禁用老旧单一 `requirepass`，全部生成独立 `/etc/kinstall/valkey/<instance>/users.acl`（`0640`，属主 `root:valkey`）：`default off`、`kinstall-admin`（运维专用）、`repl`（复制专用 `masteruser`）、`exporter`（只读 `INFO/PING`）、`app`（按业务键前缀隔离，禁用 `FLUSHALL`/`CONFIG`/`SHUTDOWN`，与 `07 §8` / `18 §3` 一致）；
- 插件内执行 `valkey-cli` 探测/建集群时，**严禁在命令行拼接 `-a <password>`**（会暴露于 `ps aux`），一律通过环境变量 `REDISCLI_AUTH` 经管道传入并全程注册 `RedactWriter` 脱敏！

### 2.4 Cluster 引导幂等与在线扩缩容迁槽（`Scale` / `Upgrade`）

1. **幂等 Cluster 创建**：`cmd.run` 的 `checkCmd` 先查询 `CLUSTER INFO` 的 `cluster_state:ok` 与 `cluster_slots_assigned:16384`，已收敛则 100% `Skipped`；未初始化时执行 `valkey-cli --cluster create <masters> --cluster-yes`，再按环状反亲和逐个执行 `--cluster add-node <replica> <master> --cluster-slave --cluster-master-id <id>`（精确掌控主从机架配对，不交给随机分配！）。
2. **在线扩容（如 3 主 3 从 → 4 主 4 从）**：
   - 新增 2 台节点部署启动并入群 → 调用 `valkey-cli --cluster rebalance --cluster-use-empty-masters --cluster-pipeline 10` 平滑迁槽（限速防打满主线程，监控迁槽期间 p99 延迟 < 5 ms）→ 全程零停机。
3. **无损滚动升级（如 `9.1.3 → 9.1.4`）**：
   - 先逐台升级所有 **Replica**（更新二进制软链 → 重启 → 等 `master_link_status:up` 与复制偏移追平 `offset_lag == 0`）；
   - 再对每组主从发起优雅 `CLUSTER FAILOVER`（或 Sentinel `FAILOVER`）将流量无损切至已升级的新版 Replica → 升级原 Master 成为新 Replica → 全程零写入中断！
4. **可观测接入**：自动部署同机 `redis_exporter`（监听内网管理 IP `<mgmt_ip>:9121` + TLS，遵循 `06 §5` 供 Prometheus 抓取），配套 Prometheus 告警规则（`rejected_connections > 0`、`mem_fragmentation_ratio > 1.5`、`rdb_last_bgsave_status != 1`、`cluster_state != 1`）。
