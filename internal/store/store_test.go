package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
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
	for _, l := range []Location{{ID: "userfs"}, {}, {ID: "usb:STICK"}, {ID: "usb:STICK", Folder: "a/b-c.d"}, {ID: "usb-uuid:1A2B-3C4D"}, {ID: "usb-uuid:0b9a4d6e-63f0-4a8e-9c0e-6a1c1e0d2f11", Folder: "f"}, {ID: "usb-path:/media/usb1"}, {ID: "usb-path:/media/usb0"}} {
		if err := l.Validate(); err != nil {
			t.Errorf("%+v: %v", l, err)
		}
	}
	for _, l := range []Location{{ID: "usb:"}, {ID: "share:nas"}, {ID: "nope"}, {ID: "usb-uuid:"}, {ID: "usb-uuid:../x"}, {ID: "usb-path:/etc"}, {ID: "usb-path:/media/usb1/../../etc"}, {ID: "usb-path:"}, {ID: "usb:X", Folder: "../x"}, {ID: "usb:X", Folder: ".hidden"}, {ID: "usb:X", Folder: "a/b/c/d/e"}} {
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
	m := mountsFrom(pm, bl, filepath.Join(dir, "by-uuid"), nil)
	if len(m) != 2 || m[0].Path != "/media/usb1" || m[0].Label != "MY STICK" || m[1].ReadOnly != true || m[1].Label != "" {
		t.Fatalf("mounts: %+v", m)
	}
	if m[0].ID() != "usb:MY STICK" || m[0].Kind() != "label" {
		t.Fatalf("id: %s %s", m[0].ID(), m[0].Kind())
	}
}

