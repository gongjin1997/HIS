# 16 插件设计：Apache Kafka（KRaft 无 ZooKeeper）

> **本文定位**：kinstall Apache Kafka 分布式消息队列插件设计。自 **Kafka 4.0（2025-03）起 ZooKeeper 模式已从源码彻底移除**，本插件全面基于 **KRaft 原生共识模式**，覆盖 `host-binary`（官方 tarball + 随包 JDK 21 + systemd）、`host-container`（Docker Compose v5）与 `kubernetes`（Strimzi 1.3.x Operator）三种形态，跨 `amd64` / `arm64`。通用约定遵循「06 中间件插件通用规范」。
>
> **版本**：v1.1（S16/V，2026-11-26；v1.1 对齐 `D3 §4.1` 共享底座 JDK 路径 `/opt/kinstall/runtimes/jdk/temurin-21.0.10-<cpu>/`、清理未映射端口 `9093` 并对齐 ADR-073 堆上限 `6–8 GiB`）。版本号、KIP 状态与 Strimzi 版本截至 2026-11-26，引用前请复核。

## 1. 定位与结论摘要（F7-Kafka、R-MW-07、ADR-073 / ADR-074）

- **版本与架构决策（ADR-073）**：
  - 支持 **Apache Kafka `4.3.2`（默认生产稳定线，2026-10 发布，修复 2 项分层存储/时间索引潜在丢数缺陷）** 与 **`4.4.0`（2026-10 最新线：Share Group DLQ、CIDR ACL、KRaft Controller 注销 KIP-1312）**，兼容 `4.2.x`；**不再支持 ZooKeeper 模式（4.0 起已移除），也不再新建 3.x 集群**；
  - 全部生产多盘节点启用 **KRaft JBOD（KIP-858，自 3.8 起生产可用）**，支持 Broker 挂载多块独立 JBOD 数据盘、单盘故障仅影响该盘分区而不拖垮整台 Broker；
  - **内置/随包 OpenJDK 21 LTS**（Temurin 或毕昇 JDK arm64 优化版），复用底座 `D3 §4.1` 共享运行时 `/opt/kinstall/runtimes/jdk/temurin-21.0.10-<cpu>/`，不依赖宿主机 JDK 且不重复随组件目录打包（ADR-068）。
- **生态配套选型（ADR-074）**：
  - K8s 模式默认采用 CNCF 孵化项目 **Strimzi Kafka Operator ≥ `1.3.2`**（1.0+ 仅用 `v1` CRD，强制 ≥ `1.0.1` 修复 `CVE-2026-55226` Entity Operator RBAC 过宽漏洞）；
  - 分区自动均衡（可选）：**Cruise Control**（2026 年已捐赠 Linux Foundation，Apache-2.0）；
  - Web 控制台（可选）：**Kafbat UI**（原 `provectus/kafka-ui` 已停止活跃维护，Kafbat 为社区活跃 Apache-2.0 分支，全功能免费含 RBAC/ACL/Schema）；
  - 监控双轨：**`prometheus/jmx_exporter 1.7.0`**（Broker/Controller 内部指标，Java Agent 模式）+ **`danielqsj/kafka_exporter v1.11.0`**（Consumer Group Lag 外部指标）。

## 2. 版本、许可与 BOM（截至 2026-11-26）

| 组件 | 版本 | 发布日期 | 许可证 | 架构与页大小 |
| --- | --- | --- | --- | --- |
| Apache Kafka | `4.3.2`（默认）/ `4.4.0`（最新） | 2026-10 | Apache-2.0 | JVM 字节码 + 原生 JNI（amd64/arm64，4K/64K 页均兼容） |
| 随包 OpenJDK | `21 LTS`（Temurin / Bisheng） | 持续维护 | GPLv2+CE | `amd64` / `arm64` |
| Strimzi Kafka Operator | `1.3.2`（要求 K8s ≥ 1.30） | 2026-11 | Apache-2.0 | `amd64` / `arm64` |
| `prometheus/jmx_exporter` | `1.7.0` | 2026-10-05 | Apache-2.0 | JVM Agent Jar |
| `danielqsj/kafka_exporter` | `v1.11.0` | 2026-10-23 | Apache-2.0 | Go 单二进制（amd64/arm64） |
| Kafbat UI（可选） | 最新稳定版 | 2026 活跃 | Apache-2.0 | OCI 镜像 / Jar |
| Cruise Control（可选） | `3.1+`（LF Apache-2.0） | 2026 | Apache-2.0 | JVM（amd64/arm64） |

