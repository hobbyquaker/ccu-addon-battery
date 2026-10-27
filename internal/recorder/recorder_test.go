package recorder

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hobbyquaker/ccu-addon-battery/internal/batteries"
	"github.com/hobbyquaker/ccu-addon-battery/internal/changes"
	"github.com/hobbyquaker/ccu-addon-battery/internal/store"
	"github.com/hobbyquaker/ccu-addon-kit/go/addonkit"
	"github.com/hobbyquaker/ccu-addon-kit/go/litesource"
	"github.com/hobbyquaker/ccu-addon-kit/go/litesource/fakelite"
)

func TestMain(m *testing.M) {
	dir, err := filepath.Abs(filepath.Join("..", "..", ".testtmp", "recorder"))
	if err == nil && os.MkdirAll(dir, 0o755) == nil {
		os.Setenv("TMPDIR", dir)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// a system with one HmIP remote (voltage + LOW_BAT), one BidCos thermostat (BATTERY_STATE on
// channel 4, LOWBAT on :0), one BidCos contact (LOWBAT only) and one HmIP mains actuator
// (OPERATING_VOLTAGE, no LOW_BAT) - and a VirtualDevices group without any of it.
func system(t *testing.T) *fakelite.Server {
	s := fakelite.New()
	t.Cleanup(s.Close)
	s.Token = "olt_test"
	s.Interfaces = []string{"HmIP-RF", "BidCos-RF", "VirtualDevices"}
	s.Devices["HmIP-RF"] = []map[string]any{
		{"ADDRESS": "WRC2", "TYPE": "HmIP-WRC2", "FIRMWARE": "1.2", "VERSION": 1, "CHILDREN": []string{"WRC2:0", "WRC2:1", "WRC2:2"}},
		{"ADDRESS": "WRC2:0", "TYPE": "MAINTENANCE", "PARENT": "WRC2"},
		{"ADDRESS": "WRC2:1", "TYPE": "KEY_TRANSCEIVER", "PARENT": "WRC2"},
		{"ADDRESS": "BBL", "TYPE": "HmIP-BBL", "FIRMWARE": "1.0", "VERSION": 1, "CHILDREN": []string{"BBL:0", "BBL:3"}},
		{"ADDRESS": "BBL:0", "TYPE": "MAINTENANCE", "PARENT": "BBL"},
	}
	s.Devices["BidCos-RF"] = []map[string]any{
		{"ADDRESS": "RTDN", "TYPE": "HM-CC-RT-DN", "FIRMWARE": "1.4", "VERSION": 1, "CHILDREN": []string{"RTDN:0", "RTDN:4"}},
		{"ADDRESS": "RTDN:0", "TYPE": "MAINTENANCE", "PARENT": "RTDN"},
		{"ADDRESS": "RTDN:4", "TYPE": "CLIMATECONTROL_RT_TRANSCEIVER", "PARENT": "RTDN"},
		{"ADDRESS": "SC", "TYPE": "HM-Sec-SC-2", "FIRMWARE": "2.4", "VERSION": 1, "CHILDREN": []string{"SC:0", "SC:1"}},
		{"ADDRESS": "SC:0", "TYPE": "MAINTENANCE", "PARENT": "SC"},
	}
	s.Devices["VirtualDevices"] = []map[string]any{{"ADDRESS": "INT0001", "TYPE": "HmIP-HEATING", "VERSION": 1, "CHILDREN": []string{"INT0001:1"}}}
	s.Paramsets["HmIP-RF/HmIP-WRC2/1.2/1/MAINTENANCE/VALUES"] = map[string]any{"OPERATING_VOLTAGE": map[string]any{"TYPE": "FLOAT"}, "OPERATING_VOLTAGE_STATUS": map[string]any{"TYPE": "ENUM"}, "LOW_BAT": map[string]any{"TYPE": "BOOL"}, "UNREACH": map[string]any{"TYPE": "BOOL"}, "BOOTED": map[string]any{"TYPE": "BOOL"}}
	s.Paramsets["HmIP-RF/HmIP-BBL/1.0/1/MAINTENANCE/VALUES"] = map[string]any{"OPERATING_VOLTAGE": map[string]any{"TYPE": "FLOAT"}, "OPERATING_VOLTAGE_STATUS": map[string]any{"TYPE": "ENUM"}, "UNREACH": map[string]any{"TYPE": "BOOL"}}
	s.Paramsets["BidCos-RF/HM-CC-RT-DN/1.4/1/MAINTENANCE/VALUES"] = map[string]any{"LOWBAT": map[string]any{"TYPE": "BOOL"}, "UNREACH": map[string]any{"TYPE": "BOOL"}}
	s.Paramsets["BidCos-RF/HM-CC-RT-DN/1.4/1/CLIMATECONTROL_RT_TRANSCEIVER/VALUES"] = map[string]any{"BATTERY_STATE": map[string]any{"TYPE": "FLOAT"}, "ACTUAL_TEMPERATURE": map[string]any{"TYPE": "FLOAT"}}
	s.Paramsets["BidCos-RF/HM-Sec-SC-2/2.4/1/MAINTENANCE/VALUES"] = map[string]any{"LOWBAT": map[string]any{"TYPE": "BOOL"}, "UNREACH": map[string]any{"TYPE": "BOOL"}}
	s.Values["HmIP-RF/WRC2:0/VALUES"] = map[string]any{"OPERATING_VOLTAGE": 2.9, "OPERATING_VOLTAGE_STATUS": 0, "LOW_BAT": false, "UNREACH": false, "BOOTED": false, "RSSI_DEVICE": -60}
	s.Values["HmIP-RF/BBL:0/VALUES"] = map[string]any{"OPERATING_VOLTAGE": 230.0, "OPERATING_VOLTAGE_STATUS": 0, "UNREACH": false}
	s.Values["BidCos-RF/RTDN:0/VALUES"] = map[string]any{"LOWBAT": false} // must never be read
	s.State = []map[string]any{
		{"interface": "BidCos-RF", "address": "SC:0", "datapoint": "LOWBAT", "value": true, "ts": "2026-10-27T10:00:00Z", "lc": "2026-10-20T08:00:00Z", "confirmed": false, "source": "restored"},
		{"interface": "BidCos-RF", "address": "RTDN:0", "datapoint": "UNREACH", "value": false, "ts": "2026-10-27T10:00:00Z", "confirmed": true, "source": "event"},
	}
	s.EventID = "boot1-0"
	s.Meta["enums"] = map[string]any{"room": map[string]any{"name": map[string]string{"en": "Rooms"}, "tree": []map[string]any{{"id": "kueche", "name": map[string]string{"de": "Küche", "en": "Kitchen"}}}}}
	s.SetName("HmIP-RF.WRC2", "Taster Küche", "room/kueche")
	s.SetName("BidCos-RF.SC:1", "Fenster")
	return s
}

func start(t *testing.T, s *fakelite.Server) (*Recorder, *store.Store, context.CancelFunc) {
	t.Helper()
	st, err := store.Open(store.Config{AddonDir: t.TempDir(), Log: t.Logf})
	if err != nil {
		t.Fatal(err)
	}
	c := litesource.New(s.URL, addonkit.StaticToken("olt_test"))
	c.Reconnect.Min, c.Reconnect.Max = 10*time.Millisecond, 50*time.Millisecond
	c.Log = t.Logf
	r := New(Options{Source: c, Store: st, Log: t.Logf, ReadPause: time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	go r.Run(ctx)
	deadline := time.Now().Add(5 * time.Second)
	// live, and the stream open: a push before the subscription would only reach the ring
	for r.State() != "live" || s.Streams() == 0 {
		if time.Now().After(deadline) {
			t.Fatalf("recorder not live: %s, streams %d", r.State(), s.Streams())
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Cleanup(func() { cancel(); st.Close() })
	return r, st, cancel
}

func find(views []View, addr string) *View {
	for i := range views {
		if views[i].Address == addr {
			return &views[i]
		}
	}
	return nil
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met in time")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestLearnSeedRecord(t *testing.T) {
	s := system(t)
	r, st, _ := start(t, s)
	ctx := context.Background()
	views := r.Devices("de")
	if len(views) != 4 {
		t.Fatalf("tracked devices: %+v", views)
	}
	wrc := find(views, "WRC2")
	if wrc == nil || wrc.VoltageDP != "0/OPERATING_VOLTAGE" || wrc.LowBatDP != "0/LOW_BAT" || wrc.Name != "Taster Küche" || len(wrc.Rooms) != 1 || wrc.Rooms[0] != "Küche" {
		t.Fatalf("WRC2: %+v", wrc)
	}
	if wrc.Voltage == nil || *wrc.Voltage != 2.9 || wrc.LowBat == nil || *wrc.LowBat || wrc.VoltageStatus != "NORMAL" {
		t.Fatalf("WRC2 seeded from the HmIP cache: %+v", wrc)
	}
	rt := find(views, "RTDN")
	if rt == nil || rt.VoltageDP != "4/BATTERY_STATE" || rt.LowBatDP != "0/LOWBAT" || rt.Unreach == nil || *rt.Unreach {
		t.Fatalf("RT-DN: %+v", rt)
	}
	sc := find(views, "SC")
	if sc == nil || sc.VoltageDP != "" || sc.LowBatDP != "0/LOWBAT" || sc.LowBat == nil || !*sc.LowBat || sc.LowBatSince != time.Date(2026, 10, 20, 8, 0, 0, 0, time.UTC).UnixMilli() || sc.Name != "Fenster" {
		t.Fatalf("SC (LOWBAT only, restored seed, named by its channel): %+v", sc)
	}
	if bbl := find(views, "BBL"); bbl == nil || bbl.LowBatDP != "" || *bbl.Voltage != 230 {
		t.Fatalf("BBL: %+v", bbl)
	}
	if find(views, "INT0001") != nil {
		t.Fatal("a group has no battery datapoint and is not tracked")
	}
	for _, c := range s.Calls {
		if strings.Contains(c, "BidCos-RF getParamset") {
			t.Fatalf("a BidCos value was read: %v", s.Calls)
		}
	}
	// the English rooms
	if en := find(r.Devices("en"), "WRC2"); en.Rooms[0] != "Kitchen" {
		t.Fatalf("rooms en: %v", en.Rooms)
	}

	// live events: the voltage moves within the step - no new row; a step - a row; a transition
	// of LOW_BAT - a row and an evidence event; a BOOTED true - an event
	st.Flush()
	c0, _ := st.Counts(ctx)
	// a pause between the pushes: two reports in one millisecond share one row by design
	push := func(iface, addr, key string, v any) {
		s.PushEvent(iface, addr, key, v)
		time.Sleep(3 * time.Millisecond)
	}
	push("HmIP-RF", "WRC2:0", "OPERATING_VOLTAGE", 2.905)
	push("HmIP-RF", "WRC2:0", "OPERATING_VOLTAGE", 2.88)
	push("HmIP-RF", "WRC2:0", "LOW_BAT", true)
	push("HmIP-RF", "WRC2:0", "BOOTED", true)
	push("BidCos-RF", "RTDN:4", "BATTERY_STATE", 2.7)
	push("BidCos-RF", "RTDN:0", "UNREACH", true)
	push("HmIP-RF", "WRC2:0", "RSSI_DEVICE", -70) // not subscribed
	waitFor(t, func() bool {
		v := find(r.Devices("de"), "RTDN")
		return v.Unreach != nil && *v.Unreach
	})
	st.Flush()
	c1, _ := st.Counts(ctx)
	// seed rows: WRC2 (voltage), SC (lowbat only), BBL, RTDN none (no voltage yet, UNREACH is no sample) = 3
	if c0["samples"] != 3 {
		t.Fatalf("seed samples: %v", c0)
	}
	// + WRC2 2.88, WRC2 LOW_BAT true (same voltage, flag changed), RTDN 2.7 = 3 more
	if c1["samples"] != 6 {
		rows, _ := st.Samples(ctx, "HmIP-RF", "WRC2", 0, 0)
		t.Fatalf("live samples: %v %+v", c1, rows)
	}
	// events: LOW_BAT false→true, BOOTED true, UNREACH false→true = 3; the seed made none
	if c1["events"] != 3 || c0["events"] != 0 {
		t.Fatalf("events: %v %v", c0, c1)
	}
	wrc = find(r.Devices("de"), "WRC2")
	if *wrc.Voltage != 2.88 || !*wrc.LowBat {
		t.Fatalf("WRC2 after events: %+v", wrc)
	}
	rows, _ := st.Samples(ctx, "HmIP-RF", "WRC2", 0, 0)
	if rows[0].Source != "seed" || rows[len(rows)-1].Source != "live" {
		t.Fatalf("sources: %+v", rows)
	}
}

func TestResyncAndRestart(t *testing.T) {
	s := system(t)
	r, st, cancel := start(t, s)
	s.PushEvent("HmIP-RF", "WRC2:0", "OPERATING_VOLTAGE", 2.5)
	waitFor(t, func() bool { v := find(r.Devices("de"), "WRC2"); return v.Voltage != nil && *v.Voltage == 2.5 })
	// a reboot of the system: the recorder re-seeds from the cache (2.9 again) without a crash
	s.Values["HmIP-RF/WRC2:0/VALUES"]["OPERATING_VOLTAGE"] = 2.95
	s.Reboot("boot2")
	waitFor(t, func() bool { v := find(r.Devices("de"), "WRC2"); return v.Voltage != nil && *v.Voltage == 2.95 })
	// a device leaves (the process does not list it any more) and another arrives
	s.Devices["BidCos-RF"] = s.Devices["BidCos-RF"][:3]
	s.Push("deleteDevices", map[string]any{"interface": "BidCos-RF", "addresses": []string{"SC", "SC:0", "SC:1"}})
	waitFor(t, func() bool { return find(r.Devices("de"), "SC") == nil })
	s.Devices["HmIP-RF"] = append(s.Devices["HmIP-RF"],
		map[string]any{"ADDRESS": "NEW", "TYPE": "HmIP-WRC2", "FIRMWARE": "1.2", "VERSION": 1, "CHILDREN": []string{"NEW:0"}},
		map[string]any{"ADDRESS": "NEW:0", "TYPE": "MAINTENANCE", "PARENT": "NEW"})
	s.Push("newDevices", map[string]any{"interface": "HmIP-RF", "addresses": []string{"NEW", "NEW:0"}})
	waitFor(t, func() bool { return find(r.Devices("de"), "NEW") != nil })
	// the store survives a restart of the recorder: the devices and current values come back
	cancel()
	st.Flush()
	devs, _ := st.Devices(context.Background())
	retired := 0
	for _, d := range devs {
		if d.RetiredAt != 0 {
			retired++
		}
	}
	if len(devs) != 5 || retired != 1 {
		t.Fatalf("devices in the store: %+v", devs)
	}
	c := litesource.New(s.URL, addonkit.StaticToken("olt_test"))
	r2 := New(Options{Source: c, Store: st, Log: t.Logf, ReadPause: time.Millisecond})
	r2.loadStore(context.Background())
	if v := find(r2.Devices("de"), "WRC2"); v == nil || v.Voltage == nil || *v.Voltage != 2.95 {
		t.Fatalf("after restart: %+v", v)
	}
	if find(r2.Devices("de"), "SC") != nil {
		t.Fatal("a retired device is not loaded")
	}
}

func TestDailyRow(t *testing.T) {
	s := system(t)
	now := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	st, _ := store.Open(store.Config{AddonDir: t.TempDir(), Log: t.Logf})
	defer st.Close()
	c := litesource.New(s.URL, addonkit.StaticToken("olt_test"))
	r := New(Options{Source: c, Store: st, Log: t.Logf, ReadPause: time.Millisecond, Now: func() time.Time { return now }})
	ctx := context.Background()
	if err := r.learnDevices(ctx); err != nil {
		t.Fatal(err)
	}
	ev := func(ts time.Time, v float64) {
		r.apply(litesource.Event{Interface: "HmIP-RF", Address: "WRC2:0", Key: "OPERATING_VOLTAGE", Value: []byte(strconv.FormatFloat(v, 'f', -1, 64)), TS: ts, Confirmed: true, Source: "event"}, true)
	}
	ev(now, 2.9)
	ev(now.Add(time.Hour), 2.9)      // same value: no row
	ev(now.Add(23*time.Hour), 2.905) // within the step: no row
	ev(now.Add(25*time.Hour), 2.905) // a day passed: a row
	ev(now.Add(25*time.Hour), 2.905) // the same millisecond: no second row
	ev(now.Add(26*time.Hour), 2.895) // a step down from the last row (2.905): a row
	st.Flush()
	rows, _ := st.Samples(ctx, "HmIP-RF", "WRC2", 0, 0)
	if len(rows) != 3 {
		t.Fatalf("rows: %+v", rows)
	}
}

// With the change keeper as the Observer: the table gives power and slots, an EXTERNAL status
// overrides the power, and live reports reach the detector (a BOOTED with a jump opens a
// candidate row).
func TestObserverProfilesAndDetection(t *testing.T) {
	s := system(t)
	st, err := store.Open(store.Config{AddonDir: t.TempDir(), Log: t.Logf})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	chg := changes.New(changes.Options{Store: st, Log: t.Logf})
	c := litesource.New(s.URL, addonkit.StaticToken("olt_test"))
	c.Reconnect.Min, c.Reconnect.Max = 10*time.Millisecond, 50*time.Millisecond
	r := New(Options{Source: c, Store: st, Observer: chg, Log: t.Logf, ReadPause: time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go r.Run(ctx)
	waitFor(t, func() bool { return r.State() == "live" && s.Streams() > 0 })
	views := r.Devices("en")
	want := map[string][2]string{"WRC2": {"battery", "2× AAA"}, "BBL": {"mains", ""}, "RTDN": {"battery", "2× AA"}, "SC": {"battery", "2× LR44"}}
	for addr, w := range want {
		v := find(views, addr)
		if v == nil || v.Power != w[0] || batteries.Describe(v.Profile.Slots) != w[1] {
			t.Errorf("%s: %+v", addr, v)
		}
	}
	st.Flush()
	db, _ := st.DB()
	var n int
	db.QueryRow(`SELECT COUNT(*) FROM slots WHERE address = 'WRC2' AND count = 2 AND type = 'AAA'`).Scan(&n)
	if n != 1 {
		t.Fatalf("slots rows: %d", n)
	}
	// the WRC2 on a desk stand reports an external supply
	s.PushEvent("HmIP-RF", "WRC2:0", "OPERATING_VOLTAGE_STATUS", 3)
	waitFor(t, func() bool { return find(r.Devices("en"), "WRC2").Power == "external" })
	s.PushEvent("HmIP-RF", "WRC2:0", "OPERATING_VOLTAGE_STATUS", 0)
	waitFor(t, func() bool { return find(r.Devices("en"), "WRC2").Power == "battery" })
	// a re-seat: BOOTED and a jump from the seeded 2.9 V; a candidate row, not an inbox entry
	s.PushEvent("HmIP-RF", "WRC2:0", "BOOTED", true)
	time.Sleep(3 * time.Millisecond)
	s.PushEvent("HmIP-RF", "WRC2:0", "OPERATING_VOLTAGE", 3.2)
	var l []changes.Change
	waitFor(t, func() bool {
		l, _ = chg.List(context.Background(), changes.Filter{Statuses: []string{changes.StatusCandidate}})
		return len(l) == 1 && l[0].Rule == "A+B"
	})
	if len(l) != 1 || !strings.Contains(string(l[0].Evidence), `"voltage_after":3.2`) {
		t.Fatalf("%+v", l)
	}
	// an override (rechargeables) is written into the slots by Reprofile
	tr := true
	if err := chg.SetDeviceOverride(context.Background(), "HmIP-RF", "WRC2", &batteries.Override{Rechargeable: &tr}); err != nil {
		t.Fatal(err)
	}
	r.Reprofile(context.Background())
	if v := find(r.Devices("en"), "WRC2"); v.Profile.Source != "override" || v.Profile.Slots[0].Chemistry != "nimh" {
		t.Fatalf("%+v", v.Profile)
	}
	st.Flush()
	db.QueryRow(`SELECT COUNT(*) FROM slots WHERE address = 'WRC2' AND rechargeable = 1`).Scan(&n)
	if n != 1 {
		t.Fatal("the override's slots were not written")
	}
}
