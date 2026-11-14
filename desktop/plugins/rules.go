package plugins

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	pythonpluginservice "github.com/josexy/flowlens/backend/services/python_plugin_service"
	"github.com/josexy/flowlens/desktop/store"
	apptheme "github.com/josexy/flowlens/desktop/theme"
	"github.com/josexy/flowlens/desktop/widgets"
	"github.com/josexy/flowlens/locales"
	nui "github.com/josexy/nuxt-ui-go"
	"github.com/josexy/nuxt-ui-go/icons/lucide"
)

type ruleDraft struct {
	id, method, pattern string
	enabled             bool
	focused             bool
	dirty               bool
}

// rulesPane edits a plugin's ordered rules.
type rulesPane struct {
	signature string
	drafts    []*ruleDraft
	err       string
	list      widgets.ReorderListState
}

func rulesSignature(plugin *store.Plugin) string {
	var text strings.Builder
	text.WriteString(plugin.ID)
	for _, rule := range plugin.Rules {
		fmt.Fprintf(&text, "|%s:%d", rule.ID, rule.UpdatedAt)
	}
	return text.String()
}

func (r *rulesPane) sync(plugin *store.Plugin) {
	signature := rulesSignature(plugin)
	if signature == r.signature {
		return
	}
	r.signature, r.err = signature, ""
	r.drafts = make([]*ruleDraft, 0, len(plugin.Rules))
	for _, rule := range plugin.Rules {
		r.drafts = append(r.drafts, &ruleDraft{id: rule.ID, method: rule.Method, pattern: rule.URLPattern, enabled: rule.Enabled})
	}
}

func (r *rulesPane) save(v *View, plugin *store.Plugin, draft *ruleDraft) {
	r.err = ""
	draft.dirty = false
	v.Data.Plugins.UpdateRule(plugin.ID, draft.id, pythonpluginservice.UpdateRuleInput{Enabled: draft.enabled, Method: draft.method, URLPattern: draft.pattern}, func(_ *store.PluginRule, err error) {
		if err != nil {
			r.err = err.Error()
		}
	})
}

func (r *rulesPane) build(c *ui.Context, theme nui.Theme, v *View, plugin *store.Plugin) {
	plugins := v.Data.Plugins
	r.sync(plugin)
	ui.Row(c.Key("rules-head")).FillWidth().Shrink(0).AlignItems(ui.Center).Justify(ui.SpaceBetween).Padding(6, 10).Gap(8).Children(func() {
		ui.Text(c, locales.PythonPluginsRulesHint()).FontSize(apptheme.TextSize(c, 12)).TextColor(theme.Muted).Grow(1).Basis(0).MinWidth(0)
		nui.Button(c.Key("add"), theme, locales.PythonPluginsAddRule(), nui.ButtonOptions{Variant: nui.Outline, Color: nui.Neutral, Size: nui.XS, Leading: lucide.Plus(), Loading: plugins.IsMutating("rule:create:" + plugin.ID)}).OnClick(func() {
			r.err = ""
			plugins.CreateRule(plugin.ID, func(_ *store.PluginRule, err error) {
				if err != nil {
					r.err = err.Error()
				}
			})
		})
	})
	if r.err != "" {
		ui.Box(c.Key("rules-error")).FillWidth().Shrink(0).Padding(0, 10, 6, 10).Children(func() {
			nui.Alert(c, theme, nui.AlertOptions{Description: r.err, Color: nui.Error, Variant: nui.Soft})
		})
	}
	if len(r.drafts) == 0 {
		ui.Column(c.Key("no-rules")).Fill().Center().Children(func() {
			widgets.EmptyState(c, theme, nui.EmptyOptions{Icon: lucide.Route(), Title: locales.PythonPluginsNoRules()})
		})
		return
	}
	drafts := r.drafts
	ids := make([]string, len(drafts))
	for index, draft := range drafts {
		ids[index] = draft.id
	}
	usable := !plugins.IsMutating("rule:reorder:" + plugin.ID)
	r.list.Build(c.Key("rule-list"), ids, r.signature, usable, len(drafts), func(ids []string) {
		r.err = ""
		plugins.ReorderRules(plugin.ID, ids, func(err error) {
			if err != nil {
				r.err = err.Error()
				r.signature = ""
			}
		})
	}).FillWidth().Grow(1).Basis(0).MinHeight(0).
		ItemKey(func(index int) any { return drafts[index].id }).Rows(func(item ui.ListRow) {
		r.row(item.Context, theme, v, plugin, item.Index, drafts[item.Index], usable)
	})
}

func (r *rulesPane) row(c *ui.Context, theme nui.Theme, v *View, plugin *store.Plugin, index int, draft *ruleDraft, reorderable bool) {
	plugins := v.Data.Plugins
	number := strconv.Itoa(index + 1)
	busy := plugins.IsMutating("rule:update:" + plugin.ID + ":" + draft.id)
	line := ui.Row(c.Key(draft.id)).FillWidth().Height(ruleRowHeight).AlignItems(ui.Center).Gap(6).PaddingX(10).BorderWidth(0, 0, 1, 0).BorderColor(theme.BorderMuted)
	line.Background(theme.Background)
	line.Children(func() {
		widgets.ReorderGrip(c.Key("grip"), theme, "drag rule "+number, reorderable)
		method := draft.method
		methodBox := ui.Box(c.Key("method")).Width(100).Shrink(0).Children(func() {
			nui.Input(c, theme, &draft.method, nui.InputOptions{Label: "Method " + number, Size: nui.SM, Disabled: busy, OnSubmit: func(string) { r.save(v, plugin, draft) }}).FillWidth()
		})
		if upper := strings.ToUpper(draft.method); upper != draft.method {
			draft.method = upper
		}
		pattern := draft.pattern
		patternBox := ui.Box(c.Key("pattern")).Grow(1).Basis(0).MinWidth(0).Children(func() {
			nui.Input(c, theme, &draft.pattern, nui.InputOptions{Label: "URL pattern " + number, Placeholder: locales.PythonPluginsRulePatternPlaceholder(), Size: nui.SM, Disabled: busy, OnSubmit: func(string) { r.save(v, plugin, draft) }}).FillWidth()
		})
		if method != draft.method || pattern != draft.pattern {
			draft.dirty = true
		}
		focused := methodBox.FocusWithin() || patternBox.FocusWithin()
		if draft.focused && !focused && draft.dirty {
			r.save(v, plugin, draft)
		}
		draft.focused = focused
		enabled := draft.enabled
		nui.Switch(c.Key("enabled"), theme, &enabled, nui.SwitchOptions{Size: nui.SM, Disabled: busy}).Label(locales.PythonPluginsEnabled() + " rule " + number)
		if enabled != draft.enabled {
			draft.enabled = enabled
			r.save(v, plugin, draft)
		}
		remove := nui.Button(c.Key("delete"), theme, "", nui.ButtonOptions{Variant: nui.Ghost, Color: nui.Error, Size: nui.XS, Square: true, Leading: lucide.Trash(), AccessibleLabel: locales.PythonPluginsDeleteRule() + " " + number, Loading: plugins.IsMutating("rule:delete:" + plugin.ID + ":" + draft.id)})
		remove.OnClick(func() {
			r.err = ""
			plugins.DeleteRule(plugin.ID, draft.id, func(err error) {
				if err != nil {
					r.err = err.Error()
				}
			})
		})
		nui.Tooltip(c, theme, remove, locales.PythonPluginsDeleteRule(), nui.TooltipOptions{})
	})
}
