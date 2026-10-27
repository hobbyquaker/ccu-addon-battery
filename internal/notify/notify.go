// Package notify sends the addon's notices - a low battery, a change due or due soon, a detection
// waiting in the inbox - over a channel of the user's: a generic webhook (a POST with a JSON body)
// or ntfy (a POST to <url>/<topic>). The system itself has no channel an addon can post to yet;
// when it gets one, it becomes a third channel here. Each notice goes out once; the same notice
// again only after Repeat, so a device that stays low does not ring every quarter of an hour.
// What was sent is kept in the store's meta table, so a restart repeats nothing.
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/hobbyquaker/ccu-addon-battery/internal/settings"
)

// Repeat is how long a sent notice holds the same notice back.
const Repeat = 7 * 24 * time.Hour

// metaKey is where the sent notices are kept.
const metaKey = "notify.sent"

// Event is one notice.
type Event struct {
	// Kind is low, due, soon, inbox or test.
	Kind string `json:"kind"`
	// Key tells the same notice from a new one: the device plus, for due, its due date; for the
	// inbox the newest pending change's id.
	Key string `json:"key"`
	// Title and Text in the user's language.
	Title string `json:"title"`
	Text  string `json:"text"`
	// Link is a path into the addon's page.
	Link string `json:"link,omitempty"`
	// Device, Name and Model where the notice is about one.
	Device string `json:"device,omitempty"`
	Name   string `json:"name,omitempty"`
	Model  string `json:"model,omitempty"`
	At     int64  `json:"at"`
}

// Meta keeps the sent notices across restarts (the store's meta table).
type Meta interface {
	MetaGet(ctx context.Context, key string) (string, error)
	MetaSet(ctx context.Context, key, value string) error
}

// Notifier sends events over the configured channel.
type Notifier struct {
	Settings func() settings.Notify
	Meta     Meta
	Client   *http.Client
	Log      func(string, ...any)
	Now      func() time.Time

	mu     sync.Mutex
	sent   map[string]int64 // key -> when
	loaded bool
}

// New makes a notifier.
func New(s func() settings.Notify, meta Meta) *Notifier {
	return &Notifier{Settings: s, Meta: meta, Client: &http.Client{Timeout: 15 * time.Second}, Log: func(string, ...any) {}, Now: time.Now, sent: map[string]int64{}}
}

// Send posts one event over the channel now, whatever was sent before (the Test button).
func (n *Notifier) Send(ctx context.Context, e Event) error {
	return n.post(ctx, n.Settings(), e)
}

// Dispatch sends the events whose kind the user wants and that were not sent within Repeat,
// and remembers them. It answers how many went out.
func (n *Notifier) Dispatch(ctx context.Context, events []Event) int {
	s := n.Settings()
	if s.Channel == "" {
		return 0
	}
	wanted := map[string]bool{}
	for _, k := range s.Events {
		wanted[k] = true
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	n.loadLocked(ctx)
	now := n.Now()
	nowMs := now.UnixMilli()
	// forget what is older than Repeat and no longer current
	current := map[string]bool{}
	for _, e := range events {
		current[e.Kind+"|"+e.Key] = true
	}
	for k, t := range n.sent {
		if !current[k] && nowMs-t > Repeat.Milliseconds() {
			delete(n.sent, k)
		}
	}
	sent := 0
	for _, e := range events {
		if !wanted[e.Kind] {
			continue
		}
		k := e.Kind + "|" + e.Key
		if t, ok := n.sent[k]; ok && nowMs-t < Repeat.Milliseconds() {
			continue
		}
		if e.At == 0 {
			e.At = nowMs
		}
		if err := n.post(ctx, s, e); err != nil {
			n.Log("notify: %s %s: %v", e.Kind, e.Key, err)
			continue
		}
		n.sent[k] = nowMs
		sent++
	}
	if sent > 0 {
		n.saveLocked(ctx)
	}
	return sent
}

// Sent lists the notices sent and still held back, for the settings page.
func (n *Notifier) Sent(ctx context.Context) map[string]int64 {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.loadLocked(ctx)
	out := map[string]int64{}
	for k, v := range n.sent {
		out[k] = v
	}
	return out
}

func (n *Notifier) loadLocked(ctx context.Context) {
	if n.loaded || n.Meta == nil {
		n.loaded = true
		return
	}
	if v, err := n.Meta.MetaGet(ctx, metaKey); err == nil && v != "" {
		_ = json.Unmarshal([]byte(v), &n.sent)
	}
	if n.sent == nil {
		n.sent = map[string]int64{}
	}
	n.loaded = true
}

func (n *Notifier) saveLocked(ctx context.Context) {
	if n.Meta == nil {
		return
	}
	b, _ := json.Marshal(n.sent)
	if err := n.Meta.MetaSet(ctx, metaKey, string(b)); err != nil {
		n.Log("notify: keeping the sent list: %v", err)
	}
}

func (n *Notifier) post(ctx context.Context, s settings.Notify, e Event) error {
	var req *http.Request
	var err error
	switch s.Channel {
	case "webhook":
		body, _ := json.Marshal(e)
		req, err = http.NewRequestWithContext(ctx, http.MethodPost, s.URL, bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		for k, v := range s.Headers {
			req.Header.Set(k, v)
		}
	case "ntfy":
		req, err = http.NewRequestWithContext(ctx, http.MethodPost, s.URL+"/"+s.Topic, strings.NewReader(e.Text))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "text/plain; charset=utf-8")
		req.Header.Set("Title", asciiHeader(e.Title))
		req.Header.Set("Tags", "battery")
		if e.Kind == "low" || e.Kind == "due" {
			req.Header.Set("Priority", "high")
		}
		if e.Link != "" {
			req.Header.Set("Click", e.Link)
		}
		if s.Token != "" {
			req.Header.Set("Authorization", "Bearer "+s.Token)
		}
	case "":
		return errors.New("no channel is configured")
	default:
		return fmt.Errorf("unknown channel %q", s.Channel)
	}
	req.Header.Set("User-Agent", "ccu-addon-battery")
	res, err := n.Client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	io.Copy(io.Discard, io.LimitReader(res.Body, 4096))
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return fmt.Errorf("%s answered %s", s.Channel, res.Status)
	}
	return nil
}

