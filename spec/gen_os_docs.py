#!/usr/bin/env python3
"""spec/gen_os_docs.py：校验 osmatch.yaml / pkgmap.yaml，执行 golden 测试，生成 03 的表格（ADR-029）。

检查内容：
  1. 结构：规则 ID 唯一、族 / 等级 / 证据 / 软件源取值合法、正则可编译、引用的 Q 编号在 03 §4.7 中存在；
  2. golden：research/repo_scan.json 的全部实测 os-release 必须唯一命中其所属规则（support.samples），
     spec/osmatch_golden.yaml 中的文档 / 构造样本必须命中期望规则，并断言变体；
  3. 基线交叉核对：baseline 的内核、systemd、编译默认 cgroup、nft、glibc、arm64 页大小与 03-A 的实测数据一致；
     os-release 中的 SUPPORT_END 与 lifecycle.eol 一致；
  4. pkgmap 回放：用 pkgmap.yaml 为每个实测样本解析全部逻辑包，结果必须与 research/pkgmap.json 一致。
生成块（03 中以“_下表由 `spec/gen_os_docs.py` 生成（块：名称）…_”标记，标记后的表格整体替换）：
  os-matrix（§3.2）、os-k8s（§3.3）、os-rules（§4.2）、os-golden（§4.2）、pkg-diff / pkg-same（§4.3）。
用法：
  python3 spec/gen_os_docs.py            校验 + 测试，并报告 03 中过期的生成块
  python3 spec/gen_os_docs.py --write    同上，并回写 03
  python3 spec/gen_os_docs.py --check    生成块过期时以非零状态退出（供 CI 使用）
  python3 spec/gen_os_docs.py --print 块名
"""
import datetime as dt, os, re, sys
import yaml

H = os.path.dirname(os.path.abspath(__file__))
ROOT = os.path.dirname(H)
DOC_ROOT = os.path.join(ROOT, 'docs') if os.path.exists(os.path.join(ROOT, 'docs', '03-CMDB与主机初始化.md')) else ROOT
sys.path.insert(0, os.path.join(DOC_ROOT, 'research'))
import gen_03a as g  # noqa: E402  03-A 的数据读取函数（只读 JSON，不联网）

DOC03 = os.path.join(DOC_ROOT, '03-CMDB与主机初始化.md')
OM = yaml.safe_load(open(os.path.join(H, 'osmatch.yaml')))
GOLD = yaml.safe_load(open(os.path.join(H, 'osmatch_golden.yaml')))
PKG = yaml.safe_load(open(os.path.join(H, 'pkgmap.yaml')))
FAM, RULES, META = OM['families'], OM['rules'], OM['metadata']
ASOF = dt.date.fromisoformat(META['dataAsOf'])
SOON = int(META.get('eolSoonDays', 180))
ERR, WARN = [], []

FIELD = {'id': 'ID', 'versionId': 'VERSION_ID', 'version': 'VERSION', 'name': 'NAME', 'prettyName': 'PRETTY_NAME',
         'platformId': 'PLATFORM_ID', 'versionCodename': 'VERSION_CODENAME'}
TIERS = ('P0', 'P1', 'P2', 'unverified')
EVID = ('measured', 'represented', 'documented', 'none')
SRC = {'online': '在线', 'online-frozen': '在线（已停更）', 'vault': 'vault / 离线', 'licensed': '需授权 / 离线', 'offline': '离线'}
CG = {'v1': 'v1', 'v2': 'v2', 'hybrid': 'hybrid', 'v1-only': '仅 v1', 'v2-only': '仅 v2'}
ST = {'supported': '支持中', 'eol-soon': '即将 EOL', 'eol': '已 EOL', 'unknown': '未公开'}

# ---------------------------------------------------------------- 匹配
def parse_osrelease(text):
    out = {}
    for line in (text or '').splitlines():
        line = line.strip()
        if not line or line.startswith(('#', '[')) or '=' not in line: continue
        k, v = line.split('=', 1)
        v = v.strip()
        if len(v) >= 2 and v[0] == v[-1] and v[0] in '"\'': v = v[1:-1]
        out[k.strip()] = v
    return out

def vmatch(pat, val):
    if val is None: return False
    for p in (pat if isinstance(pat, list) else [pat]):
        p = str(p)
        if p.startswith('re:'):
            if re.search(p[3:], val): return True
        elif p.lower() == str(val).lower():
            return True
    return False

