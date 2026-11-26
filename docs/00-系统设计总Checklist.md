# 00 系统设计总 Checklist（会话交接与终版封板总控）

> **版本**：**v4.5-complete**（S1–S16 全量基线 + 三阶段深度设计封板 + `ARCHITECTURE.md` + `DESIGN.md` v2.0 + **全仓 45 篇系统架构/安全供应链/插件/测试深度一致性终审修复**，2026-11-26）。版本号、上游生命周期与 Go / 前端依赖均于 2026-11-25 / 26 实查核验，研发启动前请按 BOM revision 复核。
>
> **新会话 / 研发团队从这里开始**
>
> 1. 架构与深度设计三阶段已 **100% 全量封板并通过深度一致性与全附件实战复审**，请直接按 **[`DEV_TASK_CHECKLIST.md`](./DEV_TASK_CHECKLIST.md)**（126 项原子开发任务）切入 M0 编码。
> 2. 架构口径以 **[`ARCHITECTURE.md`](./ARCHITECTURE.md)** 为准（28 包规范图、依赖规则、风险台账）；后端从 `deep-design/G1 §7`（全仓 Go 1.27 工程蓝图）+ `D1–D3` + `05` 起步；前端视觉样式与组件基准以 **[`DESIGN.md`](./DESIGN.md)** 为唯一标准，业务交互与工程架构对照 **[`WEB_DESIGN.md`](./WEB_DESIGN.md)**；插件研发直接对照 `deep-design/P1–P10` 与 `18 §3` 样板代码。
> 3. 工程迭代过程中如有重要架构取舍，继续登记至 §6 ADR；OS 矩阵改动须经 `python3 spec/gen_os_docs.py --check` 校验。

**状态图例**：✅ 已完成（已写入文档） · 🟡 有初稿或方案已定，待细化 · ⬜ 未开始 · ⏸ 等上游发布 · 🔬 需要实机或 VM 才能完成

## 1. 文档清单与状态

| 编号 | 文件 | 版本 | 状态 | 说明 / 覆盖内容 |
| --- | --- | --- | --- | --- |
| 00 | `00-系统设计总Checklist.md` | **v4.5-complete** | ✅ 当前版本 | 本页：132 项设计项（✅ 124）、ADR-001～077（0 待定）、交叉引用、交接记录、全三阶段深度设计封板台账与 §9 里程碑索引 |
| **ARCH** | **`ARCHITECTURE.md`** | **v1.1（唯一架构口径）** | **✅ 已封板** | **整体架构全景与一致性审计**：7 条硬约束与质量属性排序、C4 上下文、六层架构与依赖规则、28 个 `internal/` 规范包图 + depguard 矩阵、Operation / Stack / 介质解析 / SSE 运行时流、三形态部署拓扑、24 表 ER、横切关注点、P1–P10 矩阵、SLO、ADR-001～077 十大主题簇、F-01～F-10 跨文档漂移修复、RA-01～RA-12 风险台账、O-01～O-12 优化项（挂载既有 `T-*`）与架构适应度函数 |
| **DEV** | **`DEV_TASK_CHECKLIST.md`** | **v1.3（可执行总表）** | **✅ 已封板** | **全栈工程开发任务 Checklist（126 项原子任务 `T-M0-01`～`T-M5-25`）**：按 M0–M5 与 5 大并行泳道（`BASE`/`PLAT`/`WEB`/`PLUG`/`QA`）拆解至具体 Go/Vue 包、24 表迁移、本地/OBS 介质引擎、P1–P10 插件、DoD 验收与 10 条 CI 红线 |
| **DS** | **`DESIGN.md`** | **v2.0（UI 视觉基准 · 深度重构）** | **✅ 已封板** | **专门的前端 UI 设计系统与组件样式基准**：工业精密控制室视觉语言、三层 Token（原色 → 语义 → 组件）与全量实测 WCAG 2.2 对比度表、环境色 / 色盲友好可视化色板 / ANSI 16 色、8 级排版与 CJK 混排、双密度栅格与 Z-Index、可落盘 `tokens.css` / `element-overrides.css`（`html[data-theme]` + `@supports color-mix()` 回退）/ CSP 合规主题引导、12 大基础 `Ki*` + 8 大基础设施领域组件与状态矩阵、6 类页面模板与 `useKiFormat()`、A11y、CI 门禁与 16 条 PR 红线 |
| **UI** | **`WEB_DESIGN.md`** | **v1.3（工程与交互）** | **✅ 已封板** | **Web 控制台信息架构、交互流与前端工程规范**（关闭 B9 & 定稿 ADR-023）：IA 与 11 核心页面（含本地/在线/OBS 桶介质源中心）、`x-kinstall-ui` 动态表单协议、`@codemirror/merge` 3-Way Diff、`@xterm/xterm` + `fetch` SSE 分屏终端、Vue Flow 2.0 `xyflow` Stack 画布、Vue 3.5.45 / `@hey-api/openapi-ts` 与 `go:embed` 零 CDN 打包 |
| 01 | `01-调研Checklist.md` | v3.1（全量定稿） | ✅ 当前版本 | 共 116 项调研项（**✅ 108 · 🟡 4 · ⬜ 3 · ⏸ 1**，已关闭 `R-PLAT-05`），覆盖全部 S1–S16 与前端调研结论（扩展 15 项与历史审计见 `deep-design/R1–R2`） |
| 02 | `02-平台架构与插件体系.md` | v2.2（S6/S16/U/V） | ✅ 全文定稿（§1–§14） | 目标/角色/规模/术语、分层架构、插件体系、三元目标与 Docker Task、`Stack`、3-Way Diff、任务引擎与锁（ADR-035）、执行器（ADR-020）、平台 HA（ADR-021）、模板与 `expr`（ADR-036）、多集群管理 |
| 03 | `03-CMDB与主机初始化.md` | v2（S3） | ✅ 全文定稿（§1–§9） | 数据模型、Facts、OS 支持矩阵与适配器（`spec/` 生成通过 `--check`）、初始化模块与 Profile、预检验收与漂移、CMDB 功能、§9 等保主机加固（ADR-018） |
| 03-A | `03-A-OS软件源实测矩阵.md` | v1.1（S2） | ✅ 已完成 | 48 组 OS 官方源实测数据与包内实测（os-release、内核、cgroup、页大小、冲突包、KYSEC） |
| 04 | `04-Kubernetes安装规划与设计-重建版.md` | v2（S5） | ✅ 全文定稿（§1–§16） | 版本/BOM `2026.11.3`、拓扑与混合架构、双栈网络与 nftables、CRI/CNI/CSI（SELinuxMount、厂商接入）、PKI、本地代理 HA、etcd/kubelet 预留、Addon（Helm v4 SDK）、生命周期与审计 |
| 05 | `05-开发文档.md` | v1.2（S6–S7/S16/U/V） | ✅ 全文定稿（§1–§9） | 工程结构与 depguard、Go 1.27 与 35+ 依赖锁定表、核心 Go 接口（含 Docker/Compose Task）、CLI/REST API（OpenAPI+SSE）、RBAC/审批/批量导入/GitOps/通知、错误码/slog/i18n/OpenBao 信封加密、测试分层与 kubeadm 基线/容器契约、goreleaser/CI/升级、插件指南与 M0 拆解 |
| 06 | `06-中间件插件通用规范.md` | v1+（S8/S16） | ✅ 全文定稿 | 中间件统一目录/unit 规范（ADR-040）、三元目标选型、Operator 总表（禁 Bitnami，ADR-042）、统一运维语义、监控与日志（弃 Promtail，ADR-043）、声明式插件包（OCI）与 gRPC 外部插件协议（ADR-044 / 069） |
| 07 | `07-插件-Valkey-Redis.md` | v1+（S8/S16） | ✅ 全文定稿 | 默认 Valkey 9.1/9.2（BSD）、Redis 8 可选（ADR-013 定稿）；EL8 glibc 2.28 + arm64 64K 页 jemalloc 治理；单节点/哨兵/Cluster × 二进制与 Docker |
| 08 | `08-插件-PostgreSQL.md` | v1（S9） | ✅ 全文定稿 | PG 18.7 默认 / 19.0 GA；Patroni 4.1.5 + etcd + PgBouncer + HAProxy/VIP；pgBackRest ≥ 2.60.0；CloudNativePG 1.30.1 + Barman Plugin（ADR-045～047） |
| 09 | `09-插件-MySQL.md` | v1（S10） | ✅ 全文定稿 | MySQL 8.4 LTS / 9.7 LTS（禁 8.0）；InnoDB Cluster（MGR + Router + Shell）；Percona XtraBackup 8.4/9.7；Percona Operator（PS）1.3.0 / MOCO（ADR-048～050） |
| 10 | `10-插件-ES-OpenSearch.md` | v1（S11） | ✅ 全文定稿 | 默认 OpenSearch 3.10.0（Apache-2.0）/ Operator 3.0 GA；ES 9.5/8.19 + ECK 3.5 可选且不入公共离线包；31 GiB 堆上限与滚动升级（ADR-051～053） |
| 11 | `11-插件-对象存储.md` | v1（S12） | ✅ 全文定稿 | 正式放弃归档的 MinIO CE（ADR-012 定稿）；分层选型：默认 SeaweedFS 4.54（Apache-2.0）、大规模 Ceph RGW、可选 RustFS 1.0.6 / Garage v2.5.0（ADR-054～056） |
| 12 | `12-离线与供应链.md` | v1（S13） | ✅ 全文定稿 | 端到端信任链、BOM 独立发布（ADR-057）、OS 包闭包流水线、内置 HTTP/Zot v2.1.23、Bundle 裁剪与增量补丁、Syft+Grype 主扫描与隔离 Trivy（ADR-058）、全平台许可证合规表（ADR-059） |
| 13 | `13-安全合规.md` | v1（S14） | ✅ 全文定稿 | STRIDE 威胁模型、等保 2.0 三级与密评四层面全映射、国密外围边界与纯 Go SM4-GCM（`emmansun/gmsm`，ADR-060/061）、审计哈希链（ADR-062）、`compliance report` 证据包 |
| 14 | `14-测试与验收.md` | v1（S15） | ✅ 全文定稿 | 12 组正交 VM 矩阵与三层 arm64 资源（ADR-063）、Ubuntu 26.04 `sudo-rs`/`uutils` 回归、商业 OS 采样、nftables 专项、15 项故障注入（ADR-064）、1.35→1.38 升级回归、`hydrophone` + `kwok` 500 节点模拟（ADR-065）与验收报告模板 |
| 15 | `15-插件-ELK日志全栈.md` | v1（S16） | ✅ 全文定稿 | OpenSearch 全栈（默认，Apache-2.0）与 ELK 9.5.7（可选）：Fluent Bit/Filebeat + Kafka + Data Prepper 2.16/Logstash + Dashboards/Kibana（ADR-071/072） |
| 16 | `16-插件-Kafka.md` | v1（S16） | ✅ 全文定稿 | Apache Kafka 4.3.2 / 4.4.0 KRaft（无 ZooKeeper）、KIP-858 JBOD、随包 JDK 21、Strimzi 1.3.2、Cruise Control、Kafbat UI（ADR-073/074） |
| 17 | `17-插件-SkyWalking-APM.md` | v1（S16） | ✅ 全文定稿 | Apache SkyWalking 11.1.0 + Horizon UI 1.1.0 + BanyanDB 0.11.2（默认）/ OpenSearch + Satellite 1.4.0 + 多语言离线 Agent + SWCK 0.11.0（ADR-075/076） |
| 18 | `18-架构深度评估与端到端开发落地指南.md` | v1（S16） | ✅ 全文定稿 | 12 维红队架构评审与 8 大盲区修补（三元目标/Stack/64K与glibc构建/Go插件分工/3-Way Diff/诚实回滚）+ **Valkey/Redis 全拓扑×二进制与Docker端到端代码样板**（ADR-066～070） |
| D1 | `deep-design/D1-底座-执行器与安全凭据及OS适配深度设计.md` | **v1.3（底座 1/3）** | ✅ 已封板 | Go 六边形代码方案 + 9 大初始设计模式 + SSH/SFTP 双层限流连接池、Async Detached、`sudo-rs#1668`/`pam_faillock`、SFTP 并发写+原子替换、跨块脱敏、OpenBao 2.7.2 `derived=true`+`context`、1-RTT POSIX Facts |
| D2 | `deep-design/D2-底座-存储引擎任务编排与全量内置Task深度设计.md` | **v1.5（底座 2/3）** | ✅ 已封板 | SQLite/PG 13 张底座 DDL（前缀 ULID `VARCHAR(48)`、`subnets_json`、`state_json`、`operations.state` CHECK、与 G1 统一防分叉审计链）、Fencing Token 租约抢占、两级防死锁锁、3-Way Diff、`Stack` DAG（含 `externalRefs` 只读云/存量引用 + `urlEncode` 双态脱敏）、15 Tasks（含 `LimitNOFILE=1048576`、`data-root` 门禁、`compose.sync` `file\|dir` 预建与安全禁令）与 Docker 29 防火墙守卫 |
| D3 | `deep-design/D3-底座-离线供应链跨架构构建与分发体系深度设计.md` | **v1.2（底座 3/3）** | ✅ 已封板 | 零出网 `sigstore-go`+SM2、`oras-go/v3`、全局 `registryMirror` 与私仓 CA、EL8 `glibc 2.28`+64K jemalloc 构建、`runtime/{jdk,maven,node}`、Jenkins 2.580/APOC 闭包、HTTP Range 限速、Zot v2.1.23 边缘缓存、**§7 本地/在线/华为云 OBS·S3 三元介质引擎（ADR-077）**、OS 闭包与双扫描/许可总闸 |
| G1 | `deep-design/G1-平台-服务层高可用审批审计与Go后端完整代码方案.md` | **v1.2（平台 1/3）** | ✅ 已封板 | CLI/Server 同源 Facade、OpenAPI 3.1 + `@hey-api/openapi-ts` + RFC 9457 + Idempotency/ETag、无丢帧多副本 SSE、OIDC/Argon2/Token + SQL/Go 双重防自审、防分叉审计哈希链、**§6.3 全量控制面扩展 DDL（合 D2 共 24 表实测通过）**、**§7 全仓 Go 1.27 后端代码方案（依赖与 `05 §2.2` 100% 对齐）** |
| **G2** | `deep-design/G2-平台-多集群生态集成与等保密评实操深度设计.md` | **v1.1（平台 2/3）** | **✅ 已封板** | 多集群短期 kubeconfig 签发与 RBAC 映射、外部 CMDB 双向同步与两段式 GitOps 流水线、多通道通知引擎与防风暴聚合、等保 2.0 三级与密评证据包生成实操（含外围 Tongsuo TLCP 双证书网关配置） |
| **G3** | `deep-design/G3-平台-多架构测试平台故障注入与500节点压测SOP深度设计.md` | **v1.1（平台 3/3）** | **✅ 已封板** | 三层多架构测试平台（GitHub arm64 / 鲲鹏按需 / 物理 KVM）、Ubuntu 26.04 `sudo-rs`/`uutils` 容器契约、15 项自动化故障注入脚本、20 真机 + 480 `kwok` 500 节点混合压测 SOP（明确控制面 vs 数据面实测边界） |
| **P1** | `deep-design/P1-插件-Kubernetes二进制生产级深度设计.md` | v1.0（核心 1/7） | **✅ 已封板** | K8s 1.35–1.38 纯二进制插件包结构、混合架构（`amd64`/`arm64-4K`/`arm64-64K`）与版本分支治理、本地代理 HA / Calico nftables / CSI `SELinuxMount` / PKI 轮换、BOM `2026.12.1` 补丁升级 SOP |
| **P2** | `deep-design/P2-插件-Valkey与Redis全拓扑多形态深度设计.md` | v1.0（核心 2/7） | **✅ 已封板** | Valkey 9.1/9.2 默认 + Redis 8 可选；单节点 / 主从+哨兵 / 3主3从 Cluster 环状反亲和 × 二进制/Docker/K8s；64K 页 jemalloc、COW/THP 治理、`create-only` 自改保护与无停机迁槽 |
| **P3** | `deep-design/P3-插件-大型ELK与OpenSearch加Kafka中间消费全链路深度设计.md` | v1.0（核心 3/7） | **✅ 已封板** | **大型 ELK / OpenSearch + Apache Kafka 4.x KRaft 中间消费全链路**：三档规模红线、Kafka `KIP-848` 协议消除 Rebalance 风暴、Data Prepper `2.16/2.17` `acknowledgments: true` + S3/OBS DLQ、分区/消费者/分片精准对齐、4 级端到端背压传导、Mapping 爆炸拦截、180 天 ISM 归档与一键编排完整 `Stack` YAML |
| **P4** | `deep-design/P4-插件-PostgreSQL高可用与PITR深度设计.md` | **v1.1（核心 4/7）** | **✅ 已封板** | PG 18.7/19.0 + Patroni 4.1.5 + etcd + PgBouncer + HAProxy/VIP；`pgBackRest >= 2.60.0` 全增量+WAL 归档与 PITR 闭环、声明式最小权限 `bootstrapDatabases`、脑裂防双主与 `pg_rewind`、CloudNativePG 1.30.1 与跨大版本平滑升级 |
| **P5** | `deep-design/P5-插件-MySQL-InnoDBCluster与物理热备深度设计.md` | **v1.1（核心 5/7）** | **✅ 已封板** | MySQL 8.4/9.7 LTS（禁 8.0）InnoDB Cluster（MGR 单主 + Router + Shell）、大事务流控与防脑裂、`Percona XtraBackup` 同版本物理热备 + binlog PITR、声明式最小权限 `bootstrapDatabases`、Percona Operator / MOCO |
| **P6** | `deep-design/P6-插件-对象存储SeaweedFS与Ceph多引擎深度设计.md` | v1.0（核心 6/7） | **✅ 已封板** | 淘汰 MinIO CE；SeaweedFS 4.54 默认（热副本 + 冷卷后台转 EC 10+4 纠删码、JBOD XFS）、大规模 Ceph RGW、RustFS 1.0.6 / Garage v2.5.0；统一 `OutputSchema` 与外部华为云 OBS 联邦备份闭环 |
| **P7** | `deep-design/P7-插件-SkyWalking-APM与BanyanDB深度设计.md` | v1.0（核心 7/7） | **✅ 已封板** | Apache SkyWalking 11.1 + Horizon UI + BanyanDB 0.11.2（默认）/ OpenSearch；大中小型分档、Satellite 1.4 负载均衡、Kafka 异步链路、原生尾部采样、离线多语言探针与 SQL 脱敏安全基线 |
| **P8** | `deep-design/P8-插件-DevOps交付栈Harbor-GitLab-Jenkins-SonarQube与Nginx深度设计.md` | v1.0（扩展 8/10） | **✅ 已封板** | Harbor 2.16（全量 arm64 + Valkey 64K 治理）、GitLab 19 / Forgejo、Jenkins 2.580 JCasC + 9 个 detached 插件闭包 + 多版本 JDK/Maven/Node 构建机、SonarQube 26.x、Reposilite / Verdaccio、Nginx L4/L7 网关 |
| **P9** | `deep-design/P9-插件-业务消息RocketMQ与事务Seata及ZooKeeper深度设计.md` | v1.0（扩展 9/10） | **✅ 已封板** | RocketMQ 5.5.1 DLedger Controller 自动选主 + 强制绑定 `brokerIP1`、Seata 2.6.0 原生 Raft 高可用 + NamingServer（免外部注册中心）、ZooKeeper 3.9.7 遗留组件专用插件 |
| **P10** | `deep-design/P10-插件-向量Milvus文档Mongo图Neo4j协同OnlyOffice与通用业务栈深度设计.md` | **v1.1（扩展 10/10）** | **✅ 已封板** | Milvus 2.6/3.0 + Attu（端口解耦防撞 `19001`）、MongoDB 8.0 / PSMDB（SSPL 客户侧构建）、Neo4j + PG Apache AGE 开源替代、OnlyOffice Docs `10.0` vs 遗留 Workspace 12 双模式隔离、Rancher Addon、Zabbix 7 与 `generic-app`（含 `sharedNetworkRef`/`externalRefs`/显式 `file\|dir` 挂载） |
| R1 | `deep-design/R1-新增15项组件全景调研与插件归类评估.md` | v1（扩展调研） | ✅ 已完成 | 15 项新增组件（MongoDB、Rancher/Reranker、OnlyOffice、ZooKeeper、JDK 二进制、Harbor 2.16、GitLab 19、Jenkins 2.580、Maven 3.10、Npm/Node 26/24、SonarQube 26.x、RocketMQ 5.5、Seata 2.6、Milvus 2.6/3.0、Neo4j）2026-11 实证评估与架构归类 |
| R2 | `deep-design/R2-历史安装文档诊断与架构反向校准.md` | **v1.1（历史审计）** | ✅ 已完成 | 逐行审计 `old-install.md` 21 项真实生产 Bug（含 `v4.3` 补充 `#17–#21`），反哺 8 项架构增强（全局 `registryMirror`、Docker IPAM/`LimitNOFILE=1048576`、`mounts` 类型预建、`Stack.externalRefs`+`urlEncode`、`bootstrapDatabases`、OnlyOffice 双模式、多 JDK 隔离、Nginx L4/L7、`generic-app`、Zabbix 7） |
| 附 | `附-2026-11-25-调研增量与待合并清单.md` | — | ✅ 全部合并归档 | 历史调研增量（第 1–6 节 #1～#6 已全部合入正式文档 00–05，保留作追溯参考） |
| — | `spec/` | rev 2026.11.26-1 | ✅ 已完成（S2） | `osmatch.yaml`（7 族 42 规则）、`osmatch_golden.yaml`（71 样本）、`pkgmap.yaml`（64 逻辑包 + 6 冲突项）、`gen_pkgmap.py`、`gen_os_docs.py`（`--check` 通过） |
| — | `research/` | — | 参考 | `repo_scan.py`、`pkgresolve.py`、`probe.py`、`probe64.py`、`gen_03a.py` 及原始 JSON 快照 |

