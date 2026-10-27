// Package detect is the battery change detector: pure, no I/O, no clock. It takes one device's
// reports in time order (Observe) and the passing of time (Tick) and answers what it found.
//
// A trigger - BOOTED going true (rule A), a voltage jump over the 30-day maximum to the fresh
// level (B), the low-battery flag clearing (C), the first report at a fresh level after days of
// silence (D) - makes a *candidate*. Re-seating the same half-used cells sets BOOTED and lifts
// their voltage for a while (2.8 -> 3.1 V measured on an HmIP-WRC2), so a candidate is only
// offered when the voltage still holds the fresh level a confirmation window later; otherwise it
// is discarded as a boot or a recovery. A device without a voltage is offered when its
// low-battery flag stayed clear through the window. Nothing here books a change: an offered
// candidate goes into the user's inbox. docs/detection.md describes the rules.
package detect

import (
	"math"
	"strings"
	"time"
)

// Params are the detector's windows.
type Params struct {
	// Window is how long after the last trigger the voltage must still hold the fresh level
	// (24 h by default). A device reporting once a day is decided at its first report after it.
	Window time.Duration
	// Debounce: one change per device per Debounce (24 h); a trigger within it of an offered
	// change is ignored, a trigger while a candidate is open attaches to it.
	Debounce time.Duration
	// Expire: a voltage device with no reading after the window is offered anyway after Expire
	// (7 days), with low confidence and the reason "unconfirmed".
	Expire time.Duration
	// Silence is rule D's "days without a report" (2 days).
	Silence time.Duration
	// GapBefore: an UNREACH gap that ended within GapBefore of a jump makes it high confidence
	// (1 h).
	GapBefore time.Duration
	// MaxSpan is the span of the voltage maximum rule B compares with (30 days).
	MaxSpan time.Duration
}

// DefaultParams are the defaults named above.
func DefaultParams() Params {
	return Params{Window: 24 * time.Hour, Debounce: 24 * time.Hour, Expire: 7 * 24 * time.Hour, Silence: 48 * time.Hour, GapBefore: time.Hour, MaxSpan: 30 * 24 * time.Hour}
}

func (p Params) withDefaults() Params {
	d := DefaultParams()
	if p.Window <= 0 {
		p.Window = d.Window
	}
	if p.Debounce <= 0 {
		p.Debounce = d.Debounce
	}
	if p.Expire <= 0 {
		p.Expire = d.Expire
	}
	if p.Expire < p.Window {
		p.Expire = p.Window
	}
	if p.Silence <= 0 {
		p.Silence = d.Silence
	}
	if p.GapBefore <= 0 {
		p.GapBefore = d.GapBefore
	}
	if p.MaxSpan <= 0 {
		p.MaxSpan = d.MaxSpan
	}
	return p
}

// Levels are the device's pack levels (from its battery profile). Fresh 0 means the device
// reports no voltage the detector can use; only rules A and C run then.
type Levels struct {
	Fresh float64 `json:"fresh,omitempty"`
	Step  float64 `json:"step,omitempty"`
}

// Obs is one report of a device: the fields that came, nil for the others. Seed marks a value
// that was read or restored rather than reported live (the start-up read, a sweep, an entry the
// system restored): it updates the state and can confirm a candidate, but never triggers one.
type Obs struct {
	TS      int64 // Unix ms
	Voltage *float64
	LowBat  *bool
	Booted  *bool
	Unreach *bool
	Seed    bool
}