def cond(m, f):
    for k, v in m.items():
        if k in FIELD:
            if not vmatch(v, f['os'].get(FIELD[k])): return False
        elif k == 'idLike':
            if not any(vmatch(v, t) for t in (f['os'].get('ID_LIKE') or '').split()): return False
        elif k == 'osrelease':
            if not all(vmatch(vv, f['os'].get(kk)) for kk, vv in v.items()): return False
        elif k == 'file':
            c = f['files'].get(v['path'])
            if v.get('exists') is True and c is None: return False
            if v.get('exists') is False and c is not None: return False
            if 'contains' in v and (c is None or not re.search(str(v['contains'])[3:] if str(v['contains']).startswith('re:') else re.escape(str(v['contains'])), c, re.M)):
                return False
        elif k == 'anyOf':
            if not any(cond(x, f) for x in v): return False
        elif k == 'not':
            if cond(v, f): return False
        else:
            raise ValueError(f'未知匹配键 {k}')
    return True

def variants(rule, f):
    out = {}
    for name, sp in (rule.get('variants') or {}).items():
        src, val = sp['from'], None
        if src == 'osrelease':
            val = f['os'].get(sp['key'])
        else:
            text = f['files'].get(sp['path']) if src == 'file' else f['os'].get(FIELD[src])
            m = re.search(sp['pattern'], text or '', re.M) if 'pattern' in sp else None
            val = m.group('v') if m else (text if 'pattern' not in sp else None)
        if val is None: val = sp.get('default')
        if val is not None and 'map' in sp: val = sp['map'].get(str(val).lower(), val)
        if val is not None: out[name] = str(val)
    return out

def match(f):
    hits = [r for r in RULES if cond(r['match'], f)]
    if not hits: return None, {}, []
    top = max(r['priority'] for r in hits)
    best = [r for r in hits if r['priority'] == top]
    if len(best) > 1: raise LookupError('同一优先级命中多条规则：' + '、'.join(r['id'] for r in best))
    return best[0], variants(best[0], f), hits

# ---------------------------------------------------------------- 工具
def vt(s): return tuple(int(x) for x in re.findall(r'\d+', str(s or ''))[:3])
def chain(rule):
    out, f = [], rule['family']
    if f == 'detect': return out
    if FAM.get(f, {}).get('parent') == 'per-rule':
        out.append(f); f = rule.get('parent')
    while f:
        out.append(f); f = FAM[f].get('parent')
    return out
def attr(rule, k):
    if k in (rule.get('overrides') or {}): return rule['overrides'][k]
    for f in chain(rule):
        if k in FAM[f]: return FAM[f][k]
def end_of(s):
    if s is None: return None
    s = str(s)
    if re.fullmatch(r'\d{4}', s): return dt.date(int(s), 12, 31)
    if re.fullmatch(r'\d{4}-\d{2}', s):
        y, m = map(int, s.split('-')); return dt.date(y + (m == 12), m % 12 + 1, 1) - dt.timedelta(days=1)
    return dt.date.fromisoformat(s)
def status(s):
    e = end_of(s)
    if e is None: return 'unknown'
    if e < ASOF: return 'eol'
    return 'eol-soon' if (e - ASOF).days <= SOON else 'supported'
def exact(r): return r['priority'] >= 90
def esc(s): return str(s).replace('|', '\\|')
def code(s): return f'`{esc(s)}`'
def samples(r): return r['support'].get('samples') or []
def sample_facts(k):
    return {'os': parse_osrelease(g.scan.get(k, {}).get('osrelease') or ''), 'files': {}}

