package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hobbyquaker/ccu-addon-battery/internal/recorder"
	"github.com/hobbyquaker/ccu-addon-battery/internal/store"
	"github.com/hobbyquaker/ccu-addon-kit/go/addonkit"
	"github.com/hobbyquaker/ccu-addon-kit/go/litesource"
	"github.com/hobbyquaker/ccu-addon-kit/go/litesource/fakelite"
)

func TestMain(m *testing.M) {
	dir, err := filepath.Abs(filepath.Join("..", "..", ".testtmp", "httpapi"))
	if err == nil && os.MkdirAll(dir, 0o755) == nil {
		os.Setenv("TMPDIR", dir)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

const user, admin = "USERUSERUSERUSERUSERUSERUS", "ADMINADMINADMINADMINADMINA"

func server(t *testing.T) (http.Handler, *store.Store, *fakelite.Server, string) {
	t.Helper()
	s := fakelite.New()
	t.Cleanup(s.Close)
	s.Sessions[user] = fakelite.Session{User: "alice", Level: "configure"}
	s.Sessions[admin] = fakelite.Session{User: "root", Level: "administer"}
	addon := t.TempDir()
	www := filepath.Join(addon, "www")
	os.MkdirAll(filepath.Join(www, "assets"), 0o755)
	os.WriteFile(filepath.Join(www, "index.html"), []byte("<html>app</html>"), 0o644)
	os.WriteFile(filepath.Join(www, "assets", "a.js"), []byte("js"), 0o644)
	st, err := store.Open(store.Config{AddonDir: addon, Log: t.Logf, Mounts: func() []store.Mount {
		return []store.Mount{{Path: filepath.Join(addon, "stick"), Label: "STICK"}}
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	rec := recorder.New(recorder.Options{Source: litesource.New(s.URL, nil), Store: st, Log: t.Logf})
	checker := addonkit.NewChecker(addonkit.SessionOptions{Base: s.URL, Platform: func() addonkit.Kind { return addonkit.OpenccuLite }})
	h := New(Options{BasePath: "/addons/battery", WWW: www, Store: st, Recorder: rec, Session: checker, Version: "test", Started: time.Now()})
	return h, st, s, addon
}

func call(h http.Handler, method, path, sid, body string, hdr ...string) *httptest.ResponseRecorder {
	var rd *strings.Reader
	if body != "" {
		rd = strings.NewReader(body)
	} else {
		rd = strings.NewReader("")
	}
	r := httptest.NewRequest(method, "http://ccu.local"+path, rd)
	r.Host = "ccu.local"
	if sid != "" {
		r.Header.Set(addonkit.HeaderSession, sid)
	}
	for i := 0; i+1 < len(hdr); i += 2 {
		r.Header.Set(hdr[i], hdr[i+1])
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestRoutes(t *testing.T) {
	h, _, _, addon := server(t)
	if w := call(h, "GET", "/addons/battery/", "", ""); w.Code != 401 {
		t.Fatalf("no session: %d", w.Code)
	}
	if w := call(h, "GET", "/addons/battery/", user, ""); w.Code != 200 || !strings.Contains(w.Body.String(), "app") {
		t.Fatalf("index: %d %s", w.Code, w.Body)
	}
	if w := call(h, "GET", "/addons/battery/devices", user, ""); w.Code != 200 || !strings.Contains(w.Body.String(), "app") {
		t.Fatalf("app route falls back to index: %d", w.Code)
	}
	if w := call(h, "GET", "/addons/battery/assets/a.js", user, ""); w.Code != 200 || !strings.Contains(w.Header().Get("Cache-Control"), "immutable") {
		t.Fatalf("asset: %d %s", w.Code, w.Header())
	}
	if w := call(h, "GET", "/addons/battery/../etc/passwd", user, ""); w.Code != 404 {
		t.Fatalf("an unclean path is 404, never a redirect: %d %s", w.Code, w.Body)
	}
	if w := call(h, "GET", "/addons/battery/x/../index.html", user, ""); w.Code != 404 {
		t.Fatalf("an unclean path is 404: %d", w.Code)
	}
	w := call(h, "GET", "/addons/battery/api/v1/health", user, "")
	var health map[string]any
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &health) != nil || health["ok"] != true || health["version"] != "test" {
		t.Fatalf("health: %d %s", w.Code, w.Body)
	}
	if w := call(h, "GET", "/addons/battery/api/v1/devices?lang=en", user, ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"devices":[]`) {
		t.Fatalf("devices: %d %s", w.Code, w.Body)
	}
	if w := call(h, "GET", "/addons/battery/api/v1/nope", user, ""); w.Code != 404 {
		t.Fatalf("unknown api: %d", w.Code)
	}
	w = call(h, "GET", "/addons/battery/api/v1/storage", user, "")
	var storage struct {
		Health     store.Health      `json:"health"`
		Candidates []store.Candidate `json:"candidates"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &storage) != nil || storage.Health.State != "ok" || !storage.Health.InBackup || len(storage.Candidates) != 2 || !storage.Candidates[0].Current || storage.Candidates[1].ID != "usb:STICK" {
		t.Fatalf("storage: %d %s", w.Code, w.Body)
	}
	// writes: CSRF header, then the level
	move := `{"location":"usb:STICK","keep_old":true}`
	if w := call(h, "POST", "/addons/battery/api/v1/storage/move", admin, move); w.Code != 403 || !strings.Contains(w.Body.String(), "csrf") {
		t.Fatalf("no CSRF header: %d %s", w.Code, w.Body)
	}
	if w := call(h, "POST", "/addons/battery/api/v1/storage/move", user, move, addonkit.HeaderRequest, "1"); w.Code != 403 || !strings.Contains(w.Body.String(), "administrator") {
		t.Fatalf("a user may not move: %d %s", w.Code, w.Body)
	}
	if w := call(h, "POST", "/addons/battery/api/v1/storage/check", user, move, addonkit.HeaderRequest, "1"); w.Code != 200 || !strings.Contains(w.Body.String(), `"ok":true`) || !strings.Contains(w.Body.String(), `"in_backup":false`) {
		t.Fatalf("check: %d %s", w.Code, w.Body)
	}
	if w := call(h, "POST", "/addons/battery/api/v1/storage/check", user, `{"location":"share:nas"}`, addonkit.HeaderRequest, "1"); w.Code != 422 {
		t.Fatalf("a share is refused: %d %s", w.Code, w.Body)
	}
	if w := call(h, "POST", "/addons/battery/api/v1/storage/move", admin, move, addonkit.HeaderRequest, "1"); w.Code != 200 {
		t.Fatalf("move: %d %s", w.Code, w.Body)
	}
	if _, err := os.Stat(filepath.Join(addon, "stick", "battery", store.FileName)); err != nil {
		t.Fatal("moved file missing")
	}
	if _, err := os.Stat(filepath.Join(addon, "var", store.FileName)); err != nil {
		t.Fatal("keep_old did not keep the old file")
	}
	if w := call(h, "POST", "/addons/battery/api/v1/storage/move", admin, `{"location":"usb:NOPE"}`, addonkit.HeaderRequest, "1"); w.Code != 409 || !strings.Contains(w.Body.String(), "not-present") {
		t.Fatalf("absent stick: %d %s", w.Code, w.Body)
	}
	if w := call(h, "POST", "/addons/battery/api/v1/storage/move", admin, `{"location":"usb:STICK"}`, addonkit.HeaderRequest, "1"); w.Code != 409 {
		t.Fatalf("move onto itself: %d %s", w.Code, w.Body)
	}
	if w := call(h, "GET", "/addons/battery/api/v1/storage", user, ""); !strings.Contains(w.Body.String(), `"in_backup":false`) {
		t.Fatalf("after the move: %s", w.Body)
	}
}
