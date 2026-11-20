package recorder

import (
	"context"
	"encoding/json"
	"math"
	"testing"
	"time"

	"github.com/hobbyquaker/ccu-addon-battery/internal/batteries"
	"github.com/hobbyquaker/ccu-addon-battery/internal/forecast"
	"github.com/hobbyquaker/ccu-addon-battery/internal/store"
	"github.com/hobbyquaker/ccu-addon-kit/go/litesource"
)

// flapRep is one report in a flap filter case: the device, when, the voltage (NaN: none) and the
// low-battery flag (nil: not in the report).
type flapRep struct {
	dev string
	at  time.Duration
	v   float64
	low *bool
}

type flapRow struct {
	at time.Duration
	v  float64
}

func yes() *bool { b := true; return &b }

// The flap filter: a change of at most 0.1 V is recorded once it held for FlapHold (12 h) over two
// reports, at the time it came; a flip back records nothing; a larger change and a low-battery
// transition are recorded at once.
func TestFlapFilter(t *testing.T) {
	h := time.Hour
	m := time.Minute
	cases := []struct {
		name string
		reps []flapRep
		want map[string][]flapRow
	}{
		{"flips back and forth: nothing", []flapRep{
			{"A", 0, 3.0, nil}, {"A", 3 * m, 2.9, nil}, {"A", 6 * m, 3.0, nil}, {"A", 2 * h, 2.9, nil}, {"A", 3 * h, 3.0, nil},
			{"A", 5 * h, 2.9, nil}, {"A", 10 * h, 3.0, nil}, {"A", 12 * h, 3.0, nil},
		}, map[string][]flapRow{"A": {{0, 3.0}}}},
		{"a step down that holds: recorded when it came", []flapRep{
			{"A", 0, 3.0, nil}, {"A", 1 * h, 2.9, nil}, {"A", 7 * h, 2.9, nil}, {"A", 13 * h, 2.9, nil}, {"A", 14 * h, 2.9, nil},
		}, map[string][]flapRow{"A": {{0, 3.0}, {1 * h, 2.9}}}},
		{"a step down with flips inside the hold", []flapRep{
			{"A", 0, 3.0, nil}, {"A", 1 * h, 2.9, nil}, {"A", 2 * h, 3.0, nil}, {"A", 3 * h, 2.9, nil}, {"A", 15 * h, 2.9, nil},
		}, map[string][]flapRow{"A": {{0, 3.0}, {3 * h, 2.9}}}},
		{"a flip to the other side restarts the wait", []flapRep{
			{"A", 0, 3.0, nil}, {"A", 1 * h, 2.9, nil}, {"A", 2 * h, 3.1, nil}, {"A", 7 * h, 3.1, nil}, {"A", 14 * h, 3.1, nil},
		}, map[string][]flapRow{"A": {{0, 3.0}, {2 * h, 3.1}}}},
		{"one report is not enough, however long", []flapRep{
			{"A", 0, 3.0, nil}, {"A", 1 * h, 2.9, nil}, {"A", 20 * h, 3.0, nil},
		}, map[string][]flapRow{"A": {{0, 3.0}}}},
		{"a battery change: the jump up at once", []flapRep{
			{"A", 0, 2.4, nil}, {"A", 1 * h, 3.1, nil},
		}, map[string][]flapRow{"A": {{0, 2.4}, {1 * h, 3.1}}}},
		{"a pack giving out: the drop at once", []flapRep{
			{"A", 0, 2.6, nil}, {"A", 1 * h, 2.4, nil},
		}, map[string][]flapRow{"A": {{0, 2.6}, {1 * h, 2.4}}}},
		{"a low-battery flag while a change waits: at once, with the voltage it came with", []flapRep{
			{"A", 0, 3.0, nil}, {"A", 1 * h, 2.9, nil}, {"A", 2 * h, math.NaN(), yes()},
		}, map[string][]flapRow{"A": {{0, 3.0}, {2 * h, 2.9}}}},
		{"a device reporting once a day: the change replaces the daily row", []flapRep{
			{"A", 0, 3.0, nil}, {"A", 24 * h, 2.9, nil}, {"A", 48 * h, 2.9, nil},
		}, map[string][]flapRow{"A": {{0, 3.0}, {24 * h, 2.9}}}},
		{"the daily row keeps the recorded value while a change waits", []flapRep{
			{"A", 0, 3.0, nil}, {"A", 22 * h, 2.9, nil}, {"A", 25 * h, 2.9, nil}, {"A", 29 * h, 2.9, nil}, {"A", 35 * h, 2.9, nil},
		}, map[string][]flapRow{"A": {{0, 3.0}, {25 * h, 3.0}, {35 * h, 2.9}}}},
		{"mixed devices keep their own waits", []flapRep{
			{"A", 0, 3.0, nil}, {"B", 0, 2.8, nil},
			{"A", 1 * h, 2.9, nil}, {"B", 1 * h, 2.7, nil},
			{"A", 2 * h, 3.0, nil}, {"B", 2 * h, 2.7, nil},
			{"A", 3 * h, 2.9, nil}, {"B", 13 * h, 2.7, nil}, {"B", 14 * h, 3.2, nil},
			{"A", 4 * h, 3.0, nil},
		}, map[string][]flapRow{"A": {{0, 3.0}}, "B": {{0, 2.8}, {1 * h, 2.7}, {14 * h, 3.2}}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t0 := time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC)
			st, err := store.Open(store.Config{AddonDir: t.TempDir(), Log: func(string, ...any) {}})
			if err != nil {
				t.Fatal(err)
			}
			defer st.Close()
			devs := []*loadDev{{iface: "HmIP-RF", addr: "A"}, {iface: "HmIP-RF", addr: "B"}}
			r := newLoadRecorder(st, devs, func() time.Time { return t0 })
			for _, rp := range c.reps {
				at := t0.Add(rp.at)
				if !math.IsNaN(rp.v) {
					b, _ := json.Marshal(rp.v)
					r.apply(litesource.Event{Interface: "HmIP-RF", Address: rp.dev + ":0", Key: "OPERATING_VOLTAGE", Value: b, TS: at}, true)
				}
				low := false
				if rp.low != nil {
					low = *rp.low
				}
				b, _ := json.Marshal(low)
				r.apply(litesource.Event{Interface: "HmIP-RF", Address: rp.dev + ":0", Key: "LOW_BAT", Value: b, TS: at}, true)
			}
			if err := st.Flush(); err != nil {
				t.Fatal(err)
			}
			for _, dev := range []string{"A", "B"} {
				rows, err := st.Samples(context.Background(), "HmIP-RF", dev, 0, 0)
				if err != nil {
					t.Fatal(err)
				}
				want := c.want[dev]
				if len(rows) != len(want) {
					t.Fatalf("%s: %d rows, want %d: %s", dev, len(rows), len(want), fmtRows(t0, rows))
				}
				for i, w := range want {
					if rows[i].TS != t0.Add(w.at).UnixMilli() || rows[i].Voltage == nil || math.Abs(*rows[i].Voltage-w.v) > 1e-9 {
						t.Fatalf("%s row %d: want %v %.2f, got %s", dev, i, w.at, w.v, fmtRows(t0, rows))
					}
				}
			}
		})
	}
}

