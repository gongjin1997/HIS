#!/usr/bin/env python3
"""kinstall 调研（S2）：由 repo_scan.json / pkgmap.json / probe.json / probe64.json 生成 03-A 的表格（Markdown）。
用法：python3 gen_03a.py > 03a_tables.md"""
import json, os, re, sys
H = os.path.dirname(os.path.abspath(__file__))
J = lambda n: json.load(open(os.path.join(H, n))) if os.path.exists(os.path.join(H, n)) else {}
scan, pm, pr, p64 = J('repo_scan.json'), J('pkgmap.json'), J('probe.json'), J('probe64.json')

OS = [  # key, 显示名, 短名, 族
 ('centos7', 'CentOS 7.9', 'C7', 'EL'), ('centos8', 'CentOS 8.5', 'C8', 'EL'),
 ('stream9', 'CentOS Stream 9', 'S9', 'EL'), ('stream10', 'CentOS Stream 10', 'S10', 'EL'),
 ('rocky8', 'Rocky 8.10', 'R8', 'EL'), ('rocky9', 'Rocky 9.9', 'R9', 'EL'), ('rocky10', 'Rocky 10.3', 'R10', 'EL'),
 ('ol9', 'Oracle Linux 9.9', 'OL9', 'EL'),
 ('anolis7', 'Anolis 7.9', 'An7', 'AN'), ('anolis8', 'Anolis 8.10', 'An8', 'AN'), ('anolis23', 'Anolis 23.5', 'An23', 'AN'),
 ('alinux3', 'Alibaba Cloud Linux 3', 'Al3', 'AN'), ('alinux4', 'Alibaba Cloud Linux 4', 'Al4', 'AN'),
 ('bclinux8', 'BCLinux 8.8', 'BC8', 'AN'), ('oc9', 'OpenCloudOS 9.6', 'OC9', 'AN'),
 ('oe2203', 'openEuler 22.03 LTS SP4', 'oE22', 'OE'), ('oe2403', 'openEuler 24.03 LTS SP4', 'oE24', 'OE'),
 ('bclinux-oe', 'BCLinux for Euler 22.10U2', 'BCoE', 'OE'),
 ('kylinv10', '麒麟 V10 SP3 2403', 'KY10', 'KY'), ('kylinv11', '麒麟 V11 2503', 'KY11', 'KY'), ('kylinv11sp1', '麒麟 V11 SP1 2603', 'KY11S1', 'KY'),
 ('leap156', 'openSUSE Leap 15.6', 'L15', 'SUSE'), ('leap160', 'openSUSE Leap 16.0', 'L16', 'SUSE'),
 ('ubuntu2004', 'Ubuntu 20.04', 'U20', 'DEB'), ('ubuntu2204', 'Ubuntu 22.04', 'U22', 'DEB'), ('ubuntu2404', 'Ubuntu 24.04', 'U24', 'DEB'),
 ('ubuntu2604', 'Ubuntu 26.04', 'U26', 'DEB'), ('debian11', 'Debian 11', 'D11', 'DEB'), ('debian12', 'Debian 12', 'D12', 'DEB'), ('debian13', 'Debian 13', 'D13', 'DEB'),
]
NAME = {k: n for k, n, s, f in OS}; SHORT = {k: s for k, n, s, f in OS}
FAM = [('EL', 'EL：CentOS / Stream / Rocky / OL'), ('AN', '龙蜥系：Anolis / Alinux / BCLinux 8 / OC'),
       ('OE', 'openEuler 系'), ('KY', '麒麟'), ('SUSE', 'openSUSE'), ('DEB', 'Debian / Ubuntu')]
ARM = ['rocky8', 'rocky9', 'rocky10', 'anolis8', 'anolis23', 'alinux3', 'alinux4', 'oc9', 'oe2203', 'oe2403',
       'kylinv10', 'kylinv11', 'kylinv11sp1', 'ubuntu2404', 'debian12']

def up(v):  # 上游版本号
    return re.split(r'[-~+]', re.sub(r'^\d+:', '', v))[0] if v else None
def vt(s): return tuple(int(x) for x in re.findall(r'\d+', s or '')[:3])
def kern(k, which='kernel'):
    v = (scan.get(k, {}).get('pkgs', {}) or {}).get(which)
    if not v: return None
    s = re.sub(r'^\d+:', '', v[1])
    if v[0].startswith('linux-image'):
        m = re.match(r'^(\d+\.\d+\.\d+)[.-](\d+)', s); return f'{m.group(1)}-{m.group(2)}' if m else s
    if k.startswith(('leap', 'debian')): return up(s)
    m = re.match(r'^([\d.]+)-(\d+)', s); return f'{m.group(1)}-{m.group(2)}' if m else s
