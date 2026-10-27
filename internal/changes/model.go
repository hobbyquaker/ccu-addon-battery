// Package changes keeps the battery changes: what the detector offers (live, and over stored
// samples), what the user confirms, corrects or records by hand, and what a script imports
// through the backfill API, with the dedup and merge rules that keep one change one row. It runs
// the live detector for every device and holds the battery profiles (the table plus the
// per-model and per-device overrides). docs/api.md and docs/detection.md describe it.
package changes

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Battery is what went into one slot.
type Battery struct {
	Slot         int    `json:"slot,omitempty"`
	Count        int    `json:"count,omitempty"`
	Type         string `json:"type,omitempty"`
	Chemistry    string `json:"chemistry,omitempty"`
	Rechargeable bool   `json:"rechargeable,omitempty"`
	Vendor       string `json:"vendor,omitempty"`
	Product      string `json:"product,omitempty"`
	PriceCents   *int   `json:"price_cents,omitempty"`
	Lot          string `json:"lot,omitempty"`
}

// Change is one row of changes as the API shows it.
type Change struct {
	ID         int64           `json:"id"`
	Interface  string          `json:"interface"`
	Address    string          `json:"address"`
	At         int64           `json:"at"`
	Kind       string          `json:"kind"`
	Confidence string          `json:"confidence,omitempty"`
	Rule       string          `json:"rule,omitempty"`
	Status     string          `json:"status"`
	Evidence   json.RawMessage `json:"evidence,omitempty"`
	Batteries  []Battery       `json:"batteries"`
	Note       string          `json:"note"`
	Source     string          `json:"source"`
	ExternalID string          `json:"external_id,omitempty"`
	CreatedBy  string          `json:"created_by,omitempty"`
	CreatedAt  int64           `json:"created_at"`
	UpdatedAt  int64           `json:"updated_at"`
}

// Kinds and statuses.
const (
	KindDetected  = "detected"  // the detector found it
	KindConfirmed = "confirmed" // detected, and a manual or imported entry said the same
	KindManual    = "manual"    // the user recorded it
	KindImport    = "import"    // a script imported it

	StatusCandidate = "candidate" // detected, waiting for the confirmation window; not in the inbox
	StatusPending   = "pending"   // in the inbox
	StatusAccepted  = "accepted"
	StatusRejected  = "rejected"
	StatusDiscarded = "discarded" // the detector's own no: a boot or a recovery
)

// Statuses lists every status.
var Statuses = []string{StatusCandidate, StatusPending, StatusAccepted, StatusRejected, StatusDiscarded}

// live statuses: a change that counts for the dedup and the merge
var liveStatuses = []string{StatusCandidate, StatusPending, StatusAccepted}

// Time is a point in time on the API: Unix milliseconds, an RFC 3339 string, or a date
// (YYYY-MM-DD, noon in the system's time zone).
type Time int64

// UnmarshalJSON takes a number or a string.
func (t *Time) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		*t = 0
		return nil
	}
	if len(b) > 0 && b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		v, err := ParseTime(s)
		*t = Time(v)
		return err
	}
	var n float64
	if err := json.Unmarshal(b, &n); err != nil {
		return fmt.Errorf("a time is Unix milliseconds or an RFC 3339 string")
	}
	*t = Time(int64(n))
	return nil
}

// ParseTime reads Unix milliseconds, RFC 3339 or a date.
func ParseTime(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, errors.New("empty time")
	}
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		return n, nil
	}
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05", "2006-01-02 15:04:05", "2006-01-02T15:04", "2006-01-02 15:04"} {
		loc := time.Local
		if t, err := time.ParseInLocation(layout, s, loc); err == nil {
			return t.UnixMilli(), nil
		}
	}
	if t, err := time.ParseInLocation("2006-01-02", s, time.Local); err == nil {
		return t.Add(12 * time.Hour).UnixMilli(), nil
	}
	return 0, fmt.Errorf("%q is not a time (Unix ms, RFC 3339 or YYYY-MM-DD)", s)
}

// Input is a change as a client sends it.
type Input struct {
	// Device is the address (Address is the same; one of them).
	Device    string `json:"device,omitempty"`
	Address   string `json:"address,omitempty"`
	Interface string `json:"interface,omitempty"`
	// Model names the model of a device the addon does not know (one that left the system
	// before the install); with Interface it creates the device as retired.
	Model      string    `json:"model,omitempty"`
	At         Time      `json:"at"`
	Batteries  []Battery `json:"batteries,omitempty"`
	Note       string    `json:"note,omitempty"`
	Source     string    `json:"source,omitempty"` // manual (default) | import
	ExternalID string    `json:"external_id,omitempty"`
}

func (in Input) address() string {
	if in.Address != "" {
		return strings.TrimSpace(in.Address)
	}
	return strings.TrimSpace(in.Device)
}

// Patch edits a change; nil fields stay as they are.
type Patch struct {
	At        *Time      `json:"at,omitempty"`
	Batteries *[]Battery `json:"batteries,omitempty"`
	Note      *string    `json:"note,omitempty"`
	// Status: accepted (confirm), rejected, or pending (back into the inbox).
	Status *string `json:"status,omitempty"`
}

// Outcome is the answer to one created or imported change.
type Outcome struct {
	Row        int    `json:"row,omitempty"`
	Status     string `json:"status"` // created | merged | duplicate | error
	ID         int64  `json:"id,omitempty"`
	MergedInto int64  `json:"merged_into,omitempty"`
	Error      string `json:"error,omitempty"`
	Code       string `json:"code,omitempty"`
}

// Error is a client error with a code for the API.
type Error struct {
	Code string
	Msg  string
}

func (e *Error) Error() string { return e.Msg }

func bad(code, format string, a ...any) error {
	return &Error{Code: code, Msg: fmt.Sprintf(format, a...)}
}

// ErrNotFound is answered for an unknown change.
var ErrNotFound = &Error{Code: "not-found", Msg: "no such change"}

const changeCols = `id, interface, address, at, kind, confidence, rule, status, COALESCE(evidence, ''), COALESCE(batteries, '[]'), note, source, COALESCE(external_id, ''), created_by, created_at, updated_at`

type scanner interface{ Scan(...any) error }

func scanChange(r scanner) (Change, error) {
	var c Change
	var ev, bat string
	if err := r.Scan(&c.ID, &c.Interface, &c.Address, &c.At, &c.Kind, &c.Confidence, &c.Rule, &c.Status, &ev, &bat, &c.Note, &c.Source, &c.ExternalID, &c.CreatedBy, &c.CreatedAt, &c.UpdatedAt); err != nil {
		return c, err
	}
	if ev != "" {
		c.Evidence = json.RawMessage(ev)
	}
	c.Batteries = []Battery{}
	_ = json.Unmarshal([]byte(bat), &c.Batteries)
	if c.Batteries == nil {
		c.Batteries = []Battery{}
	}
	return c, nil
}

type querier interface {
	QueryRow(query string, args ...any) *sql.Row
	Query(query string, args ...any) (*sql.Rows, error)
}

func getChange(q querier, id int64) (Change, error) {
	c, err := scanChange(q.QueryRow(`SELECT `+changeCols+` FROM changes WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return c, ErrNotFound
	}
	return c, err
}

func batteriesJSON(b []Battery) string {
	if b == nil {
		b = []Battery{}
	}
	out, _ := json.Marshal(b)
	return string(out)
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func inList(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

func placeholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?, ", n), ", ")
}

func anys(l []string) []any {
	out := make([]any, len(l))
	for i, s := range l {
		out[i] = s
	}
	return out
}
