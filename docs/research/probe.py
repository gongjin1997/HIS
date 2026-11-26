#!/usr/bin/env python3
"""kinstall 调研（S2）：从官方源下载少量包做“包内实测”。
- systemd 编译默认 cgroup 层级：在 systemd / libsystemd-shared 的 ELF 中查找 "default-hierarchy=" 特性串
  （systemd >= 256 已移除该选项，只剩 unified）。
- arm64 内核页大小与 VA 位数：从 kernel-devel（rpm）或 linux-headers（deb）中读取 .config。
输入 repo_scan.json（需要其中的 loc 字段），输出 probe.json。"""
import json, os, re, subprocess, sys, io, tarfile, lzma, gzip
import concurrent.futures as cf
HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)
import repo_scan as rs

def stream_cpio(proc_stdout, want):
    """流式解析 newc cpio，只保留 want(name) 为真的文件内容。"""
    out, buf = {}, proc_stdout
    def read(n):
        b = b''
        while len(b) < n:
            chunk = buf.read(n - len(b))
            if not chunk: break
            b += chunk
        return b
    pos = 0
    while True:
        h = read(110); pos += len(h)
        if len(h) < 110 or h[:6] not in (b'070701', b'070702'): break
        f = [int(h[6 + 8 * k:14 + 8 * k], 16) for k in range(13)]
        size, nsz = f[6], f[11]
        pad1 = (4 - (110 + nsz) % 4) % 4
        name = read(nsz)[:-1].decode(errors='replace'); read(pad1)
        if name == 'TRAILER!!!': break
        data = read(size); read((4 - size % 4) % 4)
        if want(name.lstrip('.')): out[name.lstrip('.')] = data
    return out

def rpm_files(url, want):
    raw = rs.fetch(url, deadline=1800)
    p = subprocess.Popen(['rpm2cpio', '-'], stdin=subprocess.PIPE, stdout=subprocess.PIPE)
    import threading
    t = threading.Thread(target=lambda: (p.stdin.write(raw), p.stdin.close())); t.start()
    files = stream_cpio(p.stdout, want)
    p.stdout.read(); p.wait(); t.join()
    return files

def deb_files(url, want):
    raw = rs.fetch(url, deadline=1800)
    assert raw[:8] == b'!<arch>\n'
    i, out = 8, {}
    while i < len(raw):
        hdr = raw[i:i + 60]; name = hdr[:16].decode().strip().rstrip('/'); size = int(hdr[48:58].decode().strip())
        body = raw[i + 60:i + 60 + size]
        if name.startswith('data.tar'):
            data = rs.decompress(body, name)
            with tarfile.open(fileobj=io.BytesIO(data)) as tf:
                for m in tf.getmembers():
                    n = m.name.lstrip('.')
                    if m.isfile() and want(n): out[n] = tf.extractfile(m).read()
        i += 60 + size + (size & 1)
    return out

def files_of(url, want):
    return deb_files(url, want) if url.endswith('.deb') else rpm_files(url, want)

def systemd_default(url):
    """返回 systemd 编译默认的 cgroup 层级。特性串在 libsystemd-shared 中；部分发行版（如 OpenCloudOS 9）
    把它拆到 systemd-shared 子包，主包找不到时自动改查该子包。"""
    want = lambda n: ('libsystemd-shared' in n and '.so' in n) or n.endswith('/systemd/systemd') or n.endswith('/bin/systemctl')
    def scan(u):
        hits = set()
        for n, data in files_of(u, want).items():
            for m in re.finditer(rb'default-hierarchy=([a-z]+)', data):
                hits.add(m.group(1).decode())
        return hits
    hits = scan(url)
    fn = url.rsplit('/', 1)[-1]
    if not hits and re.match(r'systemd-\d', fn):
        alt = url.rsplit('/', 1)[0] + '/' + fn.replace('systemd-', 'systemd-shared-', 1)
        try: hits = scan(alt)
        except Exception: hits = set()
        if hits: return sorted(hits) + ['(取自 systemd-shared 子包)']
    if hits: return sorted(hits)
    m = re.search(r'systemd[-_](?:shared-)?(\d+)', fn) or re.search(r'libsystemd-shared_(\d+)', fn)
    v = int(m.group(1)) if m else 0
    if v and v < 233: return ['(systemd<233：不支持 unified 选项，仅 v1)']
    return ['(systemd>=256：无该选项，仅 unified)']

def kconfig(url):
    files = files_of(url, lambda n: n.endswith('/.config') and ('/usr/src/' in n))
    if not files: return {'error': 'no .config in ' + url.rsplit('/', 1)[-1]}
    txt = list(files.values())[0].decode(errors='replace')
    g = lambda k: (re.search(rf'^{k}=(\S+)', txt, re.M) or [None, None])[1]
    page = '64K' if g('CONFIG_ARM64_64K_PAGES') == 'y' else '16K' if g('CONFIG_ARM64_16K_PAGES') == 'y' else '4K' if g('CONFIG_ARM64_4K_PAGES') == 'y' else '?'
    return {'page': page, 'va_bits': g('CONFIG_ARM64_VA_BITS'), 'pgtable_levels': g('CONFIG_PGTABLE_LEVELS'),
            'thp': g('CONFIG_TRANSPARENT_HUGEPAGE'), 'cgroup_v1_memory': g('CONFIG_MEMCG_V1'),
            'src': list(files)[0]}

def probe(key, r):
    res = {'key': key}
    loc = r.get('loc', {})
    try:
        s = loc.get('libsystemd-shared') or loc.get('systemd')
        if s: res['systemd_default_hierarchy'] = systemd_default(s); res['systemd_pkg'] = s.rsplit('/', 1)[-1]
    except Exception as e: res['systemd_err'] = f'{type(e).__name__} {e}'[:200]
    if key.endswith('-arm64'):
        for lk in ('kernel-devel', 'kernel-headers'):
            if lk in loc:
                try: res['kconfig'] = kconfig(loc[lk]); res['kconfig_pkg'] = loc[lk].rsplit('/', 1)[-1]
                except Exception as e: res['kconfig_err'] = f'{type(e).__name__} {e}'[:200]
                break
    return res

if __name__ == '__main__':
    scan = json.load(open(os.path.join(HERE, 'repo_scan.json')))
    keys = sys.argv[1:] or list(scan)
    out_p = os.path.join(HERE, 'probe.json')
    out = json.load(open(out_p)) if os.path.exists(out_p) else {}
    with cf.ThreadPoolExecutor(3) as ex:
        for res in ex.map(lambda k: probe(k, scan[k]), keys):
            out[res['key']] = res
            print(json.dumps(res, ensure_ascii=False), flush=True)
    json.dump(out, open(out_p, 'w'), ensure_ascii=False, indent=1)
