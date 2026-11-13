package widgets

import (
	"slices"

	"github.com/egoist/mygo/ui"
	apptheme "github.com/josexy/flowlens/desktop/theme"
	nui "github.com/josexy/nuxt-ui-go"
)

// PillTab is one tab of PillTabs.
type PillTab struct {
	ID    string
	Label string
}

// pillTabsMemory is the index the tabs chose and the value it was synced with.
type pillTabsMemory struct {
	index int
	value string
}

// PillTabs builds a strip of extra-small pill tabs, each as wide as its label,
// for a strip that scrolls sideways instead of squeezing its tabs. It looks
// like nui.Tabs' pill variant, which shares the strip's width out evenly.
func PillTabs(c *ui.Context, theme nui.Theme, value *string, items []PillTab, label string) ui.Element {
	position := func(id string) int {
		return slices.IndexFunc(items, func(item PillTab) bool { return item.ID == id })
	}
	root := ui.Row(c).Shrink(0)
	memory := ui.Local(root, "pill-tabs", func() pillTabsMemory { return pillTabsMemory{index: -1} })
	switch current := position(*value); {
	case memory.value != *value || memory.index < 0 || memory.index >= len(items):
		// The value changed outside the tabs.
		memory.index = current
	case memory.index != current:
		// The keys or a click chose another tab.
		*value = items[memory.index].ID
	}
	if memory.index < 0 && len(items) > 0 {
		memory.index = 0
		*value = items[0].ID
	}
	memory.value = *value
	root.Children(func() {
		parts := ui.TabsBase(c, &memory.index, len(items))
		list := parts.List.Label(label).AlignItems(ui.Center).Padding(4).Radius(8).Background(theme.Elevated)
		list.Children(func() {
			for i, item := range items {
				active := i == memory.index
				tab := parts.Tab(i).Label(item.Label).PaddingX(8).PaddingY(4).Radius(6).AlignItems(ui.Center).FocusRing(true)
				foreground := theme.Muted
				switch {
				case active:
					tab.Background(theme.Primary)
					foreground = theme.Inverted
				case tab.Hovered():
					foreground = theme.Text
				}
				tab.Children(func() {
					ui.Text(c, item.Label).SingleLine().FontSize(apptheme.TextSize(c, 12)).LineHeight(16.0 / 12.0).FontWeight(500).TextColor(foreground)
				})
			}
		})
	})
	return root
}
