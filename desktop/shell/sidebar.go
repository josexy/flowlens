package shell

import (
	"github.com/egoist/mygo/ui"
	settingservice "github.com/josexy/flowlens/backend/services/setting_service"
	"github.com/josexy/flowlens/desktop/shortcuts"
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
	column := ui.Column(c).Width(primarySidebarWidth).Shrink(0).FillHeight().Padding(10, 6, 8, 6).Background(palette.Elevated)
	column.BorderWidth(0, 1, 0, 0).BorderColor(palette.BorderAccented).Shadow(8, 0, 20, -18, ui.RGBA(15, 23, 42, 0.32))
	column.Children(func() {
		ui.Column(c).FillWidth().Grow(1).Basis(0).Gap(2).Children(func() {
			for _, item := range sidebarItems {
				active := a.Store.Workbench.ActiveSection == item.section
				a.sidebarButton(c, palette, string(item.section), item.label(), item.icon(), active, true, a.kbds(sectionCommands[item.section]), func() {
					a.Store.Workbench.ActivateSection(item.section)
				})
			}
		})
		ui.Column(c).FillWidth().Gap(2).Margin(10, 0, 0, 0).PaddingY(10).Children(func() {
			a.sidebarButton(c, palette, "theme", themeModeLabelText(a.currentThemeMode()), themeModeIcon(a.currentThemeMode()), false, false, nil, a.toggleThemeMode)
			a.sidebarButton(c, palette, "language", locales.LanguageSwitch(), lucide.Languages(), false, false, nil, a.toggleLanguage)
			a.sidebarButton(c, palette, "settings", locales.MenuSettings(), lucide.Settings(), false, false, a.kbds(shortcuts.AppOpenSettings), a.openSettings)
		})
	})
}

func themeModeLabelText(mode string) string { return locales.ThemeTooltip(themeModeLabel(mode)) }

// sidebarButton is a square button of the primary sidebar. A section button
// marks the active section with the accent and a bar along its left edge.
func (a *App) sidebarButton(c *ui.Context, palette nui.Theme, key, label string, icon *ui.SVG, active, section bool, kbds []string, run func()) {
	button := ui.ButtonBase(c.Key(key)).FillWidth().Height(primarySidebarWidth - 12).Radius(8).Center().Label(label).OnClick(run)
	hovered := button.Hovered() || button.Pressed()
	color := palette.Muted
	switch {
	case active:
		button.Background(palette.Primary.Alpha(0.12).Over(palette.Elevated))
		color = palette.Primary
	case hovered:
		color = palette.Text
	}
	button.Children(func() {
		if section && (active || hovered) {
			// The bar is shorter and fainter while it only previews the choice.
			inset, alpha := float32(9), float32(1)
			if !active {
				inset, alpha = 12, 0.38
			}
			ui.Box(c.Key("indicator")).Absolute().Left(3).Top(inset).Bottom(inset).Width(2).Radius(1).Background(palette.Primary.Alpha(alpha)).IgnoreInput(true)
		}
		ui.Icon(c, icon).Size(18, 18).Shrink(0).TextColor(color)
	})
	nui.Tooltip(c, palette, button, label, nui.TooltipOptions{Kbds: kbds, Placement: nui.Set(nui.RightCenter)})
}

// sectionCommands are the shortcuts that show each section.
var sectionCommands = map[store.WorkspaceSection]string{
	store.WorkspaceCapture:       shortcuts.WorkbenchCapture,
	store.WorkspaceCategory:      shortcuts.WorkbenchCategory,
	store.WorkspaceAPICollection: shortcuts.WorkbenchAPICollection,
	store.WorkspaceRewriteRules:  shortcuts.WorkbenchRewriteRules,
	store.WorkspacePythonPlugins: shortcuts.WorkbenchPythonPlugins,
	store.WorkspaceMemStats:      shortcuts.WorkbenchMemStats,
}

// kbds returns the keys bound to a command, for tooltips.
func (a *App) kbds(commandID string) []string {
	command, ok := shortcuts.CommandByID(commandID)
	if !ok {
		return nil
	}
	var config *settingservice.ShortcutConfig
	if a.Store.Settings.Current != nil {
		config = a.Store.Settings.Current.Shortcuts
	}
	resolved := shortcuts.ResolveCommand(command, config)
	if resolved.Binding == nil {
		return nil
	}
	return shortcuts.KbdKeys(resolved.Binding, a.MacOS)
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