# ---------------------------------------------------------------- 1. 结构校验
quirk_ids = set(re.findall(r'^\| (Q-\d\d) \|', open(DOC03).read(), re.M))
ids = [r['id'] for r in RULES]
for x in {i for i in ids if ids.count(i) > 1}: ERR.append(f'规则 ID 重复：{x}')
for r in RULES:
    rid, s = r['id'], r.get('support') or {}
    if r['family'] != 'detect' and r['family'] not in FAM: ERR.append(f'{rid}：未知族 {r["family"]}')
    if FAM.get(r['family'], {}).get('parent') == 'per-rule' and r.get('parent') not in FAM: ERR.append(f'{rid}：需要 parent')
    if s.get('tier') not in TIERS: ERR.append(f'{rid}：tier 非法')
    if s.get('evidence') not in EVID: ERR.append(f'{rid}：evidence 非法')
    if not exact(r) and (s.get('tier') != 'unverified' or s.get('evidence') != 'none'): ERR.append(f'{rid}：兜底规则须为 unverified / none')
    if exact(r):
        if s.get('pkgSource') not in SRC: ERR.append(f'{rid}：pkgSource 非法')
        if 'lifecycle' not in s: ERR.append(f'{rid}：缺 lifecycle')
        if s.get('evidence') == 'measured' and not samples(r): ERR.append(f'{rid}：measured 但没有样本')
        lc = s.get('lifecycle') or {}
        for d in [lc.get('eol'), lc.get('extended')] + list(((lc.get('byVariant') or {}).get('sp') or {}).values()):
            try: end_of(d)
            except Exception: ERR.append(f'{rid}：日期格式错误 {d}')
    for k in samples(r):
        if k not in g.scan and k not in g.p64: ERR.append(f'{rid}：样本 {k} 不在 research 数据中')
    for q in r.get('quirks') or []:
        if q not in quirk_ids: ERR.append(f'{rid}：03 §4.7 中没有 {q}')
    def walk(m):
        for k, v in m.items():
            if k in ('anyOf',): [walk(x) for x in v]
            elif k == 'not': walk(v)
            elif k == 'osrelease': [walk({'id': vv}) for vv in v.values()]
            elif k == 'file': pass
            else:
                for p in (v if isinstance(v, list) else [v]):
                    if str(p).startswith('re:'):
                        try: re.compile(str(p)[3:])
                        except re.error as e: ERR.append(f'{rid}：正则错误 {p}：{e}')
    walk(r['match'])
owner = {}
for r in RULES:
    for k in samples(r):
        if k in owner: ERR.append(f'样本 {k} 同时属于 {owner[k]["id"]} 与 {r["id"]}')
        owner[k] = r
for k in g.scan:
    if k not in owner: ERR.append(f'实测样本 {k} 未归属任何规则')

# ---------------------------------------------------------------- 2. golden
gold_rows, n_meas = [], 0
for k in sorted(g.scan):
    if k not in owner: continue
    try:
        r, var, hits = match(sample_facts(k))
    except LookupError as e:
        ERR.append(f'golden {k}：{e}'); continue
    n_meas += 1
    if r is None or r['id'] != owner[k]['id']:
        ERR.append(f'golden {k}：期望 {owner[k]["id"]}，命中 {r and r["id"]}'); continue
    for vk, vv in (GOLD.get('measured') or {}).get(k, {}).items():
        if var.get(vk) != str(vv): ERR.append(f'golden {k}：变体 {vk} 期望 {vv}，得到 {var.get(vk)}')
for c in GOLD.get('cases') or []:
    f = {'os': parse_osrelease(c['osrelease']), 'files': c.get('files') or {}}
    try:
        r, var, hits = match(f)
    except LookupError as e:
        ERR.append(f'golden {c["name"]}：{e}'); continue
    got = r['id'] if r else None
    if got != c.get('expect'): ERR.append(f'golden {c["name"]}：期望 {c.get("expect")}，命中 {got}'); continue
    for vk, vv in (c.get('variants') or {}).items():
        if var.get(vk) != str(vv): ERR.append(f'golden {c["name"]}：变体 {vk} 期望 {vv}，得到 {var.get(vk)}')
    gold_rows.append((c, got, var))

# ---------------------------------------------------------------- 3. 基线交叉核对（与 03-A 数据）
def measured(k):
    sd = (g.pk(k, 'systemd') or [None, ''])[1]
    return {'kernel': g.kern(k), 'kernelHwe': g.kern(k, 'kernel-hwe'), 'systemd': int(re.match(r'\d+', sd).group()) if sd else None,
            'cgroup': g.cg(k), 'nft': g.up((g.pk(k, 'nftables') or [None, None])[1]), 'glibc': g.up((g.pk(k, 'glibc') or [None, None])[1])}
