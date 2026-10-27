package detect

import (
	"testing"
	"time"
)

func f(v float64) *float64 { return &v }
func b(v bool) *bool       { return &v }

const (
	h   = int64(time.Hour / time.Millisecond)
	day = 24 * h
)

// t0 is 2026-10-27 11:00 UTC, the day of the re-seat on the lab's WRC2.
var t0 = time.Date(2026, 10, 27, 11, 0, 0, 0, time.UTC).UnixMilli()

// the WRC2's levels from the table: 2x AAA alkaline
var wrc2 = Levels{Fresh: 2.96, Step: 0.24}

type step struct {
	o    Obs
	tick bool // Tick(o.TS) instead of Observe
}

func run(d *Detector, steps []step) []Result {
	var out []Result
	for _, s := range steps {
		if s.tick {
			out = append(out, d.Tick(s.o.TS)...)
		} else {
			out = append(out, d.Observe(s.o)...)
		}
	}
	return out
}

func kinds(rs []Result) []string {
	var out []string
	for _, r := range rs {
		out = append(out, r.Kind)
	}
	return out
}

func last(rs []Result) Result {
	if len(rs) == 0 {
		return Result{}
	}
	return rs[len(rs)-1]
}

// a device's daily status report: the fields arrive as separate events a few ms apart
func report(ts int64, v float64, low, booted bool) []step {
	return []step{
		{o: Obs{TS: ts, Booted: b(booted)}},
		{o: Obs{TS: ts + 3, Voltage: f(v)}},
		{o: Obs{TS: ts + 5, LowBat: b(low), Unreach: b(false)}},
	}
}

// days of daily reports at voltage v, starting at ts
func daily(ts int64, n int, v float64) []step {
	var out []step
	for i := 0; i < n; i++ {
		out = append(out, report(ts+int64(i)*day, v, false, false)...)
	}
	return out
}

