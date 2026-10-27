// Package settings holds the user's settings in etc/battery.json beside the storage location
// (store.SaveLocation) and the detection windows: the shopping horizon and the stock of
// batteries, and the notification channel. Reads keep the file's other keys; writes are atomic.
package settings

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/hobbyquaker/ccu-addon-battery/internal/store"
)

// Shopping is the shopping list's settings.
type Shopping struct {
	// Days is the horizon: devices due within it are on the list (90).
	Days int `json:"days"`
	// Stock is what the user has at home, per battery type ("AA": 4).
	Stock map[string]int `json:"stock"`
}

// Notify is the notification channel.
type Notify struct {
	// Channel: "" (off), "webhook" (a POST with a JSON body), "ntfy" (a POST to <url>/<topic>).
	Channel string `json:"channel"`
	URL     string `json:"url,omitempty"`
	// Topic is ntfy's topic; Token its access token (sent as a bearer), if any.
	Topic string `json:"topic,omitempty"`
	Token string `json:"token,omitempty"`
	// Headers go with a webhook request (an API key, say).
	Headers map[string]string `json:"headers,omitempty"`
	// Events: low (a device reports a low battery), due (the forecast date passed), soon (within
	// the horizon), inbox (a detection waits for the user).
	Events []string `json:"events"`
	// Language of the texts, de or en.
	Language string `json:"language"`
}

// Events the notifier knows.
var Events = []string{"low", "due", "soon", "inbox"}

// Settings is what the page edits.
type Settings struct {
	Shopping Shopping `json:"shopping"`
	Notify   Notify   `json:"notify"`
}

// Defaults are an unconfigured addon's settings.
func Defaults() Settings {
	return Settings{
		Shopping: Shopping{Days: 90, Stock: map[string]int{}},
		Notify:   Notify{Channel: "", Events: []string{"low", "due", "inbox"}, Language: "de"},
	}
}

// Validate checks what the user sent.
func (s *Settings) Validate() error {
	if s.Shopping.Days < 1 || s.Shopping.Days > 3650 {
		return errors.New("shopping.days is 1 to 3650")
	}
	if s.Shopping.Stock == nil {
		s.Shopping.Stock = map[string]int{}
	}
	for k, v := range s.Shopping.Stock {
		if strings.TrimSpace(k) == "" || len(k) > 20 {
			return fmt.Errorf("shopping.stock: %q is not a battery type", k)
		}
		if v < 0 || v > 10000 {
			return fmt.Errorf("shopping.stock.%s is 0 to 10000", k)
		}
		if v == 0 {
			delete(s.Shopping.Stock, k)
		}
	}
	n := &s.Notify
	switch n.Channel {
	case "", "webhook", "ntfy":
	default:
		return errors.New("notify.channel is empty, webhook or ntfy")
	}
	if n.Channel != "" {
		u, err := url.Parse(strings.TrimSpace(n.URL))
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return errors.New("notify.url must be an http(s) URL")
		}
		n.URL = strings.TrimRight(u.String(), "/")
	}
	if n.Channel == "ntfy" {
		n.Topic = strings.TrimSpace(n.Topic)
		if n.Topic == "" || strings.ContainsAny(n.Topic, "/ ?#") {
			return errors.New("notify.topic is required for ntfy and has no slash")
		}
	}
	for k := range n.Headers {
		if strings.ContainsAny(k, " :\r\n") || k == "" {
			return fmt.Errorf("notify.headers: %q is not a header name", k)
		}
	}
	if n.Language != "de" && n.Language != "en" {
		n.Language = "de"
	}
	seen := map[string]bool{}
	var ev []string
	for _, e := range n.Events {
		ok := false
		for _, known := range Events {
			if e == known {
				ok = true
			}
		}
		if !ok {
			return fmt.Errorf("notify.events: %q is unknown", e)
		}
		if !seen[e] {
			seen[e] = true
			ev = append(ev, e)
		}
	}
	sort.Strings(ev)
	if ev == nil {
		ev = []string{}
	}
	n.Events = ev
	return nil
}

// File is the settings file with its current content.
type File struct {
	Path string

	mu  sync.RWMutex
	cur Settings
}

// Open reads the file; a missing file or missing keys give the defaults.
func Open(path string) (*File, error) {
	f := &File{Path: path, cur: Defaults()}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return f, nil
	}
	if err != nil {
		return nil, err
	}
	var doc struct {
		Shopping *Shopping `json:"shopping"`
		Notify   *Notify   `json:"notify"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if doc.Shopping != nil {
		f.cur.Shopping = *doc.Shopping
	}
	if doc.Notify != nil {
		f.cur.Notify = *doc.Notify
	}
	if err := f.cur.Validate(); err != nil {
		// a hand-edited file that does not pass: keep the defaults, say so
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return f, nil
}

// Fallback is a file that could not be read: the defaults, saved to path on the next Set.
func Fallback(path string) *File { return &File{Path: path, cur: Defaults()} }

// Get is the current settings (a copy).
func (f *File) Get() Settings {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return copySettings(f.cur)
}

// Set validates and saves new settings, keeping the file's other keys.
func (f *File) Set(s Settings) error {
	if err := s.Validate(); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := store.SaveSetting(f.Path, "shopping", s.Shopping); err != nil {
		return err
	}
	if err := store.SaveSetting(f.Path, "notify", s.Notify); err != nil {
		return err
	}
	f.cur = copySettings(s)
	return nil
}

func copySettings(s Settings) Settings {
	out := s
	out.Shopping.Stock = map[string]int{}
	for k, v := range s.Shopping.Stock {
		out.Shopping.Stock[k] = v
	}
	out.Notify.Events = append([]string{}, s.Notify.Events...)
	if s.Notify.Headers != nil {
		out.Notify.Headers = map[string]string{}
		for k, v := range s.Notify.Headers {
			out.Notify.Headers[k] = v
		}
	}
	return out
}
