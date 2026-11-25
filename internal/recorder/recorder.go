// Package recorder records the battery-relevant datapoints of every device: it learns the devices
// and which of them report a voltage or a low-battery flag, seeds their last values from the
// state store (and, for HmIP, from hmipserver's cache, which costs no air), follows the event
// stream, keeps the current values, and writes downsampled samples and the evidence events into
// the store. Every report is handed on to the Observer (the change keeper and its detector), and
// the Observer's battery profile gives each device its power source and slots.
package recorder

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/hobbyquaker/ccu-addon-battery/internal/batteries"
	"github.com/hobbyquaker/ccu-addon-battery/internal/detect"
	"github.com/hobbyquaker/ccu-addon-battery/internal/ifaces"
	"github.com/hobbyquaker/ccu-addon-battery/internal/rega"
	"github.com/hobbyquaker/ccu-addon-battery/internal/store"
	"github.com/hobbyquaker/ccu-addon-kit/go/litesource"
)

// Keys are the datapoints the recorder subscribes to.
var Keys = []string{"OPERATING_VOLTAGE", "OPERATING_VOLTAGE_STATUS", "LOW_BAT", "LOWBAT", "BATTERY_STATE", "BATTERY_VOLTAGE", "BOOTED", "UNREACH"}

// isVoltageKey says whether a key is one a device's voltage datapoint (VoltageDP) may name.
func isVoltageKey(k string) bool {
	return k == "OPERATING_VOLTAGE" || k == "BATTERY_STATE" || k == "BATTERY_VOLTAGE"
}

// Downsampling: a row when the voltage moved by Step or more since the last row, the low-battery
// flag changed, or MaxGap passed since the last row, whichever comes first.
const (
	Step   = store.SampleStep
	MaxGap = store.SampleMaxGap
)

// Observer takes what the recorder sees: the battery profile of a device (power, slots, levels)
// and every report as a detector observation.
type Observer interface {
	Profile(ctx context.Context, d store.Device, maxV float64) batteries.Profile
	Observe(ctx context.Context, d store.Device, o detect.Obs)
}

// Options configure the recorder.
type Options struct {
	Source litesource.Source
	Store  *store.Store
	// Observer gets the reports; nil records only.
	Observer Observer
	Log      func(string, ...any)
	// ReadPause is the pause between two HmIP :0 reads at the seed (200 ms, as the system's own
	// sweep); a test shortens it.
	ReadPause time.Duration
	// Now is the clock; time.Now by default.
	Now func() time.Time
	// LearnRetry and LearnRetryMax are the backoff of learning an interface whose process did
	// not answer (hmipserver still starting at boot): 5 s, doubling, then every 60 s; a test
	// shortens them.
	LearnRetry, LearnRetryMax time.Duration
	// ServiceOff answers the addresses of the devices whose service messages the user switched
	// off in the system - a classic CCU's per-device switch in ReGa. nil: the system has no
	// such switch (openccu-lite), and the device list offers no filter by it.
	ServiceOff func(context.Context) (map[string]bool, error)
	// ServiceOffEvery is how often ServiceOff is asked; 10 min by default.
	ServiceOffEvery time.Duration
}

// Recorder is the running recorder.
type Recorder struct {
	o     Options
	logf  func(string, ...any)
	mu    sync.Mutex
	devs  map[string]*device // by interface + "." + address
	names *litesource.Names
	chans map[string][]string // the channel addresses of every named device, by interface + "." + address
	// serviceOff are the addresses of the devices whose service messages are off (Options.ServiceOff)
	serviceOff map[string]bool
	state      string // starting | seeding | live | degraded
	last       map[string]store.Sample
	ctx        context.Context
	// unlearned are the interfaces whose devices could not be learned yet, by name; "" is the
	// interface list itself. Only the Run loop touches it.
	unlearned map[string]*unlearned
}

// unlearned is one interface the recorder has not learned yet: how often it tried, and when it
// last said so in the log (once, then once a minute).
type unlearned struct {
	since    time.Time
	attempts int
	logged   time.Time
}

type device struct {
	store.Device
	cur     store.Current
	hmip    bool
	tracked bool
	profile batteries.Profile
	// written is when cur was last queued for the store; dirty says cur has moved on since in
	// its timestamps alone (a report that repeated the values), which waits for CurrentRefresh
	written int64
	dirty   bool
	// flap is the voltage change waiting for FlapHold before it is recorded (nil: none)
	flap *flap
	// supplySince is when the reports began to say the other power source than the one in force
	// (SupplyHold; 0: they agree)
	supplySince int64
}

