package plugins

import (
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/desktop/pythonapi"
	"github.com/josexy/flowlens/desktop/store"
	"github.com/josexy/flowlens/desktop/widgets"
	nui "github.com/josexy/nuxt-ui-go"
	"github.com/josexy/nuxt-ui-go/icons/lucide"
)

const (
	helpMinWidth float32 = 320
	helpMaxWidth float32 = 720
)

// Help is the Python API reference.
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
	width     float32
	details   []string
	clipboard *string
	open      bool
	opened    string
}

// NewHelp returns the panel of a window.
func NewHelp(workbench *store.WorkbenchStore, notifier *widgets.Notifier, locale func() string) *Help {
	return &Help{Workbench: workbench, Notifier: notifier, Locale: locale, tab: "quickstart", width: 420}
}

// Visible reports whether the panel is open.
func (h *Help) Visible() bool { return h.Workbench.PythonHelpScope != "" }

func (h *Help) prepare(c *ui.Context) {
	if h.clipboard != nil {
		c.WriteClipboard(*h.clipboard)
		h.clipboard = nil
	}
	scope := h.Workbench.PythonHelpScope
	context := ""
	if h.Context != nil {
		context = h.Context()
	}
	if scope != "" && context != h.opened && h.seenScope != "" {
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

// Forget notes that the panel was closed.
func (h *Help) Forget() { h.seenScope = "" }

func (h *Help) copy(text, success string) {
	h.clipboard = &text
	h.Notifier.Success(success)
}

// BuildDocked shows the panel beside the content.
func (h *Help) BuildDocked(c *ui.Context, theme nui.Theme) {
	h.prepare(c)
	ui.Row(c.Key("python-help")).Width(h.width).Shrink(0).FillHeight().Children(func() {
		handle := ui.Box(c.Key("resize")).Width(5).FillHeight().Shrink(0).Cursor(ui.CursorResizeEW).BorderWidth(0, 0, 0, 1).BorderColor(theme.Border)
		if dx, _, moved := handle.Dragged(); moved {
			h.width = min(helpMaxWidth, max(helpMinWidth, h.width-dx))
		}
		ui.Column(c.Key("panel")).Grow(1).Basis(0).MinWidth(0).FillHeight().Background(theme.Background).Children(func() { h.content(c, theme) })
	})
}

// BuildOverlay shows the panel above narrow content.
func (h *Help) BuildOverlay(c *ui.Context, theme nui.Theme) {
	h.prepare(c)
	h.open = h.Visible()
	nui.Slideover(c.Key("python-help-overlay"), theme, &h.open, nui.SlideoverOptions{Side: nui.SideRight, Extent: nui.Set[float32](h.width)}, func(palette nui.Theme) {
		ui.Column(c).Fill().MinHeight(0).Children(func() { h.content(c, palette) })
	})
	if !h.open {
		h.Workbench.ClosePythonHelp()
	}
}

func (h *Help) content(c *ui.Context, theme nui.Theme) {
	locale := h.Locale()
	messages := pythonapi.MessagesFor(locale)
	scope := h.Workbench.PythonHelpScope
	scopeLabel := messages.Global
	if scope == pythonapi.ScopeInline {
		scopeLabel = messages.Inline
	}
	ui.Column(c.Key("help-head")).FillWidth().Shrink(0).Gap(8).Padding(10).BorderWidth(0, 0, 1, 0).BorderColor(theme.Border).Children(func() {
		ui.Row(c.Key("title")).FillWidth().AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
			ui.Column(c).Gap(2).Children(func() {
				ui.Text(c, messages.Title).FontSize(14).Bold().TextColor(theme.Highlighted)
				ui.Text(c, messages.Version).FontSize(12).TextColor(theme.Muted)
			})
			nui.Button(c.Key("close"), theme, "", nui.ButtonOptions{Variant: nui.Ghost, Color: nui.Neutral, Size: nui.SM, Square: true, Leading: lucide.X(), AccessibleLabel: messages.Close}).OnClick(h.Workbench.ClosePythonHelp)
		})
		ui.Row(c.Key("scope")).FillWidth().AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
			nui.Badge(c.Key("badge"), theme, scopeLabel, nui.BadgeOptions{Color: nui.Neutral, Variant: nui.Soft, Size: nui.SM})
			nui.Button(c.Key("copy-prompt"), theme, messages.CopyContext, nui.ButtonOptions{Size: nui.SM, Leading: lucide.Copy()}).OnClick(func() {
				h.copy(pythonapi.SystemPrompt(scope, locale), messages.CopiedContext)
			})
		})
		nui.Tabs(c.Key("tabs"), theme, &h.tab, []nui.TabItem{{ID: "quickstart", Label: messages.Quickstart}, {ID: "reference", Label: messages.Reference}}, nui.TabsOptions{Label: messages.Title, Size: nui.SM, Content: nui.Set(false)})
	})
	ui.Scroll(c.Key(h.tab)).FillWidth().Grow(1).Basis(0).MinHeight(0).Children(func() {
		ui.Column(c).FillWidth().Gap(12).Padding(12).Children(func() {
			if h.tab == "reference" {
				h.reference(c, theme, locale, messages)
				return
			}
			h.quickstart(c, theme, scope, messages)
		})
	})
}

