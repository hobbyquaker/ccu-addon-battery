package batteries

import "testing"

func TestShippedLookup(t *testing.T) {
	tb := Shipped()
	if len(tb.Models) < 400 {
		t.Fatalf("only %d models", len(tb.Models))
	}
	for _, c := range []struct{ in, key, power string }{
		{"HmIP-WRC2", "HMIP-WRC2", "battery"}, // the interface's spelling differs from eQ-3's
		{"hmip-wrc2", "HMIP-WRC2", "battery"},
		{"HM-CC-RT-DN-BoM", "HM-CC-RT-DN", "battery"}, // an alias
		{" HmIP-BBL ", "HmIP-BBL", "mains"},
		{"HmIP-STE2-PCB", "HmIP-STE2-PCB", "battery-or-dc"},
	} {
		m, key, ok := tb.Lookup(c.in)
		if !ok || key != c.key || m.Power != c.power {
			t.Errorf("%s: %v %q %q", c.in, ok, key, m.Power)
		}
	}
	if _, _, ok := tb.Lookup("NOPE-1"); ok {
		t.Fatal("unknown model found")
	}
	if n := tb.TypeNames(); len(n) < 10 || n[0] != "9V" {
		t.Fatalf("types: %v", n)
	}
}

func TestResolve(t *testing.T) {
	tb := Shipped()
	tr := true
	cases := []struct {
		name                string
		model               string
		mo, do              *Override
		maxV                float64
		source, power, desc string
		fresh, low, step    float64
		detects             bool
	}{
		{"wrc2 from the table", "HmIP-WRC2", nil, nil, 0, "table", "battery", "2× AAA", 2.96, 2.2, 0.24, true},
		{"rt-dn on channel 4", "HM-CC-RT-DN", nil, nil, 0, "table", "battery", "2× AA", 2.96, 2.1, 0.24, true},
		{"mains", "HmIP-BBL", nil, nil, 0, "table", "mains", "", 0, 0, 0, false},
		{"smoke alarm: fixed cells", "HmIP-SWSD", nil, nil, 0, "table", "battery", "2× fixed", 0, 0, 0, false},
		{"rechargeables in a wrc2", "HmIP-WRC2", nil, &Override{Rechargeable: &tr}, 0, "override", "battery", "2× AAA (nimh)", 2.66, 2.2, 0.12, true},
		{"desk stand: 2x AA", "HmIP-WRC2", nil, &Override{Slots: []Slot{{Count: 2, Type: "AA"}}}, 0, "override", "battery", "2× AA", 2.96, 2.2, 0.24, true},
		{"model override, then device override wins", "HmIP-WRC2", &Override{Power: "external"}, &Override{Power: "battery"}, 0, "override", "battery", "2× AAA", 2.96, 2.2, 0.24, true},
		{"unknown, guessed from 3.0 V", "NEW-THING", nil, nil, 3.0, "guess", "battery", "2× AA", 2.96, 2.2, 0.24, true},
		{"unknown, no voltage", "NEW-THING", nil, nil, 0, "none", "unknown", "", 0, 0, 0, true},
		{"outdoor adds 0.1 V to the step", "HmIP-WRC2", nil, &Override{Outdoor: &tr}, 0, "override", "battery", "2× AAA", 2.96, 2.2, 0.34, true},
	}
	for _, c := range cases {
		p := tb.Resolve(c.model, c.mo, c.do, c.maxV)
		if p.Source != c.source || p.Power != c.power || Describe(p.Slots) != c.desc || p.Fresh != c.fresh || p.Low != c.low || p.Step != c.step || p.Detects() != c.detects {
			t.Errorf("%s: %+v (%s, detects %v)", c.name, p, Describe(p.Slots), p.Detects())
		}
	}
	if p := tb.Resolve("HmIP-WRC2", nil, &Override{Power: "external"}, 0); p.Detects() {
		t.Fatal("an externally powered device is not detected")
	}
}

func TestValidate(t *testing.T) {
	tb := Shipped()
	for _, o := range []Override{{Power: "solar"}, {Chemistry: "unobtainium"}, {Slots: []Slot{{Count: 0, Type: "AA"}}}, {Slots: []Slot{{Count: 1, Type: "AAAA"}}}, {Slots: []Slot{{Count: 1, Type: "AA", Chemistry: "x"}}}} {
		if tb.Validate(o) == nil {
			t.Errorf("%+v accepted", o)
		}
	}
	if err := tb.Validate(Override{Power: "battery", Slots: []Slot{{Count: 2, Type: "AA", Chemistry: "lithium-aa"}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := Parse([]byte(`{"models":{}}`)); err == nil {
		t.Fatal("an empty table parsed")
	}
	if _, err := Parse([]byte(`{`)); err == nil {
		t.Fatal("broken json parsed")
	}
}
