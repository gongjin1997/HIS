#!/usr/bin/env python3
"""kinstall 调研：从各 OS 官方软件源元数据实测 包名/版本/os-release。结果写 repo_scan.json"""
import urllib.request, urllib.error, gzip, lzma, bz2, subprocess, io, re, json, sys, os, hashlib, tarfile, traceback
import xml.etree.ElementTree as ET
import concurrent.futures as cf
HERE = os.path.dirname(os.path.abspath(__file__))
CACHE = os.environ.get('KI_CACHE', os.path.join(HERE, 'cache'))
OUT = os.environ.get('KI_OUT', os.path.join(HERE, 'repo_scan.json'))
os.makedirs(CACHE, exist_ok=True)
UA = {'User-Agent': 'Mozilla/5.0 kinstall-research'}
def fetch(url, timeout=60, deadline=1200, retries=3):
    """带缓存的下载：单次读超时 60s、单文件总时限 deadline 秒、失败重试；先写临时文件再改名，避免半截缓存"""
    import time
    fn = os.path.join(CACHE, hashlib.sha1(url.encode()).hexdigest())
    if os.path.exists(fn):
        return open(fn, 'rb').read()
    last = None
    for attempt in range(retries):
        try:
            req = urllib.request.Request(url, headers=UA)
            t0 = time.time(); buf = io.BytesIO()
            with urllib.request.urlopen(req, timeout=timeout) as r:
                while True:
                    chunk = r.read(1 << 20)
                    if not chunk: break
                    buf.write(chunk)
                    if time.time() - t0 > deadline:
                        raise TimeoutError(f'deadline {deadline}s exceeded')
            data = buf.getvalue()
            tmp = fn + '.part'
            open(tmp, 'wb').write(data); os.replace(tmp, fn)
            return data
        except urllib.error.HTTPError as e:
            if e.code == 404: raise
            last = e
        except Exception as e:
            last = e
        time.sleep(3 * (attempt + 1))
    raise last
def decompress(data, name):
    if name.endswith('.gz'): return gzip.decompress(data)
    if name.endswith('.xz'): return lzma.decompress(data)
    if name.endswith('.bz2'): return bz2.decompress(data)
    if name.endswith('.zst'): return subprocess.run(['zstd', '-dc'], input=data, capture_output=True, check=True).stdout
    return data
# ---- 版本比较（rpmvercmp 简化版，deb 也近似可用）
def vercmp(a, b):
    sa = re.findall(r'\d+|[a-zA-Z]+|~', a); sb = re.findall(r'\d+|[a-zA-Z]+|~', b)
    for x, y in zip(sa, sb):
        if x == y: continue
        if x == '~': return -1
        if y == '~': return 1
        if x.isdigit() and y.isdigit(): return (int(x) > int(y)) - (int(x) < int(y))
        if x.isdigit(): return 1
        if y.isdigit(): return -1
        return (x > y) - (x < y)
    return (len(sa) > len(sb)) - (len(sa) < len(sb))
NS = {'repo': 'http://linux.duke.edu/metadata/repo', 'c': 'http://linux.duke.edu/metadata/common'}
C = '{http://linux.duke.edu/metadata/common}'
def scan_rpm(base):
    repomd = ET.fromstring(fetch(base + 'repodata/repomd.xml'))
    href = [d.find('repo:location', NS).get('href') for d in repomd.findall('repo:data', NS) if d.get('type') == 'primary'][0]
    data = decompress(fetch(base + href), href)
    pkgs, osrel = {}, []
    for ev, el in ET.iterparse(io.BytesIO(data)):
        if el.tag != C + 'package': continue
        arch = el.find(C + 'arch').text
        if arch == 'src': el.clear(); continue
        name = el.find(C + 'name').text
        v = el.find(C + 'version'); ver = v.get('ver') + '-' + v.get('rel')
        loc = base + el.find(C + 'location').get('href')
        if name not in pkgs or vercmp(ver, pkgs[name][0]) > 0:
            pkgs[name] = (ver, loc)
        fmt = el.find(C + 'format')
        if fmt is not None:
            for f in fmt.findall(C + 'file'):
                if f.text in ('/etc/os-release', '/usr/lib/os-release'):
                    osrel.append((name, ver, loc))
        el.clear()
    return pkgs, osrel