for r in RULES:
    b = r.get('baseline') or {}
    for k in samples(r):
        if k.endswith('-arm64') or k not in g.scan: continue
        m = measured(k)
        if b.get('kernel') != m['kernel']: ERR.append(f'{r["id"]}：kernel {b.get("kernel")} ≠ 实测 {m["kernel"]}')
        if b.get('systemd') != m['systemd']: ERR.append(f'{r["id"]}：systemd {b.get("systemd")} ≠ 实测 {m["systemd"]}')
        if CG.get(b.get('cgroup')) != m['cgroup']: ERR.append(f'{r["id"]}：cgroup {b.get("cgroup")} ≠ 实测 {m["cgroup"]}')
        if str(b.get('nft')) != str(m['nft']): ERR.append(f'{r["id"]}：nft {b.get("nft")} ≠ 实测 {m["nft"]}')
        if str(b.get('glibc')) != str(m['glibc']): ERR.append(f'{r["id"]}：glibc {b.get("glibc")} ≠ 实测 {m["glibc"]}')
        hwe = m['kernelHwe'] if m['kernelHwe'] != m['kernel'] else None
        if bool(b.get('kernelHwe')) != bool(hwe) or (hwe and vt(hwe)[:2] != vt(b['kernelHwe'])[:2]):
            ERR.append(f'{r["id"]}：kernelHwe {b.get("kernelHwe")} ≠ 实测 {hwe}')
        se = sample_facts(k)['os'].get('SUPPORT_END')
        if se and se != str((r['support'].get('lifecycle') or {}).get('eol')): ERR.append(f'{r["id"]}：SUPPORT_END {se} ≠ lifecycle.eol')
    base = [k for k in samples(r) if not k.endswith('-arm64')]
    arm = (base[0] + '-arm64') if base else None
    if arm:
        a = b.get('arm64') or {}
        c = (g.pr.get(arm) or {}).get('kconfig') or ((g.p64.get(arm) or {}).get('default') or {})
        o = (g.p64.get(arm) or {}).get('64k')
        if c.get('page') and a.get('pageSize') != c['page']: ERR.append(f'{r["id"]}：arm64 pageSize {a.get("pageSize")} ≠ 实测 {c["page"]}')
        if not c.get('page') and a.get('pageSize'): ERR.append(f'{r["id"]}：arm64 无实测数据，不应填写 pageSize')
        if bool(o and not o.get('error')) != bool(a.get('optional64k')) and arm in g.p64:
            ERR.append(f'{r["id"]}：optional64k 与实测不一致')

# ---------------------------------------------------------------- 4. pkgmap 回放
def pkgtype(rule): return attr(rule, 'pkgType')
def resolve(p, rule):
    ov = p.get('overrides') or []
    for o in ov:
        if rule['id'] in (o['when'].get('rule') or []): return o['pkg']
    for fam in chain(rule):
        for o in ov:
            if o['when'].get('family') == fam: return o['pkg']
    return p['default'][pkgtype(rule)]
n_replay = 0
for k, r in owner.items():
    mp = (g.pm.get(k) or {}).get('map')
    if not mp: continue
    for p in PKG['packages']:
        exp = (mp.get(p['name']) or {}).get('pkg')
        got = resolve(p, r)
        n_replay += 1
        if exp != got: ERR.append(f'pkgmap {k} {p["name"]}：实测 {exp}，解析 {got}')

# ---------------------------------------------------------------- 派生：生命周期、K8s 判定、初始化提示
def lc_cells(r):
    lc = r['support'].get('lifecycle') or {}
    bv = (lc.get('byVariant') or {}).get('sp')
    if bv:
        lab = ((r.get('variants') or {}).get('sp') or {}).get('labels') or {}
        name = lambda v: lab.get(v, f'SP{v}')
        life = '；'.join(f'{name(v)} {d}' for v, d in bv.items())
        groups = {}
        for v, d in bv.items(): groups.setdefault(status(d), []).append(name(v))
        st = '；'.join(f'{"、".join(groups[s])} {ST[s]}' for s in ('supported', 'eol-soon', 'eol') if s in groups)
        return life, st, ('supported' if 'supported' in groups else 'eol-soon' if 'eol-soon' in groups else 'eol')
    e, note = lc.get('eol'), lc.get('note')
    s = status(e)
    if not e: return note or '未公开', '按版本' if (note or '').startswith('按版本') else ST[s], s
    life = f'至 {e}' if s != 'eol' else f'{e} 结束'
    if note: life += f'（{note}）'
    return life, ST[s], s

