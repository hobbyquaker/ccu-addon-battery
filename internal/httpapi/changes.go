package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/hobbyquaker/ccu-addon-battery/docs"
	"github.com/hobbyquaker/ccu-addon-battery/internal/batteries"
	"github.com/hobbyquaker/ccu-addon-battery/internal/changes"
	"github.com/hobbyquaker/ccu-addon-battery/internal/stats"
	"github.com/hobbyquaker/ccu-addon-battery/internal/store"
	"github.com/hobbyquaker/ccu-addon-kit/go/addonkit"
)

// maxBody caps a request body: a bulk import of several thousand rows fits, a mistake does not
// fill the RAM of a small system.
const maxBody = 16 << 20

func (o Options) routesChanges(api *http.ServeMux) {
	handle(api, "GET /api/v1/changes", o.listChanges)
	handle(api, "POST /api/v1/changes", o.createChange)
	handle(api, "POST /api/v1/changes:bulk", o.idempotent(o.bulkChanges))
	handle(api, "GET /api/v1/changes/{id}", o.getChange)
	handle(api, "PUT /api/v1/changes/{id}", o.patchChange)
	handle(api, "PATCH /api/v1/changes/{id}", o.patchChange)
	handle(api, "DELETE /api/v1/changes/{id}", o.deleteChange)
	handle(api, "PUT /api/v1/changes/by-external-id/{ext}", o.patchByExternal)
	handle(api, "PATCH /api/v1/changes/by-external-id/{ext}", o.patchByExternal)
	handle(api, "GET /api/v1/samples", o.listSamples)
	handle(api, "POST /api/v1/samples:bulk", o.idempotent(o.bulkSamples))
	handle(api, "POST /api/v1/detect", o.detect)
	handle(api, "GET /api/v1/suggestions", o.suggestions)
	handle(api, "GET /api/v1/prices/last", o.lastPrice)
	handle(api, "GET /api/v1/stats", o.stats)
	handle(api, "GET /api/v1/export.json", o.exportJSON)
	handle(api, "GET /api/v1/export.csv", o.exportCSV)
	handle(api, "GET /api/v1/models", o.models)
	handle(api, "GET /api/v1/models/{model}", o.model)
	handle(api, "PUT /api/v1/models/{model}/override", o.putModelOverride)
	handle(api, "DELETE /api/v1/models/{model}/override", o.deleteModelOverride)
	handle(api, "GET /api/v1/devices/{iface}/{address}/override", o.getDeviceOverride)
	handle(api, "PUT /api/v1/devices/{iface}/{address}/override", o.putDeviceOverride)
	handle(api, "DELETE /api/v1/devices/{iface}/{address}/override", o.deleteDeviceOverride)
	handle(api, "GET /api/v1/settings", o.settings)
	handle(api, "GET /api/v1/openapi.json", o.openapi)
}

func (o Options) openapi(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Write(docs.OpenAPI)
}

// writeErr maps an error onto its status: a client error with its code, the store away or
// busy as 503, anything else 500.
func writeErr(w http.ResponseWriter, err error) {
	var ce *changes.Error
	switch {
	case errors.As(err, &ce):
		status := http.StatusUnprocessableEntity
		if ce.Code == "not-found" {
			status = http.StatusNotFound
		}
		addonkit.WriteError(w, status, ce.Code, ce.Msg)
	case errors.Is(err, store.ErrUnavailable):
		addonkit.WriteError(w, http.StatusServiceUnavailable, "unavailable", err.Error())
	case errors.Is(err, store.ErrBusy):
		w.Header().Set("Retry-After", "10")
		addonkit.WriteError(w, http.StatusServiceUnavailable, "busy", err.Error())
	default:
		addonkit.WriteError(w, http.StatusInternalServerError, "internal", err.Error())
	}
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		addonkit.WriteError(w, http.StatusBadRequest, "invalid", "the body is not valid JSON for this route: "+err.Error())
		return false
	}
	return true
}

func userOf(r *http.Request) string {
	if s := addonkit.FromRequest(r); s != nil && s.User != "" {
		return s.User
	}
	return "api"
}

func flag(r *http.Request, name string) bool {
	switch strings.ToLower(r.URL.Query().Get(name)) {
	case "1", "true", "yes":
		return true
	}
	return false
}

