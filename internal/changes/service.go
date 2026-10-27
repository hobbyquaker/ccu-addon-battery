package changes

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/hobbyquaker/ccu-addon-battery/internal/batteries"
	"github.com/hobbyquaker/ccu-addon-battery/internal/detect"
	"github.com/hobbyquaker/ccu-addon-battery/internal/store"
)

// Options configure the service.
type Options struct {
	Store *store.Store
	// Table is the battery table; the shipped one by default.
	Table *batteries.Table
	// Params are the detector's windows (detect.DefaultParams by default).
	Params detect.Params
	// MergeWindow: a manual or imported change merges into a detected one of the same device
	// within +- MergeWindow, and a second one without an external id is a duplicate (48 h).
	MergeWindow time.Duration
	Now         func() time.Time
	Log         func(string, ...any)
}

// Service is the change keeper and the live detector.
type Service struct {
	o    Options
	logf func(string, ...any)

	mu      sync.Mutex
	devs    map[string]*dev // interface + "." + address
	modelOv map[string]*batteries.Override
	loaded  bool
}

type dev struct {
	d       store.Device
	devOv   *batteries.Override
	profile batteries.Profile
	det     *detect.Detector // nil when the device is not detected (mains, fixed cells)
	candID  int64            // the row of the open candidate
	maxV    float64
}