## 2. 当前进度（截至 2026-11-26）

- **全量基线（S1–S16）+ 全部三阶段像素级深度设计（底座 D1–D3 → 平台 G1–G3 & 独立前端 `WEB_DESIGN.md` → 全量插件 P1–P10）+ 全仓深度复审与 126 项研发任务表 `DEV_TASK_CHECKLIST.md` 已 100% 终版封板！**
  - **基线层（S1–S16，2026-11-25～26）**：22 篇正式文档 `00`–`18`、`03-A`、`spec/`（`gen_os_docs.py --check` 退出码 0），登记 ADR-001～076。
  - **第一阶段 · 底座封板（D1–D3 + R1–R2，2026-11-26）**：执行器/凭据/9 大模式（`D1`）、SQLite/PG 13 表核心 DDL 与 Fencing 锁/3-Way Diff/`Stack`/15 Tasks（`D2 v1.3`）、零出网供应链与**本地/在线/华为云 OBS·S3 三元介质引擎 ADR-077**（`D3 v1.1`），以及 15 项扩展调研（`R1`）与历史交付审计校准（`R2`）。
  - **第二阶段 · 平台与前端封板（G1–G3 + `WEB_DESIGN.md`，2026-11-26）**：服务端 HA/OpenAPI 3.1/无丢帧 SSE/防自审四眼审批/防分叉审计链 + **全量 24 表可执行 DDL 与全仓 Go 1.27 后端代码方案**（`G1 v1.1`）；**独立交付完整前端 `WEB_DESIGN.md`**（正式关闭 **B9** / **`R-PLAT-05`** 与定稿 **ADR-023**）；多集群/CMDB/GitOps/通知/等保密评实操（`G2`）；三层多架构测试平台 + 15 项故障注入 + 500 节点混合压测 SOP（`G3`）。
  - **第三阶段 · 全量插件封板（P1–P7 核心 + P8–P10 扩展生态，2026-11-26）**：K8s 二进制（`P1`）、Valkey/Redis 全拓扑多形态（`P2`）、**大型 ELK/OpenSearch + Kafka 4.x KRaft 中间消费全链路**（`P3`）、PostgreSQL（`P4`）、MySQL InnoDB Cluster（`P5`）、SeaweedFS/Ceph 对象存储（`P6`）、SkyWalking 11.x + BanyanDB（`P7`）、DevOps 全栈与 Nginx（`P8`）、RocketMQ 5.5/Seata 2.6/ZooKeeper（`P9`）、Milvus/MongoDB/Neo4j/OnlyOffice/Rancher/Zabbix/`generic-app`（`P10`）。
- **剩余非设计项（工程实施期 / 上游事件 / 远期规划）**：
  - ⏸ **上游事件**：K8s 1.38 GA 复核（2026-12-16 之后，E14 / R-RT-07）；K8s 上游补丁 `1.37.3 / 1.36.7 / 1.35.11` 纳入下次 BOM revision `2026.12.1`（见 §7）。
  - 🔬 **实机 / VM 执行项**：D5（`sudo-rs`/`uutils` VM 回归）、G3（商业 OS ISO 依赖闭包实测）、R-NET-06（Calico+nftables VM 实测）、R-OS-07（UOS 授权真机采样）、J2/J5/J6（VM 矩阵/故障注入/升级回归执行）。
  - ⬜ **工程阶段 / 远期规划（4 项）**：E15（模板实物落地与 golden 测试，M1）、I3（故障诊断手册，M1 之后）、E21（GPU/NPU + AI Reranker）、E22（LoongArch64 组件）；另有 F7 远期 Nacos（Kafka 4.x 与 RocketMQ 5.5 均已完成）。
- **精确统计**：
  - 设计项共 **132** 项：**✅ 124 · 🟡 3 · ⬜ 4 · ⏸ 1**（设计阶段全量应闭项 100% 完成）。
  - 调研项共 **116** 项：**✅ 108 · 🟡 4 · ⬜ 3 · ⏸ 1**（`R-PLAT-05` 随 `WEB_DESIGN.md` 关闭；另 `deep-design/R1–R2` 完成 15 项扩展组件与历史交付实战校准）。
  - 架构决策共 **77** 项：**ADR-001～077 全量已采纳（0 项待定）**。
  - 全量正式文档共 **45** 篇：基线 `00–18` / `03-A` / `附` / `README` / `文档中心` + **`ARCHITECTURE.md`** + **`DESIGN.md`** + **`WEB_DESIGN.md`** + **`DEV_TASK_CHECKLIST.md`** + `deep-design/` 18 篇。

## 3. 分步交付计划（全三阶段 100% 封板）

| 步骤 | 主题 | 产出 | 主要设计项 | 主要调研项 | 状态 |
| --- | --- | --- | --- | --- | --- |
| S1–S15 | 基线设计全量定稿（总控/OS/初始化/K8s/开发/五大中间件/离线/合规/测试） | 00–14、03-A、`spec/` | A–L 核心项 | R-* 基础调研 | ✅ 2026-11-26 |
| S16 | 架构红队评估 + Docker 容器形态 + ELK / Kafka / SkyWalking + Valkey 代码样板 | 15–18、同步 02/05/06/07 | B19–B21、F7/F11/F12 | R-PLAT-10、R-MW-07～09 | ✅ 2026-11-26 |
| **D1** | **深度设计（底座 1/3）：代码开发方案与 9 大初始设计模式 + SSH/SFTP、OpenBao/SM4 与 1-RTT OS/Facts** | `deep-design/D1-*.md` | B5、C2–C3、C8、D13、H5、H7、K1–K2 | `sudo-rs#1668`、`restic#4209`、OpenBao `/rewrap` AAD | **✅ 2026-11-26** |
| **D2** | **深度设计（底座 2/3）：SQLite/PG 完整 DDL、任务引擎与 Fencing 锁、3-Way Diff、`Stack` 与全量 15 个内置 Task（含 Docker 29 防火墙与 IPAM 防撞）** | `deep-design/D2-*.md` | B3–B4、B16、B19–B21 | `modernc.org/sqlite` `_txlock`、Docker 29 `nftables` | **✅ 2026-11-26** |
| **R1–R2** | **扩展生态 15 项调研（R1）+ `old-install.md` 历史文档诊断与 7 项反向架构校准（R2）** | `deep-design/R1–R2` | 底座 Runtime/IPAM/Mirror + P8–P10 归位 | 15 项 2026-11 实查 + 16 项真实生产坑诊断 | **✅ 2026-11-26** |
| **D3** | **深度设计（底座 3/3 · 底座封板）：离线信任链（`sigstore-go` + SM2 + `oras-go/v3`）、全局 `registryMirror`、EL8+64K 构建、底座 JDK/Maven/Node Runtime、离线插件闭包、HTTP Range 限速、Zot v2.1.23 边缘缓存与本地/在线/华为云 OBS·S3 三元介质引擎（ADR-077）** | `deep-design/D3-*.md` | B6、G1–G8、A5 | `kyverno#18525`、Jenkins 2.580、OBS/S3 `WhenRequired` | **✅ 2026-11-26** |
| **G1** | **深度设计（平台 1/3）：平台服务与多副本 HA（CLI/Server 同源 Facade、OpenAPI 3.1 + SSE + PG `LISTEN/NOTIFY`、OIDC/Argon2/四眼审批、防分叉审计链、滚动升级 `StateMigrator`）+ 全仓 Go 后端完整代码落地方案** | `deep-design/G1-*.md` | B8、B10–B12、B17、H2、K1–K3/K6 | `oapi-codegen v2` 3.1、Go SSE `ResponseController` | **✅ 2026-11-26** |
| **G2 + UI** | **深度设计（平台 2/3）：多集群管理、CMDB 同步 / GitOps / 通知、等保 2.0 三级与密评实操 + 独立交付完整前端 `WEB_DESIGN.md`**（正式关闭 **B9** 与定稿 **ADR-023**） | `deep-design/G2-*.md` + **`WEB_DESIGN.md`** | **B9**、B14–B15、E19、H3–H4 | Vue 3.5.45 / Vue Flow 2.0 / 禁用 `CVE-2026-108551` | **✅ 2026-11-26** |
| **G3** | **深度设计（平台 3/3 · 平台层封板）：多架构 KVM + 鲲鹏测试平台自动化编排、Ubuntu 26.04 容器契约、15 项故障注入脚本与 500 节点 `kwok` 压测 SOP** | `deep-design/G3-*.md` | J1–J7 | `kwok` 控制面与真机数据面实测边界 | **✅ 2026-11-26** |
| **P1–P7** | **深度设计（核心插件 1–7）：K8s（P1）、Valkey/Redis（P2）、大型 ELK/OpenSearch+Kafka 4.x 中间消费全链路（P3）、PG（P4）、MySQL（P5）、对象存储（P6）、SkyWalking（P7）** | `deep-design/P1–P7` | E1–E20、F1–F12 | Kafka 4.x `KIP-848`、Data Prepper 2.16/2.17 `acknowledgments` | **✅ 2026-11-26** |
| **P8–P10** | **深度设计（扩展生态插件 8–10 · 全量封板）：DevOps 交付栈（P8：Harbor 2.16/GitLab/Jenkins/SonarQube/Reposilite/Verdaccio/Nginx）、业务消息与事务（P9：RocketMQ 5.5/Seata 2.6/ZK）、AI 向量/文档/图与协同及通用业务栈（P10：Milvus+Attu/MongoDB/Neo4j/AGE/OnlyOffice/Rancher/Zabbix 7/`generic-app`）** | `deep-design/P8–P10` | R1–R2 全部扩展组件工程落地 | 全组件许可红黄绿隔离与离线坑规避 | **✅ 2026-11-26** |
| — | K8s 1.38 GA 复核（2026-12-16 之后）与 BOM `2026.12.1` 补丁刷新 | 04 增补 1.38 BOM | E14 | R-K8S-05（复核）、R-RT-07 | ⏸ |

