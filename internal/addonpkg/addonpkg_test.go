// Package addonpkg tests the package files the addon ships: the manifest as the system would
// validate it at install, and the lighttpd fragment `ccu-addon-kit vendor` writes from kit.json.
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

// The fragment is the kit CLI's output (kit.json: proxied /addons/battery/ to the port, the
// settings page in its own directory /addons/battery-config/). It must say what the Go side's
// renderer says - the redirect of the bare path, the proxy to the port, the 503 page -, written
// in the CLI's shape; the two renderers are to become one (ccu-addon-kit).
func TestFragment(t *testing.T) {
	ref, err := fragment.Render(fragment.Fragment{ID: "battery", Port: Port, ErrorfilePrefix: "/usr/local/addons/battery/www/lighttpd-error-"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile("../../addon/battery/etc/lighttpd.conf")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`$HTTP["url"] == "/addons/battery" {`,
		`url.redirect = ("" => "/addons/battery/")`,
		`$HTTP["url"] =~ "^/addons/battery/" {`,
		`("host" => "127.0.0.1", "port" => 8291)`,
		`server.errorfile-prefix = "/usr/local/addons/battery/www/lighttpd-error-"`,
	} {
		if !strings.Contains(string(got), want) {
			t.Errorf("the shipped fragment lacks %q:\n%s", want, got)
		}
	}
	for _, want := range []string{"127.0.0.1", "8291", "lighttpd-error-"} {
		if !strings.Contains(ref, want) {
			t.Errorf("the Go renderer lacks %q", want)
		}
	}
	if strings.Contains(string(got), "battery-config") {
		t.Error("the fragment must not touch /addons/battery-config/ (the web server serves it)")
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
