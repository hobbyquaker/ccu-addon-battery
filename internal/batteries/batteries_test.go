package batteries

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/hobbyquaker/ccu-addon-battery/data"
)

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

// The rows that stood on the unlicensed ioBroker script alone were re-derived from eQ-3's and
// ELV's documents; the ones without a document are left out (unknown to the addon) rather than
// guessed. The HM-LC-Sw4-PCB was listed as 1x 9V: it runs on an external 7-15 V supply.
func TestRederivedRows(t *testing.T) {
	tb := Shipped()
	for _, c := range []struct{ model, power, desc, conf string }{
		{"HM-LC-Sw4-PCB", "mains", "", "manufacturer"},
		{"HM-LC-Sw4-Ba-PCB", "battery-or-dc", "", "manufacturer"},
		{"HM-Sen-RD-O", "mains", "", "manufacturer"},
		{"HM-OU-CM-PCB", "mains", "", "manufacturer"},
		{"HM-MOD-Re-8", "battery-or-dc", "", "manufacturer"},
		{"HM-RC-Sec4-3", "battery", "1× AAA", "manufacturer"},
		{"HM-Sec-Key-O", "battery", "3× AA", "manufacturer"},
		{"HM-WDS30-OT2-SM-2", "battery", "2× AA", "manufacturer"},
		{"HM-LC-Sw1-Pl", "mains", "", "assumed"},
	} {
		m, _, ok := tb.Lookup(c.model)
		if !ok || m.Power != c.power || Describe(m.Slots) != c.desc || m.Confidence != c.conf {
			t.Errorf("%s: %v %+v", c.model, ok, m)
		}
	}
	for _, model := range []string{"ALPHA-IP-RBG", "HM-PBI-2-FM", "HM-RC-19-SW", "HM-Sec-RHS-2", "HM-Sec-SC2L"} {
		if _, _, ok := tb.Lookup(model); ok {
			t.Errorf("%s: still in the table without a licensed source", model)
		}
		if p := tb.Resolve(model, nil, nil, 0); p.Source != "none" || p.Power != "unknown" {
			t.Errorf("%s: %+v", model, p)
		}
	}
	if p := tb.Resolve("HM-LC-Sw4-PCB", nil, nil, 0); p.Detects() || len(p.Slots) != 0 {
		t.Errorf("HM-LC-Sw4-PCB: %+v", p)
	}
}

// No row may stand on a source without a licence (the ioBroker script) and the corpus alone.
func TestNoRowOnUnlicensedSourceAlone(t *testing.T) {
	var raw struct {
		Sources map[string]struct {
			License string `json:"license"`
		} `json:"sources"`
		Models map[string]struct {
			Sources []struct {
				ID string `json:"id"`
			} `json:"sources"`
		} `json:"models"`
	}
	if err := json.Unmarshal(data.Batteries, &raw); err != nil {
		t.Fatal(err)
	}
	for k, m := range raw.Models {
		carried := false
		for _, s := range m.Sources {
			if s.ID != "corpus" && !strings.HasPrefix(raw.Sources[s.ID].License, "none") {
				carried = true
			}
		}
		if !carried {
			t.Errorf("%s: no licensed source", k)
		}
	}
}

// Reported in the forum after 1.4.1: the ELV-SH-BS2 - the ELV kit of the 230 V HmIP-BS2 - had no row and was
// listed as a battery device of unknown batteries; the HmIP-MP3P runs on its cells or a 5 V
// supply.
func TestELVTwinsAndMP3P(t *testing.T) {
	tb := Shipped()
	for _, c := range []struct{ model, power, desc string }{
		{"ELV-SH-BS2", "mains", ""},
		{"elv-sh-bs2", "mains", ""},
		{"ELV-SH-PSMCI", "mains", ""},
		{"ELV-SH-WUA", "mains", ""},
		{"ELV-SH-WSC", "mains", ""},
		{"ELV-SH-SB8", "mains", ""},
		{"ELV-SH-BM-S", "battery-or-dc", ""},
		{"ELV-SH-CWD", "battery", "1× CR2032"},
		{"ELV-SH-DUSI", "battery", "2× AA"},
		{"ELV-SH-CAP", "battery", "1× CR2032"},
		{"ELV-SH-CTV", "battery", "1× CR2032"},
		{"HmIP-MP3P", "battery-or-dc", "3× C"},
	} {
		m, _, ok := tb.Lookup(c.model)
		if !ok || m.Power != c.power || Describe(m.Slots) != c.desc || m.Confidence != "manufacturer" {
			t.Errorf("%s: %v %+v", c.model, ok, m)
		}
	}
	if p := tb.Resolve("ELV-SH-BS2", nil, nil, 0); p.Detects() {
		t.Errorf("ELV-SH-BS2: %+v", p)
	}
	// the MP3P on its cells is watched; set to its supply it is not
	if p := tb.Resolve("HmIP-MP3P", nil, nil, 0); !p.Detects() || p.Fresh == 0 {
		t.Errorf("HmIP-MP3P: %+v", p)
	}
	if p := tb.Resolve("HmIP-MP3P", nil, &Override{Power: "external"}, 0); p.Detects() {
		t.Errorf("HmIP-MP3P on its supply: %+v", p)
	}
}

// A model the table lacks: a device that reports a voltage but no low-battery flag is a mains
// actuator (every HmIP battery device has LOW_BAT); a voltage outside a pack of 1 to 3 alkaline
// cells guesses nothing.
func TestResolveSeen(t *testing.T) {
	tb := Shipped()
	for _, c := range []struct {
		name          string
		seen          Seen
		source, power string
		detects       bool
	}{
		{"voltage, no LOW_BAT: mains", Seen{MaxV: 0, NoLowBat: true}, "datapoints", "mains", false},
		{"no LOW_BAT wins over a voltage that looks like cells", Seen{MaxV: 3.0, NoLowBat: true}, "datapoints", "mains", false},
		{"LOW_BAT and 3.0 V: two cells", Seen{MaxV: 3.0}, "guess", "battery", true},
		{"LOW_BAT and 4.6 V: three cells", Seen{MaxV: 4.6}, "guess", "battery", true},
		{"12 V is no battery pack", Seen{MaxV: 12}, "none", "unknown", true},
		{"230 V is no battery pack", Seen{MaxV: 230}, "none", "unknown", true},
		{"0.3 V is no reading of a pack", Seen{MaxV: 0.3}, "none", "unknown", true},
	} {
		p := tb.ResolveSeen("NEW-THING", nil, nil, c.seen)
		if p.Source != c.source || p.Power != c.power || p.Detects() != c.detects {
			t.Errorf("%s: %+v", c.name, p)
		}
	}
	// a row or an override always wins over what was seen
	if p := tb.ResolveSeen("HmIP-WRC2", nil, nil, Seen{NoLowBat: true}); p.Source != "table" || p.Power != "battery" {
		t.Errorf("row: %+v", p)
	}
	if p := tb.ResolveSeen("NEW-THING", nil, &Override{Slots: []Slot{{Count: 2, Type: "AA"}}}, Seen{NoLowBat: true}); p.Source != "override" || p.Power != "battery" {
		t.Errorf("override: %+v", p)
	}
}
