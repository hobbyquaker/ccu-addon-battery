// Package snapshot is the addon's part in the system backup: a consistent copy of battery.db in
// the staging directory the manifest declares (backup/ in the addon's tree), wherever the database
// lives - on the user partition or on a USB stick, which the system's backup never reads.
//
//   - On openccu-lite from 1.3.0-beta.0 the system runs bin/backup-snapshot (Hook) before every
//     backup and empties the staging after the tar; after a restore it runs bin/backup-restore
//     (Restore) before the unit starts.
//   - Anywhere else (a CCU3, OpenCCU, an older openccu-lite) the daemon keeps a snapshot at most a
//     day old in the staging itself (Runner), so the firmware's own backup carries it.
//
// While a snapshot covers the database, var/ (the default location) is tagged .nobackup: the
// backup then holds the consistent copy and not also the live file, which may be torn while the
// daemon writes. A failed snapshot removes the tag again (ccu-addon-kit's addonkit/backup).
package snapshot

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/hobbyquaker/ccu-addon-battery/internal/store"
	"github.com/hobbyquaker/ccu-addon-kit/go/addonkit/backup"
)

// Staging is the manifest's backup.staging, relative to the addon's directory.
const Staging = "backup"

// For is the database's snapshot in the addon's tree.
func For(addonDir string) backup.SQLite {
	return backup.SQLite{Staging: filepath.Join(addonDir, Staging), Name: store.FileName, DataDir: filepath.Join(addonDir, "var")}
}

func settingsPath(addonDir string) string { return filepath.Join(addonDir, "etc", "battery.json") }

func noticePath(addonDir string) string { return filepath.Join(addonDir, "var", "restore-notice.json") }

// Hook is the manifest's hook (bin/backup-snapshot): a snapshot of the database at its configured
// location, made by a process of its own while the daemon runs. The rows of the daemon's RAM batch
// (at most its flush interval, 10 minutes) are not in it. A database that does not exist yet is
// nothing to copy: no snapshot, no error.
func Hook(ctx context.Context, addonDir string, mounts func() []store.Mount) (string, error) {
	s := For(addonDir)
	loc, err := store.LoadLocation(settingsPath(addonDir))
	if err != nil {
		s.Untag()
		return "", err
	}
	path, err := loc.Resolve(addonDir, mounts())
	if err != nil {
		s.Untag()
		return "", err
	}
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return "no database yet at " + path + ", nothing to copy", nil
	}
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(30000)")
	if err != nil {
		s.Untag()
		return "", err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if err := s.Write(ctx, db, backup.ByHook); err != nil {
		return "", err
	}
	st, _ := os.Stat(s.Path())
	size := int64(0)
	if st != nil {
		size = st.Size()
	}
	return fmt.Sprintf("snapshot of %s written (%d bytes)", path, size), nil
}

// Notice says that a restore found the stick of the database missing and put the database on the
// user partition instead.
type Notice struct {
	At    time.Time `json:"at"`
	Label string    `json:"label"`
	Path  string    `json:"path"`
}

// Restore puts a snapshot that a restore brought back into the staging where the database lives,
// unless the file there is newer. When the configured stick is not there, the database goes to the
// user partition and the setting follows, with a notice. program is true for the manifest's
// restore program (bin/backup-restore), which the system runs before every start of the unit: it
// marks the start (so the daemon knows the system runs the hook) and empties the staging after a
// restore. The daemon calls it with false at its start, for the systems that run no restore
// program; there the snapshot stays, since it is the daily copy the firmware's backup takes.
func Restore(addonDir string, mounts func() []store.Mount, program bool, logf func(string, ...any)) (backup.Result, error) {
	s := For(addonDir)
	if program {
		if err := s.MarkRestoreRun(); err != nil {
			logf("battery: backup: %v", err)
		}
	}
	if _, err := os.Stat(s.Path()); err != nil {
		return backup.Nothing, nil
	}
	loc, err := store.LoadLocation(settingsPath(addonDir))
	if err != nil {
		return backup.Nothing, err
	}
	live, err := loc.Resolve(addonDir, mounts())
	fallback := false
	if errors.Is(err, store.ErrNotPresent) {
		live, fallback = filepath.Join(addonDir, "var", store.FileName), true
	} else if err != nil {
		return backup.Nothing, err
	}
	res, err := s.Restore(live)
	if err != nil {
		return res, err
	}
	switch res {
	case backup.Restored:
		logf("battery: backup: the database was restored from the system backup into %s", live)
		if fallback {
			label := loc.ID[len("usb:"):]
			if err := store.SaveLocation(settingsPath(addonDir), store.Location{ID: "userfs"}); err != nil {
				return res, err
			}
			b, _ := json.Marshal(Notice{At: time.Now(), Label: label, Path: live})
			os.WriteFile(noticePath(addonDir), b, 0o640)
			logf("battery: backup: the stick %q of the database is not there; the restored database is on the user partition now", label)
		}
	case backup.Kept:
		logf("battery: backup: the database at %s is newer than the restored snapshot and stays", live)
	}
	if program && (res == backup.Restored || res == backup.Kept) {
		if err := s.Clear(); err != nil {
			logf("battery: backup: emptying the staging: %v", err)
		}
	}
	return res, nil
}

