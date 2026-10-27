// Package recorder records the battery-relevant datapoints of every device: it learns the devices
// and which of them report a voltage or a low-battery flag, seeds their last values from the
// state store (and, for HmIP, from hmipserver's cache, which costs no air), follows the event
// stream, keeps the current values, and writes downsampled samples and the evidence events into
// the store. It records; the detection of a battery change is P1's.
package recorder

import (
	"context"
	"encoding/json"
	"log"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/hobbyquaker/ccu-addon-battery/internal/store"
	"github.com/hobbyquaker/ccu-addon-kit/go/litesource"
)

// Keys are the datapoints the recorder subscribes to.
var Keys = []string{"OPERATING_VOLTAGE", "OPERATING_VOLTAGE_STATUS", "LOW_BAT", "LOWBAT", "BATTERY_STATE", "BOOTED", "UNREACH"}

// Downsampling: a row when the voltage moved by Step or more since the last row, the low-battery
// flag changed, or MaxGap passed since the last row, whichever comes first.
const (
	Step   = 0.01
	MaxGap = 24 * time.Hour
)

// Options configure the recorder.
type Options struct {
	Source litesource.Source
	Store  *store.Store
	Log    func(string, ...any)
	// ReadPause is the pause between two HmIP :0 reads at the seed (200 ms, as the system's own
	// sweep); a test shortens it.
	ReadPause time.Duration
	// Now is the clock; time.Now by default.
	Now func() time.Time
}

// Recorder is the running recorder.
type Recorder struct {
	o     Options
	logf  func(string, ...any)
	mu    sync.Mutex
	devs  map[string]*device // by interface + "." + address
	names *litesource.Names
	state string // starting | seeding | live | degraded
	last  map[string]store.Sample
}

type device struct {
	store.Device
	cur     store.Current
	hmip    bool
	tracked bool
}

// New makes a recorder; Run starts it.
func New(o Options) *Recorder {
	if o.Log == nil {
		o.Log = log.Printf
	}
	if o.ReadPause == 0 {
		o.ReadPause = 200 * time.Millisecond
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	r := &Recorder{o: o, logf: o.Log, devs: map[string]*device{}, state: "starting", last: map[string]store.Sample{}}
	return r
}

// State is starting, seeding, live or degraded (the stream is reconnecting).
func (r *Recorder) State() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.state
}

func (r *Recorder) setState(s string) {
	r.mu.Lock()
	r.state = s
	r.mu.Unlock()
}

// Run loads what the store has, learns the devices, seeds and follows the stream until ctx ends.
func (r *Recorder) Run(ctx context.Context) error {
	r.loadStore(ctx)
	if err := r.learnDevices(ctx); err != nil {
		r.logf("recorder: learning the devices: %v (will retry with the stream)", err)
	}
	r.refreshNames(ctx)
	go r.followNames(ctx)
	snap, err := r.seed(ctx)
	since := ""
	if err != nil {
		r.logf("recorder: seed failed: %v (recording from the stream alone)", err)
	} else {
		since = snap
	}
	r.setState("live")
	ch := r.o.Source.Subscribe(ctx, litesource.Filter{Keys: Keys}, since)
	for m := range ch {
		switch m.Type {
		case "hello":
			r.setState("live")
		case "event", "state":
			if m.Event != nil {
				r.apply(*m.Event, m.Event.Source == "event")
			}
		case "resync":
			r.logf("recorder: resync (%s): reading the state again", m.Resync.Reason)
			r.setState("seeding")
			if _, err := r.seed(ctx); err != nil {
				r.logf("recorder: re-seed failed: %v", err)
			}
			r.setState("live")
		case "newDevices", "readdedDevice", "replaceDevice", "updateDevice":
			if err := r.learnDevices(ctx); err != nil {
				r.logf("recorder: learning the devices: %v", err)
			}
		case "deleteDevices":
			if m.Devices != nil {
				r.retire(m.Devices.Interface, m.Devices.Addresses)
			}
		}
	}
	return ctx.Err()
}

func (r *Recorder) loadStore(ctx context.Context) {
	devs, err := r.o.Store.Devices(ctx)
	if err != nil {
		r.logf("recorder: the store is not readable yet: %v", err)
		return
	}
	curs, _ := r.o.Store.Currents(ctx)
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, d := range devs {
		if d.RetiredAt != 0 {
			continue
		}
		r.devs[d.Interface+"."+d.Address] = &device{Device: d, hmip: strings.HasPrefix(d.Interface, "HmIP"), tracked: d.VoltageDP != "" || d.LowBatDP != "",
			cur: store.Current{Interface: d.Interface, Address: d.Address}}
	}
	for _, c := range curs {
		if d := r.devs[c.Interface+"."+c.Address]; d != nil {
			d.cur = c
		}
	}
	for k, d := range r.devs {
		if d.cur.VoltageTS != 0 {
			r.last[k] = store.Sample{TS: d.cur.VoltageTS, Voltage: d.cur.Voltage, LowBat: d.cur.LowBat}
		}
	}
}

