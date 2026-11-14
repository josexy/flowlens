package rewrite

import (
	"context"
	"strconv"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	rewriteservice "github.com/josexy/flowlens/backend/services/rewrite_service"
	"github.com/josexy/flowlens/desktop/runtime"
	"github.com/josexy/flowlens/desktop/store"
	apptheme "github.com/josexy/flowlens/desktop/theme"
	"github.com/josexy/flowlens/desktop/widgets"
	"github.com/josexy/flowlens/locales"
	"github.com/josexy/nuxt-ui-go/icons/lucide"
	nui "github.com/josexy/nuxt-ui-go/ui"
)

const (
	bodyPreviewDelay     = 300 * time.Millisecond
	maxPreviewSampleSize = 8 * 1024 * 1024
)

var ruleMethods = []string{"ALL", "GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS", "TRACE"}

// ruleForm edits one draft and keeps what its editors show.
type ruleForm struct {
	view *View

	replacement *widgets.CodeView
	bodyKey     string
	bodyText    string
	match       matchTest
	preview     *bodyPreview
}

func newRuleForm(v *View) *ruleForm {
	form := &ruleForm{view: v, replacement: widgets.NewCodeView("plaintext", false)}
	form.replacement.SetWordWrap(true)
	form.preview = newBodyPreview(v)
	form.match = matchTest{method: "GET", url: "https://example.com/example"}
	return form
}

func (f *ruleForm) close() { f.preview.cancelPending() }

func option(values []string, label func(string) string) []nui.Option[string] {
	options := make([]nui.Option[string], len(values))
	for index, value := range values {
		options[index] = nui.Option[string]{Value: value, Label: label(value)}
	}
	return options
}

func actionTypeLabel(value string) string {
	switch value {
	case rewriteservice.ActionRedirect:
		return locales.RewriteRulesActionRedirect()
	case rewriteservice.ActionResponse:
		return locales.RewriteRulesActionResponse()
	default:
		return locales.RewriteRulesActionRequest()
	}
}

func hostPolicyLabel(value string) string {
	if value == "preserve" {
		return locales.RewriteRulesHostPreserve()
	}
	return locales.RewriteRulesHostTarget()
}

func changeAction(rule *store.RewriteRule, kind string) {
	action := &rule.Action
	action.Type = kind
	if kind != rewriteservice.ActionRedirect {
		action.TargetURL = ""
	}
	edits := kind == rewriteservice.ActionRequest || kind == rewriteservice.ActionResponse
	if !edits {
		action.Headers = []rewriteservice.FieldOperation{}
		action.Body = rewriteservice.BodyAction{Mode: "none"}
	}
	if kind != rewriteservice.ActionRequest {
		action.Query = []rewriteservice.FieldOperation{}
	}
}