## 4. 下一步：正式转入 **M0–M5 工程编码与实机验证阶段**

1. **后端初始化（第 1–2 周 M0 垂直切片）**：严格按 `deep-design/G1 §7` 初始化 Go 1.27 单仓、锁定 `go.mod` 与 `.golangci.yml`（`depguard` 强制插件仅依赖 `pkg/sdk/v1alpha1`），结合 `D1`（SSH/SFTP + OpenBao + 1-RTT Facts）、`D2`（SQLite/PG DDL + Fencing 锁 + 15 Tasks）和 `D3 §7`（本地/在线/OBS 三元 `MediaResolver`）打通首个垂直切片。
2. **契约先行与前端初始化**：冻结 `api/openapi.yaml`（OpenAPI 3.1），后端用 `oapi-codegen v2.9.0` 生成 Chi Strict Server，前端严格按根目录 **`WEB_DESIGN.md`** 使用 `@hey-api/openapi-ts` + Vue `3.5.45` + Element Plus `2.14.2` + Vue Flow `2.0` 生成类型客户端并并行推进 UI 与 `go:embed` 零 CDN 内嵌构建。
3. **插件研发与测试门禁**：按 `00 §9` M0–M5 里程碑依次落地 `P1`（K8s）→ `P2/P4`（Valkey/PG）→ `P3/P5/P6/P7`（大型 ELK+Kafka / MySQL / SeaweedFS / SkyWalking）→ `P8–P10`（DevOps/RocketMQ/Milvus/`generic-app`），并接入 `G3` 三层多架构测试平台与 15 项故障注入门禁；12 月中旬跟进 K8s 1.38 GA 与 BOM `2026.12.1`。

## 5. 系统设计 Checklist（按领域）

“位置”指已有内容所在的文档章节；“步骤”指计划在哪一步完成或复核（见 §3）。

### A 产品与范围

| ID | 设计项 | 状态 | 位置 | 步骤 |
| --- | --- | --- | --- | --- |
| A1 | 定位与目标：插件化安装平台，K8s 为首个插件 | ✅ | 02 §1 | — |
| A2 | 非目标与边界 | ✅ | 02 §1 | — |
| A3 | 用户角色与使用场景细化（管理员 / 运维 / 审计；在线 / 离线 / 信创 / 混合架构） | ✅ | 02 §1.3 | — |
| A4 | 支持矩阵：OS × 架构、K8s 版本 × 组件 | ✅ | 03 §3、`spec/osmatch.yaml`、04 §2 | — |
| A5 | 许可证合规：再分发义务（GPL / AGPL / SSPL / RSAL / ELv2 等）与上游状态跟踪 | ✅ | 06 §8、12 §9、`deep-design/D3 §6.3`（ADR-059） | — |
| A6 | 支持等级（P0 / P1 / P2）与 EOL 系统策略 | ✅ | 03 §3、§5.5 | — |
| A7 | 规模目标：纳管主机数、并发执行主机数、单集群节点数、离线包体积上限 | ✅ | 02 §1.4 | — |
| A8 | 术语表：实例、Spec、Plan、Operation、Phase、Task、Profile、BOM、Bundle | ✅ | 02 §1.5 | — |
| A9 | 版本兼容策略：平台版本 × SDK × 插件版本 × 产品/BOM 版本（semver 约束） | ✅ | 02 §3.6 | — |

### B 平台架构

| ID | 设计项 | 状态 | 位置 | 步骤 |
| --- | --- | --- | --- | --- |
| B1 | 分层架构与模块划分 | ✅ | 02 §2 | — |
| B2 | 插件模型：类型、清单、接口、Task 库、交付形态 | ✅ | 02 §3 | — |
| B3 | 声明式 Spec / Status 与对账 | ✅ | 02 §4 | — |
| B4 | 任务引擎：DAG、幂等、plan、续装、并发、锁 | ✅ | 02 §5 | — |
| B5 | 执行器：SSH、sudo、跳板机、文件传输（ADR-020 / 025 / 030） | ✅ | 02 §6、05 §3.2、`deep-design/D1` | — |
| B6 | 制品与离线包 | ✅ | 02 §7、04 §11、`deep-design/D3` | — |
| B7 | 声明式插件包格式、gRPC 外部插件协议、插件签名与仓库（ADR-044） | ✅ | 02 §3.5–§3.6、06 §6–§7 | — |
| B8 | API：REST `/api/v1`、202 + Operation、SSE、RFC 9457 错误、Idempotency-Key、ETag（ADR-021） | ✅ | 05 §4.2 | — |
| B9 | Web UI 信息架构与交互原型（技术栈见 ADR-023，独立交付 `WEB_DESIGN.md`） | ✅ | `WEB_DESIGN.md` | — |
| B10 | 部署形态：CLI 单机 / Server（REST + SSE，一期无 gRPC，ADR-021） | ✅ | 02 §8 | — |
| B11 | 平台自身高可用：多副本 + PostgreSQL + 任务租约锁 + SSE 追读 | ✅ | 02 §5.3、§8 | — |
| B12 | 多租户与权限：项目 → 环境 → 资源；角色；OIDC / 本地 / Token；审批门（ADR-037） | ✅ | 02 §9、05 §4.3 | — |
| B13 | 配置与模板引擎：变量优先级、按版本选模板、Secret 引用、白名单函数、expr（ADR-036） | ✅ | 02 §13、05 §3.9 | — |
| B14 | Spec 导入导出与 GitOps：YAML、diff、版本化 | ✅ | 05 §4.5 | — |
| B15 | 事件与通知：Webhook、邮件、企业微信 / 钉钉 / 飞书 | ✅ | 05 §4.6 | — |
| B16 | 并发与锁：主机级锁、实例级锁、分批滚动、维护窗口（ADR-035） | ✅ | 02 §5.4、05 §3.3 | — |
| B17 | 平台自身升级：数据库前滚迁移（goose）、自动备份、插件状态 schema 迁移 | ✅ | 02 §3.6、05 §7.5 | — |
| B18 | 国际化（中 / 英，`go-i18n/v2`，ADR-038） | ✅ | 05 §5.4 | — |
| B19 | 宿主机容器化部署目标 `host-container`（Docker Engine 29.10 + Compose v5.8 + systemd 托管 + `DOCKER-USER` 防火墙治理，ADR-066） | ✅ | 02 §3.4、05 §3.4、06 §2、18 §2.1 | — |
| B20 | `Stack` 多组件复合应用栈编排（`dependsOn` 依赖 DAG 与跨实例端点/CA/凭据引用，ADR-067） | ✅ | 02 §4、05 §1.1、15 §6、18 §2.2 | — |
| B21 | Day-2 外部漂移三向合并（3-Way Diff：`lastApplied` vs `liveObserved` vs `desired`）与 `create-only` 保护（ADR-070） | ✅ | 02 §3.4 / §4、18 §2.5 / §3.4 | — |

### C CMDB 主机纳管

| ID | 设计项 | 状态 | 位置 | 步骤 |
| --- | --- | --- | --- | --- |
| C1 | 数据模型：主机、凭据、Facts、分组、实例角色 | ✅ | 03 §1 | — |
| C2 | Facts 采集与归一化（已补：页大小、x86-64 级别、安全模块、sudo 实现、cgroup 版本） | ✅ | 03 §2 | — |
| C3 | OS 适配矩阵与适配器接口（含 48 组实测与 `spec/` 规范） | ✅ | 03 §3–§4、03-A、`spec/` | — |
| C4 | 主机状态机 | ✅ | 03 §1.3 | — |
| C5 | 批量导入（Excel / CSV / YAML）与外部 CMDB 同步接口（Pull / Push） | ✅ | 03 §8、05 §4.4 | — |
| C6 | 配置漂移检测与合规报告 | ✅ | 03 §7.3、§8.4 | — |
| C7 | 主机自动发现（网段扫描）与 SSH 公钥批量分发 | ✅ | 03 §8.2 | — |
| C8 | 凭据管理：密码 / 密钥 / sudo / 跳板机、加密存储、轮换、按项目隔离 | ✅ | 02 §9、03 §1.4 | — |
| C9 | 标签、分组与故障域（机房 / 机架 / 可用区），供反亲和调度使用 | ✅ | 03 §1.5 | — |
| C10 | Facts 定时刷新与主机巡检（失联告警） | ✅ | 03 §8.4 | — |
| C11 | 主机与实例的关系、容量视图（端口、目录、资源占用） | ✅ | 03 §8.5 | — |
| C12 | 主机退役与清理：解除纳管、残留检查 | ✅ | 03 §5.7、04 §13.6 | — |
| C13 | OS 识别规则数据化：`osmatch.yaml`，以实测 os-release 为准（ADR-024 / ADR-029） | ✅ | 03 §4.2、`spec/osmatch.yaml` | — |

### D 主机初始化与调优

| ID | 设计项 | 状态 | 位置 | 步骤 |
| --- | --- | --- | --- | --- |
| D1 | 初始化模块清单与执行顺序 | ✅ | 03 §5.1 | — |
| D2 | Profile：base / k8s-node / database（database-pg、database-mysql）/ search / cache / object-storage | ✅ | 03 §6.1–§6.2 | — |
| D3 | 内核升级策略与重启编排 | ✅ | 03 §5.2 | — |
| D4 | sysctl / limits / 内核模块 / THP / journald / chrony 模板 | ✅ | 03 §6.2–§6.9 | — |
| D5 | 各 OS 命令与包名逐项实测：48 组官方源索引已测并通过 2880 项回放，VM 回归与 `sudo-rs`/`uutils` 用例已定稿，待实机执行 🔬 | 🟡 | 03-A、`spec/pkgmap.yaml`、14 §3 | M0–M1（VM 实测 🔬） |
| D6 | 磁盘初始化：分区（`parted`/`sfdisk`）、LVM、XFS `ftype=1`、挂载参数、I/O 调度 | ✅ | 03 §6.10 | — |
| D7 | 等保主机加固可选模块 harden（ADR-018） | ✅ | 03 §9 | — |
| D8 | 初始化前备份与回滚 | ✅ | 03 §5.4 | — |
| D9 | 软件源管理：离线源、EOL 系统的 vault / archive 源、源优先级、与发行版容器包的冲突（ADR-026） | ✅ | 03 §4.4、§5.5 | — |
| D10 | 主机名与 `/etc/hosts` 管理（DNS-1123、全局唯一、克隆机 `machine-id` 修复） | ✅ | 03 §5.6.1、04 §2.3 | — |
| D11 | 网络配置校验：多网卡与主 IP 选择、bond、MTU、默认路由、NetworkManager 忽略 CNI 网卡、netplan | ✅ | 03 §6.7 | — |
| D12 | DNS：systemd-resolved 与 kubelet `resolvConf` | ✅ | 03 §6.7、04 §4.5 | — |
| D13 | 执行账号与 sudo：sudoers 片段、sudo-rs / uutils 兼容、requiretty、口令过期与 faillock 豁免（ADR-030） | ✅ | 03 §5.6.2、§9.3 | — |
| D14 | 安全软件冲突：麒麟 KYSEC、统信安全机制、fapolicyd、SELinux、EDR / 杀毒软件、auditd 规则（ADR-027） | ✅ | 03 §4.6、§5.6.3、03-A §8 | — |
| D15 | 时区、locale、logrotate | ✅ | 03 §5.6.4、§6.6、§6.9 | — |
| D16 | 资源预留与大页联动（主机侧动态大页与容量扣减口径已定；K8s 分档见 E17） | ✅ | 03 §6.2、§8.5、04 §10.7 | — |
| D17 | 初始化验收：每个模块的 verify 与报告 | ✅ | 03 §7.2–§7.3 | — |
| D18 | cgroup v2 切换：grub 参数、重启编排、回滚、存量服务副作用 | ✅ | 03 §5.2.2、04 §2.3 | — |

### E Kubernetes 插件

| ID | 设计项 | 状态 | 位置 | 步骤 |
| --- | --- | --- | --- | --- |
| E1 | 版本基线、BOM、破坏性变更与逐版本模板分支（etcd v3.7.4、ADR-010、1.35–1.38 差异） | ✅ | 04 §2 | — |
| E2 | 拓扑：All-in-One / 单主 / 三主 / 五主 / 外置 etcd | ✅ | 04 §3 | — |
| E3 | 网络规划：地址、双栈、MTU、kube-proxy 模式（含 Calico nftables 方案）、DNS、入口 | ✅ | 04 §4 | — |
| E4 | CRI：containerd 默认（v4 配置）；Docker 29 + cri-dockerd 兼容（ADR-015、R-RT-08）；CRI-O 二期 | ✅ | 04 §5 | — |
| E5 | CNI：Calico 默认，Cilium / Flannel / Kube-OVN 可选；双栈仅新建集群（ADR-031） | ✅ | 04 §6 | —（S5 复核定稿） |
| E6 | CSI：local-path / NFS / Longhorn / Rook-Ceph / OpenEBS / 厂商 CSI；主机依赖、SELinuxMount（ADR-032） | ✅ | 04 §7 | —（S5 复核定稿） |
| E7 | PKI 证书与 TLS Bootstrap | ✅ | 04 §8 | —（S5 复核定稿） |
| E8 | 高可用入口：节点本地代理（健康检查、熔断、热加载、故障注入）+ VIP / 外部 LB | ✅ | 04 §9 | —（S5 复核定稿） |
| E9 | 组件部署规范与参数模板（含 etcd 磁盘 / RTT 调优、双栈参数） | ✅ | 04 §10 | —（S5 复核定稿） |
| E10 | 离线制品与 BOM（`images.lock` digest 锁定） | ✅ | 04 §11 | —（S5 复核定稿） |
| E11 | Addon（BOM `2026.11.3` 冻结、Helm v4 SDK、二进制控制面监控接入） | ✅ | 04 §12 | —（S5 复核定稿） |
| E12 | 安装流程与生命周期：扩缩容、升级（含 SELinuxMount / 双栈预检）、证书、备份恢复、重置 | ✅ | 04 §13 | —（S5 复核定稿） |
| E13 | 安全基线 | ✅ | 04 §14.1 | —（S5 复核定稿） |
| E14 | K8s 1.38 GA 复核与 1.38 BOM | ⏸ | 2026-12-16 之后 | 1.38 复核 |
| E15 | 各版本模板实物落地与 golden 测试 | ⬜ | — | M1 |
| E16 | 跨机房部署与 etcd 延迟要求（RTT 分档；> 100 ms 阻断，每地一套） | ✅ | 04 §3.3、§10.3 | — |
| E17 | 节点资源预留与驱逐阈值（按节点规格计算，ADR-033） | ✅ | 04 §10.7 | — |
| E18 | 审计策略模板、日志落盘与转发（集中留存 ≥ 180 天） | ✅ | 04 §14.2 | — |
| E19 | 多集群管理：一个平台管理多个集群，kubeconfig 短期签发与权限映射 | ✅ | 02 §14 | — |
| E20 | 混合架构集群（amd64 + arm64）：多架构 OCI Index 校验、节点标签与防误调度、64K 页治理 | ✅ | 04 §3.4 | — |
| E21 | GPU / NPU 节点（NVIDIA、昇腾）：驱动与设备插件 | ⬜ | — | 待定 |
| E22 | LoongArch64：上游没有官方 K8s 二进制，评估自编或厂商版本 | ⬜ | — | 待定 |
| E23 | 接管非 kinstall 安装的集群：一期不做（已决策） | ✅ | 04 §2.1 | — |

### F 中间件插件

