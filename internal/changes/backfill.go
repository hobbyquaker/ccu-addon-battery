package changes

import (
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"errors"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/hobbyquaker/ccu-addon-battery/internal/detect"
	"github.com/hobbyquaker/ccu-addon-battery/internal/store"
)

// CSVHeader is the bulk import's CSV header: one row per slot; rows with the same address and at
// are one change. interface and model are optional extra columns (for a device the addon does
// not know).
var CSVHeader = []string{"address", "at", "count", "type", "chemistry", "rechargeable", "vendor", "product", "price_cents", "note", "external_id"}

// ParseCSV reads the bulk CSV into inputs (source import) and the line each starts on. A row
// that does not parse is answered as an error outcome at its line.
func ParseCSV(r io.Reader) ([]Input, []int, []Outcome, error) {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1
	cr.TrimLeadingSpace = true
	head, err := cr.Read()
	if err != nil {
		return nil, nil, nil, bad("invalid", "the CSV has no header: %v", err)
	}
	col := map[string]int{}
	for i, h := range head {
		col[strings.ToLower(strings.TrimSpace(strings.TrimPrefix(h, "\ufeff")))] = i
	}
	for _, need := range []string{"address", "at"} {
		if _, ok := col[need]; !ok {
			return nil, nil, nil, bad("invalid", "the CSV header lacks %q (expected %s)", need, strings.Join(CSVHeader, ","))
		}
	}
	get := func(rec []string, name string) string {
		if i, ok := col[name]; ok && i < len(rec) {
			return strings.TrimSpace(rec[i])
		}
		return ""
	}
	var ins []Input
	var lines []int
	var errs []Outcome
	idx := map[string]int{}
	line := 1
	for {
		rec, err := cr.Read()
		line++
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			errs = append(errs, Outcome{Row: line, Status: "error", Code: "invalid", Error: err.Error()})
			continue
		}
		if len(rec) == 1 && strings.TrimSpace(rec[0]) == "" {
			continue
		}
		addr := get(rec, "address")
		at, terr := ParseTime(get(rec, "at"))
		if addr == "" || terr != nil {
			msg := "address is empty"
			if terr != nil {
				msg = terr.Error()
			}
			errs = append(errs, Outcome{Row: line, Status: "error", Code: "invalid", Error: msg})
			continue
		}
		b := Battery{Type: get(rec, "type"), Chemistry: get(rec, "chemistry"), Vendor: get(rec, "vendor"), Product: get(rec, "product")}
		if v := get(rec, "count"); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil {
				errs = append(errs, Outcome{Row: line, Status: "error", Code: "invalid", Error: "count is not a number"})
				continue
			}
			b.Count = n
		}
		switch strings.ToLower(get(rec, "rechargeable")) {
		case "1", "true", "yes", "ja", "y", "x":
			b.Rechargeable = true
		}
		if v := get(rec, "price_cents"); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil {
				errs = append(errs, Outcome{Row: line, Status: "error", Code: "invalid", Error: "price_cents is not a whole number"})
				continue
			}
			b.PriceCents = &n
		}
		k := addr + "\x00" + strconv.FormatInt(at, 10)
		if i, ok := idx[k]; ok {
			b.Slot = len(ins[i].Batteries) + 1
			ins[i].Batteries = append(ins[i].Batteries, b)
			if n := get(rec, "note"); n != "" && ins[i].Note == "" {
				ins[i].Note = n
			}
			continue
		}
		in := Input{Address: addr, Interface: get(rec, "interface"), Model: get(rec, "model"), At: Time(at), Note: get(rec, "note"), Source: KindImport, ExternalID: get(rec, "external_id")}
		if b != (Battery{}) && (b.Count != 0 || b.Type != "" || b.Vendor != "" || b.Product != "" || b.PriceCents != nil) {
			b.Slot = 1
			in.Batteries = []Battery{b}
		}
		idx[k] = len(ins)
		ins = append(ins, in)
		lines = append(lines, line)
	}
	return ins, lines, errs, nil
}

// BulkResult is the answer of a bulk import.
type BulkResult struct {
	DryRun  bool           `json:"dry_run"`
	Atomic  bool           `json:"atomic"`
	Applied bool           `json:"applied"`
	Counts  map[string]int `json:"counts"`
	Rows    []Outcome      `json:"rows"`
}

