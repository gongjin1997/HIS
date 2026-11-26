# P1 核心插件深度设计（1/7）：Kubernetes 纯二进制生产级插件

> **本文定位**：kinstall 第三阶段首个核心插件深度设计（`plugins/kubernetes/`，**ADR-007 / 008 / 009 / 010 / 014 / 015 / 016 / 017 / 028 / 031 / 032 / 033 / 069**），将 `04-Kubernetes安装规划与设计-重建版.md` 转化为强类型 Go 插件（仅依赖 `pkg/sdk/v1alpha1`）的工程落地蓝图。
>
> **版本**：v1.1（核心插件 1/7，2026-11-26；v1.1 修正麒麟 V10 SP3=64K / V11=4K、三分支模板矩阵与 `AuthenticationConfiguration apiserver.config.k8s.io/v1`）。覆盖 K8s `1.35 / 1.36 / 1.37`（及 `1.38` GA 预留分支）与 BOM `2026.11.3 → 2026.12.1` 补丁升级实操。

---

## 1. 插件包结构与强类型扩展点（`plugins/kubernetes/`）

```text
plugins/kubernetes/
├── plugin.yaml                    # minPlatformVersion / OutputSchema（apiServerVIP、caCertSecretId、kubeconfigRef）
├── schema/params.json             # 对接 WEB_DESIGN.md §3.3 JSON-Schema 动态表单
├── plugin.go                      # Component 实现：Validate / Plan / Scale / Upgrade / RotateCerts / EtcdBackup / Reset
├── state_migrator.go              # 实现 G1 §6.2 StateMigrator（支持旧集群状态平滑升级）
├── extensions/
│   ├── cri/{containerd,cridockerd}.go    # containerd 2.3 LTS (v4 config) / Docker 29 + cri-dockerd ≥ 0.4.11
│   ├── cni/{calico,cilium,flannel,kubeovn}.go
│   ├── csi/{localpath,nfs,longhorn,rookceph,vendor}.go  # 显式声明 selinuxMount（ADR-032）
│   ├── ha/{localproxy,vip}.go            # Go 内置本地代理 127.0.0.1:8443 + Keepalived/外部 LB（ADR-009）
│   └── addon/{coredns,envoygw,headlamp,promstack,csrapprover}.go  # Helm v4 SDK
├── templates/<component>/<semver-range>/ # >=1.35 <1.37 / >=1.37 <1.38 / >=1.38 三分支模板（04 §2.3 模板矩阵）
└── testdata/golden/                      # 与同版本 kubeadm v1beta4 基线逐项语义对比（05 §6.2）
```

---

## 2. 混合架构（`amd64` + `arm64-4K` + `arm64-64K`）与版本分支治理

1. **三层架构预检（Preflight 硬门禁）**：
   - 扫描集群全部节点 `Facts.Arch` 与 `Facts.PageSize`；
   - 核查 BOM `images.lock` 中所有系统镜像（`pause`、`coredns`、`calico`、`metrics-server`）的 OCI Index 必须同时包含目标架构列表，缺失任意平台 digest 直接阻断 `KI-K8S-0010`；
   - 存在 `arm64-64K` 节点（如鲲鹏 + 麒麟 V10 SP3 / RHEL 8 系〔Rocky 8〕，或选用 `kernel-64k` 的 EL9 / openEuler 24.03；注：`03-A §5` 实测麒麟 V11 为 4K）时，自动为该节点注入标签 `kinstall.io/page-size=64k` 与污点 `kinstall.io/page-size=64k:PreferNoSchedule`（可配置 `NoSchedule`），并校验 kubelet / containerd 及选中 CSI 均支持 64K 页，**严禁跨 4K/64K 节点直接拷贝 etcd bbolt 数据目录（ADR-028）**。
2. **版本模板分支与 `kubeadm v1beta4` 零漂移保证**：
   - `>=1.35 <1.37`：使用 `KubeletConfiguration v1beta1` + `AuthenticationConfiguration apiserver.config.k8s.io/v1`（1.34 起 GA，1.35–1.38 统一）；
   - `1.37.x+`：开启 `SELinuxMount` GA 行为校验（升级 `1.36 → 1.37` 前由插件自动扫描集群内所有挂载 RWO 卷的 Pod，对同卷多安全上下文冲突负载生成 `seLinuxChangePolicy: Recursive` 修复建议，ADR-032）；
   - `1.38.x`（预计 2026-12-16 GA）：保持 `failCgroupV1` 检查与 `cri-dockerd` `RuntimeConfig` 实测转正开关（ADR-010/015）。

---

## 3. 核心生产子系统像素级规范

### 3.1 节点本地 APIServer 代理 HA（`kinstall-apiserver-proxy`，ADR-009 / ADR-011）