// learnDevices lists every interface's devices and finds, from the VALUES descriptions, which
// datapoints each device reports; the result is upserted into the store.
func (r *Recorder) learnDevices(ctx context.Context) error {
	ifaces, err := r.o.Source.Interfaces(ctx)
	if err != nil {
		return err
	}
	now := r.o.Now().UnixMilli()
	for _, iface := range ifaces {
		descs, err := r.o.Source.Devices(ctx, iface.Name)
		if err != nil {
			r.logf("recorder: %s: listDevices: %v", iface.Name, err)
			continue
		}
		byAddr := map[string]litesource.DeviceDescription{}
		var want []string
		for _, d := range descs {
			byAddr[d.Address] = d
			if strings.HasSuffix(d.Address, ":0") || isThermostatChannel(d.Type) {
				want = append(want, d.Address)
			}
		}
		keys := map[string]string{}
		sets := litesource.ParamsetCache{}
		for i := 0; i < len(want); i += 500 {
			end := i + 500
			if end > len(want) {
				end = len(want)
			}
			k, s, err := r.o.Source.Descriptions(ctx, iface.Name, want[i:end])
			if err != nil {
				r.logf("recorder: %s: descriptions: %v", iface.Name, err)
				break
			}
			for a, key := range k {
				keys[a] = key
			}
			for key, set := range s {
				sets[key] = set
			}
		}
		for _, d := range descs {
			if strings.Contains(d.Address, ":") || d.Address == "" {
				continue
			}
			dev := store.Device{Interface: iface.Name, Address: d.Address, Model: d.Type, Firmware: d.Firmware, Power: "unknown", FirstSeen: now, LastSeen: now}
			if set := sets[keys[d.Address+":0"]]; set != nil {
				if _, ok := set["OPERATING_VOLTAGE"]; ok {
					dev.VoltageDP = "0/OPERATING_VOLTAGE"
				}
				if _, ok := set["LOW_BAT"]; ok {
					dev.LowBatDP = "0/LOW_BAT"
				} else if _, ok := set["LOWBAT"]; ok {
					dev.LowBatDP = "0/LOWBAT"
				}
			}
			if dev.VoltageDP == "" {
				for _, ch := range d.Children {
					c := byAddr[ch]
					if set := sets[keys[ch]]; set != nil && isThermostatChannel(c.Type) {
						if _, ok := set["BATTERY_STATE"]; ok {
							_, idx, _ := strings.Cut(ch, ":")
							dev.VoltageDP = idx + "/BATTERY_STATE"
							break
						}
					}
				}
			}
			r.mu.Lock()
			k := iface.Name + "." + d.Address
			ex := r.devs[k]
			if ex == nil {
				ex = &device{cur: store.Current{Interface: iface.Name, Address: d.Address}}
				r.devs[k] = ex
			} else {
				dev.FirstSeen = ex.FirstSeen
				if ex.Power != "unknown" {
					dev.Power = ex.Power
				}
			}
			ex.Device = dev
			ex.hmip = strings.HasPrefix(iface.Name, "HmIP")
			ex.tracked = dev.VoltageDP != "" || dev.LowBatDP != ""
			r.mu.Unlock()
			if ex.tracked {
				r.o.Store.UpsertDevice(dev)
			}
		}
	}
	return nil
}

func isThermostatChannel(t string) bool {
	return t == "CLIMATECONTROL_RT_TRANSCEIVER" || t == "THERMALCONTROL_TRANSMIT"
}

func (r *Recorder) retire(iface string, addresses []string) {
	now := r.o.Now().UnixMilli()
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, a := range addresses {
		if strings.Contains(a, ":") {
			continue
		}
		if d, ok := r.devs[iface+"."+a]; ok {
			delete(r.devs, iface+"."+a)
			if d.tracked {
				r.o.Store.RetireDevice(iface, a, now)
			}
		}
	}
}