def k8s(r):
    b, sup = r.get('baseline') or {}, r['support']
    if not b.get('kernel') or not b.get('systemd'): return '—', ['视具体版本']
    kv, sd = vt(b['kernel']), b['systemd']
    st = lc_cells(r)[2]
    if sd < 239 or kv < (4, 18): return '❌', ['systemd / 内核不支持 cgroup v2']
    notes, sym = [], '✅'
    cgv = b.get('cgroup')
    if cgv in ('v1', 'hybrid'): notes.append('切 cgroup v2（重启）')
    elif cgv is None: notes.append('默认 cgroup 待实测')
    if kv < (5, 8):
        up = b.get('kernelUpgrade') or {}
        hwe = b.get('kernelHwe')
        if hwe and vt(hwe) >= (5, 8):
            sym = '⚠️'; notes.append(f'升级 HWE {hwe[:4].rstrip(".")} 内核')
        elif up.get('source') == 'official' and vt(up.get('version')) >= (5, 8):
            sym = '⚠️'; notes.append(f'切换内核：{up.get("note")}')
        elif st == 'eol':
            return '❌', ['内核 < 5.8 且 OS 已 EOL']
        else:
            sym = '⚠️'; notes.append('内核 < 5.8：仅实验模式（`--allow-unsupported-kernel`）' + (f'，或换用{up["note"]}' if up.get('note') else ''))
    if st == 'eol':
        sym = '⚠️' if sym == '✅' else sym; notes.append('OS 已 EOL，不推荐')
    if (sup.get('cpu') or {}).get('amd64'): notes.append(f'x86 需 {sup["cpu"]["amd64"]}')
    if sup.get('evidence') == 'documented': notes.append('未实测')
    return sym, notes

def nft_cell(b):
    if not b.get('kernel') or not b.get('nft'): return '待实测' if b.get('kernel') else '—'
    ok_k, ok_n = vt(b['kernel']) >= (5, 13), vt(b['nft']) >= (1, 0, 1)
    return '✅' if ok_k and ok_n else '❌ 内核 < 5.13' if ok_n else '❌ nft < 1.0.1' if ok_k else '❌ 内核与 nft 均不足'

def init_cell(r):
    sup, notes = r['support'], []
    src = sup.get('pkgSource')
    notes.append({'vault': '需切 vault 或离线源', 'online-frozen': '源已停更，建议离线源', 'licensed': '需授权源或离线源',
                  'offline': '离线源'}.get(src, ''))
    if samples(r):
        req, opt = [], []
        for p in PKG['packages']:
            if resolve(p, r) is not None or p['default'].get(pkgtype(r)) is None and p['class'] != 'required': continue
            if p['class'] == 'required': req.append(p['name'])
            elif p['class'] == 'conditional':
                w = p.get('when', '')
                if w.startswith('mac:') and w.split(':', 1)[1].strip() != attr(r, 'mac'): continue
                req.append(f'{p["name"]}（{w}）')
            elif p['class'] == 'optional':
                if p['name'] == 'grubby' and attr(r, 'bootloader') != 'grubby': continue
                opt.append(p['name'])
        if req: notes.append('缺 ' + '、'.join(code(x.split('（')[0]) + ('（SELinux enforcing 时阻断）' if 'mac:' in x else '') for x in req))
        if opt: notes.append('可选工具缺 ' + '、'.join(code(x) for x in opt))
    if sup.get('evidence') == 'documented': notes.append('未实测')
    notes = [n for n in notes if n]
    return '✅' + ('；' + '；'.join(notes) if notes else '')

def sample_text(r):
    ks = samples(r)
    if not ks: return {'documented': '文档资料', 'none': '—'}.get(r['support']['evidence'], '—')
    base = sorted({k.replace('-arm64', '') for k in ks})
    arch = '、'.join(a for a, ok in (('amd64', any(not k.endswith('-arm64') for k in ks)), ('arm64', any(k.endswith('-arm64') for k in ks))) if ok)
    t = f'实测：{"、".join(g.NAME.get(k, k) for k in base)}（{arch}）'
    if r['support'].get('evidenceNote'): t += f'；{r["support"]["evidenceNote"]}'
    return t

def tier_cell(r):
    t = r['support']['tier']
    t = f'**{t}**' if t == 'P0' else t
    return t + ('（目标；实测前按 unverified）' if r['support']['evidence'] in ('documented', 'none') and r['support']['tier'] != 'unverified' else '')

def fam_cell(r):
    f = r['family'] + (f' → {r["parent"]}' if r.get('parent') else '')
    return f'{f} / {attr(r, "pkgManager")}'

