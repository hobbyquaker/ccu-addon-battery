package changes

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hobbyquaker/ccu-addon-battery/internal/batteries"
	"github.com/hobbyquaker/ccu-addon-battery/internal/detect"
	"github.com/hobbyquaker/ccu-addon-battery/internal/store"
)

func TestMain(m *testing.M) {
	dir, err := filepath.Abs(filepath.Join("..", "..", ".testtmp", "changes"))
	if err == nil && os.MkdirAll(dir, 0o755) == nil {
		os.Setenv("TMPDIR", dir)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

const (
	h   = int64(time.Hour / time.Millisecond)
	day = 24 * h
)

var t0 = time.Date(2026, 10, 27, 11, 0, 0, 0, time.UTC).UnixMilli()

type clock struct{ ms int64 }

func (c *clock) now() time.Time { return time.UnixMilli(c.ms) }

var wrc2 = store.Device{Interface: "HmIP-RF", Address: "000193C99510F3", Model: "HmIP-WRC2", VoltageDP: "0/OPERATING_VOLTAGE", LowBatDP: "0/LOW_BAT"}
var contact = store.Device{Interface: "BidCos-RF", Address: "NEQ0000001", Model: "HM-Sec-SC-2", LowBatDP: "0/LOWBAT"}
var plug = store.Device{Interface: "BidCos-RF", Address: "JEQ0465798", Model: "HM-LC-Sw1-Pl", LowBatDP: "0/LOWBAT"}

func setup(t *testing.T, dir string) (*Service, *store.Store, *clock) {
	t.Helper()
	if dir == "" {
		dir = t.TempDir()
	}
	st, err := store.Open(store.Config{AddonDir: dir, Log: t.Logf})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	for _, d := range []store.Device{wrc2, contact, plug} {
		d.FirstSeen, d.LastSeen = t0-60*day, t0
		st.UpsertDevice(d)
	}
	if err := st.Flush(); err != nil {
		t.Fatal(err)
	}
	c := &clock{ms: t0}
	return New(Options{Store: st, Now: c.now, Log: t.Logf}), st, c
}

func f(v float64) *float64 { return &v }
func b(v bool) *bool       { return &v }

func list(t *testing.T, s *Service, st ...string) []Change {
	t.Helper()
	l, err := s.List(context.Background(), Filter{Statuses: st})
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func TestCreateMergeDuplicate(t *testing.T) {
	s, _, c := setup(t, "")
	ctx := context.Background()
	c.ms = t0 + 10*day
	o, err := s.Create(ctx, Input{Device: wrc2.Address, At: Time(t0), Batteries: []Battery{{Count: 2, Type: "AAA", Vendor: "Varta"}}, Note: "Küche"}, "alice", false)
	if err != nil || o.Status != "created" || o.ID == 0 {
		t.Fatalf("%+v %v", o, err)
	}
	// the same change again a day later, no external id: a duplicate
	if o2, _ := s.Create(ctx, Input{Device: wrc2.Address, At: Time(t0 + day)}, "alice", false); o2.Status != "duplicate" || o2.ID != o.ID {
		t.Fatalf("%+v", o2)
	}
	// outside the window: a new one
	if o3, _ := s.Create(ctx, Input{Device: wrc2.Address, At: Time(t0 + 3*day)}, "alice", false); o3.Status != "created" {
		t.Fatalf("%+v", o3)
	}
	// external ids make an import repeatable
	in := Input{Address: contact.Address, At: Time(t0 - 300*day), Source: "import", ExternalID: "row-7"}
	if o, _ := s.Create(ctx, in, "script", false); o.Status != "created" {
		t.Fatalf("%+v", o)
	}
	if o, _ := s.Create(ctx, in, "script", false); o.Status != "duplicate" {
		t.Fatalf("%+v", o)
	}
	ch, err := s.UpdateByExternalID(ctx, "import", "row-7", Patch{Note: ptr("from the spreadsheet")})
	if err != nil || ch.Note != "from the spreadsheet" {
		t.Fatalf("%+v %v", ch, err)
	}
	// errors
	for _, bad := range []Input{
		{At: Time(t0)},
		{Device: wrc2.Address},
		{Device: "NOPE", At: Time(t0)},
		{Device: wrc2.Address, At: Time(t0 + 30*day)},
		{Device: wrc2.Address, At: Time(t0), Source: "magic"},
		{Device: wrc2.Address, At: Time(t0), Batteries: []Battery{{Count: 13}}},
	} {
		if _, err := s.Create(ctx, bad, "alice", false); err == nil {
			t.Errorf("%+v accepted", bad)
		}
	}
	// a device that left the system before the install, with its interface and model
	if o, err := s.Create(ctx, Input{Address: "OLD0000001", Interface: "BidCos-RF", Model: "HM-Sec-SC-2", At: Time(t0 - 900*day), Source: "import"}, "script", false); err != nil || o.Status != "created" {
		t.Fatalf("%+v %v", o, err)
	}
	// dry run: nothing written
	before := len(list(t, s))
	if o, err := s.Create(ctx, Input{Device: wrc2.Address, At: Time(t0 - 100*day)}, "alice", true); err != nil || o.Status != "created" {
		t.Fatalf("%+v %v", o, err)
	}
	if len(list(t, s)) != before {
		t.Fatal("a dry run wrote")
	}
	if err := s.Delete(ctx, o.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(ctx, o.ID); err != ErrNotFound {
		t.Fatalf("deleted: %v", err)
	}
}

func ptr[T any](v T) *T { return &v }

// feed runs a device's daily reports through the live path.
func feed(s *Service, c *clock, d store.Device, ts int64, v float64, booted bool) {
	ctx := context.Background()
	c.ms = ts
	s.Observe(ctx, d, detect.Obs{TS: ts, Booted: b(booted)})
	s.Observe(ctx, d, detect.Obs{TS: ts + 3, Voltage: f(v)})
	s.Observe(ctx, d, detect.Obs{TS: ts + 5, LowBat: b(false), Unreach: b(false)})
}

// The live path with the lab's re-seat: a candidate row while the window runs (not in the inbox),
// then discarded when the voltage falls back; a real change later lands in the inbox, and a
// manual entry for it merges into it.
func TestLiveDetection(t *testing.T) {
	dir := t.TempDir()
	s, _, c := setup(t, dir)
	ctx := context.Background()
	if p := s.Profile(ctx, wrc2, 0); p.Power != "battery" || batteries.Describe(p.Slots) != "2× AAA" {
		t.Fatalf("profile %+v", p)
	}
	for i := int64(0); i < 10; i++ {
		feed(s, c, wrc2, t0-10*day+i*day, 2.8, false)
	}
	feed(s, c, wrc2, t0, 3.1, true)
	if l := list(t, s, StatusCandidate); len(l) != 1 || l[0].Rule != "A+B" || l[0].Kind != KindDetected {
		t.Fatalf("candidate: %+v", l)
	}
	if len(list(t, s, StatusPending)) != 0 {
		t.Fatal("a candidate is in the inbox")
	}
	feed(s, c, wrc2, t0+day+h, 2.9, true)
	l := list(t, s, StatusDiscarded)
	if len(l) != 1 || !strings.Contains(string(l[0].Evidence), `"reason":"recovered"`) {
		t.Fatalf("discarded: %+v", l)
	}
	// a real change two days later
	feed(s, c, wrc2, t0+2*day+h, 2.85, false)
	feed(s, c, wrc2, t0+3*day, 3.22, true)
	// a restart in the window: a new service restores the open candidate from the store
	s2 := New(Options{Store: s.o.Store, Now: c.now, Log: t.Logf})
	s2.Profile(ctx, wrc2, 0)
	if s2.OpenCandidate(wrc2.Interface, wrc2.Address) == 0 {
		t.Fatal("the candidate was not restored")
	}
	feed(s2, c, wrc2, t0+4*day+h, 3.18, true)
	p := list(t, s2, StatusPending)
	if len(p) != 1 || p[0].Confidence != "high" || p[0].At != t0+3*day {
		t.Fatalf("pending: %+v", p)
	}
	// the user (or a script) records it with the batteries: it merges into the detection
	o, err := s2.Create(ctx, Input{Device: wrc2.Address, At: Time(t0 + 3*day + 5*h), Batteries: []Battery{{Count: 2, Type: "AAA", Vendor: "Varta", PriceCents: ptr(120)}}}, "alice", false)
	if err != nil || o.Status != "merged" || o.MergedInto != p[0].ID {
		t.Fatalf("%+v %v", o, err)
	}
	got, _ := s2.Get(ctx, p[0].ID)
	if got.Status != StatusAccepted || got.Kind != KindConfirmed || got.At != t0+3*day || len(got.Batteries) != 1 || got.Evidence == nil {
		t.Fatalf("merged: %+v", got)
	}
	// suggestions
	sg, err := s2.Suggest(ctx, "vendor", "var", 0)
	if err != nil || len(sg) != 1 || sg[0].Value != "Varta" || sg[0].Count != 1 {
		t.Fatalf("suggest: %+v %v", sg, err)
	}
	if sg, _ := s2.Suggest(ctx, "type", "aa", 0); len(sg) < 2 || sg[0].Value != "AAA" {
		t.Fatalf("type suggestions: %+v", sg)
	}
	if _, err := s2.Suggest(ctx, "colour", "", 0); err == nil {
		t.Fatal("unknown field")
	}
}

// LOWBAT-only devices are offered by Tick after the window; mains devices never.
func TestLiveLowBatOnlyAndMains(t *testing.T) {
	s, _, c := setup(t, "")
	ctx := context.Background()
	if p := s.Profile(ctx, plug, 0); p.Power != "mains" || p.Detects() {
		t.Fatalf("plug %+v", p)
	}
	for _, d := range []store.Device{contact, plug} {
		s.Observe(ctx, d, detect.Obs{TS: t0, LowBat: b(true)})
		s.Observe(ctx, d, detect.Obs{TS: t0 + h, LowBat: b(false)})
	}
	c.ms = t0 + day + 2*h
	s.Tick(ctx)
	p := list(t, s, StatusPending)
	if len(p) != 1 || p[0].Address != contact.Address || p[0].Rule != "C" || p[0].Confidence != "low" {
		t.Fatalf("%+v", p)
	}
	// the inbox: another date, confirmed
	ch, err := s.Update(ctx, p[0].ID, Patch{At: ptr(Time(t0 - day)), Status: ptr(StatusAccepted)})
	if err != nil || ch.At != t0-day || ch.Status != StatusAccepted {
		t.Fatalf("%+v %v", ch, err)
	}
	if _, err := s.Update(ctx, p[0].ID, Patch{Status: ptr("maybe")}); err == nil {
		t.Fatal("bad status")
	}
	if _, err := s.Update(ctx, 999, Patch{}); err != ErrNotFound {
		t.Fatal(err)
	}
}

func TestBulkCSV(t *testing.T) {
	s, _, c := setup(t, "")
	ctx := context.Background()
	c.ms = t0
	csv := "address,at,count,type,chemistry,rechargeable,vendor,product,price_cents,note,external_id\n" +
		wrc2.Address + ",2024-03-01,2,AAA,,,Varta,Longlife,95,first,x1\n" +
		wrc2.Address + ",2024-03-01,1,AA,,yes,Eneloop,,,,x1\n" + // the second slot of the same change
		contact.Address + ",2024-05-02T10:00:00Z,2,LR44,,,,,,,x2\n" +
		"NOPE,2024-05-02,1,AA,,,,,,,x3\n" +
		contact.Address + ",not a date,1,AA,,,,,,,x4\n" +
		contact.Address + ",2024-05-03,1,AA,,,,,,,\n" // within 48 h of x2, no id: a duplicate
	ins, rows, perr, err := ParseCSV(strings.NewReader(csv))
	if err != nil || len(ins) != 4 || len(perr) != 1 || perr[0].Row != 6 || len(ins[0].Batteries) != 2 || !ins[0].Batteries[1].Rechargeable || *ins[0].Batteries[0].PriceCents != 95 {
		t.Fatalf("%+v %v %+v", ins, err, perr)
	}
	// dry run
	r, err := s.Bulk(ctx, ins, rows, "script", true, false)
	if err != nil || r.Applied || r.Counts["created"] != 2 || r.Counts["error"] != 1 || r.Counts["duplicate"] != 1 {
		t.Fatalf("dry: %+v %v", r, err)
	}
	if len(list(t, s)) != 0 {
		t.Fatal("dry run wrote")
	}
	// atomic with an error: nothing
	r, _ = s.Bulk(ctx, ins, rows, "script", false, true)
	if r.Applied || len(list(t, s)) != 0 {
		t.Fatalf("atomic: %+v", r)
	}
	// row by row
	r, _ = s.Bulk(ctx, ins, rows, "script", false, false)
	if !r.Applied || r.Counts["created"] != 2 || r.Rows[2].Code != "unknown-device" || r.Rows[2].Row != 5 {
		t.Fatalf("rows: %+v", r)
	}
	// again: every row a duplicate (or the same error)
	r, _ = s.Bulk(ctx, ins, rows, "script", false, false)
	if r.Counts["duplicate"] != 3 {
		t.Fatalf("again: %+v", r.Counts)
	}
	// the export round-trips
	var buf bytes.Buffer
	if err := s.ExportCSV(ctx, &buf); err != nil {
		t.Fatal(err)
	}
	ins2, _, perr2, err := ParseCSV(&buf)
	if err != nil || len(perr2) != 0 || len(ins2) != 2 || ins2[0].ExternalID != "x1" || len(ins2[0].Batteries) != 2 {
		t.Fatalf("round trip: %+v %+v %v\n%s", ins2, perr2, err, buf.String())
	}
	if _, _, _, err := ParseCSV(strings.NewReader("foo,bar\n")); err == nil {
		t.Fatal("bad header accepted")
	}
	var js bytes.Buffer
	if err := s.ExportJSON(ctx, &js, "test"); err != nil {
		t.Fatal(err)
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(js.Bytes(), &doc); err != nil || doc["changes"] == nil || doc["samples"] == nil {
		t.Fatalf("export.json: %v %s", err, js.String())
	}
}

// Imported samples are downsampled, never replace a stored row, and POST detect finds the
// changes in them - the WRC2 re-seat is not one.
func TestImportSamplesAndDetect(t *testing.T) {
	s, _, c := setup(t, "")
	ctx := context.Background()
	c.ms = t0 + 100*day
	var in []SampleInput
	add := func(ts int64, v float64) {
		in = append(in, SampleInput{Device: wrc2.Address, TS: Time(ts), Voltage: f(v)})
	}
	base := t0
	for i := int64(0); i < 40; i++ {
		add(base+i*day, 2.8)
		add(base+i*day+h, 2.8) // the same value an hour later: dropped by the downsampling
	}
	add(base+40*day, 3.1) // a re-seat (no BOOTED in imported data): a jump
	add(base+41*day+h, 2.82)
	for i := int64(42); i < 60; i++ {
		add(base+i*day, 2.8)
	}
	add(base+60*day, 3.25) // fresh cells
	for i := int64(61); i < 65; i++ {
		add(base+i*day, 3.2)
	}
	in = append(in, SampleInput{Device: "NOPE", TS: Time(t0), Voltage: f(3)}, SampleInput{Device: wrc2.Address}, SampleInput{Device: wrc2.Address, TS: Time(t0), Voltage: f(99)})
	r, err := s.ImportSamples(ctx, in, false)
	if err != nil || len(r.Errors) != 3 || r.Skipped != 40 || r.Stored != len(in)-3-40 {
		t.Fatalf("%+v %v", r, err)
	}
	if r2, _ := s.ImportSamples(ctx, in[:5], false); r2.Stored != 0 {
		t.Fatalf("re-import stored again: %+v", r2)
	}
	// a manual entry from the spreadsheet a day after the real change: detect merges into it
	if o, _ := s.Create(ctx, Input{Device: wrc2.Address, At: Time(base + 61*day), Source: "import", ExternalID: "x"}, "script", false); o.Status != "created" {
		t.Fatalf("%+v", o)
	}
	d, err := s.Detect(ctx, "", t0, t0+90*day, true)
	if err != nil || d.Discarded != 1 || len(d.Found) != 1 || d.Found[0].Status != "merged" {
		t.Fatalf("dry detect: %+v %v", d, err)
	}
	d, _ = s.Detect(ctx, wrc2.Address, t0, t0+90*day, false)
	if len(d.Found) != 1 || d.Found[0].At != base+60*day || d.Found[0].Confidence != "medium" {
		t.Fatalf("detect: %+v", d)
	}
	got, _ := s.Get(ctx, d.Found[0].ID)
	if got.Kind != KindConfirmed || got.At != base+60*day || got.Status != StatusAccepted {
		t.Fatalf("merged: %+v", got)
	}
	// again: a duplicate, nothing new
	if d, _ := s.Detect(ctx, "", t0, t0+90*day, false); len(d.Found) != 1 || d.Found[0].Status != "duplicate" {
		t.Fatalf("again: %+v", d)
	}
	if _, err := s.Detect(ctx, "NOPE", 0, 0, false); err == nil {
		t.Fatal("unknown device")
	}
}

func TestOverrides(t *testing.T) {
	s, _, _ := setup(t, "")
	ctx := context.Background()
	s.Profile(ctx, wrc2, 0)
	tr := true
	if err := s.SetDeviceOverride(ctx, wrc2.Interface, wrc2.Address, &batteries.Override{Rechargeable: &tr}); err != nil {
		t.Fatal(err)
	}
	if p := s.Profile(ctx, wrc2, 0); p.Source != "override" || p.Fresh != 2.66 {
		t.Fatalf("%+v", p)
	}
	if err := s.SetModelOverride(ctx, "HMIP-wrc2", &batteries.Override{Power: "external"}); err != nil {
		t.Fatal(err)
	}
	if ov := s.ModelOverrides(ctx); ov["hmip-wrc2"].Power != "external" {
		t.Fatalf("%+v", ov)
	}
	// a new service reads both back
	s2 := New(Options{Store: s.o.Store, Log: t.Logf})
	if p := s2.Profile(ctx, wrc2, 0); p.Power != "external" || p.Detects() || s2.DeviceOverride(ctx, wrc2.Interface, wrc2.Address) == nil {
		t.Fatalf("%+v", p)
	}
	if err := s2.SetModelOverride(ctx, "HmIP-WRC2", nil); err != nil {
		t.Fatal(err)
	}
	if err := s2.SetDeviceOverride(ctx, wrc2.Interface, wrc2.Address, nil); err != nil {
		t.Fatal(err)
	}
	if p := s2.Profile(ctx, wrc2, 0); p.Source != "table" {
		t.Fatalf("%+v", p)
	}
	if err := s2.SetDeviceOverride(ctx, "x", "y", &batteries.Override{}); err == nil {
		t.Fatal("unknown device")
	}
	if err := s2.SetModelOverride(ctx, "x", &batteries.Override{Power: "nuclear"}); err == nil {
		t.Fatal("invalid override")
	}
}

func TestParseTime(t *testing.T) {
	for _, in := range []string{"1793271600000", "2026-10-27T11:00:00Z", "2026-10-27"} {
		if v, err := ParseTime(in); err != nil || v <= 0 {
			t.Errorf("%s: %d %v", in, v, err)
		}
	}
	if _, err := ParseTime("yesterday"); err == nil {
		t.Fatal("parsed")
	}
	var x struct{ At Time }
	if err := json.Unmarshal([]byte(`{"At":"2026-10-27T11:00:00Z"}`), &x); err != nil || int64(x.At) != t0 {
		t.Fatalf("%d %v", x.At, err)
	}
	if err := json.Unmarshal([]byte(`{"At":`+strconv.FormatInt(t0, 10)+`}`), &x); err != nil || int64(x.At) != t0 {
		t.Fatalf("%d %v", x.At, err)
	}
	if err := json.Unmarshal([]byte(`{"At":true}`), &x); err == nil {
		t.Fatal("bool parsed")
	}
}
