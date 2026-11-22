package addonpkg

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The architecture check at the top of update_script: a package the system cannot run is refused
// with exit 13 before the script touches anything (a package for the wrong architecture installed
// and then failed to start with `syntax error: unexpected ")"`). The check is run as it stands in
// the script, with uname and logger faked; scripts/install-test.sh runs the whole script.
func TestArchCheck(t *testing.T) {
	script, err := os.ReadFile("../../addon/update_script")
	if err != nil {
		t.Fatal(err)
	}
	s := string(script)
	begin := strings.Index(s, "# --- architecture check ---")
	end := strings.Index(s, "# --- end of the architecture check ---")
	if begin < 0 || end < begin {
		t.Fatal("update_script: the architecture check's markers are missing")
	}
	// nothing that touches the system comes before the check
	for _, line := range strings.Split(s[:begin], "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") {
			continue
		}
		for _, verb := range []string{"mount", "stop", "rm ", "mkdir", "cp ", "ln "} {
			if strings.Contains(line, verb) {
				t.Errorf("update_script runs %q before the architecture check", line)
			}
		}
	}
	check := "ADDON=battery\n" + s[begin:end] + "exit 0\n"

	cases := []struct {
		machine, pkg string
		ok           bool
		right        string
	}{
		{"x86_64", "x86_64", true, ""},
		{"x86_64", "aarch64", false, "battery-x86_64-1.5.2.tar.gz"},
		{"x86_64", "armv7l", false, "battery-x86_64-1.5.2.tar.gz"},
		{"aarch64", "aarch64", true, ""},
		{"aarch64", "armv7l", true, ""},
		{"aarch64", "x86_64", false, "battery-aarch64-1.5.2.tar.gz"},
		{"armv7l", "armv7l", true, ""},
		{"armv7l", "aarch64", false, "battery-armv7l-1.5.2.tar.gz"},
		{"armv8l", "armv7l", true, ""},
		{"armv8l", "aarch64", false, "battery-armv7l-1.5.2.tar.gz"},
		{"armv6l", "armv7l", false, "kein Paket"},
		{"x86_64", "", false, "battery-x86_64-1.5.2.tar.gz"},
	}
	for _, c := range cases {
		t.Run(c.machine+"/"+c.pkg, func(t *testing.T) {
			dir := t.TempDir()
			bin := filepath.Join(dir, "fake")
			must(t, os.MkdirAll(bin, 0o755))
			must(t, os.MkdirAll(filepath.Join(dir, "pkg", "battery"), 0o755))
			must(t, os.WriteFile(filepath.Join(bin, "uname"), []byte("#!/bin/sh\necho "+c.machine+"\n"), 0o755))
			must(t, os.WriteFile(filepath.Join(bin, "logger"), []byte("#!/bin/sh\necho \"$@\" >> "+filepath.Join(dir, "logger")+"\n"), 0o755))
			if c.pkg != "" {
				must(t, os.WriteFile(filepath.Join(dir, "pkg", "ARCH"), []byte(c.pkg+"\n"), 0o644))
			}
			must(t, os.WriteFile(filepath.Join(dir, "pkg", "battery", "versions"), []byte("VERSION_ADDON=1.5.2\n"), 0o644))
			must(t, os.WriteFile(filepath.Join(dir, "check.sh"), []byte(check), 0o755))
			cmd := exec.Command("sh", filepath.Join(dir, "check.sh"))
			cmd.Dir = filepath.Join(dir, "pkg")
			cmd.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"))
			out, err := cmd.CombinedOutput()
			code := 0
			if ee, ok := err.(*exec.ExitError); ok {
				code = ee.ExitCode()
			} else if err != nil {
				t.Fatal(err)
			}
			if c.ok {
				if code != 0 || len(out) != 0 {
					t.Fatalf("refused: exit %d, %s", code, out)
				}
				return
			}
			if code != 13 {
				t.Fatalf("exit %d, want 13: %s", code, out)
			}
			for _, want := range []string{c.right, "dieses System ist " + c.machine, "this system is " + c.machine, "unverändert", "unchanged"} {
				if !strings.Contains(string(out), want) {
					t.Errorf("message lacks %q:\n%s", want, out)
				}
			}
			logged, _ := os.ReadFile(filepath.Join(dir, "logger"))
			if !strings.Contains(string(logged), "-t battery") || !strings.Contains(string(logged), "this system is "+c.machine) {
				t.Errorf("not in the system log: %q", logged)
			}
		})
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
