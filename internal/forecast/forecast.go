// Package forecast says when a device's batteries are due: the expected life is the median of
// the device's own past lives, with fewer than two the median of its model in this installation,
// else the battery table's typical life; due = the last change + the expected life - 14 days,
// pulled forward when the voltage trend of the last weeks reaches the device's low level
// earlier. Pure functions; the page and the notifier call Compute with what they have.
package forecast

import (
	"math"
	"time"

	"github.com/hobbyquaker/ccu-addon-battery/internal/batteries"
	"github.com/hobbyquaker/ccu-addon-battery/internal/stats"
	"github.com/hobbyquaker/ccu-addon-battery/internal/store"
)

const (
	dayMs = 24 * 3600 * 1000
	// Margin is taken off the expected life: better to buy a fortnight early than to find a dead
	// sensor.
	Margin = 14.0
	// TrendWindow is how far back the voltage regression looks, TrendMinPoints and
	// TrendMinSpanDays what it needs to say anything.
	TrendWindow      = 60 * 24 * time.Hour
	TrendMinPoints   = 6
	TrendMinSpanDays = 14.0
)

// Input is what Compute needs for one device.
type Input struct {
	Profile batteries.Profile
	// Life is the device's own lives (nil without a recorded change).
	Life *stats.DeviceLife
	// Model is the model's lives across the installation (zero without one).
	Model stats.Summary
	// Samples are the device's voltage samples of the last TrendWindow, oldest first.
	Samples []store.Sample
	LowBat  bool
	Now     time.Time
}

// Trend is the voltage regression over the last weeks.
type Trend struct {
	// MVPerDay is the slope (negative while the batteries drain).
	MVPerDay float64 `json:"mv_per_day"`
	Points   int     `json:"points"`
	SpanDays float64 `json:"span_days"`
	// ReachLowAt is when the line meets the device's low level; 0 when it does not fall.
	ReachLowAt int64 `json:"reach_low_at,omitempty"`
}

// Result is the forecast of one device.
type Result struct {
	// ExpectedDays is the life the forecast counts with; 0 when nothing is known.
	ExpectedDays float64 `json:"expected_days,omitempty"`
	// Basis says where it came from: own, model, table, none; trend when the voltage trend
	// pulled the date forward.
	Basis string `json:"basis"`
	// Lives is the number of lives behind own or model.
	Lives int `json:"lives,omitempty"`
	// DueAt is the forecast date (ms); 0 when there is none.
	DueAt    int64   `json:"due_at,omitempty"`
	DaysLeft float64 `json:"days_left,omitempty"`
	// State: low (the device says so), due (the date passed), soon (within the horizon), ok,
	// unknown (no basis).
	State string `json:"state"`
	Trend *Trend `json:"trend,omitempty"`
}

// States.
const (
	StateLow     = "low"
	StateDue     = "due"
	StateSoon    = "soon"
	StateOK      = "ok"
	StateUnknown = "unknown"
)

// Compute forecasts one device; soonDays is the horizon for "soon".
func Compute(in Input, soonDays float64) Result {
	now := in.Now
	if now.IsZero() {
		now = time.Now()
	}
	nowMs := now.UnixMilli()
	r := Result{Basis: "none"}
	switch {
	case in.Life != nil && in.Life.Count >= 2 && in.Life.MedianDays > 0:
		r.ExpectedDays, r.Basis, r.Lives = in.Life.MedianDays, "own", in.Life.Count
	case in.Model.Count >= 1 && in.Model.MedianDays > 0:
		r.ExpectedDays, r.Basis, r.Lives = in.Model.MedianDays, "model", in.Model.Count
	case in.Life != nil && in.Life.Count == 1 && in.Life.MedianDays > 0:
		r.ExpectedDays, r.Basis, r.Lives = in.Life.MedianDays, "own", 1
	case in.Profile.LifeYears > 0:
		r.ExpectedDays, r.Basis = math.Round(in.Profile.LifeYears*365.25), "table"
	}
	if in.Life != nil && in.Life.RunningSince > 0 && r.ExpectedDays > 0 {
		r.DueAt = in.Life.RunningSince + int64((r.ExpectedDays-Margin)*dayMs)
	}
	if t := trend(in.Samples, in.Profile.Low, nowMs); t != nil {
		r.Trend = t
		if t.ReachLowAt > 0 && (r.DueAt == 0 || t.ReachLowAt < r.DueAt) {
			r.DueAt = t.ReachLowAt
			r.Basis = "trend"
		}
	}
	if r.DueAt > 0 {
		r.DaysLeft = math.Round(float64(r.DueAt-nowMs)/dayMs*10) / 10
	}
	switch {
	case in.LowBat:
		r.State = StateLow
	case r.DueAt == 0:
		r.State = StateUnknown
	case r.DaysLeft <= 0:
		r.State = StateDue
	case r.DaysLeft <= soonDays:
		r.State = StateSoon
	default:
		r.State = StateOK
	}
	return r
}

// trend fits a line through the voltage samples of the last TrendWindow (a least-squares
// regression) and says when it meets low; nil when the samples cannot carry a line.
func trend(samples []store.Sample, low float64, nowMs int64) *Trend {
	from := nowMs - TrendWindow.Milliseconds()
	var xs, ys []float64
	for _, s := range samples {
		if s.Voltage == nil || s.TS < from || s.TS > nowMs {
			continue
		}
		xs = append(xs, float64(s.TS-from)/dayMs)
		ys = append(ys, *s.Voltage)
	}
	n := len(xs)
	if n < TrendMinPoints {
		return nil
	}
	span := xs[n-1] - xs[0]
	if span < TrendMinSpanDays {
		return nil
	}
	var sx, sy, sxx, sxy float64
	for i := range xs {
		sx += xs[i]
		sy += ys[i]
		sxx += xs[i] * xs[i]
		sxy += xs[i] * ys[i]
	}
	den := float64(n)*sxx - sx*sx
	if den == 0 {
		return nil
	}
	slope := (float64(n)*sxy - sx*sy) / den // V per day
	intercept := (sy - slope*sx) / float64(n)
	t := &Trend{MVPerDay: math.Round(slope*1000*100) / 100, Points: n, SpanDays: math.Round(span*10) / 10}
	if slope < 0 && low > 0 {
		// the day (since from) the line meets low, from the fitted line's value today
		today := xs[n-1]
		vToday := intercept + slope*today
		if vToday > low {
			days := (vToday - low) / -slope
			t.ReachLowAt = nowMs + int64(days*dayMs)
		} else {
			t.ReachLowAt = nowMs
		}
	}
	return t
}
