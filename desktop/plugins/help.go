package plugins

import (
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/desktop/pythonapi"
	"github.com/josexy/flowlens/desktop/store"
	apptheme "github.com/josexy/flowlens/desktop/theme"
	"github.com/josexy/flowlens/desktop/widgets"
	"github.com/josexy/nuxt-ui-go/icons/lucide"
	nui "github.com/josexy/nuxt-ui-go/ui"
)

// helpWidth is the help panel's width, as 1.0.5's.
const helpWidth float32 = 420

// Help is the Python API reference, which slides over the window from its
// right edge.
type Help struct {
	Workbench *store.WorkbenchStore
	Notifier  *widgets.Notifier
	// Locale returns the language on show.
	Locale func() string
	// Context identifies what the panel was opened for; it closes when that
	// changes.
	Context func() string

	tab       string
	query     string
	expanded  []string
	seenQuery string
	seenScope string
	details   []string
	clipboard *string
	open      bool
	opened    string
}

// NewHelp returns the panel of a window.
func NewHelp(workbench *store.WorkbenchStore, notifier *widgets.Notifier, locale func() string) *Help {
	return &Help{Workbench: workbench, Notifier: notifier, Locale: locale, tab: "quickstart"}
}

// Visible reports whether the panel is open.
func (h *Help) Visible() bool { return h.Workbench.PythonHelpScope != "" }

func (h *Help) prepare(c *ui.Context) {
	if h.clipboard != nil {
		c.WriteClipboard(*h.clipboard)
		h.clipboard = nil
	}
	scope := h.Workbench.PythonHelpScope
	if scope == "" {
		h.seenScope = ""
		return
	}
	context := ""
	if h.Context != nil {
		context = h.Context()
	}
	if context != h.opened && h.seenScope != "" {
		h.seenScope = ""
		h.Workbench.ClosePythonHelp()
		return
	}
	if scope != h.seenScope {
		if h.seenScope == "" {
			h.tab, h.query, h.opened = "quickstart", "", context
		}
		h.seenScope = scope
	}
}

func (h *Help) copy(text, success string) {
	h.clipboard = &text
	h.Notifier.Success(success)
}

// Build shows the panel in a Slideover from the window's right edge. Call it
// every frame, shown or not, so the panel slides out when it closes.
func (h *Help) Build(c *ui.Context, theme nui.Theme) {
	h.prepare(c)
	h.open = h.Visible()
	messages := pythonapi.MessagesFor(h.Locale())
	nui.Slideover(c.Key("python-help"), theme, &h.open, nui.SlideoverOptions{
		Side: nui.SideRight, Extent: nui.Set(helpWidth),
		ModalOptions: nui.ModalOptions{
			Title: messages.Title, Description: messages.Version, CloseLabel: messages.Close,
			CloseButton: nui.Set(false),
			// The close button, Escape and a click beside the panel close it
			// after the build, which derives open from the store again.
			OnClose: h.Workbench.ClosePythonHelp,
			Header:  func(c *ui.Context, palette nui.Theme) { h.header(c, palette, messages) },
			Decoration: nui.Decoration{
				UI: nui.Styles{
					nui.Header: {PaddingX: nui.Set[float32](16), PaddingY: nui.Set[float32](12)},
					nui.Body:   {PaddingX: nui.Set[float32](16), PaddingY: nui.Set[float32](16), Gap: nui.Set[float32](16)},
				},
			},
		},
	}, func(palette nui.Theme) {
		if h.tab == "reference" {
			h.reference(c, palette, h.Locale(), messages)
			return
		}
		h.quickstart(c, palette, h.Workbench.PythonHelpScope, messages)
	})
}

// header shows the title, the scope with the prompt to copy, and the tabs,
// which stay in place as the body scrolls.
func (h *Help) header(c *ui.Context, theme nui.Theme, messages *pythonapi.Messages) {
	scope := h.Workbench.PythonHelpScope
	scopeLabel := messages.Global
	if scope == pythonapi.ScopeInline {
		scopeLabel = messages.Inline
	}
	ui.Column(c.Key("help-head")).FillWidth().MinWidth(0).Gap(12).Children(func() {
		ui.Row(c.Key("title-row")).FillWidth().AlignItems(ui.Start).Gap(8).Children(func() {
			ui.Column(c.Key("title")).Grow(1).Basis(0).MinWidth(0).Gap(2).Children(func() {
				ui.Text(c, messages.Title).FontSize(apptheme.TextSize(c, 14)).Bold().TextColor(theme.Highlighted).Role(ui.RoleHeading).Level(2)
				ui.Text(c, messages.Version).FontSize(apptheme.TextSize(c, 12)).TextColor(theme.Muted)
			})
			nui.Button(c.Key("close"), theme, "", nui.ButtonOptions{Variant: nui.Ghost, Color: nui.Neutral, Size: nui.SM, Square: true, Leading: lucide.X(), AccessibleLabel: messages.Close}).Shrink(0).OnClick(h.Workbench.ClosePythonHelp)
		})
		ui.Row(c.Key("scope")).FillWidth().AlignItems(ui.Center).Justify(ui.SpaceBetween).Gap(8).Wrap().Children(func() {
			nui.Badge(c.Key("badge"), theme, scopeLabel, nui.BadgeOptions{Color: nui.Neutral, Variant: nui.Soft, Size: nui.SM})
			nui.Button(c.Key("copy-prompt"), theme, messages.CopyContext, nui.ButtonOptions{Size: nui.SM, Leading: lucide.Copy()}).OnClick(func() {
				h.copy(pythonapi.SystemPrompt(scope, h.Locale()), messages.CopiedContext)
			})
		})
		nui.Tabs(c.Key("tabs"), theme, &h.tab, []nui.TabItem{{ID: "quickstart", Label: messages.Quickstart}, {ID: "reference", Label: messages.Reference}}, nui.TabsOptions{Label: messages.Title, Size: nui.SM, Content: nui.Set(false)})
	})
}