// Bulk creates many changes. Row by row by default (a bad row is an error line, the others
// land); atomic applies all or nothing. dry answers what would happen.
func (s *Service) Bulk(ctx context.Context, ins []Input, rows []int, by string, dry, atomic bool) (BulkResult, error) {
	res := BulkResult{DryRun: dry, Atomic: atomic, Counts: map[string]int{}}
	rowOf := func(i int) int {
		if i < len(rows) {
			return rows[i]
		}
		return i + 1
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if atomic || dry {
		var afters []post
		failed := false
		err := s.o.Store.Write(ctx, func(tx *sql.Tx) error {
			for i, in := range ins {
				o, after, err := s.createTx(tx, in, by)
				if err != nil {
					var e *Error
					if !errors.As(err, &e) {
						return err
					}
					o = Outcome{Status: "error", Code: e.Code, Error: e.Msg}
					failed = true
				}
				o.Row = rowOf(i)
				res.Rows = append(res.Rows, o)
				if after != nil {
					afters = append(afters, after)
				}
			}
			if dry || failed && atomic {
				return errDryRun
			}
			return nil
		})
		if err != nil && !errors.Is(err, errDryRun) {
			return res, err
		}
		res.Applied = err == nil
		if !res.Applied {
			// rolled back: those ids are not there, nor is a duplicate's that points at one
			gone := map[int64]bool{}
			for i := range res.Rows {
				if res.Rows[i].Status == "created" {
					gone[res.Rows[i].ID] = true
					res.Rows[i].ID = 0
				}
			}
			for i := range res.Rows {
				if gone[res.Rows[i].ID] {
					res.Rows[i].ID, res.Rows[i].MergedInto = 0, 0
				}
			}
		}
		if res.Applied {
			for _, a := range afters {
				a()
			}
		}
	} else {
		for i, in := range ins {
			var o Outcome
			var after post
			err := s.o.Store.Write(ctx, func(tx *sql.Tx) error {
				var err error
				o, after, err = s.createTx(tx, in, by)
				return err
			})
			if err != nil {
				var e *Error
				if !errors.As(err, &e) {
					return res, err
				}
				o = Outcome{Status: "error", Code: e.Code, Error: e.Msg}
			} else if after != nil {
				after()
			}
			o.Row = rowOf(i)
			res.Rows = append(res.Rows, o)
		}
		res.Applied = true
	}
	for _, o := range res.Rows {
		res.Counts[o.Status]++
	}
	return res, nil
}

// SampleInput is one imported voltage sample.
type SampleInput struct {
	Device    string   `json:"device,omitempty"`
	Address   string   `json:"address,omitempty"`
	Interface string   `json:"interface,omitempty"`
	TS        Time     `json:"ts"`
	Voltage   *float64 `json:"voltage"`
	LowBat    *bool    `json:"low_bat,omitempty"`
}

// SamplesResult is the answer of a samples import.
type SamplesResult struct {
	DryRun   bool      `json:"dry_run"`
	Received int       `json:"received"`
	Stored   int       `json:"stored"`
	Skipped  int       `json:"skipped"` // dropped by the downsampling or already there
	Errors   []Outcome `json:"errors"`
}

// ImportSamples stores imported samples (source import), downsampled on the way in by the
// live rule (a row when the voltage moved by 0.01 V, the low-battery flag changed, or a day
// passed), never replacing a row that is there.
func (s *Service) ImportSamples(ctx context.Context, in []SampleInput, dry bool) (SamplesResult, error) {
	res := SamplesResult{DryRun: dry, Received: len(in), Errors: []Outcome{}}
	type key2 struct{ iface, addr string }
	groups := map[key2][]SampleInput{}
	db, err := s.o.Store.DB()
	if err != nil {
		return res, err
	}
	ifaceOf := map[string][]string{}
	rows, err := db.QueryContext(ctx, `SELECT interface, address FROM devices`)
	if err != nil {
		return res, err
	}
	for rows.Next() {
		var i, a string
		if rows.Scan(&i, &a) == nil {
			ifaceOf[a] = append(ifaceOf[a], i)
		}
	}
	rows.Close()
	for n, x := range in {
		addr := strings.TrimSpace(x.Address)
		if addr == "" {
			addr = strings.TrimSpace(x.Device)
		}
		if x.TS <= 0 || addr == "" || x.Voltage == nil && x.LowBat == nil {
			res.Errors = append(res.Errors, Outcome{Row: n + 1, Status: "error", Code: "invalid", Error: "device, ts and voltage or low_bat are required"})
			continue
		}
		if x.Voltage != nil && (*x.Voltage < 0 || *x.Voltage > 30) {
			res.Errors = append(res.Errors, Outcome{Row: n + 1, Status: "error", Code: "invalid", Error: "voltage out of range"})
			continue
		}
		iface := x.Interface
		if iface == "" {
			switch l := ifaceOf[addr]; len(l) {
			case 1:
				iface = l[0]
			case 0:
				res.Errors = append(res.Errors, Outcome{Row: n + 1, Status: "error", Code: "unknown-device", Error: "no device " + addr})
				continue
			default:
				res.Errors = append(res.Errors, Outcome{Row: n + 1, Status: "error", Code: "ambiguous-device", Error: addr + " is on several interfaces; name the interface"})
				continue
			}
		} else if !inList(ifaceOf[addr], iface) {
			res.Errors = append(res.Errors, Outcome{Row: n + 1, Status: "error", Code: "unknown-device", Error: "no device " + iface + " " + addr})
			continue
		}
		groups[key2{iface, addr}] = append(groups[key2{iface, addr}], x)
	}
	err = s.o.Store.Write(ctx, func(tx *sql.Tx) error {
		for k, list := range groups {
			sort.SliceStable(list, func(i, j int) bool { return list[i].TS < list[j].TS })
			var lastTS int64
			var lastV *float64
			var lastLB *bool
			var v sql.NullFloat64
			var lb sql.NullInt64
			if err := tx.QueryRow(`SELECT ts, voltage, low_bat FROM samples WHERE interface = ? AND address = ? AND ts < ? ORDER BY ts DESC LIMIT 1`, k.iface, k.addr, int64(list[0].TS)).Scan(&lastTS, &v, &lb); err == nil {
				if v.Valid {
					lastV = &v.Float64
				}
				if lb.Valid {
					b := lb.Int64 != 0
					lastLB = &b
				}
			} else if !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			for _, x := range list {
				ts := int64(x.TS)
				keep := lastTS == 0 || ts-lastTS >= store.SampleMaxGap.Milliseconds()
				if x.Voltage != nil && (lastV == nil || abs(*x.Voltage-*lastV) >= store.SampleStep-1e-9) {
					keep = true
				}
				if x.LowBat != nil && (lastLB == nil || *lastLB != *x.LowBat) {
					keep = true
				}
				if !keep {
					res.Skipped++
					continue
				}
				r, err := tx.Exec(`INSERT OR IGNORE INTO samples (interface, address, ts, voltage, low_bat, source) VALUES (?, ?, ?, ?, ?, 'import')`, k.iface, k.addr, ts, x.Voltage, boolAny(x.LowBat))
				if err != nil {
					return err
				}
				if n, _ := r.RowsAffected(); n == 0 {
					res.Skipped++ // a row is there at that ms already: it is the last one now
				} else {
					res.Stored++
				}
				lastTS = ts
				if x.Voltage != nil {
					lastV = x.Voltage
				}
				if x.LowBat != nil {
					lastLB = x.LowBat
				}
			}
		}
		if dry {
			return errDryRun
		}
		return nil
	})
	if err != nil && !errors.Is(err, errDryRun) {
		return res, err
	}
	return res, nil
}

func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}

