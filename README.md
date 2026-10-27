# ccu-addon-battery

Battery change detection and statistics for Homematic devices: an addon for
[openccu-lite](https://github.com/hobbyquaker/openccu-lite) (and, later, for the classic CCU3 and
OpenCCU). It records the battery voltage and the low-battery flag of every device, will derive
battery changes from them, lets the user say which batteries went in, and turns the record into
how long batteries last, what is due soon and what to buy.

**Status: in development, no release yet.** What works today (phase 0): the recording, the device
list with the current values, and the choice of where the database lives. Detection, the battery
table, the annotations and the statistics follow.

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

## On openccu-lite

Install the package from the Addons page (or, once listed, from the catalogue). The addon runs
confined as its own user, reads the system's APIs with its own token (`rpc:read`, `meta:read`) and
serves its page at `/addons/battery/`, embedded in the system's shell with its theme and language.
Updates come from the system's Addons page.

### Where the database lives

The database is one SQLite file, `battery.db`. By default it is in the addon's own directory on
the user partition, `/usr/local/addons/battery/var/`, which every backup of the system includes.
It can be moved to a USB stick the system has mounted (System → Storage); the stick is **not**
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
go test -race ./...                      # the store, the recorder, the API, the package files
(cd ui && npm ci && npm run check && npm test && npm run build)
VERSION=0.1.0 scripts/build.sh           # dist/battery-<arch>-<version>.tar.gz for aarch64, x86_64, armv7l
```

The Go side uses [ccu-addon-kit](https://github.com/hobbyquaker/ccu-addon-kit)'s `addonkit`
(session, CSRF, token, manifest and lighttpd fragment) and `litesource` (the event stream with
resume, the state store, the metadata API); the web app is Svelte 5 on
[occulite-ui](https://github.com/hobbyquaker/occulite-ui). The daemon takes `-no-session-check`
and `-token-file` for development against a system other than the one it runs on:

```sh
go run ./cmd/battery -addon-dir /tmp/battery -www ui/dist -system http://<system> -token-file ./token -no-session-check
```

## Licence

MIT.