| ID | 设计项 | 状态 | 位置 | 步骤 |
| --- | --- | --- | --- | --- |
| F1 | 通用能力规范：拓扑、角色、Profile、数据盘、凭据、证书、备份恢复、监控、升级（ADR-040） | ✅ | 02 §3.7、06 §1 | — |
| F2 | Elasticsearch / OpenSearch（默认 OpenSearch，ES 可选，ADR-051～053） | ✅ | 10 | — |
| F3 | 对象存储：放弃归档 MinIO CE，默认 SeaweedFS + 大规模 Ceph RGW + 可选 RustFS/Garage（ADR-012 / 054～056） | ✅ | 11 | — |
| F4 | MySQL（8.4 LTS / 9.7 LTS，InnoDB Cluster + XtraBackup，ADR-048～050） | ✅ | 09 | — |
| F5 | PostgreSQL（18.7 / 19.0，Patroni + etcd + pgBackRest / CNPG，ADR-045～047） | ✅ | 08 | — |
| F6 | Redis / Valkey（默认 Valkey 9.1/9.2，Redis 8 可选，ADR-013） | ✅ | 07 | — |
| F7 | 消息队列：Apache Kafka 4.x KRaft（16，ADR-073/074）与 RocketMQ 5.5.1（`P9`）均已定稿；仅 Nacos 留远期 | 🟡 | 16、`P9` | M5（Nacos 远期） |
| F8 | host 部署与 K8s Operator 部署的选择规则与 Operator 选型表（禁用 Bitnami，ADR-041 / 042） | ✅ | 06 §2–§3 | — |
| F9 | 统一运维操作：备份 / 恢复 / 扩缩容 / 升级 / 参数变更 / 账号管理 / 故障切换 | ✅ | 06 §4 | — |
| F10 | 中间件监控与日志采集：exporter 版本、告警规则模板、Fluent Bit / Alloy（ADR-043） | ✅ | 06 §5、07–11 §10 | — |
| F11 | ELK / OpenSearch 日志分析全栈（Fluent Bit / Filebeat + Kafka + Data Prepper / Logstash + OpenSearch / ES + Dashboards / Kibana，ADR-071/072） | ✅ | 15 | — |
| F12 | APM 分布式链路追踪：Apache SkyWalking 11.1 + Horizon UI + BanyanDB 0.11 + Satellite + 多语言离线 Agent + SWCK（ADR-075/076） | ✅ | 17 | — |

### G 离线与供应链

| ID | 设计项 | 状态 | 位置 | 步骤 |
| --- | --- | --- | --- | --- |
| G1 | 离线包结构与多架构 | ✅ | 02 §7、04 §11 | — |
| G2 | 签名与校验信任链 | ✅ | 04 §11 | — |
| G3 | OS 依赖包闭包构建流水线（容器闭包与商业 ISO 方案已定稿，待流水线实测 🔬） | 🟡 | 12 §4 | M1（流水线实测 🔬） |
| G4 | SBOM（Syft）与双扫描器漏洞门禁（Grype 主 + 隔离 Trivy，ADR-058） | ✅ | 12 §8 | — |
| G5 | 内置制品服务：HTTP 文件服务、Zot v2.1.23 镜像仓库、OS 软件源、HA 外部仓库 | ✅ | 02 §7、12 §5 | — |
| G6 | Bundle 构建器：按 BOM × OS × 架构裁剪、层去重、zstd 分卷 | ✅ | 04 §11、12 §6 | — |
| G7 | 增量包 / 补丁包（基于 manifest diff） | ✅ | 04 §11、12 §7 | — |
| G8 | BOM 目录独立发布（BOM revision 与平台版本解耦，ADR-057） | ✅ | 04 §2.2、12 §2 | — |

### H 安全

| ID | 设计项 | 状态 | 位置 | 步骤 |
| --- | --- | --- | --- | --- |
| H1 | K8s 集群安全基线 | ✅ | 04 §14 | — |
| H2 | 平台自身安全：凭据、密钥、审计 | ✅ | 02 §9 | — |
| H3 | 等保 2.0（GB/T 22239-2019 三级）与密评（GB/T 39786-2021）映射及证据包 | ✅ | 03 §9、13 §3–§4、§7 | — |
| H4 | 国密支持边界（外围 TLCP/Tongsuo + 可选纯 Go SM4-GCM `emmansun/gmsm`，ADR-060 / 061） | ✅ | 04 §8.5、13 §4 | — |
| H5 | SSH 主机密钥校验策略：首次信任 / 预置 known_hosts / 指纹确认 | ✅ | 02 §9、03 §8.3 | — |
| H6 | 执行账号最小权限：评估 sudo 命令白名单的可行性（确立双层权限模型，ADR-030） | ✅ | 03 §5.6.2 | — |
| H7 | 密钥信封加密：KEK（文件 / env / OpenBao 2.7 Transit / KMS）/ DEK 轮换（ADR-039） | ✅ | 02 §9、05 §3.7、§5.5 | — |

### I 可观测与运维

| ID | 设计项 | 状态 | 位置 | 步骤 |
| --- | --- | --- | --- | --- |
| I1 | 平台日志、指标、审计、诊断包 | ✅ | 02 §10 | — |
| I2 | 组件监控告警模板（K8s、etcd 与五大中间件全覆盖） | ✅ | 04 §12.4、06 §5、07–11 §10 | — |
| I3 | 故障诊断手册 | ⬜ | — | M1 之后 |
| I4 | 操作报告模板：计划、实际变更、耗时、失败原因（Markdown / JSON） | ✅ | 02 §10、05 §4.7 | — |

### J 测试

| ID | 设计项 | 状态 | 位置 | 步骤 |
| --- | --- | --- | --- | --- |
| J1 | 测试分层与矩阵（L1–L7 完整分工与准出红线） | ✅ | 05 §6、14 §1 | — |
| J2 | VM 测试矩阵（12 组正交矩阵，amd64 + arm64）与三层资源方案（ADR-063；实机执行 🔬） | ✅ | 14 §2 | —（执行 🔬） |
| J3 | 验收报告模板（Markdown + JSON） | ✅ | 14 §10 | — |
| J4 | OS 适配器契约测试：`testcontainers-go` + 官方容器镜像与 systemd 方案 | ✅ | 05 §6.3 | — |
| J5 | 故障注入与高可用演练（15 项自动化用例，ADR-064；实机执行 🔬） | ✅ | 04 §9.3、14 §6 | —（执行 🔬） |
| J6 | 升级回归：1.35 → 1.36 → 1.37 → 1.38 连续性验证方案（实机执行 🔬） | ✅ | 14 §7 | —（执行 🔬） |
| J7 | 模板 golden 测试与 `kubeadm v1beta4` 基线语义对比 | ✅ | 04 §2.4、05 §6.2 | — |

### K 工程与交付

| ID | 设计项 | 状态 | 位置 | 步骤 |
| --- | --- | --- | --- | --- |
| K1 | 代码结构与依赖规则（单仓目录树、插件只依赖 `pkg/sdk`，depguard 强制，ADR-022） | ✅ | 05 §1 | — |
| K2 | 编码规范、错误码 `KI-<域>-<4位>`、slog 字段与两层脱敏、Git 流程 | ✅ | 05 §5 | — |
| K3 | 构建发布：goreleaser v2、`CGO_ENABLED=0`（5 平台）、Syft SBOM、cosign | ✅ | 05 §7.2 | — |
| K4 | 文档体系与插件开发指南（用户/运维/API/插件指南规范与自动生成） | ✅ | 05 §8 | — |
| K5 | CI 流水线：lint、单测、golden、容器契约、arm64 runner、安全扫描 | ✅ | 05 §7.3 | — |
| K6 | Go 工具链与依赖策略（Go 1.27.4；35+ 核心模块锁定版本与准入/淘汰清单） | ✅ | 05 §2 | — |

### L 计划与风险

| ID | 设计项 | 状态 | 位置 | 步骤 |
| --- | --- | --- | --- | --- |
| L1 | 路线图 M0–M5 | ✅ | 02 §11 | — |
| L2 | 风险清单（已补：arm64 64K 页、KYSEC、sudo-rs 等 OS 适配风险） | ✅ | 02 §12、03 §4.8、04 §15 | — |
| L3 | M0 两周逐日任务拆解与退出标准 | ✅ | 05 §9.1–§9.2 | — |
| L4 | M1–M5 人力与测试环境需求估算 | ✅ | 05 §9.3–§9.4 | — |

## 6. 设计决策（ADR）

