package plugins

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/internal/desktop/internal/rendertest"
	"github.com/josexy/flowlens/internal/desktop/store"
	pythonpluginservice "github.com/josexy/flowlens/internal/services/python_plugin_service"
	"github.com/josexy/flowlens/locales"
)

func TestPluginSwitchKeepsRequestedValueUntilSaveCompletes(t *testing.T) {
	for _, fail := range []bool{false, true} {
		f := newFixture(t)
		plugin := f.create("Toggle", "")
		release := make(chan struct{})
		finish := sync.OnceFunc(func() { close(release) })
		t.Cleanup(finish)
		f.backend.enable = func(id string, enabled bool) (*store.Plugin, error) {
			<-release
			if fail {
				return nil, errors.New("write failed")
			}
			return f.backend.PythonPluginService.SetPluginEnabled(id, enabled)
		}
		label := locales.PythonPluginsEnabled() + " Toggle"
		if err := f.tester.Click(label); err != nil {
			t.Fatal(err)
		}
		for range 3 {
			f.tester.Frame()
			f.tester.Advance(100 * time.Millisecond)
			info, ok := f.tester.AccessByRole(ui.RoleSwitch, label)
			if !ok || !info.Checked || !info.Disabled || plugin.Enabled {
				t.Fatalf("pending switch reverted or modified the saved snapshot: %+v", info)
			}
		}
		finish()
		f.settle()
		info, _ := f.tester.AccessByRole(ui.RoleSwitch, label)
		if info.Disabled || info.Checked == fail {
			t.Fatalf("switch did not settle to the persisted value: %+v", info)
		}
		if fail && !f.has(locales.PythonPluginsEnableFailed("write failed")) {
			t.Fatal("save error was hidden")
		}
	}
}

func TestFileDialogKeepsFieldsAndCancelStable(t *testing.T) {
	f := newFixture(t)
	f.create("Files", "")
	f.view.fileModal.show(false, "helper.py")
	f.tester.Frame()
	f.tester.SetFocused(false)
	f.tester.Advance(time.Second)
	before := f.tester.Image()
	field, ok := f.tester.AccessByRole(ui.RoleTextField, locales.PythonPluginsFilePath())
	if !ok {
		t.Fatal("missing file path input")
	}
	cancel, ok := f.tester.AccessByRole(ui.RoleButton, locales.PythonPluginsCancel())
	if !ok {
		t.Fatal("missing cancel button")
	}
	f.view.fileModal.busy = true
	for range 3 {
		f.tester.Frame()
		f.tester.Advance(100 * time.Millisecond)
		for _, prior := range []ui.AccessInfo{field, cancel} {
			info, ok := f.tester.AccessByRole(prior.Role, prior.Name)
			if !ok || !info.Disabled || info.Bounds != prior.Bounds {
				t.Errorf("file dialog moved %s or lost its write guard", prior.Name)
			}
			rendertest.RequireSamePixels(t, before, f.tester.Image(), prior.Bounds, prior.Name)
		}
	}
}

func TestRuleToggleKeepsEditorFieldsStable(t *testing.T) {
	for _, dark := range []bool{false, true} {
		f := newFixture(t)
		plugin := f.create("Rules", "")
		setRenderTheme(t, f.data, dark)
		f.data.Plugins.CreateRule(plugin.ID, func(*store.PluginRule, error) {})
		f.settle()
		f.tester.Advance(time.Second)
		before := f.tester.Image()
		bounds := make(map[string]ui.Rect)
		for _, label := range []string{"Method 1", "URL pattern 1"} {
			info, ok := f.tester.AccessByRole(ui.RoleTextField, label)
			if !ok {
				t.Fatal("missing rule input", label)
			}
			bounds[label] = info.Bounds
		}
		release := make(chan struct{})
		finish := sync.OnceFunc(func() { close(release) })
		t.Cleanup(finish)
		f.backend.updateRule = func(id, ruleID string, input pythonpluginservice.UpdateRuleInput) (*store.PluginRule, error) {
			<-release
			return f.backend.PythonPluginService.UpdateRule(id, ruleID, input)
		}
		if err := f.tester.Click(locales.PythonPluginsEnabled() + " rule 1"); err != nil {
			t.Fatal(err)
		}
		for range 3 {
			f.tester.Frame()
			f.tester.Advance(100 * time.Millisecond)
			for label, rect := range bounds {
				info, ok := f.tester.AccessByRole(ui.RoleTextField, label)
				if !ok || !info.Disabled || info.Bounds != rect {
					t.Errorf("rule input moved or lost its write guard: %+v", info)
				}
				rendertest.RequireSamePixels(t, before, f.tester.Image(), rect, label)
			}
		}
		finish()
		f.settle()
		if f.data.Plugins.SelectedPlugin().Rules[0].Enabled {
			t.Fatal("rule was not disabled")
		}
	}
}
