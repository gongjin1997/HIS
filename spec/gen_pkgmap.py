#!/usr/bin/env python3
"""spec/gen_pkgmap.py：由 S2 实测数据生成逻辑包映射草案 spec/pkgmap.yaml。

输入：research/pkgmap.json（按“命令文件路径 + Provides”反查的结果）、research/pkgresolve.py 中的 LOGICAL
      （逻辑包定义）、spec/osmatch.yaml（样本 → 规则 → 适配器族）。
输出：spec/pkgmap.yaml。解析顺序：规则覆盖 > 族覆盖（含父族）> pkgType 默认；pkg: null 表示该系统没有此包。
生成后由 spec/gen_os_docs.py 回放全部样本校验（解析结果必须与实测一致）。
用法：python3 spec/gen_pkgmap.py
"""
import collections, json, os, sys
import yaml

H = os.path.dirname(os.path.abspath(__file__))
R = os.path.join(H, '..', 'docs', 'research') if os.path.isdir(os.path.join(H, '..', 'docs', 'research')) else os.path.join(H, '..', 'research')
sys.path.insert(0, R)
from pkgresolve import LOGICAL  # noqa: E402  （仅导入定义，不触发扫描）

pm = json.load(open(os.path.join(R, 'pkgmap.json')))
om = yaml.safe_load(open(os.path.join(H, 'osmatch.yaml')))
FAM = om['families']
RULES = [r for r in om['rules'] if r['support'].get('samples')]
owner = {s: r for r in RULES for s in r['support']['samples']}

def fam_chain(rule):
    """规则的族链（由近到远），例如 kylin-v10 → [kylin, openeuler, rhel-like]。"""
    out, f = [], rule['family']
    if FAM.get(f, {}).get('parent') == 'per-rule':
        out.append(f); f = rule['parent']
    while f:
        out.append(f); f = FAM[f].get('parent')
    return out

def pkgtype(rule):
    for f in fam_chain(rule):
        if 'pkgType' in FAM[f]: return FAM[f]['pkgType']