| ID | 决策 | 状态 | 文档 |
| --- | --- | --- | --- |
| ADR-001 | 核心为 Go 单二进制；CLI 为主，Server / Web UI 可选 | 已采纳 | 02 |
| ADR-002 | 插件三种交付形态：内置 Go 插件（一期）、声明式插件包（二期）、gRPC 外部插件（三期）；不用 Go 原生 .so 插件 | 已采纳 | 02 §3.5 |
| ADR-003 | 插件只生成任务，不直接操作 SSH；幂等、日志、重试由平台 Task 库统一实现 | 已采纳 | 02 §3.4 |
| ADR-004 | 远程执行默认无 Agent（SSH）；规模大时节点从制品 HTTP 服务拉取 | 已采纳 | 02 §6 |
| ADR-005 | 默认 SQLite（纯 Go 驱动，便于交叉编译），Server 模式用 PostgreSQL | 已采纳（驱动已定稿见 ADR-034） | 02、05 §2.2 |
| ADR-006 | 主机初始化 = 初始化模块 + Profile 组合；所有改动先 plan，可备份回滚 | 已采纳 | 03 |
| ADR-007 | K8s 纯二进制 + systemd，不依赖 kubeadm；用同版本 kubeadm 生成的参数做基线对比 | 已采纳 | 04 §1 |
| ADR-008 | K8s 默认 containerd 2.3 LTS + Calico + kube-proxy nftables；条件不满足时降级 iptables；新集群不用 ipvs | 已采纳 | 04 §4–§6 |
| ADR-009 | HA：每个节点内置 Go 实现的本地 apiserver 代理（127.0.0.1:8443），集群外访问用 VIP 或外部 LB | 已采纳 | 04 §9 |
| ADR-010 | K8s 1.35+ 只支持 cgroup v2；cgroup v1 仅作为显式开启的 legacy 模式（`failCgroupV1: false`，1.38 仍可用），随上游移除而下线 | 已采纳（v2 修订措辞） | 03、04 §2.3 |
| ADR-011 | 单主集群按“可扩展为 HA”设计：证书预留 VIP / 域名，所有客户端走本地代理 | 已采纳 | 04 §1 |
| ADR-012 | 因 MinIO 社区版已于 2026-04 归档、公共镜像下线，正式放弃 MinIO CE；采用分层选型：通用默认 SeaweedFS 4.54（Apache-2.0），大规模/K8s 用 Ceph RGW，可选 RustFS 1.0.6 / Garage v2.5.0 | 已采纳（S12 定稿） | 11 §1–§2 |
| ADR-013 | 缓存/键值插件默认 Valkey（BSD-3-Clause，推荐 9.1.4 / 9.2.0），Redis 8.x 仅作显式可选；arm64 64K 页主机按 BOM 匹配专用 jemalloc 64K 构建 | 已采纳（S8 定稿） | 07 §1–§2 |
| ADR-014 | Calico 数据面与 kube-proxy 模式保持一致：内核 ≥ 5.13 且 nft ≥ 1.0.1 时都用 nftables，否则都用 iptables | 已采纳（方案已定，R-NET-06 S15 实测） | 04 §4.4、§6.2 |
| ADR-015 | Docker 运行时 = Docker Engine 29.x + cri-dockerd ≥ 0.4.8（推荐 0.4.11），只用 Docker 自带的独立 containerd（不开实验性内嵌），在 daemon.json 显式声明存储后端（默认 containerd image store，按 1.5–2× 规划盘容量）。cri-dockerd 已实现 CRI RuntimeConfig，1.38 预期可用，GA 后实测转为正式支持 | 已采纳（S4 修订） | 04 §5.3、§15 |
| ADR-016 | 南北向入口默认 Gateway API + Envoy Gateway（Ingress NGINX 已退役）；可选 Calico Ingress Gateway / Cilium Gateway API；裸金属 LoadBalancer 可选 MetalLB / kube-vip | 已采纳（v2 补登记） | 04 §4.6 |
| ADR-017 | kubelet serving 证书：安装和扩容时由平台审批 CSR，集群内默认部署 kubelet-csr-approver 处理轮换；只批准 CN 为 `system:node:<nodeName>`、SAN 与 CMDB 一致的请求 | 已采纳 | 04 §8.4 |
| ADR-018 | 等保加固是可选的 harden 模块，默认关闭，并与 K8s 和执行器兼容：执行账号单独的口令策略、faillock 下限流重试、临时目录用 `/var/lib/kinstall/tmp`、Task 显式设置文件权限 | 已采纳 | 03 §9 |
| ADR-019 | Web 控制台 Addon 用 Headlamp（Kubernetes Dashboard 已归档） | 已采纳 | 04 §12 |
| ADR-020 | 执行器基于 `x/crypto/ssh` + `pkg/sftp` 自研薄封装；参考 k0sproject/rig 的设计，不直接依赖 | 已采纳 | 02 §6、05 §3.2 |
| ADR-021 | API 采用 OpenAPI 契约优先的 REST（`/api/v1`）；长任务返回 202 + Operation；事件和日志走 SSE；一期不提供 gRPC | 已采纳 | 02 §8、05 §4.2 |
| ADR-022 | 插件只能依赖 `pkg/sdk`，不得引用 `internal/*`，由 golangci-lint depguard 强制 | 已采纳 | 02 §3.3、05 §1.2 |
| ADR-023 | Web UI 锁定 Vue `3.5.45` + TypeScript + Vite + Element Plus `2.14.2` + Pinia `3.0.4` + `@hey-api/openapi-ts`（正式禁用存在 CVSS 9.8 RCE `CVE-2026-108551` 的 `openapi-typescript-codegen`）+ `@xterm/xterm` + `@codemirror/merge` + Vue Flow `2.0`（`xyflow`），零外网 CDN 静态打包经 `go:embed` 内嵌单二进制 | 已采纳（G2 定稿） | `WEB_DESIGN.md`、01 R-PLAT-05 |
| ADR-024 | OS 识别规则数据化（`osmatch.yaml`），以官方 release 包中实测的 os-release 为准，不写死在代码里 | 已采纳 | 03 §4.2、`spec/osmatch.yaml` |
| ADR-025 | 目标主机零依赖：Facts 采集和 Task 只依赖 POSIX sh + coreutils，不依赖 Python；兼容 sudo-rs 和 Rust coreutils | 已采纳（v2 新增） | 03 §2、§4.3、§5.6.2、05 |
| ADR-026 | 只用 BOM 中的上游二进制，不用发行版自带的 containerd / docker / runc / kubernetes 包；预检发现冲突时提示卸载并写入包管理器排斥锁（exclude / Pin -1 / addlock） | 已采纳（v2 新增） | 03 §4.3、§5.5.3、03-A §7、04 §5 |
| ADR-027 | 安全执行控制（KYSEC、fapolicyd、SELinux、noexec 等）只检测与预检阻断、经管理员显式授权后按文件加白并记录在 `InstalledFile`，绝不自动关闭安全机制 | 已采纳（S2 新增） | 03 §4.6、§5.6.3、03-A §8 |
| ADR-028 | 组件 BOM 记录各构建支持的页大小，安装前按主机 `PageSize`（`getconf PAGESIZE`）预检；arm64 64K 页主机阻断 4K 编译的 jemalloc 组件，同集群混用 4K / 64K 自动打标隔离并禁止跨页大小直拷 bbolt 数据目录 | 已采纳（S2 新增） | 03 §4.5、§6.2、04 §2.2 / §3.4、03-A §5 |
| ADR-029 | OS 支持矩阵、识别规则与逻辑包映射以 `spec/`（`osmatch.yaml`、`pkgmap.yaml`、`osmatch_golden.yaml`）为唯一来源，03 中对应表格由 `spec/gen_os_docs.py` 生成并通过 `--check` 进 CI；`revision` 可独立于平台二进制发布 | 已采纳（S2 新增） | 03 §3、§4.2–§4.3、`spec/` |
| ADR-030 | 执行账号不采用全量 sudo 命令白名单（对安装类操作无法形成真实 root 边界且与 `sudo-rs` 禁用局部参数通配符冲突），采用双层权限模型：Mode A 默认专用账号 + SSH 公钥 + 部署机来源 IP 限制 + `NOPASSWD: ALL` + 双侧审计；Mode B 只读巡检账号仅白名单放行 root 拥有的无参采集脚本（`""`），变更期按窗口临时提权回收 | 已采纳（S3 新增） | 03 §5.6.2 |
| ADR-031 | K8s 双栈只能在新建集群时开启（IPv4 为主栈）；一期不支持单栈原地迁双栈，禁止双栈转单栈与主栈族互换；迁移走“新建双栈集群 + 业务迁移” | 已采纳（S5 新增） | 04 §4.2、§6.7 |
| ADR-032 | CSI 的 SELinux 策略：每个 CSI Extension 声明 `selinuxMount`（块设备类可 true、共享文件类必须 false）；1.36→1.37 升级在 SELinux enforcing 集群做卷标签冲突审计，冲突负载用 `seLinuxChangePolicy: Recursive`；厂商 CSI 以“厂商 Extension 包”接入并经 K8s 版本兼容性门禁 | 已采纳（S5 新增） | 04 §7.3、§7.4 |
| ADR-033 | kubelet 资源预留口径：输入为“内存 − 大页”；kubeReserved 用云厂商公式（CPU 分档、内存 `min(255Mi+11Mi×maxPods, 25%)`），systemReserved 按规格单列（harden / EDR 加档），evictionHard 内存 `clamp(2%, 200Mi, 1Gi)`，只对 Pod 强制 Node Allocatable；K8s 节点 `OSReserveMB` 取三者之和与 03 D16 对齐 | 已采纳（S5 新增） | 04 §10.7、03 §6.2 |
| ADR-034 | 数据访问层：SQLite 驱动采用纯 Go `modernc.org/sqlite` v1.63.0（WAL + 单写连接），PostgreSQL 用 `pgx/v5` v5.13.0，查询层用 `uptrace/bun` v1.3.0，迁移用嵌入式 `goose` v3.30.2（只前滚）；否决 gorm（`glebarez/sqlite` 停更），`ncruces/go-sqlite3` 仅作 `database/sql` 后备选 | 已采纳（S6 新增） | 02 §2、05 §2.2 / §3.8 |
| ADR-035 | 任务引擎自研 + 数据库持久化状态，支持 10s/30s 租约崩溃接管、PG `FOR UPDATE SKIP LOCKED` 抢占、先实例锁后主机按 ID 排序批量锁、维护窗口与断点续装；否决 river（仅 PG）与 Temporal（过重） | 已采纳（S6 新增） | 02 §5、05 §3.3 |
| ADR-036 | 配置模板采用 `text/template`（`missingkey=error`）+ 自维护白名单函数表（不直连停更的 sprig，禁用非确定性函数保 golden 确定性），按 semver 范围选目录；条件与校验采用 `expr` v1.18.1（通过 `Evaluator` 接口保留切换 CEL 可能） | 已采纳（S6 新增） | 02 §13、05 §3.9 |
| ADR-037 | Server 模式认证采用 OIDC SSO（生产推荐）+ 本地账号（Argon2id，离线现场）+ 带作用域短效 API Token；高风险操作启用四眼审批门（禁止自审批） | 已采纳（S7 新增） | 05 §4.3 |
| ADR-038 | 国际化采用 `go-i18n/v2`（v2.7.0）+ `x/text`，嵌入 `zh-CN`/`en-US` 目录按错误码键翻译，结构化日志键与错误码本身不翻译 | 已采纳（S7 新增） | 05 §5.4 |
| ADR-039 | 平台密钥信封加密默认 AES-256-GCM（密评可选纯 Go SM4-GCM），每条记录独立 DEK + AAD 绑定；KEK 单机默认 `0600` 密钥环文件，生产 HA 推荐 OpenBao 2.7.x Transit Secrets Engine（MPL-2.0，替代 BSL Vault），支持 `rewrap-secrets` 在线轮换 | 已采纳（S7 新增） | 05 §5.5 |
| ADR-040 | 中间件 `host` 部署统一规范：二进制 `/opt/kinstall/bin/<product>/<version>/`、配置 `/etc/kinstall/<product>/<instance>/`、数据 `/data/<product>/<instance>/`、systemd unit `kinstall-<product>-<instance>.service`、每实例独立 CA | 已采纳（S8 新增） | 06 §1.1 |
| ADR-041 | 中间件插件支持 `host` 与 `kubernetes` 双目标；M4 优先交付 `host` 目标，`kubernetes` 目标只通过成熟开源 Operator（经 Helm v4 SDK）交付，不手写裸 StatefulSet HA | 已采纳（S8 新增） | 06 §2 |
| ADR-042 | 因 Broadcom 于 2025-08 将 Bitnami 公共镜像迁入冻结的 `bitnamilegacy` 并转向收费订阅，kinstall 全平台禁止依赖 Bitnami 公共 Chart 或镜像 | 已采纳（S8 新增） | 06 §3 |
| ADR-043 | 因 Promtail 已于 2026-03-02 正式 EOL，主机日志采集模板采用 Fluent Bit（默认，Apache-2.0）或 Grafana Alloy v1.22.0 / Vector，不提供 Promtail 与 Filebeat | 已采纳（S8 新增） | 06 §5.2 |
| ADR-044 | 二期声明式插件包采用 OCI Artifact（`oras-go/v3`）+ cosign 签名分发，步骤 DSL 仅允许内置 Task 库、白名单模板函数与预编译 `expr`；三期外部插件采用 `hashicorp/go-plugin` v1.8.0，仅返回 `PipelineSpec` 绝不触碰 SSH 与 Secret 明文 | 已采纳（S8 新增） | 06 §6–§7 |
| ADR-045 | PostgreSQL `host` 模式 HA 默认 Patroni 4.1.5 + etcd v3.7.4 DCS + HAProxy/VIP（可选 PgBouncer 1.24.x），否决 repmgr 与 pg_auto_failover；`kubernetes` 模式默认 CloudNativePG 1.30.1 + Barman Cloud Plugin 0.15.1 | 已采纳（S9 新增） | 08 §3–§4 |
| ADR-046 | PostgreSQL `host` 模式备份默认 pgBackRest ≥ 2.60.0（强制 ≥ 2.59.3 修复加密子密钥缺陷），并强制开启 `initdb --data-checksums` 配合 2.60 恢复页校验和拦截 | 已采纳（S9 新增） | 08 §9 |
| ADR-047 | PostgreSQL 制品在有 PGDG 官方源的 OS 使用上游 PGDG 包，在无 PGDG 源的国产 OS 使用平台容器编译的同版本包，不使用发行版陈旧 PG 包 | 已采纳（S9 新增） | 08 §1 |
| ADR-048 | MySQL `host` 模式 HA 默认 InnoDB Cluster（Group Replication 单主 + MySQL Router + MySQL Shell），可选无损半同步主从，否决 MHA 与 Orchestrator 默认；`kubernetes` 模式默认 Percona Operator（PS）1.3.0（备选 Oracle Operator / Cybozu MOCO v0.38.0） | 已采纳（S10 新增） | 09 §1、§3–§4 |
| ADR-049 | MySQL 物理热备默认同主版本 Percona XtraBackup（8.4 / 9.7 严格绑定）+ binlog 归档实现 PITR，逻辑迁移使用 MySQL Shell `dumpInstance/loadDump` | 已采纳（S10 新增） | 09 §1、§9 |
| ADR-050 | MySQL `host` 模式默认采用官方 Generic Linux `tar.xz`（glibc 2.28，amd64/arm64），等保审计场景可选 Percona Server 启用开源 `audit_log_filter` | 已采纳（S10 新增） | 09 §1、§8 |
| ADR-051 | 搜索与日志引擎插件默认 OpenSearch（3.10.0，Apache-2.0，Linux Foundation，免费内置安全/告警/ISM），Elasticsearch（9.5/8.19）仅显式可选；K8s 模式分别采用 OpenSearch K8s Operator 3.0 GA 与 ECK 3.5.0 | 已采纳（S11 新增） | 10 §1、§4 |
| ADR-052 | 遵循许可证合规边界，kinstall 公共离线包仅内置 OpenSearch 制品；选用 Elasticsearch（ELv2/SSPL/AGPLv3）时由客户侧按 `artifacts.yaml` 自行拉取并完成合规确认 | 已采纳（S11 新增） | 10 §1 |
| ADR-053 | 搜索节点 JVM 堆锁定 `min(50% 宿主机内存, 31 GiB)` 保障 CompressedOops，强制 `bootstrap.memory_lock: true` 与 `vm.max_map_count = 1048576`，禁止 NFS 作为 `path.data` | 已采纳（S11 新增） | 10 §5、§7 |
| ADR-054 | 对象存储采用“单插件多引擎”抽象（默认 SeaweedFS 4.54，大规模/K8s 用 Ceph RGW，可选 RustFS 1.0.6 / Garage v2.5.0），统一向上层插件暴露 S3 端点与按桶隔离 IAM 凭据 | 已采纳（S12 新增） | 11 §1–§2 |
| ADR-055 | SeaweedFS 生产默认“热写跨故障域副本 + 冷卷后台转 Reed-Solomon 10+4 纠删码”，物理机采用 JBOD 独立 XFS 盘，禁止硬件 RAID 叠软件纠删码 | 已采纳（S12 新增） | 11 §6、§8 |
| ADR-056 | 对象存储强制全链路 TLS（S3 HTTPS + 内部 gRPC mTLS），默认开启 SSE-S3 加密并为备份桶支持 Object Lock（WORM）防勒索篡改 | 已采纳（S12 新增） | 11 §8 |
| ADR-057 | BOM 目录按 `<product>/bom-<ver>-rev.<YYYY.MM.N>.yaml` 独立发布与签名，与平台/插件版本解耦；安全补丁通过新 BOM revision + 增量补丁包完成 | 已采纳（S13 新增） | 12 §2、§7 |
| ADR-058 | 吸取 2026-03 Trivy `v0.69.4` 供应链投毒教训，SBOM 与漏洞门禁采用 Syft v1.57.2（SPDX）+ Grype v0.122.1（SBOM-first 主门禁），Trivy v0.76.2 仅在无出网隔离容器中按精确 SHA-256 固定做独立交叉复核；Critical 可达漏洞阻断发布 | 已采纳（S13 新增） | 12 §8 |
| ADR-059 | 许可证分级治理：主程序 Go 依赖仅允许宽松许可（`go-licenses` CI 禁止 GPL/AGPL/SSPL/ELv2）；GPLv2/LGPL 独立制品原样分发并在 `NOTICE` 附源码指引；ELv2/SSPL/RSAL/AGPLv3（Garage）/GPLv3 社区版（Neo4j）红灯组件不入公共离线包，客户侧按需构建 | 已采纳（S13/V 补强） | 12 §9、`D3 §6.3` |
| ADR-060 | 国密支持边界：K8s 与中间件内部坚持标准国际算法不魔改上游源码；密评通过外围国密 SSL 网关（TLCP/Tongsuo）、国密堡垒机、平台凭据库可选 SM4/HSM 及制品 SM2/SM3 双签满足 | 已采纳（S14 新增） | 13 §4 |
| ADR-061 | 纯 Go 国密实现采用持续维护的 `emmansun/gmsm` v0.45.4（兼容 `CGO_ENABLED=0`），禁止已停更 5 年的 `tjfoc/gmsm` v1.4.1；需 CGO 的厂商密码机 SDK 经外部插件隔离 | 已采纳（S14 新增） | 13 §4、§8 |
| ADR-062 | 平台 `audit_logs` 内置单调哈希链（SHA-256，密评并行 SM3）防篡改，提供 `audit verify` 离线校验与 RFC 5425 TLS syslog 实时外送，满足 ≥ 180 天留存 | 已采纳（S14 新增） | 13 §5 |
| ADR-063 | arm64 测试资源采用三层组合：PR 编译/契约用 GitHub Actions `ubuntu-24.04-arm`，每日夜间 E2E 用按需鲲鹏/倚天云主机，商业信创 OS 与 64K 页回归用固定物理宿主机 KVM 虚拟化 | 已采纳（S15 新增） | 14 §2.2 |
| ADR-064 | 每次 K8s 发版强制通过 15 项自动化故障注入与 `1.35→1.37`（及 1.38）逐级升级连续性验证（数据面请求成功率 ≥ 99.9%） | 已采纳（S15 新增） | 14 §6–§7 |
| ADR-065 | 500 节点规模验证采用“20 台真实 VM + `kwok v0.8.0` 模拟 480 节点 + `kube-burner v2.8.7` 压测”方案，真机集群采用 `hydrophone v0.8.0` 跑通完整 Conformance | 已采纳（S15 新增） | 14 §8 |
| ADR-066 | 部署目标升级为三元一等模型：`host-binary`（二进制/上游包 + systemd）、`host-container`（Docker Engine 29.10 + Compose v5.8 + systemd 托管 + 强制 `DOCKER-USER` 防火墙白名单）、`kubernetes`（开源 Operator + Helm v4 SDK）；内置 Task 新增 `docker.ensure` / `image.load` / `compose.sync` | 已采纳（S16 新增） | 02 §3.4、05 §3.4、06 §2、18 §2.1 |
| ADR-067 | 引入 `Stack`（多组件复合应用栈）资源，支持 `dependsOn` 依赖 DAG 与跨 `Instance` 端点/CA/凭据引用，支撑 ELK 日志全栈与 SkyWalking APM 联动部署 | 已采纳（S16 新增） | 02 §4、15 §6、18 §2.2 |
| ADR-068 | 跨架构/跨 OS 离线构建基线：C/C++ 组件（Valkey/PG/MySQL）以 EL8 `glibc 2.28` 构建，对 jemalloc 组件同时产出 `amd64`/`arm64-4k`/`arm64-64k(--with-lg-page=16)` 三份制品（旧 glibc 2.17 主机走 `host-container`）；JVM 组件（Kafka/SkyWalking）随 BOM 绑定 OpenJDK 21 LTS | 已采纳（S16 新增） | 07 §2.2、16 §1、18 §2.3 |
| ADR-069 | 插件实现分工：具复杂分布式共识与引导逻辑的核心产品（K8s、Valkey、PG、MySQL、OpenSearch/ELK、Kafka、SkyWalking、对象存储）采用强类型 Go 插件；YAML 声明式插件包专用于无复杂共识的轻量组件及新版本配置/BOM 热扩展 | 已采纳（S16 新增） | 02 §3.5、06 §6、18 §2.4 |
| ADR-070 | Planner 引入 Day-2 三向合并（3-Way Diff：`lastApplied` vs `liveObserved` vs `desired`），检测远端人工修改时阻断覆盖并提供 `--adopt`/`--force-override`；对 Sentinel 等自改配置文件支持 `file.sync` 的 `create-only` 语义 | 已采纳（S16 新增） | 02 §3.4 / §4、07 §3、18 §2.5 / §3.4 |
| ADR-071 | 日志全栈插件双路线统一 `Stack`：默认 OpenSearch 全栈（Fluent Bit + 可选 Kafka + Data Prepper 2.16 + OpenSearch 3.10 + Dashboards，全 Apache-2.0 可入公共离线包），可选 Elastic Stack / ELK 9.5.7（客户侧合规拉取） | 已采纳（S16 新增） | 15 §1 |
| ADR-072 | 日志全栈分三档（<50 GB 免缓冲直写、50–500 GB 标准清洗、>500 GB 强制 Kafka 削峰 + 冷热分层 + S3 180 天归档）；清洗层强制持久化磁盘队列，边缘采集器强制配资源硬限 | 已采纳（S16 新增） | 15 §3、§7 |
| ADR-073 | Kafka 插件全面采用 Apache Kafka 4.x KRaft 无 ZooKeeper 架构（默认 4.3.2、可选 4.4.0，启用 KIP-858 多盘 JBOD 与随包 OpenJDK 21）；JVM 堆锁定 6–8 GiB 留 75%–85% 内存给 OS Page Cache | 已采纳（S16 新增） | 16 §1、§7 |
| ADR-074 | Kafka K8s 模式默认 Strimzi ≥ 1.3.2（修复 `CVE-2026-55226`）；生态选用 Linux Foundation Cruise Control 与 Kafbat UI（Apache-2.0），监控采用 `jmx_exporter 1.7.0` + `kafka_exporter v1.11.0` 双轨 | 已采纳（S16 新增） | 16 §1、§10 |
| ADR-075 | SkyWalking APM 插件默认采用 APM 11.1.0 + Horizon UI 1.1.0 + 原生 BanyanDB 0.11.2 存储（Go 单二进制、资源约为 ES 1/5、原生尾部采样），OpenSearch 3.10 作可选；host 用 etcd 协调，K8s 用 SWCK 0.11.0 / Helm 5.0.0 | 已采纳（S16 新增） | 17 §1、§3–§4 |
| ADR-076 | SkyWalking 多语言探针（Java 9.8 / Go 0.7 / Python 1.3）纳入平台 BOM 与离线制品服务分发（K8s 支持 SWCK 自动注入），默认关闭 SQL 明文参数并强制 OAP 上报 Token 与 UI OIDC 反代鉴权 | 已采纳（S16 新增） | 17 §1、§8 |
| ADR-077 | 制品安装包介质统一支持 **本地（Local）/ 对象存储桶（华为云 OBS·阿里云 OSS·腾讯云 COS·S3·SeaweedFS）/ 在线（Online）** 三元优先级责任链与 `singleflight` 并发去重缓存；支持 `relay-cache`（默认零出网目标机）与 `direct-presigned`（同 VPC 15 分钟短效只读预签名直拉 + `RedactWriter` 自动脱敏）双交付通道；S3 驱动强制显式配置 `BaseEndpoint` + `RequestChecksumCalculationWhenRequired`/`ResponseChecksumValidationWhenRequired` 规避国产云 `aws-chunked` 兼容坑，并提供华为官方 Apache-2.0 `obs-native` 可选驱动 | 已采纳（D3 v1.1 新增） | `deep-design/D3 §7`、`D2 §5.1` |

