"""Assemble batteries.json from the crawled manufacturer data, the manual extracts, prior art and the corpus.

The committed artefact is data/batteries.json; every row carries its sources. See tools/README.md.
Usage: python3 -I build.py <work dir> <out batteries.json>
"""
import json, os, re, sys, collections

S, OUT = sys.argv[1], sys.argv[2]
TODAY = "2026-10-27"

TYPES = {
    "AA":     {"iec": "LR6",  "names": ["Mignon", "R6"], "nominal_v": 1.5, "chemistry": "alkaline"},
    "AAA":    {"iec": "LR03", "names": ["Micro", "R03"], "nominal_v": 1.5, "chemistry": "alkaline"},
    "C":      {"iec": "LR14", "names": ["Baby", "R14"], "nominal_v": 1.5, "chemistry": "alkaline"},
    "D":      {"iec": "LR20", "names": ["Mono", "R20"], "nominal_v": 1.5, "chemistry": "alkaline"},
    "9V":     {"iec": "6LR61", "names": ["E-Block", "PP3"], "nominal_v": 9.0, "chemistry": "alkaline"},
    "LR44":   {"iec": "LR44", "names": ["AG13", "A76"], "nominal_v": 1.5, "chemistry": "alkaline-button"},
    "CR2016": {"iec": "CR2016", "nominal_v": 3.0, "chemistry": "lithium-coin"},
    "CR2032": {"iec": "CR2032", "nominal_v": 3.0, "chemistry": "lithium-coin"},
    "CR2450": {"iec": "CR2450", "nominal_v": 3.0, "chemistry": "lithium-coin"},
    "CR123A": {"iec": "CR17345", "nominal_v": 3.0, "chemistry": "lithium"},
    "fixed":  {"names": ["built in, not replaceable"], "nominal_v": 3.0, "chemistry": "lithium", "replaceable": False},
    "pack":   {"names": ["a device-specific rechargeable pack"], "nominal_v": 3.6, "chemistry": "nimh"},
    "lead12V": {"names": ["12 V lead-acid"], "nominal_v": 12.0, "chemistry": "lead"},
}
CHEMS = {
    "alkaline": {"fresh": 1.48, "low": 1.1, "step": 0.12, "note": "a fresh cell reads 1.5-1.6 V; 2 cells: fresh 2.95 V, step 0.25 V (the detection rules, docs/detection.md)"},
    "alkaline-button": {"fresh": 1.48, "low": 1.2, "step": 0.1, "note": "LR44: small capacity, flat curve until a late knee (assumed)"},
    "lithium-coin": {"fresh": 2.95, "low": 2.4, "step": 0.15, "note": "CR20xx: 3.0-3.2 V fresh, 2.4 V is HmIP's default limit (FCI1)"},
    "lithium": {"fresh": 2.95, "low": 2.5, "step": 0.15, "note": "3 V lithium (CR123A, fixed smoke-alarm cells)"},
    "lithium-aa": {"fresh": 1.7, "low": 1.2, "step": 0.1, "note": "Li-FeS2 AA/AAA (Energizer Ultimate): 1.7-1.8 V fresh; set per device by the user"},
    "nimh": {"fresh": 1.33, "low": 1.1, "step": 0.06, "note": "NiMH: 1.35-1.4 V fresh off the charger, flat curve; 2 cells: fresh 2.6 V, step 0.12 V"},
    "lead": {"fresh": 2.1, "low": 1.75, "step": 0.08, "note": "per cell of a 12 V lead pack (6 cells): 12.6 V full, HM-Sec-SFA-SM's limit 10.5 V"},
}
SOURCES = {
    "eq3-site": {"name": "homematic-ip.com product pages (eQ-3 AG), field 'Versorgungsspannung'", "url": "https://homematic-ip.com/de/produkte",
                 "license": "facts from the manufacturer's data; no text reproduced beyond the supply line", "kind": "manufacturer", "read": TODAY},
    "eq3-manual": {"name": "eQ-3 installation and operating manual (Technische Daten)", "url": "https://www.eq-3.de/service/downloads.html",
                   "license": "facts from the manufacturer's manual; the supply line is quoted", "kind": "manufacturer", "read": TODAY},
    "elv-manual": {"name": "eQ-3 manual or datasheet as hosted by ELV (media.elv.com)", "url": "https://de.elv.com",
                   "license": "facts from the manufacturer's manual; the supply line is quoted", "kind": "manufacturer", "read": TODAY},
    "battery-notes": {"name": "Home Assistant Battery Notes, library/library.json", "url": "https://github.com/andrew-codechimp/HA-Battery-Notes/blob/main/library/library.json",
                      "license": "MIT (Copyright (c) 2023-2025 Andrew Jackson)", "kind": "prior-art", "read": TODAY, "note": "commit ef1809cc, 2026-10-26"},
    "aiohomematic": {"name": "aiohomematic, aiohomematic/model/calculated/operating_voltage_level.py (_BATTERY_DATA)", "url": "https://github.com/SukramJ/aiohomematic/blob/main/aiohomematic/model/calculated/operating_voltage_level.py",
                     "license": "MIT", "kind": "prior-art", "read": TODAY, "note": "the table behind homematicip_local's operating-voltage-level sensor; prefix matching"},
    "openhab": {"name": "openHAB Homematic binding, src/main/resources/homematic/batteries.properties", "url": "https://github.com/openhab/openhab-addons/blob/main/bundles/org.openhab.binding.homematic/src/main/resources/homematic/batteries.properties",
                "license": "EPL-2.0", "kind": "prior-art", "read": TODAY},
    "iobroker-servicemeldungen": {"name": "ioBroker script Servicemeldungen-Homematic (Baenker), func_Batterie", "url": "https://github.com/Baenker/Servicemeldungen-Homematic/blob/master/Servicemeldungen.js",
                                  "license": "none stated (all rights reserved by default): used only as a cross-check, never as a row's only source", "kind": "prior-art", "read": TODAY},
    "corpus": {"name": "hm-simulator device corpus, data/paramset-descriptions.json (descriptions read from real devices)", "url": "https://github.com/hobbyquaker/hm-simulator",
               "license": "MIT", "kind": "corpus", "read": TODAY, "note": "LOW_BAT_LIMIT defaults and which battery datapoints a type has"},
    "classification": {"name": "device class by model name and description (actuators on 230 V, DIN-rail, plug-in, wired bus)", "url": "https://github.com/hobbyquaker/homematic-manager",
                       "license": "model names only", "kind": "corpus", "read": TODAY, "note": "homematic-manager's device data (openccu-data model names and descriptions)"},
}

