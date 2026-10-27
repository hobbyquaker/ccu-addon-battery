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
written in one transaction every 10 minutes and at stop (SIGTERM), to spare an SD card. A failed
write keeps the queue for the next try. The queue is capped at 20,000 rows; beyond it the oldest
rows are dropped and counted (`dropped` in the health).

## The location

The setting is the `storage` object of `/usr/local/addons/battery/etc/battery.json`:

```json
{"storage": {"location": "userfs"}}
{"storage": {"location": "usb:STICK", "folder": "battery"}}
```

A location is stored as an id and a folder, never as a mount path - the way the system's own
location picker stores a choice, so a stick that comes back at another mount point is still found:

| id | Where | In the system's backups |
| --- | --- | --- |
| `userfs` (the default) | `/usr/local/addons/battery/var/battery.db` | yes (`createBackup.sh` takes `/usr/local`; `bin/` and `www/` are tagged `.nobackup`, `var/` and `etc/` are not) |
| `usb:<label>` | `<mount>/<folder>/battery.db` on the stick whose filesystem label is `<label>`, wherever the system mounted it (`/media/usb1`…`8`); `folder` defaults to `battery`, up to four levels of letters, digits, `. _ -` | **no** - the page and the README say so |
| `share:<name>` | refused | SQLite's locking is not reliable on NFS and SMB; the system keeps its own database off them for the same reason |

The addon learns the sticks from `/proc/mounts` (`/media/usb1`…`8`) and their labels from
`/dev/disk/by-label`; a stick without a label cannot be chosen. The manifest declares the group
`usbstorage` and the path `/media`, which is what a confined addon needs to write a stick (the
handbook, chapter 11); without them the sticks are read-only to the addon and the check says so.

A target is validated before anything happens: the directory exists or can be made, it is
writable (a probe file), it is not on tmpfs, ramfs, a network or a pseudo filesystem, it is not
mounted read-only, and it has room for twice the file plus 16 MiB.

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
- at the next start, a location that is not present (no stick with that label) leaves the store
  **unavailable**: reports are queued in RAM, the page says so, and every 30 s the addon looks
  again; when the stick is back the file is opened and the queue written;
- the addon **never** creates a fresh database at another location on its own. A file missing at a
  location that *is* present is created (that is the explicit setting), with a line in the journal.

## Not in phase 0

Moving while the store is degraded or unavailable (the move needs the source); a `VACUUM INTO`
download of the file; an export. The phase 1 API (`docs/api.md`) adds them.
