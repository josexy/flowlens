package capture

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/internal/desktop/internal/rendertest"
	"github.com/josexy/flowlens/locales"
)

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
