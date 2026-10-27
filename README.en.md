# ccu-addon-battery

[:de: Deutsche README](README.md)

Battery change detection and statistics for Homematic devices: an addon for
[openccu-lite](https://github.com/hobbyquaker/openccu-lite) (and, later, for the classic CCU3 and
OpenCCU). It records the battery voltage and the low-battery flag of every device, detects
battery changes from them, lets you say which batteries went in, and turns the record into how
long batteries last, which devices eat batteries, which batteries do best, what is due soon and
what to buy.

**Status: in development, no release yet.** What works today: the recording; the battery table
(which device takes which batteries); the change detection with its inbox; changes recorded by
hand, with the batteries that went in (vendor, product line, price); the device pictures, the
battery lives per device, the Batteries and Statistics tabs; the API for importing past changes
and voltage series; the choice of where the database lives. The forecasts and the shopping list
follow.

## The pages

The app opens at `/addons/battery/` (on openccu-lite inside the system's shell, with its theme
and language):

- **Devices:** every battery device with its picture, status, batteries, voltage, how long its
  batteries lasted so far (mean, shortest, longest, the number of lives - sortable, shortest
  first, to find the battery-hungry devices) and how long the current ones have been in. A device
  opens its detail: the voltage curve over the whole record with the changes marked and the
  low-battery phases as bands, and its changes, each editable. *Changed…* records a change.
- **Inbox:** what the detector found, waiting for your answer - yes (with the batteries that went
  in), another day, or no. Below it, every recorded change.
- **Batteries:** every battery ever recorded (vendor, product line, size): how many went in, in
  which devices and models, how long they lasted, what they cost per battery and per year - and,
  since a battery's life depends on the device, the life per device model behind each row, plus a
  comparison within one model or size.
- **Statistics:** the last twelve months in numbers, the devices with the shortest lives, the life
  per model and per size, and the exports.

The **settings page** is separate (on openccu-lite under *Addons → Battery → Settings*, on a CCU the
addon's *Einstellungen* button): where the database lives and, on a CCU, the service, its log and
the version. Service control and the log are the system's own pages on openccu-lite.

## What it records

| Signal | Where | Devices |
| --- | --- | --- |
| `OPERATING_VOLTAGE`, `OPERATING_VOLTAGE_STATUS`, `LOW_BAT`, `BOOTED`, `UNREACH` | channel 0 | every Homematic IP device |
| `LOWBAT`, `UNREACH` | channel 0 | every BidCos device |
| `BATTERY_STATE` | the thermostat channel | HM-CC-RT-DN, HM-TC-IT-WM-W-EU |

A device is tracked when its descriptions carry one of the battery datapoints. At the start the
addon seeds the values from the system's state store and, for Homematic IP, from the radio
process's cache (no radio traffic); BidCos values are never read, only received. Samples are
downsampled: a row when the voltage moved by 0.01 V, when the low-battery flag changed, or once a
day. The low-battery and unreachable transitions and `BOOTED` are kept as evidence.

## Which batteries a device takes

[data/batteries.json](data/README.md) is a table of 459 Homematic models: the power source
(battery, rechargeable, mains, bus, …), the battery slots (count and type), the voltage datapoint,
the device's own low-battery threshold, and the sources every row stands on - for most battery
models eQ-3's own product pages and manuals. The data is CC0: take it for any project. The addon
embeds it, looks models up case-insensitively (and by their aliases), and lets you correct it
per model or per device (a remote in a desk stand, lithium or rechargeable cells). Corrections to
the table are welcome as pull requests; `python3 data/check.py` checks it (CI runs it), and
[tools/](tools/README.md) has the scripts it was built with.

## Detecting a battery change

A device restart (`BOOTED`), a voltage jump to the fresh level, or the low-battery flag clearing
makes a *candidate*. Re-seating the same half-used cells looks the same for a while - their voltage
recovers once the load is off - so a candidate only comes into the **inbox** when the voltage
still holds the fresh level a day later; otherwise it is logged as a boot. Nothing is booked on
its own: you confirm a detection, reject it, or say it was another day. Devices that report only
a low-battery flag are detected when the flag clears; for those, and for any other device,
*Changed…* records a change by hand. [docs/detection.md](docs/detection.md) has the rules.

## The batteries and what they cost

With every change you can say what went in, per slot: the vendor ("Varta"), the product line
("Power on Demand"), whether they are rechargeable, and the price of one battery. Vendor and
product line are suggested from what was recorded before, so one vendor is not written three
ways; the price is prefilled with the last one paid for that vendor and line. A *life* is the time
between two confirmed changes of a device; the statistics are built from the lives, the running
one counts in no average. The device pictures are the platform's own (the WebUI's catalogue,
which openccu-lite ships too).

