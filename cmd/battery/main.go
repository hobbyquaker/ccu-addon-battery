// battery is the ccu-addon-battery daemon: it records the battery datapoints of every device from
// openccu-lite's APIs into its SQLite store and serves its web app and API on the loopback,
// behind lighttpd's proxy of /addons/battery/.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/hobbyquaker/ccu-addon-battery/internal/changes"
	"github.com/hobbyquaker/ccu-addon-battery/internal/detect"
	"github.com/hobbyquaker/ccu-addon-battery/internal/devimg"
	"github.com/hobbyquaker/ccu-addon-battery/internal/httpapi"
	"github.com/hobbyquaker/ccu-addon-battery/internal/notify"
	"github.com/hobbyquaker/ccu-addon-battery/internal/recorder"
	"github.com/hobbyquaker/ccu-addon-battery/internal/settings"
	"github.com/hobbyquaker/ccu-addon-battery/internal/store"
	"github.com/hobbyquaker/ccu-addon-kit/go/addonkit"
	"github.com/hobbyquaker/ccu-addon-kit/go/litesource"
)

// version is set by the build (-ldflags "-X main.version=…").
var version = "dev"

func main() {
	log.SetFlags(0) // the journal adds the time
	addonDir := flag.String("addon-dir", "/usr/local/addons/battery", "the addon's directory (etc/, var/, app/)")
	id := flag.String("id", "battery", "the addon id (token file, base path)")
	port := flag.Int("port", 8291, "the loopback port lighttpd proxies to")
	base := flag.String("system", "http://127.0.0.1", "openccu-lite's URL")
	basePath := flag.String("base-path", "", "the path the app is served at (/addons/<id> by default)")
	www := flag.String("www", "", "the built web app (<addon-dir>/app by default; www/ holds the settings page)")
	tokenFile := flag.String("token-file", "", "a token file for the system's API (the addon's own token by default)")
	flush := flag.Duration("flush", 10*time.Minute, "how often the RAM batch is written to the database")
	noSession := flag.Bool("no-session-check", false, "serve without the session check (development on a system that is not openccu-lite)")
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Println(version)
		return
	}
	if *basePath == "" {
		*basePath = "/addons/" + *id
	}
	if *www == "" {
		*www = filepath.Join(*addonDir, "app")
	}
	if err := run(*addonDir, *id, *port, *base, *basePath, *www, *tokenFile, *flush, *noSession); err != nil {
		log.Fatalf("battery: %v", err)
	}
}

func run(addonDir, id string, port int, base, basePath, www, tokenFile string, flush time.Duration, noSession bool) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	st, err := store.Open(store.Config{AddonDir: addonDir, Log: log.Printf})
	if err != nil {
		return err
	}
	defer st.Close()
	go st.Run(ctx, flush)

	var token addonkit.TokenSource
	if tokenFile != "" {
		token = &addonkit.FileToken{Path: tokenFile}
	} else {
		token = addonkit.AddonToken(id)
	}
	src := litesource.New(base, token)
	src.Log = log.Printf

	// wait for the system: before the token exists or occulited answers there is nothing to do
	for {
		if _, err := src.Probe(ctx); err == nil {
			break
		} else if err == litesource.ErrNotOpenccuLite {
			return fmt.Errorf("%s is not openccu-lite", base)
		} else {
			log.Printf("battery: waiting for the system: %v", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(5 * time.Second):
		}
	}

	params, merge := detectionSettings(filepath.Join(addonDir, "etc", "battery.json"))
	chg := changes.New(changes.Options{Store: st, Params: params, MergeWindow: merge, Log: log.Printf})
	go chg.Run(ctx)
	log.Printf("battery: detection window %s, merge window %s", params.Window, merge)

	rec := recorder.New(recorder.Options{Source: src, Store: st, Observer: chg, Log: log.Printf})
	go func() {
		if err := rec.Run(ctx); err != nil && ctx.Err() == nil {
			log.Printf("battery: recorder ended: %v", err)
		}
	}()

	sf, err := settings.Open(filepath.Join(addonDir, "etc", "battery.json"))
	if err != nil {
		log.Printf("battery: %v (defaults until the settings are saved again)", err)
		sf = settings.Fallback(filepath.Join(addonDir, "etc", "battery.json"))
	}
	notifier := notify.New(func() settings.Notify { return sf.Get().Notify }, st)
	notifier.Log = log.Printf

	var checker *addonkit.Checker
	if !noSession {
		checker = addonkit.NewChecker(addonkit.SessionOptions{Base: base, Need: addonkit.LevelUser, OnRefused: func(r *http.Request, reason string) {
			log.Printf("battery: refused %s %s: %s", r.Method, r.URL.Path, reason)
		}})
	}
	opts := httpapi.Options{BasePath: basePath, WWW: www, Store: st, Recorder: rec, Changes: chg, Images: &devimg.Catalog{}, Settings: sf, Notifier: notifier, Session: checker, Version: version, Started: time.Now()}
	h := httpapi.New(opts)
	go notifier.Run(ctx, 15*time.Minute, opts.Events)
	l, err := addonkit.Listen(port, "")
	if err != nil {
		return err
	}
	srv := &http.Server{Handler: h, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(sctx)
	}()
	log.Printf("battery %s: serving %s on %s, store %s", version, basePath, l.Addr(), st.Health().State)
	if err := srv.Serve(l); err != nil && err != http.ErrServerClosed {
		return err
	}
	log.Printf("battery: stopping, writing the batch")
	if err := st.Close(); err != nil {
		log.Printf("battery: closing the store: %v", err)
	}
	os.Stdout.Sync()
	return nil
}

// detectionSettings reads the "detection" key of etc/battery.json: {"window_hours": 24,
// "merge_window_hours": 48, "expire_hours": 168}. Absent or zero keys keep the defaults.
func detectionSettings(path string) (detect.Params, time.Duration) {
	p := detect.DefaultParams()
	merge := 48 * time.Hour
	b, err := os.ReadFile(path)
	if err != nil {
		return p, merge
	}
	var f struct {
		Detection struct {
			WindowHours      float64 `json:"window_hours"`
			ExpireHours      float64 `json:"expire_hours"`
			MergeWindowHours float64 `json:"merge_window_hours"`
		} `json:"detection"`
	}
	if err := json.Unmarshal(b, &f); err != nil {
		log.Printf("battery: %s: %v (detection defaults)", path, err)
		return p, merge
	}
	hours := func(h float64) time.Duration { return time.Duration(h * float64(time.Hour)) }
	if h := f.Detection.WindowHours; h > 0 {
		p.Window = hours(h)
	}
	if h := f.Detection.ExpireHours; h > 0 {
		p.Expire = hours(h)
	}
	if h := f.Detection.MergeWindowHours; h > 0 {
		merge = hours(h)
	}
	return p, merge
}