def load(p): return json.load(open(p))

prior = load(f"{S}/prior.json")
pnames = load(f"{S}/prior-names.json")
hmip = load(f"{S}/hmip-com.json")
manual = load(f"{S}/manual.json")          # hand-reviewed extracts: model -> {slots, power, quote, url, src, life}
hmm = load(f"{S}/hmm-models.json")

# corpus
corp = {}
raw = load(os.environ.get("CORPUS", "../hm-simulator/data/paramset-descriptions.json"))
for k, ps in raw.items():
    p = k.split("/")
    iface, typ, chtype, psk = p[0], p[1], p[-2], p[-1]
    if iface not in ("HmIP-RF", "BidCos-RF", "BidCos-Wired"): continue
    e = corp.setdefault(typ, {"iface": iface, "limit": None, "dps": {}})
    if psk == "MASTER" and "LOW_BAT_LIMIT" in ps: e["limit"] = ps["LOW_BAT_LIMIT"].get("DEFAULT")
    if psk == "VALUES":
        ch = {"CLIMATECONTROL_RT_TRANSCEIVER": "4", "THERMALCONTROL_TRANSMIT": "2"}.get(chtype, "0")
        for dp in ("OPERATING_VOLTAGE", "LOW_BAT", "LOWBAT", "BATTERY_STATE"):
            if dp in ps: e["dps"].setdefault(dp, set()).add(ch)
corp_up = {t.upper(): t for t in corp}

def parse_supply(s):
    """'2x 1,5 V LR6/Mignon/AA' -> power, slots."""
    if not s: return None, None
    t = s.replace("×", "x")
    if re.search(r"fest verbaut", t):
        m = re.search(r"(\d)\s*x", t); return "battery", [{"count": int(m.group(1)) if m else 1, "type": "fixed", "replaceable": False}]
    if re.search(r"Akkupack|NiMH", t): return "rechargeable", [{"count": 1, "type": "pack", "note": t}]
    typ = None
    for pat, ty in [(r"LR03|Micro|AAA", "AAA"), (r"LR0?6|Mignon|\bAA\b", "AA"), (r"LR14|Baby", "C"), (r"LR44", "LR44"), (r"CR2032", "CR2032"), (r"CR2016", "CR2016"), (r"CR2450", "CR2450")]:
        if re.search(pat, t): typ = ty; break
    if typ:
        m = re.search(r"(\d)\s*x", t)
        power = "battery-or-dc" if re.search(r"oder\s+\d", t) else "battery"
        return power, [{"count": int(m.group(1)) if m else 1, "type": typ}]
    if re.search(r"SELV", t) and re.search(r"24 ?VDC", t): return "bus", None
    if re.search(r"bereitgestellt", t): return "host", None
    if re.search(r"\d\s*V", t): return "mains", None
    return None, None

