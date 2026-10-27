import sys, re, os, json, time, html, urllib.request, urllib.parse
S = sys.argv[1]; models = sys.argv[2:]
UA = {"User-Agent": "Mozilla/5.0"}
def get(url, path):
    if os.path.exists(path) and os.path.getsize(path) > 0: return open(path, "rb").read()
    try:
        data = urllib.request.urlopen(urllib.request.Request(url, headers=UA), timeout=40).read()
    except Exception as e:
        return None
    open(path, "wb").write(data); time.sleep(0.5); return data
os.makedirs(f"{S}/elv/pages", exist_ok=True); os.makedirs(f"{S}/elv/pdf", exist_ok=True)
res = json.load(open(f"{S}/elv/elv.json")) if os.path.exists(f"{S}/elv/elv.json") else {}
for m in models:
    slug = m.lower()
    q = get("https://de.elv.com/search?q=" + urllib.parse.quote(m), f"{S}/elv/pages/search-{slug}.html")
    if not q: res[m] = {"err": "search"}; continue
    links = sorted(set(re.findall(r'href="(/p/[^"]+-P(\d{5,6})/\?itemId=\d+)"', q.decode("utf-8", "replace"))))
    pat = re.compile(r"-" + re.escape(slug) + r"(-(?:fuer|für|optisch|weiss|schwarz|anthrazit|silber|innen|aussen|smart|zur|mit|f|homematic|hausautomation|funk|p\d)|-P\d)", re.I)
    cand = [(l, n) for l, n in links if pat.search(l) and "set" not in l.split("/p/")[1][:30] and "bausatz" not in l and "arr" not in l]
    entry = {"candidates": [l for l, _ in cand][:4], "pdfs": []}
    for l, n in cand[:2]:
        p = get("https://de.elv.com" + l, f"{S}/elv/pages/p-{n}.html")
        if not p: continue
        for u in sorted(set(re.findall(r'https://media\.elv\.com/file/[^"\s\\]+?\.pdf', p.decode("utf-8", "replace")))):
            fn = u.rsplit("/", 1)[1]
            if not re.search(r"_(um|data|ba|bda)\b|_(um|data)\.pdf|datenblatt|bedienungsanleitung", fn, re.I): continue
            if get(u, f"{S}/elv/pdf/{fn}"): entry["pdfs"].append(u)
    res[m] = entry
    print(m, len(cand), len(entry["pdfs"]), flush=True)
json.dump(res, open(f"{S}/elv/elv.json", "w"), indent=1)
