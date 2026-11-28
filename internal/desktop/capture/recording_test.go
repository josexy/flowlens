package capture

import (
	"errors"
	"fmt"
	"math"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/internal/desktop/internal/menutest"
	"github.com/josexy/flowlens/internal/desktop/internal/rendertest"
	"github.com/josexy/flowlens/internal/desktop/shortcuts"
	apptheme "github.com/josexy/flowlens/internal/desktop/theme"
	proxyservice "github.com/josexy/flowlens/internal/services/proxy_service"
	settingservice "github.com/josexy/flowlens/internal/services/setting_service"
	"github.com/josexy/flowlens/locales"
	nui "github.com/josexy/nuxt-ui-go/ui"
)

func TestRecordingSplitButton(t *testing.T) {
	f := newFixture(t, entry(1, "retained.test"))
	start, ok := f.tester.AccessByRole(ui.RoleButton, "Start Proxy")
	if !ok {
		t.Fatal("missing start control")
	}
	f.click("Start Proxy")
	primary, ok := f.tester.AccessByRole(ui.RoleButton, "Pause")
	arrow, arrowOK := f.tester.AccessByRole(ui.RoleMenuButton, "Proxy actions")
	if !ok || !arrowOK || primary.Bounds.X != start.Bounds.X || math.Abs(float64(arrow.Bounds.X+arrow.Bounds.W-start.Bounds.X-start.Bounds.W)) > 1 {
		t.Fatalf("split control changed width: start=%+v primary=%+v arrow=%+v", start.Bounds, primary.Bounds, arrow.Bounds)
	}
	f.click("Pause")
	f.requireNoText("Paused")
	if !f.data.Proxy.Status.Running || !f.data.Proxy.Status.RecordingPaused || f.backend.starts != 1 || f.backend.stops != 0 || len(f.data.Traffic.Entries) != 1 {
		t.Fatal("pause changed listener lifecycle or retained traffic")
	}
	resume, ok := f.tester.AccessByRole(ui.RoleButton, "Resume")
	if !ok || resume.Bounds != primary.Bounds {
		t.Fatalf("resume moved the primary action: %+v / %+v", resume.Bounds, primary.Bounds)
	}
	f.click("Resume")
	if f.data.Proxy.Status.RecordingPaused {
		t.Fatal("recording did not resume")
	}
	f.click("Proxy actions")
	if _, ok := menutest.Item(f.tester, "Stop Proxy"); !ok {
		t.Fatal("missing stop menu item")
	}
	f.tester.Key(0, ui.KeyEscape)
	f.settle()
	if menutest.Shown(f.tester) || !f.tester.Focused("Proxy actions") {
		t.Fatal("Escape did not close the menu and restore trigger focus")
	}
	f.tester.Key(0, ui.KeySpace)
	f.settle()
	f.tester.Key(0, ui.KeyEnter)
	f.settle()
	if f.data.Proxy.Status.Running || !f.tester.Focused("Start Proxy") {
		t.Fatal("keyboard stop did not return focus to Start Proxy")
	}
}

func TestRecordingCommandAndBusyGuard(t *testing.T) {
	f := newFixture(t)
	if f.dispatch.Invoke(shortcuts.CaptureToggleRecording) {
		t.Fatal("recording shortcut started a stopped proxy")
	}
	f.click("Start Proxy")
	block := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(block) }) }
	t.Cleanup(unblock)
	f.backend.pauseBlock = block
	if !f.dispatch.Invoke(shortcuts.CaptureToggleRecording) {
		t.Fatal("recording command unavailable")
	}
	f.tester.Frame()
	for _, label := range []string{"Pause", "Proxy actions"} {
		role := ui.RoleButton
		if label == "Proxy actions" {
			role = ui.RoleMenuButton
		}
		control, ok := f.tester.AccessByRole(role, label)
		if !ok || !control.Disabled {
			t.Fatalf("pending operation left %q enabled", label)
		}
	}
	if f.dispatch.Invoke(shortcuts.CaptureToggleRecording) || f.dispatch.Invoke(shortcuts.CaptureToggleProxy) {
		t.Fatal("conflicting command ran during pause operation")
	}
	unblock()
	f.settle()
	if !f.data.Proxy.Status.RecordingPaused {
		t.Fatal("pause did not complete")
	}
	if !f.dispatch.Invoke(shortcuts.CaptureToggleProxy) {
		t.Fatal("proxy command unavailable")
	}
	f.settle()
	if f.data.Proxy.Status.Running || f.backend.stops != 1 {
		t.Fatal("existing proxy shortcut resumed recording instead of stopping")
	}
}

