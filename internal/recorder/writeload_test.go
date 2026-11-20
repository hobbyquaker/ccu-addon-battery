package recorder

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/hobbyquaker/ccu-addon-battery/internal/store"
	"github.com/hobbyquaker/ccu-addon-kit/go/litesource"
)

// loadSystem is a simulated installation for the write-load measurement: 100 battery devices -
// 30 HmIP thermostats reporting every 3 minutes, 50 other HmIP devices every 20 minutes, 20 BidCos
// thermostats every 3 minutes (BATTERY_STATE on channel 4, LOWBAT on :0). Each report carries the
// device's whole maintenance status, as the interface processes send it; the voltage sits at a
// value and, in 3 % of the reports, flips by the 0.1 V resolution the devices report in.
type loadDev struct {
	iface, addr string
	every       time.Duration
	bidcos      bool
	base, v     float64
	next        time.Time
}

func loadSystem(t0 time.Time, rnd *rand.Rand) []*loadDev {
	var out []*loadDev
	add := func(n int, prefix string, every time.Duration, bidcos bool) {
		for i := 0; i < n; i++ {
			iface := "HmIP-RF"
			if bidcos {
				iface = "BidCos-RF"
			}
			base := 2.7 + float64(rnd.Intn(5))/10
			out = append(out, &loadDev{iface: iface, addr: fmt.Sprintf("%s%04d", prefix, i), every: every, bidcos: bidcos, base: base, v: base,
				next: t0.Add(time.Duration(rnd.Int63n(int64(every))))})
		}
	}
	add(30, "ETRV", 3*time.Minute, false)
	add(50, "SWDO", 20*time.Minute, false)
	add(20, "RTDN", 3*time.Minute, true)
	return out
}

// newLoadRecorder makes a recorder on st with the simulated devices learned, without a source.
func newLoadRecorder(st *store.Store, devs []*loadDev, now func() time.Time) *Recorder {
	r := New(Options{Store: st, Log: func(string, ...any) {}, Now: now})
	for _, d := range devs {
		dev := store.Device{Interface: d.iface, Address: d.addr, Model: "HmIP-eTRV-2", Power: "battery", VoltageDP: "0/OPERATING_VOLTAGE", LowBatDP: "0/LOW_BAT",
			FirstSeen: now().UnixMilli(), LastSeen: now().UnixMilli()}
		if d.bidcos {
			dev.Model, dev.VoltageDP, dev.LowBatDP = "HM-CC-RT-DN", "4/BATTERY_STATE", "0/LOWBAT"
		}
		r.devs[d.iface+"."+d.addr] = &device{Device: dev, tracked: true, hmip: !d.bidcos, cur: store.Current{Interface: d.iface, Address: d.addr}}
		st.UpsertDevice(dev)
	}
	return r
}

func (d *loadDev) report(r *Recorder, at time.Time, rnd *rand.Rand) {
	if rnd.Float64() < 0.03 {
		if d.v == d.base {
			d.v = d.base - 0.1
		} else {
			d.v = d.base
		}
	}
	ev := func(addr, key string, v any) {
		b, _ := json.Marshal(v)
		r.apply(litesource.Event{Interface: d.iface, Address: addr, Key: key, Value: b, TS: at, Confirmed: true, Source: "event"}, true)
	}
	v, _ := strconv.ParseFloat(strconv.FormatFloat(d.v, 'f', 1, 64), 64)
	if d.bidcos {
		ev(d.addr+":4", "BATTERY_STATE", v)
		ev(d.addr+":0", "LOWBAT", false)
		ev(d.addr+":0", "UNREACH", false)
		return
	}
	ev(d.addr+":0", "OPERATING_VOLTAGE", v)
	ev(d.addr+":0", "OPERATING_VOLTAGE_STATUS", 0)
	ev(d.addr+":0", "LOW_BAT", false)
	ev(d.addr+":0", "UNREACH", false)
}

// runLoad plays hours of reports into a store at dir, flushing at the addon's interval, and
// answers the number of flushes that wrote a transaction.
func runLoad(t testing.TB, dir string, hours int, flush time.Duration) (commits int64, h store.Health) {
	t0 := time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC)
	clock := t0
	now := func() time.Time { return clock }
	st, err := store.Open(store.Config{AddonDir: dir, Log: func(string, ...any) {}})
	if err != nil {
		t.Fatal(err)
	}
	rnd := rand.New(rand.NewSource(1))
	devs := loadSystem(t0, rnd)
	r := newLoadRecorder(st, devs, now)
	if err := st.Flush(); err != nil {
		t.Fatal(err)
	}
	start := st.Health().Flushes
	end := t0.Add(time.Duration(hours) * time.Hour)
	for w := t0; w.Before(end); w = w.Add(flush) {
		we := w.Add(flush)
		for _, d := range devs {
			for d.next.Before(we) {
				clock = d.next
				d.report(r, d.next, rnd)
				d.next = d.next.Add(d.every)
			}
		}
		clock = we
		if err := st.Flush(); err != nil {
			t.Fatal(err)
		}
	}
	h = st.Health()
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	return h.Flushes - start, h
}

