package changes

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"github.com/hobbyquaker/ccu-addon-battery/internal/batteries"
)

var errDryRun = errors.New("dry run")

// post is what happens to the in-memory detectors once a transaction committed.
type post func()

// Create records one manual or imported change: an external id seen before is a duplicate, a
// detected change of the device within the merge window takes the batteries and the note (and
// keeps its evidence and its minute), a manual or imported one there makes this a duplicate,
// otherwise a new accepted row. dry says what would happen without writing.
func (s *Service) Create(ctx context.Context, in Input, by string, dry bool) (Outcome, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out Outcome
	var after post
	err := s.o.Store.Write(ctx, func(tx *sql.Tx) error {
		var err error
		out, after, err = s.createTx(tx, in, by)
		if err != nil {
			return err
		}
		if dry {
			return errDryRun
		}
		return nil
	})
	if errors.Is(err, errDryRun) {
		if out.Status == "created" {
			out.ID = 0 // the row was rolled back; its id would be another's
		}
		return out, nil
	}
	if err != nil {
		return Outcome{}, err
	}
	if after != nil {
		after()
	}
	return out, nil
}

func (s *Service) createTx(tx *sql.Tx, in Input, by string) (Outcome, post, error) {
	addr := in.address()
	if addr == "" {
		return Outcome{}, nil, bad("invalid", "device (the address) is required")
	}
	at := int64(in.At)
	if at <= 0 {
		return Outcome{}, nil, bad("invalid", "at is required")
	}
	now := s.o.Now().UnixMilli()
	if at > now+24*3600*1000 {
		return Outcome{}, nil, bad("invalid", "at lies in the future")
	}
	src := in.Source
	if src == "" {
		src = KindManual
	}
	if src != KindManual && src != KindImport {
		return Outcome{}, nil, bad("invalid", "source is manual or import, not %q", src)
	}
	for i, b := range in.Batteries {
		if b.Count < 0 || b.Count > 12 {
			return Outcome{}, nil, bad("invalid", "battery %d: count %d is not 0-12", i+1, b.Count)
		}
		if b.PriceCents != nil && *b.PriceCents < 0 {
			return Outcome{}, nil, bad("invalid", "battery %d: a negative price", i+1)
		}
	}
	iface, err := s.resolveDevice(tx, in, addr, at)
	if err != nil {
		return Outcome{}, nil, err
	}
	if in.ExternalID != "" {
		var id int64
		err := tx.QueryRow(`SELECT id FROM changes WHERE source = ? AND external_id = ?`, src, in.ExternalID).Scan(&id)
		if err == nil {
			return Outcome{Status: "duplicate", ID: id}, nil, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return Outcome{}, nil, err
		}
	}
	if c, ok, err := s.nearLocked(tx, iface, addr, at, 0, detectedKinds); err != nil {
		return Outcome{}, nil, err
	} else if ok {
		bat := c.Batteries
		if len(in.Batteries) > 0 {
			bat = in.Batteries
		}
		note := c.Note
		if in.Note != "" {
			note = in.Note
		}
		if _, err := tx.Exec(`UPDATE changes SET batteries = ?, note = ?, kind = ?, status = ?, source = ?, external_id = ?, updated_at = ? WHERE id = ?`,
			batteriesJSON(bat), note, KindConfirmed, StatusAccepted, src, nullStr(in.ExternalID), now, c.ID); err != nil {
			return Outcome{}, nil, err
		}
		return Outcome{Status: "merged", ID: c.ID, MergedInto: c.ID}, func() { s.settledLocked(iface, addr, c.ID, c.At) }, nil
	}
	if c, ok, err := s.nearLocked(tx, iface, addr, at, 0, settledKinds); err != nil {
		return Outcome{}, nil, err
	} else if ok {
		return Outcome{Status: "duplicate", ID: c.ID}, nil, nil
	}
	rule := ""
	if src == KindManual {
		rule = "E"
	}
	res, err := tx.Exec(`INSERT INTO changes (interface, address, at, kind, rule, status, batteries, note, source, external_id, created_by, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, iface, addr, at, src, rule, StatusAccepted, batteriesJSON(in.Batteries), in.Note, src, nullStr(in.ExternalID), by, now, now)
	if err != nil {
		return Outcome{}, nil, err
	}
	id, _ := res.LastInsertId()
	return Outcome{Status: "created", ID: id}, func() { s.settledLocked(iface, addr, 0, at) }, nil
}

// resolveDevice finds the interface of an address; a device the addon never saw is created as
// retired when the input names its interface and model.
func (s *Service) resolveDevice(tx *sql.Tx, in Input, addr string, at int64) (string, error) {
	q := `SELECT interface FROM devices WHERE address = ?`
	args := []any{addr}
	if in.Interface != "" {
		q += ` AND interface = ?`
		args = append(args, in.Interface)
	}
	rows, err := tx.Query(q, args...)
	if err != nil {
		return "", err
	}
	var found []string
	for rows.Next() {
		var i string
		if rows.Scan(&i) == nil {
			found = append(found, i)
		}
	}
	rows.Close()
	switch len(found) {
	case 1:
		return found[0], nil
	case 0:
		if in.Interface == "" || in.Model == "" {
			return "", bad("unknown-device", "no device %s; name its interface and model to record a change of a device that left the system", addr)
		}
		p := s.o.Table.Resolve(in.Model, nil, nil, 0)
		if _, err := tx.Exec(`INSERT INTO devices (interface, address, model, power, first_seen, last_seen, retired_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			in.Interface, addr, in.Model, p.Power, at, at, at); err != nil {
			return "", err
		}
		return in.Interface, nil
	default:
		return "", bad("ambiguous-device", "%s is known on %s; name the interface", addr, strings.Join(found, ", "))
	}
}