def pk(k, n):
    v = (scan.get(k, {}).get('pkgs', {}) or {}).get(n); return v
def cg(k):
    h = pr.get(k, {}).get('systemd_default_hierarchy') or []
    h = [x for x in h if not x.startswith('(取自')]
    if not h: return '—'
    x = h[0]
    return {'legacy': 'v1', 'unified': 'v2', 'hybrid': 'hybrid'}.get(x, '仅 v1' if '<233' in x else '仅 v2' if '>=256' in x else x)
def osr(k):
    o = scan.get(k, {}).get('osrelease') or ''
    f = dict(re.findall(r'^([A-Z_]+)="?([^"\n]*)"?$', o, re.M)); src = o.split('\n')[0].strip('[]') if o.startswith('[') else 'base-files'
    return f, src

def t_osrelease():
    out = ['| OS | 来源包 | ID | ID_LIKE | VERSION_ID | PLATFORM_ID | VERSION_CODENAME | PRETTY_NAME |', '| --- | --- | --- | --- | --- | --- | --- | --- |']
    for k, n, s, f in OS:
        r, src = osr(k)
        g = lambda x: f'`{r[x]}`' if r.get(x) else '—'
        out.append(f'| {n} | `{src}` | {g("ID")} | {g("ID_LIKE")} | {g("VERSION_ID")} | {g("PLATFORM_ID")} | {g("VERSION_CODENAME")} | {r.get("PRETTY_NAME", "—")} |')
    diff = []
    for k in ARM:
        a, _ = osr(k); b, _ = osr(k + '-arm64')
        keys = ('ID', 'ID_LIKE', 'VERSION_ID', 'PLATFORM_ID', 'PRETTY_NAME')
        if b and any(a.get(x) != b.get(x) for x in keys): diff.append(f'{NAME[k]}：' + '，'.join(f'{x} `{b.get(x)}`' for x in keys if a.get(x) != b.get(x)))
    return '\n'.join(out), diff

def t_core():
    out = ['| OS | 内核（默认源） | 可选 / HWE 内核 | systemd | 编译默认 cgroup | glibc | nftables | iptables 包 | libseccomp | OpenSSL | kube-proxy nftables |',
           '| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |']
    extra = {'anolis8': 'RHCK 4.18（BaseOS）', 'ubuntu2004': None, 'ol9': None}
    for k, n, s, f in OS:
        kv = kern(k); hv = kern(k, 'kernel-hwe')
        if hv == kv: hv = None
        opt = hv or ''
        if k == 'anolis8': opt = 'RHCK 4.18.0（BaseOS）；默认源最高为 ANCK'
        if k == 'ol9':
            u = pm.get('ol9-uekr8', {}).get('kernels', {}).get('kernel-uek') or pm.get('ol9-uekr8', {}).get('kernels', {}).get('kernel-uek-core')
            u7 = pm.get('ol9-uekr7', {}).get('kernels', {}).get('kernel-uek') or pm.get('ol9-uekr7', {}).get('kernels', {}).get('kernel-uek-core')
            opt = '；'.join(x for x in [f'UEK 8 {up(u[0])}' if u else '', f'UEK 7 {up(u7[0])}' if u7 else ''] if x) + '（新装默认 UEK 8）'
        nv = up((pk(k, 'nftables') or [None, None])[1])
        best = max([x for x in (kv, hv) if x], key=vt, default=None)
        okk = best and vt(best) >= (5, 13); okn = nv and vt(nv) >= (1, 0, 1)
        if k == 'ol9': okk = True
        flag = '✅' if okk and okn else ('❌ nft 过旧' if okk else ('⚠️ 仅 HWE 内核满足' if hv and vt(hv) >= (5, 13) and okn else '❌ 内核不足' if okn else '❌ 内核与 nft 均不足'))
        if k == 'ubuntu2004': flag = '❌ nft 0.9.3 过旧（HWE 也不满足）'
        ipt = pk(k, 'iptables'); ipt = f'{ipt[0]} {up(ipt[1])}' if ipt else '—'
        g = lambda n_: up((pk(k, n_) or [None, None])[1]) or '—'
        sd = (pk(k, 'systemd') or [None, ''])[1]; sd = re.match(r'(\d+)', sd).group(1) if sd else '—'
        osl = (pk(k, 'openssl') or [None, None])[1]; osl = re.split(r'-', re.sub(r'^\d+:', '', osl))[0] if osl else '—'
        out.append(f'| {n} | {kv or "—"} | {opt or "—"} | {sd} | {cg(k)} | {g("glibc")} | {nv or "—"} | {ipt} | {g("libseccomp")} | {osl} | {flag} |')
    return '\n'.join(out)