## 3. 拓扑与角色规划

| 拓扑 | 节点组成与 `process.roles` | 适用场景 | 核心约束 |
| --- | --- | --- | --- |
| `single-node` | 1 台（`broker,controller`） | 开发测试、边缘 PoC | 无高可用，副本因子 1 |
| `combined-3node` | 3 台（每台同时承担 `broker,controller`，独立划分元数据盘与数据盘） | **中小规模生产默认**（吞吐 < 200 MB/s、分区数 < 5,000） | 3 台跨 3 故障域；元数据目录 `metadata.log.dir` 必须放独立 SSD，与业务消息盘隔离防刷盘抖动影响 Raft 心跳 |
| `dedicated-kraft` | **3（或 5）台独立 `controller`**（4C/8 GiB/NVMe）+ **≥ 3 台独立 `broker`**（16C/64 GiB/多块 JBOD NVMe 或大容量 SSD） | **中大规模核心生产推荐**（高吞吐、> 5,000 分区、需独立扩缩容 Broker） | Controller 与 Broker 彻底隔离；Broker 可在线水平扩容不影响 Raft 仲裁；支持 `min.insync.replicas=2` + `default.replication.factor=3` |

## 4. 三种部署形态对比

- **`host-binary`（生产核心默认）**：解压官方 `kafka_2.13-4.3.2.tgz` 至 `/opt/kinstall/bin/kafka/4.3.2/`，配合底座共享 `/opt/kinstall/runtimes/jdk/temurin-21.0.10-<cpu>/`，unit 名 `kinstall-kafka-<instance>-controller.service` / `kinstall-kafka-<instance>-broker.service`，每块数据盘独立 XFS 挂载 `/data/kafka/<instance>/disk{1..N}`，零容器网络转发损耗，充分吃满宿主机 Linux Page Cache 零拷贝（`sendfile`）。
- **`host-container`（Docker Engine 29.10 + Compose v5.8）**：使用官方 `apache/kafka:4.3.2`（多架构 amd64/arm64 镜像）渲染 Compose + systemd 守护，绑定宿主机多盘目录与 `network_mode: host`（避免桥接 NAT 影响吞吐与 `advertised.listeners` 映射），极适合边缘/开发与快速标准化交付。
- **`kubernetes`（Strimzi 1.3.2 Operator）**：通过 Helm v4 SDK 安装 Strimzi 1.3.2，生成 `Kafka`（`kafka.strimzi.io/v1`）+ `KafkaNodePool`（分 `controller` 与 `broker` 池，启用 KRaft JBOD 多 PVC）+ `KafkaUser` + `KafkaTopic` + 可选内置 Cruise Control。

## 5. 主机要求与容量声明

- **Profile**：`base` + 新增 `messaging-kafka` 调优参数（或叠加在 `base` 上经 `HostRequirements` 下发）：
  - `sysctl`：`vm.swappiness = 1`、`vm.dirty_background_ratio = 5`、`vm.dirty_ratio = 60`（Kafka 强依赖 OS Page Cache 异步顺序刷盘）、`vm.max_map_count = 262144`、`net.core.wmem_max = 16777216`、`net.core.rmem_max = 16777216`、`net.ipv4.tcp_wmem = 4096 65536 16777216`、`net.ipv4.tcp_rmem = 4096 65536 16777216`、`net.core.somaxconn = 65535`；
  - `limits`：`nofile >= 131072`（每分区多个 segment + index 文件句柄）；
  - **磁盘（JBOD）**：多块独立物理盘直接挂载 XFS `noatime,nodiratime`（**禁止用硬件 RAID5/6**，由 Kafka 自身 3 副本提供高可靠，JBOD 吞吐与成本最优）；
  - 端口放行：Client SASL_SSL `9092`、Inter-Broker `9094`、Controller `9095`、JMX Exporter `9408`、`kafka_exporter` `9308`、Kafbat UI `8080`。
