#!/usr/bin/env python3
"""Check batteries.json: the schema, the sources, and the cell counts against LOW_BAT_LIMIT.

Stdlib only. Usage:

    python3 check.py [batteries.json] [--corpus paramset-descriptions.json] [--strict]

--corpus takes hm-simulator's data/paramset-descriptions.json (or any file of the same shape:
"<iface>/<TYPE>/<fw>/<ver>/<channel type>/<paramset>" -> paramset description) and compares
every model's cell count with the LOW_BAT_LIMIT default the device itself declares.
Exit code 0 = clean, 1 = errors (warnings never fail unless --strict).
"""
import json
import os
import re
import sys

HERE = os.path.dirname(os.path.abspath(__file__))

# The ratio LOW_BAT_LIMIT / nominal pack voltage. Seen in the corpus: 0.70 (RT-DN 2.1/3.0) .. 0.87 (WGC 2.6/3.0,
# MP3P 3.9/4.5, SFA-SM 10.5/12). One cell too few or too many lands far outside (2.2 V on one 1.5 V cell = 1.47;
# 1.1 V on two = 0.37), so the band catches a wrong count without knowing the chemistry.
RATIO_MIN, RATIO_MAX = 0.60, 0.92

errors, warnings = [], []


def err(msg):
    errors.append(msg)


def warn(msg):
    warnings.append(msg)


# --- a small JSON-schema subset: type, const, enum, required, properties, additionalProperties,
# propertyNames, pattern, minimum/maximum/exclusiveMinimum, items, minItems, maxItems, $ref (local) ---

TYPES = {
    "object": dict, "array": list, "string": str, "boolean": bool,
    "integer": int, "number": (int, float), "null": type(None),
}


def is_type(v, t):
    if t in ("integer", "number") and isinstance(v, bool):
        return False
    return isinstance(v, TYPES[t])


def validate(v, s, root, path):
    if "$ref" in s:
        ref = s["$ref"]
        if not ref.startswith("#/"):
            err(f"{path}: unsupported $ref {ref}")
            return
        node = root
        for part in ref[2:].split("/"):
            node = node[part]
        validate(v, node, root, path)
        return
    if "type" in s:
        ts = s["type"] if isinstance(s["type"], list) else [s["type"]]
        if not any(is_type(v, t) for t in ts):
            err(f"{path}: expected {'/'.join(ts)}, got {type(v).__name__}")
            return
    if "const" in s and v != s["const"]:
        err(f"{path}: must be {s['const']!r}")
    if "enum" in s and v not in s["enum"]:
        err(f"{path}: {v!r} not one of {s['enum']}")
    if isinstance(v, str) and "pattern" in s and not re.search(s["pattern"], v):
        err(f"{path}: {v!r} does not match {s['pattern']}")
    if isinstance(v, (int, float)) and not isinstance(v, bool):
        if "minimum" in s and v < s["minimum"]:
            err(f"{path}: {v} < {s['minimum']}")
        if "maximum" in s and v > s["maximum"]:
            err(f"{path}: {v} > {s['maximum']}")
        if "exclusiveMinimum" in s and v <= s["exclusiveMinimum"]:
            err(f"{path}: {v} <= {s['exclusiveMinimum']}")
    if isinstance(v, list):
        if "minItems" in s and len(v) < s["minItems"]:
            err(f"{path}: fewer than {s['minItems']} items")
        if "maxItems" in s and len(v) > s["maxItems"]:
            err(f"{path}: more than {s['maxItems']} items")
        if "items" in s:
            for i, x in enumerate(v):
                validate(x, s["items"], root, f"{path}[{i}]")
    if isinstance(v, dict):
        for k in s.get("required", []):
            if k not in v:
                err(f"{path}: missing {k!r}")
        props = s.get("properties", {})
        for k, x in v.items():
            if "propertyNames" in s:
                validate(k, s["propertyNames"], root, f"{path}.<key {k}>")
            if k in props:
                validate(x, props[k], root, f"{path}.{k}")
            elif s.get("additionalProperties") is False:
                err(f"{path}: unknown field {k!r}")
            elif isinstance(s.get("additionalProperties"), dict):
                validate(x, s["additionalProperties"], root, f"{path}.{k}")