def t_arm():
    out = ['| OS（arm64） | 默认内核 | 页大小 | VA 位数 | 页表级数 | 可选 64K 内核（包 / 版本 → 页大小 / VA / 级数） | 编译默认 cgroup |', '| --- | --- | --- | --- | --- | --- | --- |']
    for k in ARM + ['leap160']:
        a = k + '-arm64'; c = pr.get(a, {}).get('kconfig') or {}
        if k == 'leap160':
            c = (p64.get(a, {}) or {}).get('default') or {}
        kv = kern(a) or (c.get('ver') and up(c['ver'])) or '—'
        o = (p64.get(a, {}) or {}).get('64k')
        opt = f'`{o["pkg"]}` {o["ver"].split(".el")[0].split(".oe")[0].split(".al")[0]} → {o.get("page", "?")} / {o.get("va_bits", "?")} / {o.get("pgtable_levels", "?")}' if o and not o.get('error') else ('—' if not o else f'读取失败：{o["error"][:60]}')
        out.append(f'| {NAME.get(k, k)} | {kv} | **{c.get("page", "?")}** | {c.get("va_bits", "?")} | {c.get("pgtable_levels", "?")} | {opt} | {cg(a) if a in pr else cg(k)} |')
    return '\n'.join(out)

LOG_REQ = ['conntrack', 'ipset', 'socat', 'iproute', 'ethtool', 'nftables', 'iptables', 'ebtables', 'chrony', 'nfs-client', 'iscsi', 'multipath',
           'lvm2', 'xfsprogs', 'e2fsprogs', 'util-linux', 'libseccomp', 'container-selinux', 'selinux-utils', 'restorecon', 'apparmor', 'audit',
           'curl', 'tar', 'kmod', 'procps', 'iputils', 'ca-certificates', 'openssl', 'sudo']
LOG_OPT = ['ipvsadm', 'iptables-legacy', 'parted', 'gdisk', 'jq', 'rsync', 'wget', 'unzip', 'zstd', 'lsof', 'psmisc', 'sysstat', 'tcpdump', 'vim',
           'bash-completion', 'net-tools', 'dns-utils', 'netcat', 'numactl', 'tuned', 'irqbalance', 'keepalived', 'haproxy', 'nvme-cli', 'grubby',
           'firewalld', 'ufw', 'fapolicyd', 'NetworkManager', 'netplan', 'python3', 'sudo-rs', 'rust-coreutils', 'zram-generator']
NA = {'rpm': {'apparmor', 'ufw', 'netplan'}, 'deb': {'container-selinux', 'grubby'}}
def kind(k): return 'deb' if k.startswith(('ubuntu', 'debian')) else 'rpm'

def t_logical():
    out = ['| 逻辑名 | ' + ' | '.join(lbl for _, lbl in FAM) + ' |', '| --- |' + ' --- |' * len(FAM)]
    for L in LOG_REQ + LOG_OPT:
        cells = []
        for fam, _ in FAM:
            mem = [k for k, n, s, f in OS if f == fam]
            names = {}
            miss = []
            for k in mem:
                m = (pm.get(k, {}).get('map') or {}).get(L)
                if m: names.setdefault(m['pkg'], []).append(k)
                else: miss.append(k)
            parts = []
            for nm, ks in sorted(names.items(), key=lambda x: -len(x[1])):
                parts.append(f'`{nm}`' + ('' if len(ks) == len(mem) or len(names) == 1 and not miss else '（' + '、'.join(SHORT[x] for x in ks) + '）'))
            if miss: parts.append(('无' if not names else '无：' + '、'.join(SHORT[x] for x in miss)) if len(miss) < len(mem) or not names else '无')
            cells.append('；'.join(parts) if parts else '无')
        req = L in LOG_REQ
        out.append(f'| {"**" + L + "**" if req else L} | ' + ' | '.join(cells) + ' |')
    return '\n'.join(out)

def t_missing():
    out = ['| OS | 必需逻辑包缺失 | 可选工具缺失 | 说明 |', '| --- | --- | --- | --- |']
    rows = []
    for k, n, s, f in OS + [(k + '-arm64', NAME[k] + '（arm64）', '', '') for k in ARM]:
        m = pm.get(k, {}).get('map') or {}
        if not m: continue
        na = NA[kind(k)]
        req = [L for L in LOG_REQ if m.get(L) is None and L not in na]
        opt = [L for L in LOG_OPT if m.get(L) is None and L not in na and L not in ('sudo-rs', 'rust-coreutils', 'zram-generator', 'netplan', 'ufw', 'grubby', 'iptables-legacy', 'fapolicyd', 'firewalld', 'apparmor', 'NetworkManager')]
        if k.startswith('leap'):
            req = [L for L in req if L != 'container-selinux' or k == 'leap160']
        rows.append(f'| {n} | {"、".join(req) or "无"} | {"、".join(opt) or "无"} | |')
    return '\n'.join(out + rows)

