// Package rewrite builds the Rewrite Rules section.
package rewrite

import (
	"context"
	"slices"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/internal/desktop/shortcuts"
	"github.com/josexy/flowlens/internal/desktop/store"
	apptheme "github.com/josexy/flowlens/internal/desktop/theme"
	"github.com/josexy/flowlens/internal/desktop/widgets"
	rewriteservice "github.com/josexy/flowlens/internal/services/rewrite_service"
	"github.com/josexy/flowlens/locales"
	"github.com/josexy/nuxt-ui-go/icons/lucide"
	nui "github.com/josexy/nuxt-ui-go/ui"
)

// Host is what the section needs from the rest of the application.
type Host struct {
	// Preview tests a rule against a URL.
	Preview func(rule store.RewriteRule, method, url string) (rewriteservice.PreviewResult, error)
	// PreviewBody tests a regular expression against sample text.
	PreviewBody func(ctx context.Context, action rewriteservice.BodyAction, input string) (rewriteservice.BodyPreviewResult, error)
	// Kbds returns the keys of a command.
	Kbds func(commandID string) []string
	// Select shows the Rewrite Rules section.
	Select func()
}

// View is the Rewrite Rules sidebar and page.
type View struct {
	Data       *store.Store
	Host       Host
	Dispatcher *shortcuts.Dispatcher
	Notifier   *widgets.Notifier

	search      string
	list        widgets.ReorderListState
	localOrder  []string
	forms       map[string]*ruleForm
	remove      deleteDialog
	guard       quitDialog
	clipboard   *string
	seenRule    *store.RewriteRule
	seenContent *store.RewriteRule
	initialized bool
}

// New returns the section of a window.
func New(data *store.Store, host Host, dispatcher *shortcuts.Dispatcher, notifier *widgets.Notifier) *View {
	return &View{Data: data, Host: host, Dispatcher: dispatcher, Notifier: notifier, forms: map[string]*ruleForm{}}
}

func (v *View) ensureInitialized() {
	if !v.initialized {
		v.initialized = true
		v.Data.Rewrite.Initialize()
	}
}

func filterRow(row store.RewriteRow, query string) bool {
	if query == "" {
		return true
	}
	rule := row.Rule
	return strings.Contains(strings.ToLower(rule.Name+"\n"+rule.Method+"\n"+rule.URLPattern), query)
}

// rows returns the rows in display order, including an order being saved.
func (v *View) rows() []store.RewriteRow {
	rows := v.Data.Rewrite.Rows()
	query := strings.ToLower(strings.TrimSpace(v.search))
	filtered := make([]store.RewriteRow, 0, len(rows))
	for _, row := range rows {
		if filterRow(row, query) {
			filtered = append(filtered, row)
		}
	}
	if v.localOrder == nil {
		return filtered
	}
	position := func(key string) int {
		if index := slices.Index(v.localOrder, key); index >= 0 {
			return index
		}
		return len(v.localOrder) + 1
	}
	slices.SortStableFunc(filtered, func(a, b store.RewriteRow) int { return position(a.Key) - position(b.Key) })
	return filtered
}

