# Changelog

## [1.0.1] - 2026-10-27

- **openccu-lite:** the Addons page shows the addon's icon before the install. The icons are now in the repository at
  the path the manifest names, where the system's catalogue check reads them; before, it found none, showed the first
  letter instead and logged a warning per icon at every check.
- **openccu-lite:** the uninstall no longer tries to remove the system's files (the links, the proxy rule) and prints no
  "Permission denied" lines; the system removes them itself.
- **CCU3 and OpenCCU:** the uninstall restarts the web server when it removed the addon's proxy rule, so
  `/addons/battery/` answers 404 right away instead of 503 until the next restart, and removes the rule's directory
  when it was the last file in it.

## [1.0.0] - 2026-10-27

The first release.

- **Recording:** the battery voltage and the low-battery flag of every battery device on HmIP-RF and BidCos-RF
  (`OPERATING_VOLTAGE`, `LOW_BAT`, `LOWBAT`, `BATTERY_STATE`, with `BOOTED` and `UNREACH` as evidence), seeded from the
  system's state store and the radio process's cache without radio traffic, downsampled, in one SQLite file.
- **The battery table:** 459 Homematic models with their power source, battery slots, voltage datapoint and low
  level, each row with its sources (CC0); correctable per model or per device.
- **Change detection:** a restart, a voltage jump to the fresh level or a cleared low-battery flag makes a candidate,
  which comes into the inbox only when the voltage still holds a day later; nothing is booked without your answer.
  Changes can also be recorded by hand, with vendor, product line, rechargeable and price per slot.
- **Pages:** Devices (lives per device, the battery-hungry ones first, the voltage curve with changes and low phases,
  the changes of a device), Inbox, Batteries (lives and cost per battery, per model, comparisons), Shopping (what is
  low or due within a horizon, against the stock at home) and Statistics (the last twelve months, the shortest lives,
  lives per model and size, CSV and JSON exports). German and English, light and dark.
- **Forecasts:** the expected life from the device's own lives, its model's or the battery table's, pulled forward by
  the voltage trend.
- **Notifications** over ntfy or a webhook: low battery, a change due or due soon, a detection waiting in the inbox.
- **The API** at `/addons/battery/api/v1/` (OpenAPI in `docs/openapi.json`), including bulk imports of past changes and
  voltage series with a dry run.
- **The database** in the addon's directory (in every system backup) or on a USB stick, moved safely from the
  settings page.
- **Platforms:** openccu-lite (the addon's own token and session, embedded in the system's shell), OpenCCU and the
  CCU3 (XML-RPC from the interface processes, names and rooms from ReGaHSS; the settings page in the WebUI's
  *Zusatzsoftware*, whose tabs and the app take the WebUI's session; the callback registration is removed at the
  stop). Packages for aarch64, x86_64 and armv7l. Tested on openccu-lite with real devices and on OpenCCU; on the
  stock CCU3 firmware 3.89.11 installed, updated and run (also across a reboot), so far without paired battery
  devices - feedback is welcome.
