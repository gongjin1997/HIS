# P10 扩展插件深度设计（10/10）：AI 向量/文档/图数据库、协同办公与通用业务容器栈

> **本文定位**：kinstall 第三阶段收官插件族深度设计，完整落位 `R1`（Milvus、MongoDB、Neo4j、OnlyOffice、Rancher）与 `R2`（`old-install.md` 真实业务容器栈 `generic-app` 与 Zabbix 7 监控）的全部剩余组件，并对每项红灯许可证组件严格执行 ADR-059 合规隔离门禁。
>
> **版本**：v1.2（扩展插件 10/10 · 全量插件收官，2026-11-26；v1.2 对齐 `06 §1.1` `/data/generic-app/hkms-prod/` 挂载规范）。跨架构治理：Milvus（C++ Knowhere 引擎按 `amd64 AVX2` / `arm64 NEON` 分发，`arm64-64K` 页主机强校验 jemalloc 构建）、MongoDB 8.0（需 `x86-64-v2` / `arm64-v8.2-a`）、Neo4j（底座 JDK 21）与 OnlyOffice 10（`amd64`/`arm64` 多架构镜像）均接入 D1 Facts 与 D3 BOM 预检门禁；`v1.1` 补齐 OnlyOffice Docs vs 遗留 Workspace 12 双模式隔离（§4）与 `generic-app` 共享网络/外部引用/显式 `file|dir` 挂载（§5）。

---

## 1. AI 向量检索：Milvus `2.6.26 / 3.0.5` + Attu（🟢 Apache-2.0）

- **2026-11 高危安全基线（强制门禁）**：Preflight 硬拦截任何未修复 **`CVE-2026-26190`（CVSS 9.8 鉴权绕过）** 的旧版本；默认强制开启 `common.security.authorizationEnabled: true` + 强随机 `root` 密码 + gRPC/HTTP TLS。
- **双拓扑演进**：
  - **中小规模（≤ 千万级向量）**：**Milvus Standalone（Woodpecker 轻量零 MQ 模式）**，彻底去掉早期 Standalone 依赖内嵌 Pulsar/RocksMQ 的沉重开销，直接经 `Stack` 对接 `P6` SeaweedFS/OBS + `etcd`；
  - **大规模生产（亿级向量/高并发 RAG）**：**Milvus Distributed Cluster**（推荐 `kubernetes` 形态经官方 Milvus Operator Helm v4 部署，联动 `P3` Apache Kafka 4.x + `P6` SeaweedFS/Ceph RGW + `etcd`）；
- **Attu Web 管理端防撞治理（R2 反哺）**：Attu 默认 Web 端口在模板中统一分配为 **`19001`**（而非容易与 SonarQube `9000` 冲突的 `9000`），且全量登记至 `HostRoleClaims` 预检。

---

## 2. 文档数据库：MongoDB `8.0.37` / PSMDB（🔴 SSPLv1 客户侧按需构建）

- **许可证与替代透明声明**：MongoDB 与 Percona Server for MongoDB (PSMDB) 均为 **SSPLv1（红灯）**，绝不打入 kinstall 公共官方 Bundle；客户通过 `--include-restricted=mongodb --accept-license` 独立构建私有离线包。针对追求纯开源替代的客户，如实提示 **FerretDB 项目 2026 活跃度放缓风险**，并推荐对纯 JSON 业务优先选用 `P4` **PostgreSQL 18 `JSONB` + GIN 索引**（或 DocumentDB 扩展）。
- **生产高可用规范**：
  - 至少 **3 节点 Replica Set（PSS 模式，拒绝 2 节点+Arbiter 导致的写多数派隐患）**；强制启用 `security.keyFile`（`0400` 权限，平台自动生成 756B 随机密钥）+ `SCRAM-SHA-256` + 全链路 TLS；
  - `storage.wiredTiger.engineConfig.cacheSizeGB = floor((MemTotal - OSReserveMB) * 0.50)`，禁用 THP（`never`），数据盘独立 XFS；
  - 备份闭环：物理/逻辑一致快照通过 **Percona Backup for MongoDB (PBM)** 对接 `P6` SeaweedFS / 华为云 OBS 实现 PITR（Oplog 连续归档）。

---

## 3. 图数据库：Neo4j + PostgreSQL Apache AGE 开源替代（🔴 GPLv3 / 🟢 Apache-2.0）

- **架构级关键预警（R1 实证）**：**Neo4j Community Edition（CE）严格仅支持单节点，不具备任何原生主从或高可用集群能力！**
  - 仅需单机图分析且接受 GPLv3：使用 Neo4j CE（客户侧按需拉取），**强制挂载 `D3 §4.3` 离线 `apoc-<ver>-core.jar` 至 `/plugins` 并严禁注入会偷连 GitHub 的 `NEO4J_PLUGINS=["apoc"]`**（彻底解决 `old-install.md` 离线必挂坑！）；堆与页缓存按 `heap = min(8G, 30% Mem)`、`pagecache = 40% Mem` 自动计算；
  - 要求高可用集群且无商业 Neo4j EE 预算：直接通过 `P4` PostgreSQL 18 启用 **Apache AGE（Apache-2.0，支持 openCypher 图查询 + Patroni HA + pgBackRest PITR）**。

---

## 4. 协同文档、多集群 UI 与传统监控：OnlyOffice / Rancher / Zabbix 7

