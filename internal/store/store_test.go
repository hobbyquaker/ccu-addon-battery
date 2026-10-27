package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestMain puts the temporary directories on the repository's filesystem: /tmp is tmpfs on many
// machines, and CheckTarget refuses tmpfs for the database, as it should.
func TestMain(m *testing.M) {
	dir, err := filepath.Abs(filepath.Join("..", "..", ".testtmp", "store"))
	if err == nil && os.MkdirAll(dir, 0o755) == nil {
		os.Setenv("TMPDIR", dir)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func f64(v float64) *float64 { return &v }
func b(v bool) *bool         { return &v }

func testStore(t *testing.T, mounts func() []Mount) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	s, err := Open(Config{AddonDir: dir, Log: t.Logf, Mounts: mounts})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s, dir
}

func TestOpenWriteRead(t *testing.T) {
	s, dir := testStore(t, nil)
	if !s.Available() || s.Path() != filepath.Join(dir, "var", FileName) {
		t.Fatalf("default location: %v %s", s.Available(), s.Path())
	}
	now := time.Now().UnixMilli()
	s.UpsertDevice(Device{Interface: "HmIP-RF", Address: "DEVA", Model: "HmIP-WRC2", VoltageDP: "0/OPERATING_VOLTAGE", LowBatDP: "0/LOW_BAT", FirstSeen: now, LastSeen: now})
	s.PutCurrent(Current{Interface: "HmIP-RF", Address: "DEVA", Voltage: f64(2.9), VoltageTS: now, LowBat: b(false), LastReport: now})
	s.AddSample(Sample{Interface: "HmIP-RF", Address: "DEVA", TS: now, Voltage: f64(2.9), LowBat: b(false), Source: "live"})
	s.AddSample(Sample{Interface: "HmIP-RF", Address: "DEVA", TS: now + 1, Voltage: f64(2.8), Source: "live"})
	s.AddEvent(Event{Interface: "HmIP-RF", Address: "DEVA", TS: now, Key: "BOOTED", Value: "true"})
	if h := s.Health(); h.Pending != 5 || h.State != "ok" || !h.InBackup {
		t.Fatalf("health before flush: %+v", h)
	}
	if err := s.Flush(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	devs, err := s.Devices(ctx)
	if err != nil || len(devs) != 1 || devs[0].Model != "HmIP-WRC2" {
		t.Fatalf("devices: %v %+v", err, devs)
	}
	cur, err := s.Currents(ctx)
	if err != nil || len(cur) != 1 || *cur[0].Voltage != 2.9 || *cur[0].LowBat || cur[0].Unreach != nil {
		t.Fatalf("currents: %v %+v", err, cur)
	}
	last, err := s.LastSample(ctx, "HmIP-RF", "DEVA")
	if err != nil || last == nil || *last.Voltage != 2.8 || last.LowBat != nil {
		t.Fatalf("last sample: %v %+v", err, last)
	}
	rows, err := s.Samples(ctx, "HmIP-RF", "DEVA", 0, 0)
	if err != nil || len(rows) != 2 {
		t.Fatalf("samples: %v %d", err, len(rows))
	}
	c, _ := s.Counts(ctx)
	if c["samples"] != 2 || c["events"] != 1 || c["devices"] != 1 {
		t.Fatalf("counts: %v", c)
	}
	// the upsert keeps first_seen and clears a retirement
	s.RetireDevice("HmIP-RF", "DEVA", now+5)
	s.Flush()
	devs, _ = s.Devices(ctx)
	if devs[0].RetiredAt != now+5 {
		t.Fatal("retired")
	}
	s.UpsertDevice(Device{Interface: "HmIP-RF", Address: "DEVA", Model: "HmIP-WRC2", FirstSeen: now + 9, LastSeen: now + 9})
	s.Flush()
	devs, _ = s.Devices(ctx)
	if devs[0].RetiredAt != 0 || devs[0].FirstSeen != now {
		t.Fatalf("re-seen: %+v", devs[0])
	}
	// reopen: the schema version holds, the data is there
	s.Close()
	s2, err := Open(Config{AddonDir: dir, Log: t.Logf})
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	if c, _ := s2.Counts(ctx); c["samples"] != 2 {
		t.Fatal("reopen lost rows")
	}
}

func TestLocation(t *testing.T) {
	for _, l := range []Location{{ID: "userfs"}, {}, {ID: "usb:STICK"}, {ID: "usb:STICK", Folder: "a/b-c.d"}} {
		if err := l.Validate(); err != nil {
			t.Errorf("%+v: %v", l, err)
		}
	}
	for _, l := range []Location{{ID: "usb:"}, {ID: "share:nas"}, {ID: "nope"}, {ID: "usb:X", Folder: "../x"}, {ID: "usb:X", Folder: ".hidden"}, {ID: "usb:X", Folder: "a/b/c/d/e"}} {
		if err := l.Validate(); err == nil {
			t.Errorf("%+v accepted", l)
		}
	}
	mounts := []Mount{{Path: "/media/usb1", Label: "STICK", Device: "/dev/sda1", FSType: "vfat"}}
	p, err := Location{ID: "usb:STICK"}.Resolve("/usr/local/addons/battery", mounts)
	if err != nil || p != "/media/usb1/battery/battery.db" {
		t.Fatalf("usb resolve: %v %s", err, p)
	}
	p, _ = Location{ID: "usb:STICK", Folder: "data/bat"}.Resolve("/x", mounts)
	if p != "/media/usb1/data/bat/battery.db" {
		t.Fatal(p)
	}
	if _, err := (Location{ID: "usb:OTHER"}).Resolve("/x", mounts); !errors.Is(err, ErrNotPresent) {
		t.Fatalf("absent stick: %v", err)
	}
	p, _ = Location{}.Resolve("/usr/local/addons/battery", nil)
	if p != "/usr/local/addons/battery/var/battery.db" {
		t.Fatal(p)
	}
	// settings file round trip, other keys kept
	dir := t.TempDir()
	path := filepath.Join(dir, "etc", "battery.json")
	if l, err := LoadLocation(path); err != nil || l.ID != "userfs" {
		t.Fatalf("missing file is the default: %v %+v", err, l)
	}
	os.MkdirAll(filepath.Dir(path), 0o750)
	os.WriteFile(path, []byte(`{"other": {"x": 1}, "storage": {"location": "usb:STICK", "folder": "f"}}`), 0o640)
	l, err := LoadLocation(path)
	if err != nil || l.ID != "usb:STICK" || l.Folder != "f" {
		t.Fatalf("load: %v %+v", err, l)
	}
	if err := SaveLocation(path, Location{ID: "userfs"}); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	if !strings.Contains(string(raw), `"other"`) || !strings.Contains(string(raw), `"location": "userfs"`) {
		t.Fatalf("save kept the other keys: %s", raw)
	}
	os.WriteFile(path, []byte(`{"storage": {"location": "share:nas"}}`), 0o640)
	if _, err := LoadLocation(path); err == nil {
		t.Fatal("a share must be refused at load")
	}
}

func TestMounts(t *testing.T) {
	dir := t.TempDir()
	pm := filepath.Join(dir, "mounts")
	os.WriteFile(pm, []byte("/dev/sda1 /media/usb1 vfat rw,relatime 0 0\n/dev/sdb1 /media/usb2 ext4 ro,relatime 0 0\n/dev/sdc1 /media/net/nas cifs rw 0 0\n/dev/sdd1 /media/usb\\0403 vfat rw 0 0\n"), 0o644)
	bl := filepath.Join(dir, "by-label")
	os.MkdirAll(bl, 0o755)
	os.Symlink("/dev/sda1", filepath.Join(bl, "MY\\x20STICK"))
	m := mountsFrom(pm, bl)
	if len(m) != 2 || m[0].Path != "/media/usb1" || m[0].Label != "MY STICK" || m[1].ReadOnly != true || m[1].Label != "" {
		t.Fatalf("mounts: %+v", m)
	}
}

func TestCheckTarget(t *testing.T) {
	dir := t.TempDir()
	if err := CheckTarget(filepath.Join(dir, "new", "sub"), 1<<20); err != nil {
		t.Fatal(err)
	}
	if err := CheckTarget(dir, 1<<60); err == nil || !strings.Contains(err.Error(), "free") {
		t.Fatalf("space: %v", err)
	}
	if os.Getuid() != 0 {
		ro := filepath.Join(dir, "ro")
		os.MkdirAll(ro, 0o500)
		if err := CheckTarget(filepath.Join(ro, "x"), 1); err == nil {
			t.Fatal("unwritable accepted")
		}
	}
	if err := CheckTarget("/dev/shm", 1); err == nil || !strings.Contains(err.Error(), "tmpfs") {
		t.Logf("tmpfs check (skipped if /dev/shm is not tmpfs here): %v", err)
	}
}

func fill(t *testing.T, s *Store, n int) {
	t.Helper()
	now := time.Now().UnixMilli()
	s.UpsertDevice(Device{Interface: "HmIP-RF", Address: "DEVA", Model: "HmIP-WRC2", FirstSeen: now, LastSeen: now})
	for i := 0; i < n; i++ {
		s.AddSample(Sample{Interface: "HmIP-RF", Address: "DEVA", TS: now + int64(i), Voltage: f64(2.9), Source: "live"})
	}
	if err := s.Flush(); err != nil {
		t.Fatal(err)
	}
}

func TestMoveSuccess(t *testing.T) {
	stick := t.TempDir()
	mounts := func() []Mount { return []Mount{{Path: stick, Label: "STICK", FSType: "vfat"}} }
	s, dir := testStore(t, mounts)
	fill(t, s, 500)
	oldPath := s.Path()
	ctx := context.Background()
	// a write during the move lands in the batch and reaches the new file
	if err := s.Move(ctx, Location{ID: "usb:STICK"}, false, &MoveHooks{AfterCopy: func() error {
		s.AddSample(Sample{Interface: "HmIP-RF", Address: "DEVA", TS: 1, Voltage: f64(1.0), Source: "live"})
		return nil
	}}); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(stick, "battery", FileName)
	if s.Path() != want || s.Location().ID != "usb:STICK" || s.Health().InBackup {
		t.Fatalf("after move: %s %+v", s.Path(), s.Location())
	}
	if _, err := os.Stat(oldPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("old file should be removed")
	}
	c, _ := s.Counts(ctx)
	if c["samples"] != 501 {
		t.Fatalf("rows after move: %v", c)
	}
	if l, _ := LoadLocation(filepath.Join(dir, "etc", "battery.json")); l.ID != "usb:STICK" {
		t.Fatalf("setting: %+v", l)
	}
	if _, err := os.Stat(want + ".moving"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("temp file left")
	}
	// back to the userfs, keeping the old one
	if err := s.Move(ctx, Location{ID: "userfs"}, true, nil); err != nil {
		t.Fatal(err)
	}
	if s.Path() != oldPath {
		t.Fatal(s.Path())
	}
	if _, err := os.Stat(want); err != nil {
		t.Fatal("kept file gone")
	}
	// a second move there is refused (the file exists), and so is a move onto itself
	if err := s.Move(ctx, Location{ID: "usb:STICK"}, false, nil); err == nil || !strings.Contains(err.Error(), "exists") {
		t.Fatalf("existing target: %v", err)
	}
	if err := s.Move(ctx, Location{ID: "userfs"}, false, nil); err == nil {
		t.Fatal("move onto itself")
	}
	// the store reopened after a successful move still works across Close/Open
	s.Close()
	s2, err := Open(Config{AddonDir: dir, Log: t.Logf, Mounts: mounts})
	if err != nil || s2.Path() != oldPath {
		t.Fatalf("reopen: %v %s", err, s2.Path())
	}
	s2.Close()
}

func TestMoveFailures(t *testing.T) {
	stick := t.TempDir()
	mounts := func() []Mount { return []Mount{{Path: stick, Label: "STICK", FSType: "vfat"}} }
	s, dir := testStore(t, mounts)
	fill(t, s, 100)
	oldPath := s.Path()
	ctx := context.Background()
	settings := filepath.Join(dir, "etc", "battery.json")
	unchanged := func(step string) {
		t.Helper()
		if s.Path() != oldPath || !s.Available() {
			t.Fatalf("%s: store changed: %s %v", step, s.Path(), s.Health())
		}
		if l, _ := LoadLocation(settings); !l.InBackup() {
			t.Fatalf("%s: setting changed: %+v", step, l)
		}
		if c, _ := s.Counts(ctx); c["samples"] != 100 {
			t.Fatalf("%s: rows: %v", step, c)
		}
		if m, _ := filepath.Glob(filepath.Join(stick, "battery", "*")); len(m) != 0 {
			t.Fatalf("%s: target not cleaned: %v", step, m)
		}
	}
	// a stick that is not there
	if err := s.Move(ctx, Location{ID: "usb:OTHER"}, false, nil); !errors.Is(err, ErrNotPresent) {
		t.Fatalf("absent: %v", err)
	}
	unchanged("absent")
	// a failure mid-copy (after VACUUM INTO, before the verification)
	boom := errors.New("disk pulled")
	if err := s.Move(ctx, Location{ID: "usb:STICK"}, false, &MoveHooks{AfterCopy: func() error { return boom }}); !errors.Is(err, boom) {
		t.Fatalf("mid-copy: %v", err)
	}
	unchanged("mid-copy")
	// a corrupted copy does not verify
	if err := s.Move(ctx, Location{ID: "usb:STICK"}, false, &MoveHooks{AfterCopy: func() error {
		return os.WriteFile(filepath.Join(stick, "battery", FileName+".moving"), []byte("garbage"), 0o640)
	}}); err == nil || !strings.Contains(err.Error(), "verify") {
		t.Fatalf("corrupt copy: %v", err)
	}
	unchanged("corrupt")
	// a failure after the switch: the setting goes back, the old file is in use
	if err := s.Move(ctx, Location{ID: "usb:STICK"}, false, &MoveHooks{AfterSwitch: func() error { return boom }}); !errors.Is(err, boom) {
		t.Fatalf("after switch: %v", err)
	}
	unchanged("after-switch")
	// an unwritable target
	if os.Getuid() != 0 {
		ro := t.TempDir()
		os.Chmod(ro, 0o500)
		roMounts := func() []Mount { return []Mount{{Path: ro, Label: "RO", FSType: "vfat"}} }
		s2, _ := testStore(t, roMounts)
		fill(t, s2, 3)
		if err := s2.Move(ctx, Location{ID: "usb:RO"}, false, nil); err == nil {
			t.Fatal("unwritable target accepted")
		}
		if !s2.Available() || s2.Location().ID != "userfs" {
			t.Fatal("store changed after an unwritable target")
		}
	}
	// writes made during a failed move are not lost
	s.Move(ctx, Location{ID: "usb:STICK"}, false, &MoveHooks{AfterCopy: func() error {
		s.AddSample(Sample{Interface: "HmIP-RF", Address: "DEVA", TS: 7, Voltage: f64(1.0), Source: "live"})
		return boom
	}})
	if c, _ := s.Counts(ctx); c["samples"] != 101 {
		t.Fatalf("the batch of a failed move was lost: %v", c)
	}
}

func TestUnavailableAndDegraded(t *testing.T) {
	dir := t.TempDir()
	stick := filepath.Join(dir, "stick")
	present := false
	mounts := func() []Mount {
		if !present {
			return nil
		}
		return []Mount{{Path: stick, Label: "STICK", FSType: "vfat"}}
	}
	os.MkdirAll(filepath.Join(dir, "etc"), 0o750)
	SaveLocation(filepath.Join(dir, "etc", "battery.json"), Location{ID: "usb:STICK"})
	s, err := Open(Config{AddonDir: dir, Log: t.Logf, Mounts: mounts, MaxQueue: 5})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if s.Available() || s.Health().State != "unavailable" {
		t.Fatalf("a stick that is not there: %+v", s.Health())
	}
	// no file is created anywhere else
	if _, err := os.Stat(filepath.Join(dir, "var", FileName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("a database was created on the userfs although the setting says the stick")
	}
	for i := 0; i < 8; i++ {
		s.AddSample(Sample{Interface: "HmIP-RF", Address: "DEVA", TS: int64(i), Voltage: f64(2.9), Source: "live"})
	}
	if h := s.Health(); h.Pending != 5 || h.Dropped != 3 {
		t.Fatalf("the RAM batch is capped: %+v", h)
	}
	if _, err := s.Devices(context.Background()); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("reads say unavailable: %v", err)
	}
	// the stick appears: the store opens and the batch lands
	os.MkdirAll(stick, 0o750)
	present = true
	if !s.Retry() || !s.Available() {
		t.Fatalf("retry: %+v", s.Health())
	}
	if c, _ := s.Counts(context.Background()); c["samples"] != 5 {
		t.Fatalf("batch after retry: %v", c)
	}
	// the stick goes away: the next flush fails, the store is degraded, the batch is kept
	os.RemoveAll(stick)
	s.AddSample(Sample{Interface: "HmIP-RF", Address: "DEVA", TS: 99, Voltage: f64(2.9), Source: "live"})
	if err := s.Flush(); err == nil {
		t.Fatal("a flush onto a pulled stick must fail")
	}
	if h := s.Health(); h.State != "degraded" || h.Pending != 1 || !strings.Contains(h.Error, "not there") {
		t.Fatalf("degraded: %+v", h)
	}
}