func timeParam(r *http.Request, name string) (int64, error) {
	v := r.URL.Query().Get(name)
	if v == "" {
		return 0, nil
	}
	t, err := changes.ParseTime(v)
	if err != nil {
		return 0, &changes.Error{Code: "invalid", Msg: name + ": " + err.Error()}
	}
	return t, nil
}

// named is a change with the device's name and model, as the inbox shows it.
type named struct {
	changes.Change
	Name  string `json:"name"`
	Model string `json:"model"`
}

func (o Options) names(lang string) map[string][2]string {
	out := map[string][2]string{}
	for _, d := range o.Recorder.Devices(lang) {
		out[d.Interface+"."+d.Address] = [2]string{d.Name, d.Model}
	}
	return out
}

func (o Options) listChanges(w http.ResponseWriter, r *http.Request) {
	from, err := timeParam(r, "from")
	if err != nil {
		writeErr(w, err)
		return
	}
	to, err := timeParam(r, "to")
	if err != nil {
		writeErr(w, err)
		return
	}
	f := changes.Filter{Address: r.URL.Query().Get("device"), From: from, To: to}
	if st := r.URL.Query().Get("status"); st != "" {
		for _, s := range strings.Split(st, ",") {
			s = strings.TrimSpace(s)
			if !inList(changes.Statuses, s) {
				addonkit.WriteError(w, http.StatusUnprocessableEntity, "invalid", "status is one of "+strings.Join(changes.Statuses, ", "))
				return
			}
			f.Statuses = append(f.Statuses, s)
		}
	}
	if l := r.URL.Query().Get("limit"); l != "" {
		f.Limit, _ = strconv.Atoi(l)
	}
	list, err := o.Changes.List(r.Context(), f)
	if err != nil {
		writeErr(w, err)
		return
	}
	nm := o.names(lang(r))
	out := make([]named, len(list))
	for i, c := range list {
		n := nm[c.Interface+"."+c.Address]
		out[i] = named{Change: c, Name: n[0], Model: n[1]}
	}
	writeJSON(w, 200, map[string]any{"changes": out})
}

func inList(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

func (o Options) createChange(w http.ResponseWriter, r *http.Request) {
	var in changes.Input
	if !decode(w, r, &in) {
		return
	}
	out, err := o.Changes.Create(r.Context(), in, userOf(r), flag(r, "dry_run"))
	if err != nil {
		writeErr(w, err)
		return
	}
	status := http.StatusCreated
	if out.Status != "created" || flag(r, "dry_run") {
		status = http.StatusOK
	}
	writeJSON(w, status, out)
}

func (o Options) bulkChanges(w http.ResponseWriter, r *http.Request) {
	ct, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	var ins []changes.Input
	var rows []int
	var parseErrs []changes.Outcome
	body := http.MaxBytesReader(w, r.Body, maxBody)
	if ct == "text/csv" {
		var err error
		ins, rows, parseErrs, err = changes.ParseCSV(body)
		if err != nil {
			writeErr(w, err)
			return
		}
	} else {
		dec := json.NewDecoder(body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&ins); err != nil {
			addonkit.WriteError(w, http.StatusBadRequest, "invalid", "the body is a JSON array of changes or text/csv: "+err.Error())
			return
		}
		for i := range ins {
			if ins[i].Source == "" {
				ins[i].Source = changes.KindImport
			}
		}
	}
	res, err := o.Changes.Bulk(r.Context(), ins, rows, userOf(r), flag(r, "dry_run"), flag(r, "atomic"))
	if err != nil {
		writeErr(w, err)
		return
	}
	if len(parseErrs) > 0 {
		res.Rows = append(res.Rows, parseErrs...)
		sort.SliceStable(res.Rows, func(i, j int) bool { return res.Rows[i].Row < res.Rows[j].Row })
		res.Counts["error"] += len(parseErrs)
		if flag(r, "atomic") {
			res.Applied = false
		}
	}
	if res.Rows == nil {
		res.Rows = []changes.Outcome{}
	}
	writeJSON(w, 200, res)
}

func idOf(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		addonkit.WriteError(w, http.StatusNotFound, "not-found", "no such change")
		return 0, false
	}
	return id, true
}

func (o Options) getChange(w http.ResponseWriter, r *http.Request) {
	id, ok := idOf(w, r)
	if !ok {
		return
	}
	c, err := o.Changes.Get(r.Context(), id)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, c)
}

