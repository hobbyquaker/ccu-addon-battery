// Package store is the addon's SQLite store (modernc.org/sqlite, pure Go): devices, their
// current values, downsampled voltage samples, evidence events and - from P1 - the battery
// changes. Writes are batched in RAM and flushed at an interval and at stop, to spare an SD card;
// a flush that fails keeps the batch and the store reports itself degraded, never silently
// starting over elsewhere. Where the file lives is the user's choice (storage.go).
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// FileName is the database file's name at every location.
const FileName = "battery.db"

// Store is the open store, or one that waits for its location.
type Store struct {
	cfg    Config
	logf   func(string, ...any)
	mu     sync.Mutex
	db     *sql.DB
	path   string
	loc    Location
	err    error // why the store is not available or the last flush failed
	queue  []op
	drop   int64 // rows dropped from the queue because it was full
	paused bool
	last   time.Time
	flushN int64
	maxQ   int
}

type op func(tx *sql.Tx) error

// Config says where the addon keeps things.
type Config struct {
	// AddonDir is /usr/local/addons/battery: etc/battery.json holds the settings, var/ the
	// default database location.
	AddonDir string
	// MaxQueue caps the batch held in RAM while the file cannot be written (20,000 rows by
	// default; the oldest are dropped beyond it and counted).
	MaxQueue int
	// Log receives one line per noteworthy event.
	Log func(string, ...any)
	// Mounts lists the external media; DefaultMounts when nil.
	Mounts func() []Mount
}

// Open reads the settings, resolves the location and opens the file there. A location that is
// not present (a stick that is not plugged in) is not an error: the store starts unavailable,
// buffers in RAM and Retry opens it later. A file that is missing at a present location is
// created - that is the explicit location, and the log says so.
func Open(cfg Config) (*Store, error) {
	if cfg.Log == nil {
		cfg.Log = log.Printf
	}
	if cfg.MaxQueue <= 0 {
		cfg.MaxQueue = 20000
	}
	if cfg.Mounts == nil {
		cfg.Mounts = DefaultMounts
	}
	s := &Store{cfg: cfg, logf: cfg.Log, maxQ: cfg.MaxQueue}
	loc, err := LoadLocation(cfg.settingsPath())
	if err != nil {
		return nil, err
	}
	s.loc = loc
	s.Retry()
	return s, nil
}

func (c Config) settingsPath() string { return filepath.Join(c.AddonDir, "etc", "battery.json") }

// Retry tries to open the configured location when the store is unavailable; true when open.
func (s *Store) Retry() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db != nil {
		return true
	}
	path, err := s.loc.Resolve(s.cfg.AddonDir, s.cfg.Mounts())
	if err != nil {
		s.err = err
		return false
	}
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		s.logf("store: no database at %s yet, creating one", path)
	}
	db, err := openFile(path)
	if err != nil {
		s.err = fmt.Errorf("%s: %w", path, err)
		s.logf("store: cannot open %s: %v", path, err)
		return false
	}
	s.db, s.path, s.err = db, path, nil
	s.logf("store: open %s (%s)", path, s.loc)
	if len(s.queue) > 0 {
		s.flushLocked()
	}
	return true
}

func openFile(path string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path+"?_time_format=sqlite&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(ON)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err := migrate(db); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func migrate(db *sql.DB) error {
	var version int
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	if version > len(migrations) {
		return fmt.Errorf("database version %d is newer than this addon (%d)", version, len(migrations))
	}
	for v := version; v < len(migrations); v++ {
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(migrations[v]); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %d: %w", v+1, err)
		}
		if _, err := tx.Exec("PRAGMA user_version = " + strconv.Itoa(v+1)); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// Close flushes and closes.
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return s.err
	}
	s.flushLocked()
	err := s.db.Close()
	s.db = nil
	return err
}

// Available says whether the file is open and the last flush succeeded.
func (s *Store) Available() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.db != nil && s.err == nil
}

