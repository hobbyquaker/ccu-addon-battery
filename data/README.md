# batteries.json — which battery a Homematic device takes

The battery table of ccu-addon-battery: per device model the power source, the battery slots (count and type), the
voltage datapoint the addon watches, the device's own `LOW_BAT_LIMIT` default, the typical battery life the
manufacturer states, and the sources every row stands on. The addon embeds this file in its binary; the scripts that
crawled and assembled it are in [`../tools/`](../tools/README.md).

The data is **CC0-1.0** (public domain dedication, see *Licence* below): copy it into Home Assistant, ioBroker,
hm-simulator or a handbook without asking.

## Coverage (2026-10-27)

459 models: 206 with batteries (198 `battery`, 5 `rechargeable`, 3 `battery-or-dc`), 198 `mains`, 54 `bus`, 1
`host`. Of the 206 battery models, **125 stand on the manufacturer** (eQ-3's product pages or manuals, the supply line
quoted), 31 on two agreeing prior-art tables, 36 on one, 14 are `assumed` (only the unlicensed ioBroker script). Of
the mains/bus/host rows, 117 are from the manufacturer and 136 are `assumed` by model class (HM-LC-*, HMW-*, HmIPW-*).
All 30 of the most common models are manufacturer-sourced except HM-Sec-MDIR/-2, HM-RC-4-2 and
the first-generation HMIP-eTRV, HMIP-WTH and HmIP-SWDM (prior art agrees on all of them; their successors' manuals
say the same). 27 models were compared with their `LOW_BAT_LIMIT` default; one does not fit (HmIP-ASIR, below).

