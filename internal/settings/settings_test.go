package settings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "etc", "battery.json")
	f, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	if d := f.Get(); d.Shopping.Days != 90 || len(d.Notify.Events) != 3 || d.Notify.Channel != "" {
		t.Fatalf("defaults: %+v", d)
	}
	// other keys of the file survive a save
	os.MkdirAll(filepath.Dir(p), 0o755)
	os.WriteFile(p, []byte(`{"storage": {"location": "userfs"}, "detection": {"window_hours": 24}}`), 0o644)
	s := Defaults()
	s.Shopping.Days = 30
	s.Shopping.Stock = map[string]int{"AA": 4, "AAA": 0}
	s.Notify = Notify{Channel: "ntfy", URL: "https://ntfy.sh/", Topic: "home", Events: []string{"due", "low", "low"}, Language: "en"}
	if err := f.Set(s); err != nil {
		t.Fatal(err)
	}
	var doc map[string]json.RawMessage
	b, _ := os.ReadFile(p)
	if json.Unmarshal(b, &doc) != nil || doc["storage"] == nil || doc["detection"] == nil || doc["shopping"] == nil || doc["notify"] == nil {
		t.Fatalf("file: %s", b)
	}
	g := f.Get()
	if g.Shopping.Days != 30 || g.Shopping.Stock["AA"] != 4 || len(g.Shopping.Stock) != 1 || g.Notify.URL != "https://ntfy.sh" || strings.Join(g.Notify.Events, ",") != "due,low" {
		t.Fatalf("saved: %+v", g)
	}
	// re-read
	f2, err := Open(p)
	if err != nil || f2.Get().Notify.Topic != "home" || f2.Get().Shopping.Days != 30 {
		t.Fatalf("reopen: %+v %v", f2.Get(), err)
	}
	// a copy, not the inside
	g.Shopping.Stock["AA"] = 99
	if f.Get().Shopping.Stock["AA"] != 4 {
		t.Fatal("Get must copy")
	}
}

func TestValidate(t *testing.T) {
	bad := []Settings{
		{Shopping: Shopping{Days: 0}},
		{Shopping: Shopping{Days: 10, Stock: map[string]int{"": 1}}},
		{Shopping: Shopping{Days: 10, Stock: map[string]int{"AA": -1}}},
		{Shopping: Shopping{Days: 10}, Notify: Notify{Channel: "sms"}},
		{Shopping: Shopping{Days: 10}, Notify: Notify{Channel: "webhook", URL: "ftp://x"}},
		{Shopping: Shopping{Days: 10}, Notify: Notify{Channel: "webhook", URL: "not a url"}},
		{Shopping: Shopping{Days: 10}, Notify: Notify{Channel: "ntfy", URL: "https://ntfy.sh"}},
		{Shopping: Shopping{Days: 10}, Notify: Notify{Channel: "ntfy", URL: "https://ntfy.sh", Topic: "a/b"}},
		{Shopping: Shopping{Days: 10}, Notify: Notify{Channel: "webhook", URL: "https://x", Headers: map[string]string{"Bad Name": "v"}}},
		{Shopping: Shopping{Days: 10}, Notify: Notify{Events: []string{"boom"}}},
	}
	for i, s := range bad {
		if err := s.Validate(); err == nil {
			t.Errorf("case %d passed: %+v", i, s)
		}
	}
	ok := Settings{Shopping: Shopping{Days: 10}, Notify: Notify{Channel: "webhook", URL: "http://127.0.0.1:8099/hook/", Language: "fr"}}
	if err := ok.Validate(); err != nil || ok.Notify.URL != "http://127.0.0.1:8099/hook" || ok.Notify.Language != "de" || len(ok.Notify.Events) != 0 {
		t.Errorf("ok: %+v %v", ok, err)
	}
}
