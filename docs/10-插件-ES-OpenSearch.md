# 10 插件设计：Elasticsearch / OpenSearch

> **本文定位**：kinstall 搜索与日志分析引擎插件设计，覆盖 **OpenSearch（默认，Apache-2.0）** 与 **Elasticsearch（可选）** 在主机模式（官方 `tar.gz` + systemd）与 Kubernetes 模式（OpenSearch K8s Operator 3.0 / ECK 3.5）的完整规划、集群角色、安全、快照与滚动升级规范。通用约定遵循「06 中间件插件通用规范」。
>
> **版本**：v1.1（S11/V，2026-11-26；v1.1 消除 §2.1 与 §7 `bootstrap.memory_lock: true` 的 `mlockall` 描述冲突）。版本号、EOL 与许可证信息截至 2026-11-26，引用前请复核。

## 1. 定位与结论摘要（F2、R-MW-01、ADR-051～053）

- **默认选型（ADR-051）**：**默认 OpenSearch（推荐 `3.10.0`，兼容 `2.19.7`）**，**Elasticsearch（`9.5.x` / `8.19.x`）作为显式可选**。
- **选择理由**：
  1. **许可证与治理**：OpenSearch 已移交 Linux Foundation 旗下的 OpenSearch Software Foundation，纯 **Apache-2.0** 许可，可自由打包进 kinstall 离线 Bundle 且无传染性或托管限制；Elasticsearch 默认官方分发包采用 **Elastic License 2.0（ELv2）**，源码虽自 8.16.0 起增加 AGPLv3 与 SSPL 三选一，但仍限制对外托管且离线再分发需法务确认；
  2. **免费企业能力**：OpenSearch 原生免费内置 RBAC 细粒度权限、审计日志、OIDC/LDAP、告警、ISM 索引生命周期、SQL/PPL、异常检测；Elasticsearch 同类高级安全/告警/跨集群复制特性属于白金商业版；
  3. **运维生态**：OpenSearch K8s Operator 于 2026-09-25 发布 **3.0 GA**，生产级滚动升级与 TLS 管理已齐备；仅当客户业务强绑定 Elastic 专有特性（如 ES|QL、Elastic Security/APM、既有 Elastic 商业订阅）时选择 Elasticsearch。
- **交付与合规策略（ADR-052）**：kinstall 公共离线包默认只集成 OpenSearch 制品；选择 Elasticsearch 时由客户按 `artifacts.yaml` URL 在其自有构建环境拉取官方 `tar.gz` 并自行承担 ELv2 合规审查。

## 2. 版本、许可与 BOM

### 2.1 支持矩阵（截至 2026-11-26）

| 产品与版本线 | 代表版本 | 维护状态 / EOL | 许可证 | 定位 |
| --- | --- | --- | --- | --- |
| **OpenSearch 3.x** | `3.10.0` | 活跃维护（Linux Foundation） | Apache-2.0 | **默认推荐**（含内置 JDK 21+） |
| **OpenSearch 2.19** | `2.19.7` | 维护线 | Apache-2.0 | 存量 2.x 平滑过渡 |
| **Elasticsearch 9.x** | `9.5.7` / `9.4.10` | 仅维护最新两个 minor；9.x 至 ≥ 2027-10-15 | ELv2（默认分发）/ SSPL / AGPLv3 | 可选（新 ES 集群） |
| **Elasticsearch 8.19** | `8.19.26` | 8.x 最终 minor；维护至 **2027-01-15**，支持至 2027-07-15 | ELv2 / SSPL / AGPLv3 | 可选（7.17→8.19→9.x 升级跳板） |
| Elasticsearch 7.17 | `7.17.29` | **2026-01-15 已 EOL** | ELv2 / SSPL | 禁止新建 |

- **架构与页大小（ADR-028）**：OpenSearch 与 Elasticsearch 官方均提供 `linux-x64` 与 `linux-arm64` `tar.gz`（自带捆绑 JDK，目标主机无需预装 Java），支持 amd64 4K 与 arm64 4K/64K 页；开启 `bootstrap.memory_lock: true` 时在 systemd unit 配置 `LimitMEMLOCK=infinity`（容器模式配 `ulimits.memlock: -1`）并核验大页/页对齐无报错。

### 2.2 BOM 示例（OpenSearch 默认）

```yaml
product: opensearch
revision: "2026.11.1"
components:
  opensearch:            { version: "3.10.0", license: Apache-2.0, arch: [amd64, arm64], pageSizes: [4096, 65536] }
  opensearch-dashboards: { version: "3.10.0", license: Apache-2.0, optional: true }
  exporter:              { name: elasticsearch_exporter, version: "1.11.0", license: Apache-2.0 }
  plugins-optional:
    - { name: analysis-ik, version: "3.10.0", offlineZip: true } # 中文分词需离线 zip
```

