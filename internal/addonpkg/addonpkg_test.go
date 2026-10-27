// Package addonpkg tests the package files the addon ships: the manifest as the system would
// validate it at install, and the lighttpd fragment as the kit renders it.
package addonpkg

import (
	"os"
	"strings"
	"testing"

	"github.com/hobbyquaker/ccu-addon-kit/go/addonkit/fragment"
	"github.com/hobbyquaker/ccu-addon-kit/go/addonkit/manifest"
)

// Port is the loopback port the daemon listens on and the fragment proxies to.
const Port = 8291

func TestManifest(t *testing.T) {
	m, err := manifest.Load("../../addon/openccu-lite.json")
	if err != nil {
		t.Fatal(err)
	}
	if m.ID != "battery" || !m.UI.SessionHeader || m.Undeclared() || !m.Runtime.Daemon {
		t.Fatalf("manifest: %+v", m)
	}
	for _, want := range []string{"rpc:read", "meta:read"} {
		if !strings.Contains(strings.Join(m.Runtime.APIScopes, ","), want) {
			t.Errorf("scope %s missing", want)
		}
	}
	if m.Description["de"] == "" || m.Description["en"] == "" || m.Runtime.Note["de"] == "" || m.Runtime.Note["en"] == "" {
		t.Error("texts in both languages")
	}
}

func TestFragment(t *testing.T) {
	want, err := fragment.Render(fragment.Fragment{ID: "battery", Port: Port, ErrorfilePrefix: "/usr/local/addons/battery/www/lighttpd-error-"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile("../../addon/battery/etc/lighttpd.conf")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("the shipped fragment differs from the kit's rendering:\n%s\n--- want\n%s", got, want)
	}
}

func TestScriptsAreExecutable(t *testing.T) {
	for _, f := range []string{"../../addon/update_script", "../../addon/battery/rc.d"} {
		st, err := os.Stat(f)
		if err != nil {
			t.Fatal(err)
		}
		if st.Mode()&0o111 == 0 {
			t.Errorf("%s is not executable", f)
		}
		b, _ := os.ReadFile(f)
		if strings.Contains(string(b), "\r") {
			t.Errorf("%s has CR line endings", f)
		}
	}
}
