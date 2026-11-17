package snapshot

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hobbyquaker/ccu-addon-battery/internal/store"
	"github.com/hobbyquaker/ccu-addon-kit/go/addonkit/backup"
)

func quiet(string, ...any) {}

func noMounts() []store.Mount { return nil }

// fill opens the store at addonDir, writes one device and closes it.
func fill(t *testing.T, addonDir string, mounts func() []store.Mount, model string) *store.Store {
	t.Helper()
	st, err := store.Open(store.Config{AddonDir: addonDir, Log: quiet, Mounts: mounts})
	if err != nil {
		t.Fatal(err)
	}
	st.UpsertDevice(store.Device{Interface: "HmIP-RF", Address: "000A", Model: model, Power: "battery"})
	if err := st.Flush(); err != nil {
		t.Fatal(err)
	}
	return st
}

func model(t *testing.T, path string) string {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var m string
	if err := db.QueryRow("SELECT model FROM devices WHERE address = '000A'").Scan(&m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestHookAndRestore(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	// nothing yet: no snapshot, no error
	if msg, err := Hook(ctx, dir, noMounts); err != nil || !strings.Contains(msg, "no database yet") {
		t.Fatal(msg, err)
	}
	st := fill(t, dir, noMounts, "HmIP-SWDO")
	defer st.Close()
	// the hook runs while the daemon has the file open (WAL)
	msg, err := Hook(ctx, dir, noMounts)
	if err != nil {
		t.Fatal(err)
	}
	s := For(dir)
	if !strings.Contains(msg, "snapshot of") || model(t, s.Path()) != "HmIP-SWDO" || !s.Tagged() {
		t.Fatalf("%s, tagged %v", msg, s.Tagged())
	}
	if _, by, ok := s.Own(); !ok || by != backup.ByHook {
		t.Fatal("the hook's own snapshot")
	}
	// the restore program leaves the system's own snapshot alone (a backup may be taking it)
	if r, err := Restore(dir, noMounts, true, quiet); r != backup.Mine || err != nil {
		t.Fatal(r, err)
	}
	st.Close()

	// a restore onto a new system: var/ was not in the backup, the staging was
	fresh := t.TempDir()
	os.MkdirAll(filepath.Join(fresh, Staging), 0o750)
	copyFile(t, s.Path(), filepath.Join(fresh, Staging, store.FileName))
	if r, err := Restore(fresh, noMounts, true, quiet); r != backup.Restored || err != nil {
		t.Fatal(r, err)
	}
	if model(t, filepath.Join(fresh, "var", store.FileName)) != "HmIP-SWDO" {
		t.Fatal("restored")
	}
	if _, err := os.Stat(filepath.Join(fresh, Staging, store.FileName)); err == nil {
		t.Fatal("the restore program empties the staging")
	}
}

func TestRestoreNewerLiveAndDaemon(t *testing.T) {
	dir := t.TempDir()
	s := For(dir)
	os.MkdirAll(s.Staging, 0o750)
	os.WriteFile(s.Path(), []byte("old snapshot"), 0o600)
	old := time.Now().Add(-48 * time.Hour)
	os.Chtimes(s.Path(), old, old)
	st := fill(t, dir, noMounts, "HmIP-WTH-2")
	st.Close()
	// the daemon's own call keeps the snapshot (the daily copy elsewhere); the live file is newer
	if r, err := Restore(dir, noMounts, false, quiet); r != backup.Kept || err != nil {
		t.Fatal(r, err)
	}
	if _, err := os.Stat(s.Path()); err != nil {
		t.Fatal("the daemon keeps the snapshot")
	}
	if model(t, filepath.Join(dir, "var", store.FileName)) != "HmIP-WTH-2" {
		t.Fatal("the live file stays")
	}
}

func TestRestoreStickMissing(t *testing.T) {
	ctx := context.Background()
	dir, stick := t.TempDir(), t.TempDir()
	mounts := func() []store.Mount { return []store.Mount{{Path: stick, Label: "STICK"}} }
	if err := store.SaveLocation(settingsPath(dir), store.Location{ID: "usb:STICK", Folder: "battery"}); err != nil {
		t.Fatal(err)
	}
	st := fill(t, dir, mounts, "HM-Sec-SC-2")
	if _, err := Hook(ctx, dir, mounts); err != nil {
		t.Fatal(err)
	}
	st.Close()
	// a restore onto a system without the stick: the snapshot comes back, the marker does not
	fresh := t.TempDir()
	os.MkdirAll(filepath.Join(fresh, Staging), 0o750)
	os.MkdirAll(filepath.Join(fresh, "etc"), 0o750)
	copyFile(t, For(dir).Path(), filepath.Join(fresh, Staging, store.FileName))
	copyFile(t, settingsPath(dir), settingsPath(fresh))
	if r, err := Restore(fresh, noMounts, true, quiet); r != backup.Restored || err != nil {
		t.Fatal(r, err)
	}
	loc, _ := store.LoadLocation(settingsPath(fresh))
	if loc.ID != "userfs" || model(t, filepath.Join(fresh, "var", store.FileName)) != "HM-Sec-SC-2" {
		t.Fatalf("the database is on the user partition now: %+v", loc)
	}
	var n Notice
	b, _ := os.ReadFile(noticePath(fresh))
	if json.Unmarshal(b, &n) != nil || n.Label != "STICK" {
		t.Fatalf("notice %s", b)
	}
	r := &Runner{AddonDir: fresh, Log: quiet}
	r.Init()
	if st := r.Status(); st.Notice == nil || st.Notice.Label != "STICK" {
		t.Fatalf("status %+v", st)
	}
}

func TestRunnerModes(t *testing.T) {
	ctx := context.Background()
	t.Run("daily", func(t *testing.T) {
		t.Setenv("INVOCATION_ID", "")
		dir := t.TempDir()
		st := fill(t, dir, noMounts, "HmIP-SMI")
		defer st.Close()
		r := &Runner{AddonDir: dir, Store: st, Log: quiet}
		r.Init()
		if r.Status().Mode != "daily" || For(dir).Tagged() {
			t.Fatalf("%+v", r.Status())
		}
		r.Check(ctx, 24*time.Hour)
		s := For(dir)
		if r.Status().Last == nil || !s.Tagged() || model(t, s.Path()) != "HmIP-SMI" {
			t.Fatalf("%+v", r.Status())
		}
		// fresh enough: no new copy
		fi, _ := os.Stat(s.Path())
		r.Check(ctx, 24*time.Hour)
		fi2, _ := os.Stat(s.Path())
		if !fi.ModTime().Equal(fi2.ModTime()) {
			t.Fatal("a snapshot younger than a day stays")
		}
		// the RAM batch is in the daemon's copy: it writes the batch first
		st.UpsertDevice(store.Device{Interface: "HmIP-RF", Address: "000A", Model: "HmIP-SMI55", Power: "battery"})
		r.Check(ctx, 0)
		if model(t, s.Path()) != "HmIP-SMI55" {
			t.Fatal("the batch was not in the snapshot")
		}
		// a restart: the own snapshot counts, the tag stays
		r2 := &Runner{AddonDir: dir, Store: st, Log: quiet}
		r2.Init()
		if r2.Status().Last == nil || !s.Tagged() {
			t.Fatal("restart")
		}
	})
	t.Run("system", func(t *testing.T) {
		dir := t.TempDir()
		st := fill(t, dir, noMounts, "HmIP-SMI")
		defer st.Close()
		// a daily copy from before the system took over
		t.Setenv("INVOCATION_ID", "")
		(&Runner{AddonDir: dir, Store: st, Log: quiet}).Check(ctx, 0)
		t.Setenv("INVOCATION_ID", "1234")
		if r, err := Restore(dir, noMounts, true, quiet); err != nil || r != backup.Mine {
			t.Fatal(r, err)
		}
		r := &Runner{AddonDir: dir, Store: st, Log: quiet}
		r.Init()
		s := For(dir)
		if r.Status().Mode != "system" || !s.Tagged() {
			t.Fatalf("%+v", r.Status())
		}
		if _, err := os.Stat(s.Path()); err == nil {
			t.Fatal("the addon's own daily copy goes once the system takes the snapshots")
		}
		r.Run(ctx) // returns at once
	})
	t.Run("restored untags", func(t *testing.T) {
		t.Setenv("INVOCATION_ID", "")
		dir := t.TempDir()
		s := For(dir)
		s.Tag()
		r := &Runner{AddonDir: dir, Log: quiet}
		r.Init()
		if s.Tagged() {
			t.Fatal("no snapshot of the addon's own covers the database: untagged")
		}
	})
}

func copyFile(t *testing.T, src, dst string) {
	t.Helper()
	b, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, b, 0o600); err != nil {
		t.Fatal(err)
	}
}
