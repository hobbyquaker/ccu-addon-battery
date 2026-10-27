import sys, re, logging, json
logging.disable(logging.CRITICAL)
import pypdf
def text(path):
    r = pypdf.PdfReader(path)
    return re.sub(r"\s+", " ", "\n".join((p.extract_text() or "") for p in r.pages)), len(r.pages)
out = {}
for path in sys.argv[1:]:
    try: t, n = text(path)
    except Exception as e: out[path] = {"err": str(e)}; continue
    hits = []
    for pat in [r"(?:Versorgungsspannung|Spannungsversorgung|Supply voltage|Power supply|Batterien?)\s*:\s*[^:]{0,70}",
                r"\d\s?[x×]\s?(?:1[,.]5|3|1[,.]2|9)\s?V[^:]{0,40}",
                r"(?:LR03|LR6|LR06|LR14|LR20|LR44|CR2032|CR2016|CR2450|CR123A?|6LR61|Micro|Mignon|Baby)\b[^:]{0,30}"]:
        for m in re.finditer(pat, t, re.I):
            h = m.group(0).strip()
            if h not in hits: hits.append(h)
    short = re.findall(r"(?:Kurzbezeichnung|short description)\s*:\s*([A-Za-z0-9 ,/-]{3,60}?)\s+(?:Versorg|Supply|Spann|Batter|Strom)", t, re.I)
    out[path.rsplit("/",1)[1]] = {"pages": n, "chars": len(t), "short": sorted(set(short))[:4], "hits": hits[:14]}
json.dump(out, sys.stdout, ensure_ascii=False, indent=1)