// Evidence is what the detector saw, kept with the change.
type Evidence struct {
	Triggers         []string `json:"triggers"`
	VoltageBefore    *float64 `json:"voltage_before,omitempty"`
	MaxBefore        *float64 `json:"max_before,omitempty"`
	VoltageAfter     *float64 `json:"voltage_after,omitempty"`
	VoltageConfirmed *float64 `json:"voltage_confirmed,omitempty"`
	ConfirmedAt      int64    `json:"confirmed_at,omitempty"`
	LowBatBefore     *bool    `json:"low_bat_before,omitempty"`
	Booted           bool     `json:"booted,omitempty"`
	UnreachGapS      int64    `json:"unreach_gap_s,omitempty"`
	SilenceS         int64    `json:"silence_s,omitempty"`
	Fresh            float64  `json:"fresh,omitempty"`
	Step             float64  `json:"step,omitempty"`
	WindowS          int64    `json:"window_s,omitempty"`
	LastTrigger      int64    `json:"last_trigger"`
	// Reason says why a candidate was discarded (boot, recovered, low-again) or offered
	// without a confirmation (unconfirmed).
	Reason string `json:"reason,omitempty"`
}

// Candidate is a possible change waiting for its confirmation.
type Candidate struct {
	At       int64    `json:"at"`
	Evidence Evidence `json:"evidence"`
}

// Kinds of results.
const (
	Opened    = "candidate" // a new candidate opened
	Attached  = "attached"  // a trigger attached to the open candidate
	Offered   = "pending"   // the candidate goes into the inbox
	Discarded = "discarded" // logged as a boot or a recovery, not a change
)

// Result is what Observe and Tick found.
type Result struct {
	Kind       string
	Candidate  Candidate
	Rule       string // the rules that fired, "A+B"
	Confidence string // high | medium | low, on Offered
}

// Detector is one device's detector.
type Detector struct {
	P Params
	L Levels

	hist         []point
	lastV        *float64
	lowBat       *bool
	booted       *bool
	unreach      *bool
	unreachSince int64
	gapEnd       int64
	gapS         int64
	lastReport   int64
	burst        int64 // the first report after a silence, and its silence
	burstSilence int64
	open         *Candidate
	lastChange   int64
}

type point struct {
	ts int64
	v  float64
}

// burstSpan: the fields of one status report arrive as separate events within this span.
const burstSpan = 10_000

const eps = 1e-6

// New makes a detector.
func New(p Params, l Levels) *Detector { return &Detector{P: p.withDefaults(), L: l} }

// Open is the open candidate, nil when none.
func (d *Detector) Open() *Candidate {
	if d.open == nil {
		return nil
	}
	c := *d.open
	return &c
}

// Restore sets what a restart would have lost: the open candidate and the time of the last
// change (for the debounce). The reports since then are replayed by Observe with Seed set.
func (d *Detector) Restore(open *Candidate, lastChange int64) {
	if open != nil {
		c := *open
		d.open = &c
	}
	d.lastChange = lastChange
}

// SetLastChange tells the detector about a change the user or an import recorded, so the
// debounce counts from it.
func (d *Detector) SetLastChange(at int64) {
	if at > d.lastChange {
		d.lastChange = at
	}
}

// Drop forgets the open candidate (the user decided it, or a manual change took its place).
func (d *Detector) Drop() { d.open = nil }

func (d *Detector) max30(ts int64) *float64 {
	var m *float64
	for _, p := range d.hist {
		if p.ts < ts-d.P.MaxSpan.Milliseconds() || p.ts >= ts {
			continue
		}
		if m == nil || p.v > *m {
			v := p.v
			m = &v
		}
	}
	return m
}

