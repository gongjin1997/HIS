# 03-A OS 软件源实测矩阵

> **定位**：03「CMDB 与主机初始化」的附录，汇总 2026-11-25 对各 OS 官方软件源的索引实测与包内实测数据，供 03 §3（适配矩阵）、§4.2（识别规则）、§4.3（包名映射）引用。设计结论写在 03，本页只放数据、方法和数据边界。
>
> **版本**：v1.1（S2 实测数据整理版，2026-11-25；2026-11-26 更正 §6 的 Ubuntu / Debian 日期，与 `spec/osmatch.yaml` 一致）。取代附页第 4 节的旧表（旧表中部分 arm64“缺包”是扫描范围不全造成的，已更正）。
>
> **数据性质**：这里是“仓库里有什么”的观察，不等于“默认安装了什么”或“真机上怎么运行”。默认 cgroup 取自 systemd 的编译默认值，页大小取自内核包附带的 `.config`；安装镜像的启动参数、云厂商镜像的定制都可能改变实际结果，真机验证放在 S15。

## 0. 关键结论

1. **覆盖范围**：30 个 OS 版本（amd64）+ 16 个 arm64 目标 + Oracle Linux UEK R7 / R8 内核仓库，共 48 组，均读取成功。RHEL / AlmaLinux 以 Rocky 代表；统信 UOS（源需授权）、CTyunOS（仓库路径未确认）未实测，见 §1.2。
2. **除 Anolis 7.9 缺 `container-selinux` 外，必需逻辑包在所有已测 OS 上都能找到**。按“命令文件路径 + Provides”反查后，旧表中的 multipath、netcat、python3 “缺失”都是包名映射问题；arm64 的大量“缺失”是旧扫描只读了 BaseOS 造成的。真正缺失的只有：Anolis 7.9 没有 `container-selinux`（SELinux enforcing 时无法安装容器运行时）；CentOS 7.9、Anolis 7.9 没有 `jq`、`zstd`（需 EPEL 或离线包）；EL10（Stream 10、Rocky 10）没有 `gdisk`（`sgdisk`），用 `parted` / `sfdisk` 替代。见 §4。
3. **编译默认 cgroup**（§3）：v2 —— EL9 / EL10、Oracle Linux 9、Anolis 23、Alinux 4、OpenCloudOS 9、**麒麟 V11 SP1 2603**、openSUSE Leap 15.6 / 16.0、Ubuntu 22.04+、Debian 11+；v1 —— CentOS 7 / 8、EL8 系（Rocky 8、Anolis 8、Alinux 3、BCLinux 8）、openEuler 22.03 / 24.03、BCLinux for Euler、麒麟 V10、**麒麟 V11 2503**；hybrid —— Ubuntu 20.04。systemd ≥ 256 的系统（EL10、Leap 16、Debian 13、Ubuntu 26.04）已没有该编译选项，只能运行 v2。
4. **kube-proxy nftables 模式**（内核 ≥ 5.13 且 nft ≥ 1.0.1）满足：EL9 / EL10、OL9、Anolis 23、Alinux 4、OC9、openEuler 24.03、麒麟 V11 / V11 SP1、Leap 16.0、Ubuntu 22.04+、Debian 12 / 13。不满足：CentOS 7 / 8、EL8 系、Anolis 7 / 8、Alinux 3、BCLinux 8 / for Euler、openEuler 22.03、麒麟 V10、Leap 15.6（nft 0.9.8）、Ubuntu 20.04（nft 0.9.3）、Debian 11。
5. **arm64 页大小**（§5）：**Rocky 8（代表 RHEL 8 系）和麒麟 V10 SP3 默认内核是 64K 页**（VA 48 位、3 级页表）；其余已测 arm64 发行版默认 4K 页（VA 48 位、4 级页表）。EL9 / EL10、openEuler 24.03、Alinux 3 / 4、Ubuntu 24.04、Leap 16.0 另提供可选 64K 内核（实测均为 64K / 48 位 / 3 级）。**麒麟 V10 → V11 升级会从 64K 变为 4K**，依赖页大小的组件（jemalloc 构建、bbolt 数据文件）需要重新评估。所有已测内核 VA 均为 48 位，tcmalloc 的 VA 约束（≥ 48 位）在官方内核上都满足。
6. **发行版自带容器 / K8s 包**（§7）：openEuler、麒麟、Anolis 23、Alinux 4、OC9、openSUSE、Ubuntu / Debian 源中都有 containerd / docker（moby、docker-engine、docker.io）/ runc，部分有 kubelet（如麒麟 V11 的 1.29.1、Anolis 23 的 1.29.15、Leap 15.6 的 1.18.10），版本普遍落后于 BOM。EL 系只有 `podman-docker`（docker 命令兼容层）。按 ADR-026 预检冲突，不自动卸载。
7. **安全与执行控制**（§8）：麒麟 V10 / V11 / V11 SP1 源中都有 KYSEC 组件（`kysec-daemon`、`kysec_auth`、`kysec_set` 等）；`fapolicyd` 存在于 EL8+、OL9、Anolis 8、Alinux 3、BCLinux 8、OC9、Ubuntu 24.04+、Debian 12+，在 openEuler、麒麟、Anolis 23、Alinux 4、SUSE 上没有。
8. **os-release**（§2）：所有已测 arm64 目标与对应 amd64 版本的 ID、ID_LIKE、VERSION_ID、PLATFORM_ID、PRETTY_NAME 完全一致。openEuler、麒麟、Anolis 23、BCLinux for Euler 没有 ID_LIKE；OpenCloudOS 的 ID_LIKE 是 `opencloudos`；Oracle Linux 的 ID_LIKE 只有 `fedora`；Alinux 4 的 ID_LIKE 是 `anolis`。识别规则必须先按 ID 显式匹配（03 §4.2）。

## 1. 方法与覆盖范围

### 1.1 方法

| 脚本（`research/`） | 做什么 | 输出 |
| --- | --- | --- |
| `repo_scan.py` | 读取各 OS 官方镜像的 `repodata/primary.xml`（rpm）或 `Packages` 索引（deb），按包名候选取约 60 个逻辑包的最高版本；从 `*-release` / `base-files` 包中解出 os-release | `repo_scan.json` |
| `pkgresolve.py` | 按“必须提供的命令文件路径 + Provides + 包名候选”反查逻辑包对应的实际包（等价于 `dnf provides /usr/sbin/multipath`；deb 按包名和 Provides）；同时记录内核相关包地址、按文件名发现 KYSEC 等安全组件 | `pkgmap.json` |
| `probe.py` | 下载少量包做包内实测：在 `libsystemd-shared` 中查找 `default-hierarchy=` 特性串（编译默认 cgroup）；读取 arm64 默认内核 `kernel-devel` / `linux-headers` 中的 `.config`（页大小、VA 位数、页表级数） | `probe.json` |
| `probe64.py` | 读取可选 64K 内核（`kernel-64k-devel`、`linux-headers-*-generic-64k`、`kernel-64kb-devel`）和 openSUSE 默认内核的 `.config` | `probe64.json` |
| `gen_03a.py` | 由以上 JSON 生成本页表格 | — |

复现：在能访问公网镜像的机器上依次执行 `python3 repo_scan.py`、`python3 pkgresolve.py`、`python3 probe.py`、`python3 probe64.py`、`python3 gen_03a.py`。`pkgresolve.py` 解析大仓库时单进程约占 2 GB 内存，4 GB 内存的机器用 `KI_WORKERS=2`。

### 1.2 未覆盖与数据边界

| 项目 | 原因 | 处理 |
| --- | --- | --- |
| RHEL、AlmaLinux | RHEL 源需订阅；Alma 与 Rocky 同为 RHEL 重建 | 以 Rocky 数据代表，包名与版本基本一致 |
| 统信 UOS V20 / V25 | 软件源需授权（返回 404） | os-release、内核、systemd 版本取自官方文档和社区资料（§2.1），默认 cgroup 推断，待真机确认 |
| CTyunOS 2 / 3 | 镜像站为动态页面，仓库路径未确认 | 未纳入表格；支持等级待定 |
| 麒麟桌面版、行业定制版 | 不在服务器目标范围 | — |
| 云厂商镜像定制 | 镜像可能改启动参数（cgroup、页大小内核）或预装包 | 以 Facts 实测为准 |
| 默认 cgroup | 只测了 systemd 编译默认值；内核命令行 `systemd.unified_cgroup_hierarchy=` 会覆盖 | 真机复核（S15） |
| 页大小 | 只测了内核包的 `.config`；安装时选择的内核变体（如 64K 内核）决定实际值 | 以 `getconf PAGESIZE` 为准（03 §2） |
| 默认安装集合 | 索引只说明“源里有”，不说明“装好了” | 初始化模块按需安装（S3） |

### 1.3 扫描的仓库