// CurrentRefresh is how long a device's current row may lag behind in its timestamps alone
// (voltage_ts, last_report) before it is written again. A report that changes a value, or that
// writes a sample row, writes the row at once; the timestamps are what most reports change, and
// they are back from the system at the next start anyway (the seed), so they need not cost a
// write on every report. The rows still lagging are written when the recorder stops.
const CurrentRefresh = 6 * time.Hour

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
	if o.LearnRetry == 0 {
		o.LearnRetry = 5 * time.Second
	}
	if o.LearnRetryMax == 0 {
		o.LearnRetryMax = time.Minute
	}
	if o.ServiceOffEvery == 0 {
		o.ServiceOffEvery = 10 * time.Minute
	}
	r := &Recorder{o: o, logf: o.Log, devs: map[string]*device{}, state: "starting", last: map[string]store.Sample{}, ctx: context.Background(),
		unlearned: map[string]*unlearned{}}
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
// An interface whose process does not answer yet (hmipserver starts after the addon at boot) is
// learned again with a backoff, at every resync and when the stream says the interface is up;
// once it is learned the state is seeded again, so the values that came in meanwhile (the state
// store keeps the last of each, hmipserver's cache the voltages) are attributed to its devices.
func (r *Recorder) Run(ctx context.Context) error {
	r.mu.Lock()
	r.ctx = ctx
	r.mu.Unlock()
	defer r.Persist()
	r.loadStore(ctx)
	r.learn(ctx, nil)
	r.refreshNames(ctx)
	go r.followNames(ctx)
	go r.followServiceOff(ctx)
	snap, err := r.seed(ctx)
	since := ""
	if err != nil {
		r.logf("recorder: seed failed: %v (recording from the stream alone)", err)
	} else {
		since = snap
	}
	r.setState("live")
	ch := r.o.Source.Subscribe(ctx, litesource.Filter{Interfaces: ifaces.Supported, Keys: Keys}, since)
	retry := time.NewTimer(time.Hour)
	retry.Stop()
	defer retry.Stop()
	armed := false
	arm := func() {
		if armed && !retry.Stop() {
			select {
			case <-retry.C:
			default:
			}
		}
		armed = false
		if d := r.retryDelay(); d > 0 {
			retry.Reset(d)
			armed = true
		}
	}
	arm()
	reseed := func(why string) {
		r.setState("seeding")
		if _, err := r.seed(ctx); err != nil {
			r.logf("recorder: re-seed (%s) failed: %v", why, err)
		}
		r.setState("live")
	}
	for {
		var m litesource.Message
		var ok bool
		select {
		case <-retry.C:
			armed = false
			if r.learn(ctx, r.unlearnedNames()) > 0 {
				reseed("devices learned")
			}
			arm()
			continue
		case m, ok = <-ch:
			if !ok {
				return ctx.Err()
			}
		}
		switch m.Type {
		case "hello":
			r.setState("live")
		case "event", "state":
			if m.Event != nil {
				r.apply(*m.Event, m.Event.Source == "event")
			}
		case "resync":
			r.logf("recorder: resync (%s): learning the devices and reading the state again", m.Resync.Reason)
			r.learn(ctx, nil)
			reseed("resync")
			arm()
		case "interface":
			// up, restarted or added: the process answers now - learn it if it was missing or
			// came back (its devices may have changed while it was away)
			if m.Interface != nil && ifaces.IsSupported(m.Interface.Interface) && (m.Interface.State == "up" || m.Interface.State == "restarted" || m.Interface.State == "added") {
				if r.learn(ctx, []string{m.Interface.Interface}) > 0 {
					reseed("interface " + m.Interface.State)
				}
				arm()
			}
		case "newDevices", "readdedDevice", "replaceDevice", "updateDevice":
			r.learn(ctx, nil)
			arm()
		case "deleteDevices":
			if m.Devices != nil {
				r.retire(m.Devices.Interface, m.Devices.Addresses)
			}
		}
	}
}

// learn learns the devices of the interfaces named (nil: all) and keeps account of the ones that
// failed; it answers how many devices became tracked that were not before.
func (r *Recorder) learn(ctx context.Context, only []string) int {
	added, failed, err := r.learnDevices(ctx, only)
	now := r.o.Now()
	if err != nil {
		failed = map[string]error{"": err}
	} else {
		r.learned("")
		asked := only
		if asked == nil {
			asked = ifaces.Supported
		}
		for _, name := range asked {
			if _, bad := failed[name]; !bad {
				r.learned(name)
			}
		}
	}
	for name, e := range failed {
		u := r.unlearned[name]
		if u == nil {
			u = &unlearned{since: now}
			r.unlearned[name] = u
		}
		u.attempts++
		if u.logged.IsZero() || now.Sub(u.logged) >= time.Minute {
			u.logged = now
			what := name + ": listDevices"
			if name == "" {
				what = "the interface list"
			}
			r.logf("recorder: %s: %v (attempt %d, trying again in %s)", what, e, u.attempts, r.backoff(u.attempts))
		}
	}
	return added
}