// Observe takes one report and answers what it found (most often nothing).
func (d *Detector) Observe(o Obs) []Result {
	var out []Result
	// a silence before this report starts a burst rule D looks at
	if d.lastReport > 0 && o.TS-d.lastReport >= d.P.Silence.Milliseconds() {
		d.burst, d.burstSilence = o.TS, o.TS-d.lastReport
	}
	var trig []string
	if !o.Seed && d.lastReport > 0 {
		if o.Booted != nil && *o.Booted && d.booted != nil && !*d.booted {
			trig = append(trig, "A")
		}
		if o.Voltage != nil && d.L.Fresh > 0 {
			v := *o.Voltage
			if m := d.max30(o.TS); m != nil && v >= *m+d.L.Step-eps && v >= d.L.Fresh-eps {
				trig = append(trig, "B")
			} else if d.burst > 0 && o.TS-d.burst <= burstSpan && d.lastV != nil && v >= d.L.Fresh-eps && v >= *d.lastV+d.L.Step/2-eps {
				trig = append(trig, "D")
			}
		}
		if o.LowBat != nil && !*o.LowBat && d.lowBat != nil && *d.lowBat {
			trig = append(trig, "C")
		}
	}
	if d.open != nil && o.LowBat != nil && *o.LowBat && !o.Seed {
		// the flag came back: whatever lifted the voltage, it was not fresh cells
		out = append(out, d.decide(o.TS, nil, "low-again"))
	}
	if len(trig) > 0 {
		out = append(out, d.trigger(o, trig)...)
	}
	if d.open != nil && o.Voltage != nil && d.open.Evidence.VoltageAfter == nil && o.TS >= d.open.At {
		// the first reading after a trigger that came without one (BOOTED before the voltage)
		v := *o.Voltage
		d.open.Evidence.VoltageAfter = &v
		out = append(out, Result{Kind: Attached, Candidate: *d.open, Rule: strings.Join(d.open.Evidence.Triggers, "+")})
	}
	// the state
	if o.Voltage != nil {
		v := *o.Voltage
		d.lastV = &v
		d.hist = append(d.hist, point{o.TS, v})
		cut := o.TS - d.P.MaxSpan.Milliseconds()
		i := 0
		for i < len(d.hist) && d.hist[i].ts < cut {
			i++
		}
		d.hist = d.hist[i:]
	}
	if o.LowBat != nil {
		b := *o.LowBat
		d.lowBat = &b
	}
	if o.Booted != nil {
		b := *o.Booted
		d.booted = &b
	}
	if o.Unreach != nil {
		b := *o.Unreach
		if b && (d.unreach == nil || !*d.unreach) {
			d.unreachSince = o.TS
		}
		if !b && d.unreach != nil && *d.unreach && d.unreachSince > 0 {
			d.gapEnd, d.gapS = o.TS, (o.TS-d.unreachSince)/1000
		}
		d.unreach = &b
	}
	if o.TS > d.lastReport {
		d.lastReport = o.TS
	}
	// a reading after the window decides a voltage candidate
	if d.open != nil && d.L.Fresh > 0 && o.Voltage != nil && o.TS >= d.open.Evidence.LastTrigger+d.P.Window.Milliseconds() {
		v := *o.Voltage
		out = append(out, d.decide(o.TS, &v, ""))
	}
	return out
}

func (d *Detector) trigger(o Obs, trig []string) []Result {
	if d.open != nil {
		ev := &d.open.Evidence
		for _, t := range trig {
			if !has(ev.Triggers, t) {
				ev.Triggers = append(ev.Triggers, t)
			}
		}
		if has(trig, "A") {
			ev.Booted = true
		}
		if ev.VoltageAfter == nil && o.Voltage != nil {
			ev.VoltageAfter = copyF(o.Voltage)
		}
		if o.TS-d.open.At <= d.P.Debounce.Milliseconds() && o.TS > ev.LastTrigger {
			ev.LastTrigger = o.TS
		}
		return []Result{{Kind: Attached, Candidate: *d.open, Rule: strings.Join(ev.Triggers, "+")}}
	}
	if d.lastChange > 0 && o.TS-d.lastChange < d.P.Debounce.Milliseconds() && o.TS >= d.lastChange {
		return nil
	}
	ev := Evidence{Triggers: trig, VoltageBefore: copyF(d.lastV), MaxBefore: d.max30(o.TS), LowBatBefore: copyB(d.lowBat),
		Booted: has(trig, "A"), Fresh: d.L.Fresh, Step: d.L.Step, WindowS: int64(d.P.Window / time.Second), LastTrigger: o.TS, VoltageAfter: copyF(o.Voltage)}
	if d.gapEnd > 0 && o.TS-d.gapEnd <= d.P.GapBefore.Milliseconds() {
		ev.UnreachGapS = d.gapS
	}
	if d.burst > 0 && o.TS-d.burst <= burstSpan {
		ev.SilenceS = d.burstSilence / 1000
	}
	d.open = &Candidate{At: o.TS, Evidence: ev}
	return []Result{{Kind: Opened, Candidate: *d.open, Rule: strings.Join(trig, "+")}}
}