| 目标 | 架构 | 扫描的仓库 | 包数（去重） |
| --- | --- | --- | --- |
| CentOS 7.9 | amd64 | `vault.centos.org`：`/7.9.2009/os/x86_64/`、`/7.9.2009/updates/x86_64/`、`/7.9.2009/extras/x86_64/` | 8072 |
| CentOS 8.5 | amd64 | `vault.centos.org`：`/8.5.2111/BaseOS/x86_64/os/`、`/8.5.2111/AppStream/x86_64/os/` | 5745 |
| CentOS Stream 9 | amd64 | `mirror.stream.centos.org`：`/9-stream/BaseOS/x86_64/os/`、`/9-stream/AppStream/x86_64/os/` | 6132 |
| CentOS Stream 10 | amd64 | `mirror.stream.centos.org`：`/10-stream/BaseOS/x86_64/os/`、`/10-stream/AppStream/x86_64/os/` | 5712 |
| Rocky 8.10 | amd64 | `dl.rockylinux.org`：`/pub/rocky/8/BaseOS/x86_64/os/`、`/pub/rocky/8/AppStream/x86_64/os/` | 6333 |
| Rocky 9.9 | amd64 | `dl.rockylinux.org`：`/pub/rocky/9/BaseOS/x86_64/os/`、`/pub/rocky/9/AppStream/x86_64/os/` | 6057 |
| Rocky 10.3 | amd64 | `dl.rockylinux.org`：`/pub/rocky/10/BaseOS/x86_64/os/`、`/pub/rocky/10/AppStream/x86_64/os/` | 5671 |
| Oracle Linux 9.9 | amd64 | `yum.oracle.com`：`/repo/OracleLinux/OL9/baseos/latest/x86_64/`、`/repo/OracleLinux/OL9/appstream/x86_64/` | 6144 |
| Anolis 7.9 | amd64 | `mirrors.openanolis.cn`：`/anolis/7.9/os/x86_64/os/`、`/anolis/7.9/updates/x86_64/os/` | 7764 |
| Anolis 8.10 | amd64 | `mirrors.openanolis.cn`：`/anolis/8.10/BaseOS/x86_64/os/`、`/anolis/8.10/AppStream/x86_64/os/`、`/anolis/8.10/kernel-5.10/x86_64/os/` | 7506 |
| Anolis 23.5 | amd64 | `mirrors.openanolis.cn`：`/anolis/23.5/os/x86_64/os/`、`/anolis/23.5/updates/x86_64/os/` | 14026 |
| Alibaba Cloud Linux 3 | amd64 | `mirrors.aliyun.com`：`/alinux/3/os/x86_64/`、`/alinux/3/updates/x86_64/`、`/alinux/3/plus/x86_64/`、`/alinux/3/module/x86_64/` | 8482 |
| Alibaba Cloud Linux 4 | amd64 | `mirrors.aliyun.com`：`/alinux/4/os/x86_64/os/`、`/alinux/4/updates/x86_64/os/`、`/alinux/4/plus/x86_64/os/` | 13818 |
| BCLinux 8.8 | amd64 | `mirrors.cmecloud.cn`：`/bclinux/el8.8/BaseOS/x86_64/os/`、`/bclinux/el8.8/AppStream/x86_64/os/` | 7262 |
| OpenCloudOS 9.6 | amd64 | `mirrors.opencloudos.tech`：`/opencloudos/9.6/BaseOS/x86_64/os/`、`/opencloudos/9.6/AppStream/x86_64/os/` | 7058 |
| openEuler 22.03 LTS SP4 | amd64 | `repo.openeuler.org`：`/openEuler-22.03-LTS-SP4/OS/x86_64/`、`/openEuler-22.03-LTS-SP4/everything/x86_64/`、`/openEuler-22.03-LTS-SP4/update/x86_64/` | 18057 |
| openEuler 24.03 LTS SP4 | amd64 | `repo.openeuler.org`：`/openEuler-24.03-LTS-SP4/OS/x86_64/`、`/openEuler-24.03-LTS-SP4/everything/x86_64/`、`/openEuler-24.03-LTS-SP4/update/x86_64/` | 19596 |
| BCLinux for Euler 22.10U2 | amd64 | `mirrors.cmecloud.cn`：`/bclinux/oe22.10U2/OS/x86_64/`、`/bclinux/oe22.10U2/everything/x86_64/` | 16991 |
| 麒麟 V10 SP3 2403 | amd64 | `update.cs2c.com.cn`：`/NS/V10/V10SP3-2403/os/adv/lic/base/x86_64/`、`/NS/V10/V10SP3-2403/os/adv/lic/updates/x86_64/` | 15488 |
| 麒麟 V11 2503 | amd64 | `update.cs2c.com.cn`：`/NS/V11/2503/os/adv/lic/base/x86_64/`、`/NS/V11/2503/os/adv/lic/updates/x86_64/` | 19649 |
| 麒麟 V11 SP1 2603 | amd64 | `update.cs2c.com.cn`：`/NS/V11/V11SP1-2603/os/adv/lic/base/x86_64/`、`/NS/V11/V11SP1-2603/os/adv/lic/updates/x86_64/` | 19743 |
| openSUSE Leap 15.6 | amd64 | `download.opensuse.org`：`/distribution/leap/15.6/repo/oss/`、`/update/leap/15.6/oss/` | 43747 |
| openSUSE Leap 16.0 | amd64 | `download.opensuse.org`：`/distribution/leap/16.0/repo/oss/` | 37324 |
| Ubuntu 20.04 | amd64 | `archive.ubuntu.com`：`/ubuntu/focal ['main', 'universe'] amd64`、`/ubuntu/focal-updates ['main', 'universe'] amd64` | 77461 |
| Ubuntu 22.04 | amd64 | `archive.ubuntu.com`：`/ubuntu/jammy ['main', 'universe'] amd64`、`/ubuntu/jammy-updates ['main', 'universe'] amd64` | 83042 |
| Ubuntu 24.04 | amd64 | `archive.ubuntu.com`：`/ubuntu/noble ['main', 'universe'] amd64`、`/ubuntu/noble-updates ['main', 'universe'] amd64` | 76405 |
| Ubuntu 26.04 | amd64 | `archive.ubuntu.com`：`/ubuntu/resolute ['main', 'universe'] amd64`、`/ubuntu/resolute-updates ['main', 'universe'] amd64` | 75644 |
| Debian 11 | amd64 | `deb.debian.org`：`/debian/bullseye ['main'] amd64`、`/debian/bullseye-updates ['main'] amd64` | 58653 |
| Debian 12 | amd64 | `deb.debian.org`：`/debian/bookworm ['main'] amd64`、`/debian/bookworm-updates ['main'] amd64` | 63436 |
| Debian 13 | amd64 | `deb.debian.org`：`/debian/trixie ['main'] amd64`、`/debian/trixie-updates ['main'] amd64` | 68824 |
| Rocky 8.10 | arm64 | `dl.rockylinux.org`：`/pub/rocky/8/BaseOS/aarch64/os/`、`/pub/rocky/8/AppStream/aarch64/os/` | 5885 |
| Rocky 9.9 | arm64 | `dl.rockylinux.org`：`/pub/rocky/9/BaseOS/aarch64/os/`、`/pub/rocky/9/AppStream/aarch64/os/` | 5750 |
| Rocky 10.3 | arm64 | `dl.rockylinux.org`：`/pub/rocky/10/BaseOS/aarch64/os/`、`/pub/rocky/10/AppStream/aarch64/os/` | 5588 |
| Anolis 8.10 | arm64 | `mirrors.openanolis.cn`：`/anolis/8.10/BaseOS/aarch64/os/`、`/anolis/8.10/AppStream/aarch64/os/`、`/anolis/8.10/kernel-5.10/aarch64/os/` | 7144 |
| Anolis 23.5 | arm64 | `mirrors.openanolis.cn`：`/anolis/23.5/os/aarch64/os/`、`/anolis/23.5/updates/aarch64/os/` | 13937 |
| Alibaba Cloud Linux 3 | arm64 | `mirrors.aliyun.com`：`/alinux/3/os/aarch64/`、`/alinux/3/updates/aarch64/`、`/alinux/3/plus/aarch64/`、`/alinux/3/module/aarch64/`、`/alinux/3/plus-64k/aarch64/` | 7982 |
| Alibaba Cloud Linux 4 | arm64 | `mirrors.aliyun.com`：`/alinux/4/os/aarch64/os/`、`/alinux/4/updates/aarch64/os/`、`/alinux/4/plus/aarch64/os/`、`/alinux/4/kernel-64k/aarch64/os/` | 13724 |
| OpenCloudOS 9.6 | arm64 | `mirrors.opencloudos.tech`：`/opencloudos/9.6/BaseOS/aarch64/os/`、`/opencloudos/9.6/AppStream/aarch64/os/` | 6954 |
| openEuler 22.03 LTS SP4 | arm64 | `repo.openeuler.org`：`/openEuler-22.03-LTS-SP4/OS/aarch64/`、`/openEuler-22.03-LTS-SP4/everything/aarch64/`、`/openEuler-22.03-LTS-SP4/update/aarch64/` | 17980 |
| openEuler 24.03 LTS SP4 | arm64 | `repo.openeuler.org`：`/openEuler-24.03-LTS-SP4/OS/aarch64/`、`/openEuler-24.03-LTS-SP4/everything/aarch64/`、`/openEuler-24.03-LTS-SP4/update/aarch64/` | 19536 |
| 麒麟 V10 SP3 2403 | arm64 | `update.cs2c.com.cn`：`/NS/V10/V10SP3-2403/os/adv/lic/base/aarch64/`、`/NS/V10/V10SP3-2403/os/adv/lic/updates/aarch64/` | 15391 |
| 麒麟 V11 2503 | arm64 | `update.cs2c.com.cn`：`/NS/V11/2503/os/adv/lic/base/aarch64/`、`/NS/V11/2503/os/adv/lic/updates/aarch64/` | 19571 |
| 麒麟 V11 SP1 2603 | arm64 | `update.cs2c.com.cn`：`/NS/V11/V11SP1-2603/os/adv/lic/base/aarch64/`、`/NS/V11/V11SP1-2603/os/adv/lic/updates/aarch64/` | 19654 |
| Ubuntu 24.04 | arm64 | `ports.ubuntu.com`：`/ubuntu-ports/noble ['main', 'universe'] arm64`、`/ubuntu-ports/noble-updates ['main', 'universe'] arm64` | 76608 |
| Debian 12 | arm64 | `deb.debian.org`：`/debian/bookworm ['main'] arm64`、`/debian/bookworm-updates ['main'] arm64` | 62662 |
| openSUSE Leap 16.0 | arm64 | `download.opensuse.org`：`/distribution/leap/16.0/repo/oss/` | 36278 |
| Oracle Linux 9 UEK R7 | amd64 | `yum.oracle.com`：`/repo/OracleLinux/OL9/UEKR7/x86_64/` | 63 |
| Oracle Linux 9 UEK R8 | amd64 | `yum.oracle.com`：`/repo/OracleLinux/OL9/UEKR8/x86_64/` | 74 |