## 7. 交叉引用修复登记

| 引用 / 问题 | 出现位置 | 处理 | 状态 |
| --- | --- | --- | --- |
| ADR-014、015、017、018、019 未定义 | 04 | 已在 §6 定义 | ✅ v2 |
| ADR-016 未定义 | 附页 | 已在 §6 定义 | ✅ v2 |
| R-RT-06、R-ADD-01、R-ADD-04 未定义 | 04 | 已在 01 v2 定义 | ✅ v2 |
| R-PLAT-07、R-PLAT-08 未定义 | 附页 | 已在 01 v2 定义 | ✅ v2 |
| 00 v1 引用了不存在的 05 §1 / §2 / §5 / §6 / §7 | 00 v1 | v2 改为指向附页第 6 节和步骤 S6 / S7 | ✅ v2 |
| oras-go 版本前后不一（01 v1 写 v3.0.0，附页写 v2.7.0） | 01、附页 §1 | 已核实：v3.0.0 于 2026-11-01 发布，模块路径改为 `github.com/oras-project/oras-go/v3`；v2 线最新为 v2.7.0 | ✅ v2（01 R-SUP-01） |
| ADR-027 / 028 / 029 仅在 03 / 03-A 中作为建议提及 | 03、03-A | 已在 §6 正式登记，03 §4.5 / §4.6 / §3 / §4.2–§4.3 同步 | ✅ S2 |
| 01 / 03 引用已作废的附页第 4 节旧 OS 实测表 | 01、03 | 改为引用 03-A 与 `spec/` | ✅ S2 |
| “03 §9” 不存在 | 04 §14 | S3 已新增 03 §9 等保主机加固（`harden` 模块） | ✅ S3 |
| “05 §6” 不存在 | 04 §2.4 | S7 已在 05 §6.2 落地模板 golden 与 kubeadm v1beta4 基线对比 | ✅ S7 |
| etcd 写为 v3.7.0 | 04 §2.2 | 已改为 `v3.7.4` | ✅ S4 |
| “legacy 模式 1.38 起不提供” | 04 §2.3 | 已按 ADR-010 改写（1.38 仍可用、随上游 KEP-5573 移除下线） | ✅ S4 |
| “Docker 29.8 起使用内嵌 containerd” | 04 §5.3 | 已更正为：29.7.0 起提供实验性内嵌 containerd，默认仍为独立进程 | ✅ S4 |
| “cri-dockerd 发布说明未提 RuntimeConfig，1.38 标为实验性” | 04 §5.3、§15 | 已按 ADR-015 修订（源码已实现 RuntimeConfig，1.38 预期可用、GA 后实测转正） | ✅ S4 |
| Addon 版本写“待定” | 04 §12 | 已填入 R-ADD-01～03 的版本并冻结 BOM `2026.11.3`（04 §12.1） | ✅ S5 |
| “R-PLAT-04 待调研” | 04 §12 | 已改写为 Helm v4 SDK 用法（04 §12.2） | ✅ S5 |
| cby-chen 仓库“最后更新于 2026-05-11” | 04 §16 | 已更正为最新提交 2026-11-11（无 1.37 文档），见 01 R-REF-01 | ✅ S5 |
| “K8s 分档推荐值在 S5 定稿” | 03 §6.2 D16 | 改为指向 04 §10.7（ADR-033） | ✅ S5 |
| “完整 CR 见 §6.2”、§2–§4 中多处“S5 §10.3 / R-NET-07 核实”待办措辞 | 04 §2.2–§4.4 | 随 §6 重编号改为 §6.3，并改为指向定稿章节 | ✅ S5 |
| Server 模式写作“REST / gRPC” | 02 §8 | 已按 ADR-021 改为一期只提供 REST `/api/v1` + SSE（无 gRPC） | ✅ S6 |
| K8s 上游补丁 `v1.37.3 / v1.36.7 / v1.35.11` 已发布（`04` BOM `2026.11.3` 为 `.2/.6/.10`） | 04 §2.2、12 §2 | 不改动已定稿 04，登记纳入下一 BOM revision `2026.12.1`（G8，ADR-057） | ⬜ BOM `2026.12.1` |

## 8. 交接记录

