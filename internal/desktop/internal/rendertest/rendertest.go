// Package rendertest renders views headlessly for visual review against the
// FlowLens 1.0.5 reference screenshots. Only tests import it.
//
// Renders are written only when FLOWLENS_RENDER_DIR names a directory, so
// ordinary test runs write nothing:
//
//	FLOWLENS_RENDER_DIR=/tmp/renders go test ./desktop/capture -run Render
package rendertest

import (
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	proxyservice "github.com/josexy/flowlens/internal/services/proxy_service"
)

// EnvDir names the environment variable that enables writing renders.
const EnvDir = "FLOWLENS_RENDER_DIR"

// The reference screenshots are 2560×1380 at 1.6 scale, which is about this
// many points.
const (
	Width  = 1600
	Height = 860
)

// Enabled reports whether renders are written.
func Enabled() bool { return os.Getenv(EnvDir) != "" }

// Shoot renders the tester's next frame and writes it as name.png when
// FLOWLENS_RENDER_DIR is set. It returns the written path, or "" when
// rendering is off.
func Shoot(t testing.TB, tester *ui.Tester, name string) string {
	t.Helper()
	dir := os.Getenv(EnvDir)
	if dir == "" {
		return ""
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("render dir: %v", err)
	}
	tester.Frame()
	path := filepath.Join(dir, fileName(name)+".png")
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("render %s: %v", name, err)
	}
	defer file.Close()
	if err := png.Encode(file, tester.Image()); err != nil {
		t.Fatalf("render %s: %v", name, err)
	}
	return path
}

// ShootThemes renders name-light and name-dark. setDark switches the view's
// theme; Shoot runs after a few settled frames in each theme. When rendering
// is off it does nothing, and setDark is not called.
func ShootThemes(t testing.TB, tester *ui.Tester, name string, setDark func(dark bool)) []string {
	t.Helper()
	if !Enabled() {
		return nil
	}
	var paths []string
	for _, dark := range []bool{false, true} {
		setDark(dark)
		for range 3 {
			tester.Frame()
		}
		suffix := "light"
		if dark {
			suffix = "dark"
		}
		paths = append(paths, Shoot(t, tester, name+"-"+suffix))
	}
	setDark(false)
	tester.Frame()
	return paths
}

func fileName(name string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			return r
		default:
			return '-'
		}
	}, name)
}

// Highlighted lists the fixture IDs the reference screenshot shows with a
// highlight, and the color each uses.
var Highlighted = map[uint64]string{77: "#eab308", 72: "#a855f7"}

// Inspected is the fixture ID the reference screenshot shows in the detail
// panel.
const Inspected uint64 = 74

type row struct {
	id       uint64
	method   string
	host     string
	path     string
	status   int
	proto    string
	ip       string
	duration time.Duration
	size     int64
}

