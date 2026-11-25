# Changelog

## [1.5.8] - 2026-11-25

- **piVCCU, CCU3 and OpenCCU:** BidCos devices no longer stay "Unbekannt" (unknown). A BidCos device reports its
  low-battery flag only when it changes, and a classic CCU keeps no record of it that the addon could read at the
  start, so a push button or motion detector whose batteries stayed fine showed "Unbekannt" for good. At the start
  the addon now reads the flag of every BidCos device it does not know yet from the radio process's cache; nothing
  is sent over the air, and "Letzte Meldung" (last report) still shows only what the device itself reported.
  (reported by emil70 in the homematic-forum)

## [1.5.7] - 2026-11-24

- **piVCCU, CCU3 and every system:** an addon that was running before an update runs again after it. The install
  script started the service after an update only where it took the system for OpenCCU; on the CCU3 firmware it
  relied on the reboot that follows an upload there. Where that firmware installs without a reboot - a piVCCU whose
  restart after the upload is switched off, for example - the addon stayed stopped until it was started by hand.
  The script now notes whether the addon is running before it stops it and starts it again afterwards on every
  system; an addon that was stopped stays stopped. (reported by Slice in the homematic-forum)
- **OpenCCU:** after an installation or update the home page no longer keeps announcing "Neue Zusatzsoftware" with
  an older version. OpenCCU checks the addons for updates once a day and showed the version that was the newest at
  its last check until the next one, even when a newer one was installed. The install script now has OpenCCU
  check again a few seconds after the installation. (reported by Tagfahrlicht in the homematic-forum)

## [1.5.6] - 2026-11-23

- **OpenCCU:** a first installation through the WebUI no longer breaks off. The install script restarted the web
  server to read the addon's proxy rule while the WebUI's install request was still running on it, so the request
  was cut off: the WebUI showed "Error while loading the following file: /config/cp_software.cgi" (OpenCCU 3.89.14)
  or hung, no reboot followed and the addon did not start. It now reloads the web server gracefully. An update was
  not affected. (reported by zoephelweb in the homematic-forum)

## [1.5.5] - 2026-11-23

- **Every system:** the voltage of self-built devices (HB-…) is recognised on any channel, including
  `BATTERY_VOLTAGE`. Such devices often report their battery voltage on channel 1 or another channel instead of
  channel 0, as `OPERATING_VOLTAGE` (e.g. HB-UNI-Sen-CAP-MOIST, HB-UNI-Sen-WEIGHT, HB-UNI-Sen-PF-SC) or as
  `BATTERY_VOLTAGE` (e.g. HB-UNI-Sen-IAQ, HB-UNI-Sen-DIST/-LEV, HB-RC-4-Dis-TH), and the addon showed no voltage
  for them. It now takes channel 0's `OPERATING_VOLTAGE` first, as before, then the first channel with
  `OPERATING_VOLTAGE`, then the first with `BATTERY_VOLTAGE` - only a value in volts, so a voltmeter's or power
  meter's reading is never taken. Devices already in the list pick up their voltage at the next start of the addon;
  no setting is needed. The low-battery flag is still read from channel 0 only. (asked for in the homematic-forum)

## [1.5.4] - 2026-11-22

- **Every system:** a device in eQ-3's table stand (HmIP-DS55) is set with one click: *Batterien bearbeiten…*
  offers *Im Tischaufsteller (2× AA)*, for this device or for all devices of the model. The addon then counts with
  the stand's two AA cells instead of the device's AAA cells and with the doubled battery life eQ-3 states; the
  device's own low-battery level stays. The device page names the source *Tischaufsteller*, and *Zurücksetzen*
  removes it like any own value. It is offered for the 55 mm devices eQ-3 names the stand for - the wall-mount
  remotes (HmIP-WRC2, HmIP-WRC6), wall thermostats (HmIP-WTH-1/-2), temperature and humidity sensors (HmIP-STH,
  HmIP-STHD), the rotary button (HmIP-WRCR), the motion detector for 55 mm frames (HmIP-SMI55) and Homematic's
  55 mm buttons and displays (HM-PB-2-WM55, HM-PB-6-WM55, HM-Dis-WM55, HM-Dis-EP-WM55, HM-Sen-MDIR-WM55), with
  their anthracite and earlier versions. The battery table lists the eQ-3 documents it stands on. (asked for in the
  homematic-forum)