| 日期 | 会话 | 内容 | 产出 | 下一步 |
| --- | --- | --- | --- | --- |
| 2026-11-25 | 会话 A | 调研 K8s 1.35–1.38、containerd / etcd / CNI / CSI 版本与变更，评估 cby-chen/Kubernetes；改为插件化平台；补充 OS 适配与主机初始化调研 | 00–05 v1（导出包只含 00–03） | — |
| 2026-11-25 | 会话 B | 重建 04；上游版本核对；36 组 OS 官方源实测；1.38、Docker 29、cri-dockerd、etcd、Helm 4、PG / MySQL 调优、等保基线调研 | 04 v1（重建）、附页、`research/` | 合并附页 |
| 2026-11-25 | 会话 C（S1） | 00 / 01 整理为 v2：合并附页第 1–5 节结论，补登 ADR-014～026，扩充设计项和调研项，制定分步计划；复核 cby-chen 仓库、1.38 预告原文、oras-go v3 模块路径 | 00 v2、01 v2、文档包 v2 | S2 CMDB 与 OS 适配 |
| 2026-11-25 | 会话 D（S2 进行中） | 补全 OS 软件源扫描（48 组）；按命令文件 / Provides 反查逻辑包；包内实测 systemd 编译默认 cgroup 与 arm64 页大小（含可选 64K 内核）；调研 openEuler（按 SP）、Alinux 3 / 4、OL9、Leap 15.6 / 16、OC9、麒麟、UOS、CTyunOS、BCLinux 的生命周期与识别信息 | 03-A v1、附页 7.3 / 7.4、`research/` 新脚本与数据（包 v2-S2-wip2） | 按附页 7.3：改写 03 §3 / §4.2 / §4.3，更新 00 / 01，交付 S2 正式包 |
| 2026-11-26 | 会话 E（S2 进行中） | 编写 `spec/osmatch.yaml`（7 族、42 条规则）、`osmatch_golden.yaml`（45 个实测 + 26 个文档 / 构造样本）、`pkgmap.yaml` 与 `gen_pkgmap.py`、`gen_os_docs.py`（校验、golden、基线核对、pkgmap 回放、生成 03 表格）；03 §4.7 补 Q-20～Q-27；03-A 更正为 v1.1 | `spec/`、包 v2-S2-wip3 / wip4 | 按附页 7.3：改写 03、更新 00 / 01，交付 S2 正式包 |
| 2026-11-26 | 会话 F（S2 完成） | 改写 03 v2（§1.2、§3、§4.1–§4.8、§8.5），以 `spec/gen_os_docs.py --write` 生成全部 6 个表格块并通过 `--check`；更新 00 / 01（登记 ADR-027～029、新增 R-OS-22、同步全部 S2 设计与调研项并重算统计）；更新 README、文档中心、附页合并状态；拟定 S3 计划 | 03 v2（§1–§4、§8 定稿）、00 / 01 v2、S2 正式包 `kinstall-docs-2026-11-26-v2-S2.zip` | 待用户确认后开始 S3 主机初始化与调优（见 §4） |
| 2026-11-26 | 会话 G（S3 完成） | 上网调研关闭 R-INIT-02/07/09～14、R-OS-20、R-NET-08（同步 R-INIT-06/08）；改写 03 v2 §5–§7（内核与 cgroup v2 切换、主机防火墙与 CNI 共存、EOL / 离线源与冲突锁、`sudo-rs`/`uutils` 与 H6 最小权限、KYSEC/fapolicyd/SELinux 加白、主机退役清理、`database-pg`/`database-mysql` 与动态大页、chrony/网络/磁盘、逐模块 verify 与巡检基线）并新增 §9 等保主机加固（ADR-018），`gen_os_docs.py --check` 保持退出码 0；登记 ADR-030，关闭“03 §9”悬空引用；同步更新 00 / 01 / README / 文档中心 / 附页，拟定 S4 计划 | 03 v2（§1–§9 全文定稿）、00 / 01 v2、S3 正式包 `kinstall-docs-2026-11-26-v2-S3.zip` | 待用户确认后开始 S4 K8s 规划复核 I（见 §4） |
| 2026-11-26 | 会话 H（S4 完成） | 上网调研关闭 R-K8S-11（1.35–1.38 参数与模板差异）、R-K8S-12（混合架构 OCI Index 与防误调度）、R-RT-08（Docker 29 containerd image store vs overlay2 与 GC/水位），定稿 R-NET-06 方案（Calico 3.33 + kube-proxy nftables），同步 R-RT-02/04/06；改写 04 v2 §1–§5（去除 `<callout>`，更正 etcd v3.7.4、ADR-010 legacy cgroup、Docker 29.7.0、ADR-015 cri-dockerd RuntimeConfig 与 §15 风险行；新增 §3.4 混合架构与 containerd v4 配置）；关闭 00 §7 全部 4 项 S4 交叉引用与 A4/E1/E4/E20；更新 00 / 01 / README / 文档中心 / 附页，拟定 S5 计划 | 04 v2（§1–§5 定稿）、00 / 01 v2、S4 正式包 `kinstall-docs-2026-11-26-v2-S4.zip` | 待用户确认后开始 S5 K8s 规划复核 II（见 §4） |
| 2026-11-26 | 会话 I（S5 完成） | 上网调研关闭 R-K8S-10（GKE / EKS / ACK / AKS 预留公式对比）、R-NET-07（Calico / Cilium / Flannel / Kube-OVN 双栈与单栈迁双栈边界）、R-STO-02（1.37 SELinuxMount）、R-STO-03（华为 eSDK v4.8.0、XSKY、杉岩、浪潮）、R-STO-04（CSI 内核模块与服务），设计定稿 R-HA-04（本地代理）与 R-ADD-04（Addon 冻结）；改写 04 v2 §6–§16 使 04 全文定稿；登记 ADR-031～033；关闭 E16–E18 与 E5–E13 复核、I2（K8s 部分）、00 §7 全部 3 项 S5 交叉引用；03 §6.2 D16 引用更新，`gen_os_docs.py --check` 保持退出码 0；更新 00 / 01 / README / 文档中心 / 附页，拟定 S6 计划 | 04 v2（§1–§16 全文定稿）、00 / 01 v2、S5 正式包 `kinstall-docs-2026-11-26-v2-S5.zip` | 待用户确认后开始 S6 平台架构 v2 + 开发文档 v0.1（见 §4） |
| 2026-11-26 | 会话 J（S6–S15 全量完成） | 完成全部剩余设计步骤与 2026-11 上游事实核实：改写 02 v2（角色/规模/术语/版本兼容/自研引擎与租约锁/平台 HA/模板与 expr/多集群）；新建完整 `05-开发文档.md`（§1–§9：工程结构、Go 1.27 与 35+ 依赖锁定、核心接口、CLI/REST+SSE、RBAC/审批、错误码/slog/i18n/OpenBao 信封加密、测试分层与容器契约、goreleaser/CI/升级、插件指南、M0 拆解）；新建 `06` 中间件通用规范（OCI 声明式包、gRPC 外部插件、禁 Bitnami、弃 Promtail）与 `07`–`11` 五大中间件插件（Valkey 9.1/9.2、PG 18.7/19.0、MySQL 8.4/9.7 LTS、OpenSearch 3.10、对象存储淘汰 MinIO 选 SeaweedFS/Ceph RGW/RustFS）；新建 `12` 离线与供应链（含 2026-03 Trivy 投毒应对与全平台许可证汇总）、`13` 安全合规（等保/密评/国密边界与 `emmansun/gmsm`）、`14` 测试与验收（12 组正交 VM 矩阵、15 项故障注入、500 节点 kwok 模拟、验收模板）；登记 ADR-034～065 并定稿 ADR-012 / 013；关闭 00 §7 剩余悬空引用并登记 K8s `1.37.3/1.36.7/1.35.11` 待办；补充 §9 全阶段工程开发实施 Checklist；`spec/gen_os_docs.py --check` 通过 | 全量正式文档包 `kinstall-docs-2026-11-26-v2-full.zip`（00–14 + 03-A + `spec/` + `research/`） | 按 `05 §9` 与本文 `§9` 启动 M0 PoC 工程实施；12 月中旬跟进 K8s 1.38 GA 与 BOM `2026.12.1` |
| 2026-11-26 | 会话 K（S16 架构深度评审与全栈补强） | 对 00–14 开展 12 维红队评估并修补 8 大盲区（三元部署目标含 Docker Engine 29.10 + Compose v5.8、`Stack` 复合栈、EL8 glibc 2.28 + 64K jemalloc 构建与随包 JDK 21、Go 插件与声明式包分工、Day-2 三向合并与 `create-only`、大规模分发限速、三类回滚边界）；新建 `15`（ELK/OpenSearch 日志全栈）、`16`（Kafka 4.x KRaft）、`17`（SkyWalking 11.1 + BanyanDB）、`18`（架构评估 + Valkey/Redis 单节点/哨兵/Cluster × 二进制/Docker 端到端代码落地样板）；同步回写 `02`、`05`、`06`、`07` 并登记 ADR-066～076；`spec/gen_os_docs.py --check` 保持通过 | 全量终版文档包 `kinstall-docs-2026-11-26-v3-complete.zip`（00–18 + 03-A + `spec/` + `research/`） | 按 `05 §9`、`18 §4` 与 `00 §9` 启动 M0 PoC 编码 |
| 2026-11-26 | 会话 L（深度设计 D1 · 底座 1/3） | 确立“先底座（D1–D3）→ 再平台（G1–G3）→ 再各插件（P1–P7）”分模块深度设计路线；完成 `deep-design/D1`：涵盖 Go 六边形工程代码方案、单向依赖 `depguard` 铁律、显式 DI、9 大初始设计模式映射与骨架代码、Contract-First 垂直切片并行开发法，以及 SSH/SFTP 双层限流连接池（`MaxSessions`/`MaxStartups`）、Async Detached 断点重附、`sudo-rs#1668`/`pam_faillock` 熔断、`pkg/sftp` 并发写+原子替换、跨块滑动窗口脱敏、OpenBao 2.7.2 `derived=true`+`context` 规避 `/rewrap` AAD 陷阱（`hashicorp/vault#31791`）、1-RTT POSIX Facts 脚本与真实容器测试门禁；同步更新 00/README/文档中心并全量校验通过 | `deep-design/D1-*.md`、全量文档包 `kinstall-docs-2026-11-26-v3-D1.zip` | 待用户确认后进入 **D2（底座 2/3）**：SQLite/PG 完整 DDL、自研任务引擎与 Fencing 租约锁、3-Way Diff、`Stack` 复合栈与全部 15 个内置 Task（含 Docker/Compose + `DOCKER-USER`） |
| 2026-11-26 | 会话 M（深度设计 D2 · 底座 2/3 + R1 15 项扩展调研） | 完成 `deep-design/D2`（SQLite `modernc.org/sqlite` 读写池分离+`_txlock=immediate`+`StrictPragmas` 与 PG 18 完整 11 表 DDL SQL、单调 Fencing Token 租约抢占与防僵尸写回、两级防死锁资源锁、Day-2 3-Way Diff 6 场景矩阵与 `create-only`、`Stack` DAG 静态引用校验与防级联误删、全量 15 个内置 Task 契约及 Docker Engine 29 `DOCKER-USER` / `nftables` 独立表持久化守卫）；同步完成 `deep-design/R1`（MongoDB/PSMDB/FerretDB、Rancher/Reranker、OnlyOffice 10、ZooKeeper 3.9.7、JDK 二进制 Temurin 21/25+毕昇 JDK、Harbor 2.16 全量 arm64+Valkey 64K 避坑、GitLab 19/Forgejo、Jenkins 2.580+JCasC、Maven 3.10/Reposilite、Npm/Node 26/24+Verdaccio、SonarQube 26.x、RocketMQ 5.5、Seata 2.6 Raft/NamingServer、Milvus 2.6/3.0 Woodpecker+CVE-2026-26190、Neo4j CE 无集群警示与 PG+AGE 替代，明确底座 Runtime 与 P8–P10 归位）；同步更新 00/README/文档中心并通过全量校验 | `deep-design/D2-*.md`、`deep-design/R1-*.md`、全量文档包 `kinstall-docs-2026-11-26-v3-D2.zip` | 待用户确认后进入 **D3（底座 3/3 · 底座封板）**：离线供应链信任链、跨架构 4K/64K/glibc 2.28 及 `runtime/jdk/maven/nodejs` 封装、Zot + HTTP 分发限速与边缘缓存 |
| 2026-11-26 | 会话 N（R2 历史安装文档审计 + D2 反向校准） | 逐行审计用户提供的真实历史手工部署文档 `old-install.md`（2216 行），输出脱敏审计报告 `deep-design/R2`：定位 16 项真实生产 Bug（`tcp_tw_recycle` 废弃与粘连、`daemon.json` 缺逗号、`Type=oneshot+Restart` 非法、`sonar.properies` 拼写错、Sonar/Attu 9000 端口碰撞、Neo4j APOC 离线拉 GitHub、RocketMQ 缺 `brokerIP1`、多 JDK 手改 `bin/mvn` 等）并提炼 7 项反向架构增强（全局 `registryMirror` 自动镜像重写、Docker 子网 IPAM 防撞、多 JDK 无侵入隔离、Nginx L4/L7 网关、`generic-app` 业务容器栈、Zabbix 7 集成）；立即回写 D2 DDL 新增 `subnets_json` 并完善 `image.load`/`compose.sync` 契约 | `deep-design/R2-*.md`、修订 `D2 v1.1`、全量文档包 `kinstall-docs-2026-11-26-v3-D2-R2.zip` | 待用户确认后进入 **D3（底座 3/3 · 底座封板，融合 R1/R2 全部离线与多 JDK 需求）** |
| 2026-11-26 | 会话 O（深度设计 D3 · 底座 3/3 封板） | 完成第一阶段底座最后一块拼图 `deep-design/D3`（底座全量封板！）：零出网 `sigstore-go v1.4.1` `trusted_root.json` 验签（规避 2026-11 `kyverno#18525` 私有 TSA 空指针坑）+ `emmansun/gmsm` SM2/SM3 双签、`oras-go/v3 v3.0.0` 多根图同步与 Split-Host 鉴权、全局 `registryMirror` 重写引擎源码实现与 `certs.d` 私有 CA 自动下发（零 `insecure-registries`）、EL8 `glibc 2.28` + `amd64`/`arm64-4K`/`arm64-64K` jemalloc 构建与 ABI 门禁、底座多版本 `runtime/{jdk,maven,node}` 无侵入隔离（Temurin ≥ 2026-11-18 CSPU / 毕昇 JDK / `cacerts` 幂等注入 / `mavenrc`）、Jenkins `2.580+`（含 9 个 detached 插件）/ Neo4j APOC / SonarQube / OnlyOffice 离线闭包、保留 `io.ReadSeeker` 的 HTTP Range 两级限速分发引擎源码骨架、Zot `v2.1.23` 区域 Pull-Through 边缘缓存（含 `compat: ["docker2s2"]` 与 `scrub`）、OS 源/商业 ISO 闭包、流式分卷与 `bundle diff`、Syft+Grype+隔离 Trivy 与全生态红黄绿许可总闸；同步明确第二阶段平台层交付（G1 含全仓 Go 后端代码方案、G2 交付独立前端 `WEB_DESIGN.md`） | `deep-design/D3-*.md`、全量文档包 `kinstall-docs-2026-11-26-v3-D3.zip` | 待用户确认后正式进入第二阶段平台层：**G1（平台 1/3 · 服务层高可用/审批/审计 + 全仓 Go 后端完整代码落地方案）**，紧接 **G2（含独立完整前端 `WEB_DESIGN.md`）** |
| 2026-11-26 | 会话 O-2（D3 v1.1 补强 · 本地/在线/OBS 桶三元介质引擎） | 响应用户“安装包介质支持本地、在线及 OBS 桶拉取”需求，在 `D3 §7` 新增 `internal/artifact/source/` 完整方案与 Go 代码骨架，并联动回写 `D2 §5.1`（`binary.install`/`image.load`）及登记 **ADR-077**：支持 **Local 本地 / 华为云 OBS·阿里云 OSS·腾讯云 COS·S3·SeaweedFS / Online 在线** 三元优先级责任链与 `singleflight` 并发去重缓存；支持 `relay-cache`（默认零出网目标机）与 `direct-presigned`（华为云同 VPC 15m 短效只读预签名直拉 + `RedactWriter` 自动脱敏）双交付通道；规避华为云 2026 新桶禁公开策略与 `aws-sdk-go-v2` 尾随校验和（`WhenRequired`）兼容坑；提供 `s3` 与华为官方 `obs-native` 双驱动及 `verify-sources`/`pull`/`sync-to-obs` 闭环 CLI | `D3 v1.1`、`D2 v1.2`、登记 **ADR-077**、全量文档包 `kinstall-docs-2026-11-26-v3-D3.1.zip` | 待用户确认后正式进入第二阶段平台层：**G1（平台 1/3 · 服务层高可用/审批/审计 + 全仓 Go 后端完整代码落地方案）**，紧接 **G2（含独立完整前端 `WEB_DESIGN.md`）** |
| 2026-11-26 | 会话 P（深度设计 G1 · 平台 1/3 + 全仓 Go 后端代码方案） | 正式进入第二阶段平台层，完成 `deep-design/G1`：确立 CLI/Server 同源 `internal/app` Facade；落地 OpenAPI 3.1（`oapi-codegen v2.9.0` Chi Strict Server + Go Client + TS Schema）+ RFC 9457 + `Idempotency-Key`（24h 并发去重）+ `ETag` 乐观锁；给出多副本无丢帧 SSE 生产实现（`http.ResponseController.SetWriteDeadline` 解除全局写超时 + 单帧 10s 保护 + 独立直连 PG `LISTEN/NOTIFY` + 重连自动 `resync` 脉冲）；落地 OIDC PKCE / Argon2id 5 次锁定 / 短效 Token + **SQL `CHECK` 与 Go 业务层双重防自审**四眼审批状态机；解决多副本并发写审计链分叉问题（`audit_chain_head` + PG `pg_advisory_xact_lock` + 微秒精度截断 + SHA-256/SM3 双链 + 异步 RFC 5425 TLS Syslog Outbox）；定义 Expand-and-Contract HA 滚动升级与 `StateMigrator`；完整提供**全仓 Go 1.27 后端代码落地方案**（30+ 绿灯依赖锁定、18 个 `internal/` 包映射与 `cmd/kinstall/app.go` 显式 DI Wiring + 优雅停机） | `deep-design/G1-*.md`、全量文档包 `kinstall-docs-2026-11-26-v3-G1.zip` | 推进 **G2（平台 2/3）+ 独立交付完整前端 `WEB_DESIGN.md`**（关闭 B9 / ADR-023） |
| 2026-11-26 | **会话 Q（全三阶段终版封板 `v4-all-complete`）** | 一次性高质量完成全部剩余平台层与插件层深度设计并正式封板：① 独立交付根目录 **`WEB_DESIGN.md`**（完整 IA 与 11 核心页面含本地/在线/OBS 桶介质源配置中心、深/浅 Design Tokens、`x-kinstall-ui` JSON-Schema 动态表单、`@codemirror/merge` 3-Way Diff、`@xterm/xterm` + `fetch` SSE 分屏终端、Vue Flow 2.0 `xyflow` Stack DAG 画布、Vue `3.5.45` / Element Plus `2.14.2` / Pinia `3.0.4` / `@hey-api/openapi-ts` 禁用 `CVE-2026-108551`、`go:embed` 零 CDN 打包），正式关闭 **B9** 与定稿 **ADR-023**；② 完成 `deep-design/G2`（多集群短期 kubeconfig、CMDB 双向同步与两段式 GitOps、防风暴通知、等保 2.0 三级与密评实操含 Tongsuo TLCP 网关）与 `deep-design/G3`（三层多架构测试平台、Ubuntu 26.04 容器契约、15 项故障注入脚本、20 真机 + 480 `kwok` 500 节点压测 SOP）；③ 全量交付核心插件 `P1–P7`（重点含 **`P3` 大型 ELK/OpenSearch + Kafka 4.x KRaft `KIP-848` + Data Prepper `2.16/2.17` `acknowledgments` + S3/OBS DLQ + 4 级背压 + 完整 `Stack` YAML**，以及 K8s、Valkey/Redis、PG、MySQL、SeaweedFS/Ceph、SkyWalking+BanyanDB）与扩展插件 `P8–P10`（Harbor/GitLab/Jenkins/SonarQube/Reposilite/Verdaccio/Nginx、RocketMQ 5.5/Seata 2.6/ZK、Milvus+Attu/MongoDB/Neo4j+AGE/OnlyOffice/Rancher/Zabbix 7/`generic-app`）；同步更新 `00`/`README.md`/`文档中心.md` 并通过全量自动化校验 | **`WEB_DESIGN.md`**、`deep-design/G2`、`G3`、`P1–P10`、终版全量文档包 `kinstall-docs-2026-11-26-v4-all-complete.zip` | 深度一致性复审与全量开发任务 Checklist |
| 2026-11-26 | **会话 R（全仓深度一致性复审 + 126 项开发任务 Checklist `v4.1-complete`）** | 对全部 42 篇文档开展深度语义、DDL 可执行性、依赖版本与跨文档一致性复审，发现并修复 6 类隐蔽工程隐患：① `D2` 全量主键由 `CHAR(26)` 改为 `VARCHAR(48)` 以兼容前缀 ULID（杜绝 PG 长度溢出）；② 统一 `D2` 与 `G1` 的 `operations.state` CHECK 枚举、`updated_at`、`instances.state_json` 及防分叉 `audit_logs` 结构；③ 在 `G1 §6.3` 补齐 `0002_platform_control_plane.sql` 10 张控制面表，全库 **24 张表已在真实 SQLite + 外键开启环境下通过实测与 `chk_no_self_approval` 拦截验证**；④ 对齐 `G1 §7.1` 与 `05 §2.2` 全部 10 处 Go 依赖版本及 `@hey-api/openapi-ts`；⑤ 修复 `D2` `KI-ENGINE-0005` → `KI-ENG-0005`；⑥ 关闭 `01` `R-PLAT-05`（✅ 108）并清理 `02`/`05` `ADR-023 待定` 残留；独立交付 **126 项原子级开发任务表 [`DEV_TASK_CHECKLIST.md`](./DEV_TASK_CHECKLIST.md)** 并准备本地 Git 推送分支 | **`DEV_TASK_CHECKLIST.md`**、修订 `D2 v1.3` / `G1 v1.1` / `01` / `02` / `05`、终版包 **`kinstall-docs-2026-11-26-v4.1-complete.zip`** | 补齐前端 UI 样式视觉基准 `DESIGN.md` 与全领域终审 |
| 2026-11-26 | **会话 S（独立前端 UI 视觉基准 `DESIGN.md` + 10 领域全仓深度复审 `v4.2-complete`）** | ① 新增根目录 **[`DESIGN.md`](./DESIGN.md)**（520 行：工业精密控制室视觉语言、暗/亮双主题完整 `tokens.css`、8 级排版与 `tabular-nums`、双密度栅格与 Z-Index、Element Plus `2.14.2` `element-overrides.css` 桥接、10 大基础 `Ki*` + 8 大基础设施领域组件规格、A11y 与 10 条视觉反模式红线，摒弃紫光暗色套路）；② 开展 10 领域全仓硬核审计，发现并根治 6 项真实工程 Bug：修复 `G1 §3.2` + `WEB_DESIGN.md §4.3` SSE `operation.done` 复用 `lastSentID` 触发客户端无限重连缺陷、补齐 `D3`/`G1` Go 示例缺失 `import` 与未用变量、修正 `P4` HAProxy 端口 `:5000/:5001` 对齐 `08`、修正 `T-M3-14`/`T-M3-20` 与 `DESIGN.md`/`WEB_DESIGN.md` 规范漂移、验证全部 24 表外键与 44 篇文档链路 100% 绿灯 | **`DESIGN.md`**、修订 `WEB_DESIGN.md v1.1` / `G1` / `D3` / `P4` / `DEV_TASK_CHECKLIST.md`、终版包 **`kinstall-docs-2026-11-26-v4.2-complete.zip`** | 按全部原始附件再次逐行分析与实战优化 |
| 2026-11-26 | **会话 T（全附件对账 + 5 项深水区生产优化 `v4.3-complete`）** | 逐一核验全部原始附件：① 与 `kinstall-docs-2026-11-26-v2-S4.zip` 100% 零遗漏对账（0 失落文件、Design ID 127→132、ADR 30→77、Research ID 113→116）；② 深度重审 `old-install.md` 全量 2216 行并落地 5 项生产级深水区优化（`#17–#21`）：`docker.service` 强制 `LimitNOFILE=1048576`（防 systemd 252+ `infinity` 导致容器 `closefrom()` 假死/OOM）+ 独立数据盘 `data-root` 门禁、`compose.sync` 显式 `file\|dir` 挂载预建（防缺文件被 Docker 建目录崩毁）+ 禁 `chmod 777`/默认 `privileged: true`/`docker cp`、`Stack.externalRefs` 只读云/存量服务引用 + 白名单 `urlEncode` 双态脱敏、`P4`/`P5` 声明式最小权限 `bootstrapDatabases`（禁 `*.*` 与 `WITH GRANT OPTION`）、`P10` 拆分 OnlyOffice Docs `10.0` vs 遗留 Workspace 12 隔离守卫并增强 `generic-app` `sharedNetworkRef`；同步对齐 `R2` Attu 端口（`19001`）与 `DEV_TASK_CHECKLIST.md v1.1` | 修订 `D2 v1.4` / `P4 v1.1` / `P5 v1.1` / `P10 v1.1` / `R2 v1.1` / `DEV_TASK_CHECKLIST.md v1.1` / `00` / `README.md` / `文档中心.md`、终版包 **`kinstall-docs-2026-11-26-v4.3-complete.zip`** | 整体架构深度整理与 `DESIGN.md` 深度优化 |
| 2026-11-26 | **会话 U（整体架构深度整理 + `DESIGN.md` v2.0 深度重构 `v4.4-complete`）** | ① 新增根目录 **[`ARCHITECTURE.md`](./ARCHITECTURE.md)** 作为唯一架构口径：C4 上下文、六层架构、28 个 `internal/` 规范包图与 depguard 矩阵、Operation 状态机（`failed → pending` 同一 Operation `--resume`）、介质解析与 SSE 运行时流、24 表 ER、SLO、ADR 十大主题簇、RA-01～RA-12 风险台账与 O-01～O-12 优化项（全部挂载既有 `T-*`，不新增任务与 ADR）；② 修复 F-01～F-10 跨文档漂移：`D1 v1.2` 包命名（`internal/server` / `internal/artifact` / `internal/registry` / `internal/events`）与唯一组合根 `cmd/kinstall/app.go`、`G1 v1.1` §7.2 补齐 28 包蓝图、`02 v2.1` / `05 v1.1` 废除 `init()` 自注册改为组合根显式注册（`gochecknoinits` 门禁）、`02 §11` 进度以 `DEV_TASK_CHECKLIST.md` 为准；③ **`DESIGN.md` v2.0** 深度重构：三层 Token、`--ki-text-muted` 暗 `#8794a8` / 亮 `#5b6b82`（原 `#64748b` 在 surface 上仅 3.73:1）、白字实色按钮 `#2563eb`（5.17:1）、`html[data-theme]` 覆写 EP 暗色（压过 `html.dark` 特异性）、`@supports color-mix()` 回退、CSP 外置 `theme-boot.js`、新增 `KiEnvBadge` / `KiEmptyState`、`@xyflow/vue` 2.0、页面模板 / 数据格式 / 通知 / RBAC 视觉规范与 16 条 PR 红线；同步 `WEB_DESIGN.md v1.2` 与 `DEV_TASK_CHECKLIST.md v1.2` | **`ARCHITECTURE.md`**、**`DESIGN.md` v2.0**、修订 `D1 v1.2` / `G1 v1.1` / `02 v2.1` / `05 v1.1` / `WEB_DESIGN.md v1.2` / `DEV_TASK_CHECKLIST.md v1.2` / `README.md` / `文档中心.md`、终版包 **`kinstall-docs-2026-11-26-v4.4-complete.zip`** | 全仓系统架构、安全供应链、插件与测试深度审计优化 |
| 2026-11-26 | **会话 V（全仓 45 篇系统架构 / 安全供应链 / 插件 / 测试全域深度一致性终审修复 `v4.5-complete`）** | 围绕全仓 45 篇文档开展三路并发深度红队审计，集中根治 **70+ 项真实工程缺陷与跨文档漂移**：① **安全与正确性高危**：禁用 `O=system:masters` 改签可吊销 `kinstall:cluster-admin`（`G2`/`02`）；修复 `D3 §7.4` `MediaResolver` 忽略 `OfflineStrict`/`LocalRoots`/`priority`/`auto` 导致离线逃逸缺陷并确立 `KI-SRC-0005`；修复 `D2` Fencing UPDATE 缺少 `FOR SHARE` 的并发快照竞态与 `operation_events` 并发乱序提交跳帧；修复 `G1` 幂等中间件主键失配、`LimitReader` 截断与 `WithoutCancel`、审计全列哈希与独立 SM3 链、组合根补回 `BuildExecutor` 脱敏熔断装饰链与严格逆序停机；修复 `18` `glibc` 字符串比较 Bug（新增 `sdk.DottedVersionLess`）、`09` 已移除的 `default_authentication_plugin`、`P3` librdkafka `enable.idempotence` 须配 `acks=all`；② **架构与契约统一**：对齐 `D1`/`02`/`05`/`G1` 的 `OpState` 7 态、15 个内置 Task 与构造器、24 表 `Store` 与 `media_policies` 三级作用域、`kinstall.io/v1alpha1` Stack/Instance 规范、SSE `operation.done` 无 `id:` 行、`:plan/:apply` 路径、错误码登记表与 `KI-DIFF-0001/0002` vs `KI-PLAN-0012` 拆分、`05 §8.2` 插件骨架可编译化；③ **插件与测试全量校准**：统一麒麟 V10 SP3=64K / V11=4K 实测口径（`P1`/`P2`/`14`/`G3`）、`G3 §1.1` M01–M12 唯一 VM 矩阵、`14 §6` ADR-064 `FI-01–FI-15`（中间件拆分 `FX-01–FX-08`）、20 真机 + 480 `kwok` = 500 节点与完整 `hydrophone`、22 个初始化模块与 `04`–`17` / `P1`–`P10` 全部版本/端口/目录/许可/配额统一；全量 24 表 SQLite + 外键与 `chk_no_self_approval` 复测、`spec --check`、`gofmt` 与 45 篇链接全绿 | 修订 30+ 篇核心/安全/插件/测试与索引文档、终版全量包 **`kinstall-docs-2026-11-26-v4.5-complete.zip`** | 授权 `gongjin1997/HIS` 写权限后推送 `main`，并按 `DEV_TASK_CHECKLIST.md` 启动 M0 研发 |