var rows = []row{
	{81, "GET", "i1.hdslb.com", "/bfs/archive/44730ed56ff3ffaaadf38c2c1c7a.jpg", 200, "HTTP/1.1", "121.12.126.153", 11 * time.Millisecond, 9615},
	{80, "GET", "i1.hdslb.com", "/bfs/banner/158d5067d551d331fc96bc.png", 200, "HTTP/1.1", "121.12.126.153", 14 * time.Millisecond, 48005},
	{79, "GET", "s1.hdslb.com", "/bfs/seed/jinkela/short/b-mirror/biliMirror.umd.js", 304, "HTTP/2.0", "183.60.152.183", 15 * time.Millisecond, 2079},
	{78, "GET", "s1.hdslb.com", "/bfs/seed/jinkela/short/bmg/register.js", 304, "HTTP/2.0", "183.60.152.183", 13 * time.Millisecond, 2079},
	{77, "GET", "hector.baidu.com", "/static/h.gif", 200, "HTTP/1.1", "36.110.192.200", 99 * time.Millisecond, 19793},
	{76, "GET", "auth.grammarly.com", "/auth/v5/api/userinfo", 200, "HTTP/2.0", "54.161.185.188", 296 * time.Millisecond, 3482},
	{75, "GET", "base.itab.link", "/stat/engine", 200, "HTTP/2.0", "58.42.14.220", 90 * time.Millisecond, 1628},
	{74, "GET", "www.bilibili.com", "/", 200, "HTTP/2.0", "58.63.237.169", 496 * time.Millisecond, 48189},
	{73, "POST", "www.google-analytics.com", "/g/collect", 204, "HTTP/2.0", "113.108.239.225", 29 * time.Millisecond, 1935},
	{72, "OPTIONS", "base.itab.link", "/stat/engine", 204, "HTTP/2.0", "58.42.14.220", 90 * time.Millisecond, 1249},
	{71, "GET", "filters.adtidy.org", "/extension/edge/patches/2/2-s-1790000.patch", 304, "HTTP/1.1", "31.131.197.240", 325 * time.Millisecond, 938},
	{70, "POST", "mbd.baidu.com", "/ztbox", 200, "HTTP/1.1", "183.2.172.69", 13 * time.Millisecond, 2468},
	{69, "POST", "mbd.baidu.com", "/ztbox", 200, "HTTP/1.1", "183.2.172.69", 18 * time.Millisecond, 2744},
	{68, "CONNECT", "edge.microsoft.com", "", 200, "HTTP/1.1", "13.107.21.239", 41 * time.Millisecond, 0},
	{67, "DELETE", "api.example.com", "/v1/sessions/42", 404, "HTTP/2.0", "93.184.216.34", 61 * time.Millisecond, 120},
	{66, "PUT", "api.example.com", "/v1/profile", 500, "HTTP/2.0", "93.184.216.34", 812 * time.Millisecond, 310},
}

// Traffic returns fixture entries resembling the reference screenshot, newest
// first: common methods including OPTIONS and CONNECT, 2xx through 5xx
// statuses, one browser process, and HTTP/1.1 and HTTP/2 rows.
func Traffic() []*proxyservice.TrafficEntry {
	base := time.Date(2026, 10, 3, 10, 37, 0, 0, time.UTC)
	entries := make([]*proxyservice.TrafficEntry, 0, len(rows))
	for index, r := range rows {
		started := base.Add(time.Duration(index) * time.Second)
		startMicros := started.UnixMicro()
		endMicros := started.Add(r.duration).UnixMicro()
		url := fmt.Sprintf("https://%s%s", r.host, r.path)
		if r.method == "CONNECT" {
			url = r.host + ":443"
		}
		headers := []proxyservice.HTTPHeaderField{
			{Name: "host", Value: r.host},
			{Name: "user-agent", Value: "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/154.0.0.0 Safari/537.36 Edg/154.0.0.0"},
			{Name: "accept", Value: "*/*"},
		}
		entries = append(entries, &proxyservice.TrafficEntry{
			ID:         r.id,
			Revision:   1,
			Type:       "https",
			StartedAt:  started,
			Method:     r.method,
			URL:        url,
			Host:       r.host,
			Path:       r.path,
			StatusCode: r.status,
			Status:     fmt.Sprint(r.status),
			Metadata: &proxyservice.Metadata{
				RemoteDestinationAddr: r.ip + ":443",
				Process: &proxyservice.ProcessInfo{
					Status:      proxyservice.ProcessStatusResolved,
					PID:         4242,
					DisplayName: "Microsoft Edge",
					ProcessName: "msedge",
				},
			},
			Request: &proxyservice.HTTPMessage{
				Proto:        r.proto,
				HeaderFields: headers,
				Metrics:      &proxyservice.HTTPMessageMetrics{StartedAtMicros: startMicros, EndedAtMicros: startMicros, HeaderSize: 420, BodySize: 0, State: proxyservice.HTTPMessageStateCompleted},
			},
			Response: &proxyservice.HTTPMessage{
				Proto:        r.proto,
				HeaderFields: []proxyservice.HTTPHeaderField{{Name: "content-type", Value: "text/html; charset=utf-8"}},
				Metrics:      &proxyservice.HTTPMessageMetrics{StartedAtMicros: startMicros, EndedAtMicros: endMicros, HeaderSize: 180, BodySize: r.size, State: proxyservice.HTTPMessageStateCompleted},
			},
		})
	}
	return entries
}