func (h *Help) quickstart(c *ui.Context, theme nui.Theme, scope string, messages *pythonapi.Messages) {
	ui.Column(c.Key("quickstart")).FillWidth().MinWidth(0).Gap(20).Children(func() {
		ui.Text(c.Key("hint"), messages.Start.AIHint).FontSize(apptheme.TextSize(c, 13)).LineHeight(1.5).TextColor(theme.Muted).FillWidth()
		edit, enable := messages.Start.Global.Edit, messages.Start.Global.Enable
		if scope == pythonapi.ScopeInline {
			edit, enable = messages.Start.Inline.Edit, messages.Start.Inline.Enable
		}
		ui.Column(c.Key("steps")).FillWidth().Gap(12).Children(func() {
			for index, step := range []pythonapi.Step{messages.Start.Runtime, edit, enable} {
				ui.Row(c.Key(index)).FillWidth().AlignItems(ui.Start).Gap(8).Children(func() {
					ui.Text(c, strconv.Itoa(index+1)).Width(16).Shrink(0).FontSize(apptheme.TextSize(c, 14)).LineHeight(20.0 / 14.0).Bold().TextColor(theme.Primary)
					ui.Column(c).Grow(1).Basis(0).MinWidth(0).Gap(2).Children(func() {
						ui.Text(c, step.Title).FontSize(apptheme.TextSize(c, 14)).LineHeight(20.0 / 14.0).Bold().TextColor(theme.Highlighted).FillWidth()
						ui.Text(c, step.Text).FontSize(apptheme.TextSize(c, 13)).LineHeight(1.5).TextColor(theme.Muted).FillWidth()
					})
				})
			}
		})
		ui.Column(c.Key("essentials")).FillWidth().Gap(8).Children(func() {
			ui.Text(c, messages.Start.EssentialsTitle).FontSize(apptheme.TextSize(c, 14)).Bold().TextColor(theme.Highlighted)
			helpBullets(c, theme, messages.Start.Essentials)
		})
		nui.Accordion(c.Key("details"), theme, &h.details, []nui.AccordionItem{
			{ID: "template", Label: messages.MinimalTemplate, Content: func(c *ui.Context, palette nui.Theme) {
				h.template(c, palette, messages)
			}},
			{ID: "runtime", Label: messages.RuntimeDetails, Content: func(c *ui.Context, palette nui.Theme) {
				helpBullets(c, palette, messages.RuntimeSummary)
			}},
		}, nui.AccordionOptions{Multiple: true, Collapsible: nui.Set(true)})
	})
}

func helpBullets(c *ui.Context, theme nui.Theme, items []string) {
	ui.Column(c).FillWidth().Gap(6).Children(func() {
		for index, text := range items {
			ui.Row(c.Key(index)).FillWidth().AlignItems(ui.Start).Gap(6).Children(func() {
				ui.Text(c, "•").Width(8).Shrink(0).FontSize(apptheme.TextSize(c, 13)).LineHeight(1.5).TextColor(theme.Muted)
				ui.Text(c, text).Grow(1).Basis(0).MinWidth(0).FontSize(apptheme.TextSize(c, 13)).LineHeight(1.5).TextColor(theme.Toned)
			})
		}
	})
}

func (h *Help) template(c *ui.Context, theme nui.Theme, messages *pythonapi.Messages) {
	ui.Column(c.Key("template")).FillWidth().MinWidth(0).Border(1, theme.BorderMuted).Radius(theme.Radius).Background(theme.Elevated).Clip().Children(func() {
		ui.Row(c.Key("toolbar")).FillWidth().Justify(ui.End).Padding(4, 8).BorderWidth(0, 0, 1, 0).BorderColor(theme.BorderMuted).Children(func() {
			nui.Button(c.Key("copy"), theme, messages.CopyTemplate, nui.ButtonOptions{Variant: nui.Ghost, Color: nui.Neutral, Size: nui.XS, Leading: lucide.Copy()}).OnClick(func() { h.copy(pythonapi.MinimalScript(), messages.CopiedTemplate) })
		})
		// Preserve Python indentation and blank lines without wrapping code.
		script := strings.TrimRight(pythonapi.MinimalScript(), "\n")
		size := apptheme.TextSize(c, 12)
		height := float32(strings.Count(script, "\n")+1)*size*1.5 + 24
		ui.ScrollHorizontal(c.Key("code-scroll")).FillWidth().Height(height).Children(func() {
			ui.Text(c.Key("script"), script).NoWrap().Shrink(0).Padding(12).FontSize(size).LineHeight(1.5).Font(theme.MonoFont).TextColor(theme.Text).Selectable()
		})
	})
}