| 组件 | 锁定版本与许可 | 生产级核心封装规范 |
| --- | --- | --- |
| **OnlyOffice Docs** | `10.0`（🔴 AGPLv3 客户侧按需构建） | **Mode A（默认推荐 · 独立 DocumentServer）**：覆盖 95%+ 企业预览/编辑集成场景；`host-container` / K8s；**零 `privileged: true`、零 `/sys/fs/cgroup:rw`、零 `chmod 777`**；强制 `JWT_ENABLED=true` + 强随机 `JWT_SECRET`；挂载 `D3 §4.3` **Noto CJK 开源中文字体闭包**并在 Post-Hook 自动运行 `documentserver-generate-allfonts.sh`；外置 `P4` PG + `P2` Valkey |
| **OnlyOffice Legacy Workspace（隔离守卫）** | 遗留 6 容器全套（🔴 禁作默认） | **Mode B（仅存量迁移隔离守卫）**：传统 Workspace 12 全套（Community/Mail/ControlPanel）强依赖 `privileged: true` + `/sys/fs/cgroup:rw`（在现代 `cgroup v2` 宿主机如 Rocky 9 / Ubuntu 24+ / 麒麟 V11 存在兼容性失效风险）且内置已 EOL 的 Elasticsearch 7.16 高危漏洞组件。新项目严禁选用；如存量业务确需迁移，Preflight 强制要求独立隔离 VM + 四眼审批显式开启 `allowPrivileged: true`，并强制剥离内嵌 MySQL 改接 `P5` MySQL 8.4 LTS、严禁暴露 `/var/run/docker.sock` |
| **Rancher** | `2.14.2`（🟢 Apache-2.0） | 作为 **`P1` K8s 集群的可选 Addon** 经 Helm v4 离线安装（RKE1 已彻底 EOL 禁选，下游纳管仅支持 RKE2 / kinstall 原生二进制 K8s）；注：若用户所指 `ranker` 为 AI **Reranker（如 BGE-Reranker/vLLM）**，归入远期 GPU/NPU 规划 `E21` |
| **Zabbix 7** | `7.x LTS`（🔴 AGPLv3 客户侧按需构建） | 应对 `old-install.md` 传统企业运维习惯：Server + Web + Proxy 均外置 `P4` PG 18（支持 TimescaleDB 时序分区），Agent 2 随主机初始化模块批量下发并自动注册 CMDB 主机，与 Prometheus/Grafana 双轨互补 |

---

## 5. 通用企业业务容器栈插件：`generic-app`（R2 实战反哺）

针对企业自研业务应用栈（如 `old-install.md` 中的 `hkms` AIGC 知识库后端 + 前端 + 自定义中间件容器），kinstall 提供开箱即用的 **`plugins/generic-app/`（声明式 OCI 插件包，ADR-069）**，让业务系统一键享受全套底座治理能力（含混合云外部服务引用、跨栈受管共享网络与文件级挂载防坑）：

```yaml
apiVersion: kinstall.io/v1alpha1
kind: Instance
metadata:
  name: hkms-prod
  project: biz-core
  env: prod
spec:
  product: generic-app
  deployTarget: host-container
  hosts:
    app: ["biz-node-01", "biz-node-02"]
  params:
    # 二选一：独立防撞子网（默认）或显式加入同机已有受管 Compose 实例网络（如 milvus-prod）
    dockerSubnet: "172.29.10.0/24"           # ★ 自动登记 instance_host_claims.subnets_json，防路由撞车
    # sharedNetworkRef: "milvus-prod"        # ★ 可选：引用同机受管实例的隔离 bridge 网络，避免手写 external:true
    firewallAllowCIDRs: ["10.20.0.0/16"]
    services:
      - name: hkms-api
        image: "corp-biz/hkms-api:2.4.1@sha256:..." # 自动经 D3 registryMirror 重写并校验 digest
        ports: ["${host.primaryIP}:8080:8080"]      # 严禁 0.0.0.0 裸暴露，配合 DOCKER-USER 守卫
        # ★ 显式区分 file / dir 挂载与精确属主权限：compose.sync 在 up 前原子预创建空文件与受控目录，
        #   彻底消灭“宿主机缺失单文件被 Docker 建为目录导致容器崩毁”与 chmod 777 反模式！
        mounts:
          - { src: "/data/generic-app/hkms-prod/logs/app.log", dst: "/app/bin/app.log", type: file, uid: 1000, gid: 1000, mode: "0640" }
          - { src: "/data/generic-app/hkms-prod/data", dst: "/app/data", type: dir, uid: 1000, gid: 1000, mode: "0750" }
        envSecretRefs:
          DB_DSN: "${instances.pg-main.outputs.databases.hkms.dsnSecretRef}"
          # 支持通过 Stack.externalRefs + urlEncode 安全连接外部云托管服务（如华为云 MongoDB DDS / OBS）
          CLOUD_DDS_URI: "mongodb://rwuser:{{ urlEncode .ExternalSecrets.cloudDdsPass }}@${externalRefs.cloud-dds.endpoints[0]}/hkms?authSource=admin"
          REDIS_PASSWORD: "${instances.valkey-main.outputs.appCredSecretId}"
        healthcheck: { httpGet: "http://127.0.0.1:8080/actuator/health", timeout: "60s" }
        logToStack: "prod-t3-logging-platform"      # 自动下发 Fluent Bit 采集规则接入 P3 ELK+Kafka
```

- **合法 systemd 托管与安全边界**：由 `D2 §5.1` `compose.sync` 自动生成 `Type=oneshot` + `RemainAfterExit=yes`（零非法 `Restart=`），单容器自愈交给 Compose `restart: unless-stopped` + `json-file` 日志轮转（`max-size: 100m / max-file: 3`）；敏感环境变量统一渲染至 `0600 .env`（绝不以明文落入 `compose.yaml`）；默认禁止 `privileged: true` 与 `chmod 777`，彻底终结手工写残缺脚本的历史。