def cpio_files(data):
    i, out = 0, {}
    while i + 110 <= len(data):
        h = data[i:i + 110]
        if h[:6] not in (b'070701', b'070702'): break
        f = [int(h[6 + 8 * k:14 + 8 * k], 16) for k in range(13)]
        mode, size, nsz = f[1], f[6], f[11]
        name = data[i + 110:i + 110 + nsz - 1].decode(errors='replace')
        j = (i + 110 + nsz + 3) & ~3
        out[name.lstrip('.')] = (mode, data[j:j + size])
        i = (j + size + 3) & ~3
        if name == 'TRAILER!!!': break
    return out
def rpm_osrelease(url):
    raw = fetch(url)
    cp = subprocess.run(['rpm2cpio', '-'], input=raw, capture_output=True).stdout
    files = cpio_files(cp)
    for p in ('/usr/lib/os-release', '/etc/os-release'):
        if p in files and (files[p][0] & 0o170000) == 0o100000:
            return files[p][1].decode(errors='replace')
    return 'NOT FOUND in ' + ','.join(k for k in files if 'release' in k)[:300]
def parse_deb_packages(text):
    pkgs = {}
    for block in text.split('\n\n'):
        m = re.search(r'^Package: (.+)$', block, re.M)
        if not m: continue
        v = re.search(r'^Version: (.+)$', block, re.M).group(1)
        fn = re.search(r'^Filename: (.+)$', block, re.M)
        name = m.group(1).strip()
        if name not in pkgs or vercmp(v, pkgs[name][0]) > 0:
            pkgs[name] = (v, fn.group(1) if fn else '')
    return pkgs
def scan_deb(mirror, dist, comps, arch):
    pkgs = {}
    for comp in comps:
        for ext in ('.xz', '.gz'):
            url = f'{mirror}dists/{dist}/{comp}/binary-{arch}/Packages{ext}'
            try:
                txt = decompress(fetch(url), url).decode(errors='replace')
            except Exception:
                continue
            for k, (v, fn) in parse_deb_packages(txt).items():
                if k not in pkgs or vercmp(v, pkgs[k][0]) > 0:
                    pkgs[k] = (v, mirror + fn)
            break
    return pkgs
def deb_osrelease(url):
    raw = fetch(url)
    assert raw[:8] == b'!<arch>\n'
    i = 8
    while i < len(raw):
        hdr = raw[i:i + 60]; name = hdr[:16].decode().strip().rstrip('/'); size = int(hdr[48:58].decode().strip())
        body = raw[i + 60:i + 60 + size]
        if name.startswith('data.tar'):
            data = decompress(body, name)
            with tarfile.open(fileobj=io.BytesIO(data)) as tf:
                for p in ('./usr/lib/os-release', './etc/os-release'):
                    try:
                        m = tf.getmember(p)
                        if m.isfile(): return tf.extractfile(m).read().decode(errors='replace')
                    except KeyError: pass
        i += 60 + size + (size & 1)
    return 'NOT FOUND'
