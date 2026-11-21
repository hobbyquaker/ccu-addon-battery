package recorder

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/hobbyquaker/ccu-addon-battery/internal/batteries"
	"github.com/hobbyquaker/ccu-addon-battery/internal/changes"
	"github.com/hobbyquaker/ccu-addon-battery/internal/store"
	"github.com/hobbyquaker/ccu-addon-kit/go/litesource"
)

// supplyRig is a recorder on a fake clock with one HmIP-MP3P (battery-or-dc: 3x C or a 5 V
// supply) and one HmIP-WRC2 (batteries only), the change keeper as its observer.
type supplyRig struct {
	t   *testing.T
	t0  time.Time
	st  *store.Store
	chg *changes.Service
	r   *Recorder
}

func newSupplyRig(t *testing.T, st *store.Store) *supplyRig {
	t.Helper()
	t0 := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	if st == nil {
		var err error
		st, err = store.Open(store.Config{AddonDir: t.TempDir(), Log: t.Logf})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { st.Close() })
	}
	chg := changes.New(changes.Options{Store: st, Log: t.Logf, Now: func() time.Time { return t0 }})
	r := New(Options{Store: st, Observer: chg, Log: t.Logf, Now: func() time.Time { return t0 }})
	x := &supplyRig{t: t, t0: t0, st: st, chg: chg, r: r}
	for _, d := range []store.Device{
		{Interface: "HmIP-RF", Address: "MP3P", Model: "HmIP-MP3P", Power: "unknown", VoltageDP: "0/OPERATING_VOLTAGE", LowBatDP: "0/LOW_BAT"},
		{Interface: "HmIP-RF", Address: "WRC2", Model: "HmIP-WRC2", Power: "unknown", VoltageDP: "0/OPERATING_VOLTAGE", LowBatDP: "0/LOW_BAT"},
	} {
		d.FirstSeen, d.LastSeen = t0.UnixMilli(), t0.UnixMilli()
		st.UpsertDevice(d)
	}
	st.Flush()
	r.loadStore(context.Background())
	return x
}

func (x *supplyRig) report(addr string, at time.Duration, key string, v any) {
	b, _ := json.Marshal(v)
	x.r.apply(litesource.Event{Interface: "HmIP-RF", Address: addr + ":0", Key: key, Value: b, TS: x.t0.Add(at), Confirmed: true, Source: "event"}, true)
}

func (x *supplyRig) view(addr string) View {
	x.t.Helper()
	v := find(x.r.Devices("en"), addr)
	if v == nil {
		x.t.Fatalf("no device %s", addr)
	}
	return *v
}

// onSupply says whether the device's profile is the automatic external one.
func (x *supplyRig) onSupply(addr string) bool {
	v := x.view(addr)
	return v.Profile.Power == "external" && v.Profile.Auto && v.Power == "external"
}

func TestSupplyByVoltage(t *testing.T) {
	h, m := time.Hour, time.Minute
	type rep struct {
		at  time.Duration
		key string // OPERATING_VOLTAGE (v float) or OPERATING_VOLTAGE_STATUS (v int)
		v   any
	}
	volt := func(at time.Duration, v float64) rep { return rep{at, "OPERATING_VOLTAGE", v} }
	status := func(at time.Duration, v int) rep { return rep{at, "OPERATING_VOLTAGE_STATUS", v} }
	cases := []struct {
		name string
		reps []rep
		want bool
	}{
		{"on batteries", []rep{volt(0, 4.5), volt(2*h, 4.5), volt(4*h, 4.4)}, false},
		{"fresh cells at their maximum", []rep{volt(0, 4.8), volt(2*h, 4.8), volt(5*h, 4.8)}, false},
		{"a spike does not flip it", []rep{volt(0, 4.5), volt(1*h, 5.1), volt(3*h, 4.5), volt(5*h, 4.5)}, false},
		{"two high reports inside the hold are not enough", []rep{volt(0, 4.5), volt(1*h, 5.0), volt(1*h+30*m, 5.0)}, false},
		{"a high reading broken by one in the cells' range restarts the wait", []rep{volt(0, 5.0), volt(40*m, 4.6), volt(70*m, 5.0)}, false},
		{"one high report, however long ago, is not enough", []rep{volt(0, 5.0)}, false},
		{"a sustained 4.9 V flips it", []rep{volt(0, 4.9), volt(1*h, 4.9)}, true},
		{"a sustained 5 V flips it", []rep{volt(0, 4.5), volt(2*h, 5.0), volt(2*h+30*m, 5.0), volt(3*h, 5.0)}, true},
		{"a sustained OVERFLOW flips it", []rep{volt(0, 4.6), status(0, 2), status(1*h, 2)}, true},
		{"one OVERFLOW does not", []rep{volt(0, 4.6), status(0, 2), status(30*m, 0), status(2*h, 0)}, false},
		{"back in the cells' range flips it back", []rep{volt(0, 5.0), volt(1*h, 5.0), volt(2*h, 4.7), volt(3*h, 4.7)}, false},
		{"one dip on the supply does not flip it back", []rep{volt(0, 5.0), volt(1*h, 5.0), volt(2*h, 4.7), volt(2*h+10*m, 5.0), volt(4*h, 5.0)}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			x := newSupplyRig(t, nil)
			if v := x.view("MP3P"); v.Profile.Power != "battery-or-dc" || v.Profile.TablePower != "battery-or-dc" {
				t.Fatalf("profile before: %+v", v.Profile)
			}
			for _, r := range c.reps {
				x.report("MP3P", r.at, r.key, r.v)
				// a battery-only model never flips, whatever it reports
				x.report("WRC2", r.at, r.key, r.v)
			}
			if got := x.onSupply("MP3P"); got != c.want {
				t.Fatalf("on supply %v, want %v: %+v", got, c.want, x.view("MP3P"))
			}
			if w := x.view("WRC2"); w.Profile.Auto || w.Power == "external" {
				t.Fatalf("WRC2: %+v", w)
			}
			sup, err := x.st.OnSupply(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if _, ok := sup["HmIP-RF.MP3P"]; ok != c.want {
				t.Fatalf("store: %v", sup)
			}
		})
	}
}

