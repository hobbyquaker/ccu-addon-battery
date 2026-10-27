import json, re, sys, collections
S = sys.argv[1]
P = collections.defaultdict(dict)   # MODEL_UPPER -> {src: "2x AAA"}
NAMES = {}
def nm(m):
    NAMES.setdefault(m.upper().replace(" ", "_"), m)

TYPEMAP = {"AAA":"AAA","LR03":"AAA","R03":"AAA","MICRO":"AAA","AA":"AA","LR6":"AA","LR06":"AA","MIGNON":"AA","R6":"AA",
           "LR14":"C","BABY":"C","C":"C","LR44":"LR44","CR2032":"CR2032","CR2016":"CR2016","9V":"9V"}
def norm(t):
    t=t.upper()
    for k in ["LR03","LR06","LR6","LR14","LR44","CR2032","CR2016","AAA","AA","BABY","MIGNON","MICRO"]:
        if k in t: return TYPEMAP[k]
    return t
# Battery Notes
bn = json.load(open(f"{S}/dl-bn/library.json"))
for d in bn["devices"]:
    if not any(s in d["manufacturer"].lower() for s in ["eq-3","eq3","homematic"]): continue
    nm(d["model"]); m = d["model"].upper().replace(" ", "_")
    P[m]["bn"] = f'{d.get("battery_quantity",1)}x {norm(d["battery_type"])}' if norm(d["battery_type"]) in TYPEMAP.values() else d["battery_type"]
# aiohomematic
src = open(f"{S}/dl-prior/aio-ovl.py").read()
tm = {"R6":"AA","R03":"AAA","R14":"C","LR44":"LR44","CR2032":"CR2032","UNKNOWN":"UNKNOWN"}
for m,b,q in re.findall(r'_BatteryData\(model="([^"]+)", battery=_BatteryType\.(\w+)(?:, quantity=(\d))?\)', src):
    nm(m); P[m.upper()]["aio"] = f'{q or 1}x {tm[b]}' if b!="UNKNOWN" else "UNKNOWN"
# openHAB
for line in open(f"{S}/dl-prior/oh-batteries.properties"):
    line=line.strip()
    if not line or line.startswith("#") or "=" not in line: continue
    m,v = line.split("=",1)
    nm(m); mm = re.match(r"(\d)x (.*)", v)
    P[m.upper()]["oh"] = f"{mm.group(1)}x {norm(mm.group(2))}" if mm else v
# ioBroker Baenker
js = open(f"{S}/dl-prior/dl-baenker.js" if False else f"{S}/dl-prior/baenker.js").read()
lists = dict(re.findall(r"let (\w+)\s*=\s*\[([^\]]*)\];", js))
lab = {"cr2016":"1x CR2016","cr2032":"1x CR2032","lr14x2":"2x C","lr44x2":"2x LR44","lr6x2":"2x AA","lr6x3":"3x AA","lr6x4":"4x AA",
       "lr3x1":"1x AAA","lr3x2":"2x AAA","lr3x3":"3x AAA","lr3x3a":"3x AAA(NiMH)","lr14x3":"3x C","block9":"9V","fixed":"fixed","ohne":"mains","recharge":"pack"}
for k,v in lists.items():
    if k not in lab: continue
    for m in re.findall(r"'([^']+)'", v): nm(m); P[m.upper()]["iob"] = lab[k]
json.dump(P, open(f"{S}/prior.json","w"), indent=0, sort_keys=True)
json.dump(NAMES, open(f"{S}/prior-names.json","w"), indent=0, sort_keys=True)
print(len(P))