func TestRecordingFailureAndRestartState(t *testing.T) {
	f := newFixture(t)
	f.click("Start Proxy")
	f.backend.pauseErr = errors.New("pause failed")
	f.click("Pause")
	if f.data.Proxy.Status.RecordingPaused || !strings.Contains(f.data.Proxy.LastError, "pause failed") {
		t.Fatal("failed pause changed the visible state")
	}
	f.requireText("pause failed")
	f.backend.pauseErr = nil
	f.click("Pause")
	f.data.Proxy.Restart(nil)
	f.settle()
	if !f.data.Proxy.Status.RecordingPaused {
		t.Fatal("configuration restart resumed recording")
	}
	f.backend.startErr = errors.New("restart failed")
	f.data.Proxy.Restart(nil)
	f.settle()
	if f.data.Proxy.Status.Running || f.data.Proxy.LastError != "restart failed" {
		t.Fatal("restart failure did not publish the stopped backend status")
	}
}

func TestRecordingIgnoresStaleStatus(t *testing.T) {
	f := newFixture(t)
	var events proxyservice.Events
	f.data.Proxy.Initialize(&events)
	f.click("Start Proxy")
	stale := f.data.Proxy.Status
	f.click("Pause")
	events.Status.Publish(stale)
	f.settle()
	if !f.data.Proxy.Status.RecordingPaused {
		t.Fatal("late status event reverted the pause")
	}
}

func TestRenderRecordingSplitButton(t *testing.T) {
	for _, language := range []string{"en", "zh-Hans"} {
		t.Run(language, func(t *testing.T) {
			f := newFixture(t)
			i18n.SetLocaleFunc(func() string { return language })
			mode := "light"
			f.tester = ui.NewTester(func(c *ui.Context) {
				palette := apptheme.Begin(c, apptheme.Resolve(apptheme.Appearance{Mode: mode}).Config)
				ui.Column(c).Fill().Background(palette.Background).Children(func() { f.workspace.buildToolbar(c, palette) })
			}, 1200, 160)
			f.tester.SetLocale(language)
			for _, state := range []string{"stopped", "recording", "paused"} {
				f.data.Proxy.Status = proxyservice.ProxyStatus{Running: state != "stopped", RecordingPaused: state == "paused", Address: "127.0.0.1:8080"}
				for _, appearance := range []string{"light", "dark"} {
					mode = appearance
					f.workspace.toolbar.proxyMenu.Open = false
					rendertest.SettleTheme(f.tester)
					label := locales.ToolbarStart()
					if state == "recording" {
						label = locales.ToolbarPauseRecording()
					}
					if state == "paused" {
						label = locales.ToolbarResumeRecording()
					}
					control, ok := f.tester.AccessByRole(ui.RoleButton, label)
					if !ok || control.Bounds.W <= 0 || control.Bounds.X+control.Bounds.W > 1200 {
						t.Fatalf("%s %s: missing/clipped primary %+v", state, appearance, control)
					}
					if state == "paused" {
						f.click(locales.ToolbarProxyActions())
						if !menutest.Shown(f.tester) {
							t.Fatalf("menu did not open: state=%+v texts=%v", f.data.Proxy.Status, f.tester.Texts())
						}
						rendertest.SettleTheme(f.tester)
					}
					rendertest.Shoot(t, f.tester, "proxy-split-"+language+"-"+state+"-"+appearance)
					if state == "paused" {
						f.tester.SetSize(740, 160)
						f.workspace.toolbar.proxyMenu.Open = false
						f.settle()
						rendertest.SettleTheme(f.tester)
						primary, _ := f.tester.AccessByRole(ui.RoleButton, label)
						quick, quickFound := f.tester.Find(locales.ToolbarQuickMode("HTTP"))
						if !quickFound || quick.X < 0 || quick.X+quick.W > primary.Bounds.X || primary.Bounds.X+primary.Bounds.W > 740 {
							t.Fatalf("controls overlap or are clipped in narrow toolbar: quick=%+v primary=%+v", quick, primary.Bounds)
						}
						rendertest.Shoot(t, f.tester, "proxy-split-narrow-"+language+"-"+appearance)
						f.tester.SetSize(1200, 160)
					}
				}
			}
		})
	}
}

