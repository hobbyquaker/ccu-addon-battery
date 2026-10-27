package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/hobbyquaker/ccu-addon-battery/internal/batteries"
	"github.com/hobbyquaker/ccu-addon-battery/internal/changes"
	"github.com/hobbyquaker/ccu-addon-battery/internal/forecast"
	"github.com/hobbyquaker/ccu-addon-battery/internal/notify"
	"github.com/hobbyquaker/ccu-addon-battery/internal/settings"
	"github.com/hobbyquaker/ccu-addon-battery/internal/stats"
	"github.com/hobbyquaker/ccu-addon-kit/go/addonkit"
)

// DeviceForecast is a battery device with its forecast (GET forecast, GET shopping).
type DeviceForecast struct {
	Interface string           `json:"interface"`
	Address   string           `json:"address"`
	Name      string           `json:"name"`
	Rooms     []string         `json:"rooms"`
	Model     string           `json:"model"`
	Image     string           `json:"image,omitempty"`
	Slots     []batteries.Slot `json:"slots"`
	LowBat    bool             `json:"low_bat"`
	Voltage   *float64         `json:"voltage"`
	Forecast  forecast.Result  `json:"forecast"`
}

var stateRank = map[string]int{forecast.StateLow: 0, forecast.StateDue: 1, forecast.StateSoon: 2, forecast.StateOK: 3, forecast.StateUnknown: 4}

func (o Options) currentSettings() settings.Settings {
	if o.Settings == nil {
		return settings.Defaults()
	}
	return o.Settings.Get()
}

// forecasts computes every battery device's forecast: the devices from the recorder, their
// lives from the statistics, their last weeks of samples from the store.
func (o Options) forecasts(ctx context.Context, lang string, soonDays float64) ([]DeviceForecast, error) {
	st, err := o.computeStatsCtx(ctx)
	if err != nil {
		return nil, err
	}
	models := map[string]stats.Summary{}
	for _, m := range st.Models {
		models[strings.ToLower(m.Model)] = m.Life
	}
	now := time.Now()
	out := []DeviceForecast{}
	for _, v := range o.Recorder.Devices(lang) {
		if !v.Profile.Detects() && v.Profile.Power != "battery" && v.Profile.Power != "rechargeable" && v.Profile.Power != "unknown" {
			continue
		}
		if v.Profile.Power == "mains" || v.Profile.Power == "bus" || v.Profile.Power == "host" || v.Profile.Power == "external" {
			continue
		}
		key := v.Interface + "." + v.Address
		in := forecast.Input{Profile: v.Profile, Model: models[strings.ToLower(v.Model)], Now: now, LowBat: v.LowBat != nil && *v.LowBat}
		if l, ok := st.Devices[key]; ok {
			life := l
			in.Life = &life
		}
		if v.VoltageDP != "" {
			samples, err := o.Store.Samples(ctx, v.Interface, v.Address, now.Add(-forecast.TrendWindow).UnixMilli(), now.UnixMilli())
			if err == nil {
				in.Samples = samples
			}
		}
		d := DeviceForecast{Interface: v.Interface, Address: v.Address, Name: v.Name, Rooms: v.Rooms, Model: v.Model, Image: o.image(v.Model), Slots: v.Profile.Slots, LowBat: in.LowBat, Voltage: v.Voltage, Forecast: forecast.Compute(in, soonDays)}
		if d.Slots == nil {
			d.Slots = []batteries.Slot{}
		}
		if d.Rooms == nil {
			d.Rooms = []string{}
		}
		out = append(out, d)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i].Forecast, out[j].Forecast
		if stateRank[a.State] != stateRank[b.State] {
			return stateRank[a.State] < stateRank[b.State]
		}
		if a.DueAt != b.DueAt {
			if a.DueAt == 0 || b.DueAt == 0 {
				return a.DueAt != 0
			}
			return a.DueAt < b.DueAt
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

func (o Options) computeStatsCtx(ctx context.Context) (*stats.Stats, error) {
	r, _ := http.NewRequestWithContext(ctx, http.MethodGet, "/", nil)
	return o.computeStats(r)
}

func daysParam(r *http.Request, def int) float64 {
	if v, err := strconv.Atoi(r.URL.Query().Get("days")); err == nil && v > 0 && v <= 3650 {
		return float64(v)
	}
	return float64(def)
}

// forecastList answers GET forecast: every battery device with when its batteries are due,
// the urgent ones first.
func (o Options) forecastList(w http.ResponseWriter, r *http.Request) {
	days := daysParam(r, o.currentSettings().Shopping.Days)
	list, err := o.forecasts(r.Context(), lang(r), days)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"days": days, "devices": list})
}

// ShoppingItem is one line of the shopping list.
type ShoppingItem struct {
	Type    string   `json:"type"`
	Needed  int      `json:"needed"`
	Stock   int      `json:"stock"`
	Buy     int      `json:"buy"`
	Devices []string `json:"devices"`
}