// Tick lets time pass: a device without a voltage is offered when the window passed without the
// low-battery flag coming back; a voltage device with no reading after the window is offered,
// unconfirmed, after Expire.
func (d *Detector) Tick(now int64) []Result {
	if d.open == nil {
		return nil
	}
	ev := d.open.Evidence
	if d.L.Fresh == 0 && now >= ev.LastTrigger+d.P.Window.Milliseconds() {
		return []Result{d.decide(now, nil, "")}
	}
	if d.L.Fresh > 0 && now >= ev.LastTrigger+d.P.Expire.Milliseconds() {
		return []Result{d.decide(now, nil, "unconfirmed")}
	}
	return nil
}

func (d *Detector) decide(ts int64, v *float64, reason string) Result {
	c := *d.open
	d.open = nil
	ev := &c.Evidence
	ev.ConfirmedAt = ts
	rule := strings.Join(ev.Triggers, "+")
	if reason == "low-again" {
		ev.Reason = reason
		return Result{Kind: Discarded, Candidate: c, Rule: rule}
	}
	if reason == "unconfirmed" {
		ev.Reason = reason
		d.lastChange = c.At
		return Result{Kind: Offered, Candidate: c, Rule: rule, Confidence: "low"}
	}
	lowBefore := ev.LowBatBefore != nil && *ev.LowBatBefore
	if v == nil {
		// no voltage: the flag stayed clear through the window
		d.lastChange = c.At
		conf := "low"
		if has(ev.Triggers, "A") && lowBefore {
			conf = "high"
		}
		return Result{Kind: Offered, Candidate: c, Rule: rule, Confidence: conf}
	}
	vv := *v
	ev.VoltageConfirmed = &vv
	before := ev.VoltageBefore
	if before == nil {
		before = ev.MaxBefore
	}
	held := vv >= d.L.Fresh-eps && (before == nil || vv >= *before+d.L.Step/2-eps)
	if !held {
		ev.Reason = "recovered"
		if !has(ev.Triggers, "B") && !has(ev.Triggers, "D") {
			ev.Reason = "boot"
		}
		// the readings of the recovery were no level of the cells: out of the maximum rule B
		// compares with, or they would mask a real change in the next 30 days
		kept := d.hist[:0]
		for _, p := range d.hist {
			if p.ts < c.At || p.ts >= ts {
				kept = append(kept, p)
			}
		}
		d.hist = kept
		return Result{Kind: Discarded, Candidate: c, Rule: rule}
	}
	d.lastChange = c.At
	jump := before == nil || vv >= *before+d.L.Step-eps
	conf := "low"
	switch {
	case has(ev.Triggers, "A") && (jump || lowBefore):
		conf = "high"
	case has(ev.Triggers, "B") && ev.UnreachGapS > 0:
		conf = "high"
	case has(ev.Triggers, "A"), has(ev.Triggers, "B"), has(ev.Triggers, "C"):
		conf = "medium"
	}
	return Result{Kind: Offered, Candidate: c, Rule: rule, Confidence: conf}
}

func has(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

func copyF(f *float64) *float64 {
	if f == nil {
		return nil
	}
	v := *f
	return &v
}

func copyB(b *bool) *bool {
	if b == nil {
		return nil
	}
	v := *b
	return &v
}

// Round rounds a voltage to the 0.01 V the devices report, for display.
func Round(v float64) float64 { return math.Round(v*100) / 100 }
