package store

// The schema, one migration per version; Open applies the ones above the file's version. Times
// are Unix milliseconds. Devices and samples are keyed by interface and address (what the system
// reports; the metadata store uses the same key), not by a surrogate id, so a row is readable
// without a join and an import can name its device directly.
var migrations = []string{
	// 1: the P0 tables - devices, their current values, the downsampled samples, the evidence
	// events; changes and slots are created here already (P1 fills them) so the data model is
	// fixed by the first file.
	`CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT NOT NULL);
CREATE TABLE devices (
  interface TEXT NOT NULL,
  address TEXT NOT NULL,
  model TEXT NOT NULL DEFAULT '',
  firmware TEXT NOT NULL DEFAULT '',
  power TEXT NOT NULL DEFAULT 'unknown',      -- battery | mains | external | fixed | unknown
  voltage_dp TEXT NOT NULL DEFAULT '',        -- '0/OPERATING_VOLTAGE', '4/BATTERY_STATE', ''
  lowbat_dp TEXT NOT NULL DEFAULT '',         -- '0/LOW_BAT', '0/LOWBAT', ''
  first_seen INTEGER NOT NULL,
  last_seen INTEGER NOT NULL,
  retired_at INTEGER,
  profile_override TEXT,                      -- JSON, P1
  PRIMARY KEY (interface, address)
) WITHOUT ROWID;
CREATE TABLE slots (
  interface TEXT NOT NULL,
  address TEXT NOT NULL,
  slot INTEGER NOT NULL,
  count INTEGER NOT NULL DEFAULT 1,
  type TEXT NOT NULL DEFAULT '',
  chemistry TEXT NOT NULL DEFAULT '',
  rechargeable INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (interface, address, slot)
) WITHOUT ROWID;
CREATE TABLE current (
  interface TEXT NOT NULL,
  address TEXT NOT NULL,
  voltage REAL,
  voltage_ts INTEGER,
  voltage_status TEXT NOT NULL DEFAULT '',
  low_bat INTEGER,
  low_bat_since INTEGER,
  unreach INTEGER,
  unreach_since INTEGER,
  booted INTEGER,
  last_report INTEGER,
  PRIMARY KEY (interface, address)
) WITHOUT ROWID;
CREATE TABLE samples (
  interface TEXT NOT NULL,
  address TEXT NOT NULL,
  ts INTEGER NOT NULL,
  voltage REAL,
  low_bat INTEGER,
  source TEXT NOT NULL DEFAULT 'live',        -- live | seed | import
  PRIMARY KEY (interface, address, ts)
) WITHOUT ROWID;
CREATE TABLE events (
  id INTEGER PRIMARY KEY,
  interface TEXT NOT NULL,
  address TEXT NOT NULL,
  ts INTEGER NOT NULL,
  key TEXT NOT NULL,
  value TEXT NOT NULL
);
CREATE INDEX events_device ON events (interface, address, ts);
CREATE TABLE changes (
  id INTEGER PRIMARY KEY,
  interface TEXT NOT NULL,
  address TEXT NOT NULL,
  at INTEGER NOT NULL,
  kind TEXT NOT NULL,                         -- detected | confirmed | manual | import
  confidence TEXT NOT NULL DEFAULT '',
  rule TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'pending',     -- pending | accepted | rejected
  evidence TEXT,                              -- JSON
  batteries TEXT,                             -- JSON array of slots
  note TEXT NOT NULL DEFAULT '',
  source TEXT NOT NULL DEFAULT '',
  external_id TEXT,
  created_by TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);
CREATE INDEX changes_device ON changes (interface, address, at);
CREATE UNIQUE INDEX changes_external ON changes (source, external_id) WHERE external_id IS NOT NULL;`,
	// 2: P1 - the per-model overrides of the battery table (the per-device ones are
	// devices.profile_override), the answers kept for an Idempotency-Key, and the inbox's index.
	// changes.status gains candidate (waiting for its confirmation window) and discarded (a boot
	// or a recovery, kept as evidence) besides pending, accepted and rejected.
	`CREATE TABLE model_overrides (
  model TEXT PRIMARY KEY,                     -- lower case
  value TEXT NOT NULL,                        -- JSON, batteries.Override
  updated_at INTEGER NOT NULL
) WITHOUT ROWID;
CREATE TABLE idempotency (
  key TEXT PRIMARY KEY,
  created_at INTEGER NOT NULL,
  status INTEGER NOT NULL,
  body TEXT NOT NULL
) WITHOUT ROWID;
CREATE INDEX changes_status ON changes (status, at);`,
}

// Tables lists the tables a move verifies by row count.
var Tables = []string{"meta", "devices", "slots", "current", "samples", "events", "changes", "model_overrides", "idempotency"}