- **`HostRole.Claims`**：登记上述端口、全部 JBOD 数据盘与元数据盘、内存（**JVM 堆严格限制小内存，其余全部留给 OS Page Cache**，见 §7）。

## 6. 安装流程（`install` Pipeline）

| 顺序 | Phase | 目标角色 | 核心 Task | 并发 | 失败策略 | 幂等判据 |
| :---: | --- | --- | --- | :---: | --- | --- |
| 1 | `preflight` | 全部 | Profile 与 sysctl 检查、多盘 XFS 校验、时钟偏差 < 50 ms、Claims 端口检查、生成持久 `cluster.id`（存 `instances.state`） | 并行 10 | `failFast` | 只读 |
| 2 | `pki` | 本地 | 实例独立 CA；签发每节点 PKCS12 keystore/truststore（含双栈 IP/域名 SAN，密码存 `SecretStore`） | 本地 | `failFast` | keystore 指纹 |
| 3 | `install-bin` | 全部 | `user.ensure(kafka)`、`binary.install(jdk21)`、`binary.install(kafka)`、`binary.install(jmx_exporter)`、`exec.allowlist` | 并行 10 | `failFast` | 版本软链 |
| 4 | `configure-format` | `controller` / `broker` | `file.sync`（`server.properties`、JAAS、keystore/truststore、jmx 规则、systemd unit）；执行 `kafka-storage.sh format --cluster-id <id> --ignore-formatted --add-scram ...`（KIP-900 启动即注入初始 SCRAM 管理员） | 并行 10 | `failFast` | `meta.properties` 存在且 `cluster.id` 匹配 |
| 5 | `start-controllers` | `controller` | 启动 3 节点 KRaft Controller、`wait.port(9095)`、校验 `kafka-metadata-quorum.sh describe --status` 选出 Leader | 并行 3 | `failFast` | Quorum Leader 健康 |
| 6 | `start-brokers` | `broker` | 启动全部 Broker、`wait.port(9092)`、验证全部 Broker 注册到 Controller | 并行 10 | `failFast` | `broker-api-versions` 全在线 |
| 7 | `deploy-obs-ui` | 辅助节点 | 部署 `kafka_exporter v1.11.0`、可选 Kafbat UI 与 Cruise Control | 并行 10 | `continueOnError` | `/metrics` 200 |
| 8 | `verify` | 本地 | 创建 3 副本探针 Topic → 启用 SASL_SSL 生产/消费 100 条消息校验内容一致 → 删除探针 Topic → 登记 `InstalledFile` | 本地 | `failFast` | 端到端收发通过 |

## 7. 配置模板与 JVM / Page Cache 调优

- **JVM 堆铁律**：**Kafka 堆内存绝不是越大越好！** 推荐固定 **`-Xms6g -Xmx6g`**（严格不超过 **6–8 GiB**，与 ADR-073 一致，宿主机 32–64 GiB 内存时把 **75%–85% 留给 Linux Page Cache** 实现零拷贝读写），GC 使用 G1GC（JDK 21 下 `-XX:+UseG1GC -XX:MaxGCPauseMillis=20 -XX:InitiatingHeapOccupancyPercent=35`）。
- **核心 `server.properties.tmpl` 片段（独立 Broker / 融合模式通用骨架）**：

