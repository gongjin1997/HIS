# kinstall Web 控制台设计与前端工程规范（`WEB_DESIGN.md`）

> **本文定位**：kinstall Web 管理控制台的**完整独立信息架构、业务交互与前端工程落地规范**（正式关闭设计项 **B9** 并定稿 **ADR-023**），直接面向产品、UI 与前端研发团队。
>
> **视觉与组件样式唯一基准**：全量色彩 Token、暗/亮双主题、排版字距、Element Plus `2.14.2` 主题覆写、12 大基础 `Ki*` 组件与 8 大领域复合组件视觉规范、无障碍与 16 条 PR 红线，请以独立姊妹文档 **[`DESIGN.md`](./DESIGN.md)** 为唯一基准！
>
> **交付形态**：纯静态 SPA 构建产物通过 `//go:embed web/dist/**` 内嵌进 `kinstall` 单二进制（`CGO_ENABLED=0`），**100% 离线零外链 CDN**（字体、图标、主题全部本地内联打包），在完全物理隔离的涉密/金融内网浏览器中秒开可用；构建标签 `-tags=noui` 可一键产出极简 CLI 二进制。
>
> **版本**：v1.3（2026-11-26，与 `DESIGN.md` v2.0 100% 对齐；v1.3 同步 `offlineStrict` 允许 `internal: true` 内网白名单 OBS、`operation.done` 无 `id:` 行注释、§5 补齐首屏 JS ≤ 350 KiB / Lighthouse ≥ 90 / 100,000 行 ≥ 60 fps 门禁）。所有前端依赖与安全基线（Vue `3.5.45`、Element Plus `2.14.2`、Pinia `3.0.4`、Vue Flow 2.0 `xyflow`、`@hey-api/openapi-ts` 规避 `CVE-2026-108551`）均于 2026-11 实查核验。

---

## 1. 设计哲学、角色视角与信息架构（IA）

### 1.1 设计原则（面向硬核基础设施运维）

1. **安全确定性优先于炫酷动效**：一切生产变更强制两段式视觉流（`Configure → Plan 审查（3-Way Diff + Preflight）→ Approve（如需）→ Apply 实时终端`），危险操作（销毁/强覆漂移/恢复 etcd）采用**输入实例名确认 + 醒目风险摘要**，杜绝误点。
2. **高信息密度而不杂乱**：单屏清晰呈现主机架构（`amd64` / `arm64-4K` / `arm64-64K`）、OS 发行版、部署形态（`host-binary` / `host-container` / `kubernetes`）、端口/磁盘占用与健康态，采用等宽数字（`tabular-nums`）对齐 IP、端口、耗时与内存容量。
3. **契约驱动动态扩展**：前端**不硬编码每个中间件插件的参数表单**；所有插件（K8s、Valkey、PG、MySQL、ELK+Kafka、SkyWalking、SeaweedFS、Harbor、RocketMQ、Milvus 等）通过统一 JSON Schema + `x-kinstall-ui` 协议动态渲染专业表单。
4. **纯内网离线零退化**：严禁加载 Google Fonts、CDN JS/CSS 或外网头像；所有图标、JetBrains Mono / HarmonyOS Sans 子集字体与状态插画均内嵌于 `web/dist/`。

### 1.2 信息架构与完整路由树

