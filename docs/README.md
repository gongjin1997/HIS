# kinstall 完整系统设计、开发与 Checklist 文档包

> **包版本**：**`v4.5-complete`**（2026-11-26，**全域 45 篇深度一致性终审封板** · **全三阶段设计封板 + 24 表可执行 DDL 复审 + 126 项 [`DEV_TASK_CHECKLIST.md`](./DEV_TASK_CHECKLIST.md) + 独立前端 UI 样式与组件视觉基准 [`DESIGN.md`](./DESIGN.md) + 全附件实战复查 5 项深水区优化 + 整体架构全景与一致性审计 [`ARCHITECTURE.md`](./ARCHITECTURE.md) + `DESIGN.md` v2.0 深度重构**）。版本号、上游生命周期与 Go / 前端依赖均于 2026-11-25 / 26 实查核验，研发启动前请按 BOM revision 复核。
>
> **状态**：**全部 45 篇正式文档（22 篇基线、`ARCHITECTURE.md`、`DESIGN.md`、`WEB_DESIGN.md`、`DEV_TASK_CHECKLIST.md`、`deep-design/` 18 篇及索引）与 `spec/` 规范已 100% 终版封板！** 设计项 **132 项（✅ 124 · 🟡 3 · ⬜ 4 · ⏸ 1）**，调研项 **116 项（✅ 108 · 🟡 4 · ⬜ 3 · ⏸ 1）**，架构决策 **ADR-001～077 全量采纳（0 项待定）**，24 张数据库表已在真实 SQLite 实测通过，`spec/gen_os_docs.py --check` 退出码 0。

## 怎么用

- **快速上手与研发启动**：先读 **[`DEV_TASK_CHECKLIST.md`](./DEV_TASK_CHECKLIST.md)**（126 项原子开发任务，可直接导入 Jira / GitHub Projects）与 [00-系统设计总Checklist.md](./00-系统设计总Checklist.md)（总进度、132 项设计项、ADR-001～077），随后按角色切入：
  - 架构师 / 技术负责人：**`ARCHITECTURE.md`** → `18` → `02` → `deep-design/D1–D3` → `deep-design/G1–G3` → `DESIGN.md` / `WEB_DESIGN.md` → `deep-design/R1–R2` → `05 §1–§3` → `06` → `12` / `13`
  - 后端与底座研发：**`deep-design/G1 §7`（全仓 Go 1.27 后端代码总方案 + 24 表可执行 DDL）** → `deep-design/D1–D3`（含 `D3 §7` 本地/在线/华为云 OBS 三元介质引擎）→ `05` → `18 §3`（Valkey 代码样板）
  - **UI/UX 与前端研发**：以根目录 **[`DESIGN.md`](./DESIGN.md)** 为视觉样式、Design Tokens、Element Plus 主题覆写与 `Ki*` 组件唯一基准，结合 **[`WEB_DESIGN.md`](./WEB_DESIGN.md)**（IA、11 页面原型、动态表单、3-Way Diff、SSE 分屏终端、Vue Flow 2.0 画布与 `go:embed` 工程架构）落地
  - 插件研发：`deep-design/P1–P10`（覆盖 K8s、Valkey/Redis、**大型 ELK/OpenSearch + Kafka 4.x 中间消费全链路**、PG、MySQL、SeaweedFS/Ceph、SkyWalking+BanyanDB、DevOps 交付栈、RocketMQ/Seata/ZK、Milvus/Mongo/Neo4j/OnlyOffice/`generic-app`）+ 基线 `04` / `07`–`11` / `15`–`17`
  - 运维 / 安全 / 测试：`03` / `04` → `deep-design/D3` → `deep-design/G2`（等保密评实操）→ `deep-design/G3`（多架构测试平台 + 15 项故障注入 + 500 节点混合压测 SOP）→ `12` / `13` / `14`
- **校验 OS 规范与生成表**：在文档根目录执行 `python3 spec/gen_os_docs.py --check`（需 PyYAML）。

## 文件清单