LOGICAL = [
 ('kernel', ['kernel', 'kernel-default'], ['linux-image-generic', 'linux-image-amd64', 'linux-image-arm64']),
 ('kernel-hwe', [], ['linux-image-generic-hwe-26.04', 'linux-image-generic-hwe-24.04', 'linux-image-generic-hwe-22.04', 'linux-image-generic-hwe-20.04']),
 ('systemd', ['systemd'], ['systemd']), ('glibc', ['glibc'], ['libc6']),
 ('nftables', ['nftables'], ['nftables']), ('iptables', ['iptables', 'iptables-nft'], ['iptables']),
 ('conntrack', ['conntrack-tools'], ['conntrack']), ('ipset', ['ipset'], ['ipset']), ('ipvsadm', ['ipvsadm'], ['ipvsadm']),
 ('socat', ['socat'], ['socat']), ('iproute', ['iproute', 'iproute2'], ['iproute2']), ('ethtool', ['ethtool'], ['ethtool']),
 ('chrony', ['chrony'], ['chrony']), ('nfs-client', ['nfs-utils', 'nfs-client'], ['nfs-common']),
 ('iscsi', ['iscsi-initiator-utils', 'open-iscsi'], ['open-iscsi']), ('multipath', ['device-mapper-multipath', 'multipath-tools'], ['multipath-tools']),
 ('lvm2', ['lvm2'], ['lvm2']), ('xfsprogs', ['xfsprogs'], ['xfsprogs']), ('libseccomp', ['libseccomp', 'libseccomp2'], ['libseccomp2']),
 ('container-selinux', ['container-selinux'], []), ('selinux-utils', ['policycoreutils-python-utils', 'policycoreutils-python'], ['policycoreutils']),
 ('apparmor', ['apparmor-parser'], ['apparmor']), ('firewall', ['firewalld'], ['ufw']), ('audit', ['audit'], ['auditd']),
 ('jq', ['jq'], ['jq']), ('rsync', ['rsync'], ['rsync']), ('curl', ['curl'], ['curl']), ('wget', ['wget', 'wget2-wget', 'wget2'], ['wget']),
 ('tar', ['tar'], ['tar']), ('lsof', ['lsof'], ['lsof']), ('psmisc', ['psmisc'], ['psmisc']), ('sysstat', ['sysstat'], ['sysstat']),
 ('tcpdump', ['tcpdump'], ['tcpdump']), ('vim', ['vim-enhanced', 'vim'], ['vim']), ('bash-completion', ['bash-completion'], ['bash-completion']),
 ('net-tools', ['net-tools'], ['net-tools']), ('dns-utils', ['bind-utils'], ['bind9-dnsutils', 'dnsutils']),
 ('netcat', ['nmap-ncat', 'netcat', 'nc', 'netcat-openbsd', 'ncat', 'nmap'], ['netcat-openbsd', 'netcat-traditional']), ('numactl', ['numactl'], ['numactl']), ('tuned', ['tuned'], ['tuned']),
 ('irqbalance', ['irqbalance'], ['irqbalance']), ('keepalived', ['keepalived'], ['keepalived']), ('haproxy', ['haproxy'], ['haproxy']),
 ('nvme-cli', ['nvme-cli'], ['nvme-cli']), ('grubby', ['grubby'], []), ('sudo', ['sudo'], ['sudo']), ('sudo-rs', ['sudo-rs'], ['sudo-rs']),
 ('rust-coreutils', ['uutils-coreutils'], ['rust-coreutils']), ('openssl', ['openssl'], ['openssl']), ('python3', ['python3', 'python3.12', 'python3.11', 'python39', 'python38', 'python36', 'platform-python'], ['python3']),
 ('ntp', ['ntp', 'ntpsec'], ['ntp', 'ntpsec']), ('timesyncd', [], ['systemd-timesyncd']),
 ('NetworkManager', ['NetworkManager'], ['network-manager']), ('netplan', [], ['netplan.io']),
 ('containerd(发行版)', ['containerd', 'containerd.io'], ['containerd']), ('docker(发行版)', ['docker', 'docker-engine', 'moby-engine'], ['docker.io']),
 ('runc(发行版)', ['runc'], ['runc']), ('kubernetes(发行版)', ['kubernetes-kubelet', 'kubernetes-node', 'kubernetes'], ['kubernetes']),
 ('kernel-64k', ['kernel-64k', 'kernel-64k-core'], ['linux-image-generic-64k']),
 ('kernel-devel', ['kernel-devel'], []),
 ('libsystemd-shared', [], ['libsystemd-shared']),
 ('zram-generator', ['zram-generator', 'zram-generator-defaults'], ['systemd-zram-generator', 'zram-tools']),
 ('kysec', ['kysec-utils', 'kysec-daemon', 'kysec'], []),
 ('iptables-legacy', ['iptables-legacy'], []),
]
LOCS = ('kernel', 'kernel-64k', 'kernel-devel', 'systemd', 'libsystemd-shared')
R = 'https://vault.centos.org/'; ST = 'https://mirror.stream.centos.org/'; RK = 'https://dl.rockylinux.org/pub/rocky/'
AN = 'https://mirrors.openanolis.cn/anolis/'; AL = 'https://mirrors.aliyun.com/alinux/'; OC = 'https://mirrors.opencloudos.tech/opencloudos/'; OE = 'https://repo.openeuler.org/'; KY = 'https://update.cs2c.com.cn/NS/'
def el(base, arch, repos): return [base.format(r=r, a=arch) for r in repos]
TARGETS = {
 'centos7': ('rpm', [R + '7.9.2009/os/x86_64/', R + '7.9.2009/updates/x86_64/', R + '7.9.2009/extras/x86_64/']),
 'centos8': ('rpm', [R + '8.5.2111/BaseOS/x86_64/os/', R + '8.5.2111/AppStream/x86_64/os/']),
 'stream9': ('rpm', [ST + '9-stream/BaseOS/x86_64/os/', ST + '9-stream/AppStream/x86_64/os/']),
 'stream10': ('rpm', [ST + '10-stream/BaseOS/x86_64/os/', ST + '10-stream/AppStream/x86_64/os/']),
 'rocky8': ('rpm', [RK + '8/BaseOS/x86_64/os/', RK + '8/AppStream/x86_64/os/']),
 'rocky9': ('rpm', [RK + '9/BaseOS/x86_64/os/', RK + '9/AppStream/x86_64/os/']),
 'rocky10': ('rpm', [RK + '10/BaseOS/x86_64/os/', RK + '10/AppStream/x86_64/os/']),
 'anolis7': ('rpm', [AN + '7.9/os/x86_64/os/', AN + '7.9/updates/x86_64/os/']),
 'anolis8': ('rpm', [AN + '8.10/BaseOS/x86_64/os/', AN + '8.10/AppStream/x86_64/os/', AN + '8.10/kernel-5.10/x86_64/os/']),
 'anolis23': ('rpm', [AN + '23.5/os/x86_64/os/', AN + '23.5/updates/x86_64/os/']),
 'oe2203': ('rpm', [OE + 'openEuler-22.03-LTS-SP4/OS/x86_64/', OE + 'openEuler-22.03-LTS-SP4/everything/x86_64/', OE + 'openEuler-22.03-LTS-SP4/update/x86_64/']),
 'oe2403': ('rpm', [OE + 'openEuler-24.03-LTS-SP4/OS/x86_64/', OE + 'openEuler-24.03-LTS-SP4/everything/x86_64/', OE + 'openEuler-24.03-LTS-SP4/update/x86_64/']),
 'kylinv10': ('rpm', [KY + 'V10/V10SP3-2403/os/adv/lic/base/x86_64/', KY + 'V10/V10SP3-2403/os/adv/lic/updates/x86_64/']),
 'kylinv11': ('rpm', [KY + 'V11/2503/os/adv/lic/base/x86_64/', KY + 'V11/2503/os/adv/lic/updates/x86_64/']),
 'kylinv11sp1': ('rpm', [KY + 'V11/V11SP1-2603/os/adv/lic/base/x86_64/', KY + 'V11/V11SP1-2603/os/adv/lic/updates/x86_64/']),
 'oc9': ('rpm', [OC + '9.6/BaseOS/x86_64/os/', OC + '9.6/AppStream/x86_64/os/']),
 'ol9': ('rpm', ['https://yum.oracle.com/repo/OracleLinux/OL9/baseos/latest/x86_64/', 'https://yum.oracle.com/repo/OracleLinux/OL9/appstream/x86_64/']),
 'alinux3': ('rpm', [AL + '3/os/x86_64/', AL + '3/updates/x86_64/', AL + '3/plus/x86_64/', AL + '3/module/x86_64/']),
 'alinux4': ('rpm', [AL + '4/os/x86_64/os/', AL + '4/updates/x86_64/os/', AL + '4/plus/x86_64/os/']),
 'bclinux8': ('rpm', ['https://mirrors.cmecloud.cn/bclinux/el8.8/BaseOS/x86_64/os/', 'https://mirrors.cmecloud.cn/bclinux/el8.8/AppStream/x86_64/os/']),
 'bclinux-oe': ('rpm', ['https://mirrors.cmecloud.cn/bclinux/oe22.10U2/OS/x86_64/', 'https://mirrors.cmecloud.cn/bclinux/oe22.10U2/everything/x86_64/']),
 'leap156': ('rpm', ['https://download.opensuse.org/distribution/leap/15.6/repo/oss/', 'https://download.opensuse.org/update/leap/15.6/oss/']),
 'leap160': ('rpm', ['https://download.opensuse.org/distribution/leap/16.0/repo/oss/']),
 'ubuntu2004': ('deb', ('http://archive.ubuntu.com/ubuntu/', ['focal', 'focal-updates'], ['main', 'universe'], 'amd64')),
 'ubuntu2204': ('deb', ('http://archive.ubuntu.com/ubuntu/', ['jammy', 'jammy-updates'], ['main', 'universe'], 'amd64')),
 'ubuntu2404': ('deb', ('http://archive.ubuntu.com/ubuntu/', ['noble', 'noble-updates'], ['main', 'universe'], 'amd64')),
 'ubuntu2604': ('deb', ('http://archive.ubuntu.com/ubuntu/', ['resolute', 'resolute-updates'], ['main', 'universe'], 'amd64')),
 'debian11': ('deb', ('https://deb.debian.org/debian/', ['bullseye', 'bullseye-updates'], ['main'], 'amd64')),
 'debian12': ('deb', ('https://deb.debian.org/debian/', ['bookworm', 'bookworm-updates'], ['main'], 'amd64')),
 'debian13': ('deb', ('https://deb.debian.org/debian/', ['trixie', 'trixie-updates'], ['main'], 'amd64')),
 # arm64：S2 起扫完整仓库（BaseOS + AppStream / OS + everything + update），避免把 AppStream 中的包误判为缺失
 'rocky8-arm64': ('rpm', [RK + '8/BaseOS/aarch64/os/', RK + '8/AppStream/aarch64/os/']),
 'rocky9-arm64': ('rpm', [RK + '9/BaseOS/aarch64/os/', RK + '9/AppStream/aarch64/os/']),
 'rocky10-arm64': ('rpm', [RK + '10/BaseOS/aarch64/os/', RK + '10/AppStream/aarch64/os/']),
 'anolis8-arm64': ('rpm', [AN + '8.10/BaseOS/aarch64/os/', AN + '8.10/AppStream/aarch64/os/', AN + '8.10/kernel-5.10/aarch64/os/']),
 'anolis23-arm64': ('rpm', [AN + '23.5/os/aarch64/os/', AN + '23.5/updates/aarch64/os/']),
 'oe2203-arm64': ('rpm', [OE + 'openEuler-22.03-LTS-SP4/OS/aarch64/', OE + 'openEuler-22.03-LTS-SP4/everything/aarch64/', OE + 'openEuler-22.03-LTS-SP4/update/aarch64/']),
 'oe2403-arm64': ('rpm', [OE + 'openEuler-24.03-LTS-SP4/OS/aarch64/', OE + 'openEuler-24.03-LTS-SP4/everything/aarch64/', OE + 'openEuler-24.03-LTS-SP4/update/aarch64/']),
 'kylinv10-arm64': ('rpm', [KY + 'V10/V10SP3-2403/os/adv/lic/base/aarch64/', KY + 'V10/V10SP3-2403/os/adv/lic/updates/aarch64/']),
 'kylinv11-arm64': ('rpm', [KY + 'V11/2503/os/adv/lic/base/aarch64/', KY + 'V11/2503/os/adv/lic/updates/aarch64/']),
 'kylinv11sp1-arm64': ('rpm', [KY + 'V11/V11SP1-2603/os/adv/lic/base/aarch64/', KY + 'V11/V11SP1-2603/os/adv/lic/updates/aarch64/']),
 'alinux3-arm64': ('rpm', [AL + '3/os/aarch64/', AL + '3/updates/aarch64/', AL + '3/plus/aarch64/', AL + '3/module/aarch64/', AL + '3/plus-64k/aarch64/']),
 'alinux4-arm64': ('rpm', [AL + '4/os/aarch64/os/', AL + '4/updates/aarch64/os/', AL + '4/plus/aarch64/os/', AL + '4/kernel-64k/aarch64/os/']),
 'oc9-arm64': ('rpm', [OC + '9.6/BaseOS/aarch64/os/', OC + '9.6/AppStream/aarch64/os/']),
 'ubuntu2404-arm64': ('deb', ('http://ports.ubuntu.com/ubuntu-ports/', ['noble', 'noble-updates'], ['main', 'universe'], 'arm64')),
 'debian12-arm64': ('deb', ('https://deb.debian.org/debian/', ['bookworm', 'bookworm-updates'], ['main'], 'arm64')),
}
KDIRS = {
 'alinux3': [AL + '3/plus/x86_64/kernels/'],
 'alinux3-arm64': [AL + '3/plus/aarch64/kernels/'],
}
def scan_dir(url):
    """解析 HTTP 目录列表中的 rpm 文件名 → {name: (ver-rel, url)}；只用于 repodata 不含的内核目录"""
    html = fetch(url).decode('utf-8', 'replace')
    out = {}
    for f in set(re.findall(r'href="([^"/]+\.rpm)"', html)):
        m = re.match(r'^(.+)-([^-]+)-([^-]+)\.([a-z0-9_]+)\.rpm$', f)
        if not m or m.group(4) == 'src': continue
        n, v = m.group(1), m.group(2) + '-' + m.group(3)
        if n not in out or vercmp(v, out[n][0]) > 0: out[n] = (v, url + f)
    return out
