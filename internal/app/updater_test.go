package app

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	appservice "github.com/josexy/flowlens/internal/services/app_service"
)

type recordingReporter struct {
	mu       sync.Mutex
	progress []appservice.Progress
}

func (r *recordingReporter) ReportUpdateProgress(progress appservice.Progress) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.progress = append(r.progress, progress)
}

type engineFixture struct {
	engine   *mygoEngine
	reporter *recordingReporter
	clock    time.Time
}

func newEngineFixture(install func(context.Context, func(int64, int64)) error) *engineFixture {
	f := &engineFixture{reporter: &recordingReporter{}, clock: time.Unix(1000, 0)}
	f.engine = &mygoEngine{
		check: func(context.Context) (*availableUpdate, error) {
			return &availableUpdate{Version: "v1.2.0", Notes: "notes", Date: time.Unix(5, 0), Install: install}, nil
		},
		reporter: f.reporter,
		now:      func() time.Time { return f.clock },
	}
	return f
}

func TestMygoEngineMapsTheRelease(t *testing.T) {
	f := newEngineFixture(nil)
	release, err := f.engine.Check(context.Background())
	if err != nil || release.Version != "1.2.0" || release.Notes != "notes" || release.ReleaseURL != "https://github.com/josexy/flowlens/releases/tag/v1.2.0" {
		t.Fatalf("%+v %v", release, err)
	}
}

func TestMygoEngineMeasuresDownload(t *testing.T) {
	var f *engineFixture
	f = newEngineFixture(func(_ context.Context, progress func(int64, int64)) error {
		progress(10, 100)
		f.clock = f.clock.Add(time.Second)
		progress(50, 100)
		f.clock = f.clock.Add(time.Second)
		progress(100, 100)
		return nil
	})
	if _, err := f.engine.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := f.engine.DownloadAndInstall(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(f.reporter.progress) != 3 || f.reporter.progress[2].Written != 100 || f.reporter.progress[1].Written != 50 {
		t.Fatalf("%+v", f.reporter.progress)
	}
	if f.reporter.progress[2].Rate <= 0 {
		t.Fatalf("rate %+v", f.reporter.progress[2])
	}
}

func TestMygoEngineReturnsToDownloadWhenDeltaFallsBack(t *testing.T) {
	f := newEngineFixture(func(_ context.Context, progress func(int64, int64)) error {
		progress(20, 20)
		progress(5, 100)
		progress(100, 100)
		return nil
	})
	_, _ = f.engine.Check(context.Background())
	if err := f.engine.DownloadAndInstall(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(f.reporter.progress) != 3 || f.reporter.progress[1].Written != 5 || f.reporter.progress[1].Total != 100 || f.reporter.progress[1].Rate != 0 {
		t.Fatalf("fallback progress: %+v", f.reporter.progress)
	}
}

func TestMygoEngineCancelStopsInstall(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	f := newEngineFixture(func(ctx context.Context, progress func(int64, int64)) error {
		progress(1, 100)
		cancel()
		<-ctx.Done()
		return ctx.Err()
	})
	_, _ = f.engine.Check(context.Background())
	if err := f.engine.DownloadAndInstall(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestMygoEngineNeedsACheckFirst(t *testing.T) {
	f := newEngineFixture(nil)
	if err := f.engine.DownloadAndInstall(context.Background()); err == nil {
		t.Fatal("no update to install")
	}
}

func TestMygoEngineTamperedUpdateFails(t *testing.T) {
	tampered := errors.New("mygo: installing the update: bad signature")
	f := newEngineFixture(func(_ context.Context, progress func(int64, int64)) error {
		progress(100, 100)
		return tampered
	})
	_, _ = f.engine.Check(context.Background())
	if err := f.engine.DownloadAndInstall(context.Background()); !errors.Is(err, tampered) {
		t.Fatal(err)
	}
}

func releaseServer(t *testing.T, status int, body string) *manualEngine {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") != releaseUserAgent {
			t.Errorf("user agent %q", r.Header.Get("User-Agent"))
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return &manualEngine{client: server.Client(), url: server.URL, current: "1.0.5"}
}

func TestManualEngineFindsNewerRelease(t *testing.T) {
	engine := releaseServer(t, 200, `{"tag_name":"v1.1.0","name":"One","body":"# Notes","html_url":"https://github.com/josexy/flowlens/releases/tag/v1.1.0","published_at":"2026-01-02T03:04:05Z"}`)
	release, err := engine.Check(context.Background())
	if err != nil || release == nil || release.Version != "1.1.0" || release.Notes != "# Notes" || release.PublishedAt.Year() != 2026 {
		t.Fatalf("%+v %v", release, err)
	}
	if err := engine.DownloadAndInstall(context.Background()); !errors.Is(err, errManualInstall) {
		t.Fatal(err)
	}
}

func TestManualEngineUpToDate(t *testing.T) {
	for _, body := range []string{`{"tag_name":"v1.0.5"}`, `{"tag_name":"1.0.4"}`} {
		release, err := releaseServer(t, 200, body).Check(context.Background())
		if err != nil || release != nil {
			t.Fatalf("%s: %+v %v", body, release, err)
		}
	}
	if release, err := releaseServer(t, 404, `{}`).Check(context.Background()); err != nil || release != nil {
		t.Fatalf("no releases yet: %+v %v", release, err)
	}
}

func TestManualEngineReportsFailures(t *testing.T) {
	for _, testCase := range []struct {
		status int
		body   string
	}{{500, ""}, {403, `{"message":"rate limited"}`}, {200, "not json"}, {200, `{"tag_name":""}`}} {
		if _, err := releaseServer(t, testCase.status, testCase.body).Check(context.Background()); err == nil {
			t.Fatalf("%+v", testCase)
		}
	}
}

func TestManualEngineCancels(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := releaseServer(t, 200, `{}`).Check(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestCompareVersions(t *testing.T) {
	for _, testCase := range []struct {
		left, right string
		want        int
	}{
		{"1.0.5", "1.0.5", 0}, {"v1.0.6", "1.0.5", 1}, {"1.0.10", "1.0.9", 1}, {"1.2", "1.2.0", 0},
		{"1.0.5-beta.1", "1.0.5", -1}, {"1.0.5", "1.0.5-beta.1", 1}, {"1.0.5-beta.2", "1.0.5-beta.1", 1},
		{"1.0.5-beta.10", "1.0.5-beta.9", 1}, {"1.0.5-rc", "1.0.5-beta", 1}, {"1.0.5-1", "1.0.5-beta", -1},
		{"1.0.5-beta", "1.0.5-beta.1", -1}, {"1.0.5+abc", "1.0.5", 0},
	} {
		if got := compareVersions(testCase.left, testCase.right); got != testCase.want {
			t.Errorf("%s vs %s = %d", testCase.left, testCase.right, got)
		}
	}
}
