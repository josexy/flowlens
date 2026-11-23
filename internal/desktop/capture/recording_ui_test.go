package capture

import (
	"fmt"
	"math"
	"runtime"
	"strings"
	"testing"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/internal/desktop/internal/rendertest"
	"github.com/josexy/flowlens/internal/desktop/shortcuts"
	apptheme "github.com/josexy/flowlens/internal/desktop/theme"
	proxyservice "github.com/josexy/flowlens/internal/services/proxy_service"
	settingservice "github.com/josexy/flowlens/internal/services/setting_service"
	"github.com/josexy/flowlens/locales"
	nui "github.com/josexy/nuxt-ui-go/ui"
)

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
