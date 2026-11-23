package recorder

import (
	"context"
	"testing"
	"time"

	"github.com/hobbyquaker/ccu-addon-battery/internal/store"
	"github.com/hobbyquaker/ccu-addon-kit/go/addonkit"
	"github.com/hobbyquaker/ccu-addon-kit/go/litesource"
)

// ps builds a VALUES description: key -> "TYPE" or "TYPE|UNIT".
func ps(m map[string]string) litesource.Paramset {
	out := litesource.Paramset{}
	for k, v := range m {
		typ, unit := v, ""
		for i := 0; i < len(v); i++ {
			if v[i] == '|' {
				typ, unit = v[:i], v[i+1:]
				break
			}
		}
		out[k] = litesource.ParamDesc{Type: typ, Unit: unit}
	}
	return out
}

func TestDatapoints(t *testing.T) {
	maint := func(lowbat string) channel {
		return channel{Index: 0, Type: "MAINTENANCE", Set: ps(map[string]string{lowbat: "BOOL", "UNREACH": "BOOL"})}
	}
	for _, c := range []struct {
		name            string
		chans           []channel
		voltage, lowbat string
	}{
		{"HmIP: 0/OPERATING_VOLTAGE and 0/LOW_BAT",
			[]channel{{Index: 0, Type: "MAINTENANCE", Set: ps(map[string]string{"OPERATING_VOLTAGE": "FLOAT|V", "LOW_BAT": "BOOL"})}, {Index: 1, Type: "KEY_TRANSCEIVER", Set: ps(map[string]string{"PRESS_SHORT": "ACTION"})}},
			"0/OPERATING_VOLTAGE", "0/LOW_BAT"},
		{"HmIP channel 0 without a unit is taken as before",
			[]channel{{Index: 0, Set: ps(map[string]string{"OPERATING_VOLTAGE": "FLOAT", "LOW_BAT": "BOOL"})}},
			"0/OPERATING_VOLTAGE", "0/LOW_BAT"},
		{"channel 0 comes before another channel's voltage",
			[]channel{{Index: 2, Set: ps(map[string]string{"OPERATING_VOLTAGE": "FLOAT|V"})}, {Index: 0, Set: ps(map[string]string{"OPERATING_VOLTAGE": "FLOAT|V", "LOW_BAT": "BOOL"})}},
			"0/OPERATING_VOLTAGE", "0/LOW_BAT"},
		{"BidCos thermostat: BATTERY_STATE, 0/LOWBAT",
			[]channel{maint("LOWBAT"), {Index: 4, Type: "CLIMATECONTROL_RT_TRANSCEIVER", Set: ps(map[string]string{"BATTERY_STATE": "FLOAT|V"})}},
			"4/BATTERY_STATE", "0/LOWBAT"},
		{"BATTERY_STATE off a thermostat channel is not a voltage",
			[]channel{maint("LOWBAT"), {Index: 1, Type: "WEATHER", Set: ps(map[string]string{"BATTERY_STATE": "FLOAT|V"})}},
			"", "0/LOWBAT"},
		{"BidCos contact: LOWBAT only",
			[]channel{maint("LOWBAT"), {Index: 1, Type: "SHUTTER_CONTACT", Set: ps(map[string]string{"STATE": "BOOL"})}},
			"", "0/LOWBAT"},
		{"HB-UNI-Sen-CAP-MOIST: OPERATING_VOLTAGE on the WEATHER channel 1 (unit \" V\")",
			[]channel{maint("LOWBAT"), {Index: 1, Type: "WEATHER", Set: ps(map[string]string{"HUMIDITY": "INTEGER|%", "OPERATING_VOLTAGE": "FLOAT| V"})}},
			"1/OPERATING_VOLTAGE", "0/LOWBAT"},
		{"HB-UNI-Sen-PF-SC: OPERATING_VOLTAGE on channel 2",
			[]channel{maint("LOWBAT"), {Index: 1, Type: "KEY", Set: ps(map[string]string{"PRESS_SHORT": "ACTION"})}, {Index: 2, Type: "WEATHER", Set: ps(map[string]string{"OPERATING_VOLTAGE": "FLOAT|V"})}},
			"2/OPERATING_VOLTAGE", "0/LOWBAT"},
		{"HB-UNI-Sen-IAQ: BATTERY_VOLTAGE on channel 1",
			[]channel{maint("LOWBAT"), {Index: 1, Type: "WEATHER", Set: ps(map[string]string{"BATTERY_VOLTAGE": "FLOAT|V", "IAQ_LEVEL": "INTEGER"})}},
			"1/BATTERY_VOLTAGE", "0/LOWBAT"},
		{"HB-RC-4-Dis-TH: BATTERY_VOLTAGE on channel 5",
			[]channel{maint("LOWBAT"), {Index: 1, Type: "KEY", Set: ps(map[string]string{"PRESS_SHORT": "ACTION"})}, {Index: 5, Type: "WEATHER", Set: ps(map[string]string{"BATTERY_VOLTAGE": "FLOAT|V", "TEMPERATURE": "FLOAT|°C"})}},
			"5/BATTERY_VOLTAGE", "0/LOWBAT"},
		{"OPERATING_VOLTAGE comes before an earlier channel's BATTERY_VOLTAGE",
			[]channel{maint("LOWBAT"), {Index: 1, Set: ps(map[string]string{"BATTERY_VOLTAGE": "FLOAT|V"})}, {Index: 3, Set: ps(map[string]string{"OPERATING_VOLTAGE": "FLOAT|V"})}},
			"3/OPERATING_VOLTAGE", "0/LOWBAT"},
		{"the first channel with the key wins",
			[]channel{maint("LOWBAT"), {Index: 3, Set: ps(map[string]string{"BATTERY_VOLTAGE": "FLOAT|V"})}, {Index: 1, Set: ps(map[string]string{"BATTERY_VOLTAGE": "FLOAT|V"})}},
			"1/BATTERY_VOLTAGE", "0/LOWBAT"},
		{"HB-UNI-Sen-LEV-US: a BATTERY_VOLTAGE in cm is no voltage",
			[]channel{maint("LOWBAT"), {Index: 1, Set: ps(map[string]string{"BATTERY_VOLTAGE": "FLOAT|cm"})}},
			"", "0/LOWBAT"},
		{"a voltage in mV or as an INTEGER is not taken",
			[]channel{maint("LOWBAT"), {Index: 1, Set: ps(map[string]string{"OPERATING_VOLTAGE": "FLOAT|mV"})}, {Index: 2, Set: ps(map[string]string{"BATTERY_VOLTAGE": "INTEGER|V"})}},
			"", "0/LOWBAT"},
		{"HB-UNI-Sen-VOLT: a voltmeter (HB_VOLTAGE) has no battery voltage",
			[]channel{maint("LOWBAT"), {Index: 1, Set: ps(map[string]string{"HB_VOLTAGE": "FLOAT|V"})}},
			"", "0/LOWBAT"},
		{"meters and chargers: VOLTAGE, ACTUAL_VOLTAGE, HB_BAT_VOLTAGE are never taken",
			[]channel{{Index: 0, Set: ps(map[string]string{"UNREACH": "BOOL"})}, {Index: 1, Set: ps(map[string]string{"VOLTAGE": "FLOAT|V", "ACTUAL_VOLTAGE": "FLOAT|V", "HB_BAT_VOLTAGE": "FLOAT|V", "HB_PV_VOLTAGE": "FLOAT|V"})}},
			"", ""},
		{"HB-UNI-SenAct-4-4-SC: LOWBAT on contact channel 5 is no battery",
			[]channel{{Index: 0, Set: ps(map[string]string{"UNREACH": "BOOL"})}, {Index: 1, Type: "SWITCH", Set: ps(map[string]string{"STATE": "BOOL"})}, {Index: 5, Type: "SHUTTER_CONTACT", Set: ps(map[string]string{"STATE": "BOOL", "LOWBAT": "BOOL"})}},
			"", ""},
		{"LOW_BAT before LOWBAT on channel 0",
			[]channel{{Index: 0, Set: ps(map[string]string{"LOW_BAT": "BOOL", "LOWBAT": "BOOL"})}},
			"", "0/LOW_BAT"},
		{"no channels", nil, "", ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			v, l := datapoints(c.chans)
			if v != c.voltage || l != c.lowbat {
				t.Fatalf("got %q %q, want %q %q", v, l, c.voltage, c.lowbat)
			}
		})
	}
}

