# tools — how data/batteries.json was built

The scripts that crawled the manufacturer's data and the prior-art tables and assembled
[`data/batteries.json`](../data/README.md). They are kept so the table is reproducible; adding or
fixing one model is done by hand in the JSON (data/README.md, *Adding or fixing a model*), not by
re-running the crawl. Python 3, stdlib only, except `pdfbat.py`/`pdftech.py` (`pypdf`). Every
script takes a **work directory** `<dir>` as its first argument, where it caches what it fetched
and writes what it extracted; nothing is written into the repository. Run them with `python3 -I`.

| Script | Reads | Writes into `<dir>` |
| --- | --- | --- |
| `parse_hmip.py <html dir> <out.json>` | the saved homematic-ip.com product pages | `hmip-com.json`: the structured fields per product (`field_product_name_short`, `field_supply_voltage`, `field_typical_battery_life`) |
| `ipdocs.py <dir>` | `html/dl.html` (the downloads page), `hmip-com.json` | `ippdf/`: manuals and datasheets of the HmIP models, `ippdf/ipdocs.json` |
| `eq3probe.py <dir> <homematic\|homematic_ip> <model…>` | eq-3.de's manual URLs | `eq3pdf/<model>.pdf`, `eq3pdf/probe.json` |
| `elv.py <dir> <model…>` | de.elv.com's search and product pages | `elv/pages/`, `elv/pdf/`, `elv/elv.json` |
| `pdfbat.py <pdf…>`, `pdftech.py <pdf…>` | manuals | the battery / technical-data lines, printed for review |
| `manual_src.py <dir>` | the hand-reviewed supply lines in the script itself | `manual.json` |
| `prior.py <dir>` | `dl-bn/library.json` (Battery Notes), `dl-prior/aio-ovl.py` (aiohomematic), `dl-prior/oh-batteries.properties` (openHAB), `dl-prior/baenker.js` (ioBroker) | `prior.json`, `prior-names.json` |
| `build.py <dir> <out.json>` | everything above, `hmm-models.json` (the model names), the paramset corpus (`$CORPUS`, default `../hm-simulator/data/paramset-descriptions.json`) | the table |

Then `python3 data/check.py <out.json> --corpus <paramset-descriptions.json>` checks the result.
The prior-art sources and their licences are listed in data/README.md, *Sources and licences*.
