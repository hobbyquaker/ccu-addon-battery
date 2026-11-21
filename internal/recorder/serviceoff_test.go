package recorder

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hobbyquaker/ccu-addon-kit/go/addonkit"
	"github.com/hobbyquaker/ccu-addon-kit/go/litesource"

	"github.com/hobbyquaker/ccu-addon-battery/internal/store"
)

// A system with a per-device switch for service messages (a classic CCU): the devices it is off
// for carry service_messages_off, the set follows the system (a switch changes no name, so it
// is polled), and a failed read keeps the last answer. Without such a switch (openccu-lite) no
// device carries it and Filters says the filter cannot be served.
func TestServiceOff(t *testing.T) {
	s := system(t)
	st, err := store.Open(store.Config{AddonDir: t.TempDir(), Log: t.Logf})
	if err != nil {
		t.Fatal(err)
	}
	c := litesource.New(s.URL, addonkit.StaticToken("olt_test"))
	c.Reconnect.Min, c.Reconnect.Max = 10*time.Millisecond, 50*time.Millisecond
	c.Log = t.Logf
	var mu sync.Mutex
	off, fail := map[string]bool{"SC": true}, false
	r := New(Options{Source: c, Store: st, Log: t.Logf, ReadPause: time.Millisecond, ServiceOffEvery: 10 * time.Millisecond,
		ServiceOff: func(context.Context) (map[string]bool, error) {
			mu.Lock()
			defer mu.Unlock()
			if fail {
				return nil, errors.New("rega.exe does not answer")
			}
			out := map[string]bool{}
			for k, v := range off {
				out[k] = v
			}
			return out, nil
		}})
	ctx, cancel := context.WithCancel(context.Background())
	go r.Run(ctx)
	t.Cleanup(func() { cancel(); st.Close() })
	if !r.Filters().ServiceMessages {
		t.Fatal("the filter is offered where the system has the switch")
	}
	offNow := func() string {
		var l []string
		for _, v := range r.Devices("de") {
			if v.ServiceMessagesOff {
				l = append(l, v.Address)
			}
		}
		return strings.Join(l, ",")
	}
	wait := func(want string) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for offNow() != want {
			if time.Now().After(deadline) {
				t.Fatalf("service messages off for %q, want %q", offNow(), want)
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
	wait("SC")
	b, _ := json.Marshal(find(r.Devices("de"), "SC"))
	if !strings.Contains(string(b), `"service_messages_off":true`) {
		t.Fatalf("json: %s", b)
	}
	if b, _ := json.Marshal(find(r.Devices("de"), "WRC2")); strings.Contains(string(b), "service_messages_off") {
		t.Fatalf("a device with its messages on carries the key: %s", b)
	}
	// switched off for another one, on again for the first
	mu.Lock()
	off = map[string]bool{"WRC2": true}
	mu.Unlock()
	wait("WRC2")
	// ReGa does not answer: the last answer stays
	mu.Lock()
	fail = true
	mu.Unlock()
	time.Sleep(50 * time.Millisecond)
	if got := offNow(); got != "WRC2" {
		t.Fatalf("after a failed read: %q", got)
	}
}

func TestServiceOffNotOnLite(t *testing.T) {
	r, _, _ := start(t, system(t))
	if r.Filters().ServiceMessages {
		t.Fatal("openccu-lite has no per-device switch")
	}
	for _, v := range r.Devices("de") {
		if v.ServiceMessagesOff {
			t.Fatalf("%s carries service_messages_off", v.Address)
		}
	}
}
