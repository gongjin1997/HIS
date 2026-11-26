#!/usr/bin/env python3
"""kinstall 调研（S2）：逻辑包 → 实际包 的“按文件 / Provides 反查”。
与 repo_scan.py 的区别：不只按包名候选匹配，而是读取 primary.xml 中的 <file> 与 <rpm:provides>，
按“必须提供的命令路径”反查提供它的包（等价于 `dnf provides /usr/sbin/multipath`）。
deb 系只有 Packages 索引（无文件清单），按包名 + Provides 字段匹配。
输出 pkgmap.json：{target: {logical: {"pkg": 实际包名, "ver": 版本, "via": 命中方式, "alts": [其他提供者]}}}
另输出 extras：内核相关包（kernel-64k-devel 等）的下载地址、安全模块相关文件（kysec 等）。"""
import io, json, os, re, sys, concurrent.futures as cf
import xml.etree.ElementTree as ET
HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)
import repo_scan as rs

NS_REPO = {'repo': 'http://linux.duke.edu/metadata/repo'}
C = '{http://linux.duke.edu/metadata/common}'
RPMNS = '{http://linux.duke.edu/metadata/rpm}'

# 逻辑包定义：(逻辑名, 必须提供的文件之一, rpm 候选名/provides, deb 候选名/provides)
B = lambda *n: [p for x in n for p in ('/usr/sbin/' + x, '/sbin/' + x, '/usr/bin/' + x, '/bin/' + x)]
LOGICAL = [
 ('conntrack', B('conntrack'), ['conntrack-tools', 'conntrack'], ['conntrack']),
 ('ipset', B('ipset'), ['ipset'], ['ipset']),
 ('ipvsadm', B('ipvsadm'), ['ipvsadm'], ['ipvsadm']),
 ('socat', B('socat'), ['socat'], ['socat']),
 ('iproute', B('ip'), ['iproute', 'iproute2'], ['iproute2']),
 ('ethtool', B('ethtool'), ['ethtool'], ['ethtool']),
 ('nftables', B('nft'), ['nftables'], ['nftables']),
 ('iptables', B('iptables'), ['iptables', 'iptables-nft'], ['iptables']),
 ('iptables-legacy', B('iptables-legacy'), ['iptables-legacy'], ['iptables']),
 ('ebtables', B('ebtables'), ['ebtables', 'iptables-ebtables', 'ebtables-legacy'], ['ebtables']),
 ('chrony', B('chronyd'), ['chrony'], ['chrony']),
 ('nfs-client', B('mount.nfs'), ['nfs-utils', 'nfs-client'], ['nfs-common']),
 ('iscsi', B('iscsiadm'), ['iscsi-initiator-utils', 'open-iscsi'], ['open-iscsi']),
 ('multipath', B('multipath', 'multipathd'), ['device-mapper-multipath', 'multipath-tools'], ['multipath-tools']),
 ('lvm2', B('lvm'), ['lvm2'], ['lvm2']),
 ('xfsprogs', B('mkfs.xfs'), ['xfsprogs'], ['xfsprogs']),
 ('e2fsprogs', B('mkfs.ext4'), ['e2fsprogs'], ['e2fsprogs']),
 ('parted', B('parted'), ['parted'], ['parted']),
 ('gdisk', B('sgdisk'), ['gdisk', 'gptfdisk'], ['gdisk']),
 ('util-linux', B('lsblk', 'wipefs', 'findmnt'), ['util-linux', 'util-linux-core'], ['util-linux']),
 ('libseccomp', [], ['libseccomp', 'libseccomp2'], ['libseccomp2']),
 ('container-selinux', [], ['container-selinux'], []),
 ('selinux-utils', B('semanage'), ['policycoreutils-python-utils', 'policycoreutils-python'], ['policycoreutils-python-utils', 'policycoreutils']),
 ('restorecon', B('restorecon'), ['policycoreutils'], ['policycoreutils']),
 ('apparmor', B('apparmor_parser'), ['apparmor-parser'], ['apparmor']),
 ('fapolicyd', B('fapolicyd'), ['fapolicyd'], ['fapolicyd']),
 ('firewalld', B('firewalld', 'firewall-cmd'), ['firewalld'], ['firewalld']),
 ('ufw', B('ufw'), [], ['ufw']),
 ('audit', B('auditd'), ['audit'], ['auditd']),
 ('jq', B('jq'), ['jq'], ['jq']),
 ('rsync', B('rsync'), ['rsync'], ['rsync']),
 ('curl', B('curl'), ['curl'], ['curl']),
 ('wget', B('wget'), ['wget', 'wget2-wget', 'wget2'], ['wget']),
 ('tar', B('tar'), ['tar'], ['tar']),
 ('unzip', B('unzip'), ['unzip'], ['unzip']),
 ('zstd', B('zstd'), ['zstd'], ['zstd']),
 ('lsof', B('lsof'), ['lsof'], ['lsof']),
 ('psmisc', B('fuser', 'killall'), ['psmisc'], ['psmisc']),
 ('sysstat', B('iostat'), ['sysstat'], ['sysstat']),
 ('tcpdump', B('tcpdump'), ['tcpdump'], ['tcpdump']),
 ('vim', B('vim'), ['vim-enhanced', 'vim'], ['vim']),
 ('bash-completion', [], ['bash-completion'], ['bash-completion']),
 ('net-tools', B('netstat', 'ifconfig'), ['net-tools'], ['net-tools']),
 ('dns-utils', B('dig'), ['bind-utils', 'bind-utils9'], ['bind9-dnsutils', 'dnsutils']),
 ('netcat', B('nc', 'ncat', 'netcat'), ['nmap-ncat', 'netcat', 'nc', 'netcat-openbsd', 'ncat', 'nmap'], ['netcat-openbsd', 'netcat-traditional']),
 ('numactl', B('numactl'), ['numactl'], ['numactl']),
 ('tuned', B('tuned', 'tuned-adm'), ['tuned'], ['tuned']),
 ('irqbalance', B('irqbalance'), ['irqbalance'], ['irqbalance']),
 ('keepalived', B('keepalived'), ['keepalived'], ['keepalived']),
 ('haproxy', B('haproxy'), ['haproxy'], ['haproxy']),
 ('nvme-cli', B('nvme'), ['nvme-cli'], ['nvme-cli']),
 ('grubby', B('grubby'), ['grubby'], []),
 ('sudo', B('sudo'), ['sudo'], ['sudo']),
 ('sudo-rs', [], ['sudo-rs'], ['sudo-rs']),
 ('rust-coreutils', [], ['uutils-coreutils', 'rust-coreutils'], ['rust-coreutils']),
 ('openssl', B('openssl'), ['openssl'], ['openssl']),
 ('python3', B('python3') + ['/usr/libexec/platform-python'], ['python3', 'python3.12', 'python3.11', 'python39', 'python38', 'python36', 'platform-python'], ['python3']),
 ('NetworkManager', B('NetworkManager', 'nmcli'), ['NetworkManager'], ['network-manager']),
 ('netplan', B('netplan'), [], ['netplan.io']),
 ('ca-certificates', [], ['ca-certificates'], ['ca-certificates']),
 ('kmod', B('modprobe'), ['kmod'], ['kmod']),
 ('procps', B('sysctl'), ['procps-ng', 'procps'], ['procps']),
 ('iputils', B('ping'), ['iputils'], ['iputils-ping']),
 ('zram-generator', [], ['zram-generator', 'zram-generator-defaults'], ['systemd-zram-generator', 'zram-tools']),
 # 发行版自带容器 / K8s 包（仅记录冲突，kinstall 不使用）
 ('dist:containerd', B('containerd'), ['containerd', 'containerd.io'], ['containerd']),
 ('dist:docker', B('dockerd'), ['docker', 'docker-engine', 'moby-engine', 'docker-ce'], ['docker.io']),
 ('dist:runc', B('runc'), ['runc'], ['runc']),
 ('dist:podman', B('podman'), ['podman'], ['podman']),
 ('dist:kubelet', B('kubelet'), ['kubernetes-kubelet', 'kubernetes-node', 'kubernetes'], ['kubernetes']),
 ('dist:cri-o', B('crio'), ['cri-o'], ['cri-o']),
]
# 安全模块 / 特殊工具：按文件名正则发现（KYSEC 等）
DISCOVER = re.compile(r'/(kysec[^/]*|getstatus|setstatus|security-switch|nkvers|kylin_secadm|secadm[^/]*|fapolicyd-cli|usec[^/]*|uos-sec[^/]*|elf-verify[^/]*|deepin-elf-verify|cgroup-tools?|cgconfigparser)$')
KERNEL_RE = re.compile(r'^(kernel(-64k|-rt|-uek|-lt|-ml|-default|-64kb)?(-core|-devel)?|kernel-default-devel|kernel-64kb-devel|kernel-uek(-core|-devel)?|linux-image-.*|linux-headers-.*-(generic|generic-64k|arm64|amd64|cloud-arm64|rt-arm64))$')