func (f *ruleForm) build(c *ui.Context, theme nui.Theme, draft *store.RuleDraft) {
	rewrite := f.view.Data.Rewrite
	rule := draft.Rule
	disabled := rule.UnavailableReason != ""
	errors := rewrite.FieldErrors(rewrite.SelectedID)
	ui.Column(c.Key("rule-form")).FillWidth().MaxWidth(896).Gap(20).Padding(16).AlignSelf(ui.Center).Children(func() {
		ui.Row(c.Key("identity")).FillWidth().Gap(12).Children(func() {
			ui.Box(c.Key("name")).Grow(1).Basis(0).MinWidth(0).Children(func() {
				nui.FormField(c, theme, nui.FormFieldOptions{Label: locales.RewriteRulesName(), Required: true, Error: fieldErrorMessage(errors.Name), Disabled: disabled}, func(field nui.Theme) {
					nui.Input(c.Key("input"), field, &rule.Name, nui.InputOptions{Label: locales.RewriteRulesName(), Disabled: disabled, Invalid: errors.Name != ""}).FillWidth()
				})
			})
			ui.Box(c.Key("method")).Width(128).Shrink(0).Children(func() {
				nui.FormField(c, theme, nui.FormFieldOptions{Label: locales.RewriteRulesMethod(), Disabled: disabled}, func(field nui.Theme) {
					nui.Select(c.Key("select"), field, &rule.Method, option(ruleMethods, func(value string) string { return value }), nui.SelectOptions{Label: locales.RewriteRulesMethod(), Disabled: disabled})
				})
			})
		})
		nui.FormField(c.Key("pattern"), theme, nui.FormFieldOptions{Label: locales.RewriteRulesUrlPattern(), Required: true, Error: fieldErrorMessage(errors.URLPattern), Disabled: disabled}, func(field nui.Theme) {
			nui.Input(c.Key("input"), field, &rule.URLPattern, nui.InputOptions{Label: locales.RewriteRulesUrlPattern(), Disabled: disabled, Invalid: errors.URLPattern != ""}).FillWidth()
			if errors.URLPattern == "" {
				ui.Text(c, locales.RewriteRulesPatternHint()).FontSize(apptheme.TextSize(c, 12)).TextColor(field.Muted).FillWidth().Margin(6, 0, 0, 0)
			}
		})
		kinds := []string{rewriteservice.ActionRedirect, rewriteservice.ActionRequest, rewriteservice.ActionResponse}
		selected := rule.Action.Type
		if rule.UnavailableReason != "" {
			selected = ""
		}
		nui.FormField(c.Key("action"), theme, nui.FormFieldOptions{Label: locales.RewriteRulesAction(), Disabled: disabled}, func(field nui.Theme) {
			ui.Box(c).Width(256).Children(func() {
				nui.Select(c.Key("select"), field, &selected, option(kinds, actionTypeLabel), nui.SelectOptions{Label: locales.RewriteRulesAction(), Placeholder: locales.RewriteRulesUnavailable(), Disabled: disabled})
			})
		})
		if selected != "" && selected != rule.Action.Type {
			changeAction(rule, selected)
		}
		if rule.Action.Type == rewriteservice.ActionRedirect {
			f.redirect(c, theme, rule, disabled)
		}
		edits := rule.Action.Type == rewriteservice.ActionRequest || rule.Action.Type == rewriteservice.ActionResponse
		if edits {
			title := locales.RewriteRulesRequestHeaders()
			if rule.Action.Type == rewriteservice.ActionResponse {
				title = locales.RewriteRulesResponseHeaders()
			}
			operationList(c.Key("headers"), theme, title, &rule.Action.Headers, disabled)
			if rule.Action.Type == rewriteservice.ActionRequest {
				operationList(c.Key("query"), theme, locales.RewriteRulesQuery(), &rule.Action.Query, disabled)
			}
			f.body(c.Key("body"), theme, rule, disabled)
		}
		f.match.build(c.Key("match"), theme, f.view, draft, rule)
		if edits && rule.Action.Body.Mode == "regex" {
			f.preview.build(c.Key("preview"), theme, &rule.Action.Body)
		} else {
			f.preview.reset()
		}
	})
}

func (f *ruleForm) redirect(c *ui.Context, theme nui.Theme, rule *store.RewriteRule, disabled bool) {
	nui.FormField(c.Key("target"), theme, nui.FormFieldOptions{Label: locales.RewriteRulesTargetUrl(), Required: true, Disabled: disabled}, func(field nui.Theme) {
		nui.Input(c.Key("input"), field, &rule.Action.TargetURL, nui.InputOptions{Label: locales.RewriteRulesTargetUrl(), Disabled: disabled}).FillWidth()
	})
	ui.Text(c.Key("target-hint"), locales.RewriteRulesTargetHint("${1}", "${2}")).FontSize(apptheme.TextSize(c, 12)).TextColor(theme.Muted).FillWidth()
	nui.FormField(c.Key("policy"), theme, nui.FormFieldOptions{Label: locales.RewriteRulesHostPolicy(), Disabled: disabled}, func(field nui.Theme) {
		ui.Box(c).Width(256).Children(func() {
			nui.Select(c.Key("select"), field, &rule.Action.HostPolicy, option([]string{"target", "preserve"}, hostPolicyLabel), nui.SelectOptions{Label: locales.RewriteRulesHostPolicy(), Disabled: disabled})
		})
	})
}

func operationLabel(value string) string {
	switch value {
	case "add":
		return locales.RewriteRulesOperationAdd()
	case "delete":
		return locales.RewriteRulesOperationDelete()
	default:
		return locales.RewriteRulesOperationSet()
	}
}