models = {}
def key_for(name):
    u = name.upper()
    if u in corp_up: return corp_up[u]
    return name

def family(name):
    u = name.upper()
    if u.startswith("HMIPW-"): return "HmIP-Wired"
    if u.startswith("HMW-"): return "BidCos-Wired"
    if u.startswith(("HMIP-", "ELV-SH-", "ALPHA-IP")): return "HmIP-RF"
    return "BidCos-RF"

SKIP = re.compile(r"^(EQ3-|HmIP-ADA|HmIP-SK\d|HmIP-GF\d|HmIPW-BC|HmIP-BR[AU]$|HmIP-SF-2|HmIP-COR?|HmIP-COL|HmIP-CI-|HmIP-CODB|HmIP-HCU|HmIP-eTRV-C-TP|HmIP-DRA$|HmIP-BPS|HmIP-App|HmIP-DC$|HmIP-WLAN|HmIP-VDMOT|HmIP-FAL24-TR|HmIP-LS|CC-Sys|ESB1|HAP1|Amazon|Google|Doorbird|EZVIZ|Mediola|NIBE|Philips|Systemair|Modul )", re.I)

def cite_prior(u):
    c = []
    for sid, k in (("battery-notes", "bn"), ("aiohomematic", "aio"), ("openhab", "oh"), ("iobroker-servicemeldungen", "iob")):
        v = prior.get(u, {}).get(k)
        if v: c.append({"id": sid, "says": v})
    return c

def says(slots, power):
    if power in ("mains", "bus", "host"): return "mains" if power == "mains" else power
    return " + ".join(f"{s['count']}x {s['type']}" for s in slots)

def prior_norm(v):
    v = v.replace("(NiMH)", "")
    return {"Irreplaceable": "fixed", "FDK CR17450E-N (3V)": "fixed", "HM-Sec-Win-BaP": "pack", "Accu-Pack 10,8 V": "pack", "Rechargeable": "pack"}.get(v, v)

# 1. manufacturer site
for r in hmip:
    short = r.get("field_product_name_short") or ""
    if SKIP.match(short) or not short or " " in short.replace(" bzw. ", "|").split("|")[0]: continue
    power, slots = parse_supply(r.get("field_supply_voltage"))
    if power is None: continue
    for name in short.split(" bzw. "):
        name = name.strip()
        k = key_for(name)
        row = {"power": power, "family": family(name)}
        if slots: row["slots"] = slots
        life = r.get("field_typical_battery_life")
        if life and slots:
            m = re.match(r"(\d+(?:[,/]\d+)?)\s*Jahr", life)
            if m:
                v = m.group(1)
                row["life_years"] = 0.5 if v == "1/2" else float(v.replace(",", "."))
        row["sources"] = [{"id": "eq3-site", "url": f"https://homematic-ip.com/de/produkt/{r['page']}", "quote": r["field_supply_voltage"]}]
        row["confidence"] = "manufacturer"
        models[k] = row

# 2. manuals (hand-reviewed)
for name, mrow in manual.items():
    k = key_for(name)
    row = models.get(k, {"family": family(name)})
    row["power"] = mrow["power"]
    if mrow.get("slots"): row["slots"] = mrow["slots"]
    else: row.pop("slots", None)
    if mrow.get("life"): row["life_years"] = mrow["life"]
    src = row.get("sources", [])
    src.append({"id": mrow["src"], "url": mrow["url"], "quote": mrow["quote"]})
    row["sources"] = src
    row["confidence"] = "manufacturer"
    if mrow.get("note"): row["note"] = mrow["note"]
    models[k] = row