| 文件 | 内容 | 状态 |
| --- | --- | --- |
| [`文档中心.md`](./文档中心.md) | 项目定位、阅读路径、全量基线文档、`DESIGN.md`、`WEB_DESIGN.md`、`DEV_TASK_CHECKLIST.md` 与 `deep-design/` 完整索引 | **v4.5-complete** |
| [`ARCHITECTURE.md`](./ARCHITECTURE.md) | **整体架构全景与一致性审计（唯一架构口径）**：7 条硬约束与质量属性排序、C4 上下文、六层架构与依赖规则、28 个 `internal/` 规范包图 + depguard 矩阵、Operation/Stack/介质解析/SSE 运行时流、三形态部署拓扑、24 表 ER、横切关注点、P1–P10 矩阵、SLO、ADR-001～077 十大主题簇、F-01～F-10 跨文档漂移修复、RA-01～RA-12 风险台账、O-01～O-12 优化项（挂载既有 `T-*`）与架构适应度函数 | **v1.1（终审）** |
| [`DEV_TASK_CHECKLIST.md`](./DEV_TASK_CHECKLIST.md) | **全栈工程开发任务 Checklist（126 项原子任务 `T-M0-01`～`T-M5-25`）**：M0–M5 分阶段、5 泳道并行分工、DoD 验收标准与 10 条 CI 红线 | **v1.3（封板）** |
| [`DESIGN.md`](./DESIGN.md) | **专门的前端 UI 设计系统与组件样式基准（v2.0 深度重构）**：三层 Token（原色 `--ki-p-*` → 语义 → 组件）、全量实测 WCAG 2.2 对比度表、环境上下文色 / Okabe-Ito 可视化色板 / ANSI 16 色、8 级排版与 CJK 混排、双密度栅格与 Z-Index、可直接落盘的 `tokens.css` / `fonts.css` / `element-overrides.css`（`html[data-theme]` + `@supports color-mix()` 回退）/ `base.css` / CSP 合规主题引导、12 大基础 `Ki*` + 8 大领域组件与状态矩阵、6 类页面模板与 `useKiFormat()` 数据格式、A11y、Stylelint / 对比度 / 视觉回归门禁与 16 条 PR 红线 | **v2.0（深度重构）** |
| [`WEB_DESIGN.md`](./WEB_DESIGN.md) | **Web 控制台信息架构与前端工程规范（关闭 B9 & 定稿 ADR-023）**：IA、11 核心页面（含本地/在线/OBS 桶介质源中心）、动态插件表单协议、3-Way Diff、SSE 分屏终端、Vue Flow 2.0 画布、Vue 3.5.45 / `@hey-api/openapi-ts` 与 `go:embed` 打包 | **v1.3（封板）** |
| [`00-系统设计总Checklist.md`](./00-系统设计总Checklist.md) | 132 项设计项（✅ 124 · 🟡 3 · ⬜ 4 · ⏸ 1）、全三阶段封板台账、ADR-001～077（0 待定）、交叉引用、交接记录、§9 里程碑索引 | **v4.5-complete** |
| [`01-调研Checklist.md`](./01-调研Checklist.md) | 116 项调研项（✅ 108 · 🟡 4 · ⬜ 3 · ⏸ 1），实证结论与 2026-11 来源 | **v3.1（全量定稿）** |
| [`02-平台架构与插件体系.md`](./02-平台架构与插件体系.md) | 定位与规模目标、术语表、分层架构、插件体系、三元目标与 Docker Task、`Stack` 复合栈、三向漂移检测、自研任务引擎、平台 HA、模板与 `expr`、多集群 | **v2.2（定稿）** |
| [`03-CMDB与主机初始化.md`](./03-CMDB与主机初始化.md) | 数据模型、Facts、OS 支持矩阵与适配器、22 个初始化模块（含首尾 `backup/verify`）与 Profile、预检与漂移、CMDB 功能、等保主机加固（§9） | **v2.1（定稿）** |
| [`03-A-OS软件源实测矩阵.md`](./03-A-OS软件源实测矩阵.md) | 48 组 OS 官方源与包内实测：os-release、内核/systemd/nftables、默认 cgroup、逻辑包映射、arm64 页大小、冲突包、KYSEC | **v1.1（定稿）** |
| [`04-Kubernetes安装规划与设计-重建版.md`](./04-Kubernetes安装规划与设计-重建版.md) | 1.35–1.38 模板分支、BOM `2026.11.3`、混合架构、双栈与 nftables、CRI/CNI/CSI（SELinuxMount、厂商接入）、PKI、本地代理 HA、预留分档、Helm v4 SDK 与审计 | **v2.1（定稿）** |
| [`05-开发文档.md`](./05-开发文档.md) | 工程结构与 depguard、Go 1.27 与 35+ 依赖锁定、核心 Go 接口（含 Docker/Compose Task）、CLI/REST+SSE、RBAC/审批/导入/GitOps/通知、错误码/slog/i18n/OpenBao 信封加密、测试与契约、goreleaser/CI/升级、插件指南与 M0 拆解 | **v1.2（定稿）** |
| [`06-中间件插件通用规范.md`](./06-中间件插件通用规范.md) | 统一目录/unit 规范、`host-binary` / `host-container`（Docker 29.10 + Compose v5.8）/ `kubernetes` 三元选型、Operator 总表（禁 Bitnami）、日志采集（弃 Promtail）、声明式包与 gRPC 外部插件分工 | **v1.1（定稿）** |
| [`07-插件-Valkey-Redis.md`](./07-插件-Valkey-Redis.md) | 默认 Valkey 9.1.4 / 9.2.0（BSD）、Redis 8 可选（ADR-013）；EL8 glibc 2.28 + arm64 64K 页 jemalloc 编译配方；单节点/哨兵/Cluster × 二进制与 Docker | **v1.1（定稿）** |
| [`08-插件-PostgreSQL.md`](./08-插件-PostgreSQL.md) | PG 18.7 默认 / 19.0 GA；Patroni 4.1.5 + etcd + PgBouncer + HAProxy/VIP；pgBackRest ≥ 2.60.0；CloudNativePG 1.30.1 + Barman Plugin | **v1.1（定稿）** |
| [`09-插件-MySQL.md`](./09-插件-MySQL.md) | MySQL 8.4 LTS / 9.7 LTS（禁 8.0）；InnoDB Cluster（MGR + Router + Shell）；Percona XtraBackup 8.4/9.7；Percona Operator（PS）1.3.0 / MOCO | **v1.1（定稿）** |
| [`10-插件-ES-OpenSearch.md`](./10-插件-ES-OpenSearch.md) | 默认 OpenSearch 3.10.0（Apache-2.0）/ Operator 3.0 GA；ES 9.5/8.19 + ECK 3.5 可选且不入公共离线包；31 GiB 堆上限与滚动升级 | **v1.1（定稿）** |
| [`11-插件-对象存储.md`](./11-插件-对象存储.md) | 淘汰归档的 MinIO CE（ADR-012）；分层选型：默认 SeaweedFS 4.54（Apache-2.0）、大规模 Ceph RGW、可选 RustFS 1.0.6 / Garage v2.5.0 | **v1.1（定稿）** |
| [`12-离线与供应链.md`](./12-离线与供应链.md) | 端到端信任链、BOM 独立发布、OS 包闭包流水线、内置 HTTP/Zot v2.1.23、Bundle 与增量补丁、Syft+Grype 双扫描、全平台许可证汇总 | **v1.1（定稿）** |
| [`13-安全合规.md`](./13-安全合规.md) | STRIDE 威胁模型、等保 2.0 三级与密评四层面全映射、国密外围边界与纯 Go SM4-GCM（`emmansun/gmsm`）、审计哈希链与 `compliance report` 证据包 | **v1.1（定稿）** |
| [`14-测试与验收.md`](./14-测试与验收.md) | 12 组正交 VM 矩阵与三层 arm64 资源、Ubuntu 26.04 `sudo-rs`/`uutils` 回归、商业 OS 采样、nftables 专项、15 项故障注入、1.35→1.38 升级回归、`hydrophone` + `kwok` 500 节点模拟与验收模板 | **v1.1（定稿）** |
| [`15-插件-ELK日志全栈.md`](./15-插件-ELK日志全栈.md) | OpenSearch 全栈（默认，Apache-2.0）与 ELK 9.5.7（可选）：Fluent Bit/Filebeat + Kafka + Data Prepper 2.16 / Logstash + Dashboards / Kibana | **v1.1（定稿）** |
| [`16-插件-Kafka.md`](./16-插件-Kafka.md) | Apache Kafka 4.3.2 / 4.4.0 KRaft（无 ZooKeeper）、KIP-858 JBOD、随包 JDK 21、Strimzi 1.3.2、Cruise Control、Kafbat UI、双轨监控 | **v1.1（定稿）** |
| [`17-插件-SkyWalking-APM.md`](./17-插件-SkyWalking-APM.md) | Apache SkyWalking 11.1.0 + Horizon UI 1.1.0 + BanyanDB 0.11.2（默认）/ OpenSearch + Satellite 1.4.0 + 多语言离线 Agent + SWCK 0.11.0 | **v1.1（定稿）** |
| [`18-架构深度评估与端到端开发落地指南.md`](./18-架构深度评估与端到端开发落地指南.md) | 12 维红队架构评审与 8 大盲区修补 + **Valkey/Redis 单节点/哨兵/Cluster × 二进制与 Docker 端到端代码落地样板** | **v1.1（定稿）** |
| [`deep-design/D1-底座-执行器与安全凭据及OS适配深度设计.md`](./deep-design/D1-底座-执行器与安全凭据及OS适配深度设计.md) | **底座 1/3**：Go 六边形代码方案与 9 大设计模式 + SSH/SFTP 双层限流连接池、Async Detached、`sudo-rs#1668`/`pam_faillock`、跨块脱敏、OpenBao 2.7.2、1-RTT POSIX Facts | **v1.3（封板）** |
| [`deep-design/D2-底座-存储引擎任务编排与全量内置Task深度设计.md`](./deep-design/D2-底座-存储引擎任务编排与全量内置Task深度设计.md) | **底座 2/3**：SQLite/PG 13 张底座表 DDL（合 G1 共 24 表，含 `subnets_json`）、Fencing Token 租约抢占与防死锁锁、3-Way Diff、`Stack` DAG（含 `externalRefs` + `urlEncode`）、15 Tasks（含 `LimitNOFILE=1048576`、`data-root` 守卫、`compose.sync` `file\|dir` 预建）与 Docker 29 防火墙守卫 | **v1.5（封板）** |
| [`deep-design/D3-底座-离线供应链跨架构构建与分发体系深度设计.md`](./deep-design/D3-底座-离线供应链跨架构构建与分发体系深度设计.md) | **底座 3/3**：零出网 `sigstore-go`+SM2、`oras-go/v3`、全局 `registryMirror`、EL8+64K 构建、`runtime/{jdk,maven,node}`、Jenkins/APOC 闭包、HTTP Range 限速、Zot 边缘缓存、**§7 本地/在线/OBS·S3 三元介质引擎（ADR-077）** | **v1.2（封板）** |
| [`deep-design/G1-平台-服务层高可用审批审计与Go后端完整代码方案.md`](./deep-design/G1-平台-服务层高可用审批审计与Go后端完整代码方案.md) | **平台 1/3**：CLI/Server 同源 Facade、OpenAPI 3.1 + 无丢帧多副本 SSE、OIDC/Argon2/SQL+Go 双重防自审审批、防分叉审计链、滚动升级 `StateMigrator`、11 张控制面表 DDL（合 D2 共 24 表）与**全仓 Go 1.27 后端代码方案** | **v1.2（封板）** |
| [`deep-design/G2-平台-多集群生态集成与等保密评实操深度设计.md`](./deep-design/G2-平台-多集群生态集成与等保密评实操深度设计.md) | **平台 2/3**：多集群短期 kubeconfig 与 RBAC、外部 CMDB 双向同步与两段式 GitOps、多通道防风暴通知、等保 2.0 三级与密评实操（Tongsuo TLCP 网关 + 证据包） | **v1.1（封板）** |
| [`deep-design/G3-平台-多架构测试平台故障注入与500节点压测SOP深度设计.md`](./deep-design/G3-平台-多架构测试平台故障注入与500节点压测SOP深度设计.md) | **平台 3/3**：三层多架构测试平台、Ubuntu 26.04 容器契约、15 项自动化故障注入脚本、20 真机 + 480 `kwok` 500 节点混合压测 SOP | **v1.1（封板）** |
| [`deep-design/P1-插件-Kubernetes二进制生产级深度设计.md`](./deep-design/P1-插件-Kubernetes二进制生产级深度设计.md) | **核心插件 1/7**：K8s 1.35–1.38 二进制生产级插件、混合架构与版本分支、本地代理 HA / Calico nftables / CSI `SELinuxMount`、BOM `2026.12.1` 升级 SOP | **v1.1（封板）** |
| [`deep-design/P2-插件-Valkey与Redis全拓扑多形态深度设计.md`](./deep-design/P2-插件-Valkey与Redis全拓扑多形态深度设计.md) | **核心插件 2/7**：Valkey/Redis 全拓扑（单机/哨兵/Cluster 环状反亲和）× 三元形态、64K jemalloc、COW/THP 治理、`create-only` 保护与迁槽 | **v1.1（封板）** |
| [`deep-design/P3-插件-大型ELK与OpenSearch加Kafka中间消费全链路深度设计.md`](./deep-design/P3-插件-大型ELK与OpenSearch加Kafka中间消费全链路深度设计.md) | **核心插件 3/7**：**大型 ELK / OpenSearch + Kafka 4.x KRaft 中间消费全链路**（`KIP-848`、Data Prepper `2.16.x` `acknowledgments` + S3/OBS DLQ、分区/分片对齐、4 级背压、Mapping 爆炸防护、180 天归档与完整 `Stack` YAML） | **v1.1（封板）** |
| [`deep-design/P4-插件-PostgreSQL高可用与PITR深度设计.md`](./deep-design/P4-插件-PostgreSQL高可用与PITR深度设计.md) | **核心插件 4/7**：PostgreSQL 18.7/19.0 + Patroni 4.1.5 + etcd + PgBouncer + HAProxy/VIP、`pgBackRest >= 2.60.0` PITR、声明式最小权限 `bootstrapDatabases`、脑裂防护与 CNPG | **v1.2（封板）** |
| [`deep-design/P5-插件-MySQL-InnoDBCluster与物理热备深度设计.md`](./deep-design/P5-插件-MySQL-InnoDBCluster与物理热备深度设计.md) | **核心插件 5/7**：MySQL 8.4/9.7 LTS InnoDB Cluster（MGR + Router + Shell）、大事务流控、XtraBackup 物理热备 + binlog PITR、声明式最小权限 `bootstrapDatabases`、Percona Operator / MOCO | **v1.2（封板）** |
| [`deep-design/P6-插件-对象存储SeaweedFS与Ceph多引擎深度设计.md`](./deep-design/P6-插件-对象存储SeaweedFS与Ceph多引擎深度设计.md) | **核心插件 6/7**：SeaweedFS 4.54（热副本 + 冷 EC 10+4）、Ceph RGW、RustFS / Garage 四引擎统一契约与外部华为云 OBS 联邦备份闭环 | **v1.1（封板）** |
| [`deep-design/P7-插件-SkyWalking-APM与BanyanDB深度设计.md`](./deep-design/P7-插件-SkyWalking-APM与BanyanDB深度设计.md) | **核心插件 7/7**：SkyWalking 11.1 + Horizon UI + BanyanDB 0.11.2（默认）/ OpenSearch、Satellite 1.4、Kafka 异步通道、尾部采样与多语言离线探针 | **v1.1（封板）** |
| [`deep-design/P8-插件-DevOps交付栈Harbor-GitLab-Jenkins-SonarQube与Nginx深度设计.md`](./deep-design/P8-插件-DevOps交付栈Harbor-GitLab-Jenkins-SonarQube与Nginx深度设计.md) | **扩展插件 8/10**：Harbor 2.16（arm64 + Valkey 64K）、GitLab 19 / Forgejo、Jenkins 2.580 JCasC + 多版本 JDK/Maven/Node、SonarQube 26.x、Reposilite / Verdaccio、Nginx L4/L7 | **v1.0（封板）** |
| [`deep-design/P9-插件-业务消息RocketMQ与事务Seata及ZooKeeper深度设计.md`](./deep-design/P9-插件-业务消息RocketMQ与事务Seata及ZooKeeper深度设计.md) | **扩展插件 9/10**：RocketMQ 5.5.1（DLedger Controller + `brokerIP1`）、Seata 2.6.0（原生 Raft + NamingServer）、ZooKeeper 3.9.7 遗留兼容插件 | **v1.0（封板）** |
| [`deep-design/P10-插件-向量Milvus文档Mongo图Neo4j协同OnlyOffice与通用业务栈深度设计.md`](./deep-design/P10-插件-向量Milvus文档Mongo图Neo4j协同OnlyOffice与通用业务栈深度设计.md) | **扩展插件 10/10**：Milvus 2.6/3.0 + Attu（`19001` 防撞）、MongoDB 8.0 / PSMDB、Neo4j + PG Apache AGE、OnlyOffice Docs `10.0` vs Workspace 12 双模式隔离、Rancher、Zabbix 7 与 `generic-app`（`sharedNetworkRef`/`externalRefs`/显式 `file\|dir` 挂载） | **v1.2（封板）** |
| [`deep-design/R1-新增15项组件全景调研与插件归类评估.md`](./deep-design/R1-新增15项组件全景调研与插件归类评估.md) | **扩展调研 R1**：15 项新增组件 2026-11 实证评估与架构归类 | **v1（定稿）** |
| [`deep-design/R2-历史安装文档诊断与架构反向校准.md`](./deep-design/R2-历史安装文档诊断与架构反向校准.md) | **历史审计 R2**：逐行审计 `old-install.md` 21 项真实生产 Bug（含 `v4.3` 补充 `#17–#21`）与 8 项架构增强反哺 | **v1.1（定稿）** |
| `附-2026-11-25-调研增量与待合并清单.md` | 早期会话过程记录与调研增量（全部待合并项均已并入正式文档，留档追溯） | **已全部合并归档** |
| `spec/` | `osmatch.yaml`（7 族 42 规则）、`osmatch_golden.yaml`（71 样本）、`pkgmap.yaml`（64 逻辑包 + 6 冲突项）、`gen_pkgmap.py`、`gen_os_docs.py` | **rev 2026.11.26-1（校验全通过）** |
| `research/` | `repo_scan.py`、`pkgresolve.py`、`probe.py`、`probe64.py`、`gen_03a.py` 及原始 JSON 快照 | 参考 |

