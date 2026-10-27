import sys, os, json, time, urllib.request
S = sys.argv[1]; fam = sys.argv[2]; models = sys.argv[3:]
UA = {"User-Agent": "Mozilla/5.0"}
bases = ["https://www.eq-3.de/Downloads/eq3/downloads_produktkatalog/%s/bda/" % fam,
         "https://www.eq-3.de/downloads/download/%s/bda/" % fam]
res = json.load(open(f"{S}/eq3pdf/probe.json")) if os.path.exists(f"{S}/eq3pdf/probe.json") else {}
for m in models:
    if res.get(m, {}).get("url") or res.get(m, {}).get("tried2"): continue
    found = None
    for name in [m, m.lower()]:
        for suf in ["_um_eq-3_ge_web.pdf", "_UM_GE_eQ-3_web.pdf", "_um_ge_eq-3_web.pdf", "_um_ge_web.pdf", "_UM_GE_web.pdf", "_UM_GE_eQ-3.pdf"]:
            for b in bases:
                u = b + name + suf
                try:
                    r = urllib.request.urlopen(urllib.request.Request(u, headers=UA), timeout=30)
                    data = r.read()
                    if data[:4] == b"%PDF":
                        found = u; open(f"{S}/eq3pdf/{m}.pdf", "wb").write(data); break
                except Exception:
                    pass
                time.sleep(0.15)
            if found: break
        if found: break
    res[m] = {"url": found, "tried2": True}
    print(m, found, flush=True)
json.dump(res, open(f"{S}/eq3pdf/probe.json", "w"), indent=1)