# 逻辑包的类别、条件与用途（S3 细化；class：required 必需 / conditional 条件必需 / optional 可选 / detect 只检测冲突）
META = {
 'conntrack': ('required', None, 'kube-proxy 连接跟踪、连接清理'),
 'ipset': ('required', None, 'kube-proxy iptables 模式、Calico'),
 'socat': ('required', None, 'kubectl port-forward'),
 'iproute': ('required', None, '网络配置与排障（ip、tc）'),
 'ethtool': ('required', None, '网卡 offload 调整'),
 'nftables': ('required', None, 'kube-proxy nftables 模式、防火墙后端'),
 'iptables': ('required', None, 'kube-proxy iptables 模式、CNI'),
 'ebtables': ('required', None, '桥接流量规则（部分 CNI）'),
 'chrony': ('required', None, '时间同步'),
 'nfs-client': ('conditional', 'storage: nfs', 'NFS CSI、Longhorn RWX'),
 'iscsi': ('conditional', 'storage: iscsi', 'Longhorn、iSCSI 存储'),
 'multipath': ('conditional', 'storage: multipath', '多路径存储'),
 'lvm2': ('required', None, '数据盘（disk 模块）'),
 'xfsprogs': ('required', None, '数据盘格式化'),
 'e2fsprogs': ('required', None, '数据盘格式化'),
 'util-linux': ('required', None, 'lsblk、wipefs、findmnt'),
 'libseccomp': ('required', None, 'runc'),
 'container-selinux': ('conditional', 'mac: selinux', 'SELinux enforcing 下运行容器'),
 'selinux-utils': ('conditional', 'mac: selinux', 'semanage fcontext'),
 'restorecon': ('conditional', 'mac: selinux', 'restorecon 恢复文件标签'),
 'apparmor': ('conditional', 'mac: apparmor', 'AppArmor 配置加载'),
 'audit': ('required', None, '审计（等保）'),
 'curl': ('required', None, '下载、健康检查'),
 'tar': ('required', None, '解包'),
 'kmod': ('required', None, 'modprobe 加载内核模块'),
 'procps': ('required', None, 'sysctl、ps'),
 'iputils': ('required', None, 'ping'),
 'ca-certificates': ('required', None, 'TLS 根证书'),
 'openssl': ('required', None, '证书处理'),
 'sudo': ('required', None, '提权执行'),
 'ipvsadm': ('optional', None, '存量 ipvs 集群排障'),
 'iptables-legacy': ('optional', None, '检测 legacy 后端（03 Q-17）'),
 'parted': ('optional', None, '分区'),
 'gdisk': ('optional', None, 'sgdisk 分区（EL10 无，用 parted / sfdisk）'),
 'jq': ('optional', None, '排障'),
 'rsync': ('optional', None, '文件同步'),
 'wget': ('optional', None, '下载'),
 'unzip': ('optional', None, '解包'),
 'zstd': ('optional', None, '解包'),
 'lsof': ('optional', None, '排障'),
 'psmisc': ('optional', None, 'fuser、killall'),
 'sysstat': ('optional', None, 'iostat 等性能排障'),
 'tcpdump': ('optional', None, '抓包排障'),
 'vim': ('optional', None, '编辑'),
 'bash-completion': ('optional', None, '命令补全'),
 'net-tools': ('optional', None, 'netstat、ifconfig'),
 'dns-utils': ('optional', None, 'dig'),
 'netcat': ('optional', None, '端口连通性检查'),
 'numactl': ('optional', None, 'database Profile'),
 'tuned': ('optional', None, 'database Profile'),
 'irqbalance': ('optional', None, '中断均衡'),
 'keepalived': ('optional', None, 'VIP（K8s API 高可用等）'),
 'haproxy': ('optional', None, '负载均衡（K8s API 高可用等）'),
 'nvme-cli': ('optional', None, 'NVMe 盘识别'),
 'grubby': ('optional', None, '内核启动参数（rhel-like）'),
 'firewalld': ('detect', None, '检测防火墙'),
 'ufw': ('detect', None, '检测防火墙'),
 'fapolicyd': ('detect', None, '检测执行控制（03 §4.6）'),
 'NetworkManager': ('detect', None, '检测网络管理器'),
 'netplan': ('detect', None, '检测网络配置方式'),
 'python3': ('optional', None, '排障工具；kinstall 不依赖（ADR-025）'),
 'sudo-rs': ('detect', None, '检测 sudo 实现（03 Q-18）'),
 'rust-coreutils': ('detect', None, '检测 coreutils 实现（03 Q-18）'),
 'zram-generator': ('detect', None, '检测 zram swap'),
}
ORDER = [L for L, *_ in LOGICAL if not L.startswith('dist:')]

def cmds(paths):
    seen = []
    for p in paths:
        b = os.path.basename(p)
        if p.startswith(('/usr/sbin/', '/sbin/', '/usr/bin/', '/bin/')):
            if b not in seen: seen.append(b)
        elif p not in seen:
            seen.append(p)
    return seen