func operationList(c *ui.Context, theme nui.Theme, title string, operations *[]rewriteservice.FieldOperation, disabled bool) {
	ui.Column(c).FillWidth().Gap(8).Children(func() {
		ui.Row(c.Key("head")).FillWidth().AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
			ui.Row(c).AlignItems(ui.Center).Gap(4).Children(func() {
				ui.Text(c, title).FontSize(apptheme.TextSize(c, 14)).Bold().TextColor(theme.Highlighted)
				help := nui.Button(c.Key("help"), theme, "", nui.ButtonOptions{Variant: nui.Ghost, Color: nui.Neutral, Size: nui.XS, Square: true, Leading: lucide.CircleQuestionMark(), AccessibleLabel: locales.RewriteRulesOperationsHelp()})
				nui.Tooltip(c, theme, help, locales.RewriteRulesOperationsHint(), nui.TooltipOptions{})
			})
			add := nui.Button(c.Key("add"), theme, locales.RewriteRulesAddOperation(), nui.ButtonOptions{Variant: nui.Ghost, Size: nui.XS, Leading: lucide.Plus(), Disabled: disabled, AccessibleLabel: locales.RewriteRulesAddOperation() + " " + title})
			if !disabled {
				add.OnClick(func() { *operations = append(*operations, rewriteservice.FieldOperation{Operation: "set"}) })
			}
		})
		for index := range *operations {
			operationRow(c.Key(index), theme, operations, index, disabled)
		}
	})
}

func operationRow(c *ui.Context, theme nui.Theme, operations *[]rewriteservice.FieldOperation, index int, disabled bool) {
	operation := &(*operations)[index]
	number := strconv.Itoa(index + 1)
	action := func(key string, icon *ui.SVG, label string, blocked bool, run func()) {
		button := nui.Button(c.Key(key), theme, "", nui.ButtonOptions{Variant: nui.Ghost, Color: nui.Neutral, Size: nui.XS, Square: true, Leading: icon, AccessibleLabel: label + " " + number, Disabled: blocked})
		if !blocked {
			button.OnClick(run)
		}
	}
	swap := func(delta int) {
		list := append([]rewriteservice.FieldOperation(nil), *operations...)
		list[index], list[index+delta] = list[index+delta], list[index]
		*operations = list
	}
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(6).Children(func() {
		ui.Box(c.Key("operation")).Width(112).Shrink(0).Children(func() {
			nui.Select(c, theme, &operation.Operation, option([]string{"add", "set", "delete"}, operationLabel), nui.SelectOptions{Label: locales.RewriteRulesOperation() + " " + number, Size: nui.SM, Disabled: disabled})
		})
		ui.Box(c.Key("name")).Grow(1).Basis(0).MinWidth(96).Children(func() {
			nui.Input(c, theme, &operation.Name, nui.InputOptions{Label: locales.RewriteRulesFieldName() + " " + number, Placeholder: locales.RewriteRulesFieldName(), Size: nui.SM, Disabled: disabled}).FillWidth()
		})
		if operation.Operation != "delete" {
			ui.Box(c.Key("value")).Grow(1).Basis(0).MinWidth(96).Children(func() {
				nui.Input(c, theme, &operation.Value, nui.InputOptions{Label: locales.RewriteRulesFieldValue() + " " + number, Placeholder: locales.RewriteRulesFieldValue(), Size: nui.SM, Disabled: disabled}).FillWidth()
			})
		}
		action("up", lucide.ArrowUp(), locales.RewriteRulesMoveUp(), disabled || index == 0, func() { swap(-1) })
		action("down", lucide.ArrowDown(), locales.RewriteRulesMoveDown(), disabled || index == len(*operations)-1, func() { swap(1) })
		action("remove", lucide.X(), locales.RewriteRulesRemoveOperation(), disabled, func() {
			list := append([]rewriteservice.FieldOperation(nil), (*operations)[:index]...)
			*operations = append(list, (*operations)[index+1:]...)
		})
	})
}

func bodyModeLabel(value string) string {
	switch value {
	case "replace":
		return locales.RewriteRulesBodyReplace()
	case "regex":
		return locales.RewriteRulesBodyRegex()
	default:
		return locales.RewriteRulesBodyNone()
	}
}