func TestRecordingKeyboardFocusSurvivesStopAndStart(t *testing.T) {
	f := newFixture(t)
	f.click(locales.ToolbarStart())
	f.workspace.toolbar.proxyAction.Focus()
	f.tester.Frame()
	for _, label := range []string{locales.ToolbarResumeRecording(), locales.ToolbarPauseRecording()} {
		f.tester.Key(0, ui.KeySpace)
		f.settle()
		if !f.tester.Focused(label) {
			t.Fatalf("lost keyboard focus after switching to %s", label)
		}
	}
	f.click(locales.ToolbarProxyActions())
	f.tester.Key(0, ui.KeyEnter)
	f.settle()
	if !f.tester.Focused(locales.ToolbarStart()) {
		t.Fatal("stop did not focus Start Proxy")
	}
	f.tester.Key(0, ui.KeySpace)
	f.settle()
	if !f.tester.Focused(locales.ToolbarPauseRecording()) {
		t.Fatalf("restarting moved focus away from primary action; arrow focused: %v", f.tester.Focused(locales.ToolbarProxyActions()))
	}
	f.tester.Key(0, ui.KeySpace)
	f.settle()
	if !f.data.Proxy.Status.RecordingPaused || f.workspace.toolbar.proxyMenu.Open {
		t.Fatal("Space opened the menu instead of pausing after restarting")
	}
}

func TestRecordingControlsDimOnceDuringOperations(t *testing.T) {
	for _, mode := range []string{"light", "dark"} {
		t.Run(mode, func(t *testing.T) {
			f := newFixture(t)
			variant := nui.Solid
			color := nui.Primary
			f.tester = ui.NewTester(func(c *ui.Context) {
				palette := apptheme.Begin(c, apptheme.Resolve(apptheme.Appearance{Mode: mode, DisableAnimations: true}).Config)
				ui.Column(c).Fill().Background(palette.MutedBackground).Children(func() {
					f.workspace.buildToolbar(c, palette)
					nui.Button(c, palette, "Reference", nui.ButtonOptions{Color: color, Variant: variant, Disabled: true}).Width(160).Height(32)
				})
			}, 1200, 120)
			f.data.Proxy.Toggling = true
			for _, state := range []string{"stopped", "recording", "paused"} {
				f.data.Proxy.Status = proxyservice.ProxyStatus{Running: state != "stopped", RecordingPaused: state == "paused"}
				labels := []string{locales.ToolbarStart()}
				variant, color = nui.Solid, nui.Primary
				switch state {
				case "recording":
					labels = []string{locales.ToolbarPauseRecording(), locales.ToolbarProxyActions()}
					variant, color = nui.Outline, nui.Neutral
				case "paused":
					labels = []string{locales.ToolbarResumeRecording(), locales.ToolbarProxyActions()}
				}
				f.tester.Frame()
				reference, _ := f.tester.Find("Reference")
				frame := f.tester.Image()
				want := frame.RGBAAt(int(reference.X+4), int(reference.Y+reference.H/2))
				for _, label := range labels {
					control, ok := f.tester.Access(label)
					if !ok || !control.Disabled {
						t.Fatalf("%s is not guarded during a proxy operation", label)
					}
					got := frame.RGBAAt(int(control.Bounds.X+4), int(control.Bounds.Y+control.Bounds.H/2))
					if got != want {
						t.Errorf("%s dims more than a standard disabled button: got %v, want %v", label, got, want)
					}
				}
				rendertest.Shoot(t, f.tester, "proxy-operation-"+state+"-"+mode)
			}
		})
	}
}