out_pk = []
for L, paths, rpm_c, deb_c in LOGICAL:
    if L.startswith('dist:'): continue
    names = {}  # 样本 → 实测包名（None = 缺失）
    for s, r in owner.items():
        if s not in pm: continue
        m = (pm[s].get('map') or {}).get(L)
        names[s] = m['pkg'] if m else None
    # 每条规则的包名（同一规则的 amd64 / arm64 样本一致，生成时校验）
    by_rule = {}
    for s, n in names.items():
        rid = owner[s]['id']
        if rid in by_rule and by_rule[rid] != n:
            sys.exit(f'{L}: 规则 {rid} 的样本包名不一致：{by_rule[rid]} / {n}')
        by_rule[rid] = n
    rmap = {r['id']: r for r in RULES}
    default = {}
    for t in ('rpm', 'deb'):
        c = collections.Counter(n for rid, n in by_rule.items() if pkgtype(rmap[rid]) == t)
        default[t] = c.most_common(1)[0][0] if c else None
        # 平票时优先候选列表靠前的名字
        if c:
            top = [n for n, k in c.items() if k == c.most_common(1)[0][1]]
            if len(top) > 1:
                cand = rpm_c if t == 'rpm' else deb_c
                default[t] = sorted(top, key=lambda n: cand.index(n) if n in cand else 99)[0]
    # 族覆盖：族内（直接成员）全部一致且不同于继承值时，记为族覆盖
    fam_over = {}
    def inherited(rule, upto=None):
        for f in fam_chain(rule):
            if f == upto: continue
            if f in fam_over: return fam_over[f]
        return default[pkgtype(rule)]
    for f in ['rhel-like', 'anolis', 'openeuler', 'kylin', 'suse', 'debian-like']:
        mem = [rid for rid in by_rule if rmap[rid]['family'] == f]
        if not mem: continue
        vals = {by_rule[rid] for rid in mem}
        if len(vals) == 1:
            v = vals.pop()
            if v != inherited(rmap[mem[0]], upto=f):
                fam_over[f] = v
    overrides = [{'when': {'family': f}, 'pkg': v} for f, v in fam_over.items()]
    rule_over = collections.defaultdict(list)
    for rid, n in by_rule.items():
        if n != inherited(rmap[rid]):
            rule_over[n].append(rid)
    for n, rids in sorted(rule_over.items(), key=lambda x: (x[0] is None, str(x[0]))):
        overrides.append({'when': {'rule': sorted(rids, key=lambda x: [r['id'] for r in RULES].index(x))}, 'pkg': n})
    cls, cond, purpose = META.get(L, ('optional', None, ''))
    item = {'name': L, 'class': cls}
    if cond: item['when'] = cond
    item['purpose'] = purpose
    c = cmds(paths)
    if c: item['provides'] = c
    item['default'] = {'rpm': default['rpm'], 'deb': default['deb']}
    if overrides: item['overrides'] = overrides
    item['candidates'] = {'rpm': rpm_c, 'deb': deb_c}
    out_pk.append(item)

dist = [{'name': L.split(':', 1)[1], 'provides': cmds(paths), 'candidates': {'rpm': rpm_c, 'deb': deb_c}}
        for L, paths, rpm_c, deb_c in LOGICAL if L.startswith('dist:')]

doc = {
 'apiVersion': 'kinstall.io/v1alpha1',
 'kind': 'PkgMap',
 'metadata': {'revision': om['metadata']['revision'], 'dataAsOf': om['metadata']['dataAsOf'],
              'generatedFrom': 'research/pkgmap.json + research/pkgresolve.py LOGICAL + spec/osmatch.yaml'},
 'resolution': ['规则覆盖（when.rule）', '族覆盖（when.family，含父族链）', 'pkgType 默认（default.rpm / default.deb）'],
 'packages': out_pk,
 'conflicts': dist,
}
hdr = ('# kinstall 逻辑包映射（pkgmap）—— 由 spec/gen_pkgmap.py 生成，勿手改；人工调整请改生成脚本或补测数据\n'
       '# 运行时：先按 provides 检查命令是否已存在（command -v），存在即满足；否则按解析出的包名安装。\n'
       '# pkg: null 表示该系统的官方源中没有此包：required 阻断并提示补离线包，optional 跳过并在报告中注明。\n'
       '# conflicts：发行版自带的容器 / K8s 包，只检测冲突、不安装（ADR-026）。\n')
with open(os.path.join(H, 'pkgmap.yaml'), 'w') as f:
    f.write(hdr)
    yaml.safe_dump(doc, f, allow_unicode=True, sort_keys=False, width=200, default_flow_style=None)
print(f'pkgmap.yaml：{len(out_pk)} 个逻辑包，{sum(1 for p in out_pk if p.get("overrides"))} 个有覆盖，{len(dist)} 个冲突检测项')