// TestWriteLoadPrefill gives the database below BATTERY_WRITELOAD_PREFILL the history of a
// system that has run for a while (two years of downsampled samples, about 2,000 rows a device),
// so a measurement sees the B-tree of a grown file and not the few pages of a new one.
func TestWriteLoadPrefill(t *testing.T) {
	dir := os.Getenv("BATTERY_WRITELOAD_PREFILL")
	if dir == "" {
		t.Skip("BATTERY_WRITELOAD_PREFILL is not set")
	}
	t0 := time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC)
	st, err := store.Open(store.Config{AddonDir: dir, Log: func(string, ...any) {}})
	if err != nil {
		t.Fatal(err)
	}
	rnd := rand.New(rand.NewSource(2))
	devs := loadSystem(t0, rnd)
	newLoadRecorder(st, devs, func() time.Time { return t0.Add(-2 * 365 * 24 * time.Hour) })
	err = st.Write(context.Background(), func(tx *sql.Tx) error {
		for _, d := range devs {
			for i := 0; i < 2000; i++ {
				v := d.base - float64(rnd.Intn(3))/10
				if _, err := tx.Exec(`INSERT INTO samples (interface, address, ts, voltage, low_bat, source) VALUES (?, ?, ?, ?, 0, 'live')`,
					d.iface, d.addr, t0.Add(-time.Duration(2000-i)*9*time.Hour).UnixMilli(), v); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
}

// TestWriteLoadMeasure plays a day of the simulated installation into a database below
// BATTERY_WRITELOAD (a directory), for a measurement of what reaches the disk (strace, or the
// file sizes). Skipped without it.
func TestWriteLoadMeasure(t *testing.T) {
	dir := os.Getenv("BATTERY_WRITELOAD")
	if dir == "" {
		t.Skip("BATTERY_WRITELOAD is not set")
	}
	hours := 24
	if v, err := strconv.Atoi(os.Getenv("BATTERY_WRITELOAD_HOURS")); err == nil && v > 0 {
		hours = v
	}
	flush := 10 * time.Minute
	if v, err := time.ParseDuration(os.Getenv("BATTERY_WRITELOAD_FLUSH")); err == nil && v > 0 {
		flush = v
	}
	commits, h := runLoad(t, dir, hours, flush)
	t.Logf("%d h, flush every %s: %d write transactions, database %d bytes", hours, flush, commits, h.SizeBytes)
}

// A report that repeats a device's values queues nothing: only its timestamps moved, and they
// wait in RAM for CurrentRefresh or the stop. A changed value, a sample row or the refresh queue
// the current row.
func TestWriteOnlyChanges(t *testing.T) {
	t0 := time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC)
	clock := t0
	st, err := store.Open(store.Config{AddonDir: t.TempDir(), Log: t.Logf})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	d := &loadDev{iface: "HmIP-RF", addr: "ETRV0001", every: 3 * time.Minute, base: 2.9, v: 2.9}
	r := newLoadRecorder(st, []*loadDev{d}, func() time.Time { return clock })
	report := func(v float64) {
		b, _ := json.Marshal(v)
		for _, e := range []litesource.Event{
			{Interface: d.iface, Address: d.addr + ":0", Key: "OPERATING_VOLTAGE", Value: b, TS: clock},
			{Interface: d.iface, Address: d.addr + ":0", Key: "LOW_BAT", Value: json.RawMessage("false"), TS: clock},
			{Interface: d.iface, Address: d.addr + ":0", Key: "UNREACH", Value: json.RawMessage("false"), TS: clock},
		} {
			r.apply(e, true)
		}
	}
	report(2.9) // the first report: a sample and the current row
	if err := st.Flush(); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 50; i++ {
		clock = clock.Add(3 * time.Minute)
		report(2.9)
	}
	if p := st.Health().Pending; p != 0 {
		t.Fatalf("150 minutes of the same values queued %d rows", p)
	}
	clock = clock.Add(3 * time.Minute)
	report(2.7) // a new value beyond the flap band: a sample and the current row
	if p := st.Health().Pending; p != 2 {
		t.Fatalf("a changed voltage queued %d rows, want 2", p)
	}
	st.Flush()
	clock = clock.Add(CurrentRefresh)
	report(2.7) // the same value, but the row's timestamps are older than CurrentRefresh
	if p := st.Health().Pending; p != 1 {
		t.Fatalf("the refresh queued %d rows, want 1", p)
	}
	st.Flush()
	clock = clock.Add(3 * time.Minute)
	report(2.7)
	if p := st.Health().Pending; p != 0 {
		t.Fatalf("right after the refresh: %d rows", p)
	}
	r.Persist() // the stop: the lagging timestamps are queued
	if p := st.Health().Pending; p != 1 {
		t.Fatalf("Persist queued %d rows, want 1", p)
	}
	st.Flush()
	curs, err := st.Currents(context.Background())
	if err != nil || len(curs) != 1 || curs[0].LastReport != clock.UnixMilli() || *curs[0].Voltage != 2.7 {
		t.Fatalf("the current row after the stop: %v %+v", err, curs)
	}
	r.Persist()
	if p := st.Health().Pending; p != 0 {
		t.Fatalf("a second Persist queued %d rows", p)
	}
}