```text
/ (Global Shell: 左侧 224px 可折叠主导航 + 顶部 48px 上下文栏 [项目/环境切换 · ⌘K 全局命令 · 待审批铃铛 · 主题/语言 · 用户])
├── /dashboard                          # 全局基础设施驾驶舱（健康总览、架构/OS 分布、到期证书、近期变更）
├── /hosts                              # CMDB 主机管理
│   ├── /hosts/list                     # 主机列表（多维筛选、批量导入 .xlsx/.csv、快捷 Facts 刷新）
│   ├── /hosts/:id                      # 主机详情（1-RTT Facts、端口/目录/子网 Claims、已装实例、历史漂移）
│   └── /hosts/init                     # 主机初始化 Profile 编排与合规 verify 巡检
├── /instances                          # 基础设施实例（K8s 与中间件）
│   ├── /instances/catalog              # 插件市场（按核心/DevOps/消息/AI向量分类，展示三元形态徽章）
│   ├── /instances/create/:product      # 多步安装向导（拓扑选型 → 节点角色映射 → 动态参数表单 → Plan 预览）
│   └── /instances/:id                  # 实例详情（概览/拓扑、Spec 与历史版本 Diff、监控入口、运维动作）
├── /stacks                             # 复合应用栈（Stack DAG）
│   ├── /stacks/list                    # Stack 列表（如 prod-elk-kafka-s3、ai-milvus-stack）
│   └── /stacks/:id/canvas              # ★ Vue Flow 2.0 交互式 DAG 拓扑编排与跨实例输出连线画布
├── /clusters                           # 多集群统一管理（K8s 专属视图：节点池、Addon、短期 kubeconfig 签发、Headlamp）
├── /operations                         # 操作与实时执行中心
│   ├── /operations/list                # 操作流水（含筛选：Running / AwaitingApproval / Failed）
│   ├── /operations/:id                 # ★ 执行详情页（Phase 矩阵 + SSE 多主机分屏终端 + 报告导出）
│   └── /operations/approvals           # ★ 四眼审批工作台（待我审批 / 我发起的 / 审批历史）
├── /artifacts                          # 供应链与介质中心
│   ├── /artifacts/media-policies       # ★ 三元介质源配置（本地 Local / 华为云 OBS·S3 桶 / 在线代理 + 探活 + sync-to-obs）
│   ├── /artifacts/bundles              # 离线 Bundle / 增量补丁导入、Zot 镜像镜像源、BOM revision 列表
│   └── /artifacts/runtimes             # 底座共享 Runtime（JDK 8/17/21/25、Maven、Node.js）与离线插件闭包
├── /security                           # 安全、凭据与合规中心
│   ├── /security/credentials           # 凭据与密钥库（SSH/OBS AK·SK/OpenBao KEK 状态与在线轮换，永不显示明文）
│   ├── /security/audit                 # 审计日志与哈希链完整性校验（SHA-256 / SM3 实时验证按钮）
│   └── /security/compliance            # 等保 2.0 三级 / 密评 / CIS 报告生成与证据包下载
└── /settings                           # 平台管理（仅 admin / project-admin）
    ├── /settings/projects              # 项目、环境、RBAC 角色绑定、高危审批策略与维护窗口
    ├── /settings/integrations          # 外部 CMDB 同步、GitOps Webhook、企微/钉钉/飞书/邮件通知规则
    └── /settings/system                # 平台多副本 HA 状态、DB Schema 版本、API Token 管理
```

---

## 2. 视觉设计系统与 Design Tokens（完整像素基准见 [`DESIGN.md`](./DESIGN.md)）

### 2.1 视觉基调：“工业精密控制室（Industrial Precision）”

定位为类似 Linear / Grafana / Cloudflare 融合的专业运维控制台，默认提供 **深空石板暗色主题（Dark Default，适合大屏与夜间值守）** 与 **高对比亮色主题（Light，适合白天审计与大纲查阅）** 一键切换。完整色彩、排版、间距、Element Plus `2.14.2` 主题桥接、20 个基础与领域组件规格及 16 条 PR 视觉红线见 **[`DESIGN.md`](./DESIGN.md)**。

### 2.2 核心 CSS Design Tokens 速览（完整定义见 `DESIGN.md §6.1`）