// New makes the service.
func New(o Options) *Service {
	if o.Table == nil {
		o.Table = batteries.Shipped()
	}
	if o.MergeWindow <= 0 {
		o.MergeWindow = 48 * time.Hour
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Log == nil {
		o.Log = log.Printf
	}
	return &Service{o: o, logf: o.Log, devs: map[string]*dev{}, modelOv: map[string]*batteries.Override{}}
}

// Table is the battery table in use.
func (s *Service) Table() *batteries.Table { return s.o.Table }

// Params are the detector's windows and the merge window.
func (s *Service) Params() (detect.Params, time.Duration) {
	return detect.New(s.o.Params, detect.Levels{}).P, s.o.MergeWindow
}

func key(iface, address string) string { return iface + "." + address }

// loadOverrides reads the per-model overrides once the database is there.
func (s *Service) loadOverridesLocked(ctx context.Context) {
	if s.loaded {
		return
	}
	db, err := s.o.Store.DB()
	if err != nil {
		return
	}
	rows, err := db.QueryContext(ctx, `SELECT model, value FROM model_overrides`)
	if err != nil {
		s.logf("changes: model overrides: %v", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var m, v string
		if rows.Scan(&m, &v) == nil {
			var o batteries.Override
			if json.Unmarshal([]byte(v), &o) == nil {
				s.modelOv[m] = &o
			}
		}
	}
	s.loaded = true
}

// Profile answers the profile of a device of model with the overrides in force; maxV is the
// highest voltage seen (for a model without a row). The recorder calls it when it learns a
// device and writes power and slots from it.
func (s *Service) Profile(ctx context.Context, d store.Device, maxV float64) batteries.Profile {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadOverridesLocked(ctx)
	x := s.devLocked(ctx, d)
	if maxV > x.maxV {
		x.maxV = maxV
	}
	s.applyProfileLocked(x)
	return x.profile
}

// devLocked finds or makes the device's entry; a new one restores its detector from the store.
func (s *Service) devLocked(ctx context.Context, d store.Device) *dev {
	k := key(d.Interface, d.Address)
	x := s.devs[k]
	if x == nil {
		x = &dev{d: d}
		s.devs[k] = x
		x.devOv = s.readDeviceOverride(ctx, d.Interface, d.Address)
		s.applyProfileLocked(x)
		s.restoreLocked(ctx, x)
		return x
	}
	if d.Model != "" {
		x.d.Model, x.d.Firmware = d.Model, d.Firmware
	}
	if d.VoltageDP != x.d.VoltageDP || d.LowBatDP != x.d.LowBatDP {
		x.d.VoltageDP, x.d.LowBatDP = d.VoltageDP, d.LowBatDP
	}
	return x
}

func (s *Service) readDeviceOverride(ctx context.Context, iface, address string) *batteries.Override {
	db, err := s.o.Store.DB()
	if err != nil {
		return nil
	}
	var v sql.NullString
	if err := db.QueryRowContext(ctx, `SELECT profile_override FROM devices WHERE interface = ? AND address = ?`, iface, address).Scan(&v); err != nil || !v.Valid || v.String == "" {
		return nil
	}
	var o batteries.Override
	if json.Unmarshal([]byte(v.String), &o) != nil {
		return nil
	}
	return &o
}

func (s *Service) applyProfileLocked(x *dev) {
	x.profile = s.o.Table.Resolve(x.d.Model, s.modelOv[strings.ToLower(x.d.Model)], x.devOv, x.maxV)
	if !x.profile.Detects() {
		x.det = nil
		return
	}
	lv := detect.Levels{}
	if x.d.VoltageDP != "" {
		lv = detect.Levels{Fresh: x.profile.Fresh, Step: x.profile.Step}
	}
	if x.det == nil {
		x.det = detect.New(s.o.Params, lv)
	} else {
		x.det.L = lv
	}
}

// restoreLocked replays the last span of samples and evidence events into a new detector as
// seeds and restores the open candidate and the debounce from the changes table.
func (s *Service) restoreLocked(ctx context.Context, x *dev) {
	if x.det == nil {
		return
	}
	db, err := s.o.Store.DB()
	if err != nil {
		return
	}
	now := s.o.Now().UnixMilli()
	from := now - x.det.P.MaxSpan.Milliseconds()
	obs, maxV := loadObs(ctx, db, x.d.Interface, x.d.Address, from, now, true)
	if maxV > x.maxV {
		x.maxV = maxV
		s.applyProfileLocked(x)
	}
	for _, o := range obs {
		x.det.Observe(o)
	}
	var open *detect.Candidate
	var id int64
	var ev string
	var at int64
	err = db.QueryRowContext(ctx, `SELECT id, at, COALESCE(evidence, '') FROM changes WHERE interface = ? AND address = ? AND status = ? ORDER BY at DESC LIMIT 1`,
		x.d.Interface, x.d.Address, StatusCandidate).Scan(&id, &at, &ev)
	if err == nil {
		c := detect.Candidate{At: at}
		_ = json.Unmarshal([]byte(ev), &c.Evidence)
		open, x.candID = &c, id
	}
	var last sql.NullInt64
	_ = db.QueryRowContext(ctx, `SELECT MAX(at) FROM changes WHERE interface = ? AND address = ? AND status IN (?, ?)`, x.d.Interface, x.d.Address, StatusPending, StatusAccepted).Scan(&last)
	x.det.Restore(open, last.Int64)
}

// loadObs reads the samples and evidence events of a device in a span as detector observations,
// in time order; seed marks them all as seeds (a restore), otherwise a sample's own source says.
func loadObs(ctx context.Context, db *sql.DB, iface, address string, from, to int64, seed bool) ([]detect.Obs, float64) {
	var out []detect.Obs
	var maxV float64
	rows, err := db.QueryContext(ctx, `SELECT ts, voltage, low_bat, source FROM samples WHERE interface = ? AND address = ? AND ts >= ? AND ts <= ? ORDER BY ts`, iface, address, from, to)
	if err == nil {
		for rows.Next() {
			var o detect.Obs
			var v sql.NullFloat64
			var lb sql.NullInt64
			var src string
			if rows.Scan(&o.TS, &v, &lb, &src) != nil {
				continue
			}
			if v.Valid {
				vv := v.Float64
				o.Voltage = &vv
				if vv > maxV {
					maxV = vv
				}
			}
			if lb.Valid {
				b := lb.Int64 != 0
				o.LowBat = &b
			}
			o.Seed = seed || src == "seed"
			out = append(out, o)
		}
		rows.Close()
	}
	rows, err = db.QueryContext(ctx, `SELECT ts, key, value FROM events WHERE interface = ? AND address = ? AND ts >= ? AND ts <= ? ORDER BY ts, id`, iface, address, from, to)
	if err == nil {
		for rows.Next() {
			var ts int64
			var k, v string
			if rows.Scan(&ts, &k, &v) != nil {
				continue
			}
			b := v == "true"
			o := detect.Obs{TS: ts, Seed: seed}
			switch k {
			case "BOOTED":
				// the recorder keeps only the rising edge: a false just before makes it one
				f := false
				out = append(out, detect.Obs{TS: ts - 1, Booted: &f, Seed: true})
				o.Booted = &b
			case "UNREACH":
				o.Unreach = &b
			case "LOW_BAT", "LOWBAT":
				o.LowBat = &b
			default:
				continue
			}
			out = append(out, o)
		}
		rows.Close()
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].TS < out[j].TS })
	return out, maxV
}

// Observe takes one report of a device from the recorder (live, or a seed) and acts on what the
// detector found: a candidate is written as a row at once, a decision updates it.
func (s *Service) Observe(ctx context.Context, d store.Device, o detect.Obs) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadOverridesLocked(ctx)
	x := s.devLocked(ctx, d)
	if o.Voltage != nil && *o.Voltage > x.maxV {
		x.maxV = *o.Voltage
		if x.profile.Source == "none" {
			s.applyProfileLocked(x)
		}
	}
	if x.det == nil {
		return
	}
	for _, r := range x.det.Observe(o) {
		s.persistLocked(ctx, x, r)
	}
}

// Tick lets time pass for every detector (the LOWBAT-only confirmations, the expiry).
func (s *Service) Tick(ctx context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.o.Now().UnixMilli()
	for _, x := range s.devs {
		if x.det == nil {
			continue
		}
		for _, r := range x.det.Tick(now) {
			s.persistLocked(ctx, x, r)
		}
	}
}

