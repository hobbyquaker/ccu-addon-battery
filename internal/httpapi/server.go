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
	"fmt"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/hobbyquaker/ccu-addon-battery/internal/changes"
	"github.com/hobbyquaker/ccu-addon-battery/internal/devimg"
	"github.com/hobbyquaker/ccu-addon-battery/internal/notify"
	"github.com/hobbyquaker/ccu-addon-battery/internal/recorder"
	"github.com/hobbyquaker/ccu-addon-battery/internal/settings"
	"github.com/hobbyquaker/ccu-addon-battery/internal/snapshot"
	"github.com/hobbyquaker/ccu-addon-battery/internal/stats"
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
	// Images maps a model to the platform's device picture; nil: none.
	Images *devimg.Catalog
	// Settings is the user's settings file (shopping, notifications); nil: the defaults, read only.
	Settings *settings.File
	// Notifier sends the notices; nil: no notifications.
	Notifier *notify.Notifier
	// Session checks the sessions (openccu-lite); nil serves without a check unless Wrap is set.
	Session *addonkit.Checker
	// Wrap is the session check as a handler wrapper (openccu-lite's or a classic CCU's); it
	// takes precedence over Session. nil (and no Session): no check, development only.
	Wrap func(http.Handler) http.Handler
	// Backup answers the database's part in the system backup; nil: not shown.
	Backup func() snapshot.Status
	// BackupCopy sets how many days the addon's own snapshot may get old (1 or 7); nil: not offered.
	BackupCopy func(days int) error
	Version    string
	Started    time.Time
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
	handle(api, "PUT /api/v1/storage/interval", o.storageInterval)
	handle(api, "PUT /api/v1/storage/backup-copy", o.storageBackupCopy)
	o.routesChanges(api)
	api.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		addonkit.WriteError(w, http.StatusNotFound, "not-found", r.URL.Path)
	})

	root := http.NewServeMux()
	root.Handle("/api/", addonkit.CSRF(api))
	root.Handle("/", addonkit.AppFiles(o.WWW))

	var h http.Handler = http.StripPrefix(o.BasePath, root)
	switch {
	case o.Wrap != nil:
		h = o.Wrap(h)
	case o.Session != nil:
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

// inbox counts the detections waiting for the user (the ones still being checked included), -1
// when the store cannot say.
func (o Options) inbox(r *http.Request) int {
	if o.Changes == nil {
		return 0
	}
	l, err := o.Changes.List(r.Context(), changes.Filter{Statuses: changes.InboxStatuses})
	if err != nil {
		return -1
	}
	return len(l)
}

// device is a recorder view with the platform's picture and the device's battery lives.
type device struct {
	recorder.View
	Image string            `json:"image,omitempty"`
	Life  *stats.DeviceLife `json:"life,omitempty"`
}

func (o Options) image(model string) string {
	if o.Images == nil {
		return ""
	}
	return o.Images.Image(model)
}

func (o Options) devices(w http.ResponseWriter, r *http.Request) {
	views := o.Recorder.Devices(lang(r))
	var lives map[string]stats.DeviceLife
	if st, err := o.computeStats(r); err == nil {
		lives = st.Devices
	}
	out := make([]device, 0, len(views))
	for _, v := range views {
		d := device{View: v, Image: o.image(v.Model)}
		if l, ok := lives[v.Interface+"."+v.Address]; ok {
			d.Life = &l
		}
		out = append(out, d)
	}
	writeJSON(w, 200, map[string]any{"devices": out, "filters": o.Recorder.Filters()})
}

func (o Options) storage(w http.ResponseWriter, r *http.Request) {
	out := map[string]any{
		"health":     o.Store.Health(),
		"candidates": o.Store.Candidates(),
	}
	if o.Backup != nil {
		out["backup"] = o.Backup()
	}
	writeJSON(w, 200, out)
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

func (o Options) admin(w http.ResponseWriter, r *http.Request, what string) bool {
	if o.Session != nil || o.Wrap != nil {
		if s := addonkit.FromRequest(r); s == nil || !addonkit.Admits(addonkit.LevelAdmin, s.Level) {
			addonkit.WriteError(w, http.StatusForbidden, "forbidden", what+" needs an administrator")
			return false
		}
	}
	return true
}

// storageInterval sets the write interval: {"minutes": 1 | 5 | 10 | 30 | 60}.
func (o Options) storageInterval(w http.ResponseWriter, r *http.Request) {
	if !o.admin(w, r, "changing the write interval") {
		return
	}
	var b struct {
		Minutes int `json:"minutes"`
	}
	if !decode(w, r, &b) {
		return
	}
	if !store.ValidInterval(b.Minutes) {
		addonkit.WriteError(w, http.StatusUnprocessableEntity, "invalid-interval", fmt.Sprintf("minutes is one of %v", store.IntervalChoices))
		return
	}
	if err := o.Store.SetInterval(b.Minutes); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "storage": o.Store.Health()})
}

// storageBackupCopy sets how old the addon's own snapshot for the firmware's backup may get:
// {"days": 1 | 7}. Only where the system runs no hook (the backup's mode "daily").
func (o Options) storageBackupCopy(w http.ResponseWriter, r *http.Request) {
	if !o.admin(w, r, "changing the backup copy") {
		return
	}
	var b struct {
		Days int `json:"days"`
	}
	if !decode(w, r, &b) {
		return
	}
	if o.BackupCopy == nil || o.Backup == nil || o.Backup().Mode != "daily" {
		addonkit.WriteError(w, http.StatusConflict, "not-daily", "the system takes the copy before every backup itself")
		return
	}
	if !snapshot.ValidCopyDays(b.Days) {
		addonkit.WriteError(w, http.StatusUnprocessableEntity, "invalid-days", fmt.Sprintf("days is one of %v", snapshot.CopyChoices))
		return
	}
	if err := o.BackupCopy(b.Days); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "backup": o.Backup()})
}

func (o Options) storageMove(w http.ResponseWriter, r *http.Request) {
	if !o.admin(w, r, "moving the database") {
		return
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