# ---------------------------------------------------------------- 渲染
def t_matrix():
    out = ['| 系统 | 规则 | 适配器 / 包管理 | 等级 | 依据 | 生命周期 | 状态（截至 ' + META['dataAsOf'] + '） | 软件源 | 主机初始化 |',
           '| --- | --- | --- | --- | --- | --- | --- | --- | --- |']
    for r in filter(exact, RULES):
        life, st, _ = lc_cells(r)
        out.append(f'| {r["display"]} | {code(r["id"])} | {fam_cell(r)} | {tier_cell(r)} | {sample_text(r)} | {esc(life)} | {st} | {SRC[r["support"]["pkgSource"]]} | {init_cell(r)} |')
    return '\n'.join(out)

def t_k8s():
    out = ['| 系统 | 默认内核 | 其他内核 | systemd | 编译默认 cgroup | nft | kube-proxy nftables | arm64 页大小 | K8s 1.35+ | 说明 / 初始化动作 |',
           '| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |']
    for r in filter(exact, RULES):
        b = r.get('baseline') or {}
        alt = '；'.join(x for x in [f'HWE {b["kernelHwe"]}' if b.get('kernelHwe') else '', b.get('kernelAlt') or ''] if x) or '—'
        a = b.get('arm64') or {}
        pg = ('**64K**' if a.get('pageSize') == '64K' else a.get('pageSize')) if a.get('pageSize') else '未实测'
        if a.get('optional64k'):
            o = a['optional64k']
            pg += f'（可选 64K：{code(o) if o.isascii() else esc(o)}）'
        sym, notes = k8s(r)
        out.append(f'| {r["display"]} | {b.get("kernel") or "—"} | {esc(alt)} | {b.get("systemd") or "—"} | {CG.get(b.get("cgroup"), "待实测") if b.get("kernel") else "—"} | {b.get("nft") or "—"} | {nft_cell(b)} | {pg} | {sym} | {"；".join(notes) or "—"} |')
    return '\n'.join(out)

def fmtv(v):
    vs = [str(x) for x in (v if isinstance(v, list) else [v])]
    if len(vs) == 1:
        x = vs[0]; return f'~ {code(x[3:])}' if x.startswith('re:') else f'= {code(x)}'
    return '∈ {' + '、'.join(code(x[3:]) if x.startswith('re:') else code(x) for x in vs) + '}'
def rmatch(m):
    parts = []
    for k, v in m.items():
        if k in FIELD: parts.append(f'{FIELD[k]} {fmtv(v)}')
        elif k == 'idLike': parts.append(f'ID_LIKE 含 {fmtv(v)[2:] if not isinstance(v, list) else fmtv(v)[2:]}')
        elif k == 'osrelease': parts += [f'{kk} {fmtv(vv)}' for kk, vv in v.items()]
        elif k == 'file': parts.append(f'存在 {code(v["path"])}' if v.get('exists') else f'{code(v["path"])} 匹配 {code(v.get("contains"))}')
        elif k == 'anyOf': parts.append('（' + ' 或 '.join(rmatch(x) for x in v) + '）')
        elif k == 'not': parts.append('且非（' + rmatch(v) + '）')
    return '，'.join(parts)
def t_rules():
    out = ['| 优先级 | 规则 | 匹配条件 | 适配器族 | 变体 | 等级 |', '| --- | --- | --- | --- | --- | --- |']
    for r in sorted(RULES, key=lambda r: -r['priority']):
        var = '；'.join(f'{n}（{sp.get("key") or sp.get("path") or FIELD.get(sp["from"], sp["from"])}）' for n, sp in (r.get('variants') or {}).items()) or '—'
        fam = r['family'] + (f' → {r["parent"]}' if r.get('parent') else '')
        if fam == 'detect': fam = '按包管理器探测'
        out.append(f'| {r["priority"]} | {code(r["id"])} | {rmatch(r["match"])} | {fam} | {esc(var)} | {tier_cell(r)} |')
    return '\n'.join(out)

def t_golden():
    out = ['| 样本 | 来源 | 命中规则 | 变体 |', '| --- | --- | --- | --- |',
           f'| `research/repo_scan.json` 全部实测样本（{n_meas} 个，含 arm64） | 实测 | 各自所属规则（`support.samples`） | {len(GOLD.get("measured") or {})} 个样本另断言变体 |']
    for c, got, var in gold_rows:
        out.append(f'| {esc(c["name"])} | {"文档" if c["kind"] == "documented" else "构造"} | {code(got) if got else "无（→ 包管理器探测 → unknown）"} | {esc("、".join(f"{k}={v}" for k, v in var.items())) or "—"} |')
    return '\n'.join(out)