func (f *ruleForm) body(c *ui.Context, theme nui.Theme, rule *store.RewriteRule, disabled bool) {
	body := &rule.Action.Body
	target := &body.Text
	if body.Mode == "regex" {
		target = &body.Replacement
	}
	if f.bodyKey != body.Mode || *target != f.bodyText {
		f.bodyKey, f.bodyText = body.Mode, *target
		f.replacement.SetText(*target)
	}
	f.replacement.OnChange = func(text string) { f.bodyText, *target = text, text }
	f.replacement.SetReadOnly(disabled)
	ui.Column(c).FillWidth().Gap(12).Children(func() {
		nui.FormField(c.Key("mode"), theme, nui.FormFieldOptions{Label: locales.RewriteRulesBody(), Disabled: disabled}, func(field nui.Theme) {
			ui.Box(c).Width(208).Children(func() {
				nui.Select(c.Key("select"), field, &body.Mode, option([]string{"none", "replace", "regex"}, bodyModeLabel), nui.SelectOptions{Label: locales.RewriteRulesBody(), Disabled: disabled})
			})
		})
		if body.Mode == "none" {
			return
		}
		ui.Text(c.Key("body-hint"), locales.RewriteRulesBodyHint()).FontSize(apptheme.TextSize(c, 12)).TextColor(theme.Muted).FillWidth()
		label := locales.RewriteRulesReplacementText()
		if body.Mode == "regex" {
			nui.FormField(c.Key("pattern"), theme, nui.FormFieldOptions{Label: locales.RewriteRulesRegexPattern(), Disabled: disabled}, func(field nui.Theme) {
				nui.Input(c.Key("input"), field, &body.Pattern, nui.InputOptions{Label: locales.RewriteRulesRegexPattern(), Disabled: disabled}).FillWidth()
			})
			ui.Text(c.Key("regex-hint"), locales.RewriteRulesRegexHint()).FontSize(apptheme.TextSize(c, 12)).TextColor(theme.Muted).FillWidth()
			label = locales.RewriteRulesRegexReplacement()
		}
		ui.Text(c.Key("label"), label).FontSize(apptheme.TextSize(c, 14)).Bold().TextColor(theme.Highlighted)
		ui.Box(c.Key("editor")).FillWidth().Height(224).Children(func() {
			f.replacement.Build(c, f.view.Data.Settings.CodeFont())
		})
	})
}

// matchTest tries a URL against the rule without sending anything.
type matchTest struct {
	method, url string
	result      *rewriteservice.PreviewResult
	err         string
	busy        bool
	sequence    int
	seen        *store.RewriteRule
	seenMethod  string
	seenURL     string
}

func (m *matchTest) invalidate() {
	m.sequence++
	m.result, m.err, m.busy = nil, "", false
}

func (m *matchTest) run(v *View, rule *store.RewriteRule) {
	m.sequence++
	sequence := m.sequence
	m.busy, m.err, m.result = false, "", nil
	rewrite := v.Data.Rewrite
	if !rewrite.Validate(rewrite.SelectedID) {
		m.err = locales.RewriteRulesValidationFixFields()
		return
	}
	m.busy = true
	candidate, method, url := *store.CloneRule(*rule), m.method, m.url
	runtime.Run(v.Data.Runtime.Jobs, "rewrite:match", func(context.Context) (rewriteservice.PreviewResult, error) {
		return v.Host.Preview(candidate, method, url)
	}, func(result rewriteservice.PreviewResult, err error) {
		if sequence != m.sequence {
			return
		}
		m.busy = false
		if err != nil {
			m.err = err.Error()
			return
		}
		m.result = &result
	})
}