# --- the table's own rules ---

BATTERY_POWER = {"battery", "rechargeable", "battery-or-dc"}


def check_table(data):
    sources, types, chems, models = data["sources"], data["types"], data["chemistries"], data["models"]
    for name, t in types.items():
        if t["chemistry"] not in chems:
            err(f"types.{name}: chemistry {t['chemistry']!r} not in chemistries")
    seen = {}
    for key, m in models.items():
        names = [key] + m.get("aliases", [])
        for n in names:
            u = n.upper()
            if u in seen and seen[u] != key:
                err(f"models.{key}: {n!r} collides (case-insensitively) with models.{seen[u]}")
            seen[u] = key
        where = f"models.{key}"
        power = m["power"]
        slots = m.get("slots")
        if power in ("battery", "rechargeable") and not slots:
            err(f"{where}: power {power!r} needs slots")
        if power in ("mains", "bus", "host") and slots:
            err(f"{where}: power {power!r} must not have slots")
        for i, sl in enumerate(slots or []):
            if sl["type"] not in types:
                err(f"{where}.slots[{i}]: unknown type {sl['type']!r} (known: {', '.join(sorted(types))})")
                continue
            ch = sl.get("chemistry", types[sl["type"]]["chemistry"])
            if ch not in chems:
                err(f"{where}.slots[{i}]: unknown chemistry {ch!r}")
        if not m["sources"]:
            err(f"{where}: no source")
        kinds = []
        for i, c in enumerate(m["sources"]):
            if c["id"] not in sources:
                err(f"{where}.sources[{i}]: unknown source id {c['id']!r}")
                continue
            kinds.append(sources[c["id"]]["kind"])
        conf = m["confidence"]
        if conf == "manufacturer" and "manufacturer" not in kinds and "lab" not in kinds:
            err(f"{where}: confidence 'manufacturer' without a manufacturer source")
        if conf == "prior-art" and kinds.count("prior-art") < 2:
            err(f"{where}: confidence 'prior-art' needs two prior-art sources, has {kinds.count('prior-art')}")
        if conf == "manufacturer":
            for c in m["sources"]:
                if c["id"] in sources and sources[c["id"]]["kind"] == "manufacturer" and not c.get("quote"):
                    warn(f"{where}: manufacturer source {c['id']} without the quoted supply line")
        v = m.get("voltage")
        if v and power not in BATTERY_POWER:
            warn(f"{where}: voltage profile on a {power} model")
        if v and "low" in v and "fresh" in v and v["low"] >= v["fresh"]:
            err(f"{where}: voltage.low >= voltage.fresh")
        lbl = m.get("low_bat_limit")
        if lbl is not None and slots:
            check_ratio(where, lbl, slots, types, "low_bat_limit", m.get("limit_note"))


def pack_voltage(slots, types):
    return sum(sl["count"] * types[sl["type"]]["nominal_v"] for sl in slots if sl["type"] in types)


def check_ratio(where, limit, slots, types, what, acked=None):
    if any(not types.get(sl["type"], {}).get("replaceable", True) for sl in slots):
        return
    nominal = pack_voltage(slots, types)
    if nominal <= 0:
        return
    r = limit / nominal
    if not RATIO_MIN <= r <= RATIO_MAX:
        desc = " + ".join(f"{sl['count']}x {sl['type']}" for sl in slots)
        msg = (f"{where}: {what} {limit} V does not fit {desc} ({nominal:.1f} V nominal, ratio {r:.2f}, "
               f"expected {RATIO_MIN}-{RATIO_MAX})")
        if acked:
            warn(msg + f" - acknowledged: {acked}")
        else:
            err(msg)


# --- the corpus: LOW_BAT_LIMIT defaults and which battery datapoints a type has ---