## [1.5.3] - 2026-11-22

- **Every system:** the device page shows the settings for the power supply, the battery slots (count and type) and
  the battery kind again, for one device or for all devices of a model (*Batterien bearbeiten…*); they were stored,
  but the page did not show them. A device's values come before its model's, the model's before the battery table's;
  the page says where each value comes from and resets each level on its own. This also works for a model missing
  from the battery table, such as a self-built HB device. (reported in the homematic-forum)

## [1.5.2] - 2026-11-22

- **CCU3, OpenCCU and openccu-lite:** a package for another architecture is refused at the installation instead of
  installing and then failing to start (`syntax error: unexpected ")"` in the log). The installation stops before it
  touches the installed version, which keeps running; OpenCCU shows *Error (13)*, openccu-lite *unsupported
  platform*, and the system log names both architectures and the package to download (e.g.
  `battery-x86_64-1.5.2.tar.gz`). A Raspberry Pi with a 64-bit system still takes the `armv7l` package as well.
  (reported in the homematic-forum)
- **CCU3 and OpenCCU:** the update hint on the *Zusatzsoftware* page links the package for the system
  (`uname -m`) directly instead of the release page with all three files.

## [1.5.1] - 2026-11-21

- **Every system:** a model that runs on batteries or on a power supply unit, the **HmIP-MP3P** sounder, is now
  recognised on its supply by its voltage, since it has no datapoint that says so: an operating voltage of 4.9 V or
  more - above what its 3× C cells give - or the voltage status OVERFLOW, reported for an hour, puts it on the power
  supply unit (no forecast, no shopping-list entry, no change detection); back in the cells' range for an hour, it
  is battery-powered again. A single outlier changes nothing. The device page's *Power supply* now offers
  *Automatic* (the default), *Batteries* and *Power supply unit*; the manual choice wins, and the page says when the
  supply was detected automatically. The API's profile carries `auto` and `table_power`. Not yet checked against a
  real MP3P on its supply - readings are welcome. (forum: UweS)

## [1.5.0] - 2026-11-21

- **Every system:** *Do not monitor* takes a single device out of the battery views. On the device page and in the
  device list, the button takes the device out of the list, the forecast, the shopping list and the notifications,
  and change detection stops for it; detections of it still waiting in the inbox are dropped. Its readings are still
  recorded and the changes you booked stay. *Show ignored* in the list brings such devices back into view, and
  *Monitor again* switches one back on. The choice is kept per device in the addon's database. The API has
  `PUT`/`DELETE devices/{interface}/{address}/ignore`. (forum: UweS)