func fmtRows(t0 time.Time, rows []store.Sample) string {
	out := ""
	for _, s := range rows {
		v := math.NaN()
		if s.Voltage != nil {
			v = *s.Voltage
		}
		out += time.UnixMilli(s.TS).Sub(t0).String() + "=" + litesource.Event{Value: json.RawMessage(mustJSON(v))}.String() + " "
	}
	return out
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		return []byte("null")
	}
	return b
}

// A slow decline with the device flipping at every step (40 days, a report every 20 minutes, the
// voltage near each step for days): the history keeps every level of the fall, no jump up, a
// fraction of the flips, and the forecast still sees the fall.
func TestFlapFilterSlowTrend(t *testing.T) {
	t0 := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	st, err := store.Open(store.Config{AddonDir: t.TempDir(), Log: func(string, ...any) {}})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	r := newLoadRecorder(st, []*loadDev{{iface: "HmIP-RF", addr: "A"}}, func() time.Time { return t0 })
	reports, flips := 0, 0
	prev := 0.0
	end := 40 * 24 * time.Hour
	for at := time.Duration(0); at < end; at += 20 * time.Minute {
		// 3.04 V falling to 2.64 V; a daily swing of +-0.04 V (temperature) and the motor's load
		days := at.Hours() / 24
		trueV := 3.04 - 0.01*days + 0.04*math.Sin(2*math.Pi*days)
		if int(at/(20*time.Minute))%7 == 0 {
			trueV -= 0.03
		}
		v := math.Round(trueV*10) / 10
		if reports > 0 && v != prev {
			flips++
		}
		prev = v
		reports++
		b, _ := json.Marshal(v)
		r.apply(litesource.Event{Interface: "HmIP-RF", Address: "A:0", Key: "OPERATING_VOLTAGE", Value: b, TS: t0.Add(at)}, true)
	}
	st.Flush()
	rows, _ := st.Samples(context.Background(), "HmIP-RF", "A", 0, 0)
	changes, levels := 0, map[float64]bool{*rows[0].Voltage: true}
	for i := 1; i < len(rows); i++ {
		d := *rows[i].Voltage - *rows[i-1].Voltage
		if d > FlapBand+1e-9 {
			t.Fatalf("the history jumped up at row %d as at a battery change: %s", i, fmtRows(t0, rows))
		}
		if math.Abs(d) > 1e-9 {
			changes++
		}
		levels[*rows[i].Voltage] = true
	}
	if len(levels) < 4 || changes > flips/5 || len(rows) > 100 {
		t.Fatalf("%d reports, %d flips: %d rows, %d changes, %d levels: %s", reports, flips, len(rows), changes, len(levels), fmtRows(t0, rows))
	}
	if last := *rows[len(rows)-1].Voltage; last > 2.7+1e-9 {
		t.Fatalf("the last recorded value %.2f lags behind the fall", last)
	}
	f := forecast.Compute(forecast.Input{Profile: batteries.Profile{Low: 2.2}, Samples: rows, Now: t0.Add(end)}, 30)
	if f.Trend == nil || f.Trend.MVPerDay > -5 || f.Trend.ReachLowAt == 0 {
		t.Fatalf("the forecast's trend: %+v", f.Trend)
	}
	t.Logf("%d reports, %d flips of the reported value: %d rows, %d changes, trend %.1f mV/day: %s", reports, flips, len(rows), changes, f.Trend.MVPerDay, fmtRows(t0, rows))
}

