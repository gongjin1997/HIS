# kinstall 故障诊断与应急恢复手册（`TROUBLESHOOTING_RUNBOOK.md` · T-M2-15 / I3）

> 面向生产现场值守工程师，完整覆盖 **Top 30 高频错误码（`KI-*`）** 排障路径、紧急恢复 SOP 与安全合规红线。

## 1. Top 30 生产错误码速查与处置 SOP

| # | 错误码 | 域 | 触发场景 | 现场排查与修复步骤 |
| :-: | --- | :-: | --- | --- |
| 1 | `KI-AUTH-0001` | AUTH | 认证失败、Session/Token 过期或 OIDC state 失效 | 核对账号/密码；CLI 模式重新签发短期 API Token (`ki_<8>_<32>`)；OIDC 场景检查回调时间偏差 |
| 2 | `KI-AUTH-0002` | AUTH | RBAC 越权或**四眼审批拦截自审** | 核对目标项目 `roleBindings`；发起人禁止审批自身变更（Go + DB `chk_no_self_approval` 双重阻断），须由另一位 `project-admin`/`admin` 审批 |
| 3 | `KI-AUTH-0003` | AUTH | 连续 5 次密码错误触发 `423 Locked` | 等待 15 分钟自动解锁，或由平台管理员核查审计日志排除暴力破解后重置账号 |
| 4 | `KI-API-0001` | API | 请求体格式非法、字段缺失或参数越界 | 查看 RFC 9457 `detail` 与 `traceId`；核对 [openapi.yaml](../api/openapi.yaml) 契约字段 |
| 5 | `KI-API-0002` | API | 同 `Idempotency-Key` 携带不同请求体（`409`） | 禁止复用旧幂等键提交新参数，生成全新 `Idempotency-Key` 后重试 |
| 6 | `KI-API-0003` | API | 乐观锁 `ETag`/`If-Match` 版本冲突（`412`） | 重新 `GET` 拉取最新资源版本后再提交变更，防止覆盖他人配置 |
| 7 | `KI-EXEC-0001` | EXEC | SSH 建连/执行超时或 ProxyJump 不可达 | 检查目标机 22 端口、堡垒机连通性及 sshd `MaxSessions`/`MaxStartups` 上限 |
| 8 | `KI-EXEC-0002` | EXEC | SSH `known_hosts` 指纹变更阻断 | 核查主机是否重装系统或存在 MITM 风险；确认安全后在 CMDB 更新 `hostkeyFp` |
| 9 | `KI-EXEC-0003` | EXEC | `sudo` 提权失败触发防 `pam_faillock` 熔断 | 检查目标机 `sudo -n` / Ubuntu 26.04 `sudo-rs` 配置与凭据密文，修复后解除熔断 |
| 10 | `KI-EXEC-0004` | EXEC | KYSEC / `fapolicyd` / SELinux / `noexec` 阻断执行 | **严禁直接关闭安全模块**；检查 `/var/lib/kinstall/tmp` 挂载选项，或经授权通过 `security-allowlist` 模块按文件路径加白 |
| 11 | `KI-OS-0001` | OS | 未识别的 Linux 发行版或架构 | 检查目标机 `/etc/os-release`，提交采样至 `spec/osmatch.yaml` 并跑通 `spec/gen_os_docs.py --check` |
| 12 | `KI-OS-0002` | OS | 缺失逻辑包映射或离线源不可用 | 检查 `spec/pkgmap.yaml` 与离线 yum/apt 仓库元数据是否已同步 |
| 13 | `KI-OS-0003` | OS | `glibc < 2.28`（如 CentOS 7）或 cgroup v1 不兼容 | K8s 1.35+ 强制 cgroup v2；旧内核主机请升级内核或将中间件 `deployTarget` 切换为 `host-container` |
| 14 | `KI-HOST-0001` | HOST | 主机名不符合 DNS-1123 规范或重复 | 修正主机名为小写字母/数字/连字符（`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`） |
| 15 | `KI-HOST-0002` | HOST | 端口 / 目录 / Docker Bridge 子网 / 内存 Claim 冲突（`409`） | 核对 Plan 报错（如 SonarQube 9000 与 Attu 冲突 → Attu 固定 19001；调整 `dockerSubnet`；先卸载占用实例再删主机） |
| 16 | `KI-INIT-0001` | INIT | HostInit 互斥 Profile sysctl 冲突或模块自检失败 | 例如 `database-pg` (`overcommit_memory=2`) 与 `cache` (`overcommit_memory=1`) 不可混部，请拆分独立主机 |
| 17 | `KI-PLAN-0012` | PLAN | `arm64-64K` 页内核缺少 `--with-lg-page=16` 构建 | 在鲲鹏/麒麟 V10 SP3 (64K 页) 上导入标注 `pageSizes: [65536]` 的专用 BOM 制品（Valkey / Harbor 外置 Valkey） |
| 18 | `KI-PLAN-0020` | PLAN | 拓扑最小节点数不足或偶数仲裁节点 | K8s 控制面 / etcd / Patroni / Seata / ZooKeeper 必须为奇数节点（1/3/5） |
| 19 | `KI-PLAN-0021` | PLAN | 跨故障域（AZ/机架）反亲和约束不满足 | Valkey Cluster / Kafka / OpenSearch / SeaweedFS 生产高可用需分布至 ≥3 个独立故障域 |
| 20 | `KI-DIFF-0001` | DIFF | 3-Way Diff 检测到远端人工配置漂移 | 在 Web Diff 抽屉对比 `lastApplied` vs `liveObserved` vs `desired`，选择 `--adopt` 采纳或经四眼审批后 `--force-override` |
| 21 | `KI-DIFF-0002` | DIFF | 3-Way Diff 检测到双向冲突（远端与期望均偏离基线） | 人工合入远端改动至 `desiredSpec` 后重新 Plan，或经四眼审批强制覆盖 |
| 22 | `KI-DIFF-0003` | DIFF | 目标路径存在未纳管历史文件（Scenario 6） | 确认文件归属后开启 `adoptExisting=true` 纳管或手工备份移走 |
| 23 | `KI-SRC-0001` | SRC | 制品缺少 64 位 SHA-256 校验摘要 | 在 BOM/MediaRef 中补齐完整 `sha256`，平台严禁无摘要拉取 |
| 24 | `KI-SRC-0002` | SRC | 下载制品 SHA-256 校验不匹配 | 核查镜像/OBS/本地仓库是否损坏或被截断，清理缓存目录后重新拉取 |
| 25 | `KI-SRC-0005` | SRC | `offlineStrict` 策略拦截公网 Online 或公网 OBS 源 | 将离线包导入 Local 根目录，或将内网私有 OBS/SeaweedFS 源标记为 `internal: true` |
| 26 | `KI-ART-0001` | ART | 制品验签失败或包含受限许可证（SSPL/ELv2/RSAL） | 核对 Sigstore/SM2 签名；ES/Redis/MongoDB/Neo4j 不得混入公共离线包，按客户自备清单导入 |
| 27 | `KI-ART-0002` / `0003` | ART | `trusted_root.json` 损坏或国密 SM2/SM3 验签失败 | 检查离线信任根文件完整性及签名文件是否被篡改 |
| 28 | `KI-SEC-0001` | SEC | KEK 不可用、OpenBao 封存（Sealed）或 AAD 校验失败 | 严禁绕过加密；立即解封 OpenBao 或检查 `0600` 本地密钥环文件；轮换后执行 `kinstall secrets rewrap`（详见 [KEK_ROTATION_RUNBOOK.md](./KEK_ROTATION_RUNBOOK.md)） |
| 29 | `KI-ENG-0001` / `0002` | ENG | Worker 租约失效（Fencing Token 抢占）或未知 Task 类型 | 另一 HA 副本已安全接管，旧 Worker 已自动自停防双写；排查网络/DB 延迟；声明式/外部插件仅限使用 15 个内置 Task |
| 30 | `KI-STACK-0001` / `0002` & `KI-IMG-0001` / `0002` | STACK / IMG | Stack 循环依赖、输出引用无效、下游未卸载、`:latest` 可变标签或禁用镜像源（Bitnami / MinIO CE） | 解除 DAG 环路；先逆序卸载下游组件（`externalRefs` 永不触碰）；镜像锁定 `@sha256:`；MinIO CE 已归档请迁移 SeaweedFS 4.54 |

## 2. 应急恢复标准命令

```bash
# 1. 收集全脱敏诊断包（零明文凭据，Redline 6）
kinstall support-bundle --project <prj_id> --out /tmp/kinstall-support.tar.gz

# 2. 校验审计日志双哈希链完整性（SHA-256 + 国密 SM3）
kinstall audit verify

# 3. 轮换本地/OpenBao KEK 后在线重加密所有凭据 DEK（O-11）
kinstall secrets rewrap --project <prj_id>

# 4. 失败任务修复现场后从断点零重复续装（T-M0-12）
kinstall operation resume <op_id> --project <prj_id>

# 5. PostgreSQL PITR 按时间点恢复演练（T-M4-11）
kinstall instance restore <inst_id> --project <prj_id> --target-time 2026-11-27T12:00:00Z

# 6. 生成等保 2.0 三级 + 密评 GM/T-0054 合规证据包（T-M3-13）
kinstall compliance report --project <prj_id>
```
