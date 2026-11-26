import json, re
d = json.load(open('/data/work/research/repo_scan.json'))
# 已人工补查（pkgfind.py）的值，覆盖首轮扫描中候选名不全的项
manual = {
 'anolis7': {'kernel': ('kernel', '3.10.0-1160.119.1.0.1.an7'), 'systemd': ('systemd', '219-78.5.an7.3'), 'nftables': ('nftables', '0.8-14.an7')},
 'leap156': {'kernel': ('kernel-default', '6.4.0-150600.21.3'), 'libseccomp': ('libseccomp2', '2.5.3-150400.2.4')},
 'ubuntu2204': {'kernel-hwe': ('linux-image-generic-hwe-22.04', '6.8.0-149.149~22.04.1')},
}
names = {'centos7':'CentOS 7.9','centos8':'CentOS 8.5','stream9':'CentOS Stream 9','stream10':'CentOS Stream 10','rocky8':'Rocky 8','rocky9':'Rocky 9','rocky10':'Rocky 10','anolis7':'Anolis 7.9','anolis8':'Anolis 8.10','anolis23':'Anolis 23.5','oe2203':'openEuler 22.03 SP4','oe2403':'openEuler 24.03 SP4','kylinv10':'麒麟 V10 SP3 2403','kylinv11':'麒麟 V11 2503','kylinv11sp1':'麒麟 V11 SP1 2603','oc9':'OpenCloudOS 9.6','ol9':'Oracle Linux 9','alinux3':'Alibaba Cloud Linux 3','alinux4':'Alibaba Cloud Linux 4','bclinux8':'BCLinux 8.8','bclinux-oe':'BCLinux for Euler 22.10U2','leap156':'openSUSE Leap 15.6','ubuntu2004':'Ubuntu 20.04','ubuntu2204':'Ubuntu 22.04','ubuntu2404':'Ubuntu 24.04','ubuntu2604':'Ubuntu 26.04','debian11':'Debian 11','debian12':'Debian 12','debian13':'Debian 13','rocky9-arm64':'Rocky 9 (arm64)','oe2403-arm64':'openEuler 24.03 SP4 (arm64)','kylinv10-arm64':'麒麟 V10 SP3 (arm64)','kylinv11-arm64':'麒麟 V11 (arm64)','anolis8-arm64':'Anolis 8.10 BaseOS (arm64)','ubuntu2404-arm64':'Ubuntu 24.04 (arm64)','debian12-arm64':'Debian 12 (arm64)'}
def get(k, c):
    v = manual.get(k, {}).get(c) or d.get(k, {}).get('pkgs', {}).get(c)
    return tuple(v) if v else None
def ver(v):
    if not v: return '—'
    s = v[1]
    s = re.sub(r'^\d+:', '', s)
    s = re.sub(r'\.(el\d|an\d|al\d|alnx|oe\d|ky\d|oc\d|module|bclinux|ocs)[^ ]*$', '', s)
    s = re.sub(r'-[^-]*$', '', s) if not re.match(r'^\d', s) else s.split('-')[0] if '-' in s and not s.startswith('5.4.0') else s
    return s
def upstream(v):
    if not v: return None
    s = re.sub(r'^\d+:', '', v[1]); return re.split(r'[-~+]', s)[0]
def vt(s):
    return tuple(int(x) for x in re.findall(r'\d+', s)[:3]) if s else ()