## 3. 拓扑、角色与规格建议

| 拓扑 | 角色划分与节点数 | 适用场景 | 核心约束 |
| --- | --- | --- | --- |
| `single-node` | 1 台（全角色） | 开发、PoC | `discovery.type: single-node`，无高可用 |
| `converged-3node` | 3 台（`cluster_manager` + `data` + `ingest` 混布）+ 可选 1–2 台 `dashboards` | **中小规模生产默认**（单集群数据 ≤ 3 TB） | 3 台跨 3 故障域，奇数仲裁防脑裂 |
| `dedicated-roles` | 3 台独立 `cluster_manager`（4C/8 GiB）+ ≥ 3 台 `data_hot`（16C/64 GiB/NVMe）+ 可选 `data_warm`/`data_cold` + 2 台 `ingest`/`coordinating` + 2 台 `dashboards` | 大规模日志/搜索集群（> 3 TB 或高并发写入） | 独立 `cluster_manager` 隔离 GC 与磁盘 I/O 抖动，避免主节点失联 |

> 注：OpenSearch 2+/3+ 术语为 `cluster_manager`，Elasticsearch 术语为 `master`，模板按 `product` 自动映射 `node.roles`。

## 4. 部署目标：`host` vs `kubernetes`

- **`host` 模式（默认）**：官方 `tar.gz` 解压至 `/opt/kinstall/bin/opensearch/<version>/`，配置 `/etc/kinstall/opensearch/<instance>/`，数据 `/data/opensearch/<instance>/`（支持多盘 `path.data: [/data1/..., /data2/...]`），unit `kinstall-opensearch-<instance>.service`。
- **`kubernetes` 模式（`06 §3`）**：
  - OpenSearch 采用 **OpenSearch Kubernetes Operator `3.0.0` GA**（2026-09-25，API 组 `opensearch.org/v1`，Apache-2.0），完整支持生产级滚动升级、TLS 轮换与安全缩容；
  - Elasticsearch 采用 **ECK `3.5.0`**（2026-08-04，ELv2 Basic 免费）；
  - K8s 部署要求 Local NVMe PV 或高性能块 CSI（禁止 NFS 挂载 Lucene 数据目录！），并配置反亲和与 `PodDisruptionBudget`。

## 5. 主机要求与容量声明

- **Profile**：`base` + `search`（`03 §6.1`）。
- **`HostRequirements`**：
  - `sysctl`：`vm.max_map_count = 1048576`（ES 8/9 与 OpenSearch 高分片/向量索引推荐，不低于 262144）、`vm.swappiness = 1`、`net.core.somaxconn = 65535`；
  - 必须关闭 swap（`03 §6.8`），systemd unit 设置 `LimitMEMLOCK=infinity`、`LimitNOFILE=65536`、`LimitNPROC=4096`、`TasksMax=infinity`；
  - 端口放行：HTTP REST `9200`、节点传输层 `9300`、Dashboards `5601`、Exporter `9114`。
- **`HostRole.Claims`**：登记上述端口、所有数据盘路径、内存占用 = `JVM Heap × 2`（Lucene 强依赖剩余 50% 系统内存作文件系统页缓存，**绝不可与数据库或缓存混布争抢页缓存**）。
- **磁盘**：优先本地 NVMe / SSD（XFS `noatime`）；**严禁使用 NFS/SMB 等网络文件系统作为 `path.data`**（仅快照仓库可用共享 FS 或 S3）。

## 6. 安装流程（`install` Pipeline）

| 顺序 | Phase | 目标角色 | 核心 Task | 并发 | 失败策略 | 幂等判据 |
| :---: | --- | --- | --- | :---: | --- | --- |
| 1 | `preflight` | 全部 | Profile `search`、`vm.max_map_count >= 262144`、swap off、非 NFS 数据盘、Claims、时钟 < 50 ms | 并行 10 | `failFast` | 只读 |
| 2 | `pki` | 本地 | 每实例独立 CA；签发传输层节点证书（固定 DN 供 `nodes_dn` 校验）、Admin 客户端证书（`admin_dn`）、HTTP TLS 证书 | 本地 | `failFast` | 证书与 DN 匹配 |
| 3 | `install-bin` | 全部 | `user.ensure(opensearch)`、`dir.ensure`、`binary.install`、离线安装声明的插件（如 `analysis-ik` 本地 `file://` zip） | 并行 10 | `failFast` | 版本与插件列表 |
| 4 | `configure` | `cluster_manager` / `data` / `ingest` | `file.sync`（`opensearch.yml`、`jvm.options`、`log4j2.properties`、`pki/*`、systemd unit） | 并行 10 | `failFast` | sha256 校验 |
| 5 | `bootstrap` | 全部 | 按 `cluster_manager` 先行启动全部节点、`wait.port(9300)`；OpenSearch 在首主节点执行 `securityadmin.sh` 初始化安全索引（幂等 check：已初始化则跳过） | 节点并行，安全初始化串行 | `failFast` | `/_cluster/health` 返回 `green`/`yellow` |
| 6 | `dashboards-exp` | `dashboards` / `exporter` | 可选部署 OpenSearch Dashboards（HTTPS）与 `elasticsearch_exporter v1.11.0` | 并行 10 | `continueOnError` | HTTP 200 |
| 7 | `verify` | 本地 | 校验认证生效（匿名 401）、创建探针索引写入检索删除、配置默认 ISM/SLM 策略、登记 `InstalledFile` | 本地 | `failFast` | 探针通过 |

