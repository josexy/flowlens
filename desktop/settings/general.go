package settings

import (
	"slices"

	"github.com/egoist/mygo/ui"
	settingservice "github.com/josexy/flowlens/backend/services/setting_service"
	apptheme "github.com/josexy/flowlens/desktop/theme"
	"github.com/josexy/flowlens/locales"
	"github.com/josexy/nuxt-ui-go/icons/lucide"
	nui "github.com/josexy/nuxt-ui-go/ui"
)

const defaultFontID = "__default_font__"

type generalState struct {
	fonts                  []settingservice.FontOption
	fontsLoaded, fontsBusy bool
	appFont, codeFont      nui.SelectMenuState
	appValue, codeValue    []string
	appSize, codeSize      integerField
}

// integerField edits an integer with bounds.
type integerField struct {
	value nui.Optional[float64]
	state nui.NumberState
	seen  int
	ready bool
}

func (f *integerField) build(c *ui.Context, theme nui.Theme, key, label string, target *int, minimum, maximum, step float64, disabled bool) {
	if !f.ready || *target != f.seen {
		f.value, f.seen, f.ready = nui.Set(float64(*target)), *target, true
	}
	ui.Box(c.Key(key)).Width(176).Children(func() {
		nui.InputNumber(c.Key("input"), theme, &f.value, &f.state, nui.InputNumberOptions{Label: label, Disabled: disabled, Orientation: nui.Vertical, Min: nui.Set(minimum), Max: nui.Set(maximum), Step: step})
	})
	if f.value.Set {
		*target = int(f.value.Value)
		f.seen = *target
	}
}

func (v *View) loadFonts() {
	state := &v.general
	if state.fontsLoaded || state.fontsBusy || v.Host.ListFonts == nil {
		return
	}
	state.fontsBusy = true
	run(v, "fonts", func() ([]settingservice.FontOption, error) { return v.Host.ListFonts(), nil }, func(fonts []settingservice.FontOption, _ error) {
		state.fontsBusy, state.fontsLoaded, state.fonts = false, true, fonts
	})
}

func (v *View) fontItems(common *settingservice.CommonConfig) []nui.ListboxItem {
	items := []nui.ListboxItem{{ID: defaultFontID, Label: locales.SettingsFontDefault()}}
	add := func(id, label string) {
		if id != "" && !slices.ContainsFunc(items, func(item nui.ListboxItem) bool { return item.ID == id }) {
			if label == "" {
				label = id
			}
			items = append(items, nui.ListboxItem{ID: id, Label: label})
		}
	}
	add(common.AppFontFamily, "")
	add(common.CodeFontFamily, "")
	for _, font := range v.general.fonts {
		add(font.Value, font.Label)
	}
	return items
}

func (v *View) fontRow(c *ui.Context, palette nui.Theme, key, label string, family *string, size int, preview string, state *nui.SelectMenuState, selected *[]string, items []nui.ListboxItem, fallback string) {
	row(c, palette, key, label, "", func() {
		current := defaultFontID
		if *family != "" {
			current = *family
		}
		if len(*selected) != 1 || (*selected)[0] != current {
			*selected = []string{current}
		}
		ui.Box(c.Key("menu")).Width(320).Children(func() {
			nui.SelectMenu(c.Key("select"), palette, selected, state, items, nui.SelectMenuOptions{
				ListboxOptions: nui.ListboxOptions{Label: label, Loading: v.general.fontsBusy, OnChange: func(values []string) {
					if len(values) == 1 {
						*family = values[0]
						if *family == defaultFontID {
							*family = ""
						}
					}
				}}, SearchPlaceholder: locales.SettingsFontSearchPlaceholder(),
				OnOpenChange: func(open bool) {
					if open {
						v.loadFonts()
					}
				},
			})
		})
		font := *family
		if font == "" {
			font = fallback
		}
		ui.Text(c.Key("preview"), preview).Font(font).FontSize(float32(size)).TextColor(palette.Muted).FillWidth()
	})
}

func paletteOptions(names []string) []nui.ListboxItem {
	items := make([]nui.ListboxItem, len(names))
	for index, name := range names {
		items[index] = nui.ListboxItem{ID: name, Label: message("settingsThemeColorsPalette" + camel(name))}
	}
	return items
}

func (v *View) sizeRow(c *ui.Context, palette nui.Theme, key, label string, field *integerField, value *int, minimum, maximum, fallback int) {
	row(c, palette, key, label, "", func() {
		ui.Row(c.Key("line")).Gap(6).AlignItems(ui.Center).Children(func() {
			field.build(c, palette, "number", label, value, float64(minimum), float64(maximum), 1, v.committing)
			ui.Text(c, locales.SettingsUnitPixels()).FontSize(apptheme.TextSize(c, 14)).TextColor(palette.Muted)
			blocked := v.committing || *value == fallback
			reset := nui.Button(c.Key("reset"), palette, "", nui.ButtonOptions{Variant: nui.Ghost, Color: nui.Neutral, Size: nui.SM, Square: true, Leading: lucide.RotateCcw(), Disabled: blocked, AccessibleLabel: locales.SettingsFontSizeReset() + " " + label})
			if !blocked {
				reset.OnClick(func() { *value = fallback })
			}
		})
	})
}