## 2. os-release 实测取值（R-OS-12）

| OS | 来源包 | ID | ID_LIKE | VERSION_ID | PLATFORM_ID | VERSION_CODENAME | PRETTY_NAME |
| --- | --- | --- | --- | --- | --- | --- | --- |
| CentOS 7.9 | `centos-release-7-9.2009.2.el7.centos` | `centos` | `rhel fedora` | `7` | — | — | CentOS Linux 7 (Core) |
| CentOS 8.5 | `centos-linux-release-8.5-1.2111.el8` | `centos` | `rhel fedora` | `8` | `platform:el8` | — | CentOS Linux 8 |
| CentOS Stream 9 | `centos-stream-release-9.0-38.el9` | `centos` | `rhel fedora` | `9` | `platform:el9` | — | CentOS Stream 9 |
| CentOS Stream 10 | `centos-stream-release-10.0-23.el10` | `centos` | `rhel fedora` | `10` | `platform:el10` | — | CentOS Stream 10 (Coughlan) |
| Rocky 8.10 | `rocky-release-8.10-1.14.el8` | `rocky` | `rhel centos fedora` | `8.10` | `platform:el8` | — | Rocky Linux 8.10 (Green Obsidian) |
| Rocky 9.9 | `rocky-release-9.9-1.0.el9` | `rocky` | `rhel centos fedora` | `9.9` | `platform:el9` | — | Rocky Linux 9.9 (Blue Onyx) |
| Rocky 10.3 | `rocky-release-10.3-1.0.el10` | `rocky` | `rhel centos fedora` | `10.3` | `platform:el10` | — | Rocky Linux 10.3 (Red Quartz) |
| Oracle Linux 9.9 | `oraclelinux-release-9.9-1.0.4.el9` | `ol` | `fedora` | `9.9` | `platform:el9` | — | Oracle Linux Server 9.9 |
| Anolis 7.9 | `anolis-release-7.9-1.an7` | `anolis` | `rhel fedora centos` | `7.9` | — | — | Anolis OS 7.9 |
| Anolis 8.10 | `anolis-release-8.10-2.an8` | `anolis` | `rhel fedora centos` | `8.10` | `platform:an8` | — | Anolis OS 8.10 |
| Anolis 23.5 | `anolis-release-23.5-15.an23` | `anolis` | — | `23.5` | `platform:an23` | — | Anolis OS 23.5 |
| Alibaba Cloud Linux 3 | `alinux-release-3.2104.14.1-1.al8` | `alinux` | `rhel fedora centos anolis` | `3` | `platform:al8` | — | Alibaba Cloud Linux 3.2104 U14.1 (OpenAnolis Edition) |
| Alibaba Cloud Linux 4 | `alinux-release-4-17.1.alnx4` | `alinux` | `anolis` | `4` | `platform:alnx4` | — | Alibaba Cloud Linux 4.0.7.1 (OpenAnolis Edition) |
| BCLinux 8.8 | `bclinux-release-8.8-2.el8.bclinux` | `bclinux` | `rhel fedora centos anolis` | `8.8` | `platform:an8` | — | BigCloud Enterprise Linux 8.8 (Core) |
| OpenCloudOS 9.6 | `opencloudos-release-9.6-2.oc9` | `opencloudos` | `opencloudos` | `9.6` | `platform:oc9` | — | OpenCloudOS 9.6 |
| openEuler 22.03 LTS SP4 | `openEuler-release-22.03LTS_SP4-57.oe2203sp4` | `openEuler` | — | `22.03` | — | — | openEuler 22.03 (LTS-SP4) |
| openEuler 24.03 LTS SP4 | `openEuler-release-24.03LTS_SP4-58.oe2403sp4` | `openEuler` | — | `24.03` | — | — | openEuler 24.03 (LTS-SP4) |
| BCLinux for Euler 22.10U2 | `bclinux-release-22.10U2-56.oe2203sp2.bclinux` | `bclinux` | — | `22.10U2` | — | — | BigCloud Enterprise Linux For Euler 22.10U2 LTS |
| 麒麟 V10 SP3 2403 | `kylin-release-10-24.6.p195.03.ky10` | `kylin` | — | `V10` | — | — | Kylin Linux Advanced Server V10 (Halberd) |
| 麒麟 V11 2503 | `kylin-release-11-01.6.p246.01.ky11` | `kylin` | — | `V11` | — | — | Kylin Linux Advanced Server V11 (Swan25) |
| 麒麟 V11 SP1 2603 | `kylin-release-11-01.6.p269.ky11` | `kylin` | — | `V11` | — | — | Kylin Linux Advanced Server V11 (swan26) |
| openSUSE Leap 15.6 | `openSUSE-release-15.6-lp156.417.4.1` | `opensuse-leap` | `suse opensuse` | `15.6` | — | — | openSUSE Leap 15.6 |
| openSUSE Leap 16.0 | `Leap-release-16.0-lp160.281.1` | `opensuse-leap` | `suse opensuse` | `16.0` | — | — | openSUSE Leap 16.0 |
| Ubuntu 20.04 | `base-files` | `ubuntu` | `debian` | `20.04` | — | `focal` | Ubuntu 20.04.6 LTS |
| Ubuntu 22.04 | `base-files` | `ubuntu` | `debian` | `22.04` | — | `jammy` | Ubuntu 22.04.5 LTS |
| Ubuntu 24.04 | `base-files` | `ubuntu` | `debian` | `24.04` | — | `noble` | Ubuntu 24.04.5 LTS |
| Ubuntu 26.04 | `base-files` | `ubuntu` | `debian` | `26.04` | — | `resolute` | Ubuntu 26.04.1 LTS |
| Debian 11 | `base-files` | `debian` | — | `11` | — | `bullseye` | Debian GNU/Linux 11 (bullseye) |
| Debian 12 | `base-files` | `debian` | — | `12` | — | `bookworm` | Debian GNU/Linux 12 (bookworm) |
| Debian 13 | `base-files` | `debian` | — | `13` | — | `trixie` | Debian GNU/Linux 13 (trixie) |

arm64 与 amd64 对比：全部一致。

### 2.1 未实测系统的 os-release（文档与社区资料，待真机确认）

