import re, sys, html, json, os
rows = []
for f in sorted(os.listdir(sys.argv[1])):
    t = open(os.path.join(sys.argv[1], f), encoding="utf-8").read()
    fields = {}
    for m in re.finditer(r'<div\s+class="(field_[a-z0-9_]+) field has-label">\s*<div class="field-label"><span>(.*?)</span></div>(.*?)</div>\s*</div>', t, re.S):
        val = html.unescape(re.sub(r"<[^>]+>", " ", m.group(3)))
        fields[m.group(1)] = re.sub(r"\s+", " ", val).strip()
    rows.append({"page": f[:-5], **fields})
json.dump(rows, open(sys.argv[2], "w"), ensure_ascii=False, indent=1)
keys = {}
for r in rows:
    for k in r: keys[k] = keys.get(k, 0) + 1
print(keys)