// Sidebar builds the rule list.
func (v *View) Sidebar(c *ui.Context, theme nui.Theme) {
	v.ensureInitialized()
	rewrite := v.Data.Rewrite
	// Pending writes block further mutations without dimming every control.
	// Only the active control shows loading; unavailable controls still dim.
	ui.Column(c.Key("rewrite-sidebar")).Fill().MinHeight(0).Children(func() {
		ui.Column(c).FillWidth().Shrink(0).Gap(10).Padding(10).BorderWidth(0, 0, 1, 0).BorderColor(theme.Border).Children(func() {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
				ui.Text(c, locales.RewriteRulesTitle()).FontSize(apptheme.TextSize(c, 14)).FontWeight(600).TextColor(theme.Highlighted)
				ui.Row(c).Gap(2).Children(func() {
					refresh := nui.Button(c.Key("refresh"), theme, "", nui.ButtonOptions{Variant: nui.Ghost, Color: nui.Neutral, Size: nui.XS, Square: true, Leading: lucide.RefreshCw(), AccessibleLabel: locales.RewriteRulesRefresh(), Loading: rewrite.Loading, Disabled: rewrite.Busy()}).DimDisabled(false)
					if !rewrite.Busy() {
						refresh.OnClick(func() { rewrite.Refresh(true, nil) })
					}
					nui.Tooltip(c, theme, refresh, locales.RewriteRulesRefresh(), nui.TooltipOptions{})
					disabled := rewrite.Busy() || !rewrite.Ready()
					create := nui.Button(c.Key("new"), theme, "", nui.ButtonOptions{Variant: nui.Soft, Color: nui.Primary, Size: nui.XS, Square: true, Leading: lucide.Plus(), AccessibleLabel: locales.RewriteRulesNewRule(), Disabled: disabled}).DimDisabled(!rewrite.Ready())
					if !disabled {
						create.OnClick(v.create)
					}
					nui.Tooltip(c, theme, create, locales.RewriteRulesNewRule(), nui.TooltipOptions{})
				})
			})
			enabled := rewrite.State.Enabled
			loading := rewrite.Pending != nil && rewrite.Pending.Kind == store.RewriteMutationEnabled
			if loading {
				enabled = rewrite.Pending.Enabled
			}
			before := enabled
			nui.Switch(c.Key("enable-all"), theme, &enabled, nui.SwitchOptions{Label: locales.RewriteRulesEnableAll(), Disabled: rewrite.Busy() || !rewrite.Ready(), Loading: loading}).DimDisabled(!rewrite.Ready())
			if enabled != before {
				rewrite.SetEnabled(enabled, func(bool) {})
			}
			nui.Input(c.Key("search"), theme, &v.search, nui.InputOptions{Label: locales.RewriteRulesSearch(), Placeholder: locales.RewriteRulesSearch(), Leading: lucide.Search(), Size: nui.SM}).FillWidth()
		})
		rows := v.rows()
		if len(rows) == 0 {
			v.emptyList(c, theme)
		}
		reorderable := strings.TrimSpace(v.search) == "" && rewrite.Ready()
		usable := reorderable && !rewrite.Busy()
		v.list.Build(c.Key("rules"), savedIDs(rows), strconv.FormatInt(rewrite.State.Revision, 10), usable, len(rows), func(ids []string) {
			v.localOrder = ids
			rewrite.Reorder(ids, func(bool) { v.localOrder = nil })
		}).FillWidth().Grow(1).Basis(0).MinHeight(0).PaddingY(6).
			ItemKey(func(index int) any { return rows[index].Key }).Rows(func(item ui.ListRow) {
			v.row(item.Context, theme, rows[item.Index], reorderable)
		})
	})
}

func (v *View) emptyList(c *ui.Context, theme nui.Theme) {
	switch {
	case v.Data.Rewrite.Loading:
		ui.Row(c.Key("loading")).FillWidth().Height(96).Center().Children(func() {
			ui.Text(c, locales.RewriteRulesLoading()).FontSize(apptheme.TextSize(c, 14)).TextColor(theme.Muted)
		})
	case strings.TrimSpace(v.search) != "":
		widgets.EmptyState(c.Key("empty"), theme, nui.EmptyOptions{Icon: lucide.Repeat2(), Title: locales.RewriteRulesNoSearchResults()})
	default:
		widgets.EmptyState(c.Key("empty"), theme, nui.EmptyOptions{Icon: lucide.Repeat2(), Title: locales.RewriteRulesNoRules()})
	}
}

func (v *View) create() {
	v.Data.Rewrite.Create(locales.RewriteRulesNewName())
	if v.Host.Select != nil {
		v.Host.Select()
	}
}

func (v *View) selectRule(key string) {
	v.Data.Rewrite.Select(key)
	if v.Host.Select != nil {
		v.Host.Select()
	}
}

func actionLabel(rule *store.RewriteRule) string {
	if rule.UnavailableReason != "" {
		return locales.RewriteRulesUnavailable()
	}
	switch rule.Action.Type {
	case rewriteservice.ActionRedirect:
		return locales.RewriteRulesActionRedirect()
	case rewriteservice.ActionResponse:
		return locales.RewriteRulesActionResponse()
	default:
		return locales.RewriteRulesActionRequest()
	}
}

// Minimum rule row pitch, scaled with the app font size.
const ruleRowHeight float32 = 92