// Health is what the API shows.
type Health struct {
	State     string     `json:"state"` // ok | degraded | unavailable
	Error     string     `json:"error,omitempty"`
	Path      string     `json:"path"`
	Location  Location   `json:"location"`
	Pending   int        `json:"pending"`
	Dropped   int64      `json:"dropped"`
	LastFlush *time.Time `json:"last_flush,omitempty"`
	Flushes   int64      `json:"flushes"`
	SizeBytes int64      `json:"size_bytes"`
	InBackup  bool       `json:"in_backup"`
}

// Health answers the store's state.
func (s *Store) Health() Health {
	s.mu.Lock()
	defer s.mu.Unlock()
	h := Health{State: "ok", Path: s.path, Location: s.loc, Pending: len(s.queue), Dropped: s.drop, Flushes: s.flushN, InBackup: s.loc.InBackup()}
	if !s.last.IsZero() {
		last := s.last
		h.LastFlush = &last
	}
	if s.db == nil {
		h.State = "unavailable"
	} else if s.err != nil {
		h.State = "degraded"
	}
	if s.err != nil {
		h.Error = s.err.Error()
	}
	if s.path != "" {
		if st, err := os.Stat(s.path); err == nil {
			h.SizeBytes = st.Size()
		}
	}
	return h
}

// enqueue adds one write to the batch.
func (s *Store) enqueue(o op) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.queue) >= s.maxQ {
		s.queue = s.queue[1:]
		s.drop++
	}
	s.queue = append(s.queue, o)
}

// Flush writes the batch. An error leaves the batch in place for the next try and marks the
// store degraded.
func (s *Store) Flush() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.flushLocked()
}

func (s *Store) flushLocked() error {
	if s.paused {
		return nil
	}
	if s.db == nil {
		return s.err
	}
	if len(s.queue) == 0 {
		return nil
	}
	// an unlinked file (a stick pulled, a user who deleted it) still takes writes through the
	// open handle; nothing of that would ever be read again, so the path is checked first
	if _, err := os.Stat(s.path); err != nil {
		return s.fail(err)
	}
	tx, err := s.db.Begin()
	if err != nil {
		return s.fail(err)
	}
	for _, o := range s.queue {
		if err := o(tx); err != nil {
			tx.Rollback()
			return s.fail(err)
		}
	}
	if err := tx.Commit(); err != nil {
		return s.fail(err)
	}
	s.queue = nil
	s.err = nil
	s.last = time.Now()
	s.flushN++
	return nil
}

func (s *Store) fail(err error) error {
	if s.err == nil || s.err.Error() != err.Error() {
		s.logf("store: flush failed, %d rows kept in RAM: %v", len(s.queue), err)
	}
	s.err = err
	// the file may be gone (a stick pulled): the next flush says so again, the handle stays
	if _, serr := os.Stat(s.path); serr != nil {
		s.err = fmt.Errorf("%s is not there any more: %w", s.path, serr)
	}
	return s.err
}

// Run flushes every interval until ctx ends, then once more; it also retries an unavailable
// location every 30 s.
func (s *Store) Run(ctx context.Context, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	retry := time.NewTicker(30 * time.Second)
	defer retry.Stop()
	for {
		select {
		case <-ctx.Done():
			s.Flush()
			return
		case <-t.C:
			s.Flush()
		case <-retry.C:
			s.Retry()
		}
	}
}

// ---- rows ----------------------------------------------------------------------------------

// Device is one row of devices.
type Device struct {
	Interface string `json:"interface"`
	Address   string `json:"address"`
	Model     string `json:"model"`
	Firmware  string `json:"firmware"`
	Power     string `json:"power"`
	VoltageDP string `json:"voltage_dp"`
	LowBatDP  string `json:"lowbat_dp"`
	FirstSeen int64  `json:"first_seen"`
	LastSeen  int64  `json:"last_seen"`
	RetiredAt int64  `json:"retired_at,omitempty"`
}

