import sys, re, logging
logging.disable(logging.CRITICAL)
import pypdf
def norm(s): return re.sub(r"\s+", " ", s)
for path in sys.argv[1:]:
    try:
        r = pypdf.PdfReader(path)
        t = norm("\n".join((p.extract_text() or "") for p in r.pages))
    except Exception as e:
        print("==", path, "ERR", e); continue
    print("==", path.split("/")[-1], f"({len(r.pages)} p)")
    out = []
    for m in re.finditer(r"(?i)(Ger.?te-?\s?Kurzbezeichnung|Device short description|Supply voltage|Versorgungsspannung|Spannungsversorgung|Batterielebensdauer|Battery life)", t):
        out.append(t[m.start(): m.start()+110])
    seen=[]
    for o in out:
        if o[:60] not in [x[:60] for x in seen]: seen.append(o)
    for o in seen[:10]: print("  ..", o)