func (h *Help) reference(c *ui.Context, theme nui.Theme, locale string, messages *pythonapi.Messages) {
	nui.Input(c.Key("search"), theme, &h.query, nui.InputOptions{Label: messages.Search, Placeholder: messages.Search, Leading: lucide.Search(), Size: nui.SM}).FillWidth()
	ui.Text(c.Key("provided"), messages.ProvidedHint).FontSize(apptheme.TextSize(c, 13)).LineHeight(1.5).TextColor(theme.Muted).FillWidth()
	groups := pythonapi.Groups(locale, h.query)
	if len(groups) == 0 {
		ui.Column(c.Key("none")).FillWidth().Padding(16).Center().Gap(4).Children(func() {
			ui.Text(c, messages.NoResults).FontSize(apptheme.TextSize(c, 14)).Bold().TextColor(theme.Text)
			ui.Text(c, messages.NoResultsHint).FontSize(apptheme.TextSize(c, 12)).TextColor(theme.Muted)
		})
		return
	}
	if h.query != h.seenQuery {
		h.seenQuery = h.query
		h.expanded = nil
		if strings.TrimSpace(h.query) != "" {
			for _, group := range groups {
				h.expanded = append(h.expanded, group.Name)
			}
		}
	}
	items := make([]nui.AccordionItem, len(groups))
	for index := range groups {
		group := groups[index]
		items[index] = nui.AccordionItem{ID: group.Name, Label: group.Name, Content: func(c *ui.Context, palette nui.Theme) { groupBody(c, palette, group, messages) }}
	}
	nui.Accordion(c.Key("groups"), theme, &h.expanded, items, nui.AccordionOptions{Multiple: true, Collapsible: nui.Set(true)})
}

func groupBody(c *ui.Context, theme nui.Theme, group pythonapi.Group, messages *pythonapi.Messages) {
	ui.Column(c).FillWidth().Gap(8).Children(func() {
		tag := messages.Provided
		switch {
		case group.Name == "Hooks":
			tag = messages.Hook
		case group.Importable:
			tag = messages.Importable
		}
		ui.Row(c.Key("tag")).FillWidth().Children(func() {
			nui.Badge(c, theme, tag, nui.BadgeOptions{Color: nui.Neutral, Variant: nui.Outline, Size: nui.SM})
		})
		if summary := messages.GroupSummaries[group.Name]; summary != "" {
			ui.Text(c.Key("summary"), summary).FontSize(apptheme.TextSize(c, 13)).LineHeight(1.5).TextColor(theme.Muted).FillWidth()
		}
		if group.Note != "" {
			ui.Text(c.Key("note"), messages.UsageNotes+": "+group.Note).FontSize(apptheme.TextSize(c, 13)).LineHeight(1.5).TextColor(theme.Toned).FillWidth()
		}
		for index, entry := range group.Entries {
			item := ui.Column(c.Key(index)).FillWidth().Gap(6).PaddingY(8)
			if index > 0 {
				item.BorderWidth(1, 0, 0, 0).BorderColor(theme.BorderMuted)
			}
			item.Children(func() {
				ui.Row(c).FillWidth().AlignItems(ui.Start).Gap(8).Children(func() {
					ui.Text(c, strings.TrimSuffix(entry.Signature, "  # read-only")).Grow(1).Basis(0).MinWidth(0).FontSize(apptheme.TextSize(c, 12)).LineHeight(1.5).Font(theme.MonoFont).TextColor(theme.Text).Selectable()
					label := messages.Method
					switch entry.Kind {
					case "property":
						label = messages.Readonly
						if entry.Writable {
							label = messages.Writable
						}
					case "constructor":
						label = messages.Constructor
					case "hook":
						label = messages.Hook
					}
					nui.Badge(c.Key("kind"), theme, label, nui.BadgeOptions{Color: nui.Neutral, Variant: nui.Soft, Size: nui.SM}).Shrink(0)
				})
				ui.Text(c, entry.Description).FontSize(apptheme.TextSize(c, 13)).LineHeight(1.5).TextColor(theme.Muted).FillWidth().Selectable()
				if len(entry.Parameters) > 0 {
					ui.Text(c, messages.Parameters+": "+strings.Join(entry.Parameters, ", ")).FontSize(apptheme.TextSize(c, 13)).LineHeight(1.5).TextColor(theme.Toned).FillWidth()
				}
				if entry.ReturnType != "" {
					ui.Text(c, messages.Returns+": "+entry.ReturnType).FontSize(apptheme.TextSize(c, 13)).LineHeight(1.5).TextColor(theme.Toned).FillWidth()
				}
			})
		}
	})
}
