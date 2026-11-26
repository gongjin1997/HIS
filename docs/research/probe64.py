#!/usr/bin/env python3
"""kinstall 调研（S2）：arm64 可选 64K 内核（及 openSUSE 默认内核）的 .config 实测。
输入 pkgmap.json 中 kernels 字段记录的 kernel-64k-devel / linux-headers-*-generic-64k / kernel-*-devel 地址，
输出 probe64.json：{target: {variant: kconfig 结果}}。"""
import json, os, re, sys
HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)
import probe

def pick(kernels):
    out = {}
    for name, (ver, url) in kernels.items():
        if name in ('kernel-64k-devel', 'kernel-64kb-devel'): out['64k'] = (name, ver, url)
        if name == 'kernel-default-devel': out['default'] = (name, ver, url)
    # Ubuntu：GA 内核（6.8）的 generic-64k 头文件包中版本最高者
    deb = [(n, v, u) for n, (v, u) in kernels.items() if re.match(r'linux-headers-6\.8\.0-\d+-generic-64k$', n)]
    if deb:
        out['64k'] = max(deb, key=lambda x: int(re.search(r'6\.8\.0-(\d+)', x[0]).group(1)))
    return out

if __name__ == '__main__':
    pm = json.load(open(os.path.join(HERE, 'pkgmap.json')))
    out_p = os.path.join(HERE, 'probe64.json')
    out = json.load(open(out_p)) if os.path.exists(out_p) else {}
    keys = sys.argv[1:] or [k for k in pm if k.endswith('-arm64')]
    for k in keys:
        for variant, (name, ver, url) in pick(pm.get(k, {}).get('kernels', {})).items():
            try: r = probe.kconfig(url)
            except Exception as e: r = {'error': f'{type(e).__name__} {e}'[:200]}
            r.update({'pkg': name, 'ver': ver})
            out.setdefault(k, {})[variant] = r
            print(k, variant, json.dumps(r, ensure_ascii=False)[:300], flush=True)
    json.dump(out, open(out_p, 'w'), ensure_ascii=False, indent=1)
