# RFC-M5-24：GPU/NPU 算力调度、LoongArch64 架构与 Nacos 生态远期评估（ADR 预研结论）

> **对应任务**：`T-M5-24`（`E21` / `E22` / `F7`） · **状态**：Accepted (Deferred to v1.1+ Roadmap)

## 1. GPU / NPU（NVIDIA + 昇腾 Ascend）与 AI Reranker（E21）
- **架构结论**：以 **Kubernetes Extension Plugin + HostInit Profile (`gpu-nvidia` / `npu-ascend`)** 形态接入，绝不将闭源厂商驱动耦合进主二进制。
- **离线交付策略**：驱动安装包与容器工具链（NVIDIA Container Toolkit / Ascend Docker Runtime）受厂商 EULA 约束，归入 `LicenseTier: red`，仅通过客户私有 Local/OBS 介质源（ADR-077）解析，不进入公共离线 Bundle（红线 10）。
- **AI Reranker / 推理服务**：复用 `P10 generic-app` 与 `Milvus 3.x` Stack 编排能力，由模型权重只读 PVC / OBS 挂载供给。

## 2. LoongArch64（龙芯 3C5000/3C6000）组件构建（E22）
- **控制面本体**：Go 1.27 原生支持 `GOOS=linux GOARCH=loong64 CGO_ENABLED=0`，控制面与本地代理可零改造交叉编译。
- **生态短板**：部分核心中间件（OpenSearch JDK/JNI、Valkey jemalloc 16K 页、Patroni/pgBackRest、containerd 镜像生态）上游尚未提供官方长期维护 `loong64` 二进制/OCI Index。
- **落地路径**：v1.1 设立 `Tier-2-Experimental` 构建矩阵，优先落地纯 Go/静态链路（`kinstall` 本体、etcd、Kubernetes、SeaweedFS、BanyanDB），其余组件待上游 CI 稳定后纳入 BOM。

## 3. Apache Nacos 注册与配置中心（F7）
- **评估结论**：建议作为 **二期 OCI 声明式插件包（`T-M5-21`）或独立 `P9` 扩展组件** 提供，依赖底座 `runtime/jdk` 21 与 `P5 MySQL 8.4/9.7`（或 `P4 PostgreSQL`）持久化存储，默认强制开启 `nacos.core.auth.enabled=true` 与随机 `token.secret.key`（通过 `credentials` 信封加密注入，零明文默认值）。