// Current is one row of current: the last values of a device.
type Current struct {
	Interface     string   `json:"interface"`
	Address       string   `json:"address"`
	Voltage       *float64 `json:"voltage"`
	VoltageTS     int64    `json:"voltage_ts,omitempty"`
	VoltageStatus string   `json:"voltage_status,omitempty"`
	LowBat        *bool    `json:"low_bat"`
	LowBatSince   int64    `json:"low_bat_since,omitempty"`
	Unreach       *bool    `json:"unreach"`
	UnreachSince  int64    `json:"unreach_since,omitempty"`
	Booted        *bool    `json:"booted,omitempty"`
	LastReport    int64    `json:"last_report,omitempty"`
}

// Sample is one row of samples.
type Sample struct {
	Interface string
	Address   string
	TS        int64
	Voltage   *float64
	LowBat    *bool
	Source    string
}

// Event is one row of events.
type Event struct {
	Interface string
	Address   string
	TS        int64
	Key       string
	Value     string
}

// UpsertDevice queues a device row; an existing row keeps its first_seen and override.
func (s *Store) UpsertDevice(d Device) {
	s.enqueue(func(tx *sql.Tx) error {
		_, err := tx.Exec(`INSERT INTO devices (interface, address, model, firmware, power, voltage_dp, lowbat_dp, first_seen, last_seen, retired_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, NULL)
ON CONFLICT (interface, address) DO UPDATE SET model = excluded.model, firmware = excluded.firmware, power = excluded.power,
  voltage_dp = excluded.voltage_dp, lowbat_dp = excluded.lowbat_dp, last_seen = excluded.last_seen, retired_at = NULL`,
			d.Interface, d.Address, d.Model, d.Firmware, d.Power, d.VoltageDP, d.LowBatDP, d.FirstSeen, d.LastSeen)
		return err
	})
}

// RetireDevice queues the retirement of a device that left the system.
func (s *Store) RetireDevice(iface, address string, at int64) {
	s.enqueue(func(tx *sql.Tx) error {
		_, err := tx.Exec(`UPDATE devices SET retired_at = ? WHERE interface = ? AND address = ? AND retired_at IS NULL`, at, iface, address)
		return err
	})
}

// PutCurrent queues the current row of a device.
func (s *Store) PutCurrent(c Current) {
	s.enqueue(func(tx *sql.Tx) error {
		_, err := tx.Exec(`INSERT INTO current (interface, address, voltage, voltage_ts, voltage_status, low_bat, low_bat_since, unreach, unreach_since, booted, last_report)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (interface, address) DO UPDATE SET voltage = excluded.voltage, voltage_ts = excluded.voltage_ts, voltage_status = excluded.voltage_status,
  low_bat = excluded.low_bat, low_bat_since = excluded.low_bat_since, unreach = excluded.unreach, unreach_since = excluded.unreach_since,
  booted = excluded.booted, last_report = excluded.last_report`,
			c.Interface, c.Address, c.Voltage, nz(c.VoltageTS), c.VoltageStatus, boolInt(c.LowBat), nz(c.LowBatSince), boolInt(c.Unreach), nz(c.UnreachSince), boolInt(c.Booted), nz(c.LastReport))
		return err
	})
}

// AddSample queues a sample row; a second row at the same millisecond replaces the first.
func (s *Store) AddSample(r Sample) {
	s.enqueue(func(tx *sql.Tx) error {
		_, err := tx.Exec(`INSERT OR REPLACE INTO samples (interface, address, ts, voltage, low_bat, source) VALUES (?, ?, ?, ?, ?, ?)`,
			r.Interface, r.Address, r.TS, r.Voltage, boolInt(r.LowBat), r.Source)
		return err
	})
}

// AddEvent queues an evidence event.
func (s *Store) AddEvent(e Event) {
	s.enqueue(func(tx *sql.Tx) error {
		_, err := tx.Exec(`INSERT INTO events (interface, address, ts, key, value) VALUES (?, ?, ?, ?, ?)`, e.Interface, e.Address, e.TS, e.Key, e.Value)
		return err
	})
}

func nz(v int64) any {
	if v == 0 {
		return nil
	}
	return v
}

func boolInt(b *bool) any {
	if b == nil {
		return nil
	}
	if *b {
		return 1
	}
	return 0
}

// ErrUnavailable is answered by a read while the file is not open.
var ErrUnavailable = errors.New("the database is not available")

