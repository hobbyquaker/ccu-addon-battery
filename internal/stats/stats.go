// Package stats turns the accepted battery changes into lives and statistics: how long a device's
// batteries lasted (the time between two accepted changes), what a battery (vendor, product line,
// size) achieved per device model, batteries and euros per year. Pure functions over the rows the
// store holds; the forecast (P3) builds on the same lives.
package stats

import (
	"context"
	"database/sql"
	"encoding/json"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/hobbyquaker/ccu-addon-battery/internal/changes"
	"github.com/hobbyquaker/ccu-addon-battery/internal/ifaces"
	"github.com/hobbyquaker/ccu-addon-battery/internal/store"
)

const dayMs = 24 * 3600 * 1000

// Life is the time between two accepted changes of one device, or since the last one (To 0).
type Life struct {
	Interface string            `json:"interface"`
	Address   string            `json:"address"`
	Model     string            `json:"model"`
	ChangeID  int64             `json:"change_id"`
	From      int64             `json:"from"`
	To        int64             `json:"to,omitempty"`
	Days      float64           `json:"days"`
	Batteries []changes.Battery `json:"batteries"`
}

// Running reports a life that has not ended.
func (l Life) Running() bool { return l.To == 0 }

// Summary describes a set of completed lives.
type Summary struct {
	Count      int     `json:"count"`
	MinDays    float64 `json:"min_days,omitempty"`
	MaxDays    float64 `json:"max_days,omitempty"`
	AvgDays    float64 `json:"avg_days,omitempty"`
	MedianDays float64 `json:"median_days,omitempty"`
}

// DeviceLife is what the Devices table shows per device.
type DeviceLife struct {
	Summary
	// RunningSince is the last accepted change; RunningDays how long the current batteries are in.
	RunningSince int64   `json:"running_since,omitempty"`
	RunningDays  float64 `json:"running_days,omitempty"`
	// Hint is "short" when the device reports a low battery while its running life is well below
	// its own average (or its model's across the installation): a battery-hungry device.
	Hint string `json:"hint,omitempty"`
}

// ModelLife is one model's share of a battery row, or one battery's share of a model row.
type ModelLife struct {
	Model string `json:"model,omitempty"`
	// Key names the battery (vendor | product | type) in a model row.
	Key     string `json:"key,omitempty"`
	Vendor  string `json:"vendor,omitempty"`
	Product string `json:"product,omitempty"`
	Type    string `json:"type,omitempty"`
	Summary
	// CostPerYearCents is the mean cost per year of the lives with a price (0 without one).
	CostPerYearCents int `json:"cost_per_year_cents,omitempty"`
	PricedLives      int `json:"priced_lives,omitempty"`
}

// BatteryRow is one row of the Batteries tab: one vendor + product line + size ever recorded.
type BatteryRow struct {
	Key     string `json:"key"`
	Vendor  string `json:"vendor"`
	Product string `json:"product"`
	Type    string `json:"type"`
	// Total is how many single batteries went in, Changes how many changes used it.
	Total   int      `json:"total"`
	Changes int      `json:"changes"`
	Devices int      `json:"devices"`
	Models  []string `json:"models"`
	// Life is over the completed lives that started with this battery.
	Life Summary `json:"life"`
	// AvgPriceCents is the mean price per battery where one was recorded, PricedTotal how many
	// batteries had one; CostPerYearCents the mean cost per year of the lives with a price.
	AvgPriceCents    int         `json:"avg_price_cents,omitempty"`
	PricedTotal      int         `json:"priced_total,omitempty"`
	CostPerYearCents int         `json:"cost_per_year_cents,omitempty"`
	PricedLives      int         `json:"priced_lives,omitempty"`
	LastPriceCents   int         `json:"last_price_cents,omitempty"`
	LastAt           int64       `json:"last_at,omitempty"`
	PerModel         []ModelLife `json:"per_model"`
}

// ModelRow is one model's lives and the batteries used in it (the "best battery per model").
type ModelRow struct {
	Model     string      `json:"model"`
	Devices   int         `json:"devices"`
	Life      Summary     `json:"life"`
	Batteries []ModelLife `json:"batteries"`
}

// TypeRow is one battery size's lives across the installation.
type TypeRow struct {
	Type string  `json:"type"`
	Life Summary `json:"life"`
}