func (v *View) row(c *ui.Context, theme nui.Theme, row store.RewriteRow, reorderable bool) {
	rewrite := v.Data.Rewrite
	selected := rewrite.SelectedID == row.Key
	name := row.Rule.Name
	if name == "" {
		name = locales.RewriteRulesNewName()
	}
	box := ui.ButtonBase(c.Key(row.Key)).Role(ui.RoleGroup).Column().AlignItems(ui.Stretch).Justify(ui.Start).FillWidth().Height(max(ruleRowHeight, apptheme.TextSize(c, ruleRowHeight))).Padding(8, 8, 8, 5).Gap(4).FocusRing(true).AccessSelected(selected).Label(name)
	box.BorderWidth(0, 0, 0, 3).BorderColor(ui.Transparent).Background(theme.Background)
	switch {
	case selected:
		box.BorderColor(theme.Primary).Background(theme.Primary.Alpha(0.1).Over(theme.Background))
	case box.Hovered():
		box.Background(theme.Elevated)
	}
	if row.Saved == nil {
		// Drafts keep their position and cannot initiate a native row drag.
		box.Dragged()
	}
	box.OnClick(func() { v.selectRule(row.Key) })
	box.Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(6).Children(func() {
			ui.Box(c.Key("grip")).Shrink(0).Disabled(rewrite.Busy()).DimDisabled(false).Children(func() {
				widgets.ReorderGrip(c, theme, "drag "+row.Rule.Name, reorderable && row.Saved != nil)
			})
			ui.Row(c).Grow(1).Basis(0).MinWidth(0).AlignItems(ui.Center).Gap(6).Children(func() {
				ui.Text(c, name).SingleLine().Shrink(1).FontSize(apptheme.TextSize(c, 14)).FontWeight(500).TextColor(theme.Highlighted).MinWidth(0)
				if rewrite.IsDirty(row.Key) {
					ui.Box(c.Key("dirty")).Size(6, 6).Shrink(0).Radius(3).Background(theme.Primary).Label(locales.RewriteRulesUnsaved())
				}
			})
			v.ruleSwitch(c, theme, row)
		})
		ui.Text(c, row.Rule.URLPattern).SingleLine().FontSize(apptheme.TextSize(c, 12)).Font(theme.MonoFont).TextColor(theme.Muted)
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(4).Children(func() {
			nui.Badge(c.Key("method"), theme, row.Rule.Method, nui.BadgeOptions{Color: nui.Neutral, Variant: nui.Subtle, Size: nui.SM})
			color := nui.Neutral
			if row.Rule.UnavailableReason != "" {
				color = nui.Error
			}
			nui.Badge(c.Key("action"), theme, actionLabel(row.Rule), nui.BadgeOptions{Color: color, Variant: nui.Subtle, Size: nui.SM})
			if row.Saved == nil {
				ui.Text(c, locales.RewriteRulesDraft()).FontSize(apptheme.TextSize(c, 12)).TextColor(theme.Muted)
			}
		})
	})
}

func (v *View) ruleSwitch(c *ui.Context, theme nui.Theme, row store.RewriteRow) {
	rewrite := v.Data.Rewrite
	enabled := row.Saved != nil && row.Saved.Enabled
	loading := rewrite.Pending != nil && rewrite.Pending.Kind == store.RewriteMutationRuleEnabled && rewrite.Pending.ID == row.Key
	if loading {
		enabled = rewrite.Pending.Enabled
	}
	before := enabled
	unavailable := row.Saved == nil || row.Saved.UnavailableReason != ""
	nui.Switch(c.Key("enabled"), theme, &enabled, nui.SwitchOptions{Size: nui.SM, Disabled: unavailable || rewrite.Busy(), Loading: loading}).DimDisabled(unavailable).Label(locales.RewriteRulesEnableRule(row.Rule.Name))
	if enabled != before {
		rewrite.SetRuleEnabled(row.Key, enabled, func(bool) {})
	}
}

func savedIDs(rows []store.RewriteRow) []string {
	var ids []string
	for _, row := range rows {
		if row.Saved != nil {
			ids = append(ids, row.Key)
		}
	}
	return ids
}

func (v *View) prune() {
	rewrite := v.Data.Rewrite
	live := map[string]bool{}
	for _, row := range rewrite.Rows() {
		if draft := rewrite.Draft(row.Key); draft != nil {
			live[draft.EditorKey] = true
		}
	}
	for key, form := range v.forms {
		if !live[key] {
			form.close()
			delete(v.forms, key)
		}
	}
}

