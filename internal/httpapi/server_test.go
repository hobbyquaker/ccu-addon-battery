package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hobbyquaker/ccu-addon-battery/docs"
	"github.com/hobbyquaker/ccu-addon-battery/internal/changes"
	"github.com/hobbyquaker/ccu-addon-battery/internal/devimg"
	"github.com/hobbyquaker/ccu-addon-battery/internal/notify"
	"github.com/hobbyquaker/ccu-addon-battery/internal/recorder"
	"github.com/hobbyquaker/ccu-addon-battery/internal/settings"
	"github.com/hobbyquaker/ccu-addon-battery/internal/stats"
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
	chg := changes.New(changes.Options{Store: st, Log: t.Logf})
	rec := recorder.New(recorder.Options{Source: litesource.New(s.URL, nil), Store: st, Observer: chg, Log: t.Logf})
	checker := addonkit.NewChecker(addonkit.SessionOptions{Base: s.URL, Platform: func() addonkit.Kind { return addonkit.OpenccuLite }})
	devdb := filepath.Join(addon, "DEVDB.tcl")
	os.WriteFile(devdb, []byte(`array set DEV_PATHS {HmIP-WRC2 {{50 /config/img/devices/50/112_hmip-wrc2_thumb.png} {250 /config/img/devices/250/112_hmip-wrc2.png}}}`), 0o644)
	sf := settings.Fallback(filepath.Join(addon, "etc", "battery.json"))
	notifier := notify.New(func() settings.Notify { return sf.Get().Notify }, st)
	h := New(Options{BasePath: "/addons/battery", WWW: www, Store: st, Recorder: rec, Changes: chg, Images: &devimg.Catalog{Path: devdb}, Settings: sf, Notifier: notifier, Session: checker, Version: "test", Started: time.Now()})
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

// Every route the API serves is in docs/openapi.json with its method, and nothing more.
func TestOpenAPICoversRoutes(t *testing.T) {
	server(t)
	var doc struct {
		Paths map[string]map[string]any `json:"paths"`
	}
	if err := json.Unmarshal(docs.OpenAPI, &doc); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, r := range Routes {
		method, path, _ := strings.Cut(r, " ")
		ops, ok := doc.Paths[path]
		if !ok || ops[strings.ToLower(method)] == nil {
			t.Errorf("%s is not in openapi.json", r)
		}
		seen[strings.ToLower(method)+" "+path] = true
	}
	for path, ops := range doc.Paths {
		for m := range ops {
			if !seen[m+" "+path] {
				t.Errorf("openapi.json has %s %s, the API does not", m, path)
			}
		}
	}
}