每个节点（含单主节点，预留无缝扩三主能力！）均以 systemd 托管内置极简 Go L4 代理监听 **`127.0.0.1:8443`**，kubelet / kube-proxy / CNI 统一连接 `https://127.0.0.1:8443`：
- **主动健康检查**：每 2s 对全部后端控制面执行 `GET https://<cp>:6443/livez`（1s 超时），连续 2 次失败立即熔断摘除，恢复后 2 次成功自动回切；
- **零重启热加载**：控制面扩缩容（1→3 主或替换故障主）时，插件仅通过 `file.sync` 更新 `/etc/kinstall/apiserver-proxy/backends.json` 并发送 `SIGHUP`，已有长连接平滑过渡、新连接立即按新列表转发（零中断 kubelet Watch 流！）；
- **集群外部入口**：三主/五主对外统一暴露硬件 LB（F5/SLB/ELB）或 Keepalived VIP，PKI 证书 SAN 强制预写入 VIP、业务域名与全部控制面节点 IP（支持 IPv4/IPv6 双栈，ADR-031）。

### 3.2 Calico 3.33 + kube-proxy `nftables` 一致性矩阵（ADR-008 / ADR-014）

| 主机条件（全集群取最低公约数） | `kube-proxy` 模式 | Calico 数据面 | 前置与迁移规则 |
| --- | --- | --- | --- |
| `kernel >= 5.13` 且 `nft >= 1.0.1`（Rocky 9/10、Ubuntu 24/26、openEuler 24、麒麟 V11） | **`nftables`** | **`NFT`** | 新集群默认；彻底告别 `iptables` 万级 Service O(N) 性能悬崖 |
| 任一节点不满足（如麒麟 V10 4.19 内核 / Rocky 8） | `iptables` | `Iptables` | 自动整体降级保持两侧一致，绝不出现“kube-proxy 用 nft、Calico 用 iptables”的黑洞错配 |

### 3.3 节点资源预留精确计算器（ADR-033）

插件在 `Plan()` 期按每台节点真实 `Facts.MemTotalMB`、大页配置 `HugePagesMB` 与 `maxPods` 自动渲染 `KubeletConfiguration`：
- `availMemMB = MemTotalMB - HugePagesMB`；
- `kubeReserved.memory = min(255 + 11 * maxPods, 0.25 * availMemMB)`；
- `systemReserved.memory` 按宿主机档位（≤16G: `512Mi`、32–64G: `1Gi`、≥128G: `2Gi`；开启 `harden`/EDR 额外 `+512Mi`）；
- `evictionHard["memory.available"] = clamp(0.02 * availMemMB, 200Mi, 1Gi)`；
- 自动向主机初始化模块 `03 §6.2 D16` 回写 `OSReserveMB = kubeReserved + systemReserved + evictionHard`。

---

## 4. 生命周期状态机与 BOM `2026.12.1` 补丁升级 SOP

1. **完整 10 阶段安装 Pipeline（支持任意阶段失败 `--resume`）**：
   `preflight (64K/cgroup/端口/介质源探活)` → `pki (7 套独立 CA + 双栈 SAN)` → `etcd (v3.7.4 独立数据盘 + WAL 调优)` → `local-proxy (127.0.0.1:8443)` → `control-plane (apiserver/cm/scheduler + secretbox 静态加密)` → `cri (containerd v4 + certs.d 私仓 CA)` → `worker (kubelet + 自动 CSR 审批 + kube-proxy)` → `cni (Calico/Cilium)` → `coredns + csi` → `addons (Helm v4: Envoy Gateway / Headlamp / kube-prometheus-stack / kubelet-csr-approver)`。
2. **滚动升级与跨次版本约束**：
   - 严禁跨次版本跳跃（`1.35 → 1.37` 自动阻断，要求按 `1.35 → 1.36 → 1.37` 逐级升级）；
   - 顺序铁律：**先全量备份 etcd 快照至 OBS/S3 → 串行升级控制面（每台完成后等 `/readyz` 200 + 30s 观察）→ 按 `maxUnavailable` 分批 `cordon → drain → 升级 kubelet/二进制 → uncordon` 升级 Worker**；
   - **BOM 独立补丁响应（`2026.11.3 → 2026.12.1` 升级 `1.37.2 → 1.37.3`）**：无需升级 `kinstall` 主程序，只需导入签名后的新 BOM YAML 与 `bundle diff` 增量补丁包（~300 MiB），执行 `kinstall instance upgrade k8s-prod --bom-rev 2026.12.1` 即可全自动滚动完成。
3. **etcd 定时快照与灾难恢复**：
   - 自动部署 systemd timer 每 6h 执行 `etcdctl snapshot save` + SHA-256 校验并推送至企业 **OBS / SeaweedFS S3**（支持 WORM 防勒索）；
   - 灾难恢复 `kinstall cluster etcd-restore --snapshot <s3-or-local-uri>` 编排停全量控制面 → 全部 etcd 节点同步 `etcdutl snapshot restore`（重置 cluster-token 与 initial-cluster）→ 启动仲裁 → 逐台拉起控制面并核验全量 Pod 收敛。