func TestSupplyStateSurvivesARestartAndTheManualChoiceWins(t *testing.T) {
	ctx := context.Background()
	h := time.Hour
	x := newSupplyRig(t, nil)
	x.report("MP3P", 0, "OPERATING_VOLTAGE", 4.6)
	x.report("MP3P", 1*h, "OPERATING_VOLTAGE", 5.0)
	x.report("MP3P", 2*h, "OPERATING_VOLTAGE", 5.0)
	if !x.onSupply("MP3P") {
		t.Fatalf("%+v", x.view("MP3P"))
	}
	// on the supply: no forecast-relevant power, no detection - the jump to 5 V opened nothing
	x.st.Flush()
	if l, _ := x.chg.List(ctx, changes.Filter{Statuses: []string{changes.StatusCandidate, changes.StatusPending}}); len(l) != 0 {
		t.Fatalf("detections on the supply: %+v", l)
	}
	if p := x.view("MP3P").Profile; p.Detects() {
		t.Fatalf("detects: %+v", p)
	}

	// a restart reads the state back: still on the supply, without waiting for new reports
	y := newSupplyRig(t, x.st)
	if !y.onSupply("MP3P") {
		t.Fatalf("after the restart: %+v", y.view("MP3P"))
	}

	// the manual "Batteries" wins over the detected supply
	if err := y.chg.SetDeviceOverride(ctx, "HmIP-RF", "MP3P", &batteries.Override{Power: "battery"}); err != nil {
		t.Fatal(err)
	}
	y.r.Reprofile(ctx)
	if v := y.view("MP3P"); v.Profile.Power != "battery" || v.Profile.Auto {
		t.Fatalf("manual batteries: %+v", v.Profile)
	}
	// reports on the supply do not change the manual choice
	y.report("MP3P", 3*h, "OPERATING_VOLTAGE", 5.0)
	y.report("MP3P", 5*h, "OPERATING_VOLTAGE", 5.0)
	if v := y.view("MP3P"); v.Profile.Power != "battery" {
		t.Fatalf("manual batteries after reports: %+v", v.Profile)
	}
	// the detection still follows, so the automatic choice finds it current: cells back in
	y.report("MP3P", 6*h, "OPERATING_VOLTAGE", 4.7)
	y.report("MP3P", 7*h, "OPERATING_VOLTAGE", 4.7)
	if err := y.chg.SetDeviceOverride(ctx, "HmIP-RF", "MP3P", nil); err != nil {
		t.Fatal(err)
	}
	y.r.Reprofile(ctx)
	if v := y.view("MP3P"); v.Profile.Power != "battery-or-dc" || v.Profile.Auto {
		t.Fatalf("automatic again, on cells: %+v", v.Profile)
	}

	// the manual "Power supply unit" wins over cells in range: external, not automatic
	if err := y.chg.SetDeviceOverride(ctx, "HmIP-RF", "MP3P", &batteries.Override{Power: "external"}); err != nil {
		t.Fatal(err)
	}
	y.r.Reprofile(ctx)
	if v := y.view("MP3P"); v.Profile.Power != "external" || v.Profile.Auto {
		t.Fatalf("manual supply: %+v", v.Profile)
	}
}