## 7. 配置模板与 JVM 调优（ADR-053）

- **JVM 堆内存公式（ADR-053）**：
  - `HeapGB = min(floor(HostMemGB × 0.5), 31)`，且设置 `-Xms == -Xmx`；
  - 绝不超过 **31 GiB**（具体以 JVM 压缩指针 `CompressedOops` 阈值 ~31.5 GiB 为上限，若单机内存 ≥ 128 GiB，保持 30 GiB 堆将其余留给 Lucene OS 页缓存与向量索引 mmap，或采用多节点实例）；
  - 垃圾回收器统一使用内置 JDK 默认 G1GC，显式配置 `-XX:+HeapDumpOnOutOfMemoryError -XX:HeapDumpPath=/var/log/kinstall/opensearch/<instance>/`。
- **关键 `opensearch.yml.tmpl` 片段**：

```yaml
cluster.name: {{ .Instance.Name }}
node.name: {{ .Host.Name }}
node.roles: {{ toJson .Host.Roles }}
path.data: {{ toJson .Params.DataDirs }}
path.logs: /var/log/kinstall/opensearch/{{ .Instance.Name }}
bootstrap.memory_lock: true
network.host: {{ .Host.PrivateIP }}
discovery.seed_hosts: {{ toJson .Cluster.ManagerAddresses }}
cluster.initial_cluster_manager_nodes: {{ toJson .Cluster.ManagerNames }}

plugins.security.ssl.transport.pemcert_filepath: pki/node.crt
plugins.security.ssl.transport.pemkey_filepath: pki/node.key
plugins.security.ssl.transport.pemtrustedcas_filepath: pki/ca.crt
plugins.security.ssl.transport.enforce_hostname_verification: true
plugins.security.ssl.http.enabled: true
plugins.security.ssl.http.pemcert_filepath: pki/http.crt
plugins.security.ssl.http.pemkey_filepath: pki/http.key
plugins.security.ssl.http.pemtrustedcas_filepath: pki/ca.crt
plugins.security.nodes_dn: ["CN=*.{{ .Instance.Name }}.kinstall.internal,O=kinstall"]
plugins.security.authcz.admin_dn: ["CN=admin.{{ .Instance.Name }}.kinstall.internal,O=kinstall"]
```

## 8. 安全设计

- **强制双层 TLS**：传输层（9300）强制双向 mTLS + `nodes_dn` 白名单（防止未授权节点加入集群）；HTTP 层（9200）强制 TLS 1.2/1.3。
- **凭据与认证**：安装时生成随机强密码写入 `SecretStore`，禁止保留默认弱密码（如 `admin/admin`）；支持对接企业 OIDC / LDAP；内置只读监控角色专供 `elasticsearch_exporter`。
- **审计日志**：OpenSearch 原生免费开启 `plugins.security.audit.type: internal_opensearch` 或写本地 JSON 文件转发集中审计系统（满足等保 180 天留存）。

## 9. 运维操作

- **快照备份与恢复**：
  - 支持注册 **S3 兼容仓库**（SeaweedFS / Ceph RGW / RustFS / Garage，`11`）或共享 NFS（仅用于 snapshot，不可作 `path.data`）；
  - 自动配置 **ISM / SLM** 定时增量快照（Lucene 段级增量）与保留策略；恢复前校验目标索引冲突，支持重命名恢复演练。
- **滚动升级（`serial: true`，零停机标准步骤）**：
  1. 允许路径：OpenSearch `2.19.x → 3.x`（需先按迁移检查清单校验索引兼容性）；ES `8.19.x → 9.x`（7.17 必须先升 8.19，禁止直跳 9.x）；
  2. 强制升级前先打一次全量快照；
  3. 逐节点（顺序：`data` → `ingest` → 非活跃 `cluster_manager` → 当前活跃 `cluster_manager`）：
     - `PUT _cluster/settings` 设 `cluster.routing.allocation.enable: primaries`（防止重启期间触发无谓分片大迁移）；
     - 执行 `POST _flush`；
     - 停止服务 → 切换二进制与离线插件版本 → 启动服务 → 等待节点重新加入；
     - 恢复 `cluster.routing.allocation.enable: all`，等待 `/_cluster/health?wait_for_status=green` 后再处理下一节点。
