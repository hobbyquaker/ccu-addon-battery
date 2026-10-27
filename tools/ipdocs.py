import re, sys, json, html, os, time, urllib.request
S = sys.argv[1]
t = html.unescape(open(f"{S}/html/dl.html", encoding="utf-8").read())
art, docs = {}, {}
for r in re.split(r'<div class="views-row">', t)[1:]:
    m = re.search(r'<a href="([^"]+)"[^>]*>([^<]+)</a>', r); d = re.search(r'Kurz-\w+:\s*([^|<]+)\|\s*Artikel-Nr\.:\s*([^<]+)', r)
    if not m or not d: continue
    models = [x.strip() for x in re.split(r"\s*[,/]\s*|\s+bzw\.\s+", d.group(1).strip()) if x.strip()]
    arts = re.findall(r"(\d{6})", d.group(2))
    for mo in models:
        art.setdefault(mo, set()).update(arts)
        if re.search(r"Bedienungsanleitung|Datenblatt|anleitung", m.group(2), re.I) and not re.search(r"Konformit", m.group(2)):
            docs.setdefault(mo, []).append("https://homematic-ip.com" + m.group(1))
H = json.load(open(f"{S}/hmip-com.json"))
official = {n.strip().upper() for r in H for n in (r.get("field_product_name_short") or "").split(" bzw. ") if r.get("field_supply_voltage")}
UA = {"User-Agent": "Mozilla/5.0"}
def get(u, p):
    if os.path.exists(p): return True
    try:
        d = urllib.request.urlopen(urllib.request.Request(u, headers=UA), timeout=40).read()
        if d[:4] != b"%PDF": return False
        open(p, "wb").write(d); time.sleep(0.3); return True
    except Exception: return False
res = {}
for mo in sorted(set(art) | set(docs)):
    if mo.upper() in official: continue
    if re.match(r"(EQ3|HmIP-ADA|HmIP-SK|HmIP-GF|HmIPW-BC|HmIP-BR[AU]|HmIP-SF|HmIP-CO|HmIP-CI|HAP1|CC-Sys|YHL)", mo): continue
    got = []
    for i, u in enumerate(docs.get(mo, [])):
        p = f"{S}/ippdf/{mo}__hip{i}.pdf"
        if get(u.replace(" ", "%20"), p): got.append((u, p))
    if not got:
        for a in sorted(art.get(mo, [])):
            for suf in ("_um.pdf", "_data.pdf"):
                u = f"https://media.elv.com/file/{int(a)}{suf}"
                p = f"{S}/ippdf/{mo}__elv{a}{suf.replace('.pdf','')}.pdf"
                if get(u, p): got.append((u, p))
    res[mo] = {"art": sorted(art.get(mo, [])), "docs": got}
    print(mo, sorted(art.get(mo, [])), len(got), flush=True)
json.dump(res, open(f"{S}/ippdf/ipdocs.json", "w"), indent=1)