rows = []
rows.append('| OS | 内核（默认源） | HWE / 可选内核 | systemd | glibc | nftables | iptables | libseccomp | OpenSSL | kube-proxy nftables（内核≥5.13 且 nft≥1.0.1） |')
rows.append('| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |')
for k in names:
    if k not in d and k not in manual: continue
    kern, hwe = get(k,'kernel'), get(k,'kernel-hwe')
    kv = upstream(kern); hv = upstream(hwe); nv = upstream(get(k,'nftables'))
    if k.startswith('ubuntu'):
        kv = re.sub(r'^(\d+\.\d+\.\d+)\.(\d+).*$', r'\1-\2', kern[1]) if kern else None
        hv = re.sub(r'^(\d+\.\d+\.\d+)[.-](\d+).*$', r'\1-\2', hwe[1]) if hwe else None
    best = max([x for x in (kv, hv) if x], key=vt, default=None)
    ok_k = best and vt(best) >= (5,13); ok_n = nv and vt(nv) >= (1,0,1)
    if ok_k and ok_n: flag = '✅'
    elif ok_n and not ok_k: flag = '❌ 内核不足' + ('（HWE 可满足）' if hv and vt(hv) >= (5,13) else '')
    elif ok_k and not ok_n: flag = '❌ nft 过旧'
    else: flag = '❌ 内核与 nft 均不足' if kv else '待补'
    if kv is None: flag = '待补（内核在其他源）'
    rows.append(f"| {names[k]} | {kv or '—'} | {hv or '—'} | {upstream(get(k,'systemd')) or '—'} | {upstream(get(k,'glibc')) or '—'} | {nv or '—'} | {upstream(get(k,'iptables')) or '—'} | {upstream(get(k,'libseccomp')) or '—'} | {upstream(get(k,'openssl')) or '—'} | {flag} |")
open('/data/work/notes/t_core.md','w').write('\n'.join(rows)+'\n')
# os-release
rows = ['| OS | 来源包 | ID | ID_LIKE | VERSION_ID | PRETTY_NAME |', '| --- | --- | --- | --- | --- | --- |']
for k in names:
    r = d.get(k)
    if not r or not r.get('osrelease') or r['osrelease'].startswith('ERR'): 
        rows.append(f"| {names[k]} | — | 待补 | | | |"); continue
    txt = r['osrelease']; src = txt.splitlines()[0].strip('[]') if txt.startswith('[') else 'base-files'
    kv = dict(re.findall(r'^([A-Z_]+)=(.*)$', txt, re.M))
    g = lambda x: kv.get(x, '').strip('"').replace('|', '/') or '—'
    rows.append(f"| {names[k]} | `{src[:40]}` | `{g('ID')}` | `{g('ID_LIKE')}` | `{g('VERSION_ID')}` | {g('PRETTY_NAME')} |")
open('/data/work/notes/t_osrel.md','w').write('\n'.join(rows)+'\n')
# 缺失的逻辑包（排除与发行版族无关的项）
skip_rpm = {'kernel-hwe','apparmor','netplan','timesyncd','sudo-rs','rust-coreutils','ntp','kernel'}
skip_deb = {'grubby','container-selinux','tuned','ntp','sudo-rs','rust-coreutils','kernel-hwe','kernel'}
dist_only = {'containerd(发行版)','docker(发行版)','runc(发行版)','kubernetes(发行版)'}
rows = ['| OS | 官方源中缺失的逻辑包（需 EPEL / 额外源 / 离线包补齐，或用替代实现） | 发行版自带容器/K8s 包（仅记录，kinstall 不使用） |', '| --- | --- | --- |']
for k in names:
    r = d.get(k)
    if not r or not r['pkgs']: continue
    if r.get('count', 0) == 0:
        rows.append(f"| {names[k]} | 待补（首轮路径错误，已修正重跑） | — |"); continue
    kind = r['kind']; sk = skip_rpm if kind == 'rpm' else skip_deb
    miss = [c for c, v in r['pkgs'].items() if v is None and c not in sk and c not in dist_only and not get(k, c)]
    dist = [f"{v[0]} {upstream(v)}" for c, v in r['pkgs'].items() if c in dist_only and v]
    extra = []
    if kind == 'deb':
        for c in ('sudo-rs','rust-coreutils'):
            v = r['pkgs'].get(c)
            if v: extra.append(f"{c} {upstream(v)}")
    rows.append(f"| {names[k]} | {', '.join(miss) or '无'}{('；另有 ' + ', '.join(extra)) if extra else ''} | {', '.join(dist) or '—'} |")
open('/data/work/notes/t_missing.md','w').write('\n'.join(rows)+'\n')
print('ok')