**Mismatches found** (in the rows' `note`): prior art is wrong for HM-WDS30-T-O (openHAB: 2× AA; manual: 2× LR03),
HmIP-FCI6 (aiohomematic: 1× AAA; eQ-3: 2×), HmIP-MP3P (aiohomematic: 2× C; eQ-3: 3×), HmIP-SLO and HmIP-SWD
(ioBroker: AAA/AA swapped), HM-OU-CFM-Pl (ioBroker: 2× C; it is a 230 V plug — the battery one is HM-OU-CFM-TW),
HM-WDC7000 (openHAB: 3× AA; manual: 4× AA as back-up to the mains adapter), HmIP-UDI-SMI55 (aiohomematic: 2× AAA;
eQ-3: 12 V DC), HM-LC-Sw4-WM and HM-LC-Sw1-Ba-PCB (ioBroker: 9 V block; manual: 7–15 V DC resp. any 5–15 V
battery). **HmIP-STE2-PCB** is 12 V DC/AC in ELV's manual while Battery Notes and aiohomematic say 2× AA — to be
checked on a device. **HmIP-ASIR** takes 3× AA per its manual, but its `LOW_BAT_LIMIT` default is 2.2 V, a two-cell
value (`limit_note`; check `OPERATING_VOLTAGE` with fresh cells).

## Files

| File | What |
| --- | --- |
| `batteries.json` | the table |
| `batteries.schema.json` | its JSON schema (draft 2020-12) |
| `check.py` | the checker (CI runs it), stdlib only: `python3 check.py [batteries.json] [--corpus <paramset-descriptions.json>] [--strict]` |

`check.py` validates the schema (a small built-in subset of JSON schema, no `jsonschema` package), then the table's
own rules: every slot type and chemistry is known, every row cites at least one source and every cited source is in
`sources`, a `manufacturer` row has a manufacturer source with the quoted supply line, a `prior-art` row has two
prior-art sources, no two models (or aliases) are equal case-insensitively, and a battery row's `low_bat_limit` fits
its cell count. With `--corpus` (hm-simulator's `data/paramset-descriptions.json`) it compares each model's cell
count with the `LOW_BAT_LIMIT` default the device itself declares and lists corpus types with a battery datapoint
that have no row. Exit 1 on an error; warnings fail only with `--strict`.

## Fields

```json
"HMIP-SWDO": {
  "power": "battery",
  "family": "HmIP-RF",
  "slots": [{"count": 1, "type": "AAA"}],
  "voltage": {"dp": "0/OPERATING_VOLTAGE", "low": 1.1},
  "low_bat_limit": 1.1,
  "life_years": 2,
  "confidence": "manufacturer",
  "sources": [
    {"id": "elv-manual", "url": "https://media.elv.com/file/140733_um.pdf", "quote": "1x 1,5 V LR03/Micro/AAA"},
    {"id": "battery-notes", "says": "1x AAA"},
    {"id": "corpus", "says": "LOW_BAT, OPERATING_VOLTAGE; LOW_BAT_LIMIT 1.1"}
  ]
}
```

- **Key:** the model as the interface reports it in `TYPE` (`listDevices`). Where hm-simulator's corpus has the
  type, its spelling is used (`HMIP-SWDO`, `HMIP-eTRV`, `HmIP-SMI` — the first HmIP generation reports `HMIP-`);
  otherwise eQ-3's spelling. **Look models up case-insensitively**: homematic-manager's data, ioBroker and the
  CCU's WebUI use other casings of the same names. `aliases` lists other names of the same hardware.
- **`power`:** `battery` (replaceable cells, or `fixed` cells), `rechargeable` (a pack or cells the device charges
  itself — solar, USB, or mains with a rechargeable back-up: HmIP-ASIR-O, HM-Sec-Win, HM-Sec-SFA-SM, HmIP-WSS),
  `battery-or-dc` (primary cells or an external DC supply: HmIP-WGC; HM-WDC7000's back-up cells; HM-LC-Sw1-Ba-PCB,
  which takes any 5–15 V battery and has no defined slot), `mains` (230 V or a plug-in power supply), `bus`
  (HmIP-Wired / BidCos-Wired, 24 V from the bus), `host` (powered by the drive it is built into, e.g. HmIP-MOD-HO).
  `battery` and `rechargeable` always carry `slots`; `battery-or-dc` does where the cells are defined.
- **`slots`:** `count` × `type`. Types are the keys of `types`: `AA`, `AAA`, `C`, `D`, `9V`, `LR44`, `CR2016`,
  `CR2032`, `CR2450`, `CR123A`, `fixed` (built in, not replaceable), `pack` (a device-specific rechargeable pack),
  `lead12V`. A slot may override `chemistry` (e.g. `nimh` where the manufacturer ships rechargeables) and carries
  `replaceable: false` for `fixed`.
- **`voltage`:** the datapoint that carries the battery voltage — `0/OPERATING_VOLTAGE` (HmIP), `4/BATTERY_STATE`
  (HM-CC-RT-DN), `2/BATTERY_STATE` (HM-TC-IT-WM-W-EU) — or `null` when the model reports only a low-battery flag
  (most BidCos sensors, HmIP-SWSD). Absent = not known (no corpus entry). `low` is the device's `LOW_BAT_LIMIT`
  default; `fresh` is set only where it differs from the computed default (below).
- **`low_bat_limit`:** the `LOW_BAT_LIMIT` default from the device's `MASTER` description (hm-simulator corpus).
  `limit_note` explains a limit that does not fit the cell count (check.py then warns instead of failing).
- **`life_years`:** the typical battery life the manufacturer states (informational; the addon forecasts from the
  installation's own history).
- **`outdoor`:** true for outdoor models: the jump rule ([docs/detection.md](../docs/detection.md)) adds 0.1 V to the step there (temperature
  recovery).
- **`confidence`:** `manufacturer` (the supply line was read on eQ-3's product page, manual or datasheet and is
  quoted), `prior-art` (two licensed prior-art tables agree; not yet checked against a manual), `single-source`
  (one licensed table), `assumed` (no licensed source, or a classification by name; check before relying on it).
- **`sources`:** `id` into the top-level `sources`, plus `url`, `quote` (the manufacturer's words) or `says` (what
  a prior-art table or the corpus says). Prior-art citations stay on manufacturer rows as a cross-check; where one
  disagrees with the manual, `note` says so.

**Levels for the detection rules.** `chemistries` holds per-cell `fresh`, `low` and `step` (rule B's jump) values;
a row's pack levels are `count × per-cell` of its slot's chemistry (default: the type's), and the device's own
`low` (its `LOW_BAT_LIMIT`) wins over the computed one. 2× AA alkaline: fresh 2.96 V, low 2.2 V, step 0.24 V. A per-device user override (rechargeables, a DS55 desk stand) changes the chemistry or the
slots, not this table.

**Shape notes:** `sources` is a list of citations instead of one string (a row
can stand on the manual and still record what prior art says); `confidence` is new (uncertain rows are marked, not
guessed); `power` replaces the special case `{"power": "mains"}` and adds `bus`, `host`, `rechargeable`,
`battery-or-dc`; the fresh/low levels moved to per-chemistry defaults (`chemistries`) with the device's own
`LOW_BAT_LIMIT` as `low`, so a row only names what differs; `low_bat_limit`, `family` and `life_years` are new;
`outdoor` moved from the voltage object to the row; `LR44` and `CR2016` joined the types (HM-Sec-SC/RHS, the old
BidCos remotes).

## Sources and licences

| Source | Covers | Licence | Used as |
| --- | --- | --- | --- |
| homematic-ip.com product pages (eQ-3) | current HmIP, HmIP-Wired and ELV-SH products; a structured "Versorgungsspannung" field and the typical battery life | the manufacturer's facts | primary source (`eq3-site`), the supply line quoted |
| eQ-3 manuals (`eq-3.de/…/bda/<model>_UM_GE_eQ-3_web.pdf`) and ELV-hosted manuals/datasheets (`media.elv.com/file/<article>_um.pdf`) | discontinued HmIP and BidCos models | the manufacturer's facts | primary source (`eq3-manual`, `elv-manual`), the supply line quoted |
| [Home Assistant Battery Notes](https://github.com/andrew-codechimp/HA-Battery-Notes) `library/library.json` | ~130 Homematic models (manufacturer eQ-3) | MIT | cross-check; a row's source where no manual was found (with its citation) |
| [aiohomematic](https://github.com/SukramJ/aiohomematic) `operating_voltage_level.py` | 85 models (prefix-matched) | MIT | cross-check; source as above |
| [openHAB Homematic binding](https://github.com/openhab/openhab-addons/tree/main/bundles/org.openhab.binding.homematic) `batteries.properties` | ~110 models incl. Roto (ZEL STG RM) and MAX! | EPL-2.0 | cross-check; source as above |
| [ioBroker Servicemeldungen-Homematic](https://github.com/Baenker/Servicemeldungen-Homematic) (and its copy in Uhula's MduiLogHM) | ~150 models incl. mains and 9 V | no licence stated | cross-check only — never a row's only licensed source |
| hm-simulator's corpus | `LOW_BAT_LIMIT` defaults, which battery datapoints a type has | MIT | the consistency check and the `voltage` datapoint |
| homematic-manager's device data (openccu-data) | the model names | names only (the data is HMSL 2.0; no text is copied) | the model list, the `classification` of mains/bus actuators |

**What this means for CC0.** Which battery a device takes is a fact; facts are not copyrightable, and the rows
copy no expression from any source beyond the manufacturer's one-line supply statement. The rows marked
`manufacturer` stand on the manufacturer's data alone and are CC0 without reservation. Rows whose only sources are
prior-art tables (`prior-art`, `single-source`) say so in their `sources`; that citation is the attribution the MIT
and EPL tables would ask for if a fact table were protected at all (in the EU a database right may apply to a
substantial extraction; we take single facts, cross-check them, and re-derive them from the manuals over time).
Replacing a prior-art row with a manual-sourced one is the preferred contribution.

## Adding or fixing a model

1. Find the manufacturer's statement: the product page on homematic-ip.com ("Technische Daten →
   Versorgungsspannung"), the manual at `https://www.eq-3.de/Downloads/eq3/downloads_produktkatalog/<homematic|homematic_ip>/bda/<MODEL>_UM_GE_eQ-3_web.pdf`,
   or the ELV product page's "Downloads" (manual `_um.pdf`, datasheet `_data.pdf`).
2. Add the row under the model's `TYPE` as the CCU reports it, with `confidence: "manufacturer"` and a source
   `{"id": "eq3-manual", "url": "…", "quote": "2x 1,5 V LR03/Micro/AAA"}`. No manual found? Cite the prior-art table
   you took it from and use `prior-art`/`single-source`; never invent a source.
3. If you own the device: the `LOW_BAT_LIMIT` default (`getParamsetDescription(<addr>:0, MASTER)`) and which of
   `OPERATING_VOLTAGE`/`BATTERY_STATE`/`LOWBAT`/`LOW_BAT` it has go into `low_bat_limit` and `voltage`.
4. Run `python3 check.py` (with `--corpus` if you have hm-simulator next to it) and send the pull request.

The addon's *Unknown models* list prefills such an issue with the model, its `LOW_BAT_LIMIT` and the fresh voltage
seen.

## Licence

To the extent possible under law, the contributors have waived all copyright and related or neighbouring rights to
`batteries.json` and `batteries.schema.json` ([CC0 1.0 Universal](https://creativecommons.org/publicdomain/zero/1.0/)).
`check.py` is under the addon's licence.