// learned clears an interface from the unlearned ones and says so when it had failed before.
func (r *Recorder) learned(name string) {
	u := r.unlearned[name]
	if u == nil {
		return
	}
	delete(r.unlearned, name)
	if name != "" {
		r.logf("recorder: %s: devices learned (after %d failed attempts since %s)", name, u.attempts, u.since.Format("15:04:05"))
	}
}

func (r *Recorder) unlearnedNames() []string {
	if _, all := r.unlearned[""]; all {
		return nil
	}
	out := make([]string, 0, len(r.unlearned))
	for n := range r.unlearned {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// backoff is LearnRetry doubled per failed attempt, at most LearnRetryMax.
func (r *Recorder) backoff(attempts int) time.Duration {
	d := r.o.LearnRetry
	for i := 1; i < attempts && d < r.o.LearnRetryMax; i++ {
		d *= 2
	}
	if d > r.o.LearnRetryMax {
		d = r.o.LearnRetryMax
	}
	return d
}

// retryDelay is when to try the unlearned interfaces again (the one tried least often decides);
// 0 when every interface is learned.
func (r *Recorder) retryDelay() time.Duration {
	var d time.Duration
	for _, u := range r.unlearned {
		if b := r.backoff(u.attempts); d == 0 || b < d {
			d = b
		}
	}
	return d
}

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
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
	now := r.o.Now().UnixMilli()
	for _, d := range devs {
		if !ifaces.IsSupported(d.Interface) {
			// recorded by an older version (VirtualDevices, BidCos-Wired): retired, kept
			if d.RetiredAt == 0 {
				r.o.Store.RetireDevice(d.Interface, d.Address, now)
			}
			continue
		}
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
	for _, d := range r.devs {
		r.profileLocked(ctx, d, false)
	}
	for k, d := range r.devs {
		// the flap filter compares with the last recorded row; the current row may hold a
		// flip the history left out
		if s, err := r.o.Store.LastSample(ctx, d.Interface, d.Address); err == nil && s != nil {
			r.last[k] = store.Sample{TS: s.TS, Voltage: s.Voltage, LowBat: s.LowBat}
			continue
		}
		if d.cur.VoltageTS != 0 {
			r.last[k] = store.Sample{TS: d.cur.VoltageTS, Voltage: d.cur.Voltage, LowBat: d.cur.LowBat}
		}
	}
}

// learnDevices lists the devices of every interface (only: of those named; nil is all) and finds,
// from the VALUES descriptions, which datapoints each device reports; the result is upserted
// into the store. It answers how many devices became tracked that were not, and the interfaces
// whose process did not answer (their devices are left as they were); err is the interface list
// itself failing.
func (r *Recorder) learnDevices(ctx context.Context, only []string) (added int, failed map[string]error, err error) {
	list, err := r.o.Source.Interfaces(ctx)
	if err != nil {
		return 0, nil, err
	}
	failed = map[string]error{}
	now := r.o.Now().UnixMilli()
	for _, iface := range list {
		if !ifaces.IsSupported(iface.Name) {
			continue // never listed: no devices with a battery there
		}
		if only != nil && !contains(only, iface.Name) {
			continue
		}
		descs, err := r.o.Source.Devices(ctx, iface.Name)
		if err != nil {
			failed[iface.Name] = err
			continue
		}
		byAddr := map[string]litesource.DeviceDescription{}
		kids := map[string][]string{} // device address -> its channels' addresses
		for _, d := range descs {
			byAddr[d.Address] = d
			if dev, _, ok := strings.Cut(d.Address, ":"); ok {
				kids[dev] = append(kids[dev], d.Address)
			}
		}
		// every channel's VALUES description - a self-built device keeps its voltage on any
		// channel -, but asked once per kind of channel: a channel's description depends on the
		// device's type, firmware and version and the channel's type, not on its address
		var want []string
		rep := map[string]string{} // address -> the address asked for its kind
		kind := map[string]string{}
		for _, d := range descs {
			if !strings.Contains(d.Address, ":") {
				continue
			}
			p := byAddr[d.Parent]
			k := p.Type + "\x00" + p.Firmware + "\x00" + strconv.Itoa(p.Version) + "\x00" + d.Type
			if p.Address == "" {
				k = "\x00" + d.Address // an orphaned channel: on its own
			}
			if a, ok := kind[k]; ok {
				rep[d.Address] = a
				continue
			}
			kind[k] = d.Address
			rep[d.Address] = d.Address
			want = append(want, d.Address)
		}
		keys := map[string]string{}
		sets := litesource.ParamsetCache{}
		var descErr error
		for i := 0; i < len(want); i += 500 {
			end := i + 500
			if end > len(want) {
				end = len(want)
			}
			k, s, err := r.o.Source.Descriptions(ctx, iface.Name, want[i:end])
			if err != nil {
				descErr = err
				break
			}
			for a, key := range k {
				keys[a] = key
			}
			for key, set := range s {
				sets[key] = set
			}
		}
		if descErr != nil {
			// without the descriptions every device would look untracked: leave them as they are
			failed[iface.Name] = fmt.Errorf("descriptions: %w", descErr)
			continue
		}
		for a, r := range rep {
			if key, ok := keys[r]; ok {
				keys[a] = key
			}
		}
		for _, d := range descs {
			if strings.Contains(d.Address, ":") || d.Address == "" {
				continue
			}
			dev := store.Device{Interface: iface.Name, Address: d.Address, Model: d.Type, Firmware: d.Firmware, Power: "unknown", FirstSeen: now, LastSeen: now}
			var chans []channel
			for _, ch := range kids[d.Address] {
				if set := sets[keys[ch]]; set != nil {
					_, idx, _ := strings.Cut(ch, ":")
					if n, err := strconv.Atoi(idx); err == nil {
						chans = append(chans, channel{Index: n, Type: byAddr[ch].Type, Set: set})
					}
				}
			}
			dev.VoltageDP, dev.LowBatDP = datapoints(chans)
			r.mu.Lock()
			k := iface.Name + "." + d.Address
			ex := r.devs[k]
			if ex == nil {
				ex = &device{cur: store.Current{Interface: iface.Name, Address: d.Address}}
				r.devs[k] = ex
			} else {
				dev.FirstSeen, dev.Ignored, dev.OnSupply = ex.FirstSeen, ex.Ignored, ex.OnSupply
				if ex.Power != "unknown" {
					dev.Power = ex.Power
				}
			}
			was := ex.tracked
			ex.Device = dev
			ex.hmip = strings.HasPrefix(iface.Name, "HmIP")
			ex.tracked = dev.VoltageDP != "" || dev.LowBatDP != ""
			if ex.tracked && !was {
				added++
			}
			if ex.tracked {
				r.profileLocked(ctx, ex, true)
				r.o.Store.UpsertDevice(ex.Device)
			}
			r.mu.Unlock()
		}
	}
	return added, failed, nil
}

// Persist queues the current rows that lag behind in their timestamps (CurrentRefresh), so the
// store's last flush takes them; Run calls it on its way out.
func (r *Recorder) Persist() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, d := range r.devs {
		if d.dirty {
			r.o.Store.PutCurrent(d.cur)
			d.written, d.dirty = d.cur.LastReport, false
		}
	}
}

// profileLocked asks the Observer for the device's profile and takes power and slots from it;
// an external supply the device reports wins over the table's power. slots writes the slots.
func (r *Recorder) profileLocked(ctx context.Context, d *device, slots bool) {
	if r.o.Observer == nil {
		return
	}
	maxV := 0.0
	if d.cur.Voltage != nil {
		maxV = *d.cur.Voltage
	}
	d.profile = r.o.Observer.Profile(ctx, d.Device, maxV)
	d.Power = d.profile.Power
	if d.cur.VoltageStatus == "EXTERNAL" {
		d.Power = "external"
	}
	if slots {
		var sl []store.Slot
		for _, s := range d.profile.Slots {
			sl = append(sl, store.Slot{Count: s.Count, Type: s.Type, Chemistry: s.Chemistry, Rechargeable: s.Chemistry == "nimh"})
		}
		r.o.Store.PutSlots(d.Interface, d.Address, sl)
	}
}

// ErrNoDevice is answered by SetIgnored for a device the recorder does not track.
var ErrNoDevice = errors.New("no such device")

// SetIgnored switches a device's monitoring off ("do not monitor") or back on. An ignored device
// keeps being recorded - its samples and changes stay - but leaves the device list's default
// view, the forecast, the shopping list and the notifications, and the detector stops watching
// it (its open detections are discarded).
func (r *Recorder) SetIgnored(ctx context.Context, iface, address string, on bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	d := r.devs[iface+"."+address]
	if d == nil || !d.tracked {
		return ErrNoDevice
	}
	if err := r.o.Store.SetIgnored(ctx, iface, address, on, r.o.Now().UnixMilli()); err != nil {
		return err
	}
	d.Ignored = on
	r.profileLocked(ctx, d, false)
	return nil
}

// Reprofile recomputes every device's profile (after an override changed) and writes power and
// slots.
func (r *Recorder) Reprofile(ctx context.Context) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, d := range r.devs {
		if d.tracked {
			r.profileLocked(ctx, d, true)
			r.o.Store.UpsertDevice(d.Device)
		}
	}
}