// The mount tables of the systems the addon runs on, and what names each stick: its label, for
// a stick without one its UUID, and its mount point where the system tells neither - the report
// from piVCCU, whose container has no udev and so no /dev/disk/by-label: every stick looked
// label-less there and could not be chosen.
func TestMountsBySystem(t *testing.T) {
	type link struct{ dir, name, dev string }
	for _, c := range []struct {
		name   string
		mounts string
		links  []link
		blkid  map[string]string // device -> blkid's line; nil: blkid cannot read any device
		want   []string          // id kind label uuid, per stick
	}{
		{
			name: "openccu-lite and OpenCCU: udev, a labelled stick",
			mounts: "tmpfs /media tmpfs rw,nosuid,nodev,noexec,noatime 0 0\n" +
				"/dev/sda1 /media/usb1 vfat rw,nodev,noexec,noatime,nodiratime,gid=1001,fmask=0007,dmask=0007,utf8 0 0\n",
			links: []link{{"by-label", "BATTERY", "/dev/sda1"}, {"by-uuid", "1A2B-3C4D", "/dev/sda1"}, {"by-label", "userfs", "/dev/mmcblk0p3"}},
			want:  []string{"usb:BATTERY label BATTERY 1A2B-3C4D"},
		},
		{
			name:   "udev, a stick without a label: named by its UUID",
			mounts: "/dev/sda1 /media/usb1 vfat rw,noexec 0 0\n/dev/sdb1 /media/usb2 ext4 rw,noexec 0 0\n",
			links:  []link{{"by-uuid", "5E7A-01FF", "/dev/sda1"}, {"by-uuid", "0b9a4d6e-63f0-4a8e-9c0e-6a1c1e0d2f11", "/dev/sdb1"}},
			want:   []string{"usb-uuid:5E7A-01FF uuid  5E7A-01FF", "usb-uuid:0b9a4d6e-63f0-4a8e-9c0e-6a1c1e0d2f11 uuid  0b9a4d6e-63f0-4a8e-9c0e-6a1c1e0d2f11"},
		},
		{
			name:   "a CCU3 at /media/usb1, udev without the device: blkid tells",
			mounts: "/dev/sda1 /media/usb1 vfat rw,relatime 0 0\n/dev/sdb1 /media/usb2 vfat rw,relatime 0 0\n",
			blkid:  map[string]string{"/dev/sda1": `/dev/sda1: LABEL="KEYS" UUID="AAAA-BBBB" TYPE="vfat"`, "/dev/sdb1": `/dev/sdb1: UUID="CCCC-DDDD" TYPE="vfat"`},
			want:   []string{"usb:KEYS label KEYS AAAA-BBBB", "usb-uuid:CCCC-DDDD uuid  CCCC-DDDD"},
		},
		{
			name:   "piVCCU's container: no /dev/disk, blkid cannot read the device - named by the mount point",
			mounts: "/dev/sda1 /media/usb1 vfat rw,relatime,fmask=0022,dmask=0022 0 0\n",
			want:   []string{"usb-path:/media/usb1 path  "},
		},
		{
			name:   "an old RaspberryMatic at /media/usb0, read-only",
			mounts: "/dev/sda1 /media/usb0 vfat ro,relatime 0 0\n",
			want:   []string{"usb-path:/media/usb0 path  "},
		},
		{
			name:   "nothing mounted: the empty mount points of a CCU3 are no sticks",
			mounts: "tmpfs /media tmpfs rw 0 0\n/dev/mmcblk0p3 /usr/local ext4 rw 0 0\n",
		},
	} {
		dir := t.TempDir()
		pm := filepath.Join(dir, "mounts")
		os.WriteFile(pm, []byte(c.mounts), 0o644)
		for _, l := range c.links {
			os.MkdirAll(filepath.Join(dir, l.dir), 0o755)
			if err := os.Symlink(l.dev, filepath.Join(dir, l.dir, l.name)); err != nil {
				t.Fatal(err)
			}
		}
		probe := func(dev string) (string, string) { return parseBlkid(c.blkid[dev]) }
		var got []string
		for _, m := range mountsFrom(pm, filepath.Join(dir, "by-label"), filepath.Join(dir, "by-uuid"), probe) {
			got = append(got, m.ID()+" "+m.Kind()+" "+m.Label+" "+m.UUID)
			// what the location id names is found again among the mounts
			if !m.matches(m.ID()) {
				t.Errorf("%s: %s does not match itself", c.name, m.ID())
			}
			if err := (Location{ID: m.ID()}).Validate(); err != nil {
				t.Errorf("%s: %s: %v", c.name, m.ID(), err)
			}
		}
		if strings.Join(got, "\n") != strings.Join(c.want, "\n") {
			t.Errorf("%s:\n got %q\nwant %q", c.name, got, c.want)
		}
	}
}

func TestParseBlkid(t *testing.T) {
	for _, c := range []struct{ in, label, uuid string }{
		{`/dev/sda1: LABEL="MY STICK" UUID="1A2B-3C4D" TYPE="vfat"`, "MY STICK", "1A2B-3C4D"},
		{`/dev/sda1: UUID="1A2B-3C4D" TYPE="vfat"`, "", "1A2B-3C4D"},
		// util-linux: neither the FAT boot label nor the partition's label and UUID are the filesystem's
		{`/dev/sda1: LABEL_FATBOOT="X" UUID="9C33-6BBD" BLOCK_SIZE="512" TYPE="vfat" PARTLABEL="p" PARTUUID="6c3a2d1e-01"`, "", "9C33-6BBD"},
		{`/dev/sdb1: LABEL="data" UUID="0b9a4d6e-63f0-4a8e-9c0e-6a1c1e0d2f11" TYPE="ext4"`, "data", "0b9a4d6e-63f0-4a8e-9c0e-6a1c1e0d2f11"},
		{"", "", ""},
	} {
		if l, u := parseBlkid(c.in); l != c.label || u != c.uuid {
			t.Errorf("%s: %q %q", c.in, l, u)
		}
	}
	if l, u := blkid("not-a-device"); l != "" || u != "" {
		t.Errorf("blkid of no device: %q %q", l, u)
	}
}

