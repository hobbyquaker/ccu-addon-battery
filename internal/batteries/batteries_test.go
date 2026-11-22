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
	// found on its supply by its voltage: external, automatic; an override that names the
	// power wins; a model that is not battery-or-dc ignores it
	if p := tb.ResolveSeen("HmIP-MP3P", nil, nil, Seen{MaxV: 5, OnSupply: true}); p.Power != "external" || !p.Auto || p.Detects() || p.TablePower != "battery-or-dc" {
		t.Errorf("HmIP-MP3P detected on its supply: %+v", p)
	}
	if p := tb.ResolveSeen("HmIP-MP3P", nil, &Override{Power: "battery"}, Seen{OnSupply: true}); p.Power != "battery" || p.Auto || !p.Detects() {
		t.Errorf("HmIP-MP3P set to batteries: %+v", p)
	}
	if p := tb.ResolveSeen("HmIP-MP3P", nil, &Override{Power: "external"}, Seen{OnSupply: true}); p.Power != "external" || p.Auto {
		t.Errorf("HmIP-MP3P set to its supply: %+v", p)
	}
	if p := tb.ResolveSeen("HmIP-MP3P", nil, &Override{Slots: []Slot{{Count: 3, Type: "C"}}}, Seen{OnSupply: true}); p.Power != "external" || !p.Auto {
		t.Errorf("HmIP-MP3P with its slots overridden: %+v", p)
	}
	if p := tb.ResolveSeen("HmIP-WRC2", nil, nil, Seen{OnSupply: true}); p.Power != "battery" || p.Auto {
		t.Errorf("HmIP-WRC2: %+v", p)
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

// The order the device page promises: a device's own values win over its model's, the model's
// over the table - field by field, so a device override of the slots keeps the model's power.
func TestPrecedenceTableModelDevice(t *testing.T) {
	tb := Shipped()
	tr, fa := true, false
	model := &Override{Power: "battery", Slots: []Slot{{Count: 2, Type: "AA"}}, Outdoor: &tr}
	dev := &Override{Slots: []Slot{{Count: 1, Type: "CR2032"}}, Outdoor: &fa}
	for _, c := range []struct {
		name, model  string
		mOv, dOv     *Override
		power, slots string
		outdoor      bool
		source       string
	}{
		{"table", "HmIP-WRC2", nil, nil, "battery", "2× AAA", false, "table"},
		{"model over table", "HmIP-WRC2", model, nil, "battery", "2× AA", true, "override"},
		{"device over model", "HmIP-WRC2", model, dev, "battery", "1× CR2032", false, "override"},
		{"device alone over table", "HmIP-WRC2", nil, dev, "battery", "1× CR2032", false, "override"},
		{"device power, model slots", "HmIP-WRC2", model, &Override{Power: "external"}, "external", "2× AA", true, "override"},
		{"not in the table: nothing", "HB-UNI-Sen-TEMP-DS18B20", nil, nil, "unknown", "", false, "none"},
		{"not in the table: the model's", "HB-UNI-Sen-TEMP-DS18B20", model, nil, "battery", "2× AA", true, "override"},
		{"not in the table: the device's over the model's", "HB-UNI-Sen-TEMP-DS18B20", model, dev, "battery", "1× CR2032", false, "override"},
		{"not in the table: the device's alone", "HB-UNI-Sen-TEMP-DS18B20", nil, &Override{Slots: []Slot{{Count: 3, Type: "AAA"}}}, "battery", "3× AAA", false, "override"},
	} {
		p := tb.Resolve(c.model, c.mOv, c.dOv, 0)
		if p.Power != c.power || Describe(p.Slots) != c.slots || p.Outdoor != c.outdoor || p.Source != c.source {
			t.Errorf("%s: %+v", c.name, p)
		}
	}
}

// The table stand (eQ-3 HmIP-DS55): a preset of the table, offered per row, applied per model or
// per device in the usual precedence, with the device's own levels where the pack keeps the
// row's voltage and twice the table's battery life.
func TestTableStandPreset(t *testing.T) {
	tb := Shipped()
	const id = "eq3-table-stand"
	pr, ok := tb.Presets[id]
	if !ok || pr.Name["de"] != "Im Tischaufsteller (2× AA)" || pr.Name["en"] == "" || Describe(pr.Slots) != "2× AA" || pr.LifeFactor != 2 {
		t.Fatalf("preset: %+v", pr)
	}
	for _, m := range []string{"HmIP-WRC2-2", "hmip-wrc6", "HmIP-STHD", "HmIP-STH", "HMIP-WRC2", "HmIP-WTH-2", "HmIP-SMI55-2", "HM-PB-2-WM55"} {
		if l := tb.PresetsFor(m); len(l) != 1 || l[0] != id {
			t.Errorf("%s offers %v", m, l)
		}
	}
	for _, m := range []string{"HmIP-WTH-3", "HmIP-eTRV-2", "HmIP-BSM", "HB-UNI-Sen-TEMP-DS18B20"} {
		if l := tb.PresetsFor(m); l != nil {
			t.Errorf("%s offers %v", m, l)
		}
	}
	row, _, _ := tb.Lookup("HmIP-WRC6")
	stand := &Override{Preset: id}
	tr := true
	for _, c := range []struct {
		name          string
		mOv, dOv      *Override
		slots, preset string
		low           float64
		life          float64
	}{
		{"table", nil, nil, "2× AAA", "", 2.2, row.LifeYears},
		{"device in the stand", nil, stand, "2× AA", id, 2.2, row.LifeYears * 2},
		{"every device of the model", stand, nil, "2× AA", id, 2.2, row.LifeYears * 2},
		{"device's own slots over the model's stand", stand, &Override{Slots: []Slot{{Count: 1, Type: "CR2032"}}}, "1× CR2032", "", 2.4, row.LifeYears},
		{"device in the stand over the model's slots", &Override{Slots: []Slot{{Count: 3, Type: "AAA"}}}, stand, "2× AA", id, 2.2, row.LifeYears * 2},
		{"stand with rechargeables", nil, &Override{Preset: id, Rechargeable: &tr}, "2× AA (nimh)", id, 2.2, row.LifeYears * 2},
		{"stand outdoors keeps the levels", nil, &Override{Preset: id, Outdoor: &tr}, "2× AA", id, 2.2, row.LifeYears * 2},
	} {
		p := tb.Resolve("HmIP-WRC6", c.mOv, c.dOv, 0)
		if Describe(p.Slots) != c.slots || p.Preset != c.preset || p.Power != "battery" || p.LifeYears != c.life {
			t.Errorf("%s: %+v", c.name, p)
		}
		// LOW_BAT_LIMIT 2.2 stays the device's while the pack keeps the row's 3 V; rechargeables
		// change the chemistry and take its levels
		if c.name != "stand with rechargeables" && p.Low != c.low {
			t.Errorf("%s: low %v, want %v", c.name, p.Low, c.low)
		}
		if c.preset != "" && p.Source != "override" {
			t.Errorf("%s: source %q", c.name, p.Source)
		}
	}
	// a model the table lacks: the stand's slots, no levels of its own to keep
	if p := tb.Resolve("HB-UNI-Sen-TEMP-DS18B20", nil, stand, 0); Describe(p.Slots) != "2× AA" || p.Power != "battery" || p.Preset != id || p.Low == 0 {
		t.Errorf("not in the table: %+v", p)
	}
	for _, c := range []struct {
		o   Override
		err bool
	}{
		{Override{Preset: id}, false},
		{Override{Preset: id, Chemistry: "nimh", Outdoor: &tr}, false},
		{Override{Preset: "nope"}, true},
		{Override{Preset: id, Slots: []Slot{{Count: 2, Type: "AA"}}}, true},
		{Override{Preset: id, Power: "mains"}, true},
	} {
		if err := tb.Validate(c.o); (err != nil) != c.err {
			t.Errorf("Validate(%+v) = %v", c.o, err)
		}
	}
}
