# The API

The addon serves its API under its own path, `/addons/battery/api/v1/`, behind the system's
session gate - the same gate as its page. [openapi.json](openapi.json) specifies every route
(served at `api/v1/openapi.json`); this page explains them and the rules behind them.

## Access

- **A browser** that shows the addon's page has the gate cookie already.
- **A script** logs in first and keeps the cookie:

```sh
SYS=http://<system>
curl -sc jar -H 'Content-Type: application/json' \
     -d '{"username":"admin","password":"…"}' $SYS/api/auth/v1/login
curl -sb jar $SYS/addons/battery/api/v1/devices
curl -sb jar -H 'X-CCU-Addon-Request: 1' -H 'Content-Type: text/csv' \
     --data-binary @changes.csv "$SYS/addons/battery/api/v1/changes:bulk?dry_run=true"
```

- **Every writing request** (POST, PUT, PATCH, DELETE) carries the header `X-CCU-Addon-Request`
  (any value): a page of another site cannot set it, so it cannot write in the user's name.
- Reading and writing need a session of level `operate` or above; moving the database needs an
  administrator. The system's API tokens (`olt_…`) do not open the gate.

**Times** are Unix milliseconds in every answer. Requests take milliseconds, an RFC 3339 string
(`2024-03-01T08:30:00+01:00`) or a date (`2024-03-01`, noon in the system's time zone). **Errors**
answer `{"error": "<code>", "message": "…"}`: 400 for a body that is not JSON for the route (unknown
fields are refused, so a typo does not pass silently), 404, 422 with a code (`invalid`,
`unknown-device`, `ambiguous-device`), 503 `unavailable` or `busy` (the database is being moved;
`Retry-After`).

## Devices and the battery table

| Route | Does |
| --- | --- |
| `GET devices` | every tracked device: interface, address, the name and rooms from the system, the model, the current voltage and flags, `profile` - power source, slots, the levels the detector uses, and where they came from (`table`, `override`, `guess`, `none`) -, `image` (the platform's picture of the model, `/config/img/devices/250/…`, as the WebUI's `DEVDB.tcl` names it; absent without one) and `life` (the device's battery lives: `count`, `min_days`, `max_days`, `avg_days`, `median_days` of the completed ones, `running_since`/`running_days` of the current one, `hint: short` for a low battery well before the device's usual life; absent without an accepted change) |
| `GET models` | the models of this installation with their table rows; `?unknown=true` lists the ones the table lacks |
| `GET models/{model}` | one model's row, override and profile; the lookup ignores case and knows the aliases |
| `PUT`/`DELETE models/{model}/override` | fix the table for a model locally: `{"slots": [{"count": 2, "type": "AA"}], "power": "battery"}` |
| `GET`/`PUT`/`DELETE devices/{interface}/{address}/override` | one device: `{"rechargeable": true}`, `{"chemistry": "lithium-aa"}`, `{"slots": [...]}` (a remote in a desk stand), `{"power": "external"}`, `{"outdoor": true}` |

A device override wins over a model override, which wins over the table.

## Changes

A change is one battery change of one device: `at`, the `batteries` that went in (`count`,
`type`, `chemistry`, `rechargeable`, `vendor`, `product`, `price_cents`, `lot`, per slot), a
`note`, and how it came. **`price_cents` is the price of one battery** of the slot (in cents of
the user's currency), so a slot's cost is `count × price_cents`; `vendor` is the maker ("Varta")
and `product` its product line ("Power on Demand"). How it came: `kind` `detected` (the detector), `confirmed` (detected, and a manual or
imported entry said the same), `manual` or `import`. Its `status`:

| Status | Meaning |
| --- | --- |
| `candidate` | detected, waiting for its confirmation window ([detection.md](detection.md)); not in the inbox |
| `pending` | in the inbox, waiting for the user |
| `accepted` | a change: confirmed by the user, or recorded by hand or by an import |
| `rejected` | the user said no |
| `discarded` | the detector's own no (a boot, a recovery); kept as evidence |

| Route | Does |
| --- | --- |
| `GET changes?device=&from=&to=&status=&limit=` | the list, newest first; `status` takes a comma-separated list (`pending` is the inbox) |
| `POST changes` | one change: `{"device": "<address>", "at": "2024-03-01", "batteries": [...], "note": "…", "source": "manual", "external_id": "…"}` → `201 {"status": "created", "id": …}` or `200` with `merged`/`duplicate` |
| `GET`/`PUT`/`PATCH`/`DELETE changes/{id}` | one change; PUT and PATCH take the fields to change: `{"status": "accepted"}` confirms an inbox entry, `"rejected"` rejects it, `{"at": …, "status": "accepted"}` confirms it with another date |
| `PUT`/`PATCH changes/by-external-id/{id}?source=import` | the same, for the change a client knows by its own id |
| `POST changes:bulk` | many changes, see below |

A device the addon has never seen (one that left the system before the install) can be named
with `interface` and `model`; it is created as retired.

### Dedup and merge

- **`external_id`** is the client's own key, unique per `source`. The same id again answers
  `duplicate` with the existing id, so an import can be repeated safely.
- Without one, a manual or imported change **merges into a detected change of the same device
  within ±48 h** (the merge window): the detected change keeps its evidence and its minute, takes
  the batteries and the note, becomes `accepted` and `confirmed`. The answer says `merged` and
  `merged_into`. A pending detection the user has not answered yet counts, and so does a
  candidate still in its window.
- A second manual or imported change of the same device within the window is a `duplicate`.
- The other way round, a detection that comes into the inbox while a manual or imported change of
  the device is within the window merges into that one.

### Bulk

`POST changes:bulk` takes a JSON array of the objects above (`source` defaults to `import`) or
`text/csv` with the header

```
address,at,count,type,chemistry,rechargeable,vendor,product,price_cents,note,external_id
```

(`interface` and `model` may be added). One row per slot, `price_cents` per single battery: rows
with the same `address` and `at` are one change. `rechargeable` takes `true`, `1`, `yes` or `x`. The answer has one line per change
(the CSV line it starts on, or the array index + 1): `created`, `merged`, `duplicate`, or `error`
with a code and a reason, plus the counts.

- `?dry_run=true` answers what would happen and writes nothing.
- By default each row stands alone: a bad row is an error line, the others land. `?atomic=true`
  applies all or nothing.
- An `Idempotency-Key` header makes a retried request safe: the same key with the same body within
  24 h gets the stored answer (`Idempotent-Replayed: true`); the same key with another body is a
  409.

`GET export.csv` writes the accepted changes in this format (with an `interface` column and an
`external_id` for every row), so the file goes back in unchanged.

## Samples and detection over history

| Route | Does |
| --- | --- |
| `GET samples?device=&from=&to=` | a device's stored samples (`source` `live`, `seed` or `import`) |
| `POST samples:bulk` | `[{"device": "<address>", "ts": …, "voltage": 2.91, "low_bat": false}]`: imported samples, downsampled on the way in by the live rule (a row when the voltage moved by 0.01 V, the flag changed or a day passed); a row already stored at that millisecond is kept. `?dry_run=true`; an `Idempotency-Key` as above |
| `POST detect?device=&from=&to=&dry_run=` | runs the detector over the stored samples of the span (all devices, or one) and files what it offers as `pending`: a detection near a manual or imported change merges into it; one near a detected or rejected change is a `duplicate`. The answer lists what it found and counts the candidates it discarded |

So a script with years of recorded values has two ways in: push the changes it knows
(`changes:bulk`, with `external_id` = its own row id), or push the series (`samples:bulk`) and
call `detect`, then go through the inbox. Both end in the same tables.

## Notifications

Every quarter of an hour the addon looks at the forecast and the inbox and sends, over the
channel of `PUT settings`, what the chosen `events` ask for: a device reporting a low battery, a
change that is due or due within the horizon, a detection waiting in the inbox. Each notice goes
out once; the same one again only after seven days. The webhook body is
`{"kind", "key", "title", "text", "link", "device", "name", "model", "at"}`; ntfy gets the text as the
body with `Title`, `Tags: battery`, `Priority: high` for low and due, and `Click`. The system
itself offers an addon no channel yet; once it does, it becomes a third `channel` here.

## Other routes

| Route | Does |
| --- | --- |
| `GET suggestions?field=vendor\|product\|type\|chemistry&q=&vendor=` | the values recorded for a battery field, most used first, matched case-insensitively; spellings that differ only in case count as one (types and chemistries add the table's); `vendor` narrows the product lines to the ones recorded with that vendor |
| `GET prices/last?vendor=&product=&type=` | the price per battery recorded most recently for that vendor, product line and size (each optional, matched without regard to case): `{found, price_cents, at}` - the editor's prefill |
| `GET stats?lang=` | the lives and the statistics over the accepted changes: `devices` (per `interface.address`, the `life` of `GET devices`), `lives` (each: device, model, `from`, `to` - absent while it runs -, `days`, the batteries that went in), `batteries` (one row per vendor + product line + size: `total` single batteries, `changes`, `devices`, `models`, `life` summary, `avg_price_cents` per battery, `cost_per_year_cents` as the mean over the lives with a price, `last_price_cents`, and `per_model` - the same per device model, since a battery's life depends on the device), `models` (per model: the devices, the life summary, the batteries used in it with their per-model life and cost per year, longest first), `types` (per size), `year` (the last 365 days: changes, batteries, by type, cost where prices exist); `names` maps the devices to name, model and picture. A life is the time between two accepted changes of a device; the running one counts in no average |
| `GET export.json` | everything: devices with their overrides, slots, model overrides, changes, events, samples |
| `GET settings`, `PUT settings` | the detection and merge windows in force (read only), the shopping settings (`days`, the horizon; `stock` per battery type) and the notification channel (`channel` `webhook` - a POST with the notice as JSON - or `ntfy` - a POST to `<url>/<topic>`, `token` as a bearer -, `events` out of `low`, `due`, `soon`, `inbox`, `language`); PUT takes `shopping` and/or `notify` whole |
| `GET forecast?days=` | every battery device with its forecast: `expected_days` and its `basis` (`own` - the median of the device's own lives, with fewer than two `model` - the model's lives in this installation -, else `table` - the battery table's typical life), `due_at` = the last change + the expected life - 14 days, pulled forward to the voltage `trend`'s date (a regression over the last 60 days reaching the device's low level) where that is earlier, `days_left`, `state` (`low`, `due`, `soon` within the horizon, `ok`, `unknown`); the urgent ones first |
| `GET shopping?days=` | the shopping list: the devices low now or due within the horizon, and per battery type what they need, the stock, and what to buy |
| `POST notify/test` | sends a test notice over the saved channel; 502 `send-failed` when it does not take it |
| `GET health` | the addon's state; `inbox` counts the pending detections |
| `GET storage`, `POST storage/check`, `POST storage/move` | the database's location ([storage.md](storage.md)) |
| `GET openapi.json` | the specification |
