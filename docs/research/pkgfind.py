import sys, re, importlib.util
spec = importlib.util.spec_from_file_location('rs', __import__('os').path.join(__import__('os').path.dirname(__import__('os').path.abspath(__file__)), 'repo_scan.py')); rs = importlib.util.module_from_spec(spec); spec.loader.exec_module(rs)
key, pat = sys.argv[1], re.compile(sys.argv[2])
kind, spec2 = rs.TARGETS[key]
out = {}
if kind == 'rpm':
    for b in spec2:
        p, _ = rs.scan_rpm(b)
        for k, v in p.items():
            if pat.search(k) and (k not in out or rs.vercmp(v[0], out[k][0]) > 0): out[k] = v
else:
    mirror, dists, comps, arch = spec2
    for dd in dists:
        for k, v in rs.scan_deb(mirror, dd, comps, arch).items():
            if pat.search(k) and (k not in out or rs.vercmp(v[0], out[k][0]) > 0): out[k] = v
for k in sorted(out): print(k, out[k][0])