// Self-built devices on BidCos-RF: the voltage is learned on another channel, recorded from its
// events (BATTERY_VOLTAGE included), and a device whose stored voltage datapoint was empty takes
// the new one at the next learn.
func TestLearnHomebrewVoltage(t *testing.T) {
	s := system(t)
	for _, a := range []string{"MOIST1", "MOIST2"} {
		s.Devices["BidCos-RF"] = append(s.Devices["BidCos-RF"],
			map[string]any{"ADDRESS": a, "TYPE": "HB-UNI-Sen-CAP-MOIST", "FIRMWARE": "1.0", "VERSION": 1, "CHILDREN": []string{a + ":0", a + ":1"}},
			map[string]any{"ADDRESS": a + ":0", "TYPE": "MAINTENANCE", "PARENT": a},
			map[string]any{"ADDRESS": a + ":1", "TYPE": "WEATHER", "PARENT": a})
	}
	s.Devices["BidCos-RF"] = append(s.Devices["BidCos-RF"],
		map[string]any{"ADDRESS": "IAQ", "TYPE": "HB-UNI-Sen-IAQ", "FIRMWARE": "1.0", "VERSION": 1, "CHILDREN": []string{"IAQ:0", "IAQ:1"}},
		map[string]any{"ADDRESS": "IAQ:0", "TYPE": "MAINTENANCE", "PARENT": "IAQ"},
		map[string]any{"ADDRESS": "IAQ:1", "TYPE": "WEATHER", "PARENT": "IAQ"},
		map[string]any{"ADDRESS": "SENACT", "TYPE": "HB-UNI-SenAct-4-4-SC", "FIRMWARE": "1.0", "VERSION": 1, "CHILDREN": []string{"SENACT:0", "SENACT:5"}},
		map[string]any{"ADDRESS": "SENACT:0", "TYPE": "MAINTENANCE", "PARENT": "SENACT"},
		map[string]any{"ADDRESS": "SENACT:5", "TYPE": "SHUTTER_CONTACT", "PARENT": "SENACT"})
	maint := map[string]any{"LOWBAT": map[string]any{"TYPE": "BOOL"}, "UNREACH": map[string]any{"TYPE": "BOOL"}}
	s.Paramsets["BidCos-RF/HB-UNI-Sen-CAP-MOIST/1.0/1/MAINTENANCE/VALUES"] = maint
	s.Paramsets["BidCos-RF/HB-UNI-Sen-CAP-MOIST/1.0/1/WEATHER/VALUES"] = map[string]any{"OPERATING_VOLTAGE": map[string]any{"TYPE": "FLOAT", "UNIT": " V"}, "HUMIDITY": map[string]any{"TYPE": "INTEGER", "UNIT": "%"}}
	s.Paramsets["BidCos-RF/HB-UNI-Sen-IAQ/1.0/1/MAINTENANCE/VALUES"] = maint
	s.Paramsets["BidCos-RF/HB-UNI-Sen-IAQ/1.0/1/WEATHER/VALUES"] = map[string]any{"BATTERY_VOLTAGE": map[string]any{"TYPE": "FLOAT", "UNIT": "V"}}
	s.Paramsets["BidCos-RF/HB-UNI-SenAct-4-4-SC/1.0/1/MAINTENANCE/VALUES"] = map[string]any{"UNREACH": map[string]any{"TYPE": "BOOL"}}
	s.Paramsets["BidCos-RF/HB-UNI-SenAct-4-4-SC/1.0/1/SHUTTER_CONTACT/VALUES"] = map[string]any{"STATE": map[string]any{"TYPE": "BOOL"}, "LOWBAT": map[string]any{"TYPE": "BOOL"}}

	st, err := store.Open(store.Config{AddonDir: t.TempDir(), Log: t.Logf})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	// what 1.5.4 stored for the sensor: tracked by its LOWBAT, no voltage
	now := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	st.UpsertDevice(store.Device{Interface: "BidCos-RF", Address: "MOIST1", Model: "HB-UNI-Sen-CAP-MOIST", Power: "unknown", LowBatDP: "0/LOWBAT", FirstSeen: now.UnixMilli(), LastSeen: now.UnixMilli()})
	st.Flush()
	c := litesource.New(s.URL, addonkit.StaticToken("olt_test"))
	r := New(Options{Source: c, Store: st, Log: t.Logf, ReadPause: time.Millisecond, Now: func() time.Time { return now }})
	ctx := context.Background()
	r.loadStore(ctx)
	if _, _, err := r.learnDevices(ctx, nil); err != nil {
		t.Fatal(err)
	}
	views := r.Devices("de")
	for addr, want := range map[string]string{"MOIST1": "1/OPERATING_VOLTAGE", "MOIST2": "1/OPERATING_VOLTAGE", "IAQ": "1/BATTERY_VOLTAGE", "WRC2": "0/OPERATING_VOLTAGE", "RTDN": "4/BATTERY_STATE"} {
		if v := find(views, addr); v == nil || v.VoltageDP != want {
			t.Fatalf("%s: %+v, want %s", addr, v, want)
		}
	}
	if v := find(views, "IAQ"); v.LowBatDP != "0/LOWBAT" {
		t.Fatalf("IAQ lowbat: %+v", v)
	}
	if v := find(views, "WRC2"); v.LowBatDP != "0/LOW_BAT" {
		t.Fatalf("WRC2 lowbat: %+v", v)
	}
	if find(views, "SENACT") != nil {
		t.Fatal("a mains device with LOWBAT on a contact channel is tracked")
	}
	st.Flush()
	devs, _ := st.Devices(ctx)
	for _, d := range devs {
		if d.Address == "MOIST1" && d.VoltageDP != "1/OPERATING_VOLTAGE" {
			t.Fatalf("the stored voltage datapoint was not updated: %+v", d)
		}
	}

	ev := func(addr, key string, v string, ts time.Time) {
		r.apply(litesource.Event{Interface: "BidCos-RF", Address: addr, Key: key, Value: []byte(v), TS: ts, Confirmed: true, Source: "event"}, true)
	}
	ev("IAQ:1", "BATTERY_VOLTAGE", "2.9", now)
	ev("MOIST1:1", "OPERATING_VOLTAGE", "3.1", now)
	ev("MOIST1:0", "OPERATING_VOLTAGE", "9.9", now.Add(time.Minute)) // not its datapoint
	ev("IAQ:2", "BATTERY_VOLTAGE", "9.9", now.Add(time.Minute))      // not its channel
	views = r.Devices("de")
	if v := find(views, "IAQ"); v.Voltage == nil || *v.Voltage != 2.9 {
		t.Fatalf("IAQ voltage: %+v", v)
	}
	if v := find(views, "MOIST1"); v.Voltage == nil || *v.Voltage != 3.1 {
		t.Fatalf("MOIST1 voltage: %+v", v)
	}
	st.Flush()
	if rows, _ := st.Samples(ctx, "BidCos-RF", "IAQ", 0, 0); len(rows) != 1 || rows[0].Voltage == nil || *rows[0].Voltage != 2.9 {
		t.Fatalf("IAQ samples: %+v", rows)
	}
	if !isKey("BATTERY_VOLTAGE") {
		t.Fatal("BATTERY_VOLTAGE is not subscribed")
	}
}