```css
:root,
[data-theme="dark"] {
  /* 背景层级（4 阶明度递进，禁止纯黑 #000000 造成高对比刺眼） */
  --ki-bg-canvas: #0b0f17;        /* 最底层画布 */
  --ki-bg-surface: #111827;       /* 卡片 / 侧边栏 / 表格底色 */
  --ki-bg-elevated: #182235;      /* 弹窗 / 下拉 / 悬浮抽屉 */
  --ki-bg-subtle: #1f2a3e;        /* Hover / 次级底色 */
  --ki-bg-terminal: #080c12;      /* SSE 终端与代码对比器专属底色 */

  /* 边框与分割线 */
  --ki-border-subtle: #1e293b;
  --ki-border-default: #334155;
  --ki-border-focus: #3b82f6;

  /* 文本层级（WCAG AA >= 4.5:1） */
  --ki-text-primary: #f1f5f9;
  --ki-text-secondary: #94a3b8;
  --ki-text-muted: #8794a8;       /* v1.2：#64748b 在 surface 上仅 3.73:1，已上调至 ≥4.5:1 */

  /* 品牌主色与语义色（严格区分信息/成功/漂移告警/高危阻断） */
  --ki-brand: #3b82f6;            /* 精密蓝 */
  --ki-brand-hover: #60a5fa;
  --ki-status-ok: #10b981;        /* Succeeded / Healthy / Online */
  --ki-status-running: #0ea5e9;   /* Running / Pulsing */
  --ki-status-warn: #f59e0b;      /* AwaitingApproval / Drift / 64K 提示 */
  --ki-status-danger: #ef4444;    /* Failed / Blocked / CVE Critical */
  --ki-status-neutral: #64748b;   /* Skipped / Retired */

  /* 架构与部署形态专属徽章色（冷静冷色系，杜绝暗底紫红霓虹） */
  --ki-badge-binary-bg: rgba(59, 130, 246, 0.14);
  --ki-badge-container-bg: rgba(6, 182, 212, 0.14);
  --ki-badge-k8s-bg: rgba(20, 184, 166, 0.14);
  --ki-badge-arm64k-bg: rgba(245, 158, 11, 0.18); /* 64K 页主机强提醒徽章 */

  /* 字体栈（离线自托管 WOFF2 子集 + 系统回退） */
  --ki-font-sans: "Inter", "HarmonyOS Sans SC", "PingFang SC", "Microsoft YaHei", "Noto Sans CJK SC", "Source Han Sans SC", "WenQuanYi Micro Hei", system-ui, sans-serif;
  --ki-font-mono: "JetBrains Mono", "Cascadia Code", "SFMono-Regular", Consolas, "Noto Sans Mono CJK SC", ui-monospace, monospace;

  /* 栅格与圆角（克制偏直角的工业风） */
  --ki-radius-sm: 4px;
  --ki-radius-md: 6px;
  --ki-radius-lg: 8px;
  --ki-sidebar-w: 224px;
  --ki-topbar-h: 48px;
}

[data-theme="light"] {
  --ki-bg-canvas: #f6f8fa;
  --ki-bg-surface: #ffffff;
  --ki-bg-elevated: #ffffff;
  --ki-bg-subtle: #f1f5f9;
  --ki-bg-terminal: #0f172a;      /* 亮色模式下终端仍保持深色提升 ANSI 日志可读性 */
  --ki-border-subtle: #e2e8f0;
  --ki-border-default: #cbd5e1;
  --ki-text-primary: #0f172a;
  --ki-text-secondary: #475569;
  --ki-text-muted: #5b6b82;
  --ki-brand: #2563eb;
  --ki-brand-hover: #1d4ed8;
}
```

### 2.3 排版与高密度表格规范

- **字号阶梯**：页标题 `18px / 600`、区块标题 `14px / 600`、正文与表格单元格 `13px / 400`、次要标签/时间戳 `12px / 400`、终端与代码 Diff `12.5px / 1.55 line-height`（开启 `font-variant-numeric: tabular-nums`）。
- **运维表格铁律**：行高紧凑 `40px`（支持切换 `32px` 极密模式）；IP、端口、SHA-256 摘要、ULID 一律使用 `--ki-font-mono` 并附悬停一键复制按钮；所有列表统一采用游标分页（对接后端 `?limit=50&cursor=...`）。

---

## 3. 核心页面高保真布局与交互设计规范

### 3.1 全局基础设施总览驾驶舱（`/dashboard`）

```text
+---------------------------------------------------------------------------------------------------+
| [kinstall]  项目: [core-ops v]  环境: [prod v]     [ ⌘K 搜索主机/实例/操作... ]   🔔(2)  🌙  alice |
+-------------+-------------------------------------------------------------------------------------+
| ◉ 总览驾驶舱 |  [ 主机纳管 128 台 ]  [ 运行实例 24 个 ]  [ 待审批变更 2 项 ]  [ 30天到期证书 1 ]   |
|   CMDB 主机  |   amd64: 92 | arm64: 36 (含64K: 8)   K8s:3 | 中间件:21       [立即处理 →]           |
|   实例与插件 |                                                                                     |
|   Stack 拓扑 |  +--- 部署形态与架构分布 ----------------+  +--- 近期操作与实时进度 ----------------+ |
|   多集群管理 |  | host-binary    ████████████  14      |  | 🔵 op_01J... k8s-prod upgrade  68%    | |
|   执行中心   |  | host-container ██████        7       |  | 🟡 op_01J... prod-elk apply  待审批  | |
|   介质供应链 |  | kubernetes     ███           3       |  | 🟢 op_01J... valkey-ha plan  已收敛  | |
|   安全合规   |  +--------------------------------------+  +----------------------------------------+ |
|   系统设置   |  +--- 风险与漂移巡检告警（点击直达修复）---------------------------------------------+ |
|             |  | ⚠ node-kunpeng-04 (arm64-64K): 检测到外部手工修改 /etc/sysctl.d/99-custom.conf   | |
|             |  | ⚠ obs-prod-bucket: 最近一次 sync-to-obs 完成 (2026-11-26 07:30, 100% SHA256 OK)  | |
+-------------+-------------------------------------------------------------------------------------+
```

