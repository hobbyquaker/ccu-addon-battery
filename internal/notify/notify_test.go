package notify

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/hobbyquaker/ccu-addon-battery/internal/settings"
)

type fakeMeta struct {
	mu sync.Mutex
	m  map[string]string
}

func (f *fakeMeta) MetaGet(_ context.Context, k string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.m[k], nil
}

func (f *fakeMeta) MetaSet(_ context.Context, k, v string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.m == nil {
		f.m = map[string]string{}
	}
	f.m[k] = v
	return nil
}

type hit struct {
	path, body string
	header     http.Header
}

func sink(t *testing.T) (*httptest.Server, func() []hit) {
	t.Helper()
	var mu sync.Mutex
	var hits []hit
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		hits = append(hits, hit{r.URL.Path, string(b), r.Header.Clone()})
		mu.Unlock()
		if r.URL.Path == "/fail" || r.URL.Path == "/fail/topic" {
			w.WriteHeader(500)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, func() []hit {
		mu.Lock()
		defer mu.Unlock()
		return append([]hit(nil), hits...)
	}
}

func TestWebhookDispatchAndRepeat(t *testing.T) {
	srv, hits := sink(t)
	s := settings.Notify{Channel: "webhook", URL: srv.URL + "/hook", Headers: map[string]string{"X-Key": "k"}, Events: []string{"low", "due"}, Language: "en"}
	meta := &fakeMeta{}
	now := time.UnixMilli(1_000_000_000_000)
	n := New(func() settings.Notify { return s }, meta)
	n.Now = func() time.Time { return now }
	ev := []Event{
		{Kind: "low", Key: "HmIP-RF.A", Title: "Battery low: A", Text: "A (X) reports a low battery.", Device: "A"},
		{Kind: "due", Key: "HmIP-RF.B|123", Title: "Due: B", Text: "..."},
		{Kind: "inbox", Key: "9", Title: "inbox", Text: "..."},
	}
	ctx := context.Background()
	if got := n.Dispatch(ctx, ev); got != 2 {
		t.Fatalf("sent %d, want 2 (inbox is not wanted)", got)
	}
	h := hits()
	if len(h) != 2 || h[0].path != "/hook" || h[0].header.Get("X-Key") != "k" || h[0].header.Get("Content-Type") != "application/json" {
		t.Fatalf("hits: %+v", h)
	}
	var body Event
	if json.Unmarshal([]byte(h[0].body), &body) != nil || body.Kind != "low" || body.Device != "A" || body.At != now.UnixMilli() {
		t.Fatalf("body: %s", h[0].body)
	}
	// the same notices again within Repeat: nothing
	if got := n.Dispatch(ctx, ev); got != 0 {
		t.Fatalf("repeat: %d", got)
	}
	// a restart reads the sent list from the meta table
	n2 := New(func() settings.Notify { return s }, meta)
	n2.Now = func() time.Time { return now.Add(time.Hour) }
	if got := n2.Dispatch(ctx, ev); got != 0 {
		t.Fatalf("after a restart: %d", got)
	}
	if len(n2.Sent(ctx)) != 2 {
		t.Fatalf("sent list: %v", n2.Sent(ctx))
	}
	// after Repeat the notice goes out again; a notice that is gone is forgotten
	n2.Now = func() time.Time { return now.Add(Repeat + time.Minute) }
	if got := n2.Dispatch(ctx, ev[:1]); got != 1 {
		t.Fatalf("after Repeat: %d", got)
	}
	if l := n2.Sent(ctx); len(l) != 1 {
		t.Fatalf("the due notice is forgotten: %v", l)
	}
	// a failing endpoint is not remembered as sent
	s.URL = srv.URL + "/fail"
	n3 := New(func() settings.Notify { return s }, &fakeMeta{})
	if got := n3.Dispatch(ctx, ev[1:2]); got != 0 || len(n3.Sent(ctx)) != 0 {
		t.Fatalf("failure: %d %v", got, n3.Sent(ctx))
	}
	// no channel: nothing, no error
	s.Channel = ""
	if got := n3.Dispatch(ctx, ev); got != 0 {
		t.Fatal("no channel")
	}
	if err := n3.Send(ctx, ev[0]); err == nil {
		t.Fatal("Send without a channel must fail")
	}
}

func TestNtfy(t *testing.T) {
	srv, hits := sink(t)
	s := settings.Notify{Channel: "ntfy", URL: srv.URL, Topic: "home", Token: "tk_x", Events: []string{"low"}, Language: "de"}
	n := New(func() settings.Notify { return s }, nil)
	e := Event{Kind: "low", Key: "A", Title: Text("de", "low.title", map[string]string{"name": "Küche"}), Text: Text("de", "low.text", map[string]string{"name": "Küche", "model": "HmIP-WRC2"}), Link: "/addons/battery/"}
	if err := n.Send(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	h := hits()
	if len(h) != 1 || h[0].path != "/home" || h[0].body != "Küche (HmIP-WRC2) meldet eine leere Batterie." {
		t.Fatalf("%+v", h)
	}
	hd := h[0].header
	if hd.Get("Authorization") != "Bearer tk_x" || hd.Get("Priority") != "high" || hd.Get("Click") != "/addons/battery/" || hd.Get("Tags") != "battery" {
		t.Fatalf("headers: %v", hd)
	}
	if hd.Get("Title") != "=?UTF-8?B?QmF0dGVyaWUgbGVlcjogS8O8Y2hl?=" {
		t.Fatalf("title: %q", hd.Get("Title"))
	}
	if asciiHeader("Battery low: A") != "Battery low: A" {
		t.Fatal("ascii stays")
	}
	if Text("xx", "test.title", nil) != Texts["de"]["test.title"] || Text("en", "inbox.text", map[string]string{"count": "2"}) != "2 detected battery changes wait in the inbox for your confirmation." {
		t.Fatal("texts")
	}
}