## 版本记录

| 包版本 | 日期 | 步骤 | 变更 |
| --- | --- | --- | --- |
| **`v4.5-complete`** | **2026-11-26** | **全仓 45 篇系统架构 / 安全供应链 / 插件 / 测试全域深度审计与修复** | 三路并发深度红队审计并集中修复 70+ 项真实工程缺陷与跨文档漂移：安全与正确性（禁 `O=system:masters` 改可吊销 `kinstall:cluster-admin`、`D3` `MediaResolver` 离线逃逸与 `KI-SRC-0005`、`D2` Fencing `FOR SHARE` 竞态与 SSE 并发保序、`G1` 幂等主键/`MaxBytesReader`/`WithoutCancel`/全列审计与独立 SM3 链/`BuildExecutor` 装饰链、`18` `sdk.DottedVersionLess`、`09` `authentication_policy`、`P3` `idempotence+acks=all`）；架构与契约（`OpState` 7 态、15 Task、24 表 `Store`、`media_policies` 三级作用域、`kinstall.io/v1alpha1`、SSE `operation.done` 无 `id:` 行、完整错误码表）；插件与测试（麒麟 V10 SP3=64K / V11=4K、`G3` 权威 M01–M12、`14` ADR-064 `FI-01–FI-15`、20+480=500 节点、22 模块、`04`–`17`/`P1`–`P10` 全面对齐） |
| `v4.4-complete` | 2026-11-26 | 整体架构深度整理 + `DESIGN.md` v2.0 深度重构 | **v1.1（终审）** |
| `v4.3-complete` | 2026-11-26 | **全附件再分析与 5 项深水区生产优化** | 完整比对 `v2-S4.zip`（零遗漏）与 `old-install.md` 2216 行，闭环落地 5 项实战优化：① `D2 §5` 锁定 `docker.service` `LimitNOFILE=1048576`（防 systemd 252+ `infinity` 假死）+ 独立数据盘 `data-root` 门禁；② `D2 §5.1` `compose.sync` 强制显式 `file\|dir` 挂载预建（防缺失单文件被 Docker 建为目录）+ 禁 `chmod 777`/默认 `privileged: true`/`docker cp`；③ `D2 §4` 新增 `Stack.externalRefs` 只读云/存量服务引用 + `urlEncode` 双态脱敏函数；④ `P4 §3` / `P5 §3` 新增声明式最小权限 `bootstrapDatabases`（禁 `*.*` 与 `WITH GRANT OPTION`）；⑤ `P10 §4–§5` 拆分 OnlyOffice Docs `10.0` vs 遗留 Workspace 12 隔离守卫，增强 `generic-app` `sharedNetworkRef` 与显式挂载；同步对齐 `R2` Attu 端口（`19001`）与 `DEV_TASK_CHECKLIST.md` DoD |
| `v4.2-complete` | 2026-11-26 | 专门前端 UI 样式与组件视觉基准 `DESIGN.md` | **v1.3（封板）** |
| `v4.1-complete` | 2026-11-26 | 全仓深度一致性复审 + 独立 126 项开发任务 Checklist | **v1.3（封板）** |
| `v4-all-complete` | 2026-11-26 | 全三阶段终版封板（G2 + `WEB_DESIGN.md` + G3 + P1–P10） | **v1.3（封板）** |
| v3-G1 | 2026-11-26 | 深度设计 G1（平台 1/3 · 服务层高可用与全仓 Go 代码方案） | 新增 `deep-design/G1`：CLI/Server 同源 Facade、OpenAPI 3.1 + 无丢帧多副本 SSE、OIDC/Argon2/SQL+Go 双重防自审审批、防分叉审计链、HA 滚动升级与全仓 Go 1.27 后端代码落地方案 |
| v3-D3.1 | 2026-11-26 | D3 v1.1 补强：本地 / 在线 / OBS 桶统一三元介质引擎（ADR-077） | 新增 `D3 §7` 与 ADR-077，同步联动 `D2` `binary.install`/`image.load` |
| v3-D2-R2 | 2026-11-26 | R2 历史文档诊断 + D2 反向校准 | 新增 `deep-design/R2`（审计 `old-install.md` 16 项真实生产坑，提出 7 项架构增强）；回写 `D2` 补齐 `subnets_json` 与 `compose.sync` 合法 systemd unit + 镜像重写契约 |
| v3-D2 | 2026-11-26 | 深度设计 D2（底座 2/3）+ R1（15 项扩展调研） | 新增 `deep-design/D2` 与 `deep-design/R1` |
| v3-D1 | 2026-11-26 | 深度设计 D1（底座 1/3） | 确立“先底座（D1–D3）→ 再平台（G1–G3）→ 再各插件（P1–P10）”路线；新增 `deep-design/D1` |
| v3-complete | 2026-11-26 | S16 架构评估与全栈补强 | 12 维红队评估 + 8 大盲区修补；新增 `15`、`16`、`17`、`18`；登记 ADR-066～076 |
| v2-full | 2026-11-26 | S6–S15 全量完成 | 改写 `02 v2`、新建 `05`–`14`，登记 ADR-034～065 并定稿 ADR-012 / 013，新增 `00 §9` M0–M5 开发 Checklist |
| v2-S1～S5 | 2026-11-25～26 | S1–S5 完成 | `00`/`01` 升 v2、48 组 OS 实测与 `spec/`、`03` 全文定稿、`04` 全文定稿 |

## 格式说明

全部正式文档（`00`–`18`、`03-A`、`ARCHITECTURE.md`、`DESIGN.md`、`WEB_DESIGN.md`、`DEV_TASK_CHECKLIST.md`、`deep-design/*.md`、`README.md`、`文档中心.md`）均采用标准 GitHub Flavored Markdown（GFM 表格 + Mermaid），不含 Notion 专有标签（如 `<callout>`）。