### 3.2 制品介质源配置中心（`/artifacts/media-policies`，对接 ADR-077）

专为满足**本地 / 在线 / 华为云 OBS 桶三源灵活切换**设计的专属控制面：

- **顶部安全红线开关**：`严格物理离线模式（offlineStrict）` —— 开启后界面视觉出现“物理隔离保护中”蓝锁横幅，自动置灰禁用在线源与非内网白名单 OBS 桶配置（仅保留本地与 `internal: true` 内网白名单桶），后端 API 同步硬拦截（`KI-SRC-0005`）。
- **拖拽优先级卡片链（`priority`）**：
  - 卡片 1：`[1] 本地离线仓库与缓存池（Local）` —— 展示已导入 Bundle 列表、缓存池占用进度条（如 `34.2 / 100 GiB`）、一键 `清理零引用缓存（prune）`。
  - 卡片 2：`[2] 对象存储桶（华为云 OBS / 阿里云 OSS / 腾讯云 COS / S3 / SeaweedFS）`：
    - 厂商快速预设下拉（选中**华为云 OBS**自动填充 `obs.<region>.myhuaweicloud.com`、`addressingStyle=virtual-host` 提示，并标注“华为云 2026 新桶默认私有，支持加密 AK/SK 或 ECS IAM 委托”）；
    - 驱动切换：`S3v4 兼容驱动（默认，内置 WhenRequired 补丁）` vs `华为云原生 OBS 驱动（HCS/PFS 专享）`；
    - 交付模式单选：`部署机中转缓存（relay-cache，目标机零出网）` vs `同 VPC 预签名直拉（direct-presigned，15 分钟短效 + 日志自动脱敏）`；
    - 操作栏：**`[测试连通与读取权限]`**（调 `:test` 实时返回 `HeadBucket`、耗时与写权限探测）+ **`[从本地一键同步至 OBS 桶]`**（触发异步 `sync-to-obs` Operation 并跳转实时进度）。
  - 卡片 3：`[3] 在线官方源与代理（Online）` —— 配置 `HTTP/HTTPS/SOCKS5` 代理、出网域名白名单列表（防 SSRF）、强制校验 BOM SHA-256 开关（默认锁定开启）。

### 3.3 JSON-Schema 动态插件表单协议（`x-kinstall-ui`）

每个插件（内置 Go 插件或声明式 OCI 包）只需提供一份标准 JSON Schema 2020-12（`plugins/<name>/schema/params.json`），前端 `DynamicSchemaForm.vue` 自动渲染出完整企业级表单，**新增插件零前端发版**！

**约定的 7 个 `x-kinstall-ui` 专用 UI 扩展控件**：

| 扩展关键字 | 渲染控件 | 典型用法 |
| --- | --- | --- |
| `x-kinstall-ui.group` | 表单分节 / 折叠高级参数面板（`basic` / `topology` / `storage` / `security` / `advanced`） | 隐藏 80% 专家参数，首屏清爽 |
| `x-kinstall-ui.widget: "host-role-picker"` | 可视化主机池角色分配器（自动高亮架构 `amd64`/`arm64-64K`、剩余内存、端口冲突预警） | 为 Valkey Cluster 选 6 台主机并自动做机架反亲和校验 |
| `x-kinstall-ui.widget: "credential-select"` | 平台加密凭据库选择器 + 行内“新建加密凭据”抽屉（表单值仅传 `cred_01J...`） | 选 OBS AK/SK、SSH 凭据或 S3 备份凭据 |
| `x-kinstall-ui.widget: "deploy-target-cards"` | 三元目标卡片（`host-binary` / `host-container` / `kubernetes`），自动灰显当前插件不支持的形态 | 切换 Valkey 二进制与 Docker 部署 |
| `x-kinstall-ui.widget: "cidr-allocator"` | Docker Bridge 子网 / K8s Pod CIDR 分配器（实时调后端检测 `subnets_json` 路由冲突） | 根治 Docker 网桥与办公网段撞车（R2） |
| `x-kinstall-ui.visibleWhen` | 声明式联动显隐表达式（如 `deployTarget == 'host-container'` 或 `topology == 'cluster'`） | 切单节点时隐藏哨兵 quorum 字段 |
| `x-kinstall-ui.riskWarning` | 高危参数即时提示横幅（如选 Elasticsearch / Redis 8 / MongoDB / OnlyOffice 时弹出许可证合规确认框） | 强执行 ADR-059 红灯许可门禁 |

### 3.4 Plan 审查与 3-Way Diff 可视化对比器