func boolAny(b *bool) any {
	if b == nil {
		return nil
	}
	if *b {
		return 1
	}
	return 0
}

// Found is one change the detector found over stored samples.
type Found struct {
	Interface  string          `json:"interface"`
	Address    string          `json:"address"`
	At         int64           `json:"at"`
	Rule       string          `json:"rule"`
	Confidence string          `json:"confidence"`
	Status     string          `json:"status"` // created | merged | duplicate
	ID         int64           `json:"id,omitempty"`
	Evidence   detect.Evidence `json:"evidence"`
}

// DetectResult is the answer of a detection run.
type DetectResult struct {
	DryRun    bool    `json:"dry_run"`
	Devices   int     `json:"devices"`
	Found     []Found `json:"found"`
	Discarded int     `json:"discarded"` // candidates the confirmation turned down (boots, recoveries)
	Undecided int     `json:"undecided"` // candidates with no reading after the window in the span
}

// Detect runs the detector over the stored samples and evidence of a span (all devices, or one
// address) and files what it offers as pending changes in the inbox: one that a manual or
// imported change of the device within the merge window already records merges into it
// (evidence and minute), one near a detected or rejected change is a duplicate.
func (s *Service) Detect(ctx context.Context, address string, from, to int64, dry bool) (DetectResult, error) {
	res := DetectResult{DryRun: dry, Found: []Found{}}
	db, err := s.o.Store.DB()
	if err != nil {
		return res, err
	}
	if to <= 0 {
		to = s.o.Now().UnixMilli()
	}
	if from > to {
		return res, bad("invalid", "from lies after to")
	}
	q := `SELECT interface, address, model, voltage_dp, lowbat_dp, COALESCE(profile_override, '') FROM devices`
	var args []any
	if address != "" {
		q += ` WHERE address = ?`
		args = append(args, address)
	}
	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return res, err
	}
	type devRow struct{ iface, addr, model, vdp, ldp, ov string }
	var devs []devRow
	for rows.Next() {
		var d devRow
		if rows.Scan(&d.iface, &d.addr, &d.model, &d.vdp, &d.ldp, &d.ov) == nil {
			devs = append(devs, d)
		}
	}
	rows.Close()
	if address != "" && len(devs) == 0 {
		return res, bad("unknown-device", "no device %s", address)
	}
	s.mu.Lock()
	s.loadOverridesLocked(ctx)
	modelOv := s.modelOv
	s.mu.Unlock()
	params := detect.New(s.o.Params, detect.Levels{}).P
	var found []Found
	for _, d := range devs {
		devOv := s.readDeviceOverride(ctx, d.iface, d.addr)
		obs, maxV := loadObs(ctx, db, d.iface, d.addr, from-params.MaxSpan.Milliseconds(), to+params.Expire.Milliseconds(), false)
		prof := s.o.Table.Resolve(d.model, modelOv[strings.ToLower(d.model)], devOv, maxV)
		if !prof.Detects() {
			continue
		}
		res.Devices++
		lv := detect.Levels{}
		if d.vdp != "" || maxV > 0 {
			lv = detect.Levels{Fresh: prof.Fresh, Step: prof.Step}
		}
		det := detect.New(s.o.Params, lv)
		var results []detect.Result
		for _, o := range obs {
			results = append(results, det.Observe(o)...)
			results = append(results, det.Tick(o.TS)...)
		}
		end := to + params.Expire.Milliseconds()
		if now := s.o.Now().UnixMilli(); end > now {
			end = now
		}
		results = append(results, det.Tick(end)...)
		if c := det.Open(); c != nil && c.At >= from && c.At <= to {
			res.Undecided++
		}
		for _, r := range results {
			if r.Candidate.At < from || r.Candidate.At > to {
				continue
			}
			switch r.Kind {
			case detect.Discarded:
				res.Discarded++
			case detect.Offered:
				found = append(found, Found{Interface: d.iface, Address: d.addr, At: r.Candidate.At, Rule: r.Rule, Confidence: r.Confidence, Evidence: r.Candidate.Evidence})
			}
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	err = s.o.Store.Write(ctx, func(tx *sql.Tx) error {
		now := s.o.Now().UnixMilli()
		for i := range found {
			f := &found[i]
			ev := mustJSON(f.Evidence)
			if c, ok, err := s.nearLocked(tx, f.Interface, f.Address, f.At, 0, []string{KindDetected, KindConfirmed}); err != nil {
				return err
			} else if ok {
				f.Status, f.ID = "duplicate", c.ID
				continue
			}
			var rejected int64
			w := s.o.MergeWindow.Milliseconds()
			if err := tx.QueryRow(`SELECT id FROM changes WHERE interface = ? AND address = ? AND status = ? AND at >= ? AND at <= ? LIMIT 1`, f.Interface, f.Address, StatusRejected, f.At-w, f.At+w).Scan(&rejected); err == nil {
				f.Status, f.ID = "duplicate", rejected
				continue
			}
			if c, ok, err := s.nearLocked(tx, f.Interface, f.Address, f.At, 0, recordedKinds); err != nil {
				return err
			} else if ok {
				if _, err := tx.Exec(`UPDATE changes SET at = ?, kind = ?, rule = ?, confidence = ?, evidence = ?, updated_at = ? WHERE id = ?`, f.At, KindConfirmed, f.Rule, f.Confidence, ev, now, c.ID); err != nil {
					return err
				}
				f.Status, f.ID = "merged", c.ID
				continue
			}
			r, err := tx.Exec(`INSERT INTO changes (interface, address, at, kind, rule, confidence, status, evidence, batteries, source, created_by, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, '[]', 'detect', 'detect', ?, ?)`, f.Interface, f.Address, f.At, KindDetected, f.Rule, f.Confidence, StatusPending, ev, now, now)
			if err != nil {
				return err
			}
			f.Status = "created"
			f.ID, _ = r.LastInsertId()
		}
		if dry {
			return errDryRun
		}
		return nil
	})
	if err != nil && !errors.Is(err, errDryRun) {
		return res, err
	}
	res.Found = found
	if res.Found == nil {
		res.Found = []Found{}
	}
	return res, nil
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(b)
}

// Suggestion is one value already recorded, with how often.
type Suggestion struct {
	Value string `json:"value"`
	Count int    `json:"count"`
}

// Suggest answers the values recorded for a battery field (vendor, product, type, chemistry),
// most used first, matching q case-insensitively anywhere; spellings that differ only in case
// count as one, shown in their most used form. Types add the table's own. A vendor narrows the
// products to the ones recorded with it (case-insensitively).
func (s *Service) Suggest(ctx context.Context, field, q string, limit int, vendor string) ([]Suggestion, error) {
	if !inList([]string{"vendor", "product", "type", "chemistry"}, field) {
		return nil, bad("invalid", "field is vendor, product, type or chemistry")
	}
	db, err := s.o.Store.DB()
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	where := ""
	var args []any
	if vendor = strings.TrimSpace(vendor); vendor != "" && field == "product" {
		where = " AND lower(json_extract(j.value, '$.vendor')) = lower(?)"
		args = append(args, vendor)
	}
	rows, err := db.QueryContext(ctx, `SELECT json_extract(j.value, '$.`+field+`') AS v, COUNT(*) FROM changes, json_each(changes.batteries) AS j
WHERE v IS NOT NULL AND v != ''`+where+` GROUP BY v`, args...)
	if err != nil {
		return nil, err
	}
	type agg struct {
		total int
		best  string
		bestN int
	}
	by := map[string]*agg{}
	for rows.Next() {
		var v string
		var n int
		if rows.Scan(&v, &n) != nil {
			continue
		}
		k := strings.ToLower(v)
		a := by[k]
		if a == nil {
			a = &agg{}
			by[k] = a
		}
		a.total += n
		if n > a.bestN {
			a.best, a.bestN = v, n
		}
	}
	rows.Close()
	if field == "type" {
		for _, t := range s.o.Table.TypeNames() {
			if by[strings.ToLower(t)] == nil {
				by[strings.ToLower(t)] = &agg{best: t}
			}
		}
	}
	if field == "chemistry" {
		for c := range s.o.Table.Chemistries {
			if by[strings.ToLower(c)] == nil {
				by[strings.ToLower(c)] = &agg{best: c}
			}
		}
	}
	ql := strings.ToLower(strings.TrimSpace(q))
	out := []Suggestion{}
	for k, a := range by {
		if ql != "" && !strings.Contains(k, ql) {
			continue
		}
		out = append(out, Suggestion{Value: a.best, Count: a.total})
	}
	sort.Slice(out, func(i, j int) bool {
		pi, pj := strings.HasPrefix(strings.ToLower(out[i].Value), ql), strings.HasPrefix(strings.ToLower(out[j].Value), ql)
		if pi != pj {
			return pi
		}
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Value < out[j].Value
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// LastPrice is the price per battery recorded most recently for a vendor, product line and size
// (each matched case-insensitively; an empty one matches anything), with the change's time;
// found false when no accepted change carries one.
func (s *Service) LastPrice(ctx context.Context, vendor, product, typ string) (cents int, at int64, found bool, err error) {
	db, err := s.o.Store.DB()
	if err != nil {
		return 0, 0, false, err
	}
	q := `SELECT json_extract(j.value, '$.price_cents'), changes.at FROM changes, json_each(changes.batteries) AS j
WHERE changes.status = ? AND json_extract(j.value, '$.price_cents') IS NOT NULL`
	args := []any{StatusAccepted}
	for k, v := range map[string]string{"vendor": vendor, "product": product, "type": typ} {
		if v = strings.TrimSpace(v); v != "" {
			q += ` AND lower(coalesce(json_extract(j.value, '$.` + k + `'), '')) = lower(?)`
			args = append(args, v)
		}
	}
	q += ` ORDER BY changes.at DESC, changes.id DESC LIMIT 1`
	row := db.QueryRowContext(ctx, q, args...)
	var c sql.NullInt64
	if err := row.Scan(&c, &at); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, 0, false, nil
		}
		return 0, 0, false, err
	}
	return int(c.Int64), at, true, nil
}
