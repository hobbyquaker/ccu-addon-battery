// Package httpapi is the addon's own HTTP server: the Svelte app's files and the JSON API under
// one base path (/addons/battery/), which lighttpd proxies to the loopback port. Every request
// carries the gate's session header, confirmed through addonkit; writes carry the kit's CSRF
// header; moving the database needs an administrator. docs/api.md documents the routes and
// docs/openapi.json (served at api/v1/openapi.json) specifies them.
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/hobbyquaker/ccu-addon-battery/internal/changes"
	"github.com/hobbyquaker/ccu-addon-battery/internal/recorder"
	"github.com/hobbyquaker/ccu-addon-battery/internal/store"
	"github.com/hobbyquaker/ccu-addon-kit/go/addonkit"
)

// Options configure the server.
type Options struct {
	// BasePath is where the app is served, /addons/battery (no trailing slash).
	BasePath string
	// WWW is the directory with the built app (index.html and its assets).
	WWW      string
	Store    *store.Store
	Recorder *recorder.Recorder
	Changes  *changes.Service
	// Session checks the sessions; nil serves without a check (development only).
	Session *addonkit.Checker
	Version string
	Started time.Time
}

// New builds the handler.
func New(o Options) http.Handler {
	o.BasePath = strings.TrimRight(o.BasePath, "/")
	api := http.NewServeMux()
	handle(api, "GET /api/v1/health", o.health)
	handle(api, "GET /api/v1/devices", o.devices)
	handle(api, "GET /api/v1/storage", o.storage)
	handle(api, "POST /api/v1/storage/check", o.storageCheck)
	handle(api, "POST /api/v1/storage/move", o.storageMove)
	o.routesChanges(api)
	api.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		addonkit.WriteError(w, http.StatusNotFound, "not-found", r.URL.Path)
	})

	root := http.NewServeMux()
	root.Handle("/api/", addonkit.CSRF(api))
	root.Handle("/", http.HandlerFunc(o.static))

	var h http.Handler = http.StripPrefix(o.BasePath, root)
	if o.Session != nil {
		h = o.Session.Handler(h)
	}
	return guard(h)
}

// Routes lists every "METHOD /path" the API serves; a test holds docs/openapi.json to it.
var Routes []string

func handle(mux *http.ServeMux, pattern string, h http.HandlerFunc) {
	found := false
	for _, r := range Routes {
		if r == pattern {
			found = true
		}
	}
	if !found {
		Routes = append(Routes, pattern)
	}
	mux.HandleFunc(pattern, h)
}

// guard refuses a path that is not clean (the mux would redirect it, and after StripPrefix the
// redirect would point outside the base path) and marks API answers as not cacheable.
func guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if p := r.URL.Path; p != "/" && (path.Clean(p) != strings.TrimSuffix(p, "/") && path.Clean(p) != p) {
			http.NotFound(w, r)
			return
		}
		if strings.Contains(r.URL.Path, "/api/") {
			w.Header().Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func lang(r *http.Request) string {
	if l := r.URL.Query().Get("lang"); l == "de" || l == "en" {
		return l
	}
	_, l := addonkit.Prefs(r)
	if l == "" {
		l = "de"
	}
	return l
}

func (o Options) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{
		"ok":       o.Store.Available(),
		"version":  o.Version,
		"uptime_s": int(time.Since(o.Started).Seconds()),
		"recorder": o.Recorder.State(),
		"storage":  o.Store.Health(),
		"devices":  len(o.Recorder.Devices("en")),
		"inbox":    o.inbox(r),
	})
}

// inbox counts the detections waiting for the user, -1 when the store cannot say.
func (o Options) inbox(r *http.Request) int {
	if o.Changes == nil {
		return 0
	}
	l, err := o.Changes.List(r.Context(), changes.Filter{Statuses: []string{changes.StatusPending}})
	if err != nil {
		return -1
	}
	return len(l)
}

func (o Options) devices(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"devices": o.Recorder.Devices(lang(r))})
}

func (o Options) storage(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{
		"health":     o.Store.Health(),
		"candidates": o.Store.Candidates(),
	})
}

type moveBody struct {
	store.Location
	KeepOld bool `json:"keep_old"`
}

func decodeMove(w http.ResponseWriter, r *http.Request) (moveBody, bool) {
	var b moveBody
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&b); err != nil {
		addonkit.WriteError(w, http.StatusBadRequest, "invalid", err.Error())
		return b, false
	}
	if b.ID == "" {
		b.ID = "userfs"
	}
	if err := b.Validate(); err != nil {
		addonkit.WriteError(w, http.StatusUnprocessableEntity, "invalid-location", err.Error())
		return b, false
	}
	return b, true
}

// storageCheck says whether a location can take the database, without moving anything.
func (o Options) storageCheck(w http.ResponseWriter, r *http.Request) {
	b, ok := decodeMove(w, r)
	if !ok {
		return
	}
	dir, err := b.Dir(filepath.Dir(filepath.Dir(o.Store.Path())), o.mounts())
	if err != nil {
		writeJSON(w, 200, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	need := int64(16 << 20)
	if st, err := os.Stat(o.Store.Path()); err == nil {
		need += 2 * st.Size()
	}
	if err := store.CheckTarget(dir, need); err != nil {
		writeJSON(w, 200, map[string]any{"ok": false, "error": err.Error(), "path": filepath.Join(dir, store.FileName)})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "path": filepath.Join(dir, store.FileName), "in_backup": b.InBackup()})
}

func (o Options) mounts() []store.Mount {
	var out []store.Mount
	for _, c := range o.Store.Candidates() {
		if strings.HasPrefix(c.ID, "usb:") && c.Name != "" {
			out = append(out, store.Mount{Path: c.Path, Label: c.Name})
		}
	}
	return out
}

func (o Options) storageMove(w http.ResponseWriter, r *http.Request) {
	if o.Session != nil {
		if s := addonkit.FromRequest(r); s == nil || !addonkit.Admits(addonkit.LevelAdmin, s.Level) {
			addonkit.WriteError(w, http.StatusForbidden, "forbidden", "moving the database needs an administrator")
			return
		}
	}
	b, ok := decodeMove(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Minute)
	defer cancel()
	if err := o.Store.Move(ctx, b.Location, b.KeepOld, nil); err != nil {
		code := "move-failed"
		if errors.Is(err, store.ErrNotPresent) {
			code = "not-present"
		}
		addonkit.WriteError(w, http.StatusConflict, code, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "storage": o.Store.Health()})
}

// static serves the app: a file under WWW, or index.html for a path without one (the app's own
// routes). Dotfiles and paths outside WWW answer 404.
func (o Options) static(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		addonkit.WriteError(w, http.StatusMethodNotAllowed, "method", r.Method)
		return
	}
	p := path.Clean("/" + r.URL.Path)
	if strings.Contains(p, "/.") {
		http.NotFound(w, r)
		return
	}
	file := filepath.Join(o.WWW, filepath.FromSlash(p))
	if st, err := os.Stat(file); err == nil && !st.IsDir() {
		if strings.HasPrefix(p, "/assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		http.ServeFile(w, r, file)
		return
	}
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeFile(w, r, filepath.Join(o.WWW, "index.html"))
}