在点击 `生成 Plan` 后、正式 `Apply` 前，呈现全屏分栏审查台：
1. **顶部预检门禁条（Preflight Banner）**：展示通过项 / 警告项 / 阻断项（例如 `❌ node-arm-02 为 64K 页内核，无法安装 4K Valkey 制品 [KI-PLAN-0012]`，存在阻断项时 `执行 Apply` 按钮物理禁用）。
2. **左侧变更任务树**：按 Phase → Host → Task 列出全部 `Create`（绿）、`Update`（蓝）、`Drift Conflict`（橙红闪烁）、`Skipped`（灰）计数，默认折叠 `Skipped` 聚焦真实变更。
3. **右侧 3-Way Diff 视图（基于 `@codemirror/merge`）**：
   - 常规变更：双栏 `Live Observed` vs `Desired` 语法高亮 Diff；
   - **检测到远端人工漂移时（ADR-070）**：自动切换为**三栏对比（`上次下发基线 lastApplied` | `远端现状 liveObserved（标红人工改行）` | `本次期望 desired`）**，并强制运维在底部单选决策：
     - `中止并人工核对`（默认安全选项）
     - `采纳远端改动（--adopt）`
     - `强行覆盖远端（--force-override，自动记入高危审批门）`

### 3.5 Operation 执行中心与 SSE 多主机分屏终端（`/operations/:id`）

```text
+---------------------------------------------------------------------------------------------------+
| op_01JCZ... · valkey-prod · upgrade (9.1.3 → 9.1.4)    [🟢 Running 02:14]  [暂停] [取消] [导出报告] |
+-----------------------------+---------------------------------------------------------------------+
| Phase / Host 执行矩阵        | 终端视图: [全部主机 v] [仅看错误] [自动滚底 ☑] [连接: 🟢 SSE #48291] |
|                             +---------------------------------------------------------------------+
| ▼ 1. preflight (6/6)        | [08:01:12][node-01][binary.install] Resolving via obs-prod...       |
|   ✅ node-01..06     (4s)   | [08:01:13][node-01][binary.install] Direct URL: obs://corp/bin/...  |
| ▼ 2. rolling-update (3/6)   |            [PRESIGNED-REDACTED] (sha256 verified OK)                |
|   ✅ node-01 (master) 18s   | [08:01:15][node-01][systemd.unit]   Reloading kinstall-valkey...    |
|   🔵 node-02 (replica) 9s   | [08:01:17][node-02][binary.install] Downloading 18.4 MiB (49 MB/s)  |
|   ⬜ node-03 (replica) --   | [08:01:19][node-02][wait.tcp]       10.20.1.12:6379 healthy (24ms)  |
|                             |                                                                     |
| [如失败：一键 --resume 续装] | (底层采用 @xterm/xterm 虚拟滚动，支持 10 万行日志 60fps 零卡顿)       |
+-----------------------------+---------------------------------------------------------------------+
```

- **断网零丢帧体验**：当浏览器切后台或反代短暂闪断时，前端顶部徽章变为 `🟡 重连中（断点 #48291）`，自动携带 `Last-Event-ID: 48291` 重连任意 Server 副本无缝追平。
- **失败定点排障**：点击任意红色失败主机行，终端自动过滤到该主机该 Task 的完整上下文，并弹出结构化 `KI-*` 错误卡片（含中文根因与修复建议 `suggestion`）及**`修复后从断点继续（--resume）`**按钮。

### 3.6 `Stack` 复合栈 DAG 拓扑画布（`/stacks/:id/canvas`）

基于 **Vue Flow 2.0（`xyflow`）** 实现所见即所得的跨组件依赖编排（如大型 `ELK/OpenSearch + Kafka 4.x + Data Prepper + SeaweedFS S3`）：
- **节点卡片**：每个节点代表一个 `Instance`，卡片顶部展示组件图标、版本与部署形态徽章，左侧为输入端口（如 `s3Endpoint`、`kafkaBrokers`），右侧为该插件 `OutputSchema` 声明的输出端口（凭据类端口带🔒图标标明“仅传递加密 `secretId`”）；
- **连线即 `dependsOn` + 静态校验**：拖拽连线时前端即时做环路检测（成环弹出红色虚线拒绝连线），自动写入 `${instances.<id>.outputs.<key>}` 引用表达式；
- **执行态实时染色**：`Stack Apply` 运行时，画布与 SSE 联动，已完成节点描绿边、并行执行中节点流动蓝边、被阻塞下游节点虚线灰态，点击任意节点直钻该子实例 SSE 终端。

### 3.7 四眼审批工作台、多集群管理与安全审计页