func seq(parts ...[]step) []step {
	var out []step
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

// The measurement of 2026-10-27: the same half-used cells re-seated after ~10 s. BOOTED went
// true with the first telegram, 2.8 -> 3.1 V, no UNREACH; the recovered voltage then sags back.
// That is a boot, never a change.
func TestWRC2ReseatIsNotAChange(t *testing.T) {
	d := New(DefaultParams(), wrc2)
	reseat := t0 + 26*60*1000
	rs := run(d, seq(
		daily(t0-20*day, 20, 2.8),               // the baseline
		report(reseat, 3.1, false, true),        // re-seated: BOOTED, the recovery jump
		report(reseat+60_000, 3.1, false, true), // key presses: BOOTED stays true
		report(reseat+6*h, 3.0, false, true),
		report(reseat+25*h, 2.9, false, true), // the next day's report: back towards 2.8
	))
	if got := kinds(rs); len(got) < 2 || got[0] != Opened || last(rs).Kind != Discarded {
		t.Fatalf("results %v", got)
	}
	r := last(rs)
	if r.Candidate.Evidence.Reason != "recovered" || *r.Candidate.Evidence.VoltageBefore != 2.8 || *r.Candidate.Evidence.VoltageAfter != 3.1 || *r.Candidate.Evidence.VoltageConfirmed != 2.9 || !r.Candidate.Evidence.Booted {
		t.Fatalf("evidence %+v", r.Candidate.Evidence)
	}
	if r.Rule != "A+B" {
		t.Fatalf("rule %q", r.Rule)
	}
	if d.Open() != nil {
		t.Fatal("still open")
	}
	// the decision does not debounce: a real change the day after is still found
	rs = run(d, seq(report(reseat+48*h, 2.85, false, false), report(reseat+50*h, 3.2, false, true), daily(reseat+75*h, 2, 3.15)))
	if last(rs).Kind != Offered || last(rs).Confidence != "high" {
		t.Fatalf("after the re-seat: %v %+v", kinds(rs), last(rs))
	}
}

// The re-seat with a baseline of 2.9 V: 3.1 is no jump of a step over the maximum (3.14), so only
// BOOTED makes the candidate; the voltage at the window decides it the same way.
func TestReseatBootedOnly(t *testing.T) {
	d := New(DefaultParams(), wrc2)
	rs := run(d, seq(daily(t0-10*day, 10, 2.9), report(t0, 3.1, false, true), report(t0+24*h+60_000, 2.92, false, true)))
	if r := last(rs); r.Kind != Discarded || r.Rule != "A" || r.Candidate.Evidence.Reason != "boot" {
		t.Fatalf("%v %+v", kinds(rs), r)
	}
}

// Fresh cells: the voltage holds the fresh level a day later.
func TestRealChange(t *testing.T) {
	d := New(DefaultParams(), wrc2)
	change := t0 + 10*h
	rs := run(d, seq(
		daily(t0-30*day, 30, 2.45),
		report(change, 3.22, false, true),
		report(change+day+h, 3.18, false, true),
	))
	r := last(rs)
	if r.Kind != Offered || r.Confidence != "high" || r.Candidate.At != change || r.Rule != "A+B" {
		t.Fatalf("%v %+v", kinds(rs), r)
	}
	if *r.Candidate.Evidence.VoltageConfirmed != 3.18 || r.Candidate.Evidence.WindowS != 86400 {
		t.Fatalf("evidence %+v", r.Candidate.Evidence)
	}
	// debounce: a second jump within 24 h of the change is ignored
	if rs := run(d, report(change+day+2*h, 3.4, false, false)); len(rs) != 0 {
		t.Fatalf("debounced: %v", kinds(rs))
	}
}

// A jump without BOOTED (a BidCos thermostat's BATTERY_STATE) is medium; with an UNREACH gap that
// ended within the hour before it is high.
func TestJumpConfidence(t *testing.T) {
	d := New(DefaultParams(), Levels{Fresh: 2.96, Step: 0.24})
	var s []step
	for i := 0; i < 100; i++ {
		s = append(s, step{o: Obs{TS: t0 + int64(i)*10*60_000, Voltage: f(2.3)}})
	}
	at := t0 + 2*day
	s = append(s, step{o: Obs{TS: at, Voltage: f(3.1)}}, step{o: Obs{TS: at + day + 1, Voltage: f(3.05)}})
	if r := last(run(d, s)); r.Kind != Offered || r.Confidence != "medium" || r.Rule != "B" {
		t.Fatalf("%+v", r)
	}
	d = New(DefaultParams(), Levels{Fresh: 2.96, Step: 0.24})
	s = s[:100]
	s = append(s, step{o: Obs{TS: at - 30*60_000, Unreach: b(true)}}, step{o: Obs{TS: at - 60_000, Unreach: b(false)}}, step{o: Obs{TS: at, Voltage: f(3.1)}}, step{o: Obs{TS: at + day + 1, Voltage: f(3.05)}})
	r := last(run(d, s))
	if r.Kind != Offered || r.Confidence != "high" || r.Candidate.Evidence.UnreachGapS != 29*60 {
		t.Fatalf("%+v", r)
	}
}

// Temperature: an outdoor sensor's voltage swings by 0.15 V between night and noon; the 30-day
// maximum keeps that from ever triggering.
func TestTemperatureSwingIsNoTrigger(t *testing.T) {
	d := New(DefaultParams(), Levels{Fresh: 2.96, Step: 0.24})
	var s []step
	for i := 0; i < 60; i++ {
		v := 2.85
		if i%2 == 1 {
			v = 3.0
		}
		s = append(s, step{o: Obs{TS: t0 + int64(i)*12*h, Voltage: f(v)}})
	}
	if rs := run(d, s); len(rs) != 0 {
		t.Fatalf("%v", kinds(rs))
	}
}

// LOWBAT-only (a BidCos contact): the flag clearing is a candidate; it is offered when it stays
// clear through the window, with low confidence, and discarded when it comes back.
func TestLowBatOnly(t *testing.T) {
	d := New(DefaultParams(), Levels{})
	rs := run(d, []step{
		{o: Obs{TS: t0, LowBat: b(false)}},
		{o: Obs{TS: t0 + 10*day, LowBat: b(true)}},
		{o: Obs{TS: t0 + 12*day, LowBat: b(false)}},
		{o: Obs{TS: t0 + 12*day + 23*h}, tick: true},
	})
	if got := kinds(rs); len(got) != 1 || got[0] != Opened {
		t.Fatalf("before the window: %v", got)
	}
	rs = d.Tick(t0 + 13*day)
	if r := last(rs); r.Kind != Offered || r.Confidence != "low" || r.Rule != "C" {
		t.Fatalf("%+v", r)
	}
	// flapping: the flag clears on a warm afternoon and comes back at night
	d = New(DefaultParams(), Levels{})
	rs = run(d, []step{
		{o: Obs{TS: t0, LowBat: b(true)}},
		{o: Obs{TS: t0 + 6*h, LowBat: b(false)}},
		{o: Obs{TS: t0 + 14*h, LowBat: b(true)}},
		{o: Obs{TS: t0 + 3*day}, tick: true},
	})
	if r := last(rs); r.Kind != Discarded || r.Candidate.Evidence.Reason != "low-again" {
		t.Fatalf("flapping: %v %+v", kinds(rs), r)
	}
}

// Seeds never trigger: the start-up read after a restart reports a value that differs from the
// last one stored, BOOTED included.
func TestSeedsNeverTrigger(t *testing.T) {
	d := New(DefaultParams(), wrc2)
	rs := run(d, []step{
		{o: Obs{TS: t0, Voltage: f(2.4), LowBat: b(true), Booted: b(false), Seed: true}},
		{o: Obs{TS: t0 + day, Voltage: f(3.2), LowBat: b(false), Booted: b(true), Seed: true}},
	})
	if len(rs) != 0 {
		t.Fatalf("%v", kinds(rs))
	}
	// and the first live report of a device the detector has never seen does not either
	d = New(DefaultParams(), wrc2)
	if rs := run(d, report(t0, 3.2, false, true)); len(rs) != 0 {
		t.Fatalf("first report: %v", kinds(rs))
	}
}

// Rule D: after days of silence, the first report is at the fresh level and well above the last.
func TestSilenceThenFresh(t *testing.T) {
	d := New(DefaultParams(), wrc2)
	// the maximum of the last 30 days is high (an old peak), so B does not fire
	rs := run(d, seq(report(t0-20*day, 3.25, false, false), daily(t0-10*day, 5, 2.8), []step{{o: Obs{TS: t0, Voltage: f(3.05)}}}, daily(t0+day+h, 1, 3.04)))
	if r := last(rs); r.Kind != Offered || r.Rule != "D" || r.Confidence != "low" || r.Candidate.Evidence.SilenceS < 5*86400 {
		t.Fatalf("%v %+v", kinds(rs), r)
	}
}

// A voltage device that goes silent after the trigger is offered, unconfirmed, after Expire.
func TestUnconfirmedAfterExpire(t *testing.T) {
	d := New(DefaultParams(), wrc2)
	run(d, seq(daily(t0-5*day, 5, 2.4), report(t0, 3.2, false, true)))
	if rs := d.Tick(t0 + 6*day); len(rs) != 0 {
		t.Fatalf("too early: %v", kinds(rs))
	}
	r := last(d.Tick(t0 + 7*day + h))
	if r.Kind != Offered || r.Confidence != "low" || r.Candidate.Evidence.Reason != "unconfirmed" {
		t.Fatalf("%+v", r)
	}
}

// The low-battery flag clearing on a voltage device is confirmed by the voltage (medium).
func TestLowBatClearedWithVoltage(t *testing.T) {
	d := New(DefaultParams(), wrc2)
	rs := run(d, seq(daily(t0-5*day, 5, 2.9), []step{{o: Obs{TS: t0 - h, LowBat: b(true)}}, {o: Obs{TS: t0, LowBat: b(false)}}}, daily(t0+day+h, 1, 3.1)))
	if r := last(rs); r.Kind != Offered || r.Rule != "C" || r.Confidence != "medium" {
		t.Fatalf("%v %+v", kinds(rs), r)
	}
}

// A configurable window: 48 h instead of 24 h waits for the second day's report.
func TestWindowParameter(t *testing.T) {
	p := DefaultParams()
	p.Window = 48 * time.Hour
	d := New(p, wrc2)
	rs := run(d, seq(daily(t0-5*day, 5, 2.4), report(t0, 3.2, false, true), report(t0+day+h, 3.15, false, true)))
	if last(rs).Kind != Attached && last(rs).Kind != Opened {
		if len(rs) > 1 {
			t.Fatalf("decided after one day with a 48 h window: %v", kinds(rs))
		}
	}
	if d.Open() == nil {
		t.Fatal("decided too early")
	}
	if r := last(run(d, report(t0+2*day+h, 3.12, false, true))); r.Kind != Offered {
		t.Fatalf("%+v", r)
	}
}

// Restore after a restart: the open candidate survives and is decided by the next reading.
func TestRestore(t *testing.T) {
	d := New(DefaultParams(), wrc2)
	rs := run(d, seq(daily(t0-5*day, 5, 2.4), report(t0, 3.2, false, true)))
	open := d.Open()
	if open == nil || last(rs).Kind != Attached {
		t.Fatalf("%v", kinds(rs))
	}
	d2 := New(DefaultParams(), wrc2)
	d2.Restore(open, 0)
	run(d2, []step{{o: Obs{TS: t0 + h, Voltage: f(3.2), Booted: b(true), Seed: true}}})
	if r := last(run(d2, report(t0+day+h, 3.18, false, true))); r.Kind != Offered || r.Candidate.At != t0 {
		t.Fatalf("%+v", r)
	}
	d2.SetLastChange(t0 + 2*day)
	if rs := run(d2, report(t0+2*day+h, 3.5, false, false)); len(rs) != 0 {
		t.Fatal("not debounced from a manual change")
	}
	d2.Drop()
}