func (o Options) patchChange(w http.ResponseWriter, r *http.Request) {
	id, ok := idOf(w, r)
	if !ok {
		return
	}
	var p changes.Patch
	if !decode(w, r, &p) {
		return
	}
	c, err := o.Changes.Update(r.Context(), id, p)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, c)
}

func (o Options) patchByExternal(w http.ResponseWriter, r *http.Request) {
	var p changes.Patch
	if !decode(w, r, &p) {
		return
	}
	c, err := o.Changes.UpdateByExternalID(r.Context(), r.URL.Query().Get("source"), r.PathValue("ext"), p)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, c)
}

func (o Options) deleteChange(w http.ResponseWriter, r *http.Request) {
	id, ok := idOf(w, r)
	if !ok {
		return
	}
	if err := o.Changes.Delete(r.Context(), id); err != nil {
		writeErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type sampleOut struct {
	TS      int64    `json:"ts"`
	Voltage *float64 `json:"voltage"`
	LowBat  *bool    `json:"low_bat"`
	Source  string   `json:"source"`
}

func (o Options) listSamples(w http.ResponseWriter, r *http.Request) {
	addr := r.URL.Query().Get("device")
	if addr == "" {
		addonkit.WriteError(w, http.StatusUnprocessableEntity, "invalid", "device is required")
		return
	}
	from, err := timeParam(r, "from")
	if err != nil {
		writeErr(w, err)
		return
	}
	to, err := timeParam(r, "to")
	if err != nil {
		writeErr(w, err)
		return
	}
	iface := r.URL.Query().Get("interface")
	if iface == "" {
		for _, d := range o.Recorder.Devices("en") {
			if d.Address == addr {
				iface = d.Interface
			}
		}
	}
	list, err := o.Store.Samples(r.Context(), iface, addr, from, to)
	if err != nil {
		writeErr(w, err)
		return
	}
	out := make([]sampleOut, len(list))
	for i, s := range list {
		out[i] = sampleOut{TS: s.TS, Voltage: s.Voltage, LowBat: s.LowBat, Source: s.Source}
	}
	writeJSON(w, 200, map[string]any{"interface": iface, "address": addr, "samples": out})
}

func (o Options) bulkSamples(w http.ResponseWriter, r *http.Request) {
	var in []changes.SampleInput
	if !decode(w, r, &in) {
		return
	}
	res, err := o.Changes.ImportSamples(r.Context(), in, flag(r, "dry_run"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, res)
}

func (o Options) detect(w http.ResponseWriter, r *http.Request) {
	from, err := timeParam(r, "from")
	if err != nil {
		writeErr(w, err)
		return
	}
	to, err := timeParam(r, "to")
	if err != nil {
		writeErr(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
	defer cancel()
	res, err := o.Changes.Detect(ctx, r.URL.Query().Get("device"), from, to, flag(r, "dry_run"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, res)
}

func (o Options) suggestions(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	s, err := o.Changes.Suggest(r.Context(), r.URL.Query().Get("field"), r.URL.Query().Get("q"), limit, r.URL.Query().Get("vendor"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"suggestions": s})
}

// lastPrice answers the price per battery recorded most recently for vendor, product and type
// (the change editor's prefill).
func (o Options) lastPrice(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	cents, at, found, err := o.Changes.LastPrice(r.Context(), q.Get("vendor"), q.Get("product"), q.Get("type"))
	if err != nil {
		writeErr(w, err)
		return
	}
	if !found {
		writeJSON(w, 200, map[string]any{"found": false})
		return
	}
	writeJSON(w, 200, map[string]any{"found": true, "price_cents": cents, "at": at})
}

// stats answers the lives and the statistics over the accepted changes (internal/stats), with
// the devices' names in lang.
func (o Options) stats(w http.ResponseWriter, r *http.Request) {
	st, err := o.computeStats(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	names := map[string]map[string]string{}
	for _, d := range o.Recorder.Devices(lang(r)) {
		names[d.Interface+"."+d.Address] = map[string]string{"name": d.Name, "model": d.Model, "image": o.image(d.Model)}
	}
	writeJSON(w, 200, map[string]any{"stats": st, "names": names})
}

func (o Options) computeStats(r *http.Request) (*stats.Stats, error) {
	db, err := o.Store.DB()
	if err != nil {
		return nil, err
	}
	cs, devs, err := stats.Load(r.Context(), db)
	if err != nil {
		return nil, err
	}
	low := map[string]bool{}
	for _, d := range o.Recorder.Devices("en") {
		if d.LowBat != nil && *d.LowBat {
			low[d.Interface+"."+d.Address] = true
		}
	}
	st := stats.Compute(cs, devs, stats.Options{LowBat: func(i, a string) bool { return low[i+"."+a] }})
	return &st, nil
}

func (o Options) exportJSON(w http.ResponseWriter, r *http.Request) {
	if _, err := o.Store.DB(); err != nil {
		writeErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="battery-export.json"`)
	if err := o.Changes.ExportJSON(r.Context(), w, o.Version); err != nil {
		// the head is out already; the truncated document does not parse, which says enough
		return
	}
}

func (o Options) exportCSV(w http.ResponseWriter, r *http.Request) {
	var buf bytes.Buffer
	if err := o.Changes.ExportCSV(r.Context(), &buf); err != nil {
		writeErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="battery-changes.csv"`)
	w.Write(buf.Bytes())
}

// modelInfo is a model of the installation with what the table says.
type modelInfo struct {
	Model    string              `json:"model"`
	Devices  int                 `json:"devices"`
	InTable  bool                `json:"in_table"`
	Row      *batteries.Model    `json:"row,omitempty"`
	Override *batteries.Override `json:"override,omitempty"`
	Profile  batteries.Profile   `json:"profile"`
}

// models lists the models the installation has, with their table rows; ?unknown=true only the
// ones the table lacks (the list behind a "please add" issue).
func (o Options) models(w http.ResponseWriter, r *http.Request) {
	ov := o.Changes.ModelOverrides(r.Context())
	by := map[string]*modelInfo{}
	for _, d := range o.Recorder.Devices("en") {
		m := by[strings.ToLower(d.Model)]
		if m == nil {
			m = &modelInfo{Model: d.Model, Profile: d.Profile}
			if row, _, ok := o.Changes.Table().Lookup(d.Model); ok {
				m.InTable, m.Row = true, &row
			}
			if x, ok := ov[strings.ToLower(d.Model)]; ok {
				m.Override = &x
			}
			by[strings.ToLower(d.Model)] = m
		}
		m.Devices++
	}
	out := []modelInfo{}
	for _, m := range by {
		if flag(r, "unknown") && m.InTable {
			continue
		}
		out = append(out, *m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Model < out[j].Model })
	writeJSON(w, 200, map[string]any{"table": map[string]any{"version": o.Changes.Table().Version, "updated": o.Changes.Table().Updated, "models": len(o.Changes.Table().Models)}, "models": out})
}

func (o Options) model(w http.ResponseWriter, r *http.Request) {
	row, key, ok := o.Changes.Table().Lookup(r.PathValue("model"))
	ov := o.Changes.ModelOverrides(r.Context())
	x, hasOv := ov[strings.ToLower(r.PathValue("model"))]
	if !ok && !hasOv {
		addonkit.WriteError(w, http.StatusNotFound, "not-found", "the battery table has no model "+r.PathValue("model"))
		return
	}
	out := map[string]any{"model": key, "in_table": ok, "profile": o.Changes.Table().Resolve(r.PathValue("model"), ovPtr(x, hasOv), nil, 0)}
	if ok {
		out["row"] = row
	}
	if hasOv {
		out["override"] = x
	}
	writeJSON(w, 200, out)
}

func ovPtr(o batteries.Override, ok bool) *batteries.Override {
	if !ok {
		return nil
	}
	return &o
}

func (o Options) putModelOverride(w http.ResponseWriter, r *http.Request) {
	var ov batteries.Override
	if !decode(w, r, &ov) {
		return
	}
	if err := o.Changes.SetModelOverride(r.Context(), r.PathValue("model"), &ov); err != nil {
		writeErr(w, err)
		return
	}
	o.Recorder.Reprofile(r.Context())
	writeJSON(w, 200, map[string]any{"ok": true, "override": ov})
}

func (o Options) deleteModelOverride(w http.ResponseWriter, r *http.Request) {
	if err := o.Changes.SetModelOverride(r.Context(), r.PathValue("model"), nil); err != nil {
		writeErr(w, err)
		return
	}
	o.Recorder.Reprofile(r.Context())
	w.WriteHeader(http.StatusNoContent)
}

func (o Options) getDeviceOverride(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"override": o.Changes.DeviceOverride(r.Context(), r.PathValue("iface"), r.PathValue("address"))})
}

func (o Options) putDeviceOverride(w http.ResponseWriter, r *http.Request) {
	var ov batteries.Override
	if !decode(w, r, &ov) {
		return
	}
	if err := o.Changes.SetDeviceOverride(r.Context(), r.PathValue("iface"), r.PathValue("address"), &ov); err != nil {
		writeErr(w, err)
		return
	}
	o.Recorder.Reprofile(r.Context())
	writeJSON(w, 200, map[string]any{"ok": true, "override": ov})
}

func (o Options) deleteDeviceOverride(w http.ResponseWriter, r *http.Request) {
	if err := o.Changes.SetDeviceOverride(r.Context(), r.PathValue("iface"), r.PathValue("address"), nil); err != nil {
		writeErr(w, err)
		return
	}
	o.Recorder.Reprofile(r.Context())
	w.WriteHeader(http.StatusNoContent)
}

func (o Options) settings(w http.ResponseWriter, r *http.Request) {
	p, merge := o.Changes.Params()
	writeJSON(w, 200, map[string]any{
		"detection": map[string]any{
			"window_hours":       p.Window.Hours(),
			"debounce_hours":     p.Debounce.Hours(),
			"expire_hours":       p.Expire.Hours(),
			"silence_hours":      p.Silence.Hours(),
			"merge_window_hours": merge.Hours(),
		},
	})
}

// idempotent replays the stored answer of a request with the same Idempotency-Key (and the
// same body) for 24 h, so a retried bulk import does not run twice; a different body under the
// same key is a 409.
func (o Options) idempotent(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		k := r.Header.Get("Idempotency-Key")
		if k == "" || flag(r, "dry_run") {
			next(w, r)
			return
		}
		if len(k) > 200 {
			addonkit.WriteError(w, http.StatusBadRequest, "invalid", "Idempotency-Key is longer than 200 characters")
			return
		}
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBody))
		if err != nil {
			addonkit.WriteError(w, http.StatusRequestEntityTooLarge, "too-large", err.Error())
			return
		}
		sum := sha256.Sum256(append([]byte(r.URL.Path+"?"+r.URL.RawQuery+"\n"), body...))
		fp := hex.EncodeToString(sum[:])
		db, err := o.Store.DB()
		if err != nil {
			writeErr(w, err)
			return
		}
		var status int
		var stored string
		if err := db.QueryRowContext(r.Context(), `SELECT status, body FROM idempotency WHERE key = ? AND created_at >= ?`, k, time.Now().UnixMilli()-24*3600*1000).Scan(&status, &stored); err == nil {
			var rec struct {
				FP   string          `json:"fp"`
				Body json.RawMessage `json:"body"`
			}
			if json.Unmarshal([]byte(stored), &rec) != nil || rec.FP != fp {
				addonkit.WriteError(w, http.StatusConflict, "idempotency-key-reused", "this Idempotency-Key was used for another request")
				return
			}
			w.Header().Set("Idempotent-Replayed", "true")
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(status)
			w.Write(rec.Body)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		rec := httptest.NewRecorder()
		next(rec, r)
		for k, v := range rec.Header() {
			w.Header()[k] = v
		}
		w.WriteHeader(rec.Code)
		w.Write(rec.Body.Bytes())
		if rec.Code < 300 {
			doc, _ := json.Marshal(map[string]any{"fp": fp, "body": json.RawMessage(bytes.TrimSpace(rec.Body.Bytes()))})
			now := time.Now().UnixMilli()
			_ = o.Store.Write(r.Context(), func(tx *sql.Tx) error {
				if _, err := tx.Exec(`DELETE FROM idempotency WHERE created_at < ?`, now-24*3600*1000); err != nil {
					return err
				}
				_, err := tx.Exec(`INSERT OR REPLACE INTO idempotency (key, created_at, status, body) VALUES (?, ?, ?, ?)`, k, now, rec.Code, string(doc))
				return err
			})
		}
	}
}