func (v *View) canSave() bool {
	rewrite := v.Data.Rewrite
	draft := rewrite.SelectedDraft()
	return draft != nil && rewrite.IsDirty(rewrite.SelectedID) && !draft.Conflict && draft.Rule.UnavailableReason == ""
}

func (v *View) save() { v.Data.Rewrite.Save("", func(store.RuleSaveResult) {}) }

func (v *View) declareShortcut() {
	rewrite := v.Data.Rewrite
	v.Dispatcher.Handle(shortcuts.Handler{
		CommandID: shortcuts.AppSave,
		When: func() bool {
			draft := rewrite.SelectedDraft()
			return !rewrite.Busy() && draft != nil && !draft.Conflict
		},
		Run: v.save,
	})
}

// clearErrorOnEdit forgets a shown error once the draft changes.
func (v *View) clearErrorOnEdit() {
	rule := v.Data.Rewrite.SelectedRule()
	if rule == nil {
		v.seenRule, v.seenContent = nil, nil
		return
	}
	if rule != v.seenRule || v.seenContent == nil {
		v.seenRule, v.seenContent = rule, store.CloneRule(*rule)
		return
	}
	if !store.SameRuleContent(*rule, *v.seenContent) {
		v.seenContent = store.CloneRule(*rule)
		v.Data.Rewrite.Error = ""
	}
}

// Content builds the page of the selected rule.
func (v *View) Content(c *ui.Context, theme nui.Theme) {
	v.ensureInitialized()
	if v.clipboard != nil {
		c.WriteClipboard(*v.clipboard)
		v.clipboard = nil
	}
	v.declareShortcut()
	v.prune()
	v.clearErrorOnEdit()
	rewrite := v.Data.Rewrite
	draft := rewrite.SelectedDraft()
	ui.Column(c.Key("rewrite-page")).Fill().MinHeight(0).Background(theme.Background).Children(func() {
		v.header(c, theme, draft)
		if rewrite.Error != "" {
			pageAlert(c, theme, "error", rewrite.Error, nui.Error)
		}
		if draft != nil && draft.Conflict {
			v.conflictBanner(c, theme)
		}
		if draft != nil && draft.Rule.UnavailableReason != "" {
			pageAlert(c, theme, "unavailable", draft.Rule.UnavailableReason, nui.Error)
		}
		switch {
		case draft != nil:
			form := v.forms[draft.EditorKey]
			if form == nil {
				form = newRuleForm(v)
				v.forms[draft.EditorKey] = form
			}
			ui.Scroll(c.Key(draft.EditorKey)).FillWidth().Grow(1).Basis(0).MinHeight(0).Children(func() {
				form.build(c, theme, draft)
			})
		case rewrite.Loading:
			ui.Row(c.Key("loading")).FillWidth().Grow(1).Basis(0).Center().Gap(8).Children(func() {
				widgets.Spinner(c, theme)
				ui.Text(c, locales.RewriteRulesLoading()).FontSize(apptheme.TextSize(c, 14)).TextColor(theme.Muted)
			})
		default:
			v.emptyPage(c, theme)
		}
	})
	v.remove.build(c, theme, v)
}

func pageAlert(c *ui.Context, theme nui.Theme, key, text string, color nui.Color) {
	ui.Box(c.Key(key)).FillWidth().Shrink(0).Children(func() {
		nui.Alert(c, theme, nui.AlertOptions{Description: text, Color: color, Variant: nui.Subtle})
	})
}

func (v *View) emptyPage(c *ui.Context, theme nui.Theme) {
	rewrite := v.Data.Rewrite
	ui.Column(c.Key("empty")).FillWidth().Grow(1).Basis(0).MinHeight(0).Children(func() {
		nui.Empty(c, theme, nui.EmptyOptions{
			Icon: lucide.Repeat2(), Title: locales.RewriteRulesEmptyTitle(), Description: locales.RewriteRulesEmptyDescription(),
			Actions: func() {
				disabled := rewrite.Busy() || !rewrite.Ready()
				button := nui.Button(c.Key("create"), theme, locales.RewriteRulesNewRule(), nui.ButtonOptions{Leading: lucide.Plus(), Disabled: disabled})
				if !disabled {
					button.OnClick(v.create)
				}
			},
		}).Grow(1).Basis(0)
	})
}