// shopping answers GET shopping: what the devices due within the horizon (or low now) need,
// per battery type, minus the stock.
func (o Options) shopping(w http.ResponseWriter, r *http.Request) {
	s := o.currentSettings()
	days := daysParam(r, s.Shopping.Days)
	list, err := o.forecasts(r.Context(), lang(r), days)
	if err != nil {
		writeErr(w, err)
		return
	}
	due := []DeviceForecast{}
	byType := map[string]*ShoppingItem{}
	for _, d := range list {
		if d.Forecast.State != forecast.StateLow && d.Forecast.State != forecast.StateDue && d.Forecast.State != forecast.StateSoon {
			continue
		}
		due = append(due, d)
		for _, sl := range d.Slots {
			if sl.Type == "" || sl.Type == "fixed" || sl.Type == "pack" {
				continue
			}
			it := byType[sl.Type]
			if it == nil {
				it = &ShoppingItem{Type: sl.Type, Stock: s.Shopping.Stock[sl.Type], Devices: []string{}}
				byType[sl.Type] = it
			}
			n := sl.Count
			if n <= 0 {
				n = 1
			}
			it.Needed += n
			name := d.Name
			if name == "" {
				name = d.Address
			}
			it.Devices = append(it.Devices, name)
		}
	}
	items := []ShoppingItem{}
	for _, it := range byType {
		it.Buy = it.Needed - it.Stock
		if it.Buy < 0 {
			it.Buy = 0
		}
		items = append(items, *it)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Buy != items[j].Buy {
			return items[i].Buy > items[j].Buy
		}
		return items[i].Type < items[j].Type
	})
	writeJSON(w, 200, map[string]any{"days": days, "stock": s.Shopping.Stock, "items": items, "devices": due})
}

// putSettings takes the shopping and notification settings (either or both) and saves them.
func (o Options) putSettings(w http.ResponseWriter, r *http.Request) {
	if o.Settings == nil {
		addonkit.WriteError(w, http.StatusNotImplemented, "unavailable", "no settings file")
		return
	}
	var body struct {
		Shopping *settings.Shopping `json:"shopping"`
		Notify   *settings.Notify   `json:"notify"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		addonkit.WriteError(w, http.StatusBadRequest, "invalid", err.Error())
		return
	}
	cur := o.Settings.Get()
	if body.Shopping != nil {
		cur.Shopping = *body.Shopping
	}
	if body.Notify != nil {
		cur.Notify = *body.Notify
	}
	if err := o.Settings.Set(cur); err != nil {
		addonkit.WriteError(w, http.StatusUnprocessableEntity, "invalid", err.Error())
		return
	}
	o.settings(w, r)
}

// notifyTest sends a test notice over the channel as it is saved.
func (o Options) notifyTest(w http.ResponseWriter, r *http.Request) {
	if o.Notifier == nil {
		addonkit.WriteError(w, http.StatusNotImplemented, "unavailable", "no notifier")
		return
	}
	lang := o.currentSettings().Notify.Language
	e := notify.Event{Kind: "test", Key: "test", Title: notify.Text(lang, "test.title", nil), Text: notify.Text(lang, "test.text", nil), Link: o.BasePath + "/", At: time.Now().UnixMilli()}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	if err := o.Notifier.Send(ctx, e); err != nil {
		addonkit.WriteError(w, http.StatusBadGateway, "send-failed", err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

// Events collects what the notifier should say now: low batteries, changes due or due soon,
// detections waiting in the inbox.
func (o Options) Events(ctx context.Context) []notify.Event {
	s := o.currentSettings()
	lang := s.Notify.Language
	var out []notify.Event
	list, err := o.forecasts(ctx, lang, float64(s.Shopping.Days))
	if err == nil {
		for _, d := range list {
			name := d.Name
			if name == "" {
				name = d.Address
			}
			p := map[string]string{"name": name, "model": d.Model, "days": strconv.Itoa(int(absF(d.Forecast.DaysLeft)))}
			key := d.Interface + "." + d.Address
			base := notify.Event{Device: d.Address, Name: name, Model: d.Model, Link: o.BasePath + "/"}
			switch d.Forecast.State {
			case forecast.StateLow:
				e := base
				e.Kind, e.Key, e.Title, e.Text = "low", key, notify.Text(lang, "low.title", p), notify.Text(lang, "low.text", p)
				out = append(out, e)
			case forecast.StateDue:
				e := base
				e.Kind, e.Key, e.Title, e.Text = "due", key+"|"+strconv.FormatInt(d.Forecast.DueAt, 10), notify.Text(lang, "due.title", p), notify.Text(lang, "due.text", p)
				out = append(out, e)
			case forecast.StateSoon:
				e := base
				e.Kind, e.Key, e.Title, e.Text = "soon", key+"|"+strconv.FormatInt(d.Forecast.DueAt, 10), notify.Text(lang, "soon.title", p), notify.Text(lang, "soon.text", p)
				out = append(out, e)
			}
		}
	}
	if o.Changes != nil {
		if pending, err := o.Changes.List(ctx, changes.Filter{Statuses: []string{changes.StatusPending}}); err == nil && len(pending) > 0 {
			newest := pending[0].ID
			for _, c := range pending {
				if c.ID > newest {
					newest = c.ID
				}
			}
			p := map[string]string{"count": strconv.Itoa(len(pending))}
			out = append(out, notify.Event{Kind: "inbox", Key: strconv.FormatInt(newest, 10), Title: notify.Text(lang, "inbox.title", p), Text: notify.Text(lang, "inbox.text", p), Link: o.BasePath + "/"})
		}
	}
	return out
}

func absF(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}
