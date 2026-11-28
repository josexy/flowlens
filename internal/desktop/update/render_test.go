package update

import (
	"fmt"
	"strings"
	"testing"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/internal/desktop/internal/rendertest"
	"github.com/josexy/flowlens/internal/desktop/theme"
	"github.com/josexy/flowlens/internal/desktop/widgets"
	appservice "github.com/josexy/flowlens/internal/services/app_service"
	"github.com/josexy/flowlens/locales"
)

func TestRenderUpdater(t *testing.T) {
	for _, language := range []string{"en", "zh-Hans"} {
		for _, state := range []string{"available", "downloading", "finishing", "ready", "manual", "long-notes", "error", "idle"} {
			t.Run(language+"/"+state, func(t *testing.T) {
				f := newFixture(t, appservice.UpdatePhaseAvailable, self)
				i18n.SetLocaleFunc(func() string { return language })
				f.view.Host.Frame = widgets.WindowFrame{Custom: true, Title: locales.UpdaterWindowTitle(), Close: func() { f.closed++ }}
				// The minimum native window size, including its custom title bar.
				f.tester = ui.NewTester(f.view.Build, 520, 520)
				f.tester.SetLocale(language)
				s := &f.data.Updater.Snapshot
				s.CurrentVersion, s.Release.Version = "2.0.0", "2.1.0"
				s.Release.PublishedAt = "2026-11-20T09:00:00Z"
				s.Release.Notes = "### Improvements\n\n- A simpler software update window.\n- Restart automatically after installation.\n- Keep working while the update downloads.\n\n### Fixes\n\n- Retry a failed restart without downloading again."
				if language == "zh-Hans" {
					s.Release.Notes = "### 改进\n\n- 更简洁的软件更新窗口。\n- 安装完成后自动重启，保留未保存内容保护。\n- 下载期间可以继续使用 FlowLens。\n\n### 修复\n\n- 重启失败后重试，无需重新下载更新。"
				}
				switch state {
				case "downloading", "finishing":
					s.Phase, s.CanCancel, s.RestartAfterInstall = appservice.UpdatePhaseInstalling, state == "downloading", true
					s.Progress = &appservice.UpdateProgress{Written: 24 << 20, Total: 64 << 20, Rate: 3 << 20}
					if state == "finishing" {
						s.Progress.Written = s.Progress.Total
					}
				case "ready":
					s.Phase = appservice.UpdatePhaseReady
				case "manual":
					s.ApplyMode = manual
				case "long-notes":
					s.Release.Notes = strings.Repeat(s.Release.Notes+"\n\n", 12)
				case "error":
					s.Phase = appservice.UpdatePhaseError
					s.Failure = &appservice.UpdateFailure{Stage: appservice.UpdatePhaseInstalling, Message: strings.Repeat("The update could not be installed. ", 20)}
				case "idle":
					s.Phase, s.Release = appservice.UpdatePhaseIdle, nil
				}
				for _, dark := range []bool{false, true} {
					mode := theme.ModeLight
					if dark {
						mode = theme.ModeDark
					}
					if err := f.data.Settings.SetThemeMode(mode); err != nil {
						t.Fatal(err)
					}
					f.settle()
					rendertest.SettleTheme(f.tester)
					// Long scroll content cannot push the fixed actions offscreen.
					for _, node := range f.tester.AccessAll() {
						if node.Role != ui.RoleButton {
							continue
						}
						r := node.Bounds
						if r.W <= 0 || r.H <= 0 || r.X < 0 || r.Y < 0 || r.X+r.W > 520 || r.Y+r.H > 520 {
							t.Fatalf("button %q outside minimum window: %+v", node.Name, r)
						}
					}
					rendertest.Shoot(t, f.tester, fmt.Sprintf("updater-%s-%s-%s", language, state, mode))
				}
			})
		}
	}
}

func TestUpdateToastIsTopCenterWithoutProgress(t *testing.T) {
	f := newFixture(t, appservice.UpdatePhaseIdle, appservice.UpdateApplyModeSelf)
	f.view.Notifier.Info("Update note")
	f.tester.Frame()
	f.tester.Frame()
	rect, ok := f.tester.Find("Update note")
	if !ok {
		t.Fatalf("%v", f.tester.Texts())
	}
	if center := rect.X + rect.W/2; center < 350-60 || center > 350+60 || rect.Y > 120 {
		t.Fatalf("toast at %+v, want top-center of 700 wide", rect)
	}
	if _, ok := f.tester.AccessByRole(ui.RoleProgress, ""); ok {
		t.Fatal("toast shows a progress bar")
	}
}