func (v *View) header(c *ui.Context, theme nui.Theme, draft *store.RuleDraft) {
	rewrite := v.Data.Rewrite
	title := locales.RewriteRulesTitle()
	if draft != nil && draft.Rule.Name != "" {
		title = draft.Rule.Name
	}
	ui.Row(c.Key("rewrite-header")).FillWidth().Height(48).Shrink(0).AlignItems(ui.Center).Gap(8).PaddingX(12).BorderWidth(0, 0, 1, 0).BorderColor(theme.Border).Children(func() {
		ui.Icon(c, lucide.Repeat2()).Size(16, 16).Shrink(0).TextColor(theme.Primary)
		ui.Text(c, title).SingleLine().Grow(1).Basis(0).MinWidth(0).FontSize(apptheme.TextSize(c, 14)).Bold().TextColor(theme.Highlighted)
		if rewrite.SelectedID != "" && rewrite.IsDirty(rewrite.SelectedID) {
			nui.Badge(c.Key("unsaved"), theme, locales.RewriteRulesUnsaved(), nui.BadgeOptions{Color: nui.Warning, Variant: nui.Soft})
		}
		canRevert := rewrite.SelectedID != "" && (rewrite.IsDirty(rewrite.SelectedID) || (draft != nil && draft.Conflict))
		revert := nui.Button(c.Key("revert"), theme, locales.RewriteRulesRevert(), nui.ButtonOptions{Variant: nui.Ghost, Color: nui.Neutral, Disabled: rewrite.Busy() || !canRevert}).DimDisabled(!canRevert)
		if !rewrite.Busy() && canRevert {
			revert.OnClick(func() { rewrite.Revert("") })
		}
		saving := rewrite.Pending != nil && rewrite.Pending.Kind == store.RewriteMutationSave && rewrite.Pending.ID == rewrite.SelectedID
		canSave := v.canSave()
		save := nui.Button(c.Key("save"), theme, locales.RewriteRulesSave(), nui.ButtonOptions{Leading: lucide.Save(), Loading: saving, Disabled: rewrite.Busy() || !canSave}).DimDisabled(!canSave)
		if !rewrite.Busy() && canSave {
			save.OnClick(v.save)
		}
		var kbds []string
		if v.Host.Kbds != nil {
			kbds = v.Host.Kbds(shortcuts.AppSave)
		}
		nui.Tooltip(c, theme, save, locales.RewriteRulesSave(), nui.TooltipOptions{Kbds: kbds})
		disabled := rewrite.Busy() || draft == nil
		trash := nui.Button(c.Key("delete"), theme, "", nui.ButtonOptions{Variant: nui.Ghost, Color: nui.Error, Square: true, Leading: lucide.Trash(), AccessibleLabel: locales.RewriteRulesDelete(), Disabled: disabled}).DimDisabled(draft == nil)
		if !disabled {
			trash.OnClick(func() { v.remove.show(v) })
		}
	})
}

func (v *View) conflictBanner(c *ui.Context, theme nui.Theme) {
	rewrite := v.Data.Rewrite
	ui.Column(c.Key("conflict")).FillWidth().Shrink(0).Gap(8).Padding(12).Background(theme.Warning.Alpha(0.1)).BorderWidth(0, 0, 1, 0).BorderColor(theme.Warning.Alpha(0.3)).Children(func() {
		ui.Text(c, locales.RewriteRulesConflict()).FontSize(apptheme.TextSize(c, 14)).TextColor(theme.Warning).FillWidth()
		ui.Row(c).Gap(8).Children(func() {
			keep := nui.Button(c.Key("keep"), theme, locales.RewriteRulesKeepDraft(), nui.ButtonOptions{Variant: nui.Outline, Color: nui.Warning, Size: nui.XS, Disabled: rewrite.Busy()}).DimDisabled(false)
			load := nui.Button(c.Key("load"), theme, locales.RewriteRulesLoadSaved(), nui.ButtonOptions{Variant: nui.Outline, Color: nui.Neutral, Size: nui.XS, Disabled: rewrite.Busy()}).DimDisabled(false)
			if !rewrite.Busy() {
				keep.OnClick(func() { rewrite.AcceptConflict("") })
				load.OnClick(func() { rewrite.Revert("") })
			}
		})
	})
}