func (m *matchTest) build(c *ui.Context, theme nui.Theme, v *View, draft *store.RuleDraft, rule *store.RewriteRule) {
	if m.seen == nil || !store.SameRuleContent(*m.seen, *rule) || m.seenMethod != m.method || m.seenURL != m.url {
		if m.seen != nil {
			m.invalidate()
		}
		m.seen, m.seenMethod, m.seenURL = store.CloneRule(*rule), m.method, m.url
	}
	ui.Column(c).FillWidth().Gap(12).Padding(12).Border(1, theme.Border).Radius(theme.Radius).Background(theme.MutedBackground).Children(func() {
		ui.Column(c).FillWidth().Gap(4).Children(func() {
			ui.Text(c, locales.RewriteRulesPreview()).FontSize(apptheme.TextSize(c, 14)).Bold().TextColor(theme.Highlighted)
			ui.Text(c, locales.RewriteRulesPreviewHint()).FontSize(apptheme.TextSize(c, 12)).TextColor(theme.Muted)
		})
		ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Box(c.Key("method")).Width(96).Shrink(0).Children(func() {
				nui.Input(c, theme, &m.method, nui.InputOptions{Label: locales.RewriteRulesMethod() + " test"}).FillWidth()
			})
			ui.Box(c.Key("url")).Grow(1).Basis(0).MinWidth(0).Children(func() {
				nui.Input(c, theme, &m.url, nui.InputOptions{Label: locales.RewriteRulesTestUrl(), Placeholder: locales.RewriteRulesTestUrl()}).FillWidth()
			})
			nui.Button(c.Key("test"), theme, locales.RewriteRulesTestMatch(), nui.ButtonOptions{Loading: m.busy}).OnClick(func() { m.run(v, rule) })
		})
		if m.err != "" {
			nui.Alert(c.Key("error"), theme, nui.AlertOptions{Description: m.err, Color: nui.Error, Variant: nui.Soft})
		}
		if m.result != nil {
			m.resultPane(c, theme)
		}
	})
}

func (m *matchTest) resultPane(c *ui.Context, theme nui.Theme) {
	result := m.result
	ui.Column(c.Key("result")).FillWidth().Gap(8).Children(func() {
		label, color := locales.RewriteRulesNotMatched(), nui.Neutral
		if result.Matched {
			label, color = locales.RewriteRulesMatched(), nui.Success
		}
		nui.Badge(c.Key("badge"), theme, label, nui.BadgeOptions{Color: color, Variant: nui.Soft})
		for index, capture := range result.Captures {
			ui.Text(c.Key(index), strconv.Itoa(index+1)+": "+capture).FontSize(apptheme.TextSize(c, 12)).Font(theme.MonoFont).TextColor(theme.Text).Selectable().FillWidth()
		}
		if result.TargetURL != "" {
			ui.Text(c.Key("target-label"), locales.RewriteRulesExpandedTarget()).FontSize(apptheme.TextSize(c, 12)).TextColor(theme.Muted)
			ui.Text(c.Key("target"), result.TargetURL).FontSize(apptheme.TextSize(c, 12)).Font(theme.MonoFont).TextColor(theme.Text).Selectable().FillWidth()
		}
	})
}

// bodyPreview tries a regular expression on sample text.
type bodyPreview struct {
	view *View

	sampleView, outputView *widgets.CodeView
	sample                 string
	sampleSeen             string
	previewing             bool
	result                 *rewriteservice.BodyPreviewResult
	err                    string
	busy                   bool
	wrap                   bool
	sequence               int
	cancel                 context.CancelFunc
	due                    time.Time
	scheduled              bool
	seenPattern            string
	seenReplacement        string
	started                bool
	diff                   *editor.DiffState
	diffResult             *rewriteservice.BodyPreviewResult
}

func newBodyPreview(v *View) *bodyPreview {
	p := &bodyPreview{view: v, wrap: true, sampleView: widgets.NewCodeView("plaintext", false), outputView: widgets.NewCodeView("plaintext", true)}
	p.sampleView.OnChange = func(text string) { p.sample, p.sampleSeen = text, text; p.invalidate(time.Time{}) }
	return p
}

func (p *bodyPreview) cancelPending() {
	p.sequence++
	p.scheduled = false
	if p.cancel != nil {
		p.cancel()
		p.cancel = nil
	}
	p.busy = false
}

func (p *bodyPreview) reset() {
	if p.started {
		p.cancelPending()
		p.started, p.previewing, p.result, p.err = false, false, nil, ""
	}
}

func (p *bodyPreview) editSample() {
	p.cancelPending()
	p.previewing, p.result, p.err = false, nil, ""
}