def run(key):
    kind, spec = TARGETS[key]
    res = {'key': key, 'kind': kind, 'errors': [], 'pkgs': {}, 'osrelease': None, 'count': 0}
    allp = {}
    try:
        if kind == 'rpm':
            osrel = []
            for base in spec:
                try:
                    p, o = scan_rpm(base); osrel += o
                    for k, v in p.items():
                        if k not in allp or vercmp(v[0], allp[k][0]) > 0: allp[k] = v
                except Exception as e:
                    res['errors'].append(f'{base}: {type(e).__name__} {e}'[:200])
            for d in KDIRS.get(key, []):
                try:
                    for k, v in scan_dir(d).items():
                        if k not in allp or vercmp(v[0], allp[k][0]) > 0: allp[k] = v
                    res.setdefault('kdirs', []).append(d)
                except Exception as e:
                    res['errors'].append(f'{d}: {type(e).__name__} {e}'[:200])
            if osrel:
                # 同名 release 包取最高版本（如 OL9 latest 仓库同时含 9.0–9.x 的 oraclelinux-release）
                import functools
                osrel.sort(key=functools.cmp_to_key(lambda a, b: (a[0] > b[0]) - (a[0] < b[0]) or -vercmp(a[1], b[1])))
                name, ver, loc = osrel[0]
                try: res['osrelease'] = f'[{name}-{ver}]\n' + rpm_osrelease(loc)
                except Exception as e: res['osrelease'] = f'ERR {e}'
        else:
            mirror, dists, comps, arch = spec
            for d in dists:
                try:
                    p = scan_deb(mirror, d, comps, arch)
                    if not p: res['errors'].append(f'{d}: empty')
                    for k, v in p.items():
                        if k not in allp or vercmp(v[0], allp[k][0]) > 0: allp[k] = v
                except Exception as e:
                    res['errors'].append(f'{d}: {type(e).__name__} {e}'[:200])
            if 'base-files' in allp:
                try: res['osrelease'] = deb_osrelease(allp['base-files'][1])
                except Exception as e: res['osrelease'] = f'ERR {e}'
        res['count'] = len(allp)
        for lname, rpmc, debc in LOGICAL:
            cands = rpmc if kind == 'rpm' else debc
            hit = [(c, allp[c][0]) for c in cands if c in allp]
            res['pkgs'][lname] = hit[0] if hit else None
            if hit and lname in LOCS:
                res.setdefault('loc', {})[lname] = allp[hit[0][0]][1]
        if kind == 'deb':
            hdr = [(k, v) for k, v in allp.items() if re.match(r'^linux-headers-\d[\d.\-]*-(generic|arm64|amd64)$', k)]
            if hdr:
                k, v = max(hdr, key=lambda kv: [int(x) for x in re.findall(r'\d+', kv[0])])
                res['pkgs']['kernel-headers'] = (k, v[0]); res.setdefault('loc', {})['kernel-headers'] = v[1]
    except Exception as e:
        res['errors'].append(traceback.format_exc()[-300:])
    return res
def save(out):
    """增量写出：每完成一个目标就合并写一次 JSON（中断不丢已完成结果）"""
    old = json.load(open(OUT)) if os.path.exists(OUT) else {}
    old.update(out)
    tmp = OUT + '.tmp'
    json.dump(old, open(tmp, 'w'), ensure_ascii=False, indent=1); os.replace(tmp, OUT)
if __name__ == '__main__':
    keys = sys.argv[1:] or list(TARGETS)
    n = int(os.environ.get('KI_WORKERS', '2'))
    Pool = cf.ProcessPoolExecutor if os.environ.get('KI_PROC', '1') == '1' else cf.ThreadPoolExecutor
    with Pool(n) as ex:
        futs = {ex.submit(run, k): k for k in keys}
        for f in cf.as_completed(futs):
            try: r = f.result()
            except Exception as e:
                r = {'key': futs[f], 'errors': [f'{type(e).__name__} {e}'], 'count': 0, 'pkgs': {}}
            save({r['key']: r})
            print(r['key'], r['count'], 'errors:', len(r['errors']), flush=True)
