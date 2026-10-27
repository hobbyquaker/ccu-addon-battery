// Package batteries is the battery table (data/batteries.json) as the addon uses it: a
// case-insensitive model lookup with aliases, the per-chemistry levels, and the profile of one
// device - its power source, slots and the pack levels the detector compares with - from the
// table, a per-model override and a per-device override, in that order.
package batteries

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/hobbyquaker/ccu-addon-battery/data"
)

// Table is the parsed batteries.json.
type Table struct {
	Version     int                  `json:"version"`
	Updated     string               `json:"updated"`
	Types       map[string]Type      `json:"types"`
	Chemistries map[string]Chemistry `json:"chemistries"`
	Models      map[string]Model     `json:"models"`

	index map[string]string // lower-case model or alias -> key in Models
}

// Type is one battery type a slot may name (AA, CR2032, ...).
type Type struct {
	IEC         string   `json:"iec,omitempty"`
	Names       []string `json:"names,omitempty"`
	NominalV    float64  `json:"nominal_v"`
	Chemistry   string   `json:"chemistry"`
	Replaceable *bool    `json:"replaceable,omitempty"`
}

// Chemistry holds the per-cell levels of the detection rules.
type Chemistry struct {
	Fresh float64 `json:"fresh"`
	Low   float64 `json:"low"`
	Step  float64 `json:"step"`
}

// Slot is count x type, optionally with another chemistry than the type's.
type Slot struct {
	Count       int    `json:"count"`
	Type        string `json:"type"`
	Chemistry   string `json:"chemistry,omitempty"`
	Replaceable *bool  `json:"replaceable,omitempty"`
}

// Voltage is the datapoint carrying the battery voltage and the device's own levels.
type Voltage struct {
	DP    string  `json:"dp"`
	Low   float64 `json:"low,omitempty"`
	Fresh float64 `json:"fresh,omitempty"`
}

// Model is one row of the table (the fields the addon uses; sources and notes stay in the file).
type Model struct {
	Power       string   `json:"power"`
	Family      string   `json:"family,omitempty"`
	Slots       []Slot   `json:"slots,omitempty"`
	Voltage     *Voltage `json:"voltage"`
	Outdoor     bool     `json:"outdoor,omitempty"`
	LowBatLimit float64  `json:"low_bat_limit,omitempty"`
	LifeYears   float64  `json:"life_years,omitempty"`
	Aliases     []string `json:"aliases,omitempty"`
	Confidence  string   `json:"confidence"`
}

// Parse reads a table.
func Parse(b []byte) (*Table, error) {
	var t Table
	if err := json.Unmarshal(b, &t); err != nil {
		return nil, err
	}
	if len(t.Models) == 0 {
		return nil, errors.New("batteries.json has no models")
	}
	t.index = map[string]string{}
	for k, m := range t.Models {
		t.index[strings.ToLower(k)] = k
		for _, a := range m.Aliases {
			if _, dup := t.index[strings.ToLower(a)]; !dup {
				t.index[strings.ToLower(a)] = k
			}
		}
	}
	return &t, nil
}

var shipped *Table

// Shipped is the table embedded in the binary.
func Shipped() *Table {
	if shipped == nil {
		t, err := Parse(data.Batteries)
		if err != nil {
			panic("the embedded batteries.json does not parse: " + err.Error())
		}
		shipped = t
	}
	return shipped
}

// Lookup finds a model by its TYPE, case-insensitively and through the aliases; the second
// result is the table's own spelling.
func (t *Table) Lookup(model string) (Model, string, bool) {
	k, ok := t.index[strings.ToLower(strings.TrimSpace(model))]
	if !ok {
		return Model{}, "", false
	}
	return t.Models[k], k, true
}