// settledLocked tells a device's detector that a change was recorded: an open candidate it
// merged into (or that was decided) is dropped, and the debounce counts from at.
func (s *Service) settledLocked(iface, addr string, candID, at int64) {
	x := s.devs[key(iface, addr)]
	if x == nil || x.det == nil {
		return
	}
	if candID != 0 && x.candID == candID {
		x.det.Drop()
		x.candID = 0
	}
	x.det.SetLastChange(at)
}

// Get answers one change.
func (s *Service) Get(ctx context.Context, id int64) (Change, error) {
	db, err := s.o.Store.DB()
	if err != nil {
		return Change{}, err
	}
	return getChange(db, id)
}

// Update edits a change: the date, the batteries, the note, the status (accepted confirms an
// inbox entry, rejected says no, pending puts it back).
func (s *Service) Update(ctx context.Context, id int64, p Patch) (Change, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out Change
	var old Change
	err := s.o.Store.Write(ctx, func(tx *sql.Tx) error {
		c, err := getChange(tx, id)
		if err != nil {
			return err
		}
		old = c
		if p.At != nil {
			at := int64(*p.At)
			if at <= 0 || at > s.o.Now().UnixMilli()+24*3600*1000 {
				return bad("invalid", "at is not a valid time")
			}
			c.At = at
		}
		if p.Batteries != nil {
			c.Batteries = *p.Batteries
			for i, b := range c.Batteries {
				if b.Count < 0 || b.Count > 12 || b.PriceCents != nil && *b.PriceCents < 0 {
					return bad("invalid", "battery %d is not valid", i+1)
				}
			}
		}
		if p.Note != nil {
			c.Note = *p.Note
		}
		if p.Status != nil {
			if !inList([]string{StatusAccepted, StatusRejected, StatusPending}, *p.Status) {
				return bad("invalid", "status is accepted, rejected or pending")
			}
			c.Status = *p.Status
		}
		c.UpdatedAt = s.o.Now().UnixMilli()
		if _, err := tx.Exec(`UPDATE changes SET at = ?, batteries = ?, note = ?, status = ?, updated_at = ? WHERE id = ?`,
			c.At, batteriesJSON(c.Batteries), c.Note, c.Status, c.UpdatedAt, id); err != nil {
			return err
		}
		out = c
		return nil
	})
	if err != nil {
		return Change{}, err
	}
	if old.Status == StatusCandidate && out.Status != StatusCandidate {
		if x := s.devs[key(out.Interface, out.Address)]; x != nil && x.det != nil && x.candID == id {
			x.det.Drop()
			x.candID = 0
		}
	}
	if out.Status == StatusAccepted {
		s.settledLocked(out.Interface, out.Address, 0, out.At)
	}
	return out, nil
}

// UpdateByExternalID edits the change a client knows by its own id.
func (s *Service) UpdateByExternalID(ctx context.Context, source, extID string, p Patch) (Change, error) {
	db, err := s.o.Store.DB()
	if err != nil {
		return Change{}, err
	}
	if source == "" {
		source = KindImport
	}
	var id int64
	if err := db.QueryRowContext(ctx, `SELECT id FROM changes WHERE source = ? AND external_id = ?`, source, extID).Scan(&id); errors.Is(err, sql.ErrNoRows) {
		return Change{}, ErrNotFound
	} else if err != nil {
		return Change{}, err
	}
	return s.Update(ctx, id, p)
}