- **四眼审批台（`/operations/approvals`）**：高危待审批卡片直接内嵌“变更影响主机数 + 核心 Diff 摘要 + 发起人”；若当前登录用户 == 发起人，`批准` 按钮物理禁用并悬浮提示 `遵循四眼原则（ADR-037），发起人不可审批自己的请求`（后端与 SQL 约束三重保障）。
- **多集群页（`/clusters`）**：集中展示所有纳管 K8s 集群的版本、节点/Pod 健康度、etcd 延迟与证书到期天数；点击 **`签发短期 kubeconfig`** 弹窗可选有效期（`1h / 8h / 24h`）与受限 RBAC 角色，自动签发并记审计，同时提供一键跳转集群内 Headlamp 入口。
- **审计哈希链与合规中心（`/security/audit` & `/security/compliance`）**：审计日志列表顶部设 **`立即校验哈希链完整性`** 按钮（调用后端逐行重算 SHA-256 / SM3 链，返回绿盾“1..48,910 条完整无篡改”或精确红警定位被篡改序号）；合规页支持勾选 `等保 2.0 三级` / `密评 GB/T 39786` / `CIS Kubernetes` 一键生成并下载标准化测评证据包 `.tar.gz`。

---

## 4. 前端工程技术栈与完整代码架构（`web/`）

### 4.1 锁定技术栈清单（ADR-023 正式定稿，2026-11 实查）

| 类别 | 选型与锁定版本 | 许可证 | 选型依据与 2026-11 安全核查 |
| --- | --- | :---: | --- |
| **框架与构建** | **Vue `3.5.45`** + **TypeScript `5.9`** + **Vite `6.4+`** | MIT | Vue 3.5 为当前生产稳定线（3.6 仍在 RC）；构建产物极小且支持严格离线打包 |
| **UI 组件库** | **Element Plus `2.14.2`**（按需自动导入 `unplugin-vue-components`） | MIT | 中文企业运维表格/表单/抽屉/树组件最完备，暗色主题 CSS 变量体系成熟 |
| **状态与路由** | **Pinia `3.0.4`** + **Vue Router `4.6+`** | MIT | 轻量类型安全，天然适配多项目/环境全局上下文切换 |
| **OpenAPI 代码生成** | **`@hey-api/openapi-ts`**（直接消费后端 `api/openapi.yaml`） | MIT | **★ 2026-10 安全红线**：正式禁用已停更且存在 **CVSS 9.8 RCE（`CVE-2026-108551`）** 的 `openapi-typescript-codegen`；`@hey-api/openapi-ts` 原生支持 OpenAPI 3.1 且安全转义 |
| **实时终端** | **`@xterm/xterm`** + `@xterm/addon-fit` + `@xterm/addon-search` | MIT | 虚拟化渲染 10 万行 ANSI 彩色日志零掉帧 |
| **3-Way Diff** | **`@codemirror/merge`** + `@codemirror/lang-yaml` | MIT | 体积远小于 Monaco Editor（节省 ~8 MiB 内嵌包体积！），完美支持双栏/三栏合并视图 |
| **DAG 拓扑画布** | **Vue Flow `2.0`**（npm 包名 **`@xyflow/vue` `2.0.x`**，`xyflow` 官方家族，2026-11-17 发布） | MIT | 原 `@vue-flow/core` 1.x 已并入 `xyflow` 升级 2.0（**含破坏性变更，禁止混装 1.x 插件包**）；全部样式位于 `@layer xyflow`，`DESIGN.md §8.6` 的 `Ki*` 覆写无需 `!important`；用 `forceColorMode` 与 `data-theme` 同步暗/亮 |
| **国际化** | **`vue-i18n` v11** | MIT | 与后端 `go-i18n` 共用 `KI-*` 错误码键，支持中/英无刷新切换 |
| **离线包体积红线** | `web/dist/` gzip 后严格 **`≤ 4 MiB`**（整体 `kinstall` 二进制 ≤ 150 MiB） | — | CI `vite-bundle-visualizer` + 体积断言门禁；严禁引入整包 Monaco / ECharts 全量未裁剪包（图表按需引入 ECharts Core 仅含柱/折/环图） |

### 4.2 `web/` 目录结构蓝图

