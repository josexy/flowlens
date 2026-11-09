package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/netproxy"
	appservice "github.com/josexy/flowlens/backend/services/app_service"
)

const (
	latestReleaseURL  = "https://api.github.com/repos/josexy/flowlens/releases/latest"
	releasePageFormat = "https://github.com/josexy/flowlens/releases/tag/v%s"
	releaseBodyLimit  = 4 << 20
	releaseUserAgent  = "flowlens-updater"

	// Install reports nothing between the last byte and returning, so the
	// time spent verifying is inferred.
	verifyPhaseDuration = 400 * time.Millisecond
	progressInterval    = 100 * time.Millisecond
	rateInterval        = 250 * time.Millisecond
)

var errManualInstall = errors.New("this installation must be updated manually")

// configureUpdates picks how this installation is updated: in place when MyGo
// can replace it, otherwise from the release page.
func configureUpdates(service *appservice.AppService) {
	if mygo.Updater.Enabled() {
		service.SetUpdateEngine(newMygoEngine(service), appservice.UpdateApplyModeSelf)
		return
	}
	service.SetUpdateEngine(newManualEngine(appservice.APP_VERSION), appservice.UpdateApplyModeManual)
}

// updateReporter hears the phases and progress of a download.
type updateReporter interface {
	ReportUpdatePhase(appservice.UpdatePhase)
	ReportUpdateProgress(appservice.Progress)
}

// availableUpdate is a newer version MyGo found.
type availableUpdate struct {
	Version string
	Notes   string
	Date    time.Time
	Install func(ctx context.Context, progress func(downloaded, total int64)) error
}

// mygoEngine installs signed updates in place.
type mygoEngine struct {
	check    func(context.Context) (*availableUpdate, error)
	reporter updateReporter
	now      func() time.Time
	after    func(time.Duration, func()) (stop func())

	mu     sync.Mutex
	latest *availableUpdate
}

func newMygoEngine(reporter updateReporter) *mygoEngine {
	return &mygoEngine{
		check: func(ctx context.Context) (*availableUpdate, error) {
			found, err := mygo.Updater.Check(ctx)
			if err != nil || found == nil {
				return nil, err
			}
			return &availableUpdate{Version: found.Version, Notes: found.Notes, Date: found.Date, Install: found.Install}, nil
		},
		reporter: reporter,
		now:      time.Now,
		after: func(delay time.Duration, run func()) func() {
			timer := time.AfterFunc(delay, run)
			return func() { timer.Stop() }
		},
	}
}

// Check implements appservice.UpdateEngine.
func (e *mygoEngine) Check(ctx context.Context) (*appservice.Release, error) {
	found, err := e.check(ctx)
	e.mu.Lock()
	e.latest = found
	e.mu.Unlock()
	if err != nil || found == nil {
		return nil, err
	}
	return &appservice.Release{
		Version: strings.TrimPrefix(found.Version, "v"), Notes: found.Notes, PublishedAt: found.Date,
		ReleaseURL: fmt.Sprintf(releasePageFormat, strings.TrimPrefix(found.Version, "v")),
	}, nil
}

// DownloadAndInstall implements appservice.UpdateEngine.
func (e *mygoEngine) DownloadAndInstall(ctx context.Context) error {
	e.mu.Lock()
	found := e.latest
	e.mu.Unlock()
	if found == nil {
		return errors.New("no update is available")
	}
	tracker := &installTracker{reporter: e.reporter, now: e.now, after: e.after, phase: appservice.UpdatePhaseDownloading}
	defer tracker.finish()
	return found.Install(ctx, tracker.observe)
}

// installTracker infers the phases of Install from its byte counts.
type installTracker struct {
	reporter updateReporter
	now      func() time.Time
	after    func(time.Duration, func()) (stop func())

	mu          sync.Mutex
	phase       appservice.UpdatePhase
	written     int64
	seen        bool
	done        bool
	lastSent    time.Time
	sampleAt    time.Time
	sampleBytes int64
	rate        float64
	generation  int
	stop        func()
}

func (t *installTracker) enter(phase appservice.UpdatePhase) {
	t.phase = phase
	t.generation++
	if t.stop != nil {
		t.stop()
		t.stop = nil
	}
	t.reporter.ReportUpdatePhase(phase)
}

func (t *installTracker) observe(written, total int64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.done {
		return
	}
	now := t.now()
	restarted := t.seen && written < t.written
	if restarted || (t.phase != appservice.UpdatePhaseDownloading && (total <= 0 || written < total)) {
		// A delta that could not make the new version is followed by the
		// whole update, from its first byte.
		t.rate, t.sampleBytes, t.sampleAt, t.lastSent = 0, 0, time.Time{}, time.Time{}
		t.enter(appservice.UpdatePhaseDownloading)
	}
	t.seen, t.written = true, written
	complete := total > 0 && written >= total
	if t.phase != appservice.UpdatePhaseDownloading {
		return
	}
	t.sample(now, written)
	if complete || t.lastSent.IsZero() || now.Sub(t.lastSent) >= progressInterval {
		t.lastSent = now
		t.reporter.ReportUpdateProgress(appservice.Progress{Written: written, Total: total, Rate: t.rate})
	}
	if complete {
		t.enter(appservice.UpdatePhaseVerifying)
		generation := t.generation
		t.stop = t.after(verifyPhaseDuration, func() { t.prepare(generation) })
	}
}