# 3. prior art for the rest
SKIP_PRIOR = re.compile(r"^(HM-CCU-1$|HM-OU-CF-PL$|MAX!|BC-|THERMOSTAT|EQ-3_BLUETOOTH|M-WDS40|ETRV-F$|HMIP-KRC$|HMIP-WRC$|HMIP-SWO$|HMIP-HEATING|HM-CC-VG-1|HB-|ZEL |HMIP-ETRV-2 I9F|HM-WDS30-TO$|HM-SEN-MDIR-O-2M)", re.I)
for u, v in prior.items():
    if SKIP_PRIOR.match(u): continue
    k = key_for(next((x for x in [u] if False), u))
    # find an existing row case-insensitively
    exist = next((kk for kk in models if kk.upper() == u), None)
    cites = cite_prior(u)
    if exist:
        # attach agreeing/disagreeing prior art as cross-check (no change of the row)
        models[exist].setdefault("_prior", cites)
        continue
    vals = [prior_norm(c["says"]) for c in cites if c["id"] != "iobroker-servicemeldungen" or True]
    counts = collections.Counter(prior_norm(c["says"]) for c in cites)
    licensed = [c for c in cites if c["id"] != "iobroker-servicemeldungen"]
    best, n = counts.most_common(1)[0]
    if best in ("UNKNOWN",):
        continue
    name = u
    # spelling: prefer battery-notes / aiohomematic / openhab spelling
    for sid in ("aiohomematic", "battery-notes", "openhab"):
        pass
    row = {"family": family(u)}
    if best == "mains":
        row["power"] = "mains"
    elif best == "fixed":
        row["power"] = "battery"; row["slots"] = [{"count": 1, "type": "fixed", "replaceable": False}]
    elif best == "pack":
        row["power"] = "rechargeable"; row["slots"] = [{"count": 1, "type": "pack"}]
    elif best == "9V":
        row["power"] = "battery"; row["slots"] = [{"count": 1, "type": "9V"}]
    else:
        m = re.match(r"(\d)x (\w+)", best)
        row["power"] = "battery"; row["slots"] = [{"count": int(m.group(1)), "type": m.group(2)}]
    row["sources"] = cites
    lic_agree = sum(1 for c in licensed if prior_norm(c["says"]) == best)
    if lic_agree >= 2 and len(counts) == 1: row["confidence"] = "prior-art"
    elif lic_agree >= 2: row["confidence"] = "prior-art"; row["note"] = "prior art disagrees: " + ", ".join(f"{c['id']} {c['says']}" for c in cites if prior_norm(c["says"]) != best)
    elif lic_agree == 1: row["confidence"] = "single-source"
    else: row["confidence"] = "assumed"; row["note"] = "only the unlicensed ioBroker script names it; re-derive from the manual"
    models[key_for(pnames.get(u, u).replace(" ", "_"))] = row


# 3b. classification by name for the remaining actuators (mains / wired bus), confidence "assumed"
MAINS_RX = re.compile(r"^(HM-LC-(?!SW1-BA-PCB|SW4-BA-PCB|SW4-PCB|SW4-WM)|HM-ES-PMSW|HM-SYS-SRP-PL|HM-DW-WM|HM-OU-CF-PL|HM-OU-CM-PCB$|HM-RC-2-PBU-FM-2|HMIP-(PS|PSM|PDT|BSM|FSM|BBL|BDT|BROLL|FROLL|FDT|DRS|DRSI|DRBLI|DRDI|FSI|FS6|PCBS2|PMFS|WHS2|BSL|BS2|MIOB|RGBW|SMO230|WGT|WGS|WGD|BWTH|FAL|USBSM|WUA|HAP|FBL|SCTH230|WRC6-230|BRC2|DRG-DALI|MOD-OC8|MOD-HO|MOD-TM|MOD-WD-VK|HDM|SRD|FWI|FLC|FDC|WSS|UDI|LSC|M-TD15|ESI-IND|WGTC|SWDM-B2))", re.I)
BUS_RX = re.compile(r"^(HMW-|HMIPW-|HBW-)", re.I)
VIRTUAL_RX = re.compile(r"^(HM-RCV-50|HMW-RCV-50|HMIP-RCV-50|HMIP-HEATING|HM-CC-VG-1|HMIP-CCU3|HM-CCU-1|HMIP-RFUSB|RPI-RF-MOD|CMM|DEVICE|HMIP-HAP|VIR-|CUX|RF-LAN|WIRED-LAN|HSS-DX|HM-SEC-XX|HM-SWI-X|HM-PBI-X|HM-RC-X|HM-SEN-X|HM-LC-SWX|HM-LC-BLX|HM-LC-JAX|HM-OU-X|HM-RC-SB-X|HM-ES-PMSWX|HMIPW-DRAP|HMIPW-DRPS|HMW-SYS-PS7-DR)", re.I)
have = {k.upper() for k in models}
names = {}
for n in list(hmm) + [t for t in corp]:
    names.setdefault(n.upper(), corp_up.get(n.upper(), n))