- **Every system:** ELV's own Homematic IP kits have rows in the battery table. The **ELV-SH-BS2**, the kit of the
  230 V switch actuator HmIP-BS2, was missing, so the addon listed it as a battery device with *unknown* batteries;
  it is now mains, like the ELV-SH-PSMCI, ELV-SH-WUA, ELV-SH-WSC and ELV-SH-SB8 (mains or an external DC supply,
  from ELV's manuals and ELVjournal articles). The ELV-SH-CWD, -CAP and -CTV take 1× CR2032 and the ELV-SH-DUSI
  2× AA. (forum: UweS)
- **Every system:** a model the battery table lacks that reports a voltage but no low-battery flag counts as
  mains-powered instead of a battery device of unknown batteries - that is how Homematic IP actuators report. The
  cell count is guessed from a voltage only when it is that of 1 to 3 cells (0.8-5.0 V).
- **Every system:** the **HmIP-MP3P** sounder runs on 3× C or on a 5 V plug-in supply. The device page of such a
  model now offers *Power supply: batteries / power supply unit*; on the supply it gets no forecast, no
  shopping-list entry and no change detection from supply-voltage noise. (forum: UweS)

## [1.4.1] - 2026-11-21

- **Every system:** the battery table no longer takes a battery or a mains supply from a source that is only a
  cross-check. 31 models stood on such a source alone; they were checked against eQ-3's manuals and datasheets and
  ELV's kit documents, and six were wrong: the **HM-LC-Sw4-PCB** was listed as *1× 9V*, but it runs on an external
  7–15 V DC supply (its battery sibling is the HM-LC-Sw4-Ba-PCB, which was listed as mains); the HM-MOD-EM-8, the
  HM-MOD-Re-8 and the HM-OU-CM-PCB were listed as *1× 9V* as well and take an external DC supply or cells of the
  builder's choice; the HM-Sen-RD-O rain sensor was listed as *1× 9V* and runs on an external 7.5–30 V supply. 24
  rows now cite the manufacturer's document, two plug-in actuators (HM-LC-Sw1-Pl, HM-LC-Sw1-Pl-2) are mains by their
  model class, and five models no document could be found for (ALPHA-IP-RBG, HM-PBI-2-FM, HM-RC-19-SW,
  HM-Sec-RHS-2, HM-Sec-SC2L) are shown as *unknown* instead of a guess (a pull request with the manufacturer's
  document is welcome). After the update a device that is not battery-powered leaves the battery views; detections of it
  still waiting in the inbox are dropped, the changes you recorded yourself stay. (forum: Slice)

## [1.4.0] - 2026-11-21

- **CCU3, OpenCCU, piVCCU:** umlauts in device and room names are shown correctly. The names come from the CCU in its
  own character set (ISO-8859-1), which the addon took for UTF-8, so *Türe Geräteschuppen* appeared as
  *T�re Ger�teschuppen* everywhere - in the lists, the notifications and the exports. Nothing was stored wrongly: the
  names are right as soon as the update is installed. (#1, @thofra; forum: Slice)
- **CCU3, OpenCCU, piVCCU:** the rooms a CCU comes with are shown by their names - *Küche*, *Wohnzimmer*, in English
  *Kitchen*, *Living room* - as the WebUI shows them. The CCU keeps those rooms under an internal key, and the *Room*
  column showed that key (*roomKitchen*, *roomLivingRoom*). Rooms you named yourself were always right and stay as
  you typed them.
- **Every system:** the device list can hide devices. *Hide unreachable devices* leaves out the ones that are
  unreachable right now; on a **CCU3, OpenCCU and piVCCU**, *Hide devices with service messages switched off* leaves
  out the ones whose service messages you switched off in the CCU (*Settings → Devices*, the *Service messages*
  checkbox) - the devices you have already told the CCU not to bother you with. openccu-lite has no such switch per
  device, and the list says so. Nothing is hidden until you tick a box, and the browser remembers the choice.
  (#2, @thofra)
- **Every system, piVCCU above all:** the database can be moved to a USB stick that has no name. Until now the
  settings page listed such a stick as *no label, cannot be chosen* - and on piVCCU, where the system does not tell
  the addon a stick's name at all, that was every stick. A stick is now known by its name, without one by the UUID of
  its filesystem, and where the system tells neither, by where it is mounted (the page says so, and what follows from
  it: if another stick is plugged in there later, the addon writes nothing to it and waits for the right one). A
  database already on a named stick stays where it is. When a move fails, the reason now stays on the settings page
  instead of only flashing by as a message. (forum: Slice)

## [1.3.0] - 2026-11-20

- **Every system:** the addon writes less to the SD card. A report that only repeats a device's values writes nothing
  any more (its time is kept in memory and written at most every six hours and at the stop), and new values are written
  together every 30 minutes instead of every 10. The interval is a choice on the settings page (*Database*: 1, 5, 10,
  30 or 60 minutes): a longer one writes less often, and at a power cut the voltage readings of at most that long are
  lost; battery changes, your entries and the settings are still written at once. In a simulation of 100 battery
  devices on a grown database this cuts the addon's writes from about 310 to 230 KB an hour and its transactions from
  six to two an hour.
- **Every system:** the voltage history no longer records short 0.1 V flips. A device reports its voltage in steps of
  0.1 V, and near a step it flips back and forth with the load and the temperature, often for days; such a change now
  goes into the history only once it held for half a day, and then at the time it first came. Larger steps - a battery
  change, a pack giving out - and every low-battery report go in at once, and the current values, the change detection
  and the warnings still see every report. History recorded before stays as it is. In the same simulation this cuts
  the addon's writes further to about 40 KB an hour.
- **CCU3, OpenCCU and openccu-lite before 1.3.0-beta.0:** the copy of the database the addon keeps for the system's
  backup can be renewed weekly instead of daily (settings page, *Database*). Each copy writes the whole database to the
  SD card; a weekly one spares six of seven, and a restored backup then lacks up to a week of readings instead of a day.

## [1.2.0] - 2026-11-17

- **Every system:** the database is in the system's backups as a checked copy, wherever it lives - a database on a USB
  stick included, which no backup held before. On openccu-lite (from 1.3.0-beta.0) the system has the addon write the
  copy right before every backup (`VACUUM INTO`, then `integrity_check` on the copy) and takes it instead of the live
  file, which could be caught half-written; the system's Backup page lists the addon under its addon data and can
  switch it off. On a CCU3, OpenCCU and an older openccu-lite the addon keeps such a copy at most a day old in
  `/usr/local/addons/battery/backup/` itself, which the firmware's backup takes. After a restore the copy goes back
  where the database lives unless the file there is newer; when its USB stick is not plugged in, the database lands on
  the user partition and the settings page says so. The settings page says how the database gets into the backups.
- **openccu-lite:** a program can use the addon's API with an API token of the scope `addon:battery` (made under
  *System → Remote access → API tokens*), without a login; it counts as a user's session, and a Full-access token as
  an administrator's. Before, the addon refused every token although the system let it through.

## [1.1.0] - 2026-11-15

- **Every system:** a detected battery change shows in the inbox at once, while the addon still checks for a day that
  the voltage holds, marked "still being checked" with the time the check ends. It can be answered right away: confirm
  it (with the batteries that went in, on another day if need be) or reject it. The answer is final; the check no
  longer turns a confirmed change into a restart or a rejected one into a new entry. A detection nobody answers is
  decided by the check as before, and the inbox notification still goes out only once the check passed. The API lists
  such a detection with `status=candidate` and its `check_until`; the health `inbox` count includes it.

## [1.0.4] - 2026-10-28

- **Every system:** after an update the browser shows the new version of the app at the next normal reload. The
  packages are built reproducibly, so every file has the same date in every version; the app's `index.html` was
  answered by that date, the browser's check said "unchanged", and it kept the old page until its cache was cleared.
  The page is now checked by its content.
- **openccu-lite:** the Addons page links this changelog as the release notes beside an offered update (the manifest
  names it again; it had been left out of 1.0.0).

## [1.0.3] - 2026-10-28

- **Every system:** the devices of an interface whose radio process was not answering yet when the addon started (at
  boot the addon can be up before the HmIP process) are now learned as soon as it answers. Before, its devices stayed
  unknown until a device was paired or the addon was restarted, and devices the addon had not recorded before were
  missing from the Devices page. The addon tries again after 5 seconds, then less often up to once a minute, and at
  once when the system reports the interface up or the event stream resynchronises; the log says so once when it
  starts trying and once when the devices are learned. The values that came in meanwhile are read from the system's
  state and the radio process's cache, without radio traffic.

## [1.0.2] - 2026-10-28

- **openccu-lite:** the addon comes up by itself after a reboot. The system's web server answers every API request
  with 503 "starting" for a few seconds after boot, until the system service is up; the daemon took that answer for
  "not openccu-lite", exited and stayed failed until started by hand from the Addons page. It now waits for the system
  (a log line when the wait begins, then one a minute) and keeps waiting on openccu-lite whatever the web server
  answers; only on a system that is not openccu-lite is a missing API still the end.

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