```text
web/
├── package.json                    # pnpm-lock.yaml 锁定，CI 离线镜像走 Verdaccio（D3 §4.3）
├── vite.config.ts                  # 关闭外部资源引用、手动分块、生产剔除 console/sourcemap
├── openapi-ts.config.ts            # 从 ../api/openapi.yaml 生成 src/api/gen/
├── src/
│   ├── main.ts                     # 初始化 Pinia、Router、Element Plus、i18n、全局错误拦截
│   ├── styles/                     # 与 DESIGN.md §11.1 严格一致（Stylelint 禁硬编码 hex）
│   │   ├── fonts.css               # 本地内嵌 Inter / JetBrains Mono WOFF2 子集
│   │   ├── tokens.css              # 暗/亮双主题与双密度 --ki-* 唯一真相源
│   │   ├── element-overrides.css   # Element Plus 2.14.2 --el-* 到 --ki-* 主题桥接
│   │   ├── base.css                # Reset、.ki-mono、:focus-visible、reduced-motion
│   │   └── utilities.css           # 轻量布局工具类（零 Tailwind/UnoCSS 依赖）
│   ├── api/
│   │   ├── gen/                    # @hey-api/openapi-ts 自动生成（禁止手改，CI 漂移检测）
│   │   ├── client.ts               # 统一注入 X-Request-ID、Idempotency-Key、Accept-Language、RFC 9457 解析
│   │   └── sse.ts                  # ★ 生产级 SSE 客户端（支持自定义鉴权头、Last-Event-ID 断点重连与心跳看门狗）
│   ├── stores/
│   │   ├── auth.ts                 # 当前用户、角色、权限判定 can(action, scope)
│   │   ├── workspace.ts            # 当前 project / env、主题（dark/light）、密度、语言（zh-CN/en-US）
│   │   └── approvals.ts            # 待审批角标实时计数
│   ├── components/
│   │   ├── DynamicSchemaForm/      # ★ §3.3 JSON-Schema + x-kinstall-ui 动态表单引擎
│   │   ├── ThreeWayDiff/           # ★ §3.4 @codemirror/merge 3-Way 漂移对比器
│   │   ├── OperationTerminal/      # ★ §3.5 @xterm/xterm + SSE 分屏日志终端
│   │   ├── StackCanvas/            # ★ §3.6 Vue Flow 2.0 DAG 编排画布
│   │   ├── HostRolePicker/         # 主机选择与 64K 页/端口冲突实时预览弹窗
│   │   └── CommandPalette.vue      # 全局 ⌘K 快捷搜索与跳转
│   ├── views/                      # 对应 §1.2 全部路由页面（dashboard/hosts/instances/stacks/clusters/operations/artifacts/security/settings）
│   └── locales/                    # zh-CN.json / en-US.json（与后端错误码对齐）
└── e2e/                            # Playwright 关键链路回归（登录、Plan 3-Way Diff、OBS 探活、四眼防自审）
```

### 4.3 关键前端核心代码骨架

#### A. 支持 `Last-Event-ID` 断点续传、鉴权头与 45s 心跳看门狗的 SSE Composable（`web/src/api/sse.ts`）

> **为什么不用浏览器原生 `EventSource`？** 原生 `EventSource` **不支持自定义 `Authorization` 请求头**（API Token 场景受限）且无法感知静默僵死连接。以下基于原生 `fetch` ReadableStream 实现零额外依赖的工业级 SSE 客户端（与 `G1 §3.2` 严格契约对齐：`operation.done` 控制帧不带 `id:`，避免与最后一条日志 `seq` 碰撞导致误丢与死循环重连）：