for u, n in sorted(names.items()):
    if u in have or VIRTUAL_RX.match(u): continue
    if BUS_RX.match(u):
        models[n] = {"power": "bus", "family": family(n), "sources": [{"id": "classification", "says": "wired bus device (24 V bus supply)"}], "confidence": "assumed"}
    elif MAINS_RX.match(u):
        models[n] = {"power": "mains", "family": family(n), "sources": [{"id": "classification", "says": "mains actuator by model and description: " + (hmm.get(u, {}).get("en") or "?")}], "confidence": "assumed"}
    have.add(u)

# 4. corpus: voltage datapoint, LOW_BAT_LIMIT default
for k, row in models.items():
    t = corp_up.get(k.upper())
    if not t: continue
    e = corp[t]
    if row["power"] in ("battery", "rechargeable", "battery-or-dc"):
        if "OPERATING_VOLTAGE" in e["dps"]: row["voltage"] = {"dp": "0/OPERATING_VOLTAGE"}
        elif "BATTERY_STATE" in e["dps"]:
            ch = sorted(e["dps"]["BATTERY_STATE"])[0]
            row["voltage"] = {"dp": f"{ch}/BATTERY_STATE"}
        else: row["voltage"] = None
    if e["limit"] is not None and row.get("slots"):
        row["low_bat_limit"] = e["limit"]
        if row.get("voltage"): row["voltage"]["low"] = e["limit"]
    row["sources"].append({"id": "corpus", "says": ", ".join(sorted(e["dps"])) + (f"; LOW_BAT_LIMIT {e['limit']}" if e["limit"] is not None else "")})

# extra rows (mains etc.) from manual.json's "_mains" handled in manual; finalize
OUTDOOR = re.compile(r"(-O(-\d)?$|-O-|SWO|STHO|SLO|SMO(?!230)|WDS10|WDS100|WDS20|KRCO|TACO|ASIR-O|SRD|SEN-RD|WA-OD|MDIR-O|LI-O)", re.I)
ALIASES = {"HM-CC-RT-DN": ["HM-CC-RT-DN-BoM"], "HM-Sec-SD": ["HM-Sec-SD-Team"], "HM-Sec-SD-2": ["HM-Sec-SD-2-Team"]}
LIMIT_NOTES = {
    "HmIP-ASIR": "the manual says 3x AA, the device's LOW_BAT_LIMIT default is 2.2 V (a two-cell value); unresolved - check OPERATING_VOLTAGE on a device with fresh cells",
}
final = {}
for k in sorted(models, key=str.upper):
    row = models[k]
    pri = row.pop("_prior", None)
    if pri:
        if row.get("slots"):
            sl = row["slots"]
            mine = "fixed" if sl[0]["type"] == "fixed" else "pack" if sl[0]["type"] == "pack" else says(sl, row["power"])
        else:
            mine = "mains" if row["power"] in ("mains", "bus", "host") else row["power"]
        bad = []
        for c in pri:
            row["sources"].append(c)
            if prior_norm(c["says"]).split("(")[0] not in (mine, "UNKNOWN"):
                bad.append(f"{c['id']} says {c['says']}")
        if bad:
            msg = "prior art disagrees with the manufacturer: " + "; ".join(bad)
            row["note"] = (row["note"] + ". " + msg) if row.get("note") else msg
    if row["power"] in ("battery", "rechargeable", "battery-or-dc") and OUTDOOR.search(k):
        row["outdoor"] = True
    if k in LIMIT_NOTES: row["limit_note"] = LIMIT_NOTES[k]
    for kk, al in ALIASES.items():
        if kk.upper() == k.upper(): row["aliases"] = al
    order = ["power", "family", "slots", "voltage", "low_bat_limit", "limit_note", "life_years", "outdoor", "aliases", "confidence", "note", "sources"]
    final[k] = {f: row[f] for f in order if f in row}

doc = {"$schema": "./batteries.schema.json", "version": 1, "license": "CC0-1.0", "updated": TODAY,
       "sources": SOURCES, "types": TYPES, "chemistries": CHEMS, "models": final}
with open(OUT, "w") as f:
    json.dump(doc, f, ensure_ascii=False, indent=1)
    f.write("\n")
print(len(final), collections.Counter(r["confidence"] for r in final.values()), collections.Counter(r["power"] for r in final.values()))