// A stick without a label can be chosen: the candidates offer it, the database moves there and
// is found again after a restart - by its UUID wherever it is mounted, by its mount point where
// the system tells nothing else. A stick that got a label later is still found by its UUID.
func TestMoveToAStickWithoutALabel(t *testing.T) {
	for _, c := range []struct {
		name, uuid, id string
	}{
		{"by UUID", "5E7A-01FF", "usb-uuid:5E7A-01FF"},
		{"by mount point", "", "usb-path:/media/usb1"},
	} {
		t.Run(c.name, func(t *testing.T) {
			stick := t.TempDir()
			mounted := Mount{Path: stick, UUID: c.uuid, Device: "/dev/sda1", FSType: "vfat"}
			if c.uuid == "" {
				// the test's stick is not at /media/usb1; the id is its path, so name it
				c.id = "usb-path:" + stick
			}
			var mu sync.Mutex
			present := []Mount{mounted}
			mounts := func() []Mount {
				mu.Lock()
				defer mu.Unlock()
				return append([]Mount(nil), present...)
			}
			s, dir := testStore(t, mounts)
			s.UpsertDevice(Device{Interface: "HmIP-RF", Address: "A", Model: "HmIP-WRC2", FirstSeen: 1, LastSeen: 1})
			s.Flush()
			var cand *Candidate
			for _, x := range s.Candidates() {
				if x.Path == stick {
					x := x
					cand = &x
				}
			}
			if cand == nil || !cand.Allowed || cand.Reason != "" || cand.ID != mounted.ID() || cand.Name != "" || cand.Current {
				t.Fatalf("candidate: %+v", cand)
			}
			if c.uuid != "" && (cand.ID != c.id || cand.Kind != "uuid") {
				t.Fatalf("candidate: %+v", cand)
			}
			if c.uuid == "" && cand.Kind != "path" {
				t.Fatalf("candidate: %+v", cand)
			}
			loc := Location{ID: cand.ID}
			if c.uuid == "" {
				// a real mount point passes Validate; the test's temporary directory does not,
				// so the move is driven below the API's validation
				if err := (Location{ID: "usb-path:/media/usb1"}).Validate(); err != nil {
					t.Fatal(err)
				}
				mountShape = regexp.MustCompile(`^/`)
				t.Cleanup(func() { mountShape = regexp.MustCompile(`^/media/usb[0-9]$`) })
			}
			if err := s.Move(context.Background(), loc, false, nil); err != nil {
				t.Fatalf("move: %v", err)
			}
			want := filepath.Join(stick, DefaultFolder, FileName)
			if s.Path() != want || s.Location().ID != cand.ID {
				t.Fatalf("after the move: %s %s", s.Path(), s.Location())
			}
			for _, x := range s.Candidates() {
				if x.Path == stick && !x.Current {
					t.Fatalf("the stick is not the current candidate: %+v", x)
				}
			}
			s.Close()

			// a restart: found again
			s2, err := Open(Config{AddonDir: dir, Log: t.Logf, Mounts: mounts})
			if err != nil || s2.Path() != want || s2.Health().State != "ok" {
				t.Fatalf("reopen: %v %s %+v", err, s2.Path(), s2.Health())
			}
			if d, _ := s2.Devices(context.Background()); len(d) != 1 {
				t.Fatalf("devices after the reopen: %d", len(d))
			}
			s2.Close()

			if c.uuid != "" {
				// the stick got a label and another mount point: still the same stick
				other := filepath.Join(filepath.Dir(stick), "elsewhere")
				if err := os.Rename(stick, other); err != nil {
					t.Fatal(err)
				}
				mu.Lock()
				present = []Mount{{Path: other, UUID: c.uuid, Label: "NOWNAMED"}}
				mu.Unlock()
				s3, err := Open(Config{AddonDir: dir, Log: t.Logf, Mounts: mounts})
				if err != nil || s3.Path() != filepath.Join(other, DefaultFolder, FileName) || s3.Health().State != "ok" {
					t.Fatalf("reopen after the label: %v %s", err, s3.Path())
				}
				s3.Close()
				return
			}
			// by mount point: another stick in the same place holds no database, and none is
			// begun there - the store waits as for a stick that is away
			if err := os.Rename(filepath.Join(stick, DefaultFolder), filepath.Join(stick, "moved-away")); err != nil {
				t.Fatal(err)
			}
			s4, err := Open(Config{AddonDir: dir, Log: t.Logf, Mounts: mounts})
			if err != nil {
				t.Fatal(err)
			}
			defer s4.Close()
			if h := s4.Health(); h.State == "ok" || !strings.Contains(h.Error, "another stick") {
				t.Fatalf("another stick at the mount point: %+v", h)
			}
			if _, err := os.Stat(want); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("a database was begun on the other stick: %v", err)
			}
			// the right stick is back
			if err := os.Rename(filepath.Join(stick, "moved-away"), filepath.Join(stick, DefaultFolder)); err != nil {
				t.Fatal(err)
			}
			if !s4.Retry() || s4.Health().State != "ok" {
				t.Fatalf("the stick is back: %+v", s4.Health())
			}
		})
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

func TestInterval(t *testing.T) {
	s, dir := testStore(t, nil)
	settings := filepath.Join(dir, "etc", "battery.json")
	if s.Interval() != IntervalDefault || s.Health().Interval != 30 || s.Health().Fixed {
		t.Fatalf("default: %s %+v", s.Interval(), s.Health())
	}
	if err := SaveLocation(settings, Location{ID: "userfs"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetInterval(7); err == nil {
		t.Fatal("7 minutes accepted")
	}
	if err := s.SetInterval(60); err != nil {
		t.Fatal(err)
	}
	if s.Interval() != time.Hour || s.Health().Interval != 60 {
		t.Fatalf("after SetInterval: %s", s.Interval())
	}
	// the other keys stay, and a new store reads it back
	if loc, err := LoadLocation(settings); err != nil || loc.ID != "userfs" {
		t.Fatalf("the location after SetInterval: %v %v", loc, err)
	}
	if d := LoadInterval(settings, t.Logf); d != time.Hour {
		t.Fatalf("LoadInterval: %s", d)
	}
	for body, want := range map[string]time.Duration{
		`{"write_interval": 5}`:   5 * time.Minute,
		`{"write_interval": 7}`:   IntervalDefault,
		`{"write_interval": "x"}`: IntervalDefault,
		`{}`:                      IntervalDefault,
		`not json`:                IntervalDefault,
	} {
		p := filepath.Join(t.TempDir(), "battery.json")
		os.WriteFile(p, []byte(body), 0o640)
		if d := LoadInterval(p, t.Logf); d != want {
			t.Errorf("%s: %s, want %s", body, d, want)
		}
	}
	if d := LoadInterval(filepath.Join(t.TempDir(), "none.json"), nil); d != IntervalDefault {
		t.Errorf("no file: %s", d)
	}
}

// Run follows the setting: a change writes what waits at once and the new interval counts from
// then; a fixed interval (the -flush flag) wins over the setting.
func TestRunInterval(t *testing.T) {
	s, _ := testStore(t, nil)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { s.Run(ctx, 0); close(done) }()
	s.AddEvent(Event{Interface: "HmIP-RF", Address: "DEVA", TS: 1, Key: "BOOTED", Value: "true"})
	if err := s.SetInterval(1); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for s.Health().Pending != 0 {
		if time.Now().After(deadline) {
			t.Fatal("the change of the interval did not write the batch")
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-done

	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	go s.Run(ctx, time.Hour)
	deadline = time.Now().Add(3 * time.Second)
	for !s.Health().Fixed {
		if time.Now().After(deadline) {
			t.Fatal("not fixed")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if s.Interval() != time.Hour || s.Health().Interval != 60 {
		t.Fatalf("fixed: %s", s.Interval())
	}
}