func (t *installTracker) sample(now time.Time, written int64) {
	if t.sampleAt.IsZero() {
		t.sampleAt, t.sampleBytes = now, written
		return
	}
	elapsed := now.Sub(t.sampleAt)
	if elapsed < rateInterval {
		return
	}
	current := float64(written-t.sampleBytes) / elapsed.Seconds()
	if t.rate == 0 {
		t.rate = current
	} else {
		t.rate = 0.7*t.rate + 0.3*current
	}
	t.sampleAt, t.sampleBytes = now, written
}

func (t *installTracker) prepare(generation int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.done || generation != t.generation {
		return
	}
	t.enter(appservice.UpdatePhasePreparing)
}

func (t *installTracker) finish() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.done = true
	if t.stop != nil {
		t.stop()
		t.stop = nil
	}
}

// manualEngine finds releases for installations that cannot update themselves.
type manualEngine struct {
	client  *http.Client
	url     string
	current string
}

func newManualEngine(current string) *manualEngine {
	return &manualEngine{client: &http.Client{Transport: netproxy.Transport(), Timeout: time.Minute}, url: latestReleaseURL, current: current}
}

type githubRelease struct {
	TagName     string    `json:"tag_name"`
	Name        string    `json:"name"`
	Body        string    `json:"body"`
	HTMLURL     string    `json:"html_url"`
	PublishedAt time.Time `json:"published_at"`
}

// Check implements appservice.UpdateEngine.
func (e *manualEngine) Check(ctx context.Context) (*appservice.Release, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, e.url, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", releaseUserAgent)
	response, err := e.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		return nil, nil // nothing has been released yet
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub answered %s", response.Status)
	}
	var release githubRelease
	if err := json.NewDecoder(io.LimitReader(response.Body, releaseBodyLimit)).Decode(&release); err != nil {
		return nil, fmt.Errorf("read the latest release: %w", err)
	}
	version := strings.TrimPrefix(strings.TrimSpace(release.TagName), "v")
	if version == "" {
		return nil, errors.New("the latest release has no version")
	}
	if compareVersions(version, e.current) <= 0 {
		return nil, nil
	}
	return &appservice.Release{Version: version, Name: release.Name, Notes: release.Body, PublishedAt: release.PublishedAt, ReleaseURL: release.HTMLURL}, nil
}

// DownloadAndInstall implements appservice.UpdateEngine.
func (e *manualEngine) DownloadAndInstall(context.Context) error { return errManualInstall }

// compareVersions orders semantic versions, without build metadata.
func compareVersions(left, right string) int {
	leftCore, leftPre := splitVersion(left)
	rightCore, rightPre := splitVersion(right)
	for index := range max(len(leftCore), len(rightCore)) {
		var a, b string
		if index < len(leftCore) {
			a = leftCore[index]
		}
		if index < len(rightCore) {
			b = rightCore[index]
		}
		if order := compareIdentifier(a, b, true); order != 0 {
			return order
		}
	}
	switch {
	case len(leftPre) == 0 && len(rightPre) == 0:
		return 0
	case len(leftPre) == 0:
		return 1
	case len(rightPre) == 0:
		return -1
	}
	for index := range min(len(leftPre), len(rightPre)) {
		if order := compareIdentifier(leftPre[index], rightPre[index], false); order != 0 {
			return order
		}
	}
	switch {
	case len(leftPre) < len(rightPre):
		return -1
	case len(leftPre) > len(rightPre):
		return 1
	}
	return 0
}

func splitVersion(version string) (core, prerelease []string) {
	version, _, _ = strings.Cut(strings.TrimPrefix(version, "v"), "+")
	base, pre, hasPre := strings.Cut(version, "-")
	core = strings.Split(base, ".")
	if hasPre {
		prerelease = strings.Split(pre, ".")
	}
	return core, prerelease
}

func compareIdentifier(left, right string, zeroMissing bool) int {
	if zeroMissing {
		if left == "" {
			left = "0"
		}
		if right == "" {
			right = "0"
		}
	}
	leftNumber, leftErr := strconv.ParseUint(left, 10, 64)
	rightNumber, rightErr := strconv.ParseUint(right, 10, 64)
	switch {
	case leftErr == nil && rightErr == nil:
		switch {
		case leftNumber < rightNumber:
			return -1
		case leftNumber > rightNumber:
			return 1
		}
		return 0
	case leftErr == nil:
		return -1 // numeric identifiers sort before others
	case rightErr == nil:
		return 1
	}
	return strings.Compare(left, right)
}
