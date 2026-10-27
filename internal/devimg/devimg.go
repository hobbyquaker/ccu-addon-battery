// Package devimg maps a device model to its picture: the CCU WebUI's catalogue
// /www/config/devdescr/DEVDB.tcl names, in its DEV_PATHS array, the picture of every model in two
// sizes (50 and 250 px) under /config/img/devices/, which the CCU3, OpenCCU and openccu-lite all
// serve at those URLs - so the addon's page can show the same pictures Homematic Manager shows,
// without shipping any. The file is read once and again when its modification time changes.
package devimg

import (
	"os"
	"regexp"
	"strings"
	"sync"
	"time"
)

// DefaultPath is the catalogue on every CCU firmware and on openccu-lite.
const DefaultPath = "/www/config/devdescr/DEVDB.tcl"

// Catalog answers a model's picture.
type Catalog struct {
	// Path is the Tcl file; DefaultPath when empty.
	Path string

	mu    sync.Mutex
	mtime time.Time
	size  int64
	paths map[string]string // lower-case model -> the 250 px picture's URL path
}

// Image is the URL path of a model's picture ("/config/img/devices/250/112_hmip-wrc2.png"), ""
// when the catalogue has none. The lookup ignores case (HMIP-WRC2 / HmIP-WRC2).
func (c *Catalog) Image(model string) string {
	if model == "" {
		return ""
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.loadLocked()
	return c.paths[strings.ToLower(model)]
}

func (c *Catalog) loadLocked() {
	p := c.Path
	if p == "" {
		p = DefaultPath
	}
	fi, err := os.Stat(p)
	if err != nil {
		c.paths, c.mtime, c.size = nil, time.Time{}, 0
		return
	}
	if c.paths != nil && fi.ModTime().Equal(c.mtime) && fi.Size() == c.size {
		return
	}
	b, err := os.ReadFile(p)
	if err != nil {
		c.paths, c.mtime, c.size = nil, time.Time{}, 0
		return
	}
	c.paths = Parse(string(b))
	c.mtime, c.size = fi.ModTime(), fi.Size()
}

// an entry of DEV_PATHS: `<model> {{50 <path>} {250 <path>}}`, the model bare or in braces
var entry = regexp.MustCompile(`(?s)(?:\{([^{}]+)\}|([^\s{}]+))\s+\{((?:\s*\{\s*\d+\s+[^\s{}]+\s*\})+)\s*\}`)
var size = regexp.MustCompile(`\{\s*(\d+)\s+([^\s{}]+)\s*\}`)

// Parse reads the DEV_PATHS array out of the Tcl file: model -> the 250 px picture (the 50 px one
// when a model has only that), keyed in lower case. A path that does not start with
// /config/img/devices/ or that climbs with ".." is left out.
func Parse(tcl string) map[string]string {
	out := map[string]string{}
	i := strings.Index(tcl, "DEV_PATHS")
	if i < 0 {
		return out
	}
	body := tcl[i:]
	if j := strings.Index(body, "{"); j >= 0 {
		body = body[j+1:]
	}
	for _, m := range entry.FindAllStringSubmatch(body, -1) {
		model := m[1]
		if model == "" {
			model = m[2]
		}
		best, bestSize := "", -1
		for _, s := range size.FindAllStringSubmatch(m[3], -1) {
			path := s[2]
			if !strings.HasPrefix(path, "/config/img/devices/") || strings.Contains(path, "..") {
				continue
			}
			n := len(s[1])
			if s[1] == "250" {
				n = 1000
			}
			if n > bestSize {
				best, bestSize = path, n
			}
		}
		if best != "" {
			out[strings.ToLower(strings.TrimSpace(model))] = best
		}
	}
	return out
}