CLS = {'required': '必需', 'conditional': '条件必需', 'optional': '可选', 'detect': '检测'}
def t_pkgdiff():
    out = ['| 逻辑名 | 类别 | 默认（rpm / deb） | 覆盖（族 / 规则 → 包名） | 用途 |', '| --- | --- | --- | --- | --- |']
    for p in PKG['packages']:
        if not p.get('overrides'): continue
        d = p['default']
        dd = ' / '.join(code(d[t]) if d[t] else '无' for t in ('rpm', 'deb'))
        ov = []
        for o in p['overrides']:
            w = o['when'].get('family') or '、'.join(o['when'].get('rule') or [])
            ov.append(f'{w} → {code(o["pkg"]) if o["pkg"] else "无此包"}')
        cls = CLS[p['class']] + (f'（{p["when"]}）' if p.get('when') else '')
        out.append(f'| {p["name"]} | {cls} | {dd} | {esc("；".join(ov))} | {esc(p["purpose"])} |')
    return '\n'.join(out)
def same_cell(p):
    d, n = p['default'], p['name']
    r, b = d.get('rpm'), d.get('deb')
    if r == n and b == n: return code(n)
    if r == b: return f'{code(n)}（{code(r)}）'
    parts = [f'{t}{"：" + code(v) if v else " 无"}' for t, v in (('rpm', r), ('deb', b)) if v != n]
    return f'{code(n)}（{"；".join(parts)}）'
def t_pkgsame():
    out = ['| 类别 | 无族 / 规则差异的逻辑包（括号内为与逻辑名不同的 rpm / deb 包名） |', '| --- | --- |']
    for c in ('required', 'conditional', 'optional', 'detect'):
        xs = [p for p in PKG['packages'] if p['class'] == c and not p.get('overrides')]
        if xs: out.append(f'| {CLS[c]} | {"、".join(same_cell(p) for p in xs)} |')
    return '\n'.join(out)

BLOCKS = {'os-matrix': t_matrix, 'os-k8s': t_k8s, 'os-rules': t_rules, 'os-golden': t_golden, 'pkg-diff': t_pkgdiff, 'pkg-same': t_pkgsame}
MARK = re.compile(r'^_下表由 `spec/gen_os_docs\.py` 生成（块：(?P<n>[a-z0-9-]+)）')

def apply(text, gen):
    lines, out, i, found, changed = text.split('\n'), [], 0, set(), set()
    while i < len(lines):
        out.append(lines[i]); m = MARK.match(lines[i]); i += 1
        if not m: continue
        n = m.group('n'); found.add(n)
        j = i
        while j < len(lines) and lines[j].strip() == '': j += 1
        k = j
        while k < len(lines) and lines[k].startswith('|'): k += 1
        old = '\n'.join(lines[j:k]); new = gen[n]
        if old != new: changed.add(n)
        out += [''] + new.split('\n'); i = k
    return '\n'.join(out), found, changed

if __name__ == '__main__':
    if '--print' in sys.argv:
        print(BLOCKS[sys.argv[sys.argv.index('--print') + 1]]()); sys.exit(0)
    if ERR:
        print(f'校验失败（{len(ERR)}）：'); [print('  -', e) for e in ERR]; sys.exit(1)
    gen = {n: f() for n, f in BLOCKS.items()}
    text = open(DOC03).read()
    new, found, changed = apply(text, gen)
    missing = set(BLOCKS) - found
    n_exact = sum(1 for r in RULES if exact(r))
    print(f'osmatch {META["revision"]}：{len(RULES)} 条规则（精确 {n_exact}、兜底 {len(RULES) - n_exact}），{len(FAM)} 个族')
    print(f'golden：实测 {n_meas} 个样本 + 文档 / 构造 {len(gold_rows)} 个样本，全部通过')
    print(f'基线核对：通过；pkgmap 回放：{n_replay} 项一致')
    if missing: print('03 中缺少生成块标记：' + '、'.join(sorted(missing)))
    if '--write' in sys.argv:
        if new != text: open(DOC03, 'w').write(new)
        print('已回写 03：' + ('、'.join(sorted(changed)) or '无变化'))
    else:
        print('03 中过期的生成块：' + ('、'.join(sorted(changed)) or '无'))
        if '--check' in sys.argv and (changed or missing): sys.exit(1)