func isThermostatChannel(t string) bool {
	return t == "CLIMATECONTROL_RT_TRANSCEIVER" || t == "THERMALCONTROL_TRANSMIT"
}

// channel is one channel of a device as the detection of its datapoints sees it: its index, its
// type and its VALUES description.
type channel struct {
	Index int
	Type  string
	Set   litesource.Paramset
}

// datapoints finds a device's voltage and low-battery datapoints ("<channel>/<key>", or "")
// from its channels' VALUES descriptions:
//   - the voltage: OPERATING_VOLTAGE on channel 0 (every Homematic IP device), else
//     BATTERY_STATE on a thermostat channel (HM-CC-RT-DN, HM-TC-IT-WM-W-EU), else the first
//     channel with OPERATING_VOLTAGE, else the first with BATTERY_VOLTAGE - those two only as a
//     FLOAT in volts, which is how self-built (HB-) devices report their battery on another
//     channel; a voltmeter's or power meter's reading (HB_VOLTAGE, VOLTAGE, ACTUAL_VOLTAGE,
//     HB_BAT_VOLTAGE) is never taken;
//   - the low-battery flag: LOW_BAT or LOWBAT on channel 0 only - a mains device may carry
//     LOWBAT on a contact channel (HB-UNI-SenAct-4-4-SC), which says nothing of a battery.
func datapoints(chans []channel) (voltage, lowbat string) {
	sort.SliceStable(chans, func(i, j int) bool { return chans[i].Index < chans[j].Index })
	has := func(c channel, k string) bool { _, ok := c.Set[k]; return ok }
	volts := func(c channel, k string) bool {
		p, ok := c.Set[k]
		return ok && p.Type == "FLOAT" && strings.TrimSpace(p.Unit) == "V"
	}
	for _, c := range chans {
		if c.Index != 0 {
			continue
		}
		if has(c, "OPERATING_VOLTAGE") {
			voltage = "0/OPERATING_VOLTAGE"
		}
		if has(c, "LOW_BAT") {
			lowbat = "0/LOW_BAT"
		} else if has(c, "LOWBAT") {
			lowbat = "0/LOWBAT"
		}
	}
	pick := func(match func(channel) bool, key string) {
		for _, c := range chans {
			if voltage == "" && match(c) {
				voltage = strconv.Itoa(c.Index) + "/" + key
			}
		}
	}
	pick(func(c channel) bool { return isThermostatChannel(c.Type) && has(c, "BATTERY_STATE") }, "BATTERY_STATE")
	pick(func(c channel) bool { return volts(c, "OPERATING_VOLTAGE") }, "OPERATING_VOLTAGE")
	pick(func(c channel) bool { return volts(c, "BATTERY_VOLTAGE") }, "BATTERY_VOLTAGE")
	return voltage, lowbat
}