```ts
import { ref, onUnmounted } from 'vue';

export interface OperationEventFrame {
  id: number;
  event: string;
  data: Record<string, unknown>;
}

export function useOperationSSE(opId: string, onFrame: (f: OperationEventFrame) => void) {
  const status = ref<'connecting' | 'open' | 'reconnecting' | 'closed' | 'error'>('connecting');
  const lastEventId = ref<number>(0);

  let abortCtrl: AbortController | null = null;
  let stopped = false;
  let lastActivityAt = Date.now();
  let watchdogTimer: number | null = null;
  let reconnectTimer: number | null = null;

  const parseAndDispatch = (rawBlock: string) => {
    lastActivityAt = Date.now();
    if (!rawBlock.trim() || rawBlock.startsWith(':')) return; // 心跳注释行仅刷新看门狗时间
    let id = 0;
    let event = 'message';
    const dataLines: string[] = [];
    for (const line of rawBlock.split('\n')) {
      if (line.startsWith('id:')) id = Number(line.slice(3).trim()) || 0;
      else if (line.startsWith('event:')) event = line.slice(6).trim();
      else if (line.startsWith('data:')) dataLines.push(line.slice(5).trimStart());
    }
    // 持久事件帧严格单调去重；终态控制帧 operation.done 由服务端不带 id: 行下发（本地解析为 id=0），不推进 lastEventId 且永不被去重误吞！
    if (event !== 'operation.done' && id > 0 && id <= lastEventId.value) return;
    if (id > 0) lastEventId.value = id;
    const payload = dataLines.join('\n');
    try {
      const parsed = payload ? JSON.parse(payload) : {};
      onFrame({ id, event, data: parsed });
      if (event === 'operation.done') {
        stopped = true;
        status.value = 'closed';
        abortCtrl?.abort();
      }
    } catch {
      onFrame({ id, event, data: { raw: payload } });
    }
  };

  const connect = async (attempt = 0) => {
    if (stopped) return;
    status.value = attempt === 0 ? 'connecting' : 'reconnecting';
    abortCtrl = new AbortController();
    lastActivityAt = Date.now();

    const headers: Record<string, string> = { Accept: 'text/event-stream' };
    if (lastEventId.value > 0) headers['Last-Event-ID'] = String(lastEventId.value);

    try {
      const resp = await fetch(`/api/v1/operations/${encodeURIComponent(opId)}/events`, {
        method: 'GET',
        credentials: 'same-origin', // 自动携带 HttpOnly OIDC Session Cookie
        headers,
        signal: abortCtrl.signal,
      });
      if (!resp.ok || !resp.body) throw new Error(`SSE HTTP ${resp.status}`);
      status.value = 'open';

      const reader = resp.body.getReader();
      const decoder = new TextDecoder('utf-8');
      let buf = '';
      while (!stopped) {
        const { done, value } = await reader.read();
        if (done) break;
        // 兼容部分企业网关将 LF 规范化为 CRLF
        buf += decoder.decode(value, { stream: true }).replace(/\r\n/g, '\n');
        const parts = buf.split('\n\n');
        buf = parts.pop() ?? '';
        for (const block of parts) parseAndDispatch(block);
      }
    } catch {
      // 网络波动或看门狗主动 abort，进入指数退避重连
    }
    if (!stopped) {
      const delayMs = Math.min(1000 * 2 ** attempt, 10000);
      reconnectTimer = window.setTimeout(() => void connect(attempt + 1), delayMs);
    }
  };

  // 45s 无任何帧或 15s 心跳即判定半死连接，主动掐断触发断点重连
  watchdogTimer = window.setInterval(() => {
    if (!stopped && status.value === 'open' && Date.now() - lastActivityAt > 45_000) {
      abortCtrl?.abort();
    }
  }, 10_000);

  void connect(0);

  const close = () => {
    stopped = true;
    status.value = 'closed';
    if (watchdogTimer) clearInterval(watchdogTimer);
    if (reconnectTimer) clearTimeout(reconnectTimer);
    abortCtrl?.abort();
  };
  onUnmounted(close);

  return { status, lastEventId, close };
}
```

#### B. Go 内嵌静态资源与零外链 CSP 头（`internal/server/embed_ui.go`）

- 前端路由采用 HTML5 History 模式，Go HTTP 服务对所有非 `/api/`、非静态文件后缀路径回退返回 `web/dist/index.html`（强制 `Cache-Control: no-cache`）；带内容哈希的 `/assets/*.js|css` 设 `Cache-Control: public, max-age=31536000, immutable`；
- 全局下发严格离线安全响应头：`Content-Security-Policy: default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; font-src 'self'; connect-src 'self'; frame-ancestors 'none'`，彻底阻断 XSS 与意外外网请求。

---

## 5. 前端验收门禁清单

| 类别 | 门禁要求 |
| --- | --- |
| **契约零漂移** | CI 运行 `pnpm openapi-ts` 后 `git diff --exit-code web/src/api/gen/` 为空；`pnpm typecheck`（`vue-tsc --noEmit`）零报错 |
| **纯离线零外链** | Playwright 在**拦截一切非 `127.0.0.1` 请求**的断网浏览器上下文中打开全部 11 个主页面，零网络失败、零缺失字体/图标、零控制台 Error |
| **包体与性能** | `web/dist` gzip 总大小 `≤ 4 MiB`；首屏 JS `≤ 350 KiB` gzip（`ARCHITECTURE O-09`，重组件路由级懒加载）；Lighthouse Performance `≥ 90`；首屏渲染 `< 1.5s`（4G / 中端机）；`OperationTerminal` 压测灌入 100,000 行日志滚动帧率 `≥ 60 fps` |
| **安全与交互回归** | Playwright 自动验证：① 发起人打开审批页 `批准` 按钮禁用；② 检测到外部漂移时强制展示 3-Way Diff；③ OBS 介质源测试连通性与 `offlineStrict` 联动锁定生效；④ 深/浅双主题与中/英切换全页面无文字截断或溢出 |