// seed reads the state store for the keys (what it keeps: the low-battery flags, UNREACH), then
// every tracked HmIP device's :0 VALUES from hmipserver's cache (free on air; never BidCos). It
// answers the stream position to subscribe from.
func (r *Recorder) seed(ctx context.Context) (string, error) {
	r.setState("seeding")
	snap, err := r.o.Source.Seed(ctx, litesource.Filter{Keys: Keys})
	if err != nil {
		return "", err
	}
	for _, e := range snap.Entries {
		r.apply(e, false)
	}
	r.mu.Lock()
	var hmip []*device
	for _, d := range r.devs {
		if d.hmip && d.tracked {
			hmip = append(hmip, d)
		}
	}
	r.mu.Unlock()
	sort.Slice(hmip, func(i, j int) bool { return hmip[i].Address < hmip[j].Address })
	ts := r.o.Now()
	for i, d := range hmip {
		if i > 0 {
			select {
			case <-ctx.Done():
				return snap.EventID, ctx.Err()
			case <-time.After(r.o.ReadPause):
			}
		}
		vals, err := r.o.Source.Read(ctx, d.Interface, d.Address+":0", "VALUES")
		if err != nil {
			r.logf("recorder: %s %s:0 VALUES: %v", d.Interface, d.Address, err)
			continue
		}
		for k, v := range vals {
			if !isKey(k) {
				continue
			}
			r.apply(litesource.Event{Interface: d.Interface, Address: d.Address + ":0", Key: k, Value: json.RawMessage(v), TS: ts, Confirmed: true, Source: "sweep"}, false)
		}
	}
	return snap.EventID, nil
}

func isKey(k string) bool {
	for _, x := range Keys {
		if x == k {
			return true
		}
	}
	return false
}

// apply takes one report. live says it is a live event (evidence); a seed, a sweep or a
// restored entry updates the current values and the samples but never the events table.
func (r *Recorder) apply(e litesource.Event, live bool) {
	key := e.Interface + "." + e.Device()
	r.mu.Lock()
	d := r.devs[key]
	if d == nil || !d.tracked {
		r.mu.Unlock()
		return
	}
	ts := e.TS
	if ts.IsZero() {
		ts = r.o.Now()
	}
	ms := ts.UnixMilli()
	when := ms
	if !e.LC.IsZero() {
		when = e.LC.UnixMilli() // since when the value stands, when the store says
	}
	cur := &d.cur
	changed := false
	var sample bool
	switch e.Key {
	case "OPERATING_VOLTAGE", "BATTERY_STATE":
		if want := strconv.Itoa(e.Channel()) + "/" + e.Key; want != d.VoltageDP {
			break
		}
		v, ok := e.Float()
		if !ok {
			break
		}
		if cur.Voltage == nil || *cur.Voltage != v {
			changed = true
		}
		vv := v
		cur.Voltage, cur.VoltageTS = &vv, ms
		sample = true
	case "OPERATING_VOLTAGE_STATUS":
		s := statusName(e)
		if s != cur.VoltageStatus {
			cur.VoltageStatus = s
			changed = true
			if s == "EXTERNAL" && d.Power == "unknown" {
				d.Power = "external"
				r.o.Store.UpsertDevice(d.Device)
			}
		}
	case "LOW_BAT", "LOWBAT":
		b, ok := e.Bool()
		if !ok {
			break
		}
		if cur.LowBat == nil || *cur.LowBat != b {
			changed = true
			if live && cur.LowBat != nil {
				r.o.Store.AddEvent(store.Event{Interface: d.Interface, Address: d.Address, TS: ms, Key: e.Key, Value: strconv.FormatBool(b)})
			}
			cur.LowBatSince = when
			sample = true
		}
		bb := b
		cur.LowBat = &bb
	case "UNREACH":
		b, ok := e.Bool()
		if !ok {
			break
		}
		if cur.Unreach == nil || *cur.Unreach != b {
			changed = true
			if live && cur.Unreach != nil {
				r.o.Store.AddEvent(store.Event{Interface: d.Interface, Address: d.Address, TS: ms, Key: e.Key, Value: strconv.FormatBool(b)})
			}
			cur.UnreachSince = when
		}
		bb := b
		cur.Unreach = &bb
	case "BOOTED":
		b, ok := e.Bool()
		if !ok {
			break
		}
		if live && b && (cur.Booted == nil || !*cur.Booted) {
			r.o.Store.AddEvent(store.Event{Interface: d.Interface, Address: d.Address, TS: ms, Key: e.Key, Value: "true"})
		}
		if cur.Booted == nil || *cur.Booted != b {
			changed = true
		}
		bb := b
		cur.Booted = &bb
	default:
		r.mu.Unlock()
		return
	}
	if ms > cur.LastReport {
		cur.LastReport = ms
	}
	if sample && cur.Voltage != nil || sample && e.Key != "OPERATING_VOLTAGE" && e.Key != "BATTERY_STATE" && d.VoltageDP == "" {
		r.maybeSample(key, d, ms, live)
	}
	if changed || sample {
		r.o.Store.PutCurrent(*cur)
	}
	r.mu.Unlock()
}

