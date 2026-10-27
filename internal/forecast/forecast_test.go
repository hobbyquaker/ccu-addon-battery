package forecast

import (
	"testing"
	"time"

	"github.com/hobbyquaker/ccu-addon-battery/internal/batteries"
	"github.com/hobbyquaker/ccu-addon-battery/internal/stats"
	"github.com/hobbyquaker/ccu-addon-battery/internal/store"
)

func ms(day float64) int64 { return int64(day * dayMs) }
func f(v float64) *float64 { return &v }

func TestBasis(t *testing.T) {
	now := time.UnixMilli(ms(1000))
	own := &stats.DeviceLife{Summary: stats.Summary{Count: 3, MedianDays: 400}, RunningSince: ms(900)}
	one := &stats.DeviceLife{Summary: stats.Summary{Count: 1, MedianDays: 300}, RunningSince: ms(900)}
	model := stats.Summary{Count: 4, MedianDays: 500}
	table := batteries.Profile{LifeYears: 2}
	cases := []struct {
		name  string
		in    Input
		basis string
		exp   float64
		due   int64
		state string
	}{
		{"own lives win", Input{Life: own, Model: model, Profile: table, Now: now}, "own", 400, ms(900) + ms(400-Margin), StateOK},
		{"one own life: the model", Input{Life: one, Model: model, Profile: table, Now: now}, "model", 500, ms(900) + ms(500-Margin), StateOK},
		{"one own life, no model: that one", Input{Life: one, Profile: table, Now: now}, "own", 300, ms(900) + ms(300-Margin), StateOK},
		{"nothing recorded: the table", Input{Life: &stats.DeviceLife{RunningSince: ms(900)}, Profile: table, Now: now}, "table", 731, ms(900) + ms(731-Margin), StateOK},
		{"no change at all: no date", Input{Profile: table, Now: now}, "table", 731, 0, StateUnknown},
		{"no basis", Input{Life: &stats.DeviceLife{RunningSince: ms(900)}, Now: now}, "none", 0, 0, StateUnknown},
		{"due passed", Input{Life: &stats.DeviceLife{Summary: stats.Summary{Count: 2, MedianDays: 50}, RunningSince: ms(900)}, Now: now}, "own", 50, ms(900) + ms(50-Margin), StateDue},
		{"soon", Input{Life: &stats.DeviceLife{Summary: stats.Summary{Count: 2, MedianDays: 130}, RunningSince: ms(900)}, Now: now}, "own", 130, ms(900) + ms(130-Margin), StateSoon},
		{"low wins", Input{Life: own, LowBat: true, Now: now}, "own", 400, ms(900) + ms(400-Margin), StateLow},
	}
	for _, c := range cases {
		r := Compute(c.in, 30)
		if r.Basis != c.basis || r.ExpectedDays != c.exp || r.DueAt != c.due || r.State != c.state {
			t.Errorf("%s: %+v", c.name, r)
		}
	}
}

func TestTrend(t *testing.T) {
	now := time.UnixMilli(ms(1000))
	// 2.95 V falling 5 mV a day over 40 days: 2.75 V today, low 2.2 V -> 110 days to go
	var samples []store.Sample
	for d := 40.0; d >= 0; d -= 2 {
		samples = append(samples, store.Sample{TS: ms(1000 - d), Voltage: f(2.75 + d*0.005)})
	}
	in := Input{Profile: batteries.Profile{Low: 2.2, LifeYears: 3}, Life: &stats.DeviceLife{RunningSince: ms(700)}, Samples: samples, Now: now}
	r := Compute(in, 30)
	if r.Trend == nil || r.Trend.MVPerDay != -5 || r.Trend.Points != 21 {
		t.Fatalf("trend: %+v", r.Trend)
	}
	// the table said 3 years from day 700 (due ~ day 1782); the trend says day 1110: it wins
	if r.Basis != "trend" || r.DaysLeft < 109 || r.DaysLeft > 111 {
		t.Errorf("trend pulls the date forward: %+v", r)
	}
	// a rising or flat voltage says nothing about the date
	var flat []store.Sample
	for d := 40.0; d >= 0; d -= 2 {
		flat = append(flat, store.Sample{TS: ms(1000 - d), Voltage: f(2.9)})
	}
	r = Compute(Input{Profile: batteries.Profile{Low: 2.2, LifeYears: 3}, Life: &stats.DeviceLife{RunningSince: ms(700)}, Samples: flat, Now: now}, 30)
	if r.Trend == nil || r.Trend.ReachLowAt != 0 || r.Basis != "table" {
		t.Errorf("flat: %+v %+v", r, r.Trend)
	}
	// too few points, or too short a span: no trend
	if r := Compute(Input{Samples: samples[:4], Now: now}, 30); r.Trend != nil {
		t.Error("4 points carry no line")
	}
	short := []store.Sample{}
	for d := 10.0; d >= 0; d -= 1 {
		short = append(short, store.Sample{TS: ms(1000 - d), Voltage: f(2.8 - d*0.01)})
	}
	if r := Compute(Input{Samples: short, Now: now}, 30); r.Trend != nil {
		t.Error("10 days carry no line")
	}
	// samples older than the window are ignored
	old := []store.Sample{}
	for d := 200.0; d >= 100; d -= 5 {
		old = append(old, store.Sample{TS: ms(1000 - d), Voltage: f(2.8)})
	}
	if r := Compute(Input{Samples: old, Now: now}, 30); r.Trend != nil {
		t.Error("old samples carry no line")
	}
	// already below low: due now
	var below []store.Sample
	for d := 40.0; d >= 0; d -= 2 {
		below = append(below, store.Sample{TS: ms(1000 - d), Voltage: f(2.1 + d*0.005)})
	}
	r = Compute(Input{Profile: batteries.Profile{Low: 2.2}, Samples: below, Now: now}, 30)
	if r.State != StateDue || r.DueAt != now.UnixMilli() {
		t.Errorf("below low: %+v", r)
	}
}