- **安全缩容（`scale`）**：先通过 `cluster.routing.allocation.exclude._name` 将待下线节点分片迁空 → 确认 `cat/shards` 该节点分片为 0 → 停止并移出配置（若缩减 `cluster_manager`，必须逐台操作维持多数派）。
- **离线插件管理**：中文分词（`analysis-ik`、`analysis-pinyin`）等插件随对应产品精确小版本打包进离线 Bundle，通过 `opensearch-plugin install --batch file:///...` 离线安装，升级时同步替换对应版本插件包。

## 10. 监控、日志与告警规则

使用 `prometheus-community/elasticsearch_exporter v1.11.0`（同时兼容 ES 与 OpenSearch REST API），关键告警模板：

```yaml
groups:
  - name: kinstall.search
    rules:
      - alert: SearchClusterStatusRed
        expr: elasticsearch_cluster_health_status{color="red"} == 1
        for: 2m
        labels: { severity: critical }
      - alert: SearchClusterStatusYellow
        expr: elasticsearch_cluster_health_status{color="yellow"} == 1
        for: 15m
        labels: { severity: warning }
      - alert: SearchJvmHeapHigh
        expr: elasticsearch_jvm_memory_used_bytes{area="heap"} / elasticsearch_jvm_memory_max_bytes{area="heap"} > 0.85
        for: 10m
        labels: { severity: warning }
      - alert: SearchDiskWatermarkApproaching
        expr: (elasticsearch_filesystem_data_size_bytes - elasticsearch_filesystem_data_free_bytes) / elasticsearch_filesystem_data_size_bytes > 0.80
        for: 10m
        labels: { severity: warning }
      - alert: SearchThreadPoolRejected
        expr: increase(elasticsearch_thread_pool_rejected_count[5m]) > 0
        for: 5m
        labels: { severity: warning }
```

## 11. 测试与验收

1. **三节点集群 + 故障注入**：创建 3 主 1 副分片索引并持续写入，强杀 1 台数据节点验证状态 `yellow` 可读写、重启后自动恢复 `green`；强杀活跃 `cluster_manager` 验证 ≤ 10 s 选出新主；
2. **S3 快照与恢复**：写入 10 万文档 → 触发快照 → 删除索引 → 恢复并核对文档数与聚合值；
3. **滚动升级**：小版本滚动升级验证全程 `allocation.enable` 切换无分片风暴且查询可用；
4. **中文分词**：离线安装 `analysis-ik` 并验证 `_analyze` 分词结果。

## 12. 风险与待办

- 第三方社区分词插件（如 `analysis-ik`）往往滞后 OpenSearch/ES 最新小版本数天，BOM 冻结时需核对插件精确版本匹配；
- Elasticsearch 8.19 将于 2027-01-15 结束维护，选 ES 的新集群应直接部署 9.5.x。

## 13. 参考来源

- [OpenSearch 版本历史](https://docs.opensearch.org/latest/version-history/)
- [OpenSearch Kubernetes Operator 3.0 GA（2026-09-25）](https://opensearch.org/blog/opensearch-kubernetes-operator-3-0-is-now-generally-available/)
- [Elasticsearch Releases & EOL](https://acemq.com/blogs/elasticsearch-end-of-life/)
- [Elastic Cloud on Kubernetes（ECK 3.5.0）](https://www.elastic.co/downloads/elastic-cloud-kubernetes)
- [prometheus-community/elasticsearch_exporter v1.11.0](https://github.com/prometheus-community/elasticsearch_exporter/releases)

## 附 A. 本文涉及的决策与清单映射

- **新增 ADR**：
  - **ADR-051**：搜索与日志引擎插件默认 **OpenSearch**（3.10.0，Apache-2.0，Linux Foundation 托管，原生免费包含安全/告警/ISM），**Elasticsearch**（9.5.x / 8.19.x）作为显式可选；K8s 模式分别采用 OpenSearch K8s Operator 3.0 GA 与 ECK 3.5.0（10 §1、§4）。
  - **ADR-052**：遵循许可证合规边界，kinstall 公共离线包仅内置 OpenSearch 制品；使用 Elasticsearch（ELv2 / SSPL / AGPLv3）时由客户侧按 `artifacts.yaml` 自行拉取并完成合规确认（10 §1）。
  - **ADR-053**：搜索节点 JVM 堆按 `min(50% 宿主机内存, 31 GiB)` 锁定（保障 CompressedOops 压缩指针），强制 `bootstrap.memory_lock: true` 与 `vm.max_map_count = 1048576`，禁止将 NFS 作为 `path.data`（10 §5、§7）。
- **关闭设计项**：F2。
- **关闭调研项**：R-MW-01（部署方式与选型全部定稿）。