type deleteDialog struct {
	open  bool
	id    string
	name  string
	dirty bool
}

func (d *deleteDialog) show(v *View) {
	rewrite := v.Data.Rewrite
	rule := rewrite.SelectedRule()
	if rule == nil {
		return
	}
	*d = deleteDialog{open: true, id: rewrite.SelectedID, name: rule.Name, dirty: rewrite.IsDirty(rewrite.SelectedID)}
}

func (d *deleteDialog) run(v *View, saveFirst bool) {
	rewrite := v.Data.Rewrite
	finish := func(id string) {
		rewrite.Remove(id, func(removed bool) {
			if removed {
				d.open = false
			}
		})
	}
	if !saveFirst {
		finish(d.id)
		return
	}
	rewrite.Save(d.id, func(result store.RuleSaveResult) {
		if result.Saved {
			finish(result.RuleID)
		}
	})
}

func fieldErrorMessage(code string) string {
	switch code {
	case store.RuleErrorNameRequired:
		return locales.RewriteRulesValidationNameRequired()
	case store.RuleErrorNameTooLong:
		return locales.RewriteRulesValidationNameTooLong()
	case store.RuleErrorURLRequired:
		return locales.RewriteRulesValidationUrlRequired()
	case store.RuleErrorURLTooLong:
		return locales.RewriteRulesValidationUrlTooLong()
	default:
		return ""
	}
}

func (d *deleteDialog) build(c *ui.Context, theme nui.Theme, v *View) {
	if !d.open {
		return
	}
	rewrite := v.Data.Rewrite
	busy := rewrite.Busy()
	v.Dispatcher.NoteModal()
	nui.ModalWithOptions(c.Key("delete-rule"), theme, &d.open, nui.ModalOptions{Title: locales.RewriteRulesDelete(), Width: nui.Set[float32](460), Dismissible: nui.Set(!busy), Footer: func(c *ui.Context, palette nui.Theme) {
		ui.Row(c).FillWidth().Justify(ui.End).Gap(8).Children(func() {
			nui.Button(c.Key("cancel"), palette, locales.RewriteRulesCancel(), nui.ButtonOptions{Variant: nui.Outline, Color: nui.Neutral, Disabled: busy}).OnClick(func() { d.open = false })
			label := locales.RewriteRulesDelete()
			if d.dirty {
				label = locales.RewriteRulesDiscardDelete()
			}
			nui.Button(c.Key("confirm"), palette, label, nui.ButtonOptions{Variant: nui.Outline, Color: nui.Error, Disabled: busy}).OnClick(func() { d.run(v, false) })
			if d.dirty {
				draft := rewrite.SelectedDraft()
				blocked := busy || (draft != nil && draft.Conflict)
				button := nui.Button(c.Key("save-delete"), palette, locales.RewriteRulesSaveDelete(), nui.ButtonOptions{Disabled: blocked})
				if !blocked {
					button.OnClick(func() { d.run(v, true) })
				}
			}
		})
	}}, func(palette nui.Theme) {
		ui.Column(c).FillWidth().Gap(10).Children(func() {
			message := locales.RewriteRulesDeleteConfirm(d.name)
			if d.dirty {
				message = locales.RewriteRulesDeleteDirty(d.name)
			}
			ui.Text(c, message).FontSize(apptheme.TextSize(c, 14)).TextColor(palette.Text).FillWidth()
			if field := fieldErrorMessage(rewrite.FieldErrors(d.id).First()); field != "" {
				nui.Alert(c.Key("field"), palette, nui.AlertOptions{Description: field, Color: nui.Error, Variant: nui.Soft})
			}
			if rewrite.Error != "" {
				nui.Alert(c.Key("error"), palette, nui.AlertOptions{Description: rewrite.Error, Color: nui.Error, Variant: nui.Soft})
			}
			if draft := rewrite.SelectedDraft(); draft != nil && draft.Conflict {
				nui.Alert(c.Key("conflict"), palette, nui.AlertOptions{Description: locales.RewriteRulesConflict(), Color: nui.Warning, Variant: nui.Soft})
			}
		})
	})
}

// QuitPrompt asks what to do with unsaved rules before quitting or updating.
type QuitPrompt struct {
	Reason  string
	Respond func(decision string)
}