// Delete removes a change.
func (s *Service) Delete(ctx context.Context, id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var c Change
	err := s.o.Store.Write(ctx, func(tx *sql.Tx) error {
		var err error
		if c, err = getChange(tx, id); err != nil {
			return err
		}
		_, err = tx.Exec(`DELETE FROM changes WHERE id = ?`, id)
		return err
	})
	if err != nil {
		return err
	}
	if x := s.devs[key(c.Interface, c.Address)]; x != nil && x.det != nil && x.candID == id {
		x.det.Drop()
		x.candID = 0
	}
	return nil
}

// Filter selects changes.
type Filter struct {
	Address  string
	From, To int64
	Statuses []string
	Limit    int
}

// List answers the changes, newest first.
func (s *Service) List(ctx context.Context, f Filter) ([]Change, error) {
	db, err := s.o.Store.DB()
	if err != nil {
		return nil, err
	}
	q := `SELECT ` + changeCols + ` FROM changes WHERE 1 = 1`
	var args []any
	if f.Address != "" {
		q += ` AND address = ?`
		args = append(args, f.Address)
	}
	if f.From > 0 {
		q += ` AND at >= ?`
		args = append(args, f.From)
	}
	if f.To > 0 {
		q += ` AND at <= ?`
		args = append(args, f.To)
	}
	if len(f.Statuses) > 0 {
		q += ` AND status IN (` + placeholders(len(f.Statuses)) + `)`
		args = append(args, anys(f.Statuses)...)
	}
	q += ` ORDER BY at DESC, id DESC`
	if f.Limit <= 0 || f.Limit > 10000 {
		f.Limit = 10000
	}
	q += ` LIMIT ?`
	args = append(args, f.Limit)
	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Change{}
	for rows.Next() {
		c, err := scanChange(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ---- overrides ---------------------------------------------------------------------------

// ModelOverrides answers the per-model overrides.
func (s *Service) ModelOverrides(ctx context.Context) map[string]batteries.Override {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadOverridesLocked(ctx)
	out := map[string]batteries.Override{}
	for k, v := range s.modelOv {
		out[k] = *v
	}
	return out
}

// SetModelOverride sets (nil: removes) the override of a model.
func (s *Service) SetModelOverride(ctx context.Context, model string, o *batteries.Override) error {
	model = strings.ToLower(strings.TrimSpace(model))
	if model == "" {
		return bad("invalid", "model is required")
	}
	if o != nil {
		if err := s.o.Table.Validate(*o); err != nil {
			return bad("invalid", "%v", err)
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadOverridesLocked(ctx)
	err := s.o.Store.Write(ctx, func(tx *sql.Tx) error {
		if o == nil {
			_, err := tx.Exec(`DELETE FROM model_overrides WHERE model = ?`, model)
			return err
		}
		b, _ := json.Marshal(o)
		_, err := tx.Exec(`INSERT INTO model_overrides (model, value, updated_at) VALUES (?, ?, ?) ON CONFLICT (model) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
			model, string(b), s.o.Now().UnixMilli())
		return err
	})
	if err != nil {
		return err
	}
	if o == nil {
		delete(s.modelOv, model)
	} else {
		s.modelOv[model] = o
	}
	for _, x := range s.devs {
		if strings.ToLower(x.d.Model) == model {
			s.applyProfileLocked(x)
		}
	}
	return nil
}

// SetDeviceOverride sets (nil: removes) the override of one device.
func (s *Service) SetDeviceOverride(ctx context.Context, iface, address string, o *batteries.Override) error {
	if o != nil {
		if err := s.o.Table.Validate(*o); err != nil {
			return bad("invalid", "%v", err)
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	err := s.o.Store.Write(ctx, func(tx *sql.Tx) error {
		var v any
		if o != nil {
			b, _ := json.Marshal(o)
			v = string(b)
		}
		res, err := tx.Exec(`UPDATE devices SET profile_override = ? WHERE interface = ? AND address = ?`, v, iface, address)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return bad("unknown-device", "no device %s %s", iface, address)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if x := s.devs[key(iface, address)]; x != nil {
		x.devOv = o
		s.applyProfileLocked(x)
	}
	return nil
}

// DeviceOverride answers the override of one device, nil when none.
func (s *Service) DeviceOverride(ctx context.Context, iface, address string) *batteries.Override {
	return s.readDeviceOverride(ctx, iface, address)
}

// OpenCandidate answers the row id of a device's open candidate, 0 when none.
func (s *Service) OpenCandidate(iface, address string) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	if x := s.devs[key(iface, address)]; x != nil {
		return x.candID
	}
	return 0
}