## 9. 全面系统开发实施 Checklist（M0–M5 研发里程碑摘要）

> **★ 完整 126 项带编号（`T-M0-01`～`T-M5-25`）、分泳道分工与 DoD 验收门禁的可执行工程任务清单，请直接打开根目录独立文档 [`DEV_TASK_CHECKLIST.md`](./DEV_TASK_CHECKLIST.md)**。以下保留高层里程碑摘要供快速查阅。

### M0：PoC 垂直切片（第 1–2 周，详见 `05 §9.1`）
- [ ] 初始化 Go 1.27 单仓结构（`05 §1.1`），配置 `.golangci.yml`（v2 + `depguard` 强制 ADR-022）与基础 CI（`05 §7.3`）
- [ ] 嵌入 `spec/`（`osmatch.yaml` / `pkgmap.yaml` / `osmatch_golden.yaml`），接入 `gen_os_docs.py --check` 至 CI
- [ ] 实现 `internal/executor/sshx`（连接复用、hostkey TOFU/固定、ProxyJump、sudo -n/-S、SFTP + stdin 降级，`02 §6`）与 `gliderlabs/ssh` 仿真单测
- [ ] 实现 POSIX 零依赖 Facts 采集与 `OSAdapter`（`03 §2 / §4`），跑通 Rocky 9 / Ubuntu 24.04 / openEuler 24.03 容器契约测试（`05 §6.3`）
- [ ] 实现 SQLite（`modernc.org/sqlite` + `bun` + `goose`，ADR-034）最小 Schema 与自研任务引擎（Phase/Task/Check/Apply/断点续装，ADR-035）
- [ ] 实现 `pkg/sdk/v1alpha1` 接口与最小 Task 库（`file.sync` / `pkg.ensure` / `binary.install` / `systemd.unit` / `sysctl.apply` / `kmod.ensure` / `cmd.run` / `wait.http`）
- [ ] 实现 `internal/template`（`text/template` + 白名单函数 + `expr`，ADR-036）与黄金测试框架
- [ ] 完成 K8s 1.37 单主最小链路（PKI、etcd v3.7.4、控制面、kubelet、containerd v4、本地代理、Calico、CoreDNS）
- [ ] 在 Rocky 9（amd64）、Ubuntu 24.04（amd64）、麒麟 V11（arm64）跑通冒烟 + 连续两次 `apply` 100% `Skipped` 退出门禁

### M1：平台核心 + CMDB + K8s 生产 MVP（第 3–10 周）
- [ ] 补齐全部 22 个主机初始化模块（含首尾 `backup/verify`）与 7 个 Profile（含 `database-pg/mysql`、`harden` 等保模块，`03 §5–§6 / §9`）及逐模块 `verify` 与漂移检测
- [ ] 实现主机批量导入（CSV/YAML/`excelize` `.xlsx`，`05 §4.4`）、网段发现、公钥分发、定时巡检与 `HostRole.Claims` 容量冲突检测（`03 §8`）
- [ ] 实现 `internal/secrets` 信封加密（AES-256-GCM + 可选 `emmansun/gmsm` SM4-GCM + `file`/`env`/`OpenBao Transit` KEK + `kinstall secrets rewrap`，`05 §5.5`）
- [ ] 实现 `internal/lock`（实例锁 + 主机按 ID 排序批量共享/排他锁 + 维护窗口，`02 §5.4`）与错误码 `KI-` + `go-i18n` 中英目录 + 两层脱敏日志（`05 §5`）
- [ ] 实现 K8s 1.35–1.37 多版本模板分支（`04 §2.4`）+ `kubeadm v1beta4` CI 基线语义对比（`05 §6.2`）
- [ ] 完成三主/五主 HA、外置 etcd、混合架构（OCI Index 强校验 + 64K 页隔离）、双栈（ADR-031）、Calico nftables / Cilium / Flannel / Kube-OVN、CSI Extension（含 SELinuxMount 与厂商接入，ADR-032）
- [ ] 实现离线 Bundle 构建器（裁剪/层去重/zstd 分卷）、内置 HTTP 软件源 + Zot v2.1.23、OS 包闭包流水线与 Syft+Grype+cosign 信任链（`12`）
- [ ] 完成 Addon（Helm v4 SDK、Headlamp、Envoy Gateway、kube-prometheus-stack、kubelet-csr-approver）与 30 分钟离线 3+3 集群验收

### M2：K8s 完整生命周期与高可用演练（第 11–16 周）
- [ ] 实现 K8s `scale`（增删控制面/工作节点）、`upgrade`（1.35→1.36→1.37 逐级滚动 + SELinuxMount 审计）、`rotate-certs`、`etcd-backup/restore`、`uninstall/reset`（`04 §13`）
- [ ] 实现 `kinstall support-bundle` 诊断包与 `kinstall operation report` Markdown/JSON 报告（`05 §4.7`）
- [ ] 搭建 KVM + 鲲鹏测试环境，执行 12 组正交 VM 矩阵（含 Ubuntu 26.04 `sudo-rs`/`uutils`、Calico+nftables 实测）、15 项故障注入（`14 §6`）、`hydrophone v0.8.0` Conformance 与 `kwok` 500 节点模拟（`14 §8`）
- [ ] 跟进 K8s 1.38 GA（2026-12-16 后）与 BOM `2026.12.1`（1.37.3/1.36.7/1.35.11）补丁发布

### M3：Server 模式 + Web UI + 多集群与合规（第 17–24 周）
- [ ] 冻结 `api/openapi.yaml`（OpenAPI 3.1），生成 chi strict server 与 Go/TS 客户端（`oapi-codegen v2.9.0`）；实现 202+Operation、SSE + `Last-Event-ID`、RFC 9457、ETag、`Idempotency-Key`（`05 §4.2`）
- [ ] 实现 PostgreSQL（`pgx` + `bun` + `goose`）多副本无状态 HA（`FOR UPDATE SKIP LOCKED` 抢占 + 租约接管 + 滚动升级 schema 门禁，`02 §8`、`05 §7.5`）
- [ ] 实现 OIDC SSO + Argon2id 本地账号 + API Token + 项目/环境 RBAC + 四眼审批门（`05 §4.3`）
- [ ] 实现多集群视图与短效 RBAC kubeconfig 签发/吊销（`02 §14`）、Spec 导入导出与 GitOps `diff/apply`（`05 §4.5`）、Webhook/邮件/企微/钉钉/飞书通知（`05 §4.6`）
- [ ] 实现 `audit_logs` 哈希链（SHA-256/SM3）+ TLS syslog 外送 + `kinstall compliance report` 等保/密评/CIS 证据包（`13 §5 / §7`）
- [ ] 按 `WEB_DESIGN.md`（ADR-023 定稿：Vue `3.5.45` / Element Plus `2.14.2` / `@hey-api/openapi-ts` / Vue Flow `2.0`）实现 `go:embed` 零 CDN 内嵌控制台（主机纳管、本地/在线/OBS 桶介质源配置中心、动态表单、3-Way Diff、SSE 分屏终端、Stack 画布、四眼审批、多集群）

### M4：声明式插件体系 + 第一批中间件（第 25–30 周）
- [ ] 实现 `docker.ensure` / `image.load` / `compose.sync`（含 `DOCKER-USER` 防火墙链）、`internal/stack` 复合栈编排与 Planner 三向合并 3-Way Diff（ADR-066 / 067 / 070）
- [ ] 交付 `07` / `18 §3` **Valkey / Redis** 完整 Go 插件（单节点 / 主从+哨兵 / 3主3从 Cluster × `host-binary` 与 `host-container` × `amd64` / `arm64-4K` / `arm64-64K`）
- [ ] 交付 `08` **PostgreSQL** 插件（Patroni 4.1.5 + etcd + PgBouncer + pgBackRest ≥ 2.60.0 + K8s CNPG 1.30）
- [ ] 通过 Valkey & PG 的离线安装、HA 切主、PITR 恢复演练与小版本滚动升级验收

### M5：中间件扩展 + 外部插件 + 信创深化（第 31 周起持续迭代）
- [ ] 交付 `09` **MySQL**（8.4/9.7 LTS、InnoDB Cluster + XtraBackup + Percona Operator/MOCO）
- [ ] 交付 `10` / `15` **OpenSearch / ELK 日志全栈**（Fluent Bit/Filebeat + Kafka + Data Prepper/Logstash + OpenSearch/ES + Dashboards/Kibana）
- [ ] 交付 `16` **Apache Kafka 4.x KRaft** 插件（多盘 JBOD + 随包 JDK 21 + Strimzi 1.3.2 + Kafbat UI / Cruise Control）
- [ ] 交付 `17` **Apache SkyWalking 11.x APM** 插件（BanyanDB 0.11 + Horizon UI + Satellite + 多语言离线 Agent + SWCK 0.11）
- [ ] 交付 `11` **对象存储**（默认 SeaweedFS 4.54 + Ceph RGW + 可选 RustFS 1.0.6 / Garage v2.5.0）并与 PG/ES/K8s 备份打通闭环
- [ ] 实现声明式插件包加载器（面向轻量组件与配置热扩展，ADR-069）与 `StateMigrator`
- [ ] 实现三期 `hashicorp/go-plugin v1.8.0` 外部进程插件协议（`06 §7`），接入厂商 CSI / 硬件密码机 HSM 外部插件
- [ ] 持续扩展信创矩阵（授权真机采样 UOS / CTyunOS 回填 `spec/`）、评估 GPU/NPU（E21）与 Kafka/Nacos（F7）插件
