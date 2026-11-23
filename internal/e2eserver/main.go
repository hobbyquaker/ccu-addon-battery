// Command e2eserver runs the addon's API and the built web app against a fake openccu-lite
// (the kit's fakelite) for the browser tests in ui/e2e: the real store, recorder, battery table
// and overrides, with a few devices - two of a model the table has, two of a self-built model it
// lacks - and every request signed in. Development and CI only; it is not in the package.
//
//	go run ./internal/e2eserver -addr 127.0.0.1:8795 -www ui/dist
package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/hobbyquaker/ccu-addon-battery/internal/changes"
	"github.com/hobbyquaker/ccu-addon-battery/internal/devimg"
	"github.com/hobbyquaker/ccu-addon-battery/internal/httpapi"
	"github.com/hobbyquaker/ccu-addon-battery/internal/notify"
	"github.com/hobbyquaker/ccu-addon-battery/internal/recorder"
	"github.com/hobbyquaker/ccu-addon-battery/internal/settings"
	"github.com/hobbyquaker/ccu-addon-battery/internal/store"
	"github.com/hobbyquaker/ccu-addon-kit/go/addonkit"
	"github.com/hobbyquaker/ccu-addon-kit/go/litesource"
	"github.com/hobbyquaker/ccu-addon-kit/go/litesource/fakelite"
)

const sid = "E2EE2EE2EE2EE2EE2EE2EE2EE2"

func main() {
	addr := flag.String("addr", "127.0.0.1:8795", "where to listen")
	www := flag.String("www", "ui/dist", "the built web app")
	flag.Parse()

	s := fakelite.New()
	defer s.Close()
	s.Sessions[sid] = fakelite.Session{User: "e2e", Level: "configure"}
	s.Interfaces = []string{"HmIP-RF", "BidCos-RF"}
	maint := func(addr, model, fw string) []map[string]any {
		return []map[string]any{
			{"ADDRESS": addr, "TYPE": model, "FIRMWARE": fw, "VERSION": 1, "CHILDREN": []string{addr + ":0"}},
			{"ADDRESS": addr + ":0", "TYPE": "MAINTENANCE", "PARENT": addr},
		}
	}
	// two remotes the table knows (2x AAA), two self-built sensors it lacks (LOWBAT only)
	s.Devices["HmIP-RF"] = append(maint("WRC2A", "HmIP-WRC2", "1.2"), maint("WRC2B", "HmIP-WRC2", "1.2")...)
	s.Devices["BidCos-RF"] = append(maint("HBSEN0001", "HB-UNI-Sen-TEMP-DS18B20", "1.0"), maint("HBSEN0002", "HB-UNI-Sen-TEMP-DS18B20", "1.0")...)
	s.Paramsets["HmIP-RF/HmIP-WRC2/1.2/1/MAINTENANCE/VALUES"] = map[string]any{"OPERATING_VOLTAGE": map[string]any{"TYPE": "FLOAT"}, "LOW_BAT": map[string]any{"TYPE": "BOOL"}, "UNREACH": map[string]any{"TYPE": "BOOL"}}
	s.Paramsets["BidCos-RF/HB-UNI-Sen-TEMP-DS18B20/1.0/1/MAINTENANCE/VALUES"] = map[string]any{"LOWBAT": map[string]any{"TYPE": "BOOL"}, "UNREACH": map[string]any{"TYPE": "BOOL"}}
	s.Values["HmIP-RF/WRC2A:0/VALUES"] = map[string]any{"OPERATING_VOLTAGE": 2.9, "LOW_BAT": false, "UNREACH": false}
	s.Values["HmIP-RF/WRC2B:0/VALUES"] = map[string]any{"OPERATING_VOLTAGE": 3.0, "LOW_BAT": false, "UNREACH": false}
	s.Values["BidCos-RF/HBSEN0001:0/VALUES"] = map[string]any{"LOWBAT": false, "UNREACH": false}
	s.Values["BidCos-RF/HBSEN0002:0/VALUES"] = map[string]any{"LOWBAT": false, "UNREACH": false}
	// a self-built sensor with its battery voltage on channel 1 (BATTERY_VOLTAGE), seeded from the
	// state store as BidCos values are
	s.Devices["BidCos-RF"] = append(s.Devices["BidCos-RF"],
		map[string]any{"ADDRESS": "HBIAQ0001", "TYPE": "HB-UNI-Sen-IAQ", "FIRMWARE": "1.0", "VERSION": 1, "CHILDREN": []string{"HBIAQ0001:0", "HBIAQ0001:1"}},
		map[string]any{"ADDRESS": "HBIAQ0001:0", "TYPE": "MAINTENANCE", "PARENT": "HBIAQ0001"},
		map[string]any{"ADDRESS": "HBIAQ0001:1", "TYPE": "WEATHER", "PARENT": "HBIAQ0001"})
	s.Paramsets["BidCos-RF/HB-UNI-Sen-IAQ/1.0/1/MAINTENANCE/VALUES"] = map[string]any{"LOWBAT": map[string]any{"TYPE": "BOOL"}, "UNREACH": map[string]any{"TYPE": "BOOL"}}
	s.Paramsets["BidCos-RF/HB-UNI-Sen-IAQ/1.0/1/WEATHER/VALUES"] = map[string]any{"BATTERY_VOLTAGE": map[string]any{"TYPE": "FLOAT", "UNIT": "V"}, "IAQ_LEVEL": map[string]any{"TYPE": "INTEGER"}}
	s.State = append(s.State, map[string]any{"interface": "BidCos-RF", "address": "HBIAQ0001:1", "datapoint": "BATTERY_VOLTAGE", "value": 2.95, "ts": time.Now().UTC().Format(time.RFC3339), "confirmed": true, "source": "event"})

	dir, err := os.MkdirTemp("", "battery-e2e-")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(dir)
	st, err := store.Open(store.Config{AddonDir: dir, Log: log.Printf, Mounts: func() []store.Mount { return nil }})
	if err != nil {
		log.Fatal(err)
	}
	defer st.Close()
	chg := changes.New(changes.Options{Store: st, Log: log.Printf})
	rec := recorder.New(recorder.Options{Source: litesource.New(s.URL, nil), Store: st, Observer: chg, Log: log.Printf})
	sf := settings.Fallback(filepath.Join(dir, "etc", "battery.json"))
	o := httpapi.Options{
		BasePath: "/addons/battery", WWW: *www, Store: st, Recorder: rec, Changes: chg,
		Images: &devimg.Catalog{Path: filepath.Join(dir, "DEVDB.tcl")}, Settings: sf,
		Notifier: notify.New(func() settings.Notify { return sf.Get().Notify }, st),
		Session:  addonkit.NewChecker(addonkit.SessionOptions{Base: s.URL, Platform: func() addonkit.Kind { return addonkit.OpenccuLite }}),
		Version:  "e2e", Started: time.Now(),
	}
	h := httpapi.New(o)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	go rec.Run(ctx)
	srv := &http.Server{Addr: *addr, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Header.Set(addonkit.HeaderSession, sid) // lighttpd's gate on a real system
		h.ServeHTTP(w, r)
	})}
	go func() {
		<-ctx.Done()
		srv.Close()
	}()
	log.Printf("e2eserver: http://%s/addons/battery/", *addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Print(err)
	}
}
