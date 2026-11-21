# The database and where it lives

The addon keeps everything in one SQLite file, `battery.db` (pure Go `modernc.org/sqlite`, WAL
mode, `synchronous=NORMAL`). This document is the design of the configurable location and the move.

## Tables

| Table | Holds |
| --- | --- |
| `meta` | key/value, reserved |
| `devices` | every tracked device: `interface`, `address` (the key), `model`, `firmware`, `power` (`battery`, `mains`, `external`, `fixed`, `unknown`), `voltage_dp` (`0/OPERATING_VOLTAGE`, `4/BATTERY_STATE` or empty), `lowbat_dp` (`0/LOW_BAT`, `0/LOWBAT` or empty), `first_seen`, `last_seen`, `retired_at`, `profile_override` (JSON, phase 1) |
| `slots` | the device's battery slots (count, type, chemistry, rechargeable) - phase 1 fills them |
| `current` | the last values per device: voltage and its time, `voltage_status`, `low_bat` and since when, `unreach` and since when, `booted`, `last_report` |
| `samples` | the downsampled voltage series: `ts`, `voltage`, `low_bat`, `source` (`live`, `seed`, `import`) |
| `events` | the evidence trail: `LOW_BAT`/`LOWBAT` and `UNREACH` transitions, `BOOTED` - live reports only |
| `changes` | the battery changes (phase 1): kind, confidence, rule, status, evidence, batteries, note, source, `external_id` |

Times are Unix milliseconds. The schema version is `PRAGMA user_version`; `Open` applies the
migrations above it.

## Writes

Reports are applied to the in-memory current values at once and queued as rows; the queue is
written in one transaction at the **write interval** and at stop (SIGTERM), to spare an SD card.
A failed write keeps the queue for the next try. The queue is capped at 20,000 rows; beyond it the
oldest rows are dropped and counted (`dropped` in the health). What a user or a script changes -
a battery change, an override, a detection's state - is written at once, the queue first.

What reaches the queue is only what changed:

- a **sample** row when the voltage moved by 0.01 V or more since the device's last row (through
  the flap filter below), its low-battery flag changed, or 24 hours passed;
- a device's **current** row when one of its values changed or a sample was written. A report that
  repeats the values moves only the row's timestamps (`voltage_ts`, `last_report`), and a voltage
  the flap filter holds back is treated the same; those are kept in RAM (the pages show them at
  once) and written at most every 6 hours and at stop - the system hands them back at the next
  start anyway;
- an **event** for a low-battery, unreachable or boot transition.