// Run ticks every minute until ctx ends.
func (s *Service) Run(ctx context.Context) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.Tick(ctx)
		}
	}
}

func (s *Service) persistLocked(ctx context.Context, x *dev, r detect.Result) {
	ev, _ := json.Marshal(r.Candidate.Evidence)
	now := s.o.Now().UnixMilli()
	err := s.o.Store.Write(ctx, func(tx *sql.Tx) error {
		switch r.Kind {
		case detect.Opened:
			res, err := tx.Exec(`INSERT INTO changes (interface, address, at, kind, rule, status, evidence, batteries, source, created_by, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, '[]', 'detector', 'detector', ?, ?)`, x.d.Interface, x.d.Address, r.Candidate.At, KindDetected, r.Rule, StatusCandidate, string(ev), now, now)
			if err != nil {
				return err
			}
			x.candID, _ = res.LastInsertId()
			return nil
		case detect.Attached:
			_, err := tx.Exec(`UPDATE changes SET rule = ?, evidence = ?, updated_at = ? WHERE id = ? AND status = ?`, r.Rule, string(ev), now, x.candID, StatusCandidate)
			return err
		case detect.Discarded, detect.Offered:
			id := x.candID
			x.candID = 0
			if id == 0 {
				// the candidate's row was never written (the store was away): write it now
				res, err := tx.Exec(`INSERT INTO changes (interface, address, at, kind, rule, status, evidence, batteries, source, created_by, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, '[]', 'detector', 'detector', ?, ?)`, x.d.Interface, x.d.Address, r.Candidate.At, KindDetected, r.Rule, StatusCandidate, string(ev), now, now)
				if err != nil {
					return err
				}
				id, _ = res.LastInsertId()
			}
			if r.Kind == detect.Discarded {
				_, err := tx.Exec(`UPDATE changes SET rule = ?, evidence = ?, status = ?, updated_at = ? WHERE id = ? AND status = ?`, r.Rule, string(ev), StatusDiscarded, now, id, StatusCandidate)
				return err
			}
			// a manual or imported change of the same device near it: the detection merges into it
			if other, ok, err := s.nearLocked(tx, x.d.Interface, x.d.Address, r.Candidate.At, id, recordedKinds); err != nil {
				return err
			} else if ok {
				if _, err := tx.Exec(`UPDATE changes SET at = ?, kind = ?, rule = ?, confidence = ?, evidence = ?, updated_at = ? WHERE id = ?`,
					r.Candidate.At, KindConfirmed, r.Rule, r.Confidence, string(ev), now, other.ID); err != nil {
					return err
				}
				_, err := tx.Exec(`DELETE FROM changes WHERE id = ? AND status = ?`, id, StatusCandidate)
				return err
			}
			_, err := tx.Exec(`UPDATE changes SET rule = ?, confidence = ?, evidence = ?, status = ?, updated_at = ? WHERE id = ? AND status = ?`,
				r.Rule, r.Confidence, string(ev), StatusPending, now, id, StatusCandidate)
			return err
		}
		return nil
	})
	if err != nil {
		s.logf("changes: %s %s: %s: %v", x.d.Interface, x.d.Address, r.Kind, err)
		return
	}
	if r.Kind == detect.Offered || r.Kind == detect.Discarded {
		s.logf("changes: %s %s: %s (rule %s, %s%s)", x.d.Interface, x.d.Address, r.Kind, r.Rule, r.Confidence, reasonSuffix(r.Candidate.Evidence.Reason))
	}
}

func reasonSuffix(r string) string {
	if r == "" {
		return ""
	}
	return ", " + r
}

// nearLocked finds a live change of the device within the merge window of at, other than
// except, of one of kinds.
func (s *Service) nearLocked(q querier, iface, address string, at, except int64, kinds []string) (Change, bool, error) {
	w := s.o.MergeWindow.Milliseconds()
	args := []any{iface, address, at - w, at + w, except}
	args = append(args, anys(liveStatuses)...)
	args = append(args, anys(kinds)...)
	args = append(args, at)
	c, err := scanChange(q.QueryRow(`SELECT `+changeCols+` FROM changes WHERE interface = ? AND address = ? AND at >= ? AND at <= ? AND id != ?
AND status IN (`+placeholders(len(liveStatuses))+`) AND kind IN (`+placeholders(len(kinds))+`) ORDER BY ABS(at - ?) LIMIT 1`, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return c, false, nil
	}
	return c, err == nil, err
}

// the kinds the merge rules look for
var (
	detectedKinds = []string{KindDetected}
	recordedKinds = []string{KindManual, KindImport}
	settledKinds  = []string{KindConfirmed, KindManual, KindImport}
)