func TestChangesAPI(t *testing.T) {
	h, st, _, _ := server(t)
	st.UpsertDevice(store.Device{Interface: "HmIP-RF", Address: "WRC2", Model: "HmIP-WRC2", VoltageDP: "0/OPERATING_VOLTAGE", LowBatDP: "0/LOW_BAT", FirstSeen: 1, LastSeen: 1})
	st.UpsertDevice(store.Device{Interface: "BidCos-RF", Address: "SC", Model: "HM-Sec-SC-2", LowBatDP: "0/LOWBAT", FirstSeen: 1, LastSeen: 1})
	st.Flush()
	csrf := []string{addonkit.HeaderRequest, "1"}
	if w := call(h, "POST", "/addons/battery/api/v1/changes", user, `{"device":"WRC2","at":"2026-01-10"}`); w.Code != 403 {
		t.Fatalf("no CSRF: %d", w.Code)
	}
	w := call(h, "POST", "/addons/battery/api/v1/changes", user, `{"device":"WRC2","at":"2026-01-10","batteries":[{"count":2,"type":"AAA","vendor":"Varta","price_cents":80}]}`, csrf...)
	var out changes.Outcome
	if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &out) != nil || out.Status != "created" {
		t.Fatalf("create: %d %s", w.Code, w.Body)
	}
	if w := call(h, "POST", "/addons/battery/api/v1/changes", user, `{"device":"WRC2","at":"2026-01-11"}`, csrf...); w.Code != 200 || !strings.Contains(w.Body.String(), `"duplicate"`) {
		t.Fatalf("duplicate: %d %s", w.Code, w.Body)
	}
	if w := call(h, "POST", "/addons/battery/api/v1/changes", user, `{"device":"NOPE","at":"2026-01-11"}`, csrf...); w.Code != 422 || !strings.Contains(w.Body.String(), "unknown-device") {
		t.Fatalf("unknown: %d %s", w.Code, w.Body)
	}
	if w := call(h, "POST", "/addons/battery/api/v1/changes", user, `{"device":"WRC2","at":"2026-01-11","colour":"red"}`, csrf...); w.Code != 400 {
		t.Fatalf("unknown field: %d %s", w.Code, w.Body)
	}
	// bulk CSV with an Idempotency-Key: the retry is replayed, not run again
	csv := "address,at,count,type,chemistry,rechargeable,vendor,product,price_cents,note,external_id\nSC,2025-05-01,2,LR44,,,,,,,r1\nSC,2025-09-01,2,LR44,,,,,,,r2\n"
	idem := append([]string{"Content-Type", "text/csv", "Idempotency-Key", "k1"}, csrf...)
	w = call(h, "POST", "/addons/battery/api/v1/changes:bulk", user, csv, idem...)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"created":2`) {
		t.Fatalf("bulk: %d %s", w.Code, w.Body)
	}
	w = call(h, "POST", "/addons/battery/api/v1/changes:bulk", user, csv, idem...)
	if w.Code != 200 || w.Header().Get("Idempotent-Replayed") != "true" || !strings.Contains(w.Body.String(), `"created":2`) {
		t.Fatalf("replay: %d %s %v", w.Code, w.Body, w.Header())
	}
	if w := call(h, "POST", "/addons/battery/api/v1/changes:bulk", user, csv+"SC,2024-01-01,,,,,,,,,r3\n", idem...); w.Code != 409 {
		t.Fatalf("key reused: %d %s", w.Code, w.Body)
	}
	if w := call(h, "POST", "/addons/battery/api/v1/changes:bulk?dry_run=true", user, `[{"device":"SC","at":"2024-01-01"}]`, csrf...); w.Code != 200 || !strings.Contains(w.Body.String(), `"applied":false`) {
		t.Fatalf("dry json bulk: %d %s", w.Code, w.Body)
	}
	// list, edit, delete
	w = call(h, "GET", "/addons/battery/api/v1/changes?device=SC&status=accepted", user, "")
	var l struct {
		Changes []changes.Change `json:"changes"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &l) != nil || len(l.Changes) != 2 {
		t.Fatalf("list: %d %s", w.Code, w.Body)
	}
	if w := call(h, "GET", "/addons/battery/api/v1/changes?status=maybe", user, ""); w.Code != 422 {
		t.Fatalf("bad status: %d", w.Code)
	}
	id := strconv.FormatInt(l.Changes[0].ID, 10)
	if w := call(h, "PATCH", "/addons/battery/api/v1/changes/"+id, user, `{"note":"Keller","status":"rejected"}`, csrf...); w.Code != 200 || !strings.Contains(w.Body.String(), `"rejected"`) {
		t.Fatalf("patch: %d %s", w.Code, w.Body)
	}
	if w := call(h, "PUT", "/addons/battery/api/v1/changes/by-external-id/r1", user, `{"note":"x"}`, csrf...); w.Code != 200 {
		t.Fatalf("by external id: %d %s", w.Code, w.Body)
	}
	if w := call(h, "GET", "/addons/battery/api/v1/changes/"+id, user, ""); w.Code != 200 || !strings.Contains(w.Body.String(), "Keller") {
		t.Fatalf("get: %d %s", w.Code, w.Body)
	}
	if w := call(h, "DELETE", "/addons/battery/api/v1/changes/"+id, user, "", csrf...); w.Code != 204 {
		t.Fatalf("delete: %d", w.Code)
	}
	if w := call(h, "GET", "/addons/battery/api/v1/changes/"+id, user, ""); w.Code != 404 {
		t.Fatalf("deleted: %d", w.Code)
	}
	if w := call(h, "GET", "/addons/battery/api/v1/changes/abc", user, ""); w.Code != 404 {
		t.Fatalf("bad id: %d", w.Code)
	}
	// samples, detect
	var samples []map[string]any
	for i := 0; i < 20; i++ {
		samples = append(samples, map[string]any{"device": "WRC2", "ts": int64(1767225600000) + int64(i)*86400000, "voltage": 2.5})
	}
	samples = append(samples, map[string]any{"device": "WRC2", "ts": int64(1767225600000) + 20*86400000, "voltage": 3.2}, map[string]any{"device": "WRC2", "ts": int64(1767225600000) + 21*86400000 + 3600000, "voltage": 3.15})
	b, _ := json.Marshal(samples)
	if w := call(h, "POST", "/addons/battery/api/v1/samples:bulk", user, string(b), csrf...); w.Code != 200 || !strings.Contains(w.Body.String(), `"stored":22`) {
		t.Fatalf("samples: %d %s", w.Code, w.Body)
	}
	if w := call(h, "GET", "/addons/battery/api/v1/samples?device=WRC2", user, ""); w.Code != 200 {
		t.Fatalf("samples get: %d %s", w.Code, w.Body)
	}
	if w := call(h, "GET", "/addons/battery/api/v1/samples", user, ""); w.Code != 422 {
		t.Fatalf("samples without a device: %d", w.Code)
	}
	w = call(h, "POST", "/addons/battery/api/v1/detect?device=WRC2&from=2026-01-01&to=2026-03-01", user, "", csrf...)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"status":"created"`) {
		t.Fatalf("detect: %d %s", w.Code, w.Body)
	}
	if w := call(h, "POST", "/addons/battery/api/v1/detect?from=yesterday", user, "", csrf...); w.Code != 422 {
		t.Fatalf("detect bad time: %d", w.Code)
	}
	if w := call(h, "GET", "/addons/battery/api/v1/suggestions?field=vendor&q=v", user, ""); w.Code != 200 || !strings.Contains(w.Body.String(), "Varta") {
		t.Fatalf("suggestions: %d %s", w.Code, w.Body)
	}
	if w := call(h, "GET", "/addons/battery/api/v1/health", user, ""); !strings.Contains(w.Body.String(), `"inbox":1`) {
		t.Fatalf("inbox count: %s", w.Body)
	}
	// exports
	if w := call(h, "GET", "/addons/battery/api/v1/export.csv", user, ""); w.Code != 200 || !strings.HasPrefix(w.Body.String(), "interface,address,at") {
		t.Fatalf("csv: %d %s", w.Code, w.Body)
	}
	if w := call(h, "GET", "/addons/battery/api/v1/export.json", user, ""); w.Code != 200 || !json.Valid(w.Body.Bytes()) {
		t.Fatalf("json: %d %s", w.Code, w.Body)
	}
	// the table and the overrides
	if w := call(h, "GET", "/addons/battery/api/v1/models/hmip-wrc2", user, ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"in_table":true`) {
		t.Fatalf("model: %d %s", w.Code, w.Body)
	}
	if w := call(h, "GET", "/addons/battery/api/v1/models/NOPE", user, ""); w.Code != 404 {
		t.Fatalf("unknown model: %d", w.Code)
	}
	if w := call(h, "PUT", "/addons/battery/api/v1/models/HmIP-WRC2/override", user, `{"slots":[{"count":2,"type":"AA"}]}`, csrf...); w.Code != 200 {
		t.Fatalf("model override: %d %s", w.Code, w.Body)
	}
	if w := call(h, "PUT", "/addons/battery/api/v1/models/HmIP-WRC2/override", user, `{"power":"solar"}`, csrf...); w.Code != 422 {
		t.Fatalf("bad override: %d %s", w.Code, w.Body)
	}
	if w := call(h, "GET", "/addons/battery/api/v1/models", user, ""); w.Code != 200 {
		t.Fatalf("models: %d", w.Code)
	}
	if w := call(h, "DELETE", "/addons/battery/api/v1/models/HmIP-WRC2/override", user, "", csrf...); w.Code != 204 {
		t.Fatalf("model override delete: %d", w.Code)
	}
	if w := call(h, "PUT", "/addons/battery/api/v1/devices/HmIP-RF/WRC2/override", user, `{"rechargeable":true}`, csrf...); w.Code != 200 {
		t.Fatalf("device override: %d %s", w.Code, w.Body)
	}
	if w := call(h, "GET", "/addons/battery/api/v1/devices/HmIP-RF/WRC2/override", user, ""); !strings.Contains(w.Body.String(), `"rechargeable":true`) {
		t.Fatalf("device override get: %s", w.Body)
	}
	if w := call(h, "DELETE", "/addons/battery/api/v1/devices/HmIP-RF/WRC2/override", user, "", csrf...); w.Code != 204 {
		t.Fatalf("device override delete: %d", w.Code)
	}
	if w := call(h, "PUT", "/addons/battery/api/v1/devices/HmIP-RF/NOPE/override", user, `{}`, csrf...); w.Code != 422 {
		t.Fatalf("unknown device override: %d", w.Code)
	}
	if w := call(h, "GET", "/addons/battery/api/v1/settings", user, ""); !strings.Contains(w.Body.String(), `"window_hours":24`) {
		t.Fatalf("settings: %s", w.Body)
	}
	if w := call(h, "GET", "/addons/battery/api/v1/openapi.json", user, ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"openapi"`) {
		t.Fatalf("openapi: %d", w.Code)
	}
}

// The statistics and the price prefill over a few recorded changes.
func TestStatsAPI(t *testing.T) {
	h, st, _, _ := server(t)
	st.UpsertDevice(store.Device{Interface: "HmIP-RF", Address: "WRC2", Model: "HmIP-WRC2", VoltageDP: "0/OPERATING_VOLTAGE", LowBatDP: "0/LOW_BAT", FirstSeen: 1, LastSeen: 1})
	st.UpsertDevice(store.Device{Interface: "BidCos-RF", Address: "SC", Model: "HM-Sec-SC-2", LowBatDP: "0/LOWBAT", FirstSeen: 1, LastSeen: 1})
	st.Flush()
	csrf := []string{addonkit.HeaderRequest, "1"}
	w := call(h, "GET", "/addons/battery/api/v1/stats", user, "")
	var empty struct {
		Stats stats.Stats `json:"stats"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &empty) != nil || len(empty.Stats.Lives) != 0 || len(empty.Stats.Batteries) != 0 {
		t.Fatalf("empty stats: %d %s", w.Code, w.Body)
	}
	csv := "address,at,count,type,chemistry,rechargeable,vendor,product,price_cents,note,external_id\nSC,2025-05-01,1,LR44,,,Varta,Power on Demand,90,,r1\nSC,2025-09-01,1,LR44,,,Varta,Power on Demand,95,,r2\n"
	if w := call(h, "POST", "/addons/battery/api/v1/changes:bulk", user, csv, append([]string{"Content-Type", "text/csv"}, csrf...)...); w.Code != 200 || !strings.Contains(w.Body.String(), `"created":2`) {
		t.Fatalf("bulk: %d %s", w.Code, w.Body)
	}
	w = call(h, "GET", "/addons/battery/api/v1/stats?lang=de", user, "")
	var got struct {
		Stats stats.Stats                  `json:"stats"`
		Names map[string]map[string]string `json:"names"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &got) != nil {
		t.Fatalf("stats: %d %s", w.Code, w.Body)
	}
	sc := got.Stats.Devices["BidCos-RF.SC"]
	if sc.Count != 1 || sc.AvgDays != 123 || sc.RunningSince == 0 {
		t.Fatalf("SC life: %+v", sc)
	}
	if len(got.Stats.Lives) != 2 || len(got.Stats.Batteries) != 1 || got.Stats.Batteries[0].Key != "varta|power on demand|lr44" || got.Stats.Batteries[0].Total != 2 || got.Stats.Batteries[0].LastPriceCents != 95 {
		t.Fatalf("batteries: %+v", got.Stats.Batteries)
	}
	if len(got.Stats.Models) != 1 || got.Stats.Models[0].Model != "HM-Sec-SC-2" || len(got.Stats.Models[0].Batteries) != 1 {
		t.Fatalf("models: %+v", got.Stats.Models)
	}
	// the last price, matched without regard to case; none for an unknown vendor
	if w := call(h, "GET", "/addons/battery/api/v1/prices/last?vendor=VARTA&type=lr44", user, ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"price_cents":95`) {
		t.Fatalf("last price: %d %s", w.Code, w.Body)
	}
	if w := call(h, "GET", "/addons/battery/api/v1/prices/last?vendor=nobody", user, ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"found":false`) {
		t.Fatalf("no price: %d %s", w.Code, w.Body)
	}
	// products narrowed by the vendor
	if w := call(h, "GET", "/addons/battery/api/v1/suggestions?field=product&vendor=varta", user, ""); w.Code != 200 || !strings.Contains(w.Body.String(), "Power on Demand") {
		t.Fatalf("products: %d %s", w.Code, w.Body)
	}
	if w := call(h, "GET", "/addons/battery/api/v1/suggestions?field=product&vendor=duracell", user, ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"suggestions":[]`) {
		t.Fatalf("products of another vendor: %d %s", w.Code, w.Body)
	}
	// the picture lookup behind the devices view
	o := Options{Images: &devimg.Catalog{Path: filepath.Join(filepath.Dir(st.Path()), "..", "DEVDB.tcl")}}
	if o.image("hmip-wrc2") != "/config/img/devices/250/112_hmip-wrc2.png" || o.image("HM-Sec-SC-2") != "" || (Options{}).image("HmIP-WRC2") != "" {
		t.Fatal("image lookup")
	}
}