// asciiHeader keeps a header value within what HTTP carries: ntfy reads a UTF-8 title only when
// it is RFC 2047-encoded; plain ASCII is left, anything else goes encoded.
func asciiHeader(s string) string {
	for _, r := range s {
		if r > 126 || r < 32 {
			return "=?UTF-8?B?" + base64(s) + "?="
		}
	}
	return s
}

func base64(s string) string {
	const tbl = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	b := []byte(s)
	var out strings.Builder
	for i := 0; i < len(b); i += 3 {
		var n uint32
		rem := len(b) - i
		for j := 0; j < 3; j++ {
			n <<= 8
			if j < rem {
				n |= uint32(b[i+j])
			}
		}
		for j := 0; j < 4; j++ {
			if j <= rem {
				out.WriteByte(tbl[(n>>(18-6*uint(j)))&63])
			} else {
				out.WriteByte('=')
			}
		}
	}
	return out.String()
}

// Texts are the notices' words, German and English.
var Texts = map[string]map[string]string{
	"de": {
		"low.title":   "Batterie leer: {name}",
		"low.text":    "{name} ({model}) meldet eine leere Batterie.",
		"due.title":   "Batteriewechsel fällig: {name}",
		"due.text":    "Die Batterien von {name} ({model}) sind seit {days} Tagen fällig (voraussichtliche Haltedauer erreicht).",
		"soon.title":  "Batteriewechsel bald fällig: {name}",
		"soon.text":   "Die Batterien von {name} ({model}) sind voraussichtlich in {days} Tagen fällig.",
		"inbox.title": "Erkannter Batteriewechsel wartet",
		"inbox.text":  "{count} erkannte Batteriewechsel warten im Eingang auf Ihre Bestätigung.",
		"test.title":  "Testnachricht von Batterie",
		"test.text":   "Die Benachrichtigungen des Batterie-Addons kommen an.",
	},
	"en": {
		"low.title":   "Battery low: {name}",
		"low.text":    "{name} ({model}) reports a low battery.",
		"due.title":   "Battery change due: {name}",
		"due.text":    "The batteries of {name} ({model}) have been due for {days} days (the expected life is reached).",
		"soon.title":  "Battery change due soon: {name}",
		"soon.text":   "The batteries of {name} ({model}) are due in about {days} days.",
		"inbox.title": "A detected battery change waits",
		"inbox.text":  "{count} detected battery changes wait in the inbox for your confirmation.",
		"test.title":  "Test message from Battery",
		"test.text":   "The Battery addon's notifications arrive.",
	},
}

// Text fills a notice's words.
func Text(lang, key string, params map[string]string) string {
	m := Texts[lang]
	if m == nil {
		m = Texts["de"]
	}
	s := m[key]
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		s = strings.ReplaceAll(s, "{"+k+"}", params[k])
	}
	return s
}

// Run dispatches what collect answers every interval, and once at the start after a short
// delay (the recorder needs a moment to know the devices).
func (n *Notifier) Run(ctx context.Context, interval time.Duration, collect func(context.Context) []Event) {
	t := time.NewTimer(2 * time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		if n.Settings().Channel != "" {
			if sent := n.Dispatch(ctx, collect(ctx)); sent > 0 {
				n.Log("notify: %d notice(s) sent", sent)
			}
		}
		t.Reset(interval)
	}
}