func (h *Help) quickstart(c *ui.Context, theme nui.Theme, scope string, messages *pythonapi.Messages) {
	ui.Text(c.Key("hint"), messages.Start.AIHint).FontSize(13).TextColor(theme.Muted).FillWidth()
	edit, enable := messages.Start.Global.Edit, messages.Start.Global.Enable
	if scope == pythonapi.ScopeInline {
		edit, enable = messages.Start.Inline.Edit, messages.Start.Inline.Enable
	}
	for index, step := range []pythonapi.Step{messages.Start.Runtime, edit, enable} {
		ui.Row(c.Key(index)).FillWidth().Gap(8).Children(func() {
			ui.Text(c, strconv.Itoa(index+1)).Shrink(0).FontSize(13).Bold().TextColor(theme.Primary)
			ui.Column(c).Grow(1).Basis(0).MinWidth(0).Gap(2).Children(func() {
				ui.Text(c, step.Title).FontSize(13).Bold().TextColor(theme.Highlighted)
				ui.Text(c, step.Text).FontSize(12).TextColor(theme.Muted).FillWidth()
			})
		})
	}
	ui.Column(c.Key("essentials")).FillWidth().Gap(4).Children(func() {
		ui.Text(c, messages.Start.EssentialsTitle).FontSize(13).Bold().TextColor(theme.Highlighted)
		for index, text := range messages.Start.Essentials {
			ui.Text(c.Key(index), "• "+text).FontSize(12).TextColor(theme.Toned).FillWidth()
		}
	})
	nui.Accordion(c.Key("details"), theme, &h.details, []nui.AccordionItem{
		{ID: "template", Label: messages.MinimalTemplate, Content: func(c *ui.Context, palette nui.Theme) {
			ui.Column(c).FillWidth().Gap(6).Children(func() {
				nui.Button(c.Key("copy"), palette, messages.CopyTemplate, nui.ButtonOptions{Variant: nui.Ghost, Color: nui.Neutral, Size: nui.XS, Leading: lucide.Copy()}).OnClick(func() { h.copy(pythonapi.MinimalScript(), messages.CopiedTemplate) })
				ui.Text(c.Key("script"), pythonapi.MinimalScript()).FontSize(12).Font(palette.MonoFont).TextColor(palette.Text).Selectable().FillWidth()
			})
		}},
		{ID: "runtime", Label: messages.RuntimeDetails, Content: func(c *ui.Context, palette nui.Theme) {
			ui.Column(c).FillWidth().Gap(4).Children(func() {
				for index, text := range messages.RuntimeSummary {
					ui.Text(c.Key(index), "• "+text).FontSize(12).TextColor(palette.Toned).FillWidth()
				}
			})
		}},
	}, nui.AccordionOptions{Multiple: true, Collapsible: nui.Set(true)})
}

func (h *Help) reference(c *ui.Context, theme nui.Theme, locale string, messages *pythonapi.Messages) {
	nui.Input(c.Key("search"), theme, &h.query, nui.InputOptions{Label: messages.Search, Placeholder: messages.Search, Leading: lucide.Search(), Size: nui.SM}).FillWidth()
	ui.Text(c.Key("provided"), messages.ProvidedHint).FontSize(12).TextColor(theme.Muted).FillWidth()
	groups := pythonapi.Groups(locale, h.query)
	if len(groups) == 0 {
		ui.Column(c.Key("none")).FillWidth().Padding(16).Center().Gap(4).Children(func() {
			ui.Text(c, messages.NoResults).FontSize(13).Bold().TextColor(theme.Text)
			ui.Text(c, messages.NoResultsHint).FontSize(12).TextColor(theme.Muted)
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
		nui.Badge(c.Key("tag"), theme, tag, nui.BadgeOptions{Color: nui.Neutral, Variant: nui.Outline, Size: nui.SM})
		if summary := messages.GroupSummaries[group.Name]; summary != "" {
			ui.Text(c.Key("summary"), summary).FontSize(12).TextColor(theme.Muted).FillWidth()
		}
		if group.Note != "" {
			ui.Text(c.Key("note"), messages.UsageNotes+": "+group.Note).FontSize(12).TextColor(theme.Toned).FillWidth()
		}
		for index, entry := range group.Entries {
			ui.Column(c.Key(index)).FillWidth().Gap(2).Children(func() {
				ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(6).Children(func() {
					ui.Text(c, strings.TrimSuffix(entry.Signature, "  # read-only")).Grow(1).Basis(0).MinWidth(0).FontSize(12).Font(theme.MonoFont).TextColor(theme.Text).Selectable()
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
					nui.Badge(c.Key("kind"), theme, label, nui.BadgeOptions{Color: nui.Neutral, Variant: nui.Soft, Size: nui.SM})
				})
				ui.Text(c, entry.Description).FontSize(12).TextColor(theme.Muted).FillWidth().Selectable()
				if len(entry.Parameters) > 0 {
					ui.Text(c, messages.Parameters+": "+strings.Join(entry.Parameters, ", ")).FontSize(12).TextColor(theme.Toned).FillWidth()
				}
				if entry.ReturnType != "" {
					ui.Text(c, messages.Returns+": "+entry.ReturnType).FontSize(12).TextColor(theme.Toned).FillWidth()
				}
			})
		}
	})
}
