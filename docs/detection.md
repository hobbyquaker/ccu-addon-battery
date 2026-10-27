# Detecting a battery change

The addon never books a change on its own. It watches every battery device's reports, and when
they look like fresh batteries it puts a **detection into the inbox**; the user confirms it,
rejects it, or gives another date. This page describes the rules (`internal/detect`).

## The signals

| Signal | Devices | Used for |
| --- | --- | --- |
| `OPERATING_VOLTAGE` (channel 0) | every Homematic IP device | the voltage rules |
| `BATTERY_STATE` (the thermostat channel) | HM-CC-RT-DN, HM-TC-IT-WM-W-EU | the voltage rules |
| `BOOTED` (channel 0) | Homematic IP | rule A: true in the first report after a power-up, and in later reports for a while |
| `LOW_BAT` / `LOWBAT` | all | rule C; the only signal of most BidCos sensors and remotes |
| `UNREACH` | all | an UNREACH gap that ended in the hour before a jump raises its confidence |

What a device takes (count, type, chemistry) comes from the battery table
([data/batteries.json](../data/README.md)), changed by a per-model or per-device override. The
table also gives the **levels** the voltage rules compare with: the pack's *fresh* level and the
*step* of a jump, both `count × per-cell` of the slot's chemistry (2× alkaline: fresh 2.96 V,
step 0.24 V; 2× NiMH: 2.66 V and 0.12 V; outdoor models get 0.1 V more step for the temperature
swing). Mains, bus and externally powered devices (`OPERATING_VOLTAGE_STATUS` `EXTERNAL`) and
devices with fixed cells are not watched.

## Candidates

A **trigger** opens a candidate:

| Rule | Trigger |
| --- | --- |
| **A** | `BOOTED` goes from false to true |
| **B** | the voltage exceeds the highest value of the last 30 days by the step **and** reaches the fresh level |
| **C** | the low-battery flag goes from true to false |
| **D** | the first report after two days of silence is at the fresh level and half a step above the last one |
| **E** | the user (or a script) records the change: not a detection, booked as given |

Values the addon *read* rather than received (its start-up read, the system's sweep, entries
the system restored after a restart) never trigger. A second trigger while a candidate is open
attaches to it (its evidence lists both). Once a change is offered or recorded, triggers within
the next 24 h are ignored (the debounce).

## Why a candidate is not yet a change

Re-seating the *same* half-used cells sets `BOOTED` and lifts their voltage for a while: on an
HmIP-WRC2 we measured 2.8 → 3.1 V, which meets rules A and B. A cell that recovered after the
load was taken off falls back over the next hours; fresh cells stay up. So a candidate waits for
a **confirmation window** (24 h by default, measured from its last trigger) and is decided by the
first voltage reading after it:

- **offered** (into the inbox) when that reading is still at the fresh level and at least half a
  step above the voltage before the trigger;
- **discarded** otherwise, with the reason `recovered` (there was a jump) or `boot` (a `BOOTED`
  alone). The readings of a discarded candidate are dropped from the 30-day maximum, so the
  recovery does not hide a real change in the following weeks.

A low-battery flag that comes back during the window discards the candidate (`low-again`). A
device without a voltage (rule C alone) is offered when the window passed without the flag coming
back. A voltage device that sends no reading for a week after the trigger is offered anyway,
with low confidence and the reason `unconfirmed`. Discarded candidates stay in the database as
evidence (`status: discarded`); they are not shown in the inbox.

## Confidence

| Confidence | When |
| --- | --- |
| high | rule A with a full step of a jump (or after a low-battery phase); rule B with an UNREACH gap in the hour before |
| medium | rule A with half a step; rule B; rule C confirmed by the voltage |
| low | rule D; rule C on a device without a voltage; `unconfirmed` |

Whatever the confidence, the detection waits in the inbox.

## The windows

Set in `/usr/local/addons/battery/etc/battery.json`, read at the start:

```json
{"detection": {"window_hours": 24, "expire_hours": 168, "merge_window_hours": 48}}
```

`window_hours` is the confirmation window. 24 h fits a device that reports once a day (most HmIP
sensors); a longer window decides later but rules out slower recoveries. `merge_window_hours` is
the window of the merge rules in [api.md](api.md). `GET api/v1/settings` answers the values in
force.

## Over imported history

`POST api/v1/detect` runs the same detector over the stored samples of a span (imported ones
included) and files what it offers as pending changes. Imported series rarely carry `BOOTED`, so
there rule B (the jump) and rule C do the work.