func (r *Recorder) retire(iface string, addresses []string) {
	if !ifaces.IsSupported(iface) {
		return
	}
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
// every tracked HmIP device's :0 VALUES from hmipserver's cache (free on air), then the :0 VALUES
// of every tracked BidCos device whose low-battery flag is still unknown (cacheBidCos). It
// answers the stream position to subscribe from.
func (r *Recorder) seed(ctx context.Context) (string, error) {
	r.setState("seeding")
	snap, err := r.o.Source.Seed(ctx, litesource.Filter{Interfaces: ifaces.Supported, Keys: Keys})
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
	if err := r.cacheBidCos(ctx); err != nil {
		return snap.EventID, err
	}
	return snap.EventID, nil
}

// SourceCache marks a value read from an interface process's cache: it sets the current value
// but is no report of the device, so "Last report" stays where it was.
const SourceCache = "cache"

// cacheBidCos reads LOWBAT and UNREACH of every tracked BidCos device whose low-battery flag is
// still unknown from rfd's cache (getParamset <address>:0 VALUES). rfd sends LOWBAT and UNREACH
// as events only when they change, and a classic CCU has no state store: without this read a
// battery device that never changed its flag since the addon came stays "unknown" for good. rfd
// answers the MAINTENANCE channel from its cache - nothing goes on air, a sleeping device is never
// asked. A device the state store already seeded is not read again.
func (r *Recorder) cacheBidCos(ctx context.Context) error {
	r.mu.Lock()
	var todo []*device
	for _, d := range r.devs {
		if d.tracked && d.Interface == "BidCos-RF" && d.LowBatDP != "" && d.cur.LowBat == nil {
			todo = append(todo, d)
		}
	}
	r.mu.Unlock()
	sort.Slice(todo, func(i, j int) bool { return todo[i].Address < todo[j].Address })
	ts := r.o.Now()
	for i, d := range todo {
		if i > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(r.o.ReadPause):
			}
		}
		vals, err := r.o.Source.Read(ctx, d.Interface, d.Address+":0", "VALUES")
		if err != nil {
			r.logf("recorder: %s %s:0 VALUES: %v", d.Interface, d.Address, err)
			continue
		}
		for _, k := range []string{"LOWBAT", "UNREACH"} {
			if v, ok := vals[k]; ok {
				r.apply(litesource.Event{Interface: d.Interface, Address: d.Address + ":0", Key: k, Value: json.RawMessage(v), TS: ts, Confirmed: true, Source: SourceCache}, false)
			}
		}
	}
	return nil
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
	if !ifaces.IsSupported(e.Interface) {
		return
	}
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
	var sample, supply bool
	obs := detect.Obs{TS: ms, Seed: !live}
	switch e.Key {
	case "OPERATING_VOLTAGE", "BATTERY_STATE", "BATTERY_VOLTAGE":
		if want := strconv.Itoa(e.Channel()) + "/" + e.Key; want != d.VoltageDP {
			break
		}
		v, ok := e.Float()
		if !ok {
			break
		}
		if cur.Voltage == nil {
			changed = true
		}
		// a voltage that moved is written with its sample row; one the flap filter holds back
		// waits like the timestamps (CurrentRefresh)
		vv := v
		cur.Voltage, cur.VoltageTS = &vv, ms
		sample, supply = true, e.Key == "OPERATING_VOLTAGE"
		ov := v
		obs.Voltage = &ov
	case "OPERATING_VOLTAGE_STATUS":
		supply = true
		s := statusName(e)
		if s != cur.VoltageStatus {
			cur.VoltageStatus = s
			changed = true
			if s == "EXTERNAL" && d.Power != "external" {
				d.Power = "external"
				r.o.Store.UpsertDevice(d.Device)
			} else if s != "EXTERNAL" && d.Power == "external" && d.profile.Power != "" && d.profile.Power != "external" {
				d.Power = d.profile.Power
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
		ob := b
		obs.LowBat = &ob
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
		ob := b
		obs.Unreach = &ob
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
		ob := b
		obs.Booted = &ob
	default:
		r.mu.Unlock()
		return
	}
	if ms > cur.LastReport && e.Source != SourceCache {
		cur.LastReport = ms
	}
	if supply {
		r.supplyLocked(d, ms)
	}
	wrote := false
	if sample && cur.Voltage != nil || sample && !isVoltageKey(e.Key) && d.VoltageDP == "" {
		wrote = r.maybeSample(key, d, ms, live)
	}
	switch {
	case changed || wrote || d.dirty && ms-d.written >= CurrentRefresh.Milliseconds():
		r.o.Store.PutCurrent(*cur)
		d.written, d.dirty = ms, false
	default:
		// the same values again: only the timestamps moved
		d.dirty = true
	}
	dev := d.Device
	ctx := r.ctx
	r.mu.Unlock()
	if r.o.Observer != nil && (obs.Voltage != nil || obs.LowBat != nil || obs.Unreach != nil || obs.Booted != nil) {
		r.o.Observer.Observe(ctx, dev, obs)
	}
}

// Power supply detection. A device that runs on batteries or a power supply unit (the table's
// battery-or-dc, the HmIP-MP3P) has no datapoint that says which, but on its 5 V supply it reports
// an OPERATING_VOLTAGE no cells of it can give (batteries.SupplyMinV, 4.9 V), or the status
// OVERFLOW. Once its reports say so for SupplyHold - the first such report and one at least that
// long after it, none in between saying otherwise - it is on its supply (store.Device.OnSupply,
// kept over a restart): its profile is external, so no forecast, no shopping entry and no
// detection, as with the manual choice. Reports back in the cells' range for SupplyHold switch it
// back. A single spike does neither. An override that names the power (the device page's
// "Batteries" or "Power supply unit") wins: the detected state applies only while the choice is
// automatic.
const SupplyHold = time.Hour

// supplyLocked takes one voltage or voltage-status report of a battery-or-dc device at ms (the
// caller holds the lock).
func (r *Recorder) supplyLocked(d *device, ms int64) {
	// the state is followed while an override names the power too, so going back to the automatic
	// choice finds it current
	if d.profile.TablePower != "battery-or-dc" && d.profile.Power != "battery-or-dc" && !d.profile.Auto {
		d.supplySince = 0
		return
	}
	high := d.cur.VoltageStatus == "OVERFLOW" || d.cur.Voltage != nil && *d.cur.Voltage >= batteries.SupplyMinV-1e-9
	if high == (d.OnSupply > 0) {
		d.supplySince = 0
		return
	}
	if d.supplySince == 0 || ms < d.supplySince {
		d.supplySince = ms
		return
	}
	if ms-d.supplySince < SupplyHold.Milliseconds() {
		return
	}
	since := d.supplySince
	if err := r.o.Store.SetOnSupply(r.ctx, d.Interface, d.Address, high, since); err != nil {
		r.logf("recorder: %s %s: the power supply detection is not saved: %v", d.Interface, d.Address, err)
		return
	}
	d.supplySince = 0
	if high {
		d.OnSupply = since
		r.logf("recorder: %s %s (%s): on its power supply (voltage %s, status %s), detected automatically", d.Interface, d.Address, d.Model, voltageText(d.cur.Voltage), orDash(d.cur.VoltageStatus))
	} else {
		d.OnSupply = 0
		r.logf("recorder: %s %s (%s): back on its batteries (voltage %s), detected automatically", d.Interface, d.Address, d.Model, voltageText(d.cur.Voltage))
	}
	r.profileLocked(r.ctx, d, false)
	r.o.Store.UpsertDevice(d.Device)
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func voltageText(v *float64) string {
	if v == nil {
		return "-"
	}
	return strconv.FormatFloat(*v, 'f', 2, 64) + " V"
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

// The flap filter. A device reports its voltage in steps of 0.1 V, and a voltage that lies near
// a step flips between the two values with the load (a thermostat's motor) and the temperature
// (day and night) for days, as the pack drains past it. A change of at most FlapBand from the last
// recorded value is therefore only recorded once it held: the reports stayed on the same side of
// the recorded value for FlapHold, half a day, over two reports at least; it is then recorded at
// the time it first came, so the history shows it where it happened. Half a day leaves out the
// motor's dips and the night's, and still takes a falling pack's next step within a day of the
// time it is reached for good (TestFlapFilterSlowTrend). A report back at the recorded value
// ends the wait and records nothing. A change of more than FlapBand (a battery change, a pack
// giving out) is recorded at once, and so is every change of the low-battery flag. Only the
// history (the samples) is filtered: the current values, the detector and the warnings see every
// report as it comes.
const (
	FlapBand = 0.1
	FlapHold = 12 * time.Hour
)

// flap is a device's voltage waiting for its FlapHold: the first value that left the recorded
// one, since when, whether that report was live, and how many reports kept to its side.
type flap struct {
	v     float64
	since int64
	live  bool
	n     int
}

// maybeSample writes a sample row when the downsampling rule and the flap filter say so (the
// caller holds the lock); true when it did.
func (r *Recorder) maybeSample(key string, d *device, ms int64, live bool) bool {
	last, had := r.last[key]
	if !had || lowBatDiffers(d.cur.LowBat, last.LowBat) || d.cur.Voltage != nil && last.Voltage == nil {
		d.flap = nil
		return r.writeSample(key, d, ms, d.cur.Voltage, live)
	}
	if v := d.cur.Voltage; v != nil {
		diff := *v - *last.Voltage
		switch {
		case abs(diff) < Step-1e-9:
			// still, or back, at the recorded value: a flip that came back leaves nothing
			d.flap = nil
		case abs(diff) > FlapBand+1e-9:
			d.flap = nil
			return r.writeSample(key, d, ms, v, live)
		case d.flap == nil || (d.flap.v < *last.Voltage) != (diff < 0):
			d.flap = &flap{v: *v, since: ms, live: live, n: 1}
		default:
			f := d.flap
			f.n++
			if f.n >= 2 && ms-f.since >= FlapHold.Milliseconds() {
				ts := f.since
				if ts < last.TS {
					// a daily row was written meanwhile: the history must stay in order (a daily
					// row at the very report that started the wait is replaced by this one)
					ts = ms
				}
				held := f.v
				d.flap = nil
				r.writeSample(key, d, ts, &held, f.live)
				if abs(*v-held) >= Step-1e-9 {
					// the value moved on while it was held: it waits on its own
					d.flap = &flap{v: *v, since: ms, live: live, n: 1}
				}
				return true
			}
		}
	}
	if ms-last.TS >= MaxGap.Milliseconds() {
		v := d.cur.Voltage
		if d.flap != nil {
			v = last.Voltage // the daily row keeps the recorded value while a change waits
		}
		return r.writeSample(key, d, ms, v, live)
	}
	return false
}

// writeSample queues one sample row of the device with the voltage v and its current low-battery
// flag, and makes it the device's last row (the caller holds the lock).
func (r *Recorder) writeSample(key string, d *device, ms int64, v *float64, live bool) bool {
	if last, had := r.last[key]; had && ms <= last.TS {
		// two reports in the same millisecond (one status multicall): one row with the newest values
		ms = last.TS
	}
	src := "seed"
	if live {
		src = "live"
	}
	s := store.Sample{Interface: d.Interface, Address: d.Address, TS: ms, Voltage: copyF(v), LowBat: copyB(d.cur.LowBat), Source: src}
	r.last[key] = s
	r.o.Store.AddSample(s)
	return true
}

func lowBatDiffers(a, b *bool) bool {
	return (a == nil) != (b == nil) || a != nil && b != nil && *a != *b
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
	chans := channelIndex(n)
	r.mu.Lock()
	r.names = n
	r.chans = chans
	r.mu.Unlock()
}

// followServiceOff keeps the set of devices whose service messages are off: read at the start
// and every ServiceOffEvery - a switch in the WebUI changes no name, so nothing announces it. A
// failed read keeps the last answer.
func (r *Recorder) followServiceOff(ctx context.Context) {
	if r.o.ServiceOff == nil {
		return
	}
	failed := false
	for {
		off, err := r.o.ServiceOff(ctx)
		if err != nil {
			if !failed && ctx.Err() == nil {
				r.logf("recorder: the devices' service message switches: %v", err)
			}
			failed = true
		} else {
			failed = false
			r.mu.Lock()
			r.serviceOff = off
			r.mu.Unlock()
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(r.o.ServiceOffEvery):
		}
	}
}

// Filters says which filters of the device list the system can serve: service_messages only
// where it has a per-device switch for them.
type Filters struct {
	ServiceMessages bool `json:"service_messages"`
}

// Filters answers the filters the system can serve.
func (r *Recorder) Filters() Filters {
	return Filters{ServiceMessages: r.o.ServiceOff != nil}
}

// channelIndex lists the channel addresses of every device the names know, in channel order.
func channelIndex(n *litesource.Names) map[string][]string {
	if n == nil {
		return nil
	}
	out := map[string][]string{}
	for key := range n.Objects {
		dot := strings.IndexByte(key, '.')
		colon := strings.LastIndexByte(key, ':')
		if dot < 0 || colon < dot {
			continue
		}
		dev := key[:colon]
		out[dev] = append(out[dev], key[dot+1:])
	}
	for _, list := range out {
		sort.Slice(list, func(i, j int) bool { return channelLess(list[i], list[j]) })
	}
	return out
}

// channelLess orders channel addresses by their number, then as text.
func channelLess(a, b string) bool {
	na, ea := strconv.Atoi(a[strings.LastIndexByte(a, ':')+1:])
	nb, eb := strconv.Atoi(b[strings.LastIndexByte(b, ':')+1:])
	if ea == nil && eb == nil && na != nb {
		return na < nb
	}
	return a < b
}

// deviceRooms answers the rooms of a device in lang: its own and those of its channels, each
// once, in channel order. openccu-lite's metadata puts rooms on the channels (the device object
// usually has none); a classic CCU's ReGa does the same, and ccusource adds the union to the
// device too - the duplicates fall out here. The CCU's own default rooms come out in lang, as
// the WebUI shows them, not as ReGa's key.
func deviceRooms(n *litesource.Names, chans map[string][]string, iface, address, lang string) []string {
	out := []string{}
	if n == nil {
		return out
	}
	seen := map[string]bool{}
	add := func(rooms []string) {
		for _, room := range rooms {
			// a room the CCU shipped with is a key in ReGa ("roomKitchen"), which the WebUI
			// translates; a name the user typed passes as it is
			room = rega.DefaultName(room, lang)
			if room == "" || seen[room] {
				continue
			}
			seen[room] = true
			out = append(out, room)
		}
	}
	add(n.RoomNames(iface, address, lang))
	for _, ch := range chans[iface+"."+address] {
		add(n.RoomNames(iface, ch, lang))
	}
	return out
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
	// ServiceMessagesOff: the user switched the device's service messages off in the system
	// (a classic CCU; never set on openccu-lite, which has no such switch).
	ServiceMessagesOff bool `json:"service_messages_off,omitempty"`
	// Ignored: the user switched the device's monitoring off (SetIgnored).
	Ignored bool `json:"ignored,omitempty"`
	// Profile is the battery profile: slots, levels, where it came from.
	Profile batteries.Profile `json:"profile"`
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
			Unreach: d.cur.Unreach, UnreachSince: d.cur.UnreachSince, LastReport: d.cur.LastReport, FirstSeen: d.FirstSeen, Rooms: []string{}, Profile: d.profile,
			Ignored: d.Ignored}
		if v.Profile.Slots == nil {
			v.Profile.Slots = []batteries.Slot{}
		}
		v.ServiceMessagesOff = r.serviceOff[d.Address]
		if r.names != nil {
			v.Name = r.names.Name(d.Interface, d.Address)
			v.Rooms = deviceRooms(r.names, r.chans, d.Interface, d.Address, lang)
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
