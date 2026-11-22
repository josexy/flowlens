package capture

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/internal/desktop/internal/rendertest"
	"github.com/josexy/flowlens/internal/desktop/shortcuts"
	"github.com/josexy/flowlens/locales"
)

func TestQuickSettingSavesKeepProxyControlsStable(t *testing.T) {
	for _, dark := range []bool{false, true} {
		for _, state := range []string{"stopped", "recording", "paused"} {
			t.Run(state+map[bool]string{false: "-light", true: "-dark"}[dark], func(t *testing.T) {
				f := newFixture(t)
				f.setDark(dark)
				label := locales.ToolbarStart()
				if state != "stopped" {
					f.click(label)
					label = locales.ToolbarPauseRecording()
				}
				if state == "paused" {
					f.click(label)
					label = locales.ToolbarResumeRecording()
				}
				f.tester.Move(1, 1)
				f.tester.SetFocused(false)
				f.tester.Advance(time.Second)
				f.tester.Frame()
				before := f.tester.Image()
				status := f.data.Proxy.Status
				primary, ok := f.tester.AccessByRole(ui.RoleButton, label)
				if !ok || primary.Disabled {
					t.Fatal("primary action unavailable before saving")
				}
				controls := []ui.AccessInfo{primary}
				if state != "stopped" {
					arrow, ok := f.tester.AccessByRole(ui.RoleMenuButton, locales.ToolbarProxyActions())
					if !ok || arrow.Disabled {
						t.Fatal("proxy menu unavailable before saving")
					}
					controls = append(controls, arrow)
				}
				// Leave the save completion queued so its pending frames can be inspected.
				if err := f.tester.Click(locales.ToolbarDisableHttp2()); err != nil {
					t.Fatal(err)
				}
				if !f.workspace.toolbar.applying || !f.data.Settings.Saving {
					t.Fatal("quick setting did not start saving")
				}
				for range 3 {
					f.tester.Frame()
					f.tester.Advance(100 * time.Millisecond)
					for _, control := range controls {
						info, ok := f.tester.AccessByRole(control.Role, control.Name)
						if !ok || !info.Disabled || info.Bounds != control.Bounds {
							t.Fatalf("%s moved or lost its pending-write guard", control.Name)
						}
						rendertest.RequireSamePixels(t, before, f.tester.Image(), control.Bounds, control.Name)
					}
				}
				if f.dispatch.Invoke(shortcuts.CaptureToggleProxy) || f.dispatch.Invoke(shortcuts.CaptureToggleRecording) {
					t.Fatal("conflicting command ran while saving")
				}
				rendertest.Shoot(t, f.tester, "proxy-pending-"+t.Name())
				f.settle()
				if !f.data.Settings.Current.ProxyConfig.DisableHTTP2 || f.workspace.toolbar.applying || f.data.Proxy.Status != status {
					t.Fatal("quick setting failed to save or changed proxy status")
				}
				for _, control := range controls {
					info, ok := f.tester.AccessByRole(control.Role, control.Name)
					if !ok || info.Disabled || info.Bounds != control.Bounds {
						t.Fatalf("%s did not recover after saving", control.Name)
					}
					rendertest.RequireSamePixels(t, before, f.tester.Image(), control.Bounds, control.Name)
				}
			})
		}
	}
}

func TestQuickSettingSavesKeepFieldsStable(t *testing.T) {
	for _, dark := range []bool{false, true} {
		for _, kind := range []string{"port", "dns-apply", "dns-clear", "hosts", "alias"} {
			t.Run(kind+map[bool]string{false: "-light", true: "-dark"}[dark], func(t *testing.T) {
				f := newFixture(t)
				f.setDark(dark)
				var pending func()
				type control struct {
					role  ui.Role
					label string
				}
				var controls []control
				switch kind {
				case "port":
					f.click("Proxy port: 8080")
					pending = func() { f.workspace.toolbar.applying = true }
					controls = []control{{ui.RoleTextField, locales.ToolbarPort()}, {ui.RoleButton, locales.ToolbarCancel()}}
				case "dns-apply", "dns-clear":
					f.click(locales.ToolbarDnsCache())
					pending = func() {
						f.workspace.toolbar.dnsApplying = kind == "dns-apply"
						f.workspace.toolbar.dnsClearing = kind == "dns-clear"
					}
					controls = []control{{ui.RoleSwitch, locales.ToolbarDnsCacheEnabled()}, {ui.RoleTextField, locales.ToolbarDnsCacheTtl()}}
				case "hosts":
					f.workspace.toolbar.hosts.show(f.workspace.proxyConfig())
					f.workspace.toolbar.hosts.include = []hostFilterRow{{id: 1, value: "*.example.com"}}
					f.settle()
					pending = func() { f.workspace.toolbar.applying = true }
					controls = []control{{ui.RoleTextField, locales.SettingsIncludeHosts()}, {ui.RoleButton, locales.ToolbarCancel()}}
				case "alias":
					f.workspace.ShowAliasDialog()
					f.settle()
					pending = func() { f.workspace.alias.saving = true }
					controls = []control{{ui.RoleTextField, locales.CaptureSetAlias()}, {ui.RoleButton, locales.HistoryCancel()}}
				}
				f.tester.Move(1, 1)
				f.tester.SetFocused(false)
				f.tester.Advance(time.Second)
				f.tester.Frame()
				before := f.tester.Image()
				bounds := make([]ui.Rect, len(controls))
				for index, control := range controls {
					info, ok := f.tester.AccessByRole(control.role, control.label)
					if !ok || info.Disabled {
						t.Fatalf("control unavailable before saving: %s", control.label)
					}
					bounds[index] = info.Bounds
				}
				pending()
				for range 3 {
					f.tester.Frame()
					f.tester.Advance(100 * time.Millisecond)
					for index, control := range controls {
						info, ok := f.tester.AccessByRole(control.role, control.label)
						if !ok || !info.Disabled || info.Bounds != bounds[index] {
							t.Errorf("control moved or lost its pending-write guard: %s", control.label)
						}
						rendertest.RequireSamePixels(t, before, f.tester.Image(), bounds[index], control.label)
					}
				}
				rendertest.Shoot(t, f.tester, "capture-pending-"+t.Name())
			})
		}
	}
}