// The forecast, the shopping list, the settings and a test notice against a local sink.
func TestForecastAndSettingsAPI(t *testing.T) {
	h, _, _, addon := server(t)
	csrf := []string{addonkit.HeaderRequest, "1"}
	// no devices known to the recorder: empty lists, not errors
	if w := call(h, "GET", "/addons/battery/api/v1/forecast?days=30", user, ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"devices":[]`) {
		t.Fatalf("forecast: %d %s", w.Code, w.Body)
	}
	if w := call(h, "GET", "/addons/battery/api/v1/shopping", user, ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"items":[]`) || !strings.Contains(w.Body.String(), `"days":90`) {
		t.Fatalf("shopping: %d %s", w.Code, w.Body)
	}
	if w := call(h, "GET", "/addons/battery/api/v1/settings", user, ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"writable":true`) || !strings.Contains(w.Body.String(), `"channel":""`) {
		t.Fatalf("settings: %d %s", w.Code, w.Body)
	}
	// a local sink takes the notices
	var mu sync.Mutex
	var got []string
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b := make([]byte, 4096)
		n, _ := r.Body.Read(b)
		mu.Lock()
		got = append(got, r.URL.Path+" "+string(b[:n]))
		mu.Unlock()
	}))
	defer sink.Close()
	if w := call(h, "POST", "/addons/battery/api/v1/notify/test", user, "", csrf...); w.Code != 502 {
		t.Fatalf("test without a channel: %d %s", w.Code, w.Body)
	}
	body := `{"shopping":{"days":60,"stock":{"AA":4}},"notify":{"channel":"webhook","url":"` + sink.URL + `/hook","events":["low","inbox"],"language":"en"}}`
	if w := call(h, "PUT", "/addons/battery/api/v1/settings", user, body); w.Code != 403 {
		t.Fatalf("no CSRF: %d", w.Code)
	}
	w := call(h, "PUT", "/addons/battery/api/v1/settings", user, body, csrf...)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"days":60`) || !strings.Contains(w.Body.String(), `"channel":"webhook"`) {
		t.Fatalf("put settings: %d %s", w.Code, w.Body)
	}
	if b, err := os.ReadFile(filepath.Join(addon, "etc", "battery.json")); err != nil || !strings.Contains(string(b), `"shopping"`) || !strings.Contains(string(b), `"notify"`) {
		t.Fatalf("settings file: %s %v", b, err)
	}
	if w := call(h, "PUT", "/addons/battery/api/v1/settings", user, `{"notify":{"channel":"ntfy","url":"https://ntfy.sh"}}`, csrf...); w.Code != 422 {
		t.Fatalf("ntfy without a topic: %d %s", w.Code, w.Body)
	}
	if w := call(h, "PUT", "/addons/battery/api/v1/settings", user, `{"colour":"red"}`, csrf...); w.Code != 400 {
		t.Fatalf("unknown field: %d", w.Code)
	}
	if w := call(h, "POST", "/addons/battery/api/v1/notify/test", user, "", csrf...); w.Code != 200 {
		t.Fatalf("test notice: %d %s", w.Code, w.Body)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 || !strings.HasPrefix(got[0], "/hook ") || !strings.Contains(got[0], `"kind":"test"`) || !strings.Contains(got[0], "The Battery addon") {
		t.Fatalf("sink: %v", got)
	}
}