func statusName(e litesource.Event) string {
	if f, ok := e.Float(); ok {
		names := []string{"NORMAL", "UNKNOWN", "OVERFLOW", "EXTERNAL"}
		if i := int(f); i >= 0 && i < len(names) {
			return names[i]
		}
		return strconv.Itoa(int(f))
	}
	return e.String()
}

// maybeSample writes a sample row when the downsampling rule says so (the caller holds the lock).
func (r *Recorder) maybeSample(key string, d *device, ms int64, live bool) {
	last, had := r.last[key]
	write := !had
	if had {
		if d.cur.Voltage != nil && (last.Voltage == nil || abs(*d.cur.Voltage-*last.Voltage) >= Step-1e-9) {
			write = true
		}
		if (d.cur.LowBat == nil) != (last.LowBat == nil) || d.cur.LowBat != nil && last.LowBat != nil && *d.cur.LowBat != *last.LowBat {
			write = true
		}
		if ms-last.TS >= MaxGap.Milliseconds() {
			write = true
		}
	}
	if !write {
		return
	}
	if had && ms <= last.TS {
		// two reports in the same millisecond (one status multicall): one row with the newest values
		ms = last.TS
	}
	src := "seed"
	if live {
		src = "live"
	}
	s := store.Sample{Interface: d.Interface, Address: d.Address, TS: ms, Voltage: copyF(d.cur.Voltage), LowBat: copyB(d.cur.LowBat), Source: src}
	r.last[key] = s
	r.o.Store.AddSample(s)
}

func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}

func copyF(f *float64) *float64 {
	if f == nil {
		return nil
	}
	v := *f
	return &v
}

func copyB(b *bool) *bool {
	if b == nil {
		return nil
	}
	v := *b
	return &v
}

func (r *Recorder) refreshNames(ctx context.Context) {
	n, err := r.o.Source.Names(ctx)
	if err != nil {
		r.logf("recorder: names: %v", err)
		return
	}
	r.mu.Lock()
	r.names = n
	r.mu.Unlock()
}

func (r *Recorder) followNames(ctx context.Context) {
	r.mu.Lock()
	since := int64(0)
	if r.names != nil {
		since = r.names.Revision
	}
	r.mu.Unlock()
	ch := r.o.Source.NameEvents(ctx, since)
	var timer *time.Timer
	for ev := range ch {
		_ = ev
		// one re-read per burst of changes
		if timer != nil {
			timer.Stop()
		}
		timer = time.AfterFunc(2*time.Second, func() { r.refreshNames(ctx) })
	}
}

// View is one device as the API and the UI show it.
type View struct {
	Interface     string   `json:"interface"`
	Address       string   `json:"address"`
	Name          string   `json:"name"`
	Rooms         []string `json:"rooms"`
	Model         string   `json:"model"`
	Firmware      string   `json:"firmware,omitempty"`
	Power         string   `json:"power"`
	VoltageDP     string   `json:"voltage_dp,omitempty"`
	LowBatDP      string   `json:"lowbat_dp,omitempty"`
	Voltage       *float64 `json:"voltage"`
	VoltageTS     int64    `json:"voltage_ts,omitempty"`
	VoltageStatus string   `json:"voltage_status,omitempty"`
	LowBat        *bool    `json:"low_bat"`
	LowBatSince   int64    `json:"low_bat_since,omitempty"`
	Unreach       *bool    `json:"unreach"`
	UnreachSince  int64    `json:"unreach_since,omitempty"`
	LastReport    int64    `json:"last_report,omitempty"`
	FirstSeen     int64    `json:"first_seen"`
}

// Devices answers every tracked device with its current values and names in lang.
func (r *Recorder) Devices(lang string) []View {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]View, 0, len(r.devs))
	for _, d := range r.devs {
		if !d.tracked {
			continue
		}
		v := View{Interface: d.Interface, Address: d.Address, Model: d.Model, Firmware: d.Firmware, Power: d.Power, VoltageDP: d.VoltageDP, LowBatDP: d.LowBatDP,
			Voltage: d.cur.Voltage, VoltageTS: d.cur.VoltageTS, VoltageStatus: d.cur.VoltageStatus, LowBat: d.cur.LowBat, LowBatSince: d.cur.LowBatSince,
			Unreach: d.cur.Unreach, UnreachSince: d.cur.UnreachSince, LastReport: d.cur.LastReport, FirstSeen: d.FirstSeen, Rooms: []string{}}
		if r.names != nil {
			v.Name = r.names.Name(d.Interface, d.Address)
			if rooms := r.names.RoomNames(d.Interface, d.Address, lang); rooms != nil {
				v.Rooms = rooms
			}
			if v.Name == "" {
				// a device named only by its first channel, as the ReGa import leaves some
				v.Name = r.names.Name(d.Interface, d.Address+":1")
			}
		}
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].Address < out[j].Address
	})
	return out
}