// Status is what the API shows about the database's part in the backup.
type Status struct {
	// Mode: "system" - the system runs the hook before every backup (openccu-lite from
	// 1.3.0-beta.0); "daily" - the addon keeps a snapshot at most a day old for the firmware's own
	// backup.
	Mode string `json:"mode"`
	// Last is when the addon last wrote its own snapshot (daily), Error why the last one failed.
	Last  *time.Time `json:"last,omitempty"`
	Error string     `json:"error,omitempty"`
	// Notice: a restore put the database on the user partition because its stick was missing.
	Notice *Notice `json:"notice,omitempty"`
}

// Runner keeps the daemon's side: the mode, the tag and, where the system runs no hook, the daily
// snapshot.
type Runner struct {
	AddonDir string
	Store    *store.Store
	Log      func(string, ...any)
	// Every is how often the snapshot's age is checked (1 h), MaxAge when it is renewed (24 h).
	Every, MaxAge time.Duration

	mu     sync.Mutex
	mode   string
	last   *time.Time
	err    string
	notice *Notice
}

// Status answers the current state.
func (r *Runner) Status() Status {
	r.mu.Lock()
	defer r.mu.Unlock()
	st := Status{Mode: r.mode, Last: r.last, Error: r.err, Notice: r.notice}
	if st.Mode == "" {
		st.Mode = "daily"
	}
	return st
}

// Init decides the mode, before the daemon serves anything: a system that ran the restore program
// in this start runs the hook (var/ is tagged: the hook covers the database when the user keeps the
// addon's snapshot on, and the user chose to leave it out when it is off; a snapshot of the addon's
// own from before goes), anywhere else the daily snapshot.
func (r *Runner) Init() {
	s := For(r.AddonDir)
	r.mu.Lock()
	defer r.mu.Unlock()
	if b, err := os.ReadFile(noticePath(r.AddonDir)); err == nil {
		var n Notice
		if json.Unmarshal(b, &n) == nil && time.Since(n.At) < 14*24*time.Hour {
			r.notice = &n
		}
	}
	if s.SystemRunsHook() {
		r.mode = "system"
		if err := s.Tag(); err != nil {
			r.Log("battery: backup: %v", err)
		}
		if _, by, ok := s.Own(); ok && by == backup.ByAddon {
			s.Clear()
		}
		r.Log("battery: backup: the system takes a snapshot of the database before every backup")
		return
	}
	r.mode = "daily"
	if _, _, ok := s.Own(); !ok && s.Tagged() {
		// no snapshot of ours covers the database (a restore, a staging emptied by hand): the
		// backup takes the live file until the first snapshot
		s.Untag()
	}
	if age, _, ok := s.Own(); ok {
		t := time.Now().Add(-age)
		r.last = &t
	}
}

// Run keeps the daily snapshot until ctx ends; it does nothing where the system runs the hook.
func (r *Runner) Run(ctx context.Context) {
	if r.Status().Mode != "daily" {
		return
	}
	every, maxAge := r.Every, r.MaxAge
	if every <= 0 {
		every = time.Hour
	}
	if maxAge <= 0 {
		maxAge = 24 * time.Hour
	}
	first := time.NewTimer(30 * time.Second)
	defer first.Stop()
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-first.C:
		case <-t.C:
		}
		r.Check(ctx, maxAge)
	}
}

// Check writes the daily snapshot when there is none of the addon's own or it is older than maxAge.
func (r *Runner) Check(ctx context.Context, maxAge time.Duration) {
	s := For(r.AddonDir)
	if age, by, ok := s.Own(); ok && by == backup.ByAddon && age < maxAge {
		return
	}
	err := r.Store.Snapshot(ctx, func(db *sql.DB) error { return s.Write(ctx, db, backup.ByAddon) })
	r.mu.Lock()
	defer r.mu.Unlock()
	if err != nil {
		if r.err != err.Error() {
			r.Log("battery: backup: the daily snapshot failed: %v", err)
		}
		r.err = err.Error()
		return
	}
	now := time.Now()
	r.last, r.err = &now, ""
}