```properties
process.roles={{ .Host.KafkaRoles }}                # controller | broker | broker,controller
node.id={{ .Host.NumericID }}
controller.quorum.voters={{ .Cluster.QuorumVoters }} # 1@10.0.1.11:9095,2@10.0.1.12:9095,3@10.0.1.13:9095
listeners=SASL_SSL://{{ .Host.PrivateIP }}:9092,INTERNAL://{{ .Host.PrivateIP }}:9094{{ if .Host.IsController }},CONTROLLER://{{ .Host.PrivateIP }}:9095{{ end }}
advertised.listeners=SASL_SSL://{{ .Host.AdvertiseAddr }}:9092,INTERNAL://{{ .Host.PrivateIP }}:9094
listener.security.protocol.map=SASL_SSL:SASL_SSL,INTERNAL:SASL_SSL,CONTROLLER:SASL_SSL
inter.broker.listener.name=INTERNAL
controller.listener.names=CONTROLLER

# KRaft JBOD 多数据盘 + 独立元数据盘
metadata.log.dir=/data/kafka/{{ .Instance.Name }}/metadata
log.dirs={{ join "," .Host.DataDirs }}

# 高可靠基线
num.partitions=6
default.replication.factor=3
min.insync.replicas=2
unclean.leader.election.enable=false                # 严禁非 ISR 选举导致丢消息
offsets.topic.replication.factor=3
transaction.state.log.replication.factor=3
transaction.state.log.min.isr=2
broker.rack={{ .Host.Labels.zone }}                 # 按可用区/机架反亲和摆放副本

# 安全（TLS + SCRAM-SHA-512 + StandardAuthorizer）
sasl.enabled.mechanisms=SCRAM-SHA-512
authorizer.class.name=org.apache.kafka.metadata.authorizer.StandardAuthorizer
allow.everyone.if.no.acl.found=false
super.users=User:kinstall-admin
ssl.keystore.type=PKCS12
ssl.keystore.location=/etc/kinstall/kafka/{{ .Instance.Name }}/pki/kafka.keystore.p12
ssl.truststore.type=PKCS12
ssl.truststore.location=/etc/kinstall/kafka/{{ .Instance.Name }}/pki/kafka.truststore.p12
```

## 8. 安全设计

- **认证与鉴权**：生产默认 `SASL_SSL`（TLS 1.3 + `SCRAM-SHA-512`，KIP-900 直接存 KRaft 元数据无需外部存储）；启用 `StandardAuthorizer`（`allow.everyone.if.no.acl.found=false`）按业务账号精确授予 Topic 读/写与 ConsumerGroup ACL（Kafka 4.4 支持 CIDR 网段 ACL KIP-1276）。
- **凭据安全**：keystore/truststore 密码与 SCRAM 口令全部托管于平台 `SecretStore`；支持 `rotate-certs` 利用 Kafka 动态配置 `kafka-configs.sh --alter` 零重启热替换 TLS 证书！

## 9. 运维操作

| 操作 | 实现要点 |
| --- | --- |
| `scale`（扩缩 Broker） | 扩容：新 Broker 加入并自动分配 `node.id` + `broker.rack` → 通过内置分区重分配计划（`kafka-reassign-partitions.sh --throttle` 限速，或调用 Cruise Control）渐进均衡分区；**缩容**：先执行分区迁出且确认该 Broker 分区数为 0 再安全停机注销（4.4 支持 `kafka-cluster.sh unregister`） |
| `upgrade`（零停机滚动升级） | 两阶段标准协议（如 `4.2 → 4.3.2 → 4.4.0`）：①`serial: true` 逐台滚动升级全部 Broker 与 Controller（先确认 `UnderReplicatedPartitions == 0` 再切下一台）；②观察稳定后执行 `kafka-features.sh upgrade --metadata <new_version>` 提交最终元数据版本（提交前支持平滑回退二进制） |
| `backup` / 跨机房复制 | 消息队列不靠磁盘快照备份；跨机房灾备与迁移通过内建 **MirrorMaker 2** 插件角色异步复制 Topic 数据、ACL 与消费组偏移量 |
| `reconfigure` | 支持动态参数（限流、日志保留时间 `log.retention.ms`、SSL 证书）直接 `kafka-configs.sh --alter` 生效；静态参数走先确认 ISR 健康再逐台滚动重启 |