// After a restart the filter compares with the last recorded row, not with the current row,
// which may hold a flip the history left out.
func TestFlapFilterRestart(t *testing.T) {
	t0 := time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC)
	st, err := store.Open(store.Config{AddonDir: t.TempDir(), Log: func(string, ...any) {}})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	devs := []*loadDev{{iface: "HmIP-RF", addr: "A"}}
	newLoadRecorder(st, devs, func() time.Time { return t0 })
	three, flipped := 3.0, 2.9
	st.AddSample(store.Sample{Interface: "HmIP-RF", Address: "A", TS: t0.UnixMilli(), Voltage: &three, Source: "live"})
	st.PutCurrent(store.Current{Interface: "HmIP-RF", Address: "A", Voltage: &flipped, VoltageTS: t0.Add(time.Hour).UnixMilli()})
	st.Flush()
	r := New(Options{Store: st, Log: func(string, ...any) {}, Now: func() time.Time { return t0.Add(2 * time.Hour) }})
	r.loadStore(context.Background())
	for k, d := range r.devs {
		d.tracked = true
		if l := r.last[k]; l.Voltage == nil || *l.Voltage != 3.0 {
			t.Fatalf("the filter's reference after the restart: %+v", l)
		}
	}
	b, _ := json.Marshal(3.0)
	r.apply(litesource.Event{Interface: "HmIP-RF", Address: "A:0", Key: "OPERATING_VOLTAGE", Value: b, TS: t0.Add(2 * time.Hour)}, true)
	st.Flush()
	if rows, _ := st.Samples(context.Background(), "HmIP-RF", "A", 0, 0); len(rows) != 1 {
		t.Fatalf("back at the recorded value after a restart: %s", fmtRows(t0, rows))
	}
}