// Decisions a quit prompt can get.
const (
	DecisionSave    = "save"
	DecisionDiscard = "discard"
	DecisionCancel  = "cancel"
)

// ReasonUpdate is the reason of a prompt before an update restart.
const ReasonUpdate = "update"

type quitDialog struct {
	prompt *QuitPrompt
}

// AskQuit shows the prompt, or answers it when nothing is unsaved.
func (v *View) AskQuit(prompt QuitPrompt) {
	v.guard.prompt = &prompt
	if !v.Data.Rewrite.HasDirtyDrafts() {
		v.reply(DecisionSave)
	}
}

// QuitPending reports whether a prompt is open.
func (v *View) QuitPending() bool { return v.guard.prompt != nil }

func (v *View) reply(decision string) {
	rewrite := v.Data.Rewrite
	prompt := v.guard.prompt
	if prompt == nil || rewrite.Busy() {
		return
	}
	finish := func() {
		v.guard.prompt = nil
		prompt.Respond(decision)
	}
	switch decision {
	case DecisionSave:
		rewrite.SaveAll(func(saved bool) {
			if !saved {
				if v.Host.Select != nil {
					v.Host.Select()
				}
				return
			}
			finish()
		})
	case DecisionDiscard:
		rewrite.DiscardAll()
		finish()
	default:
		finish()
	}
}

// Dirty reports whether unsaved rules exist.
func (v *View) Dirty() bool { return v.Data.Rewrite.HasDirtyDrafts() }

// Overlay builds the prompt shown before quitting.
func (v *View) Overlay(c *ui.Context, theme nui.Theme) {
	if v.guard.prompt == nil {
		return
	}
	rewrite := v.Data.Rewrite
	prompt := v.guard.prompt
	open := true
	busy := rewrite.Busy()
	count := strconv.Itoa(len(rewrite.DirtyIDs()))
	v.Dispatcher.NoteModal()
	nui.ModalWithOptions(c.Key("rewrite-quit"), theme, &open, nui.ModalOptions{Title: locales.RewriteRulesQuitTitle(), Width: nui.Set[float32](480), Dismissible: nui.Set(false), CloseButton: nui.Set(false), Footer: func(c *ui.Context, palette nui.Theme) {
		ui.Row(c).FillWidth().Justify(ui.End).Gap(8).Children(func() {
			nui.Button(c.Key("cancel"), palette, locales.RewriteRulesCancel(), nui.ButtonOptions{Variant: nui.Outline, Color: nui.Neutral, Disabled: busy}).DimDisabled(false).OnClick(func() { v.reply(DecisionCancel) })
			nui.Button(c.Key("discard"), palette, locales.RewriteRulesDiscardContinue(), nui.ButtonOptions{Variant: nui.Outline, Color: nui.Error, Disabled: busy}).DimDisabled(false).OnClick(func() { v.reply(DecisionDiscard) })
			nui.Button(c.Key("save"), palette, locales.RewriteRulesSaveContinue(), nui.ButtonOptions{Leading: lucide.Save(), Loading: busy}).DimDisabled(false).OnClick(func() { v.reply(DecisionSave) })
		})
	}}, func(palette nui.Theme) {
		ui.Column(c).FillWidth().Gap(10).Children(func() {
			message := locales.RewriteRulesQuitDirty(count)
			if prompt.Reason == ReasonUpdate {
				message = locales.RewriteRulesUpdateDirty(count)
			}
			ui.Text(c, message).FontSize(apptheme.TextSize(c, 14)).TextColor(palette.Text).FillWidth()
			if field := fieldErrorMessage(rewrite.FieldErrors(rewrite.SelectedID).First()); field != "" {
				nui.Alert(c.Key("field"), palette, nui.AlertOptions{Description: field, Color: nui.Error, Variant: nui.Soft})
			}
			if rewrite.Error != "" {
				nui.Alert(c.Key("error"), palette, nui.AlertOptions{Description: rewrite.Error, Color: nui.Error, Variant: nui.Soft})
			}
			if draft := rewrite.SelectedDraft(); draft != nil && draft.Conflict {
				nui.Alert(c.Key("conflict"), palette, nui.AlertOptions{Description: locales.RewriteRulesConflict(), Color: nui.Warning, Variant: nui.Soft})
			}
		})
	})
}