def t_specials():
    """仅列出平台关心的“有无”型逻辑包：iptables-legacy、fapolicyd、firewalld、NetworkManager、netplan、grubby、sudo-rs、rust-coreutils、zram-generator"""
    cols = ['iptables-legacy', 'fapolicyd', 'firewalld', 'ufw', 'NetworkManager', 'netplan', 'grubby', 'sudo-rs', 'rust-coreutils', 'zram-generator', 'python3']
    out = ['| OS | ' + ' | '.join(cols) + ' |', '| --- |' + ' --- |' * len(cols)]
    for k, n, s, f in OS:
        m = pm.get(k, {}).get('map') or {}
        cell = lambda L: (f'`{m[L]["pkg"]}` {up(m[L]["ver"])}' if L in ('python3', 'sudo-rs', 'rust-coreutils') else '✅' if m[L]['pkg'] == L or L in ('firewalld','NetworkManager') else f'✅ `{m[L]["pkg"]}`') if m.get(L) else '—'
        out.append(f'| {n} | ' + ' | '.join(cell(L) for L in cols) + ' |')
    return '\n'.join(out)

def t_dist():
    cols = [('dist:containerd', 'containerd'), ('dist:docker', 'docker'), ('dist:runc', 'runc'), ('dist:podman', 'podman'), ('dist:cri-o', 'cri-o'), ('dist:kubelet', 'kubelet')]
    out = ['| OS | ' + ' | '.join(c for _, c in cols) + ' |', '| --- |' + ' --- |' * len(cols)]
    for k, n, s, f in OS:
        m = pm.get(k, {}).get('map') or {}
        def cell(L):
            x = m.get(L)
            if not x: return '—'
            v = up(x['ver'])
            return f'`{x["pkg"]}` {v}' if x['pkg'] not in (L.split(':')[1],) else v
        out.append(f'| {n} | ' + ' | '.join(cell(L) for L, _ in cols) + ' |')
    return '\n'.join(out)

def t_discover():
    out = ['| OS | 发现的安全 / 执行控制相关文件（按文件名匹配） |', '| --- | --- |']
    for k, n, s, f in OS:
        ds = {p: [x for x in v if not x.startswith('/etc/selinux/') and 'cgconfigparser' not in x] for p, v in (pm.get(k, {}).get('discover') or {}).items()}
        ds = {p: v for p, v in ds.items() if v}
        if not ds: out.append(f'| {n} | — |'); continue
        out.append(f'| {n} | ' + '；'.join(f'`{p}`：' + '、'.join(f'`{x}`' for x in sorted(v)[:6]) for p, v in sorted(ds.items())) + ' |')
    return '\n'.join(out)

def t_repos():
    out = ['| 目标 | 架构 | 扫描的仓库 | 包数（去重） |', '| --- | --- | --- | --- |']
    for k, n, s, f in OS + [(k + '-arm64', NAME[k], '', '') for k in ARM + ['leap160']] + [('ol9-uekr7', 'Oracle Linux 9 UEK R7', '', ''), ('ol9-uekr8', 'Oracle Linux 9 UEK R8', '', '')]:
        r = pm.get(k, {})
        if not r: continue
        rs_ = r.get('repos') or []
        short = []
        for u in rs_:
            u2 = re.sub(r'^https?://', '', u)
            short.append(u2)
        host = short[0].split('/')[0] if short else ''
        paths = [x[len(host):] if x.startswith(host) else x for x in short]
        out.append(f'| {n} | {"arm64" if k.endswith("-arm64") else "amd64"} | `{host}`：' + '、'.join(f'`{p}`' for p in paths) + f' | {r.get("count", "—")} |')
    return '\n'.join(out)

if __name__ == '__main__':
    t, diff = t_osrelease()
    print('<!-- T_REPOS -->'); print(t_repos())
    print('<!-- T_OSRELEASE -->'); print(t); print('<!-- T_OSRELEASE_ARMDIFF -->'); print('\n'.join('- ' + x for x in diff) or '- 无差异')
    print('<!-- T_CORE -->'); print(t_core())
    print('<!-- T_ARM -->'); print(t_arm())
    print('<!-- T_LOGICAL -->'); print(t_logical())
    print('<!-- T_MISSING -->'); print(t_missing())
    print('<!-- T_SPECIALS -->'); print(t_specials())
    print('<!-- T_DIST -->'); print(t_dist())
    print('<!-- T_DISCOVER -->'); print(t_discover())
