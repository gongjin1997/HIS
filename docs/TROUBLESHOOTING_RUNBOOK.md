# kinstall 故障诊断与应急恢复手册（`TROUBLESHOOTING_RUNBOOK.md` · T-M2-15 / I3）

> 面向生产现场值守工程师，覆盖高频错误码（`KI-*`）排障路径、紧急恢复命令与数据安全红线。

## 1. 核心错误码速查与处置 SOP

| 错误码 | 触发场景 | 现场排查与修复步骤 |
| --- | --- | --- |
| `KI-AUTH-0001` | 认证失败或 Token 过期 | 核对登录账号/密码；CLI 模式重新签发短期 API Token (`ki_<8>_<32>`) |
| `KI-AUTH-0002` | RBAC 越权或**四眼审批拦截自审** | 确认当前用户在目标项目角色绑定；发起人禁止审批自身请求，须由另一位 `project-admin`/`admin` 复核 |
| `KI-AUTH-0003` | 连续 5 次密码错误触发 `423 Locked` | 等待 15 分钟自动解锁，或由平台管理员核查是否存在暴力破解并重置状态 |
| `KI-EXEC-0001` | SSH 连接/超时失败 | 检查目标主机 22 端口、ProxyJump 堡垒机连通性与 `MaxSessions`/`MaxStartups` |
| `KI-EXEC-0002` | HostKey 指纹变更阻断 | 核查目标机是否重装系统或遭遇 MITM；确认安全后在 CMDB 更新 `hostkey_fp` |
| `KI-EXEC-0003` | `sudo` 提权失败（已熔断防 `pam_faillock`） | 验证目标机 `sudo -n` / `sudo-rs` 口令与 sudoers 配置，修复后解除主机熔断 |
| `KI-EXEC-0004` | KYSEC / `fapolicyd` / `noexec` 拦截 | 严禁关闭安全模块；通过 `hostinit` 显式授权按文件路径加白或调整工作目录 |
| `KI-OS-0003` | `glibc < 2.28`（如 CentOS 7） | 无法运行 `host-binary` 新版中间件，请将 `deployTarget` 切换为 `host-container` |
| `KI-PLAN-0012` | `arm64-64K` 页内核缺失专用 jemalloc 构建 | 在鲲鹏/麒麟 V10 SP3 (64K) 上导入含 `--with-lg-page=16` 构建的 BOM 制品（Valkey / Harbor 外置 Valkey） |
| `KI-PLAN-0020` / `0021` | 集群节点数或跨故障域反亲和不足 | K8s/etcd/Seata/ZK 必须为奇数节点（≥3）；Valkey Cluster 须跨 ≥3 个不同主机/机架 |
| `KI-HOST-0002` | 端口 / 目录 / Docker 子网冲突 | 查看 Plan 冲突详情（如 SonarQube 9000 与 Attu 冲突 → Attu 固定 19001；调整 `dockerSubnet`） |
| `KI-DIFF-0001` / `0002` | 检测到远端人工配置漂移或双向冲突 | 使用 3-Way Diff 核对 `lastApplied` / `liveObserved` / `desired`，选择 `--adopt` 或经四眼审批后 `--force-override` |
| `KI-SRC-0005` | `offlineStrict` 阻断公网源/非白名单 OBS | 将离线 Bundle 导入 Local 目录，或将私有 OBS/SeaweedFS 桶标记为 `internal: true` |
| `KI-SEC-0001` | KEK 不可用或 OpenBao 已封存（Sealed） | 切勿绕过加密；立即解封 OpenBao 或检查 0600 本地密钥环文件权限；轮换后执行 `kinstall secrets rewrap` |
| `KI-ENG-0001` | Worker 租约失效（Fencing Token 过期） | 另一副本已安全接管任务，原 Worker 已自动自停避免双写；排查网络抖动或 DB 延迟 |
| `KI-STACK-0001` / `0002` | Stack DAG 存在循环依赖或下游仍引用待删实例 | 解除环路依赖；先按拓扑逆序卸载下游组件后再删除上游依赖 |
| `KI-IMG-0001` / `0002` | 可变镜像标签（`:latest`）或红灯/禁用源（Bitnami / MinIO CE） | 改为 `@sha256:` 锁定镜像；MinIO CE 已归档请迁移 SeaweedFS 4.54 |

## 2. 应急恢复标准命令

```bash
# 1. 收集全脱敏诊断包（零明文凭据）
kinstall support-bundle --project <prj_id> --out /tmp/kinstall-support.tar.gz

# 2. 校验审计日志双哈希链完整性（SHA-256 + SM3）
kinstall audit verify

# 3. 轮换本地/OpenBao KEK 后在线重加密所有凭据 DEK
kinstall secrets rewrap --project <prj_id>

# 4. 失败任务修复现场后从断点零重复续装
kinstall operation resume <op_id>

# 5. 生成等保 2.0 三级 + 密评合规证据包
kinstall compliance report --project <prj_id>
```
