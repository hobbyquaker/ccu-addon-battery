package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/hobbyquaker/ccu-addon-kit/go/addonkit"
	"github.com/hobbyquaker/ccu-addon-kit/go/litesource/fakelite"
)

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// TestRunOutlivesTheSystemsStart is the boot order on openccu-lite: the addon's unit starts a few
// seconds after occulited's, while the web server still answers every API request with 503
// "starting" and Retry-After: 5 - here three times, so the system serves after ~15 s. The daemon
// must wait, not exit (a 1.0.0 took the 503 for "not openccu-lite" and stayed failed until
// started by hand).
func TestRunOutlivesTheSystemsStart(t *testing.T) {
	if testing.Short() {
		t.Skip("waits 15 s for the fake system")
	}
	s := fakelite.New()
	defer s.Close()
	s.Token = "olt_test"
	s.SetStarting(3)
	dir := t.TempDir()
	tokenFile := filepath.Join(dir, "token")
	if err := os.WriteFile(tokenFile, []byte("olt_test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	port := freePort(t)
	done := make(chan error, 1)
	started := time.Now()
	go func() {
		done <- run(dir, "battery", port, s.URL, "/addons/battery", filepath.Join(dir, "app"), tokenFile, time.Minute, true, addonkit.OpenccuLite, 0)
	}()
	health := fmt.Sprintf("http://127.0.0.1:%d/addons/battery/api/v1/health", port)
	deadline := time.Now().Add(40 * time.Second)
	for {
		res, err := http.Get(health)
		if err == nil {
			res.Body.Close()
			if res.StatusCode == 200 {
				break
			}
		}
		select {
		case err := <-done:
			t.Fatalf("run ended while the system was starting: %v", err)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatalf("the daemon did not serve within 40 s (system answered starting %d times)", s.StartingSeen)
		}
		time.Sleep(200 * time.Millisecond)
	}
	if d := time.Since(started); d < 14*time.Second || s.StartingSeen != 3 {
		t.Fatalf("served after %v with %d starting answers; want ~15 s and 3 (one probe per Retry-After)", d, s.StartingSeen)
	}
	syscall.Kill(os.Getpid(), syscall.SIGTERM)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run: %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("run did not end on SIGTERM")
	}
}

// TestRunEndsOnACCU: off openccu-lite a system that answers 404 for the metadata API is not
// openccu-lite, and the daemon says so instead of waiting forever.
func TestRunEndsOnACCU(t *testing.T) {
	if addonkit.IsOpenccuLite() {
		t.Skip("on openccu-lite the wait never ends on a 404")
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: http.NotFoundHandler()}
	go srv.Serve(l)
	defer srv.Close()
	dir := t.TempDir()
	tokenFile := filepath.Join(dir, "token")
	os.WriteFile(tokenFile, []byte("olt_test\n"), 0o600)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- run(dir, "battery", freePort(t), "http://"+l.Addr().String(), "/addons/battery", filepath.Join(dir, "app"), tokenFile, time.Minute, true, addonkit.OpenccuLite, 0)
	}()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "is not openccu-lite") {
			t.Fatalf("run: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("run did not end on a CCU's 404")
	}
}