| 系统 | 已知取值 | 辅助识别 | 来源 |
| --- | --- | --- | --- |
| UOS Server 20 1070e（openEuler 路线） | `ID=uos`、`VERSION_ID="20"`、`NAME` / `PRETTY_NAME="UOS Server 20"`（arm64 1050e 见到 "UnionTech OS Server 20"）、`VERSION_CODENAME=fuyu`、`PLATFORM_ID="platform:uel20"`，无 ID_LIKE；内核串含 `uel20` | `/etc/os-version`（`[Version]` 段：`SystemName=UOS Server`、`ProductType=Server`、`MinorVersion=1070` 等）；`uosinfo`（如 "UOS Server 20 (1070e) 20240423 amd64"）；`/etc/issue` | [统信 1070 安装手册](https://www.uniontech.com)、腾讯云社区文章（2414101 / 2433934 / 2426660） |
| UOS Server 20 1070a（龙蜥路线） | `ID=uos`、`VERSION_ID="20"`、`VERSION_CODENAME=kongzi`、`PLATFORM_ID="platform:uelc20"` | 同上 | 同上 |
| UOS Server V25（2510） | 2026-09-08 发布，Linux 6.6 LTS；os-release 未见公开样例 | — | [统信 V25 发布](https://www.uniontech.com/news-info/3007.html) |

1070u1 发行注记：默认内核 4.19，ISO 另带可选 5.10；A 版内核 4.19.0 / 5.10.0、E 版 4.19.90 / 5.10.0；glibc 2.28；systemd 243（A / E 相同）。systemd 243 与麒麟 V10 相同，按同类系统推断默认为 cgroup v1，需真机确认。

## 3. 核心组件版本（amd64）

“编译默认 cgroup”取自 systemd 的 `default-hierarchy=` 编译选项：v1 = legacy、v2 = unified；“仅 v1”为 systemd 219（不支持 unified 选项）；“仅 v2”为 systemd ≥ 256（已移除该选项）。OpenCloudOS 9 的特性串在 `systemd-shared` 子包中。

| OS | 内核（默认源） | 可选 / HWE 内核 | systemd | 编译默认 cgroup | glibc | nftables | iptables 包 | libseccomp | OpenSSL | kube-proxy nftables |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| CentOS 7.9 | 3.10.0-1160 | — | 219 | 仅 v1 | 2.17 | 0.8 | iptables 1.4.21 | 2.3.1 | 1.0.2k | ❌ 内核与 nft 均不足 |
| CentOS 8.5 | 4.18.0-348 | — | 239 | v1 | 2.28 | 0.9.3 | iptables 1.8.4 | 2.5.1 | 1.1.1k | ❌ 内核与 nft 均不足 |
| CentOS Stream 9 | 5.14.0-765 | — | 252 | v2 | 2.34 | 1.0.9 | iptables-nft 1.8.10 | 2.5.6 | 3.5.9 | ✅ |
| CentOS Stream 10 | 6.12.0-284 | — | 257 | 仅 v2 | 2.39 | 1.1.7 | iptables-nft 1.8.13 | 2.5.6 | 3.5.9 | ✅ |
| Rocky 8.10 | 4.18.0-553 | — | 239 | v1 | 2.28 | 1.0.4 | iptables 1.8.5 | 2.5.2 | 1.1.1k | ❌ 内核不足 |
| Rocky 9.9 | 5.14.0-741 | — | 252 | v2 | 2.34 | 1.0.9 | iptables-nft 1.8.10 | 2.5.6 | 3.5.9 | ✅ |
| Rocky 10.3 | 6.12.0-264 | — | 257 | 仅 v2 | 2.39 | 1.1.5 | iptables-nft 1.8.11 | 2.5.6 | 3.5.9 | ✅ |
| Oracle Linux 9.9 | 5.14.0-741 | UEK 8 6.12.0；UEK 7 5.15.0（新装默认 UEK 8） | 252 | v2 | 2.34 | 1.0.9 | iptables-nft 1.8.10 | 2.5.6 | 3.5.9 | ✅ |
| Anolis 7.9 | 3.10.0-1160 | — | 219 | 仅 v1 | 2.17 | 0.8 | iptables 1.4.21 | 2.3.1 | 1.0.2k | ❌ 内核与 nft 均不足 |
| Anolis 8.10 | 5.10.134-20 | RHCK 4.18.0（BaseOS）；默认源最高为 ANCK | 239 | v1 | 2.28 | 1.0.4 | iptables 1.8.5 | 2.5.2 | 1.1.1k | ❌ 内核不足 |
| Anolis 23.5 | 6.6.102-8 | — | 255 | v2 | 2.38 | 1.0.8 | iptables-nft 1.8.9 | 2.5.4 | 3.0.12 | ✅ |
| Alibaba Cloud Linux 3 | 5.10.134-20 | — | 239 | v1 | 2.32 | 1.0.4 | iptables 1.8.5 | 2.5.2 | 1.1.1k | ❌ 内核不足 |
| Alibaba Cloud Linux 4 | 6.6.102-8 | — | 255 | v2 | 2.38 | 1.0.8 | iptables-nft 1.8.9 | 2.5.4 | 3.0.12 | ✅ |
| BCLinux 8.8 | 4.19.0-553 | — | 239 | v1 | 2.28 | 0.9.3 | iptables 1.8.4 | 2.5.2 | 1.1.1k | ❌ 内核与 nft 均不足 |
| OpenCloudOS 9.6 | 6.6.119-53 | — | 255 | v2 | 2.38 | 1.0.8 | iptables-nft 1.8.9 | 2.5.4 | 3.0.12 | ✅ |
| openEuler 22.03 LTS SP4 | 5.10.0-342 | — | 249 | v1 | 2.34 | 1.0.0 | iptables 1.8.7 | 2.5.3 | 1.1.1wa | ❌ 内核与 nft 均不足 |
| openEuler 24.03 LTS SP4 | 6.6.0-159 | — | 255 | v1 | 2.38 | 1.0.8 | iptables 1.8.9 | 2.5.4 | 3.5.6 | ✅ |
| BCLinux for Euler 22.10U2 | 5.10.0-153 | — | 249 | v1 | 2.34 | 1.0.0 | iptables 1.8.7 | 2.5.3 | 1.1.1m | ❌ 内核与 nft 均不足 |
| 麒麟 V10 SP3 2403 | 4.19.90-89 | — | 243 | v1 | 2.28 | 0.9.6 | iptables 1.8.5 | 2.5.0 | 1.1.1f | ❌ 内核与 nft 均不足 |
| 麒麟 V11 2503 | 6.6.0-32 | — | 255 | v1 | 2.38 | 1.0.8 | iptables 1.8.9 | 2.5.4 | 3.0.12 | ✅ |
| 麒麟 V11 SP1 2603 | 6.6.0-40 | — | 255 | v2 | 2.38 | 1.0.8 | iptables 1.8.9 | 2.5.4 | 3.0.12 | ✅ |
| openSUSE Leap 15.6 | 6.4.0 | — | 254 | v2 | 2.38 | 0.9.8 | iptables 1.8.7 | 2.5.3 | 3.1.4 | ❌ nft 过旧 |
| openSUSE Leap 16.0 | 6.12.0 | — | 257 | 仅 v2 | 2.40 | 1.1.3 | iptables 1.8.11 | 2.6.0 | 3.5.0 | ✅ |
| Ubuntu 20.04 | 5.4.0-216 | 5.15.0-139 | 245 | hybrid | 2.31 | 0.9.3 | iptables 1.8.4 | 2.5.1 | 1.1.1f | ❌ nft 0.9.3 过旧（HWE 也不满足） |
| Ubuntu 22.04 | 5.15.0-204 | 6.8.0-149 | 249 | v2 | 2.35 | 1.0.2 | iptables 1.8.7 | 2.5.3 | 3.0.2 | ✅ |
| Ubuntu 24.04 | 6.8.0-151 | 7.0.0-44 | 255 | v2 | 2.39 | 1.0.9 | iptables 1.8.10 | 2.5.5 | 3.0.13 | ✅ |
| Ubuntu 26.04 | 7.0.0-44 | — | 259 | 仅 v2 | 2.43 | 1.1.6 | iptables 1.8.11 | 2.6.0 | 3.5.5 | ✅ |
| Debian 11 | 5.10.223-1 | — | 247 | v2 | 2.31 | 0.9.8 | iptables 1.8.7 | 2.5.1 | 1.1.1w | ❌ 内核与 nft 均不足 |
| Debian 12 | 6.1.176-1 | — | 252 | v2 | 2.36 | 1.0.6 | iptables 1.8.9 | 2.5.4 | 3.0.20 | ✅ |
| Debian 13 | 6.12.117-1 | — | 257 | 仅 v2 | 2.41 | 1.1.3 | iptables 1.8.11 | 2.6.0 | 3.5.9 | ✅ |

说明：

- Anolis 8.10 默认源中最高的内核是 ANCK 5.10，RHCK 4.18 也在源中；实际运行哪个以 `uname -r` 为准（03 Q-06）。
- Alibaba Cloud Linux 3 的内核在 `plus` 仓库（已由该仓库 repodata 索引，包文件在 `plus/<架构>/kernels/`）。
- Oracle Linux 9 表中“内核”列为 RHCK；9.7 / 9.8 起新装默认 UEK 8（6.12），升级来的系统保持 UEK 7（5.15）；aarch64 只提供 UEK。kube-proxy nftables 判定按 UEK 8 / RHCK 5.14 均满足。
- Ubuntu 的“可选 / HWE 内核”为 HWE 元包当前指向的版本。
- “iptables 包”为 `iptables-nft` 时，后端固定为 nf_tables；为 `iptables` 时后端由 alternatives 决定，运行时用 `iptables -V` 判定（03 Q-17）。

## 4. 逻辑包映射

### 4.1 映射表（amd64，粗体为初始化必需）

单元格只有一个包名表示族内全部 OS 相同；括号内为使用该包名的 OS。缩写：C7 / C8 = CentOS 7 / 8，S9 / S10 = Stream 9 / 10，R8–R10 = Rocky，OL9 = Oracle Linux 9，An7 / An8 / An23 = Anolis，Al3 / Al4 = Alinux，BC8 = BCLinux 8，OC9 = OpenCloudOS 9，oE22 / oE24 = openEuler 22.03 / 24.03，BCoE = BCLinux for Euler，KY10 / KY11 / KY11S1 = 麒麟 V10 SP3 / V11 2503 / V11 SP1，L15 / L16 = Leap 15.6 / 16.0，U20–U26 = Ubuntu，D11–D13 = Debian。

| 逻辑名 | EL：CentOS / Stream / Rocky / OL | 龙蜥系：Anolis / Alinux / BCLinux 8 / OC | openEuler 系 | 麒麟 | openSUSE | Debian / Ubuntu |
| --- | --- | --- | --- | --- | --- | --- |
| **conntrack** | `conntrack-tools` | `conntrack-tools` | `conntrack-tools` | `conntrack-tools` | `conntrack-tools` | `conntrack` |
| **ipset** | `ipset` | `ipset` | `ipset` | `ipset` | `ipset` | `ipset` |
| **socat** | `socat` | `socat` | `socat` | `socat` | `socat` | `socat` |
| **iproute** | `iproute` | `iproute` | `iproute` | `iproute` | `iproute2` | `iproute2` |
| **ethtool** | `ethtool` | `ethtool` | `ethtool` | `ethtool` | `ethtool` | `ethtool` |
| **nftables** | `nftables` | `nftables` | `nftables` | `nftables` | `nftables` | `nftables` |
| **iptables** | `iptables-nft`（S9、S10、R9、R10、OL9）；`iptables`（C7、C8、R8） | `iptables`（An7、An8、Al3、BC8）；`iptables-nft`（An23、Al4、OC9） | `iptables` | `iptables` | `iptables` | `iptables` |
| **ebtables** | `iptables-nft`（S9、S10、R9、R10、OL9）；`iptables-ebtables`（C8、R8）；`ebtables`（C7） | `iptables-ebtables`（An8、Al3、BC8）；`iptables-nft`（An23、Al4、OC9）；`ebtables`（An7） | `ebtables` | `ebtables` | `ebtables`（L15）；`iptables`（L16） | `ebtables` |
| **chrony** | `chrony` | `chrony` | `chrony` | `chrony` | `chrony` | `chrony` |
| **nfs-client** | `nfs-utils` | `nfs-utils` | `nfs-utils` | `nfs-utils` | `nfs-client` | `nfs-common` |
| **iscsi** | `iscsi-initiator-utils` | `iscsi-initiator-utils` | `open-iscsi` | `open-iscsi` | `open-iscsi` | `open-iscsi` |
| **multipath** | `device-mapper-multipath` | `device-mapper-multipath`（An7、An8、Al3、BC8、OC9）；`multipath-tools`（An23、Al4） | `multipath-tools` | `multipath-tools` | `multipath-tools` | `multipath-tools` |
| **lvm2** | `lvm2` | `lvm2` | `lvm2` | `lvm2` | `lvm2` | `lvm2` |
| **xfsprogs** | `xfsprogs` | `xfsprogs` | `xfsprogs` | `xfsprogs` | `xfsprogs` | `xfsprogs` |
| **e2fsprogs** | `e2fsprogs` | `e2fsprogs` | `e2fsprogs` | `e2fsprogs` | `e2fsprogs` | `e2fsprogs` |
| **util-linux** | `util-linux` | `util-linux` | `util-linux` | `util-linux` | `util-linux` | `util-linux` |
| **libseccomp** | `libseccomp` | `libseccomp` | `libseccomp` | `libseccomp` | `libseccomp2` | `libseccomp2` |
| **container-selinux** | `container-selinux` | `container-selinux`（An8、An23、Al3、Al4、BC8、OC9）；无：An7 | `container-selinux` | `container-selinux` | `container-selinux`（L16）；无：L15 | 无 |
| **selinux-utils** | `policycoreutils-python-utils`（C8、S9、S10、R8、R9、R10、OL9）；`policycoreutils-python`（C7） | `policycoreutils-python-utils`（An8、An23、Al3、Al4、BC8、OC9）；`policycoreutils-python`（An7） | `policycoreutils-python-utils` | `policycoreutils-python-utils` | `policycoreutils-python-utils` | `policycoreutils-python-utils` |
| **restorecon** | `policycoreutils` | `policycoreutils` | `policycoreutils` | `policycoreutils` | `policycoreutils` | `policycoreutils` |
| **apparmor** | 无 | 无 | 无 | `apparmor`（KY11、KY11S1）；`apparmor-parser`（KY10） | `apparmor-parser` | `apparmor` |
| **audit** | `audit` | `audit` | `audit` | `audit` | `audit` | `auditd` |
| **curl** | `curl` | `curl` | `curl` | `curl` | `curl` | `curl` |
| **tar** | `tar` | `tar` | `tar` | `tar` | `tar` | `tar` |
| **kmod** | `kmod` | `kmod` | `kmod` | `kmod` | `kmod` | `kmod` |
| **procps** | `procps-ng` | `procps-ng` | `procps-ng` | `procps-ng` | `procps` | `procps` |
| **iputils** | `iputils` | `iputils` | `iputils` | `iputils` | `iputils` | `iputils-ping` |
| **ca-certificates** | `ca-certificates` | `ca-certificates` | `ca-certificates` | `ca-certificates` | `ca-certificates` | `ca-certificates` |
| **openssl** | `openssl` | `openssl` | `openssl` | `openssl` | `openssl` | `openssl` |
| **sudo** | `sudo` | `sudo` | `sudo` | `sudo` | `sudo` | `sudo` |
| ipvsadm | `ipvsadm` | `ipvsadm` | `ipvsadm` | `ipvsadm` | `ipvsadm` | `ipvsadm` |
| iptables-legacy | 无 | `iptables`（An8、Al3、BC8）；`iptables-legacy`（An23、Al4、OC9）；无：An7 | `iptables` | `iptables` | `iptables`（L15）；无：L16 | `iptables` |
| parted | `parted` | `parted` | `parted` | `parted` | `parted` | `parted` |
| gdisk | `gdisk`（C7、C8、S9、R8、R9、OL9）；无：S10、R10 | `gdisk` | `gdisk` | `gdisk` | `gptfdisk` | `gdisk` |
| jq | `jq`（C8、S9、S10、R8、R9、R10、OL9）；无：C7 | `jq`（An8、An23、Al3、Al4、BC8、OC9）；无：An7 | `jq` | `jq` | `jq` | `jq` |
| rsync | `rsync` | `rsync` | `rsync` | `rsync` | `rsync` | `rsync` |
| wget | `wget` | `wget` | `wget` | `wget` | `wget` | `wget` |
| unzip | `unzip` | `unzip` | `unzip` | `unzip` | `unzip` | `unzip` |
| zstd | `zstd`（C8、S9、S10、R8、R9、R10、OL9）；无：C7 | `zstd`（An8、An23、Al3、Al4、BC8、OC9）；无：An7 | `zstd` | `zstd` | `zstd` | `zstd` |
| lsof | `lsof` | `lsof` | `lsof` | `lsof` | `lsof` | `lsof` |
| psmisc | `psmisc` | `psmisc` | `psmisc` | `psmisc` | `psmisc` | `psmisc` |
| sysstat | `sysstat` | `sysstat` | `sysstat` | `sysstat` | `sysstat` | `sysstat` |
| tcpdump | `tcpdump` | `tcpdump` | `tcpdump` | `tcpdump` | `tcpdump` | `tcpdump` |
| vim | `vim-enhanced` | `vim-enhanced` | `vim-enhanced` | `vim-enhanced` | `vim` | `vim` |
| bash-completion | `bash-completion` | `bash-completion` | `bash-completion` | `bash-completion` | `bash-completion` | `bash-completion` |
| net-tools | `net-tools` | `net-tools` | `net-tools` | `net-tools` | `net-tools` | `net-tools` |
| dns-utils | `bind-utils` | `bind-utils` | `bind-utils` | `bind-utils` | `bind-utils` | `bind9-dnsutils` |
| netcat | `nmap-ncat` | `nmap-ncat` | `nmap` | `nmap` | `netcat-openbsd` | `netcat-openbsd` |
| numactl | `numactl` | `numactl` | `numactl` | `numactl` | `numactl` | `numactl` |
| tuned | `tuned` | `tuned` | `tuned` | `tuned` | `tuned` | `tuned` |
| irqbalance | `irqbalance` | `irqbalance` | `irqbalance` | `irqbalance` | `irqbalance` | `irqbalance` |
| keepalived | `keepalived` | `keepalived` | `keepalived` | `keepalived` | `keepalived` | `keepalived` |
| haproxy | `haproxy` | `haproxy` | `haproxy` | `haproxy` | `haproxy` | `haproxy` |
| nvme-cli | `nvme-cli` | `nvme-cli` | `nvme-cli` | `nvme-cli` | `nvme-cli` | `nvme-cli` |
| grubby | `grubby` | `grubby` | `grubby` | `grubby` | `grubby`（L15）；无：L16 | 无 |
| firewalld | `firewalld` | `firewalld` | `firewalld` | `firewalld` | `firewalld` | `firewalld` |
| ufw | 无 | 无 | 无 | 无 | `ufw`（L16）；无：L15 | `ufw` |
| fapolicyd | `fapolicyd`（C8、S9、S10、R8、R9、R10、OL9）；无：C7 | `fapolicyd`（An8、Al3、BC8、OC9）；无：An7、An23、Al4 | 无 | 无 | 无 | `fapolicyd`（U24、U26、D12、D13）；无：U20、U22、D11 |
| NetworkManager | `NetworkManager` | `NetworkManager` | `NetworkManager` | `NetworkManager` | `NetworkManager` | `network-manager` |
| netplan | 无 | 无 | 无 | 无 | 无 | `netplan.io` |
| python3 | `python3`（C7、S9、S10、R9、R10、OL9）；`python39`（C8）；`python3.12`（R8） | `python3`（An7、An23、Al4、OC9）；`python3.11`（An8、Al3）；`python39`（BC8） | `python3` | `python3` | `python3` | `python3` |
| sudo-rs | 无 | 无 | 无 | 无 | 无 | `sudo-rs`（U24、U26、D13）；无：U20、U22、D11、D12 |
| rust-coreutils | 无 | 无 | 无 | 无 | 无 | `rust-coreutils`（U24、U26、D12、D13）；无：U20、U22、D11 |
| zram-generator | `zram-generator`（S9、S10、R9、R10、OL9）；无：C7、C8、R8 | `zram-generator`（An23、Al4、OC9）；无：An7、An8、Al3、BC8 | 无 | 无 | `zram-generator` | `systemd-zram-generator`（U22、U24、U26、D12、D13）；`zram-tools`（U20、D11） |

### 4.2 真实缺失

“不适用”的不计入：rpm 系的 apparmor、ufw、netplan；deb 系的 container-selinux、grubby；sudo-rs、rust-coreutils、zram-generator 属于可选组件；EL8+ 不提供 `iptables-legacy` 属发行版设计（只用 nft 后端）。

| OS | 必需逻辑包缺失 | 可选工具缺失 | 说明 |
| --- | --- | --- | --- |
| CentOS 7.9 | 无 | jq、zstd | |
| CentOS 8.5 | 无 | 无 | |
| CentOS Stream 9 | 无 | 无 | |
| CentOS Stream 10 | 无 | gdisk | |
| Rocky 8.10 | 无 | 无 | |
| Rocky 9.9 | 无 | 无 | |
| Rocky 10.3 | 无 | gdisk | |
| Oracle Linux 9.9 | 无 | 无 | |
| Anolis 7.9 | container-selinux | jq、zstd | |
| Anolis 8.10 | 无 | 无 | |
| Anolis 23.5 | 无 | 无 | |
| Alibaba Cloud Linux 3 | 无 | 无 | |
| Alibaba Cloud Linux 4 | 无 | 无 | |
| BCLinux 8.8 | 无 | 无 | |
| OpenCloudOS 9.6 | 无 | 无 | |
| openEuler 22.03 LTS SP4 | 无 | 无 | |
| openEuler 24.03 LTS SP4 | 无 | 无 | |
| BCLinux for Euler 22.10U2 | 无 | 无 | |
| 麒麟 V10 SP3 2403 | 无 | 无 | |
| 麒麟 V11 2503 | 无 | 无 | |
| 麒麟 V11 SP1 2603 | 无 | 无 | |
| openSUSE Leap 15.6 | 无 | 无 | |
| openSUSE Leap 16.0 | 无 | 无 | |
| Ubuntu 20.04 | 无 | 无 | |
| Ubuntu 22.04 | 无 | 无 | |
| Ubuntu 24.04 | 无 | 无 | |
| Ubuntu 26.04 | 无 | 无 | |
| Debian 11 | 无 | 无 | |
| Debian 12 | 无 | 无 | |
| Debian 13 | 无 | 无 | |
| Rocky 8.10（arm64） | 无 | 无 | |
| Rocky 9.9（arm64） | 无 | 无 | |
| Rocky 10.3（arm64） | 无 | gdisk | |
| Anolis 8.10（arm64） | 无 | 无 | |
| Anolis 23.5（arm64） | 无 | 无 | |
| Alibaba Cloud Linux 3（arm64） | 无 | 无 | |
| Alibaba Cloud Linux 4（arm64） | 无 | 无 | |
| OpenCloudOS 9.6（arm64） | 无 | 无 | |
| openEuler 22.03 LTS SP4（arm64） | 无 | 无 | |
| openEuler 24.03 LTS SP4（arm64） | 无 | 无 | |
| 麒麟 V10 SP3 2403（arm64） | 无 | 无 | |
| 麒麟 V11 2503（arm64） | 无 | 无 | |
| 麒麟 V11 SP1 2603（arm64） | 无 | 无 | |
| Ubuntu 24.04（arm64） | 无 | 无 | |
| Debian 12（arm64） | 无 | 无 | |

处理：必需包缺失时阻断并提示补离线包；可选工具缺失时跳过并在报告中注明。Anolis 7.9 没有 `container-selinux`，SELinux enforcing 时不能安装容器运行时，只能 permissive 或提供离线包（S3 细化）。

### 4.3 平台关心的“有无”型组件

| OS | iptables-legacy | fapolicyd | firewalld | ufw | NetworkManager | netplan | grubby | sudo-rs | rust-coreutils | zram-generator | python3 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| CentOS 7.9 | — | — | ✅ | — | ✅ | — | ✅ | — | — | — | `python3` 3.6.8 |
| CentOS 8.5 | — | ✅ | ✅ | — | ✅ | — | ✅ | — | — | — | `python39` 3.9.6 |
| CentOS Stream 9 | — | ✅ | ✅ | — | ✅ | — | ✅ | — | — | ✅ | `python3` 3.9.25 |
| CentOS Stream 10 | — | ✅ | ✅ | — | ✅ | — | ✅ | — | — | ✅ | `python3` 3.12.16 |
| Rocky 8.10 | — | ✅ | ✅ | — | ✅ | — | ✅ | — | — | — | `python3.12` 3.12.16 |
| Rocky 9.9 | — | ✅ | ✅ | — | ✅ | — | ✅ | — | — | ✅ | `python3` 3.9.25 |
| Rocky 10.3 | — | ✅ | ✅ | — | ✅ | — | ✅ | — | — | ✅ | `python3` 3.12.16 |
| Oracle Linux 9.9 | — | ✅ | ✅ | — | ✅ | — | ✅ | — | — | ✅ | `python3` 3.9.25 |
| Anolis 7.9 | — | — | ✅ | — | ✅ | — | ✅ | — | — | — | `python3` 3.6.8 |
| Anolis 8.10 | ✅ `iptables` | ✅ | ✅ | — | ✅ | — | ✅ | — | — | — | `python3.11` 3.11.13 |
| Anolis 23.5 | ✅ | — | ✅ | — | ✅ | — | ✅ | — | — | ✅ | `python3` 3.11.6 |
| Alibaba Cloud Linux 3 | ✅ `iptables` | ✅ | ✅ | — | ✅ | — | ✅ | — | — | — | `python3.11` 3.11.13 |
| Alibaba Cloud Linux 4 | ✅ | — | ✅ | — | ✅ | — | ✅ | — | — | ✅ | `python3` 3.11.6 |
| BCLinux 8.8 | ✅ `iptables` | ✅ | ✅ | — | ✅ | — | ✅ | — | — | — | `python39` 3.9.16 |
| OpenCloudOS 9.6 | ✅ | ✅ | ✅ | — | ✅ | — | ✅ | — | — | ✅ | `python3` 3.11.6 |
| openEuler 22.03 LTS SP4 | ✅ `iptables` | — | ✅ | — | ✅ | — | ✅ | — | — | — | `python3` 3.9.9 |
| openEuler 24.03 LTS SP4 | ✅ `iptables` | — | ✅ | — | ✅ | — | ✅ | — | — | — | `python3` 3.11.6 |
| BCLinux for Euler 22.10U2 | ✅ `iptables` | — | ✅ | — | ✅ | — | ✅ | — | — | — | `python3` 3.9.9 |
| 麒麟 V10 SP3 2403 | ✅ `iptables` | — | ✅ | — | ✅ | — | ✅ | — | — | — | `python3` 3.7.9 |
| 麒麟 V11 2503 | ✅ `iptables` | — | ✅ | — | ✅ | — | ✅ | — | — | — | `python3` 3.11.6 |
| 麒麟 V11 SP1 2603 | ✅ `iptables` | — | ✅ | — | ✅ | — | ✅ | — | — | — | `python3` 3.11.6 |
| openSUSE Leap 15.6 | ✅ `iptables` | — | ✅ | — | ✅ | — | ✅ | — | — | ✅ | `python3` 3.6.15 |
| openSUSE Leap 16.0 | — | — | ✅ | ✅ | ✅ | — | — | — | — | ✅ | `python3` 3.13.14 |
| Ubuntu 20.04 | ✅ `iptables` | — | ✅ | ✅ | ✅ | ✅ `netplan.io` | — | — | — | ✅ `zram-tools` | `python3` 3.8.2 |
| Ubuntu 22.04 | ✅ `iptables` | — | ✅ | ✅ | ✅ | ✅ `netplan.io` | — | — | — | ✅ `systemd-zram-generator` | `python3` 3.10.6 |
| Ubuntu 24.04 | ✅ `iptables` | ✅ | ✅ | ✅ | ✅ | ✅ `netplan.io` | — | `sudo-rs` 0.2.2 | `rust-coreutils` 0.0.24 | ✅ `systemd-zram-generator` | `python3` 3.12.3 |
| Ubuntu 26.04 | ✅ `iptables` | ✅ | ✅ | ✅ | ✅ | ✅ `netplan.io` | — | `sudo-rs` 0.2.13 | `rust-coreutils` 0.10.0 | ✅ `systemd-zram-generator` | `python3` 3.14.3 |
| Debian 11 | ✅ `iptables` | — | ✅ | ✅ | ✅ | ✅ `netplan.io` | — | — | — | ✅ `zram-tools` | `python3` 3.9.2 |
| Debian 12 | ✅ `iptables` | ✅ | ✅ | ✅ | ✅ | ✅ `netplan.io` | — | — | `rust-coreutils` 0.0.17 | ✅ `systemd-zram-generator` | `python3` 3.11.2 |
| Debian 13 | ✅ `iptables` | ✅ | ✅ | ✅ | ✅ | ✅ `netplan.io` | — | `sudo-rs` 0.2.5 | `rust-coreutils` 0.0.30 | ✅ `systemd-zram-generator` | `python3` 3.13.5 |

python3 列为按候选顺序命中的包：EL8 系没有名为 `python3` 的包，`dnf install python3` 由模块流（python36 等）提供，另有 python3.11 / 3.12 可选；`platform-python` 是系统内部使用的解释器。kinstall 不依赖 Python（ADR-025），本列仅供排障工具参考。

## 5. arm64 页大小与虚拟地址位数（R-OS-17）

取自默认内核 `kernel-devel` / `linux-headers` 中的 `.config`（`CONFIG_ARM64_*_PAGES`、`CONFIG_ARM64_VA_BITS`、`CONFIG_PGTABLE_LEVELS`）。openSUSE Leap 16.0 取 `kernel-default-devel`。

| OS（arm64） | 默认内核 | 页大小 | VA 位数 | 页表级数 | 可选 64K 内核（包 / 版本 → 页大小 / VA / 级数） | 编译默认 cgroup |
| --- | --- | --- | --- | --- | --- | --- |
| Rocky 8.10 | 4.18.0-553 | **64K** | 48 | 3 | — | v1 |
| Rocky 9.9 | 5.14.0-741 | **4K** | 48 | 4 | `kernel-64k-devel` 5.14.0-741.27.1 → 64K / 48 / 3 | v2 |
| Rocky 10.3 | 6.12.0-264 | **4K** | 48 | 4 | `kernel-64k-devel` 6.12.0-264.20.1 → 64K / 48 / 3 | 仅 v2 |
| Anolis 8.10 | 5.10.134-20 | **4K** | 48 | 4 | — | v1 |
| Anolis 23.5 | 6.6.102-8 | **4K** | 48 | 4 | — | v2 |
| Alibaba Cloud Linux 3 | 5.10.134-20 | **4K** | 48 | 4 | `kernel-64k-devel` 5.10.134-16.1.64k.1 → 64K / 48 / 3 | v1 |
| Alibaba Cloud Linux 4 | 6.6.102-8 | **4K** | 48 | 4 | `kernel-64k-devel` 6.6.102-8.1 → 64K / 48 / 3 | v2 |
| OpenCloudOS 9.6 | 6.6.119-53 | **4K** | 48 | 4 | — | v2 |
| openEuler 22.03 LTS SP4 | 5.10.0-342 | **4K** | 48 | 4 | — | v1 |
| openEuler 24.03 LTS SP4 | 6.6.0-159 | **4K** | 48 | 4 | `kernel-64k-devel` 6.6.0-159.4.23.177 → 64K / 48 / 3 | v1 |
| 麒麟 V10 SP3 2403 | 4.19.90-89 | **64K** | 48 | 3 | — | v1 |
| 麒麟 V11 2503 | 6.6.0-32 | **4K** | 48 | 4 | — | v1 |
| 麒麟 V11 SP1 2603 | 6.6.0-40 | **4K** | 48 | 4 | — | v2 |
| Ubuntu 24.04 | 6.8.0-151 | **4K** | 48 | 4 | `linux-headers-6.8.0-151-generic-64k` 6.8.0-151.151 → 64K / 48 / 3 | v2 |
| Debian 12 | 6.1.176-1 | **4K** | 48 | 4 | — | v2 |
| openSUSE Leap 16.0 | 6.12.0 | **4K** | 48 | 4 | `kernel-64kb-devel` 6.12.0-160000.41.1 → 64K / 48 / 3 | 仅 v2 |

结论：

- 默认 64K：Rocky 8（RHEL 8 系 arm64 默认内核）、麒麟 V10 SP3。社区实测麒麟 V10 arm64 主机 `getconf PAGESIZE` 为 65536，4K 构建的 jemalloc 报 “Unsupported system page size”（qdrant#6450、milvus#27726），与本表一致。
- 默认 4K：EL9 / EL10、Anolis 8 / 23、Alinux 3 / 4、OC9、openEuler 22.03 / 24.03、麒麟 V11 / V11 SP1、Ubuntu 24.04、Debian 12、Leap 16.0。
- 可选 64K 内核：EL9 / EL10（`kernel-64k`）、openEuler 24.03（`kernel-64k`）、Alinux 3（`plus-64k` 仓库）、Alinux 4（`kernel-64k` 仓库，包名也叫 kernel）、Ubuntu 24.04（`linux-image-generic-64k`）、Leap 16.0（`kernel-64kb`）。
- VA 位数：全部为 48 位。tcmalloc（Envoy）类组件的 VA 约束在已测官方内核上都满足，但第三方或定制内核仍需按 Facts 预检。
- 对 kinstall 的影响（ADR-028 建议）：组件 BOM 记录每个构建支持的页大小；安装前按主机 `PageSize` 预检；同一集群内混用 4K / 64K 主机时提示（bbolt 数据文件不能跨页大小直接拷贝）。

## 6. 生命周期调研汇总（截至 2026-11-25）

| 系统 | 生命周期 | 来源 |
| --- | --- | --- |
| openEuler | 2025-08 起新政策：LTS 每 4 年一个，社区支持 4 年，LTS 全生命周期 6 年（4 年全面支持 + 2 年维护）；SP 在偶数年 12 月、奇数年 6 月（可选）和 12 月发布；创新版每年 9 月发布、支持 6 个月。22.03 LTS 已于 2026-03-30 EOL；24.03 LTS 基础版 2026-05、SP1 2026-12、**SP2 已于 2026-04-24 停维**（特性收编至 SP3）、SP3 2027-12、SP4 2027-03（论坛答复 SP4 之后可能转 SP5）；创新版 26.09 至 2027-03。EOL 后源保留但不再更新 → **按 SP 粒度判定生命周期** | [生命周期](https://www.openeuler.org/en/other/lifecycle/)、[下载页](https://www.openeuler.org/en/download/)、停维公告（mailweb.openeuler.org）、[论坛](https://forum.openeuler.org/t/topic/19672) |
| Alibaba Cloud Linux | Alinux 4：开发支持 2025-07～2030-06、维护 2030-07～2035-06，扩展至 2038-06-30；内核 6.6 LTS、glibc 2.38、默认 cgroup v2。Alinux 3：开发支持 2021-04～2026-03、维护 2026-04～2031-03，扩展至 2034-03-31；内核 5.10 ANCK、默认 cgroup v1。Alinux 2 已于 2026-03-31 停止支持 | [Alibaba Cloud Linux 概述](https://help.aliyun.com/zh/ecs/user-guide/alibaba-cloud-linux)、[Alinux 4 与 3 的差异](https://www.alibabacloud.com/help/en/alinux/getting-started/differences-between-alinux-4-and-3) |
| Oracle Linux 9 | Premier 支持至 2032-06，Extended 至 2035-06；x86-64-v2；9.7 / 9.8 新装默认 UEK 8（6.12），RHCK 5.14 可选 | Oracle Linux 9 内核版本矩阵与 9.8 Shipped Kernels（docs.oracle.com）、Oracle Lifetime Support Policy |
| openSUSE Leap | 15.6 已于 2026-04-30 EOL；16.0 于 2025-10-01 发布，每个小版本支持 24 个月（16.0 约至 2027-10），最终版 16.6 预计 2031 年秋 | news.opensuse.org（2026-04-01 公告）、[openSUSE Lifetime](https://en.opensuse.org/Lifetime)、Leap 16 发行说明 |
| OpenCloudOS | V8：2022-01-26 发布，完整支持至 2027-05-31、维护至 2029-05-31；V9：2023-04-30 发布，完整支持至 2030-04-30、维护至 2033-04-30；V10 计划 2027-04-30 | [OpenCloudOS 版本介绍](https://docs.opencloudos.org/release/oc_intro/) |
| 麒麟 | V10 高级服务器 5 + 5 + 3 共 13 年（V10 白皮书，2024-03）；各 SP 的具体停服日期官方未公开；V11 2503 基于 6.6 内核 | 麒麟 V10 白皮书、天翼云文档 |
| 统信 UOS | 天翼云文档：UnionTechOS 停止支持 2032 年；V25（2510）2026-09-08 发布 | 天翼云文档、[统信新闻](https://www.uniontech.com/news-info/3007.html) |
| CTyunOS | CTyunOS 2 停止支持 2031-06-30；CTyunOS 3 至 2032-12-31 | [天翼云文档](https://www.ctyun.cn/document/10027726/10599037) |
| BCLinux | 镜像站有 el8.8、oe21.10、oe22.10（U1、U2）、v21.10U4 / U5、v25 等目录；未找到公开的生命周期 | [移动云镜像站](https://mirrors.cmecloud.cn/bclinux/) |
| 其他（已在 01 登记） | Anolis 7 已于 2024-06-30 EOL、Anolis 8 至 2031-03-31、Anolis 23 维护至 2033；Rocky / Alma 8 至 2029-05-31、9 至 2032-05-31、10 至 2035-05-31；Stream 9 至 2027-05-31、Stream 10 至 2030-05-31；Ubuntu 20.04 标准支持 2025-05-31 结束、22.04 至 2027-06、24.04 至 2029-05（List of releases 页为 2029-06）、26.04 至 2031-05；Debian 11 LTS 2026-08-31 结束、12 安全支持 2026-06 结束（LTS 至 2028-06-30）、13 安全支持至 2028-08-09（LTS 至 2030-06-30，ELTS 至 2035-06-30） | 01 R-OS-01～R-OS-10 |

## 7. 发行版自带的容器 / K8s 包（ADR-026：只记录冲突，不使用）

| OS | containerd | docker | runc | podman | cri-o | kubelet |
| --- | --- | --- | --- | --- | --- | --- |
| CentOS 7.9 | — | 1.13.1 | 1.0.0 | 1.6.4 | — | `kubernetes-node` 1.5.2 |
| CentOS 8.5 | — | `podman-docker` 3.3.1 | 1.0.2 | 3.3.1 | — | — |
| CentOS Stream 9 | — | `podman-docker` 5.8.5 | 1.4.2 | 5.8.5 | — | — |
| CentOS Stream 10 | — | — | — | 6.1.0 | — | — |
| Rocky 8.10 | — | `podman-docker` 4.9.4 | 1.2.9 | 4.9.4 | — | — |
| Rocky 9.9 | — | `podman-docker` 5.8.5 | 1.4.2 | 5.8.5 | — | — |
| Rocky 10.3 | — | — | — | 6.1.4 | — | — |
| Oracle Linux 9.9 | — | `podman-docker` 5.8.5 | 1.4.2 | 5.8.5 | — | — |
| Anolis 7.9 | — | — | — | — | — | — |
| Anolis 8.10 | — | `podman-docker` 4.9.4 | 1.2.9 | 4.9.4 | — | — |
| Anolis 23.5 | 1.7.34 | 24.0.9 | 1.3.3 | — | — | `kubernetes-node` 1.29.15 |
| Alibaba Cloud Linux 3 | — | `podman-docker` 4.9.4 | 1.2.9 | 4.9.4 | — | — |
| Alibaba Cloud Linux 4 | 1.7.34 | 24.0.9 | 1.2.8 | — | — | `kubernetes-node` 1.29.15 |
| BCLinux 8.8 | — | `podman-docker` 4.4.1 | 1.1.12 | 4.4.1 | — | — |
| OpenCloudOS 9.6 | 1.7.29 | `moby` 29.7.0 | 1.3.3 | — | — | — |
| openEuler 22.03 LTS SP4 | 1.2.0 | `docker-engine` 18.09.0 | 1.1.3 | 3.4.4 | — | — |
| openEuler 24.03 LTS SP4 | 1.6.22 | `docker-engine` 18.09.0 | 1.1.8 | 4.9.4 | — | — |
| BCLinux for Euler 22.10U2 | 1.2.0 | `docker-engine` 18.09.0 | `docker-runc` 1.1.3 | 3.4.4 | — | — |
| 麒麟 V10 SP3 2403 | 1.2.0 | `docker-engine` 18.09.0 | `docker-runc` 1.0.0.rc3 | 0.10.1 | — | `kubernetes-node` 1.20.2 |
| 麒麟 V11 2503 | 1.7.23 | `moby` 24.0.9 | 1.1.12 | 4.9.4 | — | `kubernetes-kubelet` 1.29.1 |
| 麒麟 V11 SP1 2603 | 1.7.23 | `moby` 24.0.9 | 1.1.12 | 4.9.4 | — | `kubernetes-kubelet` 1.29.1 |
| openSUSE Leap 15.6 | 1.7.10 | 24.0.7_ce | 1.1.12 | 4.8.3 | 1.24.3 | `kubernetes-kubelet` 1.18.10 |
| openSUSE Leap 16.0 | 1.7.33 | 29.7.2_ce | 1.3.6 | 5.4.2 | — | — |
| Ubuntu 20.04 | 1.7.24 | `docker.io` 26.1.3 | 1.1.12 | — | — | `kubernetes` 1.0 |
| Ubuntu 22.04 | 2.2.1 | `docker.io` 29.1.3 | 1.3.4 | 3.4.4 | — | `kubernetes` 1.0 |
| Ubuntu 24.04 | 2.2.1 | `docker.io` 29.1.3 | 1.3.4 | 4.9.3 | — | `kubernetes` 1.0 |
| Ubuntu 26.04 | 2.2.2 | `docker.io` 29.1.3 | 1.4.0 | 5.7.0 | — | — |
| Debian 11 | 1.4.13 | `docker.io` 20.10.5 | 1.0.0 | 3.0.1 | — | — |
| Debian 12 | 1.6.20 | `docker.io` 20.10.24 | 1.1.5 | 4.3.1 | — | — |
| Debian 13 | 1.7.24 | `docker.io` 26.1.5 | 1.1.15 | 5.4.2 | — | — |

`podman-docker` 只提供 `docker` 命令兼容层，不是 Docker Engine。Ubuntu 的 `kubernetes` 1.0 是无关的同名包。kinstall 只用 BOM 中的上游二进制；预检发现这些包已安装、或 dnf / apt 的更新可能覆盖时，提示卸载或 `exclude` / `apt-mark hold`。

## 8. 安全与执行控制相关组件（R-OS-18）

按文件名匹配 KYSEC、麒麟安全工具、`fapolicyd-cli` 等（已去掉 SELinux 策略模块与 libcgroup 等无关匹配）。

| OS | 发现的安全 / 执行控制相关文件（按文件名匹配） |
| --- | --- |
| CentOS 7.9 | — |
| CentOS 8.5 | `fapolicyd`：`/usr/sbin/fapolicyd-cli` |
| CentOS Stream 9 | `fapolicyd`：`/usr/sbin/fapolicyd-cli` |
| CentOS Stream 10 | `fapolicyd`：`/usr/sbin/fapolicyd-cli` |
| Rocky 8.10 | `fapolicyd`：`/usr/sbin/fapolicyd-cli` |
| Rocky 9.9 | `fapolicyd`：`/usr/sbin/fapolicyd-cli` |
| Rocky 10.3 | `fapolicyd`：`/usr/sbin/fapolicyd-cli` |
| Oracle Linux 9.9 | `fapolicyd`：`/usr/sbin/fapolicyd-cli` |
| Anolis 7.9 | — |
| Anolis 8.10 | `fapolicyd`：`/usr/sbin/fapolicyd-cli` |
| Anolis 23.5 | — |
| Alibaba Cloud Linux 3 | `fapolicyd`：`/usr/sbin/fapolicyd-cli` |
| Alibaba Cloud Linux 4 | — |
| BCLinux 8.8 | `fapolicyd`：`/usr/sbin/fapolicyd-cli` |
| OpenCloudOS 9.6 | `fapolicyd`：`/usr/sbin/fapolicyd-cli` |
| openEuler 22.03 LTS SP4 | — |
| openEuler 24.03 LTS SP4 | — |
| BCLinux for Euler 22.10U2 | — |
| 麒麟 V10 SP3 2403 | `kylin-release`：`/usr/bin/nkvers`；`kylin-security-daemon`：`/usr/sbin/kysec-sync-notify`、`/usr/sbin/kysec_init`；`kysec-auth`：`/sbin/kysec_auth`；`kysec-daemon`：`/etc/pam.d/kysec-authority`、`/usr/sbin/kysec-daemon`、`/usr/sbin/kysec-dinit`、`/usr/sbin/kysec-init`、`/usr/sbin/kysec-wlinit`；`kysec-module-authorize-test`：`/usr/sbin/kysec-module-authorize-test`；`kysec-sync-daemon`：`/usr/sbin/kysec-sync`、`/usr/sbin/kysec-sync-daemon`；`kysec-utils`：`/usr/sbin/getstatus`、`/usr/sbin/kysec_get`、`/usr/sbin/kysec_monitor`、`/usr/sbin/kysec_netctl_init`、`/usr/sbin/kysec_set`、`/usr/sbin/kysec_sync_init`；`kyseclog-daemon`：`/etc/logrotate.d/kysec`、`/usr/sbin/kyseclogd`；`security-switch`：`/usr/sbin/security-switch` |
| 麒麟 V11 2503 | `kylin-release`：`/usr/bin/nkvers`；`kysec-daemon`：`/etc/pam.d/kysec-authority`、`/usr/sbin/kysec-daemon`、`/usr/sbin/kysec-dinit`、`/usr/sbin/kysec-init`、`/usr/sbin/kysec-wlinit`；`kysec-module-authorize-test`：`/usr/sbin/kysec-module-authorize-test`；`kysec-sync-daemon`：`/usr/sbin/kysec-sync`、`/usr/sbin/kysec-sync-daemon`；`kysec-utils`：`/usr/sbin/getstatus`、`/usr/sbin/kysec_get`、`/usr/sbin/kysec_monitor`、`/usr/sbin/kysec_netctl_init`、`/usr/sbin/kysec_policy`、`/usr/sbin/kysec_set`；`kysec2-migration`：`/usr/sbin/kysec_migration`；`kysec2-rbac-utils`：`/usr/sbin/kysec-rbac-query`、`/usr/sbin/kysec-rbac-set`；`kysec2-scene`：`/usr/bin/kysec-scene-daemon`；`kysec2-scene-example`：`/usr/bin/kysec-scene-accessctl`、`/usr/bin/kysec-scene-boxctl`、`/usr/bin/kysec-scene-devctl`、`/usr/bin/kysec-scene-example`、`/usr/bin/kysec-scene-exectl-label`、`/usr/bin/kysec-scene-kidctl`；`kyseclog-daemon`：`/etc/logrotate.d/kysec`、`/usr/sbin/kyseclogd`；`security-switch`：`/usr/sbin/security-switch` |
| 麒麟 V11 SP1 2603 | `kylin-release`：`/usr/bin/nkvers`；`kysec-daemon`：`/etc/kysec/kysec_caller.json`、`/usr/sbin/kysec-daemon`、`/usr/sbin/kysec_monitor`、`/usr/sbin/kysec_sync`；`kysec-module-authorize-test`：`/usr/sbin/kysec-module-authorize-test`；`kysec-utils`：`/usr/sbin/getstatus`、`/usr/sbin/kysec_get`、`/usr/sbin/kysec_policy`、`/usr/sbin/kysec_set`、`/usr/sbin/setstatus`；`kysec2-control-module`：`/usr/bin/kysec2-control-module-tool`；`kysec2-devctl-module`：`/usr/bin/kysec2-devctl-module-tool`；`kysec2-exectl-module`：`/usr/bin/kysec2-exectl-module-tool`；`kysec2-kbox`：`/usr/bin/kysec2-kbox-tool`、`/usr/sbin/kysec2-kbox-load`；`kysec2-rbac-utils`：`/etc/kysec/rbac/secadm.conf`、`/usr/sbin/kysec-rbac-query`、`/usr/sbin/kysec-rbac-set`；`kyseclog-daemon`：`/etc/logrotate.d/kysec`、`/usr/sbin/kyseclogd`；`security-switch`：`/usr/sbin/security-switch`；`x-kernel`：`/etc/ksaf/policy/kysec_xkernel.policy` |
| openSUSE Leap 15.6 | — |
| openSUSE Leap 16.0 | — |
| Ubuntu 20.04 | — |
| Ubuntu 22.04 | — |
| Ubuntu 24.04 | — |
| Ubuntu 26.04 | — |
| Debian 11 | — |
| Debian 12 | — |
| Debian 13 | — |

麒麟 V10 / V11 / V11 SP1 源中都有 KYSEC 组件。KYSEC 的状态、检测命令和处理方式见 03 §4.6；`fapolicyd` 的检测与授权加白同见 03 §4.6。