**The flap filter** (always on). The devices report their voltage in steps of 0.1 V, and a voltage
near a step flips between the two values with the load (a thermostat's motor) and the temperature
(day and night), for days while a pack drains past it. The history no longer records such short
flips:

- a change of **more than 0.1 V** from the last recorded value is recorded at once - a battery
  change, a pack giving out;
- a change of **at most 0.1 V** is recorded only once it held: every report for **12 hours** stayed
  on the same side of the recorded value, and at least two reports came. It is then recorded at the
  time it first came, so the chart shows it where it happened. A report back at the recorded value
  before ends the wait and leaves nothing; a report on the other side starts a new wait;
- a **low-battery** transition is recorded at once, with the voltage it came with;
- the **daily row** keeps the recorded value while a change waits;
- after a restart the filter compares with the last recorded row.

Half a day leaves out the motor's dips and the night's, and a falling pack's next step is still
recorded within a day of the time it is reached for good (with the time it came). Only the history
is filtered: the current values, the change detection (it sees every report live), the low-battery
warnings and the device's own status see every report as it comes. History recorded before is left
as it is.

SQLite rewrites whole 4 KiB pages: a sample of some 30 bytes costs a page in the WAL, and the
samples of different devices lie on different pages. Every transaction also touches the
`-shm` index and the file's metadata. A longer interval puts more rows on each page and costs
fewer transactions.

**The write interval** is the user's choice on the settings page (*Database* tab), the key
`write_interval` (minutes) of `etc/battery.json`: 1, 5, 10, 30 (the default) or 60 minutes.
`PUT api/v1/storage/interval {"minutes": 30}` sets it (an administrator's session) and takes
effect at once (the queue is written first); `GET api/v1/storage` answers it as the health's
`interval_minutes`. The daemon's `-flush` flag overrides it (`interval_fixed` in the health). At a
power cut the voltage readings of at most one interval are lost; the system's current values come
back at the next start.

Measured with the recorder test `TestWriteLoadMeasure` under `strace` (100 battery devices - 30
HmIP thermostats every 3 minutes, 50 other HmIP devices every 20 minutes, 20 BidCos thermostats
every 3 minutes, the voltage flipping by 0.1 V in 3 % of the reports - on a database grown to
9.7 MB, 24 hours):

| | transactions/h | written/h (WAL + checkpoint) |
| --- | --- | --- |
| every report's current row, 10 minutes (up to 1.2.0) | 6 | 308 KB |
| changed rows only, 10 minutes | 6 | 280 KB |
| changed rows only, 30 minutes | 2 | 229 KB |
| changed rows only, 60 minutes | 1 | 197 KB |
| changed rows only and the flap filter, 30 minutes (the default) | 0.5 | 41 KB |

With the flap filter the simulated day records 107 sample rows instead of 917, and 13 of the 48
write intervals find anything to write.

Where the addon keeps its own copy for the firmware's backup (below), that copy is a full write of
the file: with this 9.7 MB database 9.7 MB a day (about 400 KB/h) when renewed daily, 1.4 MB a day
on average when weekly.

The WAL is checkpointed into the file by SQLite every 1,000 pages (about 4 MB of WAL: with
`synchronous=NORMAL` the only `fsync`s, a few a day here); the WAL file keeps its size and is
reused.

## The location

The setting is the `storage` object of `/usr/local/addons/battery/etc/battery.json`:

```json
{"storage": {"location": "userfs"}}
{"storage": {"location": "usb:STICK", "folder": "battery"}}
{"storage": {"location": "usb-uuid:1A2B-3C4D", "folder": "battery"}}
```

A location is stored as an id and a folder - the way the system's own location picker stores a
choice, so a stick that comes back at another mount point is still found. A stick is named by what
identifies it best: its label, without a label its filesystem's UUID, and only where the system
tells neither, its mount point:

| id | Where | In the system's backups |
| --- | --- | --- |
| `userfs` (the default) | `/usr/local/addons/battery/var/battery.db` | as its snapshot (below) |
| `usb:<label>` | `<mount>/<folder>/battery.db` on the stick whose filesystem label is `<label>`, wherever the system mounted it (`/media/usb1`…`8`); `folder` defaults to `battery`, up to four levels of letters, digits, `. _ -` | as its snapshot (below) |
| `usb-uuid:<uuid>` | the same, on the stick without a label whose filesystem has that UUID (a FAT volume id such as `1A2B-3C4D`). A stick that gets a label later is still found by its UUID | as its snapshot (below) |
| `usb-path:/media/usb<N>` | the same, on whatever stick is mounted at that mount point - for a system that tells neither label nor UUID (piVCCU's container has no udev, so no `/dev/disk`, and `blkid` cannot read the device there). Weaker than a name: see below | as its snapshot (below) |
| `share:<name>` | refused | SQLite's locking is not reliable on NFS and SMB; the system keeps its own database off them for the same reason |

The addon learns the sticks from `/proc/mounts` (a mount at `/media/usb0`…`9`; a CCU3, OpenCCU and
openccu-lite mount at `usb1`…`8`), and their labels and UUIDs from udev's `/dev/disk/by-label` and
`/dev/disk/by-uuid`. Where udev does not know a device it asks `blkid <device>`; a stick neither
can name is still offered, by its mount point. Every mounted stick can be chosen, with or without
a label (until 1.3.0 a stick without one could not, and on a system without `/dev/disk/by-label`
that was every stick). A stick known only by its mount point gets one extra rule, because any stick
plugged in there would pass for it: when the database file is missing on it, the addon takes it for
another stick, begins no database there and waits - the store is unavailable as for a stick that is
away - until the stick with the database is back. The manifest declares the group
`usbstorage` and the path `/media`, which is what a confined addon needs to write a stick (the
handbook, chapter 11); without them the sticks are read-only to the addon and the check says so.

A target is validated before anything happens: the directory exists or can be made, it is
writable (a probe file), it is not on tmpfs, ramfs, a network or a pseudo filesystem, it is not
mounted read-only, and it has room for twice the file plus 16 MiB.

## In the system's backups

The system's backup is a tar of `/usr/local` that skips directories tagged `.nobackup`: a database
on a stick is never in it, and a live file on the user partition can be copied while the daemon
writes. So the backup takes a **snapshot** instead, in `/usr/local/addons/battery/backup/battery.db`
(ccu-addon-kit's `addonkit/backup`):

- **openccu-lite from 1.3.0-beta.0:** the manifest's `backup` block names `bin/backup-snapshot` as
  the hook. Before every backup the system runs it as the addon's user: a process of its own opens
  the database at its configured location, writes `VACUUM INTO backup/battery.db.tmp` (one read
  transaction, consistent while the daemon writes; the daemon's RAM batch of at most one write
  interval is not in it), checks the copy with `PRAGMA integrity_check` and renames it into place. The tar takes
  it, the system empties `backup/` afterwards. The Backup page can switch it off for this addon
  (`size_hint_mb` 20: on by default).
- **Everywhere else** (a CCU3, OpenCCU, an older openccu-lite, or one whose stored manifest predates
  the block - an addon installed on 1.2.x until it is updated): the daemon writes the same snapshot
  itself, through the store with the batch written first, when there is none of its own or it is
  older than a day (checked hourly), and keeps it there for the firmware's backup. Every such
  snapshot is a full copy of the database on the user partition - the largest single write of the
  addon on these systems - so the age is the user's choice: 1 day (the default) or 7, the key
  `backup_copy_days` of `etc/battery.json`, set on the settings page or with
  `PUT api/v1/storage/backup-copy {"days": 7}` (an administrator's session; refused with 409 where
  the system runs the hook).
- **Which of the two** the daemon learns from the system: `bin/backup-restore`, the manifest's
  `restore`, runs before every start of the unit exactly when the system knows the block, and leaves
  the start's `INVOCATION_ID` in `var/`; the daemon started by the same start finds it.
- **`var/` is tagged `.nobackup`** while a snapshot covers the database: always where the system
  runs the hook (switched off on the Backup page, the database is then in no backup - the user's
  choice), elsewhere after the daemon's first snapshot. A snapshot that fails removes the tag, so the
  backup then takes the live file as it lies; the hook exits 1 and the Backup page names it.
- **After a restore** the snapshot is back in `backup/`, `var/` is not. `backup-restore` (and the
  daemon's start, where nothing runs it) copies it to the configured location unless the file there
  is newer (its `-wal` counts), removing the old `-wal` and `-shm`; a stick that is not plugged in
  gets it on the user partition, the setting follows, and the settings page shows a notice for two
  weeks. A marker in `var/` names the snapshot the system wrote last, so a restart during a backup
  never takes it for a restored one. On openccu-lite `backup/` is emptied afterwards; elsewhere it
  stays as the daily copy.
- By hand: `rc.d backup-prepare` and `rc.d backup-restore`.

`GET api/v1/storage` answers `backup: {mode: "system" | "daily", last?, error?, notice?, copy_days?}`; the
settings page says which.

## The move

`POST api/v1/storage/move {location, folder?, keep_old}` (an administrator's session, the kit's
CSRF header):

1. validate the target; refuse a target that is the current location or already holds a
   `battery.db`;
2. pause the writers - reports go on into the RAM queue - and write the pending rows into the
   current file, so the copy holds everything;
3. `VACUUM INTO <target>/battery.db.moving`: a consistent, compacted copy made by SQLite itself;
4. verify the copy: `PRAGMA integrity_check` is `ok`, and every table's row count equals the
   source's;
5. rename the copy to `battery.db` (atomic within the target filesystem), then write the setting
   atomically (temporary file, fsync, rename) - **the switch point**;
6. close the old file, open the new one; if that fails, write the old setting back, remove the
   copy, reopen the old file, and report the error;
7. remove the old file (and its `-wal`/`-shm`) when `keep_old` is false - only now, after the new
   one is open;
8. resume the writers; the queue lands in the new file.

Any failure before step 5 leaves everything exactly as it was (the temporary copy is removed).
`POST api/v1/storage/check` runs step 1 alone, for the page's *Check* button. The page asks before
the move through the kit's dialog (never `window.confirm`), with *Keep the old file* as a choice.

## When the location disappears

A stick with the database is pulled, or a user deletes the file:

- the next write finds the path gone (the file is stat'ed before every write, because an unlinked
  file still accepts writes through the open handle and nothing of that would ever be read again)
  and fails; the store is **degraded**: the queue is kept, the health says why, the page shows a
  banner, the journal has one line;
- at the next start, a location that is not present (no stick with that label or UUID, nothing
  mounted at that mount point, or - for a stick known by its mount point - a stick without the
  database) leaves the store
  **unavailable**: reports are queued in RAM, the page says so, and every 30 s the addon looks
  again; when the stick is back the file is opened and the queue written;
- the addon **never** creates a fresh database at another location on its own. A file missing at a
  location that *is* present is created (that is the explicit setting), with a line in the journal -
  except on a stick known only by its mount point, as said above.

## Not in phase 0

Moving while the store is degraded or unavailable (the move needs the source); a `VACUUM INTO`
download of the file; an export. The phase 1 API (`docs/api.md`) adds them.