func (s *Store) reader() (*sql.DB, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, s.err)
	}
	return s.db, nil
}

// Devices reads every device row, retired ones included.
func (s *Store) Devices(ctx context.Context) ([]Device, error) {
	db, err := s.reader()
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, `SELECT interface, address, model, firmware, power, voltage_dp, lowbat_dp, first_seen, last_seen, COALESCE(retired_at, 0) FROM devices ORDER BY interface, address`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Device
	for rows.Next() {
		var d Device
		if err := rows.Scan(&d.Interface, &d.Address, &d.Model, &d.Firmware, &d.Power, &d.VoltageDP, &d.LowBatDP, &d.FirstSeen, &d.LastSeen, &d.RetiredAt); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// Currents reads every current row.
func (s *Store) Currents(ctx context.Context) ([]Current, error) {
	db, err := s.reader()
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, `SELECT interface, address, voltage, COALESCE(voltage_ts, 0), voltage_status, low_bat, COALESCE(low_bat_since, 0), unreach, COALESCE(unreach_since, 0), booted, COALESCE(last_report, 0) FROM current`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Current
	for rows.Next() {
		var c Current
		var v sql.NullFloat64
		var lb, ur, bt sql.NullInt64
		if err := rows.Scan(&c.Interface, &c.Address, &v, &c.VoltageTS, &c.VoltageStatus, &lb, &c.LowBatSince, &ur, &c.UnreachSince, &bt, &c.LastReport); err != nil {
			return nil, err
		}
		if v.Valid {
			c.Voltage = &v.Float64
		}
		c.LowBat, c.Unreach, c.Booted = nullBool(lb), nullBool(ur), nullBool(bt)
		out = append(out, c)
	}
	return out, rows.Err()
}

func nullBool(n sql.NullInt64) *bool {
	if !n.Valid {
		return nil
	}
	b := n.Int64 != 0
	return &b
}

// LastSample reads the newest sample of a device, nil when none.
func (s *Store) LastSample(ctx context.Context, iface, address string) (*Sample, error) {
	db, err := s.reader()
	if err != nil {
		return nil, err
	}
	var r Sample
	var v sql.NullFloat64
	var lb sql.NullInt64
	err = db.QueryRowContext(ctx, `SELECT interface, address, ts, voltage, low_bat, source FROM samples WHERE interface = ? AND address = ? ORDER BY ts DESC LIMIT 1`, iface, address).
		Scan(&r.Interface, &r.Address, &r.TS, &v, &lb, &r.Source)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if v.Valid {
		r.Voltage = &v.Float64
	}
	r.LowBat = nullBool(lb)
	return &r, nil
}

// Samples reads the samples of a device in a span, oldest first.
func (s *Store) Samples(ctx context.Context, iface, address string, from, to int64) ([]Sample, error) {
	db, err := s.reader()
	if err != nil {
		return nil, err
	}
	if to == 0 {
		to = 1<<62 - 1
	}
	rows, err := db.QueryContext(ctx, `SELECT interface, address, ts, voltage, low_bat, source FROM samples WHERE interface = ? AND address = ? AND ts >= ? AND ts <= ? ORDER BY ts`, iface, address, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Sample
	for rows.Next() {
		var r Sample
		var v sql.NullFloat64
		var lb sql.NullInt64
		if err := rows.Scan(&r.Interface, &r.Address, &r.TS, &v, &lb, &r.Source); err != nil {
			return nil, err
		}
		if v.Valid {
			r.Voltage = &v.Float64
		}
		r.LowBat = nullBool(lb)
		out = append(out, r)
	}
	return out, rows.Err()
}

// Counts answers the row count of every table (for the API and for a move's check).
func (s *Store) Counts(ctx context.Context) (map[string]int64, error) {
	db, err := s.reader()
	if err != nil {
		return nil, err
	}
	return counts(ctx, db)
}

func counts(ctx context.Context, db *sql.DB) (map[string]int64, error) {
	out := map[string]int64{}
	for _, t := range Tables {
		var n int64
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+t).Scan(&n); err != nil {
			return nil, err
		}
		out[t] = n
	}
	return out, nil
}