func (p *bodyPreview) invalidate(now time.Time) {
	p.cancelPending()
	p.err = ""
	if p.previewing {
		p.busy, p.scheduled, p.due = true, true, now.Add(bodyPreviewDelay)
		return
	}
	p.result = nil
}

func (p *bodyPreview) hasPreview() bool { return p.previewing && p.result != nil }

func (p *bodyPreview) largeSample() bool { return widgets.RequiresPlainText(p.sample) }

func (p *bodyPreview) largeResult() bool {
	return p.result != nil && (p.largeSample() || widgets.RequiresPlainText(p.result.Output))
}

func (p *bodyPreview) showDiff() bool { return p.hasPreview() && !p.largeResult() }

func (p *bodyPreview) canCopy() bool { return !p.previewing || (p.hasPreview() && !p.busy) }

func (p *bodyPreview) effectiveWrap() bool { return p.wrap && !p.largeSample() && !p.largeResult() }

func (p *bodyPreview) run(body *rewriteservice.BodyAction) {
	p.cancelPending()
	p.previewing, p.err = true, ""
	if len(p.sample) > maxPreviewSampleSize {
		p.result, p.err = nil, locales.RewriteRulesRegexTestTooLarge()
		return
	}
	p.sequence++
	sequence := p.sequence
	p.busy = true
	ctx, cancel := context.WithCancel(context.Background())
	p.cancel = cancel
	action, input := rewriteservice.BodyAction{Mode: "regex", Pattern: body.Pattern, Replacement: body.Replacement}, p.sample
	host := p.view.Host
	runtime.Run(p.view.Data.Runtime.Jobs, "rewrite:preview-body", func(context.Context) (rewriteservice.BodyPreviewResult, error) {
		return host.PreviewBody(ctx, action, input)
	}, func(result rewriteservice.BodyPreviewResult, err error) {
		cancel()
		if sequence != p.sequence {
			return
		}
		p.cancel, p.busy = nil, false
		if err != nil {
			p.result, p.err = nil, err.Error()
			return
		}
		p.result = &result
	})
}

func (p *bodyPreview) syncInputs(c *ui.Context, body *rewriteservice.BodyAction) {
	now := c.Now()
	if !p.started {
		p.started, p.seenPattern, p.seenReplacement = true, body.Pattern, body.Replacement
	}
	if body.Pattern != p.seenPattern || body.Replacement != p.seenReplacement {
		p.seenPattern, p.seenReplacement = body.Pattern, body.Replacement
		p.invalidate(now)
	}
	if p.sample != p.sampleSeen {
		p.sampleSeen = p.sample
		p.sampleView.SetText(p.sample)
		p.invalidate(now)
	}
	if p.scheduled {
		if remaining := p.due.Sub(now); remaining > 0 {
			c.After(remaining)
		} else {
			p.scheduled = false
			p.run(body)
		}
	}
	p.sampleView.SetReadOnly(p.previewing)
	p.sampleView.SetWordWrap(p.wrap && !p.largeSample())
	p.outputView.SetWordWrap(false)
}

func (p *bodyPreview) copy() {
	if !p.canCopy() {
		return
	}
	text := p.sample
	if p.previewing {
		text = p.result.Output
	}
	p.view.clipboard = &text
	p.view.Notifier.Success(locales.DetailBodyCopied())
}

func (p *bodyPreview) preparePreviewPane(theme nui.Theme) {
	if p.result == p.diffResult {
		return
	}
	p.diffResult = p.result
	if p.showDiff() {
		p.diff = &editor.DiffState{Old: editor.NewBuffer(p.sample), New: editor.NewBuffer(p.result.Output), Layout: editor.DiffUnified, ExpandUnchanged: true}
		return
	}
	p.diff = nil
	if p.hasPreview() {
		p.outputView.SetText(p.result.Output)
	}
}