// Year counts the last 365 days.
type Year struct {
	From             int64          `json:"from"`
	Changes          int            `json:"changes"`
	Batteries        int            `json:"batteries"`
	ByType           map[string]int `json:"by_type"`
	CostCents        int            `json:"cost_cents"`
	PricedBatteries  int            `json:"priced_batteries"`
	AllBatteriesEver int            `json:"batteries_ever"`
	FirstChange      int64          `json:"first_change,omitempty"`
}

// Stats is everything the statistics pages draw.
type Stats struct {
	Now       int64                 `json:"now"`
	Devices   map[string]DeviceLife `json:"devices"`
	Lives     []Life                `json:"lives"`
	Batteries []BatteryRow          `json:"batteries"`
	Models    []ModelRow            `json:"models"`
	Types     []TypeRow             `json:"types"`
	Year      Year                  `json:"year"`
}

// Options steer Compute.
type Options struct {
	Now time.Time
	// LowBat says whether a device reports a low battery now (for the hint); nil: none does.
	LowBat func(iface, address string) bool
}

// Load reads the accepted changes and the devices, oldest change first.
func Load(ctx context.Context, db *sql.DB) ([]changes.Change, []store.Device, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, interface, address, at, batteries FROM changes WHERE status = ? AND `+ifaces.SQL("interface")+` ORDER BY at, id`, changes.StatusAccepted)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var cs []changes.Change
	for rows.Next() {
		var c changes.Change
		var bats sql.NullString
		if err := rows.Scan(&c.ID, &c.Interface, &c.Address, &c.At, &bats); err != nil {
			return nil, nil, err
		}
		if bats.Valid && bats.String != "" {
			_ = json.Unmarshal([]byte(bats.String), &c.Batteries)
		}
		cs = append(cs, c)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	drows, err := db.QueryContext(ctx, `SELECT interface, address, model, power, retired_at FROM devices WHERE `+ifaces.SQL("interface"))
	if err != nil {
		return nil, nil, err
	}
	defer drows.Close()
	var ds []store.Device
	for drows.Next() {
		var d store.Device
		var retired sql.NullInt64
		if err := drows.Scan(&d.Interface, &d.Address, &d.Model, &d.Power, &retired); err != nil {
			return nil, nil, err
		}
		d.RetiredAt = retired.Int64
		ds = append(ds, d)
	}
	return cs, ds, drows.Err()
}

// Lives derives the lives from the accepted changes (oldest first): per device one life from
// each change to the next, and a running one from the last change to now, unless the device is
// retired.
func Lives(cs []changes.Change, devs []store.Device, now time.Time) []Life {
	models := map[string]store.Device{}
	for _, d := range devs {
		models[d.Interface+"."+d.Address] = d
	}
	byDev := map[string][]changes.Change{}
	var order []string
	for _, c := range cs {
		k := c.Interface + "." + c.Address
		if _, ok := byDev[k]; !ok {
			order = append(order, k)
		}
		byDev[k] = append(byDev[k], c)
	}
	sort.Strings(order)
	nowMs := now.UnixMilli()
	var out []Life
	for _, k := range order {
		list := byDev[k]
		sort.SliceStable(list, func(i, j int) bool { return list[i].At < list[j].At })
		d := models[k]
		for i, c := range list {
			l := Life{Interface: c.Interface, Address: c.Address, Model: d.Model, ChangeID: c.ID, From: c.At, Batteries: c.Batteries}
			if l.Batteries == nil {
				l.Batteries = []changes.Battery{}
			}
			if i+1 < len(list) {
				l.To = list[i+1].At
				l.Days = days(l.To - l.From)
			} else {
				if d.RetiredAt > 0 || c.At > nowMs {
					continue
				}
				l.Days = days(nowMs - c.At)
			}
			out = append(out, l)
		}
	}
	return out
}

func days(ms int64) float64 { return math.Round(float64(ms)/dayMs*10) / 10 }

func summarize(ds []float64) Summary {
	s := Summary{Count: len(ds)}
	if len(ds) == 0 {
		return s
	}
	sorted := append([]float64(nil), ds...)
	sort.Float64s(sorted)
	sum := 0.0
	for _, d := range sorted {
		sum += d
	}
	s.MinDays, s.MaxDays = sorted[0], sorted[len(sorted)-1]
	s.AvgDays = math.Round(sum/float64(len(sorted))*10) / 10
	if n := len(sorted); n%2 == 1 {
		s.MedianDays = sorted[n/2]
	} else {
		s.MedianDays = math.Round((sorted[n/2-1]+sorted[n/2])/2*10) / 10
	}
	return s
}

// Key names a battery row: vendor, product and type, trimmed, lower-cased for grouping.
func Key(b changes.Battery) string {
	return strings.ToLower(strings.TrimSpace(b.Vendor)) + "|" + strings.ToLower(strings.TrimSpace(b.Product)) + "|" + strings.ToLower(strings.TrimSpace(b.Type))
}

// lifeCost is the cost of a life's batteries in cents when every slot has a price (ok false
// otherwise), and how many single batteries it holds.
func lifeCost(bats []changes.Battery) (cents int, count int, ok bool) {
	ok = len(bats) > 0
	for _, b := range bats {
		n := b.Count
		if n <= 0 {
			n = 1
		}
		count += n
		if b.PriceCents == nil {
			ok = false
			continue
		}
		cents += n * *b.PriceCents
	}
	return cents, count, ok
}

func perYear(cents int, d float64) int {
	if d <= 0 {
		return 0
	}
	return int(math.Round(float64(cents) / (d / 365.25)))
}

type acc struct {
	days      []float64
	costYear  []int
	modelDays map[string][]float64
	modelCost map[string][]int
}

func (a *acc) add(model string, d float64, cost int, priced bool) {
	a.days = append(a.days, d)
	if a.modelDays == nil {
		a.modelDays, a.modelCost = map[string][]float64{}, map[string][]int{}
	}
	a.modelDays[model] = append(a.modelDays[model], d)
	if priced {
		a.costYear = append(a.costYear, perYear(cost, d))
		a.modelCost[model] = append(a.modelCost[model], perYear(cost, d))
	}
}

func mean(xs []int) int {
	if len(xs) == 0 {
		return 0
	}
	s := 0
	for _, x := range xs {
		s += x
	}
	return int(math.Round(float64(s) / float64(len(xs))))
}

// Compute builds the statistics from the accepted changes and the devices.
func Compute(cs []changes.Change, devs []store.Device, o Options) Stats {
	now := o.Now
	if now.IsZero() {
		now = time.Now()
	}
	lives := Lives(cs, devs, now)
	st := Stats{Now: now.UnixMilli(), Devices: map[string]DeviceLife{}, Lives: lives, Batteries: []BatteryRow{}, Models: []ModelRow{}, Types: []TypeRow{}}
	devModel := map[string]string{}
	for _, d := range devs {
		devModel[d.Interface+"."+d.Address] = d.Model
	}

	// per device
	devDays := map[string][]float64{}
	running := map[string]Life{}
	for _, l := range lives {
		k := l.Interface + "." + l.Address
		if l.Running() {
			running[k] = l
		} else {
			devDays[k] = append(devDays[k], l.Days)
		}
	}
	// per model (the device's lives count for the model)
	modelDays := map[string][]float64{}
	modelDevs := map[string]map[string]bool{}
	modelBats := map[string]map[string]*acc{}
	// per battery and per type
	batRows := map[string]*BatteryRow{}
	batAcc := map[string]*acc{}
	typeDays := map[string][]float64{}
	for _, l := range lives {
		model := l.Model
		if modelDevs[model] == nil {
			modelDevs[model] = map[string]bool{}
		}
		modelDevs[model][l.Interface+"."+l.Address] = true
		cost, _, priced := lifeCost(l.Batteries)
		if !l.Running() {
			modelDays[model] = append(modelDays[model], l.Days)
		}
		for _, b := range l.Batteries {
			key := Key(b)
			row := batRows[key]
			if row == nil {
				row = &BatteryRow{Key: key, Vendor: strings.TrimSpace(b.Vendor), Product: strings.TrimSpace(b.Product), Type: strings.TrimSpace(b.Type), PerModel: []ModelLife{}}
				batRows[key] = row
				batAcc[key] = &acc{}
			}
			n := b.Count
			if n <= 0 {
				n = 1
			}
			row.Total += n
			row.Changes++
			if b.PriceCents != nil {
				row.PricedTotal += n
				row.AvgPriceCents += n * *b.PriceCents // a sum until the end
				if l.From >= row.LastAt {
					row.LastAt, row.LastPriceCents = l.From, *b.PriceCents
				}
			}
			if !l.Running() {
				batAcc[key].add(model, l.Days, cost, priced)
				if modelBats[model] == nil {
					modelBats[model] = map[string]*acc{}
				}
				if modelBats[model][key] == nil {
					modelBats[model][key] = &acc{}
				}
				modelBats[model][key].add(model, l.Days, cost, priced)
				t := strings.TrimSpace(b.Type)
				if t != "" {
					typeDays[t] = append(typeDays[t], l.Days)
				}
			}
		}
	}

	modelSummary := map[string]Summary{}
	for m, ds := range modelDays {
		modelSummary[m] = summarize(ds)
	}
	for _, d := range devs {
		k := d.Interface + "." + d.Address
		ds := devDays[k]
		r, hasRunning := running[k]
		if len(ds) == 0 && !hasRunning {
			continue
		}
		dl := DeviceLife{Summary: summarize(ds)}
		if hasRunning {
			dl.RunningSince, dl.RunningDays = r.From, r.Days
			ref := dl.AvgDays
			if dl.Count == 0 {
				ref = modelSummary[d.Model].AvgDays
			}
			if ref > 0 && o.LowBat != nil && o.LowBat(d.Interface, d.Address) && r.Days < 0.75*ref {
				dl.Hint = "short"
			}
		}
		st.Devices[k] = dl
	}

	// the battery rows
	for key, row := range batRows {
		a := batAcc[key]
		row.Life = summarize(a.days)
		if row.PricedTotal > 0 {
			row.AvgPriceCents = int(math.Round(float64(row.AvgPriceCents) / float64(row.PricedTotal)))
		}
		row.CostPerYearCents, row.PricedLives = mean(a.costYear), len(a.costYear)
		devSet := map[string]bool{}
		modelSet := map[string]bool{}
		for _, l := range lives {
			for _, b := range l.Batteries {
				if Key(b) == key {
					devSet[l.Interface+"."+l.Address] = true
					if l.Model != "" {
						modelSet[l.Model] = true
					}
				}
			}
		}
		row.Devices = len(devSet)
		for m := range modelSet {
			row.Models = append(row.Models, m)
		}
		sort.Strings(row.Models)
		if row.Models == nil {
			row.Models = []string{}
		}
		for m, ds := range a.modelDays {
			ml := ModelLife{Model: m, Summary: summarize(ds), CostPerYearCents: mean(a.modelCost[m]), PricedLives: len(a.modelCost[m])}
			row.PerModel = append(row.PerModel, ml)
		}
		sort.Slice(row.PerModel, func(i, j int) bool { return row.PerModel[i].Model < row.PerModel[j].Model })
		st.Batteries = append(st.Batteries, *row)
	}
	sort.Slice(st.Batteries, func(i, j int) bool {
		if st.Batteries[i].Total != st.Batteries[j].Total {
			return st.Batteries[i].Total > st.Batteries[j].Total
		}
		return st.Batteries[i].Key < st.Batteries[j].Key
	})

	// the model rows
	for m, devset := range modelDevs {
		mr := ModelRow{Model: m, Devices: len(devset), Life: modelSummary[m], Batteries: []ModelLife{}}
		for key, a := range modelBats[m] {
			row := batRows[key]
			mr.Batteries = append(mr.Batteries, ModelLife{Key: key, Vendor: row.Vendor, Product: row.Product, Type: row.Type, Summary: summarize(a.days), CostPerYearCents: mean(a.costYear), PricedLives: len(a.costYear)})
		}
		sort.Slice(mr.Batteries, func(i, j int) bool {
			if mr.Batteries[i].AvgDays != mr.Batteries[j].AvgDays {
				return mr.Batteries[i].AvgDays > mr.Batteries[j].AvgDays
			}
			return mr.Batteries[i].Key < mr.Batteries[j].Key
		})
		st.Models = append(st.Models, mr)
	}
	sort.Slice(st.Models, func(i, j int) bool { return st.Models[i].Model < st.Models[j].Model })

	for t, ds := range typeDays {
		st.Types = append(st.Types, TypeRow{Type: t, Life: summarize(ds)})
	}
	sort.Slice(st.Types, func(i, j int) bool { return st.Types[i].Type < st.Types[j].Type })

	// the last 365 days
	y := Year{From: now.Add(-365 * 24 * time.Hour).UnixMilli(), ByType: map[string]int{}}
	for i, c := range cs {
		if i == 0 || c.At < y.FirstChange {
			y.FirstChange = c.At
		}
		for _, b := range c.Batteries {
			n := b.Count
			if n <= 0 {
				n = 1
			}
			y.AllBatteriesEver += n
			if c.At < y.From {
				continue
			}
			y.Batteries += n
			if t := strings.TrimSpace(b.Type); t != "" {
				y.ByType[t] += n
			}
			if b.PriceCents != nil {
				y.CostCents += n * *b.PriceCents
				y.PricedBatteries += n
			}
		}
		if c.At >= y.From {
			y.Changes++
		}
	}
	st.Year = y
	return st
}
