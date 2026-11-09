package shell

import (
	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/desktop/store"
	"github.com/josexy/flowlens/desktop/theme"
	"github.com/josexy/flowlens/locales"
	nui "github.com/josexy/nuxt-ui-go"
	"github.com/josexy/nuxt-ui-go/icons/lucide"
)

const primarySidebarWidth = 52

type sidebarItem struct {
	section store.WorkspaceSection
	label   func() string
	icon    func() *ui.SVG
}

var sidebarItems = []sidebarItem{
	{store.WorkspaceCapture, locales.MenuCapture, lucide.Radio},
	{store.WorkspaceCategory, locales.MenuCategory, lucide.FolderOpen},
	{store.WorkspaceAPICollection, locales.MenuApiCollection, lucide.GitBranch},
	{store.WorkspaceRewriteRules, locales.MenuRewriteRules, lucide.Repeat2},
	{store.WorkspacePythonPlugins, locales.MenuPythonPlugins, lucide.FileCode},
	{store.WorkspaceMemStats, locales.MenuMemstats, lucide.Activity},
}

func nextThemeMode(mode string) string {
	switch mode {
	case theme.ModeAuto:
		return theme.ModeLight
	case theme.ModeLight:
		return theme.ModeDark
	default:
		return theme.ModeAuto
	}
}

func themeModeLabel(mode string) string {
	switch mode {
	case theme.ModeLight:
		return locales.ThemeLight()
	case theme.ModeDark:
		return locales.ThemeDark()
	default:
		return locales.ThemeAuto()
	}
}

func themeModeIcon(mode string) *ui.SVG {
	switch mode {
	case theme.ModeLight:
		return lucide.Sun()
	case theme.ModeDark:
		return lucide.Moon()
	default:
		return lucide.MonitorSmartphone()
	}
}

func (a *App) primarySidebar(c *ui.Context, palette nui.Theme) {
	column := ui.Column(c).Width(primarySidebarWidth).FillHeight().AlignItems(ui.Center).Gap(4).PaddingY(8)
	column.BorderWidth(0, 1, 0, 0).BorderColor(palette.Border)
	column.Children(func() {
		for _, item := range sidebarItems {
			active := a.Store.Workbench.ActiveSection == item.section
			variant, color := nui.Ghost, nui.Neutral
			if active {
				variant, color = nui.Soft, nui.Primary
			}
			button := nui.Button(c.Key(item.section), palette, "", nui.ButtonOptions{
				Variant: variant, Color: color, Square: true, Leading: item.icon(), AccessibleLabel: item.label(), Active: active,
			}).OnClick(func() { a.Store.Workbench.ActivateSection(item.section) })
			nui.Tooltip(c, palette, button, item.label(), nui.TooltipOptions{})
		}
		ui.Box(c).Grow(1)
		a.utilityButton(c, palette, "theme", themeModeLabelText(a.currentThemeMode()), themeModeIcon(a.currentThemeMode()), a.toggleThemeMode)
		a.utilityButton(c, palette, "language", locales.LanguageSwitch(), lucide.Languages(), a.toggleLanguage)
		a.utilityButton(c, palette, "settings", locales.MenuSettings(), lucide.Settings(), a.openSettings)
	})
}

func themeModeLabelText(mode string) string { return locales.ThemeTooltip(themeModeLabel(mode)) }

func (a *App) utilityButton(c *ui.Context, palette nui.Theme, key, label string, icon *ui.SVG, run func()) {
	button := nui.Button(c.Key(key), palette, "", nui.ButtonOptions{
		Variant: nui.Ghost, Color: nui.Neutral, Square: true, Leading: icon, AccessibleLabel: label,
	}).OnClick(run)
	nui.Tooltip(c, palette, button, label, nui.TooltipOptions{})
}

func (a *App) currentThemeMode() string { return a.Store.Settings.Theme().Mode }

func (a *App) toggleThemeMode() {
	if err := a.Store.Settings.SetThemeMode(nextThemeMode(a.currentThemeMode())); err != nil {
		a.Notifier.Error(err.Error())
	}
}

func (a *App) toggleLanguage() {
	next := "zh"
	if a.Store.Settings.Language() == "zh" {
		next = "en"
	}
	if err := a.Store.Settings.SetLanguage(next); err != nil {
		a.Notifier.Error(err.Error())
	}
}