func (p *bodyPreview) build(c *ui.Context, theme nui.Theme, body *rewriteservice.BodyAction) {
	p.syncInputs(c, body)
	p.preparePreviewPane(theme)
	ui.Column(c).FillWidth().Gap(12).Padding(12).Border(1, theme.Border).Radius(theme.Radius).Background(theme.MutedBackground).Children(func() {
		ui.Column(c).FillWidth().Gap(4).Children(func() {
			ui.Text(c, locales.RewriteRulesRegexTest()).FontSize(apptheme.TextSize(c, 14)).Bold().TextColor(theme.Highlighted)
			ui.Text(c, locales.RewriteRulesRegexTestHint()).FontSize(apptheme.TextSize(c, 12)).TextColor(theme.Muted).FillWidth()
		})
		ui.Column(c.Key("pane")).FillWidth().Border(1, theme.Border).Clip().Children(func() {
			p.toolbar(c, theme, body)
			ui.Box(c.Key("content")).FillWidth().Height(288).Background(theme.Background).Children(func() { p.content(c, theme) })
		})
		if p.busy {
			ui.Row(c.Key("status")).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Spinner(c)
				ui.Text(c, locales.RewriteRulesRegexTestUpdating()).FontSize(apptheme.TextSize(c, 12)).TextColor(theme.Muted)
			})
		} else if p.hasPreview() {
			label, color := locales.RewriteRulesNotMatched(), nui.Neutral
			if p.result.MatchCount > 0 {
				label, color = locales.RewriteRulesRegexTestMatches(strconv.Itoa(p.result.MatchCount)), nui.Success
			}
			nui.Badge(c.Key("count"), theme, label, nui.BadgeOptions{Color: color, Variant: nui.Soft})
		}
		if p.err != "" {
			nui.Alert(c.Key("error"), theme, nui.AlertOptions{Description: p.err, Color: nui.Error, Variant: nui.Soft})
		}
	})
}

func (p *bodyPreview) toolbar(c *ui.Context, theme nui.Theme, body *rewriteservice.BodyAction) {
	title, copyLabel := locales.RewriteRulesRegexTestInput(), locales.RewriteRulesRegexTestCopyInput()
	if p.previewing {
		title, copyLabel = locales.RewriteRulesRegexTestOutput(), locales.RewriteRulesRegexTestCopyOutput()
	}
	ui.Row(c.Key("head")).FillWidth().AlignItems(ui.Center).Justify(ui.SpaceBetween).Padding(6, 12).BorderWidth(0, 0, 1, 0).BorderColor(theme.Border).Children(func() {
		ui.Text(c, title).FontSize(apptheme.TextSize(c, 14)).Bold().TextColor(theme.Highlighted)
		ui.Row(c).Gap(4).AlignItems(ui.Center).Children(func() {
			iconButton(c, theme, "copy", lucide.Copy(), copyLabel, nui.Neutral, !p.canCopy(), p.copy)
			color := nui.Neutral
			if p.effectiveWrap() {
				color = nui.Primary
			}
			iconButton(c, theme, "wrap", lucide.TextWrap(), locales.RewriteRulesRegexTestWrap(), color, p.largeSample() || p.largeResult(), func() { p.wrap = !p.wrap })
			if p.previewing {
				iconButton(c, theme, "edit", lucide.Pencil(), locales.RewriteRulesRegexTestEdit(), nui.Neutral, false, p.editSample)
			} else {
				iconButton(c, theme, "preview", lucide.ScanText(), locales.RewriteRulesRegexTestPreview(), nui.Neutral, false, func() { p.run(body) })
			}
		})
	})
}

func iconButton(c *ui.Context, theme nui.Theme, key string, icon *ui.SVG, label string, color nui.Color, disabled bool, run func()) {
	button := nui.Button(c.Key(key), theme, "", nui.ButtonOptions{Variant: nui.Ghost, Color: color, Size: nui.XS, Square: true, Leading: icon, AccessibleLabel: label, Disabled: disabled})
	if !disabled {
		button.OnClick(run)
	}
	nui.Tooltip(c, theme, button, label, nui.TooltipOptions{})
}

func (p *bodyPreview) content(c *ui.Context, theme nui.Theme) {
	font := p.view.Data.Settings.CodeFont()
	switch {
	case p.showDiff() && p.diff != nil:
		p.diff.Font = font
		p.diff.Wrap = p.effectiveWrap()
		editor.DiffView(c.Key("diff"), p.diff).Fill()
	case p.hasPreview():
		p.outputView.BuildUnframed(c.Key("output"), font)
	default:
		p.sampleView.BuildUnframed(c.Key("sample"), font)
	}
}
