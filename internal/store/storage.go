package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
)

// Location says where the database file lives, as a stored id and a folder - never a mount
// path, the way openccu-lite's own location picker stores a choice: "userfs" is the addon's own
// var/ on the user partition (the default, in every system backup); "usb:<label>" is the stick
// whose filesystem label is <label>, wherever it is mounted, with the folder under its root
// ("battery" when empty). Network shares are not offered: SQLite's locking is not reliable on
// NFS and SMB, the same reason the system keeps its own database off them.
type Location struct {
	ID     string `json:"location"`
	Folder string `json:"folder,omitempty"`
}

// String is the location as the log names it.
func (l Location) String() string {
	if l.ID == "" || l.ID == "userfs" {
		return "userfs"
	}
	if l.Folder == "" {
		return l.ID + "/" + DefaultFolder
	}
	return l.ID + "/" + l.Folder
}

// InBackup says whether the system's backups hold the file: only the user partition.
func (l Location) InBackup() bool { return l.ID == "" || l.ID == "userfs" }

// DefaultFolder is the folder on a stick when none is given.
const DefaultFolder = "battery"

var folderShape = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*(/[A-Za-z0-9][A-Za-z0-9._-]*){0,3}$`)

// Validate checks the shape: an id of userfs or usb:<label>, a folder of up to four levels of
// letters, digits, . _ -, none hidden, no "..".
func (l Location) Validate() error {
	switch {
	case l.ID == "" || l.ID == "userfs":
		return nil
	case strings.HasPrefix(l.ID, "usb:"):
		if strings.TrimPrefix(l.ID, "usb:") == "" {
			return errors.New("a stick without a label cannot be chosen")
		}
	case strings.HasPrefix(l.ID, "share:"):
		return errors.New("a network share cannot hold the database (SQLite needs local locking)")
	default:
		return fmt.Errorf("unknown location %q", l.ID)
	}
	if l.Folder != "" && (!folderShape.MatchString(l.Folder) || strings.Contains(l.Folder, "..")) {
		return fmt.Errorf("folder %q: letters, digits, . _ - and up to four levels", l.Folder)
	}
	return nil
}

// Mount is one external medium the addon may write to.
type Mount struct {
	// Path is the mount point (/media/usb1).
	Path string `json:"path"`
	// Label is the filesystem label, "" when it has none (then it cannot be chosen).
	Label string `json:"label"`
	// Device is the block device (/dev/sda1).
	Device string `json:"device"`
	FSType string `json:"fstype"`
	// ReadOnly from the mount's options.
	ReadOnly bool `json:"read_only"`
}

// ID is the location id of the mount, "usb:<label>".
func (m Mount) ID() string { return "usb:" + m.Label }

// DefaultMounts lists the USB sticks openccu-lite mounts at /media/usb1..8 (FAT, exFAT and NTFS
// mounted root:usbstorage; ext4 keeps its own owners), from /proc/mounts, with the label from
// /dev/disk/by-label. A confined addon sees them only when its manifest declares the group
// usbstorage and the path /media.
func DefaultMounts() []Mount { return mountsFrom("/proc/mounts", "/dev/disk/by-label") }

func mountsFrom(procMounts, byLabel string) []Mount {
	b, err := os.ReadFile(procMounts)
	if err != nil {
		return nil
	}
	labels := map[string]string{}
	if entries, err := os.ReadDir(byLabel); err == nil {
		for _, e := range entries {
			target, err := os.Readlink(filepath.Join(byLabel, e.Name()))
			if err != nil {
				continue
			}
			dev := target
			if !filepath.IsAbs(target) {
				dev = filepath.Join(byLabel, target)
			}
			labels[filepath.Clean(dev)] = unescapeLabel(e.Name())
		}
	}
	var out []Mount
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) < 4 {
			continue
		}
		mp := unescapeMount(f[1])
		if !regexp.MustCompile(`^/media/usb[1-8]$`).MatchString(mp) {
			continue
		}
		ro := false
		for _, o := range strings.Split(f[3], ",") {
			if o == "ro" {
				ro = true
			}
		}
		out = append(out, Mount{Path: mp, Label: labels[filepath.Clean(f[0])], Device: f[0], FSType: f[2], ReadOnly: ro})
	}
	return out
}

// unescapeMount decodes /proc/mounts' octal escapes (\040 for a space).
func unescapeMount(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) {
			var c byte
			if _, err := fmt.Sscanf(s[i+1:i+4], "%o", &c); err == nil {
				b.WriteByte(c)
				i += 3
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// unescapeLabel decodes udev's \x20 escapes in /dev/disk/by-label names.
func unescapeLabel(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) && s[i+1] == 'x' {
			var c byte
			if _, err := fmt.Sscanf(s[i+2:i+4], "%x", &c); err == nil {
				b.WriteByte(c)
				i += 3
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// ErrNotPresent is answered when the location's medium is not mounted.
var ErrNotPresent = errors.New("the storage location is not present")

// Dir answers the directory the location names, or ErrNotPresent for a stick that is not there.
func (l Location) Dir(addonDir string, mounts []Mount) (string, error) {
	if err := l.Validate(); err != nil {
		return "", err
	}
	if l.InBackup() {
		return filepath.Join(addonDir, "var"), nil
	}
	label := strings.TrimPrefix(l.ID, "usb:")
	for _, m := range mounts {
		if m.Label == label {
			folder := l.Folder
			if folder == "" {
				folder = DefaultFolder
			}
			return filepath.Join(m.Path, filepath.FromSlash(folder)), nil
		}
	}
	return "", fmt.Errorf("%w: no stick labelled %q is mounted", ErrNotPresent, label)
}

// Resolve answers the database file's path.
func (l Location) Resolve(addonDir string, mounts []Mount) (string, error) {
	dir, err := l.Dir(addonDir, mounts)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, FileName), nil
}

// LoadLocation reads the setting from etc/battery.json; a missing file is the default.
func LoadLocation(path string) (Location, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Location{ID: "userfs"}, nil
	}
	if err != nil {
		return Location{}, err
	}
	var f struct {
		Storage Location `json:"storage"`
	}
	if err := json.Unmarshal(b, &f); err != nil {
		return Location{}, fmt.Errorf("%s: %w", path, err)
	}
	if f.Storage.ID == "" {
		f.Storage.ID = "userfs"
	}
	if err := f.Storage.Validate(); err != nil {
		return Location{}, fmt.Errorf("%s: %w", path, err)
	}
	return f.Storage, nil
}

// SaveLocation writes the setting atomically (a temporary file, fsync, rename), keeping the
// file's other keys.
func SaveLocation(path string, l Location) error {
	return SaveSetting(path, "storage", l)
}

// SaveSetting writes one top-level key of the settings file (etc/battery.json) atomically - a
// temporary file, fsync, rename - keeping the file's other keys as they are.
func SaveSetting(path, key string, v any) error {
	doc := map[string]json.RawMessage{}
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &doc)
	}
	lb, err := json.Marshal(v)
	if err != nil {
		return err
	}
	doc[key] = lb
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o640)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(out, '\n')); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}

// Candidate is one location the settings page offers.
type Candidate struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Path       string `json:"path"`
	FreeBytes  int64  `json:"free_bytes"`
	TotalBytes int64  `json:"total_bytes"`
	Allowed    bool   `json:"allowed"`
	Reason     string `json:"reason,omitempty"`
	InBackup   bool   `json:"in_backup"`
	Current    bool   `json:"current"`
}

// Candidates lists the user partition and every mounted stick, with whether it may be chosen.
func (s *Store) Candidates() []Candidate {
	s.mu.Lock()
	cur := s.loc
	s.mu.Unlock()
	userfs := filepath.Join(s.cfg.AddonDir, "var")
	out := []Candidate{{ID: "userfs", Name: "userfs", Path: userfs, Allowed: true, InBackup: true, Current: cur.InBackup()}}
	if free, total, err := space(userfs); err == nil {
		out[0].FreeBytes, out[0].TotalBytes = free, total
	}
	for _, m := range s.cfg.Mounts() {
		c := Candidate{ID: m.ID(), Name: m.Label, Path: m.Path, Allowed: true, Current: cur.ID == m.ID()}
		if m.Label == "" {
			c.ID, c.Allowed, c.Reason = "usb:", false, "no-label"
		}
		if m.ReadOnly {
			c.Allowed, c.Reason = false, "read-only"
		}
		if free, total, err := space(m.Path); err == nil {
			c.FreeBytes, c.TotalBytes = free, total
		}
		out = append(out, c)
	}
	return out
}

func space(dir string) (free, total int64, err error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return 0, 0, err
	}
	return int64(st.Bavail) * int64(st.Bsize), int64(st.Blocks) * int64(st.Bsize), nil
}

// the filesystems a database may not live on: memory, and network ones without local locking
var refusedFS = map[int64]string{
	0x01021994: "tmpfs", 0x858458f6: "ramfs", 0x6969: "nfs", 0xFF534D42: "cifs", 0xFE534D42: "smb2",
	0x65735546: "fuse", 0x9fa0: "proc", 0x62656572: "sysfs", 0x73717368: "squashfs", 0x28cd3d45: "cramfs",
}

// CheckTarget validates a directory for the database: it exists (or can be made), is writable,
// is not on tmpfs or a network filesystem, and has room for the file (twice its size plus
// 16 MiB, so a VACUUM INTO and a WAL fit). A probe file is written and removed.
func CheckTarget(dir string, needBytes int64) error {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("cannot create %s: %w", dir, err)
	}
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return fmt.Errorf("cannot stat %s: %w", dir, err)
	}
	if name, bad := refusedFS[int64(st.Type)]; bad {
		return fmt.Errorf("%s is on %s, which cannot hold the database", dir, name)
	}
	if st.Flags&1 != 0 { // ST_RDONLY
		return fmt.Errorf("%s is read-only", dir)
	}
	if free := int64(st.Bavail) * int64(st.Bsize); free < needBytes {
		return fmt.Errorf("%s has %d MB free, %d MB are needed", dir, free>>20, needBytes>>20)
	}
	probe := filepath.Join(dir, ".battery-write-test")
	if err := os.WriteFile(probe, []byte("ok\n"), 0o640); err != nil {
		return fmt.Errorf("%s is not writable: %w", dir, err)
	}
	os.Remove(probe)
	return nil
}

// Location answers the store's location.
func (s *Store) Location() Location {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loc
}

// Path answers the file's path, "" while unavailable.
func (s *Store) Path() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.path
}

// MoveHooks let a test fail a move at a chosen step.
type MoveHooks struct {
	AfterCopy   func() error
	AfterSwitch func() error
}

// Move takes the database to another location, safely: the writers are paused (reports go on
// into the RAM batch), the batch is flushed, a consistent copy is made with VACUUM INTO, the
// copy is verified (integrity_check, every table's row count), it is put in place, the setting
// is switched atomically, the store reopens on the new file, and the old file is removed or
// kept as the caller says. Any failure before the switch leaves everything as it was; a failure
// to reopen afterwards switches the setting back and reopens the old file. The old file is never
// touched until the new one is open.
func (s *Store) Move(ctx context.Context, target Location, keepOld bool, hooks *MoveHooks) (err error) {
	if hooks == nil {
		hooks = &MoveHooks{}
	}
	if err := target.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	if s.db == nil {
		s.mu.Unlock()
		return fmt.Errorf("%w: cannot move while it is %v", ErrUnavailable, s.err)
	}
	if s.paused {
		s.mu.Unlock()
		return errors.New("a move is already running")
	}
	oldPath, oldLoc := s.path, s.loc
	s.paused = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.paused = false
		s.mu.Unlock()
		s.Flush()
	}()

	newPath, rerr := target.Resolve(s.cfg.AddonDir, s.cfg.Mounts())
	if rerr != nil {
		return rerr
	}
	if filepath.Clean(newPath) == filepath.Clean(oldPath) {
		return errors.New("the database is already there")
	}
	var size int64
	if st, err := os.Stat(oldPath); err == nil {
		size = st.Size()
	}
	if err := CheckTarget(filepath.Dir(newPath), 2*size+16<<20); err != nil {
		return err
	}
	if _, err := os.Stat(newPath); err == nil {
		return fmt.Errorf("%s exists already; remove or rename it first", newPath)
	}

	// 1. the batch into the old file, so the copy holds everything
	s.mu.Lock()
	s.paused = false
	ferr := s.flushLocked()
	s.paused = true
	db := s.db
	s.mu.Unlock()
	if ferr != nil {
		return fmt.Errorf("flush before the copy: %w", ferr)
	}

	// 2. a consistent copy
	tmp := newPath + ".moving"
	os.Remove(tmp)
	if _, err := db.ExecContext(ctx, "VACUUM INTO ?", tmp); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("copy: %w", err)
	}
	cleanupTmp := func() { os.Remove(tmp); os.Remove(tmp + "-wal"); os.Remove(tmp + "-shm") }
	if hooks.AfterCopy != nil {
		if err := hooks.AfterCopy(); err != nil {
			cleanupTmp()
			return err
		}
	}

	// 3. verify the copy
	want, err := counts(ctx, db)
	if err != nil {
		cleanupTmp()
		return err
	}
	if err := verifyCopy(ctx, tmp, want); err != nil {
		cleanupTmp()
		return fmt.Errorf("the copy did not verify: %w", err)
	}

	// 4. in place, then the setting: the switch point
	if err := os.Rename(tmp, newPath); err != nil {
		cleanupTmp()
		return err
	}
	settings := s.cfg.settingsPath()
	if err := SaveLocation(settings, target); err != nil {
		os.Remove(newPath)
		return fmt.Errorf("saving the setting: %w", err)
	}
	if hooks.AfterSwitch != nil {
		if err := hooks.AfterSwitch(); err != nil {
			_ = SaveLocation(settings, oldLoc)
			os.Remove(newPath)
			return err
		}
	}

	// 5. reopen on the new file; on failure back to the old one, which was not touched
	s.mu.Lock()
	s.db.Close()
	newDB, oerr := openFile(newPath)
	if oerr != nil {
		_ = SaveLocation(settings, oldLoc)
		os.Remove(newPath)
		if s.db, err = openFile(oldPath); err != nil {
			s.db, s.err = nil, err
		}
		s.mu.Unlock()
		return fmt.Errorf("opening the moved database: %w (the old one stays in use)", oerr)
	}
	s.db, s.path, s.loc, s.err = newDB, newPath, target, nil
	s.mu.Unlock()
	s.logf("store: moved to %s (%s)", newPath, target)

	// 6. the old file
	if !keepOld {
		for _, suffix := range []string{"", "-wal", "-shm"} {
			os.Remove(oldPath + suffix)
		}
		s.logf("store: removed the old database at %s", oldPath)
	}
	return nil
}

func verifyCopy(ctx context.Context, path string, want map[string]int64) error {
	db, err := sql.Open("sqlite", path+"?mode=ro")
	if err != nil {
		return err
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	var res string
	if err := db.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&res); err != nil {
		return err
	}
	if res != "ok" {
		return fmt.Errorf("integrity_check: %s", res)
	}
	got, err := counts(ctx, db)
	if err != nil {
		return err
	}
	for t, n := range want {
		if got[t] != n {
			return fmt.Errorf("%s has %d rows, the source %d", t, got[t], n)
		}
	}
	return nil
}
