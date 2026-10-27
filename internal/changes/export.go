package changes

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"io"
	"strconv"
	"time"
)

// ExportCSV writes the accepted changes in the bulk import's CSV format (one row per slot, a
// change without batteries as one row), so the file goes back in through POST changes:bulk.
func (s *Service) ExportCSV(ctx context.Context, w io.Writer) error {
	list, err := s.List(ctx, Filter{Statuses: []string{StatusAccepted}})
	if err != nil {
		return err
	}
	cw := csv.NewWriter(w)
	cw.Write(append([]string{"interface"}, CSVHeader...))
	for i := len(list) - 1; i >= 0; i-- {
		c := list[i]
		at := time.UnixMilli(c.At).UTC().Format(time.RFC3339)
		ext := c.ExternalID
		if ext == "" {
			ext = "battery-" + strconv.FormatInt(c.ID, 10)
		}
		bats := c.Batteries
		if len(bats) == 0 {
			bats = []Battery{{}}
		}
		for j, b := range bats {
			note := ""
			if j == 0 {
				note = c.Note
			}
			price := ""
			if b.PriceCents != nil {
				price = strconv.Itoa(*b.PriceCents)
			}
			count := ""
			if b.Count > 0 {
				count = strconv.Itoa(b.Count)
			}
			rech := ""
			if b.Rechargeable {
				rech = "true"
			}
			cw.Write([]string{c.Interface, c.Address, at, count, b.Type, b.Chemistry, rech, b.Vendor, b.Product, price, note, ext})
		}
	}
	cw.Flush()
	return cw.Error()
}

// ExportJSON streams everything the addon keeps: devices (with their overrides), slots, model
// overrides, changes, events and samples. Rows are written as they are read, so a long sample
// table does not have to fit in memory.
func (s *Service) ExportJSON(ctx context.Context, w io.Writer, version string) error {
	db, err := s.o.Store.DB()
	if err != nil {
		return err
	}
	bw := bufio.NewWriterSize(w, 64<<10)
	defer bw.Flush()
	head, _ := json.Marshal(map[string]any{"format": "ccu-addon-battery-export", "format_version": 1, "version": version, "exported_at": s.o.Now().UTC().Format(time.RFC3339)})
	bw.Write(head[:len(head)-1])
	tables := []struct{ name, q string }{
		{"devices", `SELECT interface, address, model, firmware, power, voltage_dp, lowbat_dp, first_seen, last_seen, retired_at, profile_override FROM devices ORDER BY interface, address`},
		{"slots", `SELECT interface, address, slot, count, type, chemistry, rechargeable FROM slots ORDER BY interface, address, slot`},
		{"model_overrides", `SELECT model, value, updated_at FROM model_overrides ORDER BY model`},
		{"changes", `SELECT id, interface, address, at, kind, confidence, rule, status, evidence, batteries, note, source, external_id, created_by, created_at, updated_at FROM changes ORDER BY at, id`},
		{"events", `SELECT interface, address, ts, key, value FROM events ORDER BY ts, id`},
		{"samples", `SELECT interface, address, ts, voltage, low_bat, source FROM samples ORDER BY interface, address, ts`},
	}
	jsonCols := map[string]bool{"profile_override": true, "value": false, "evidence": true, "batteries": true}
	for _, t := range tables {
		if t.name == "model_overrides" {
			jsonCols["value"] = true
		} else {
			jsonCols["value"] = false
		}
		bw.WriteString(`,"` + t.name + `":[`)
		if err := dumpRows(ctx, db, t.q, jsonCols, bw); err != nil {
			return err
		}
		bw.WriteString("]")
	}
	bw.WriteString("}\n")
	return nil
}

func dumpRows(ctx context.Context, db *sql.DB, q string, jsonCols map[string]bool, w *bufio.Writer) error {
	rows, err := db.QueryContext(ctx, q)
	if err != nil {
		return err
	}
	defer rows.Close()
	cols, _ := rows.Columns()
	vals := make([]any, len(cols))
	ptrs := make([]any, len(cols))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	first := true
	for rows.Next() {
		if err := rows.Scan(ptrs...); err != nil {
			return err
		}
		m := make(map[string]any, len(cols))
		for i, c := range cols {
			v := vals[i]
			if b, ok := v.([]byte); ok {
				v = string(b)
			}
			if s, ok := v.(string); ok && jsonCols[c] && s != "" {
				v = json.RawMessage(s)
			}
			m[c] = v
		}
		b, err := json.Marshal(m)
		if err != nil {
			return err
		}
		if !first {
			w.WriteByte(',')
		}
		first = false
		w.Write(b)
	}
	return rows.Err()
}
