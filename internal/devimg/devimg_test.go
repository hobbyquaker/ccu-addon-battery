package devimg

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

const sample = `# the WebUI's device catalogue
array set DEV_PATHS {HmIP-WRC2 {{50 /config/img/devices/50/112_hmip-wrc2_thumb.png} {250 /config/img/devices/250/112_hmip-wrc2.png}} {ZEL STG RM FWT} {{50 /config/img/devices/50/42_hm-cc-tc_thumb.png} {250 /config/img/devices/250/42_hm-cc-tc.png}} HM-Sec-SC {{50 /config/img/devices/50/sc_thumb.png}} EVIL {{250 /config/img/devices/250/../../x.png}} OTHER {{250 /etc/passwd}}}
array set DEV_DESCRIPTION {HmIP-WRC2 {{"2" 4 0.540 0.366 0.04 0.044}}}
`

func TestParse(t *testing.T) {
	m := Parse(sample)
	want := map[string]string{
		"hmip-wrc2":      "/config/img/devices/250/112_hmip-wrc2.png",
		"zel stg rm fwt": "/config/img/devices/250/42_hm-cc-tc.png",
		"hm-sec-sc":      "/config/img/devices/50/sc_thumb.png",
	}
	if len(m) != len(want) {
		t.Fatalf("got %v", m)
	}
	for k, v := range want {
		if m[k] != v {
			t.Errorf("%s: %q, want %q", k, m[k], v)
		}
	}
	if len(Parse("nothing here")) != 0 {
		t.Error("a file without DEV_PATHS")
	}
}

func TestCatalog(t *testing.T) {
	p := filepath.Join(t.TempDir(), "DEVDB.tcl")
	c := &Catalog{Path: p}
	if c.Image("HmIP-WRC2") != "" {
		t.Error("no file: a picture")
	}
	os.WriteFile(p, []byte(sample), 0o644)
	for _, in := range []string{"HmIP-WRC2", "HMIP-WRC2", "hmip-wrc2"} {
		if got := c.Image(in); got != "/config/img/devices/250/112_hmip-wrc2.png" {
			t.Errorf("%s: %q", in, got)
		}
	}
	if c.Image("") != "" || c.Image("HmIP-NOPE") != "" {
		t.Error("unknown models have no picture")
	}
	// a changed file is read again
	os.WriteFile(p, []byte(`array set DEV_PATHS {HmIP-WRC2 {{250 /config/img/devices/250/new.png}}}`), 0o644)
	future := time.Now().Add(2 * time.Second)
	os.Chtimes(p, future, future)
	if got := c.Image("HmIP-WRC2"); got != "/config/img/devices/250/new.png" {
		t.Errorf("after the change: %q", got)
	}
}