def parse_primary(base, arch):
    repomd = ET.fromstring(rs.fetch(base + 'repodata/repomd.xml'))
    href = [d.find('repo:location', NS_REPO).get('href') for d in repomd.findall('repo:data', NS_REPO) if d.get('type') == 'primary'][0]
    data = rs.decompress(rs.fetch(base + href), href)
    pk = []
    for ev, el in ET.iterparse(io.BytesIO(data)):
        if el.tag != C + 'package': continue
        a = el.find(C + 'arch').text
        if a not in (arch, 'noarch'): el.clear(); continue
        name = el.find(C + 'name').text
        v = el.find(C + 'version'); ver = v.get('ver') + '-' + v.get('rel')
        loc = base + el.find(C + 'location').get('href')
        fmt = el.find(C + 'format'); prov, files = [], []
        if fmt is not None:
            p = fmt.find(RPMNS + 'provides')
            if p is not None: prov = [e.get('name') for e in p.findall(RPMNS + 'entry')]
            files = [f.text for f in fmt.findall(C + 'file')]
        pk.append((name, ver, loc, prov, files, base))
        el.clear()
    return pk

def parse_deb(mirror, dist, comps, arch):
    pk = []
    for comp in comps:
        for ext in ('.xz', '.gz'):
            url = f'{mirror}dists/{dist}/{comp}/binary-{arch}/Packages{ext}'
            try: txt = rs.decompress(rs.fetch(url), url).decode(errors='replace')
            except Exception: continue
            for block in txt.split('\n\n'):
                m = re.search(r'^Package: (.+)$', block, re.M)
                if not m: continue
                v = re.search(r'^Version: (.+)$', block, re.M).group(1)
                fn = re.search(r'^Filename: (.+)$', block, re.M)
                pv = re.search(r'^Provides: (.+)$', block, re.M)
                prov = [re.split(r'\s|\(', x.strip())[0] for x in pv.group(1).split(',')] if pv else []
                pk.append((m.group(1).strip(), v, mirror + (fn.group(1) if fn else ''), prov, [], f'{dist}/{comp}'))
            break
    return pk