## The API

Everything the page does goes through the addon's API at `/addons/battery/api/v1/`, behind the
system's login: devices with their battery profiles, pictures and lives, the changes (list,
record, confirm, edit, delete), bulk imports of past changes (JSON or CSV, with a dry run,
deduplicated and merged with what the detector found), voltage series, a detection run over
imported history, value suggestions, the last price, the statistics and exports.
[docs/api.md](docs/api.md) explains it with a curl recipe; [docs/openapi.json](docs/openapi.json)
specifies it.

## On openccu-lite

Install the package from the Addons page (or, once listed, from the catalogue). The addon runs
confined as its own user, reads the system's APIs with its own token (`rpc:read`, `meta:read`) and
serves its page at `/addons/battery/`, embedded in the system's shell with its theme and language;
its settings page is `/addons/battery-config/`. Updates come from the system's Addons page.

### Where the database lives

The database is one SQLite file, `battery.db`. By default it is in the addon's own directory on
the user partition, `/usr/local/addons/battery/var/`, which every backup of the system includes.
It can be moved to a USB stick the system has mounted (the settings page); the stick is **not**
part of the system's backups, and the page says so. Network shares are not offered: SQLite needs
local file locking, which NFS and SMB do not give.

The move is safe: the writers pause (new values wait in memory), the pending rows are written, a
consistent copy is made with `VACUUM INTO`, the copy is verified (`integrity_check`, the row count
of every table), the setting is switched, the addon reopens on the new file, and the old file is
kept or removed as you chose. If any step fails, nothing changes. When a stick with the database
is pulled, the addon keeps the new values in memory (up to 20,000 rows), says so on its page and in
the journal, and goes on where it was once the stick is back - it never starts a fresh database
somewhere else on its own.

The setting is `storage` in `/usr/local/addons/battery/etc/battery.json`
(`{"storage": {"location": "usb:<label>", "folder": "battery"}}`); the details are in
[docs/storage.md](docs/storage.md).

### Uninstall

Removes the program and keeps `/usr/local/addons/battery/etc` (the settings) and `var` (the
database) for a reinstall. Remove that directory by hand to be rid of the data.

## Development

```sh
go test -race ./...                      # the store, the recorder, the detector, the changes, the statistics, the API, the package files
python3 data/check.py                    # the battery table
(cd ui && npm ci && npm run check && npm test && npm run build)   # the app and the settings page
VERSION=0.1.0 scripts/build.sh           # dist/battery-<arch>-<version>.tar.gz for aarch64, x86_64, armv7l
```

The Go side uses [ccu-addon-kit](https://github.com/hobbyquaker/ccu-addon-kit)'s `addonkit`
(session, CSRF, token, manifest) and `litesource` (the event stream with resume, the state store,
the metadata API); the web app is Svelte 5 on [occulite-ui](https://github.com/hobbyquaker/occulite-ui)
with [uPlot](https://github.com/leeoniya/uPlot) for the voltage curve; the settings page is the
kit's frame with the addon's own tab, vendored into the package by `ccu-addon-kit vendor` from
[addon/kit.json](addon/kit.json). The daemon takes `-no-session-check` and `-token-file` for
development against a system other than the one it runs on:

```sh
go run ./cmd/battery -addon-dir /tmp/battery -www ui/dist -system http://<system> -token-file ./token -no-session-check
```

## Licence

MIT; the battery table (`data/batteries.json` and its schema) is CC0-1.0.