func TestRecordingStopMenuOmitsShortcut(t *testing.T) {
	for _, language := range []string{"en", "zh-Hans"} {
		for _, mode := range []string{"light", "dark"} {
			for _, size := range []int{16, 24} {
				t.Run(fmt.Sprintf("%s-%s-%d", language, mode, size), func(t *testing.T) {
					f := newFixture(t)
					i18n.SetLocaleFunc(func() string { return language })
					f.data.Settings.Current.Shortcuts = &settingservice.ShortcutConfig{Overrides: map[string]settingservice.ShortcutOverride{
						shortcuts.CaptureToggleProxy: {Binding: &settingservice.ShortcutBinding{Key: "F12", Modifiers: []settingservice.ShortcutModifier{"control", "shift", "alt"}}},
					}}
					f.tester = ui.NewTester(func(c *ui.Context) {
						palette := apptheme.Begin(c, apptheme.Resolve(apptheme.Appearance{Mode: mode, AppFontSize: size, DisableAnimations: true}).Config)
						ui.Column(c).Fill().Background(palette.Background).Children(func() { f.workspace.buildToolbar(c, palette) })
					}, 1200, 160)
					f.tester.SetLocale(language)
					f.click(locales.ToolbarStart())
					f.click(locales.ToolbarProxyActions())
					menu, ok := f.tester.AccessByRole(ui.RoleMenu, locales.ToolbarProxyActions())
					if !ok || menu.Bounds.X < 0 || menu.Bounds.X+menu.Bounds.W > 1200 {
						t.Fatalf("menu is missing or outside the window: %+v", menu.Bounds)
					}
					label, ok := f.tester.Find(locales.ToolbarStop())
					if !ok || label.W <= 0 || label.H <= 0 || label.X < menu.Bounds.X || label.X+label.W > menu.Bounds.X+menu.Bounds.W {
						t.Errorf("Stop Proxy label is missing or clipped: label=%+v menu=%+v", label, menu.Bounds)
					}
					for _, key := range f.workspace.kbds(shortcuts.CaptureToggleProxy) {
						text := strings.ToUpper(nui.FormatKey(key, runtime.GOOS))
						if _, ok := f.tester.Find(text); ok {
							t.Errorf("Stop Proxy menu displays shortcut key %s", text)
						}
					}
					rendertest.Shoot(t, f.tester, "proxy-stop-menu-"+t.Name())
				})
			}
		}
	}
}

func TestRecordingControlGeometryAcrossStates(t *testing.T) {
	for _, language := range []string{"en", "zh-Hans"} {
		for _, size := range []int{12, 16, 24} {
			for _, zoom := range []float32{1, 1.25} {
				t.Run(fmt.Sprintf("%s-%d-%.2f", language, size, zoom), func(t *testing.T) {
					f := newFixture(t)
					i18n.SetLocaleFunc(func() string { return language })
					label := locales.ToolbarStart()
					var minLabelWidth float32
					f.tester = ui.NewTester(func(c *ui.Context) {
						palette := apptheme.Begin(c, apptheme.Resolve(apptheme.Appearance{AppFontSize: size, DisableAnimations: true}).Config)
						width, _ := c.MeasureText(0, ui.Span{Text: label, Font: palette.Font, Size: apptheme.TextSize(c, 14), Weight: 500})
						minLabelWidth = (width + apptheme.TextSize(c, 20) + 6*palette.Density) * zoom
						ui.Column(c).Fill().Background(palette.Background).Children(func() { f.workspace.buildToolbar(c, palette) })
					}, 1200, 180)
					f.tester.SetLocale(language)
					f.tester.SetPreferences(ui.Preferences{TextScale: zoom})
					start, _ := f.tester.Find(label)
					clear, _ := f.tester.Find(locales.ToolbarClear())
					for _, state := range []string{"stopped", "recording", "paused", "recording", "stopped"} {
						f.data.Proxy.Status = proxyservice.ProxyStatus{Running: state != "stopped", RecordingPaused: state == "paused"}
						label = locales.ToolbarStart()
						switch state {
						case "recording":
							label = locales.ToolbarPauseRecording()
						case "paused":
							label = locales.ToolbarResumeRecording()
						}
						for _, busy := range []bool{false, true} {
							f.data.Proxy.Toggling = busy
							f.tester.Frame()
							primary, ok := f.tester.Find(label)
							if !ok || primary.W < minLabelWidth || primary.X < 0 || primary.X+primary.W > 1200 {
								t.Fatalf("%s busy=%v: label clipped: %+v, minimum width %v", state, busy, primary, minLabelWidth)
							}
							control := primary
							if state != "stopped" {
								arrow, ok := f.tester.Find(locales.ToolbarProxyActions())
								if !ok || arrow.Y != primary.Y || arrow.H != primary.H {
									t.Fatalf("%s: split segments misaligned", state)
								}
								control.W = arrow.X + arrow.W - primary.X
							}
							currentClear, _ := f.tester.Find(locales.ToolbarClear())
							if control.X != start.X || control.Y != start.Y || control.H != start.H || math.Abs(float64(control.W-start.W)) > 1 || currentClear != clear {
								t.Fatalf("%s busy=%v shifted the toolbar: start=%+v current=%+v", state, busy, start, control)
							}
						}
					}
				})
			}
		}
	}
}