// TypeNames lists the slot types, sorted (for the type suggestions).
func (t *Table) TypeNames() []string {
	out := make([]string, 0, len(t.Types))
	for k := range t.Types {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Override changes what the table says, for one model (fixing the table locally until a release
// carries the fix) or one device (a remote in a desk stand, an eTRV on lithium cells, a slot of
// rechargeables). A zero field leaves the value below it alone.
type Override struct {
	Power        string `json:"power,omitempty"`
	Slots        []Slot `json:"slots,omitempty"`
	Chemistry    string `json:"chemistry,omitempty"`
	Rechargeable *bool  `json:"rechargeable,omitempty"`
	Outdoor      *bool  `json:"outdoor,omitempty"`
}

// Powers a profile may name. The table's six, plus external (the device reports an external
// supply) and unknown (no row).
var Powers = []string{"battery", "rechargeable", "battery-or-dc", "mains", "bus", "host", "external", "unknown"}

// Validate checks an override against the table's types and chemistries.
func (t *Table) Validate(o Override) error {
	if o.Power != "" && !contains(Powers, o.Power) {
		return fmt.Errorf("unknown power %q", o.Power)
	}
	if o.Chemistry != "" {
		if _, ok := t.Chemistries[o.Chemistry]; !ok {
			return fmt.Errorf("unknown chemistry %q", o.Chemistry)
		}
	}
	for i, s := range o.Slots {
		if s.Count < 1 || s.Count > 12 {
			return fmt.Errorf("slot %d: count %d is not 1-12", i+1, s.Count)
		}
		if _, ok := t.Types[s.Type]; !ok {
			return fmt.Errorf("slot %d: unknown type %q", i+1, s.Type)
		}
		if s.Chemistry != "" {
			if _, ok := t.Chemistries[s.Chemistry]; !ok {
				return fmt.Errorf("slot %d: unknown chemistry %q", i+1, s.Chemistry)
			}
		}
	}
	return nil
}

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

// Profile is what the addon knows about one device's power: where it came from, the slots and
// the pack levels the detector uses (0 when the device has no voltage to compare).
type Profile struct {
	// Source is table, override (a model or device override changed something), guess (no row:
	// the cell count guessed from the voltage) or none.
	Source     string  `json:"source"`
	Model      string  `json:"model,omitempty"` // the table's spelling
	Power      string  `json:"power"`
	Slots      []Slot  `json:"slots"`
	Fresh      float64 `json:"fresh,omitempty"`
	Low        float64 `json:"low,omitempty"`
	Step       float64 `json:"step,omitempty"`
	Outdoor    bool    `json:"outdoor,omitempty"`
	LifeYears  float64 `json:"life_years,omitempty"`
	Confidence string  `json:"confidence,omitempty"`
}

// Detects says whether the detector runs for the device at all: not for mains, bus, host or
// externally powered devices, nor for fixed cells.
func (p Profile) Detects() bool {
	switch p.Power {
	case "mains", "bus", "host", "external":
		return false
	}
	for _, s := range p.Slots {
		if s.Type == "fixed" {
			return false
		}
	}
	return true
}

// Resolve builds the profile of a device of model from the table and the overrides (model, then
// device; either may be nil). maxV, when > 0, is the highest voltage seen on the device: for a
// model without a row it guesses the cell count (1 to 3 cells of alkaline), so the detector has
// levels at all.
func (t *Table) Resolve(model string, modelOv, devOv *Override, maxV float64) Profile {
	p := Profile{Source: "none", Power: "unknown", Slots: []Slot{}}
	var lowLimit, fresh float64
	if m, key, ok := t.Lookup(model); ok {
		p.Source, p.Model, p.Power, p.Outdoor, p.LifeYears, p.Confidence = "table", key, m.Power, m.Outdoor, m.LifeYears, m.Confidence
		p.Slots = append(p.Slots, m.Slots...)
		if m.Voltage != nil {
			lowLimit, fresh = m.Voltage.Low, m.Voltage.Fresh
		}
		if lowLimit == 0 {
			lowLimit = m.LowBatLimit
		}
	}
	changed := false
	for _, o := range []*Override{modelOv, devOv} {
		if o == nil {
			continue
		}
		if o.Power != "" {
			p.Power, changed = o.Power, true
		}
		if len(o.Slots) > 0 {
			p.Slots, changed = append([]Slot{}, o.Slots...), true
			fresh = 0 // the row's own fresh level was for its own slots
		}
		chem := o.Chemistry
		if chem == "" && o.Rechargeable != nil && *o.Rechargeable {
			chem = "nimh"
		}
		if chem != "" {
			for i := range p.Slots {
				p.Slots[i].Chemistry = chem
			}
			fresh, changed = 0, true
		}
		if o.Outdoor != nil {
			p.Outdoor, changed = *o.Outdoor, true
		}
	}
	if changed {
		p.Source = "override"
		if p.Power == "unknown" && len(p.Slots) > 0 {
			p.Power = "battery"
		}
	}
	if p.Source == "none" && maxV > 0 {
		cells := 1
		switch {
		case maxV > 3.6:
			cells = 3
		case maxV > 1.8:
			cells = 2
		}
		p.Source, p.Power, p.Slots = "guess", "battery", []Slot{{Count: cells, Type: "AA"}}
	}
	// the pack levels: count x per-cell of each slot's chemistry
	var f, l, s float64
	for _, sl := range p.Slots {
		chem := sl.Chemistry
		if chem == "" {
			chem = t.Types[sl.Type].Chemistry
		}
		c, ok := t.Chemistries[chem]
		if !ok || sl.Type == "fixed" {
			continue
		}
		n := float64(sl.Count)
		f, l, s = f+n*c.Fresh, l+n*c.Low, s+n*c.Step
	}
	if f > 0 {
		p.Fresh, p.Low, p.Step = round2(f), round2(l), round2(s)
		if fresh > 0 {
			p.Fresh = fresh
		}
		if lowLimit > 0 && !changed {
			p.Low = lowLimit
		}
		if p.Outdoor {
			p.Step = round2(p.Step + 0.1)
		}
	}
	return p
}

func round2(f float64) float64 { return math.Round(f*100) / 100 }

// Describe renders slots as "2× AAA", "1× CR2032 + 2× AA".
func Describe(slots []Slot) string {
	var parts []string
	for _, s := range slots {
		x := fmt.Sprintf("%d× %s", s.Count, s.Type)
		if s.Chemistry != "" {
			x += " (" + s.Chemistry + ")"
		}
		parts = append(parts, x)
	}
	return strings.Join(parts, " + ")
}