def best(entries):
    out = {}
    for e in entries:
        n = e[0]
        if n not in out or rs.vercmp(e[1], out[n][1]) > 0: out[n] = e
    return out

TARGETS = dict(rs.TARGETS)
TARGETS.update({
 'ol9-uekr7': ('rpm', ['https://yum.oracle.com/repo/OracleLinux/OL9/UEKR7/x86_64/']),
 'ol9-uekr8': ('rpm', ['https://yum.oracle.com/repo/OracleLinux/OL9/UEKR8/x86_64/']),
 'leap160-arm64': ('rpm', ['https://download.opensuse.org/distribution/leap/16.0/repo/oss/']),
})
ARCH = lambda k: 'aarch64' if k.endswith('-arm64') else 'x86_64'

def run(key):
    kind, spec = TARGETS[key]
    res = {'key': key, 'errors': [], 'map': {}, 'kernels': {}, 'discover': {}, 'repos': []}
    entries = []
    if kind == 'rpm':
        for base in spec:
            try: entries += parse_primary(base, ARCH(key)); res['repos'].append(base)
            except Exception as e: res['errors'].append(f'{base}: {type(e).__name__} {e}'[:200])
    else:
        mirror, dists, comps, arch = spec
        for d in dists:
            try: entries += parse_deb(mirror, d, comps, arch); res['repos'].append(f'{mirror}{d} {comps} {arch}')
            except Exception as e: res['errors'].append(f'{d}: {type(e).__name__} {e}'[:200])
    by = best(entries)
    fidx, pidx = {}, {}
    for n, e in by.items():
        for f in e[4]: fidx.setdefault(f, set()).add(n)
        for p in e[3]: pidx.setdefault(p, set()).add(n)
        for f in e[4]:
            if DISCOVER.search(f): res['discover'].setdefault(n, []).append(f)
        if KERNEL_RE.match(n): res['kernels'][n] = [e[1], e[2]]
    res['count'] = len(by)
    for lname, files, rpmc, debc in LOGICAL:
        cands = rpmc if kind == 'rpm' else debc
        hit = None
        for c in cands:
            if c in by: hit = (c, 'name'); break
        provs = set()
        for f in files: provs |= fidx.get(f, set())
        for c in cands: provs |= pidx.get(c, set())
        if not hit and provs:
            # 优先：名字含逻辑名关键词的提供者，否则取字典序第一个
            pick = sorted(provs, key=lambda n: (0 if lname.split(':')[-1].split('-')[0] in n else 1, len(n), n))[0]
            via = 'file' if any(pick in fidx.get(f, set()) for f in files) else 'provides'
            hit = (pick, via)
        if hit:
            res['map'][lname] = {'pkg': hit[0], 'ver': by[hit[0]][1], 'via': hit[1], 'repo': by[hit[0]][5],
                                 'alts': sorted(provs - {hit[0]})[:6]}
        else:
            res['map'][lname] = None
    return res

if __name__ == '__main__':
    keys = sys.argv[1:] or list(TARGETS)
    OUT = os.path.join(HERE, 'pkgmap.json')
    out = json.load(open(OUT)) if os.path.exists(OUT) else {}
    with cf.ProcessPoolExecutor(int(os.environ.get('KI_WORKERS', '4'))) as ex:
        futs = {ex.submit(run, k): k for k in keys}
        for f in cf.as_completed(futs):
            k = futs[f]
            try: r = f.result()
            except Exception as e: r = {'key': k, 'errors': [f'{type(e).__name__} {e}'[:300]], 'map': {}}
            out[k] = r
            tmp = OUT + '.tmp'; json.dump(out, open(tmp, 'w'), ensure_ascii=False, indent=1); os.replace(tmp, OUT)
            miss = [l for l, v in r.get('map', {}).items() if v is None and not l.startswith('dist:')]
            print(k, r.get('count'), 'errs', len(r.get('errors', [])), 'miss', miss, flush=True)