## 10. 监控、日志与告警规则

组合 `prometheus/jmx_exporter 1.7.0`（Broker/Controller 内部指标）+ `kafka_exporter v1.11.0`（Consumer Lag）：

```yaml
groups:
  - name: kinstall.kafka
    rules:
      - alert: KafkaUnderReplicatedPartitions
        expr: kafka_server_replicamanager_underreplicatedpartitions > 0
        for: 5m
        labels: { severity: critical }
      - alert: KafkaOfflinePartitions
        expr: sum by (kinstall_instance) (kafka_controller_kafkacontroller_offlinepartitionscount) > 0
        for: 1m
        labels: { severity: critical }
      - alert: KafkaActiveControllerAbnormal
        expr: sum by (kinstall_instance) (kafka_controller_kafkacontroller_activecontrollercount) != 1
        for: 1m
        labels: { severity: critical }
      - alert: KafkaConsumerGroupLagHigh
        expr: sum by (kinstall_instance, consumergroup, topic) (kafka_consumergroup_lag) > 100000
        for: 10m
        labels: { severity: warning }
      - alert: KafkaLogDirOffline
        expr: kafka_log_logmanager_offlinelogdirectorycount > 0
        for: 1m
        labels: { severity: critical }
```

## 11. 测试与验收

1. **高可靠故障注入**：持续以 `acks=all` 生产消息期间强杀 1 台 Broker 与 1 台非 Leader Controller，验证零消息丢失、零生产失败；再卸载 JBOD 其中 1 块盘，验证其余盘分区继续服务且触发 `KafkaLogDirOffline` 告警；
2. **吞吐压测基线**：运行 `kafka-producer-perf-test.sh` 与 `kafka-consumer-perf-test.sh` 记录吞吐与 P99 延迟；
3. **三形态与双架构**：验证 `host-binary`（amd64/arm64）、`host-container`（Docker Compose）与 K8s Strimzi 1.3.2 均安装幂等通过。

## 12. 参考来源

- [Apache Kafka 4.4 / 4.3.2 / 4.0 Release & KRaft 文档](https://kafka.apache.org/blog/)
- [Strimzi Kafka Operator 1.3.2 & CVE-2026-55226](https://github.com/strimzi/strimzi-kafka-operator/releases)
- [Cruise Control（Linux Foundation Apache-2.0）与 Kafbat UI](https://github.com/conduktor/awesome-kafka)
- [prometheus/jmx_exporter 1.7.0](https://github.com/prometheus/jmx_exporter) & [kafka_exporter v1.11.0](https://github.com/danielqsj/kafka_exporter)

## 附 A. 本文涉及的决策与清单映射

- **新增 ADR**：
  - **ADR-073**：Kafka 插件全面采用 **Apache Kafka 4.x KRaft 无 ZooKeeper 架构**（默认 `4.3.2`、可选 `4.4.0`，启用 KIP-858 多盘 JBOD 与随包 OpenJDK 21），不再部署 ZooKeeper；JVM 堆锁定 6–8 GiB 把绝大部分内存留给 OS Page Cache（16 §1、§7）。
  - **ADR-074**：Kafka K8s 模式默认 **Strimzi ≥ 1.3.2**（强制修复 `CVE-2026-55226`）；可选生态采用 Linux Foundation **Cruise Control**（均衡）与 **Kafbat UI**（Apache-2.0，替代原停维 kafka-ui）；监控采用 `jmx_exporter 1.7.0` + `kafka_exporter v1.11.0` 双轨（16 §1、§10）。
- **关闭设计项**：F7（Kafka 部分落地，其余 Nacos/RocketMQ 列远期）。
- **推进调研项**：R-MW-07（Kafka 已完成定稿）。