def load_corpus(path):
    raw = json.load(open(path))
    out = {}
    for k, ps in raw.items():
        parts = k.split("/")
        if len(parts) < 6:
            continue
        iface, typ, chtype, pskey = parts[0], parts[1], parts[-2], parts[-1]
        if iface not in ("HmIP-RF", "BidCos-RF", "BidCos-Wired"):
            continue
        e = out.setdefault(typ, {"iface": iface, "limit": None, "dps": set()})
        if pskey == "MASTER" and "LOW_BAT_LIMIT" in ps:
            e["limit"] = ps["LOW_BAT_LIMIT"].get("DEFAULT")
        if pskey == "VALUES":
            for dp in ("OPERATING_VOLTAGE", "LOW_BAT", "LOWBAT", "BATTERY_STATE"):
                if dp in ps:
                    e["dps"].add(dp)
    return out


def check_corpus(data, corpus):
    types, models = data["types"], data["models"]
    index = {}
    for key, m in models.items():
        for n in [key] + m.get("aliases", []):
            index[n.upper()] = key
    compared = 0
    for typ, e in sorted(corpus.items()):
        key = index.get(typ.upper())
        battery_dp = e["dps"] & {"LOW_BAT", "BATTERY_STATE"} or (e["iface"] == "HmIP-RF" and e["limit"])
        if key is None:
            if battery_dp or e["limit"]:
                warn(f"corpus: {typ} ({e['iface']}) has a battery datapoint but no row")
            continue
        m = models[key]
        if e["limit"] is not None and m.get("slots"):
            compared += 1
            if m.get("low_bat_limit") is None:
                check_ratio(f"models.{key}", e["limit"], m["slots"], types, "corpus LOW_BAT_LIMIT default",
                            m.get("limit_note"))
            if m.get("low_bat_limit") is not None and abs(m["low_bat_limit"] - e["limit"]) > 1e-6:
                err(f"models.{key}: low_bat_limit {m['low_bat_limit']} differs from the corpus default {e['limit']}")
        if "OPERATING_VOLTAGE" in e["dps"] and m["power"] in BATTERY_POWER and not m.get("voltage"):
            warn(f"models.{key}: the corpus has OPERATING_VOLTAGE but the row has no voltage profile")
        if "BATTERY_STATE" in e["dps"] and not m.get("voltage"):
            warn(f"models.{key}: the corpus has BATTERY_STATE but the row has no voltage profile")
        if e["iface"] == "HmIP-RF" and "LOW_BAT" in e["dps"] and m["power"] in ("mains", "bus"):
            warn(f"models.{key}: row says {m['power']}, the corpus has LOW_BAT")
    return compared


def main(argv):
    args = [a for a in argv if not a.startswith("--")]
    strict = "--strict" in argv
    corpus_path = None
    if "--corpus" in argv:
        i = argv.index("--corpus")
        corpus_path = argv[i + 1]
        args.remove(corpus_path)
    path = args[0] if args else os.path.join(HERE, "batteries.json")
    schema_path = os.path.join(os.path.dirname(os.path.abspath(path)), "batteries.schema.json")
    data = json.load(open(path))
    schema = json.load(open(schema_path))
    validate(data, schema, schema, "$")
    if not errors:
        check_table(data)
    compared = 0
    if corpus_path and not errors:
        compared = check_corpus(data, load_corpus(corpus_path))
    models = data.get("models", {})
    by_conf = {}
    for m in models.values():
        by_conf[m.get("confidence")] = by_conf.get(m.get("confidence"), 0) + 1
    by_power = {}
    for m in models.values():
        by_power[m.get("power")] = by_power.get(m.get("power"), 0) + 1
    for w in warnings:
        print("warning:", w)
    for e in errors:
        print("error:", e)
    print(f"{len(models)} models; power {by_power}; confidence {by_conf}"
          + (f"; {compared} compared with the corpus LOW_BAT_LIMIT" if corpus_path else ""))
    print(f"{len(errors)} errors, {len(warnings)} warnings")
    return 1 if errors or (strict and warnings) else 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