func (v *View) generalPage(c *ui.Context, palette nui.Theme, draft *settingservice.Settings) {
	common, window := draft.CommonConfig, draft.WindowConfig
	state := &v.general
	v.section(c, palette, "appearance", locales.SettingsSectionAppearance(), false, nil, func() {
		colorRow := func(key, label string, names []string, target *string) {
			const swatchSize, swatchGap = 12, 8
			row(c, palette, key, label, "", func() {
				ui.Box(c.Key("menu")).Width(320).Children(func() {
					options := make([]nui.Option[string], len(names))
					for index, item := range paletteOptions(names) {
						options[index] = option(item.ID, item.Label)
						colors, _ := nui.PaletteByName(item.ID)
						options[index].Build = func(c *ui.Context, _ nui.Theme) {
							ui.Box(c).Size(swatchSize, swatchSize).Radius(swatchSize / 2).Background(colors.Shade(500)).Shrink(0).AccessHidden(true)
							ui.Text(c, item.Label).SingleLine().Grow(1).MinWidth(0)
						}
					}
					select_ := nui.Select(c.Key("select"), palette, target, options, nui.SelectOptions{
						Label: label, Disabled: v.committing,
						Style: func(slot nui.Slot, element ui.Element) {
							switch slot {
							case nui.Label:
								element.PaddingStart(swatchSize + swatchGap)
							case nui.Item:
								element.AlignItems(ui.Center).Gap(swatchGap)
							}
						},
					})
					select_.Children(func() {
						colors, _ := nui.PaletteByName(*target)
						ui.Box(c.Key("swatch")).Absolute().Left(10).Top(0).Bottom(0).Width(swatchSize).Center().IgnoreInput(true).AccessHidden(true).Children(func() {
							ui.Box(c).Size(swatchSize, swatchSize).Radius(swatchSize / 2).Background(colors.Shade(500))
						})
					})
				})
			})
		}
		colorRow("primary", locales.SettingsThemeColorsPrimary(), nui.PrimaryPaletteNames(), &common.ThemePrimaryColor)
		colorRow("neutral", locales.SettingsThemeColorsNeutral(), nui.NeutralPaletteNames(), &common.ThemeNeutralColor)
		row(c, palette, "animations", locales.SettingsAnimationsLabel(), locales.SettingsAnimationsHint(), func() {
			enabled := !common.DisableAnimations
			nui.Switch(c.Key("switch"), palette, &enabled, nui.SwitchOptions{Disabled: v.committing}).
				Label(locales.SettingsAnimationsLabel()).OnClick(func() { common.DisableAnimations = !enabled })
		})
		items := v.fontItems(common)
		v.fontRow(c, palette, "app-font", locales.SettingsAppFontLabel(), &common.AppFontFamily, common.AppFontSize, locales.SettingsFontPreviewText(), &state.appFont, &state.appValue, items, palette.Font)
		v.sizeRow(c, palette, "app-size", locales.SettingsAppFontSizeLabel(), &state.appSize, &common.AppFontSize, apptheme.MinAppFontSize, apptheme.MaxAppFontSize, apptheme.DefaultAppFontSize)
		v.fontRow(c, palette, "code-font", locales.SettingsCodeFontLabel(), &common.CodeFontFamily, common.CodeFontSize, locales.SettingsCodeFontPreviewText(), &state.codeFont, &state.codeValue, items, palette.MonoFont)
		v.sizeRow(c, palette, "code-size", locales.SettingsCodeFontSizeLabel(), &state.codeSize, &common.CodeFontSize, apptheme.MinCodeFontSize, apptheme.MaxCodeFontSize, apptheme.DefaultCodeFontSize)
	})
	v.section(c, palette, "behavior", locales.SettingsSectionBehavior(), false, nil, func() {
		row(c, palette, "frame", locales.SettingsWindowFrameModeLabel(), "", func() {
			value := string(window.FrameMode)
			ui.Box(c.Key("menu")).Width(320).Children(func() {
				nui.Select(c.Key("select"), palette, &value, []nui.Option[string]{
					option(string(settingservice.WindowFrameModeCustom), locales.SettingsWindowFrameModeCustom()),
					option(string(settingservice.WindowFrameModeSystem), locales.SettingsWindowFrameModeSystem()),
				}, nui.SelectOptions{Label: locales.SettingsWindowFrameModeLabel()})
			})
			window.FrameMode = settingservice.WindowFrameMode(value)
		})
		row(c, palette, "close", locales.SettingsMainWindowCloseBehaviorLabel(), "", func() {
			value := string(window.MainWindowCloseBehavior)
			ui.Box(c.Key("menu")).Width(320).Children(func() {
				nui.Select(c.Key("select"), palette, &value, []nui.Option[string]{
					option(string(settingservice.MainWindowCloseBehaviorHideToTray), locales.SettingsMainWindowCloseBehaviorHideToTray()),
					option(string(settingservice.MainWindowCloseBehaviorQuit), locales.SettingsMainWindowCloseBehaviorQuit()),
				}, nui.SelectOptions{Label: locales.SettingsMainWindowCloseBehaviorLabel()})
			})
			window.MainWindowCloseBehavior = settingservice.MainWindowCloseBehavior(value)
		})
		if window.FrameMode != v.Data.Settings.ActiveFrameMode {
			nui.Alert(c.Key("restart"), palette, nui.AlertOptions{Title: locales.SettingsWindowFrameModeRestartNotice(), Color: nui.Warning, Variant: nui.Soft})
		}
	})
}
