package stats

import (
	"testing"
	"time"

	"github.com/hobbyquaker/ccu-addon-battery/internal/changes"
	"github.com/hobbyquaker/ccu-addon-battery/internal/store"
)

func ms(day int) int64 { return int64(day) * dayMs }
func p(c int) *int     { return &c }

func TestLivesAndStats(t *testing.T) {
	now := time.UnixMilli(ms(1000))
	devs := []store.Device{
		{Interface: "HmIP-RF", Address: "A", Model: "HmIP-WRC2"},
		{Interface: "HmIP-RF", Address: "B", Model: "HmIP-WRC2"},
		{Interface: "HmIP-RF", Address: "C", Model: "HmIP-eTRV-2"},
		{Interface: "HmIP-RF", Address: "R", Model: "HmIP-WRC2", RetiredAt: ms(900)},
		{Interface: "HmIP-RF", Address: "N", Model: "HmIP-SWDO"},
	}
	varta := []changes.Battery{{Slot: 1, Count: 2, Type: "AAA", Vendor: "Varta", Product: "Power on Demand", PriceCents: p(50)}}
	vartaNoPrice := []changes.Battery{{Slot: 1, Count: 2, Type: "AAA", Vendor: "varta ", Product: "power on demand"}}
	duracell := []changes.Battery{{Slot: 1, Count: 2, Type: "AA", Vendor: "Duracell", PriceCents: p(80)}}
	cs := []changes.Change{
		// A: 100 -> 300 (200 d, Varta), 300 -> 700 (400 d, Varta no price), running since 700 (300 d)
		{ID: 1, Interface: "HmIP-RF", Address: "A", At: ms(100), Batteries: varta},
		{ID: 2, Interface: "HmIP-RF", Address: "A", At: ms(300), Batteries: vartaNoPrice},
		{ID: 3, Interface: "HmIP-RF", Address: "A", At: ms(700), Batteries: varta},
		// B: one change, running since 800
		{ID: 4, Interface: "HmIP-RF", Address: "B", At: ms(800), Batteries: varta},
		// C: 0 -> 365 (Duracell), running
		{ID: 5, Interface: "HmIP-RF", Address: "C", At: ms(0), Batteries: duracell},
		{ID: 6, Interface: "HmIP-RF", Address: "C", At: ms(365), Batteries: duracell},
		// R: retired - no running life
		{ID: 7, Interface: "HmIP-RF", Address: "R", At: ms(50), Batteries: nil},
	}
	lives := Lives(cs, devs, now)
	if len(lives) != 6 {
		t.Fatalf("lives: %d, want 6: %+v", len(lives), lives)
	}
	got := map[int64][2]float64{}
	for _, l := range lives {
		got[l.ChangeID] = [2]float64{l.Days, float64(l.To)}
	}
	for id, want := range map[int64][2]float64{1: {200, float64(ms(300))}, 2: {400, float64(ms(700))}, 3: {300, 0}, 4: {200, 0}, 5: {365, float64(ms(365))}, 6: {635, 0}} {
		if got[id] != want {
			t.Errorf("life of change %d: %v, want %v", id, got[id], want)
		}
	}
	if _, ok := got[7]; ok {
		t.Error("a retired device has no running life")
	}

	st := Compute(cs, devs, Options{Now: now, LowBat: func(_, a string) bool { return a == "A" }})
	a := st.Devices["HmIP-RF.A"]
	if a.Count != 2 || a.MinDays != 200 || a.MaxDays != 400 || a.AvgDays != 300 || a.MedianDays != 300 || a.RunningDays != 300 || a.RunningSince != ms(700) {
		t.Errorf("A: %+v", a)
	}
	if a.Hint != "" {
		t.Errorf("A runs 300 of 300 days: no hint, got %q", a.Hint)
	}
	b := st.Devices["HmIP-RF.B"]
	if b.Count != 0 || b.RunningDays != 200 || b.AvgDays != 0 {
		t.Errorf("B: %+v", b)
	}
	if _, ok := st.Devices["HmIP-RF.N"]; ok {
		t.Error("a device without changes has no entry")
	}
	if _, ok := st.Devices["HmIP-RF.R"]; ok {
		t.Error("a retired device without completed lives has no entry")
	}

	// the hint: low battery while the running life is well below the model's average
	cs2 := append(append([]changes.Change(nil), cs...), changes.Change{ID: 8, Interface: "HmIP-RF", Address: "B", At: ms(950), Batteries: varta})
	st2 := Compute(cs2, devs, Options{Now: now, LowBat: func(_, a string) bool { return a == "B" }})
	bb := st2.Devices["HmIP-RF.B"]
	// B's own average is 150 (800 -> 950); running 50 d < 0.75 * 150
	if bb.Hint != "short" || bb.RunningDays != 50 {
		t.Errorf("B with a low battery: %+v", bb)
	}

	// battery rows: Varta Power on Demand AAA (two spellings are one row), Duracell AA
	if len(st.Batteries) != 2 {
		t.Fatalf("batteries: %+v", st.Batteries)
	}
	v := st.Batteries[0]
	if v.Key != "varta|power on demand|aaa" || v.Vendor != "Varta" || v.Product != "Power on Demand" || v.Type != "AAA" {
		t.Errorf("varta row: %+v", v)
	}
	if v.Total != 8 || v.Changes != 4 || v.Devices != 2 || len(v.Models) != 1 || v.Models[0] != "HmIP-WRC2" {
		t.Errorf("varta counts: %+v", v)
	}
	if v.Life.Count != 2 || v.Life.MinDays != 200 || v.Life.MaxDays != 400 || v.Life.AvgDays != 300 {
		t.Errorf("varta life: %+v", v.Life)
	}
	// prices: 3 changes with 50 ct x 2 = 6 priced batteries; cost per year: the 200 d life cost
	// 100 ct -> 183 ct/year; the 400 d life had no price
	if v.AvgPriceCents != 50 || v.PricedTotal != 6 || v.PricedLives != 1 || v.CostPerYearCents != 183 || v.LastPriceCents != 50 || v.LastAt != ms(800) {
		t.Errorf("varta prices: %+v", v)
	}
	if len(v.PerModel) != 1 || v.PerModel[0].Model != "HmIP-WRC2" || v.PerModel[0].Count != 2 || v.PerModel[0].AvgDays != 300 {
		t.Errorf("varta per model: %+v", v.PerModel)
	}
	d := st.Batteries[1]
	if d.Key != "duracell||aa" || d.Life.Count != 1 || d.Life.AvgDays != 365 || d.CostPerYearCents != 160 {
		t.Errorf("duracell: %+v", d)
	}

	// model rows
	if len(st.Models) != 2 {
		t.Fatalf("models: %+v", st.Models)
	}
	var wrc ModelRow
	for _, m := range st.Models {
		if m.Model == "HmIP-WRC2" {
			wrc = m
		}
	}
	if wrc.Model != "HmIP-WRC2" || wrc.Devices != 2 || wrc.Life.Count != 2 || len(wrc.Batteries) != 1 || wrc.Batteries[0].Key != "varta|power on demand|aaa" {
		t.Errorf("wrc2: %+v", wrc)
	}
	if len(st.Types) != 2 || st.Types[0].Type != "AA" || st.Types[1].Type != "AAA" || st.Types[1].Life.Count != 2 {
		t.Errorf("types: %+v", st.Types)
	}

	// the year: changes at day 700, 800 are within 365 days of day 1000; 365 and 300 are not
	if st.Year.Changes != 2 || st.Year.Batteries != 4 || st.Year.ByType["AAA"] != 4 || st.Year.CostCents != 200 || st.Year.PricedBatteries != 4 || st.Year.AllBatteriesEver != 12 || st.Year.FirstChange != 0 {
		t.Errorf("year: %+v", st.Year)
	}
}

func TestSummarize(t *testing.T) {
	if s := summarize(nil); s.Count != 0 || s.AvgDays != 0 {
		t.Errorf("empty: %+v", s)
	}
	s := summarize([]float64{30, 10, 20, 40})
	if s.Count != 4 || s.MinDays != 10 || s.MaxDays != 40 || s.AvgDays != 25 || s.MedianDays != 25 {
		t.Errorf("%+v", s)
	}
	if s := summarize([]float64{3, 1, 2}); s.MedianDays != 2 {
		t.Errorf("%+v", s)
	}
}

func TestLifeCost(t *testing.T) {
	c, n, ok := lifeCost([]changes.Battery{{Count: 2, PriceCents: p(50)}, {Count: 0, PriceCents: p(10)}})
	if c != 110 || n != 3 || !ok {
		t.Errorf("%d %d %v", c, n, ok)
	}
	if _, _, ok := lifeCost([]changes.Battery{{Count: 2}}); ok {
		t.Error("a slot without a price is not priced")
	}
	if _, _, ok := lifeCost(nil); ok {
		t.Error("no batteries, no price")
	}
	if perYear(100, 200) != 183 || perYear(100, 0) != 0 {
		t.Error("perYear")
	}
}
