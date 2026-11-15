package editor

import (
	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/desktop/logic"
	apptheme "github.com/josexy/flowlens/desktop/theme"
	"github.com/josexy/flowlens/desktop/widgets"
	"github.com/josexy/flowlens/locales"
	nui "github.com/josexy/nuxt-ui-go/ui"
)

func settingsContent(c *ui.Context, theme nui.Theme, title string, controls func()) {
	widgets.PaneTitle(c, theme, title, nil)
	widgets.PaneContent(c, func() {
		ui.Scroll(c.Key("settings")).Fill().Children(func() {
			ui.Column(c).FillWidth().Gap(6).Children(controls)
		})
	})
}

func proxySettings(c *ui.Context, theme nui.Theme, mode, customProxy *string) {
	options := []nui.Option[string]{
		{Value: logic.ProxyModeNone, Label: locales.WorkspaceHttpRequestProxyModeNone()},
		{Value: logic.ProxyModeSystem, Label: locales.WorkspaceHttpRequestProxyModeSystem()},
		{Value: logic.ProxyModeMITM, Label: locales.WorkspaceHttpRequestProxyModeMitm()},
		{Value: logic.ProxyModeCustom, Label: locales.WorkspaceHttpRequestProxyModeCustom()},
	}
	selectRow(c, theme, "proxy", locales.WorkspaceHttpRequestProxyMode(), mode, options, nil)
	if *mode == logic.ProxyModeCustom {
		label := locales.WorkspaceHttpRequestCustomProxy()
		settingRow(c, theme, "custom", label, func() {
			nui.Input(c.Key("input"), theme, customProxy, nui.InputOptions{Label: label, Placeholder: locales.WorkspaceHttpRequestCustomProxyPlaceholder(), Size: nui.SM}).FillWidth()
		})
	}
}

func tlsProfileOptions() []nui.Option[string] {
	labels := map[string]func() string{
		"golang": locales.WorkspaceHttpRequestTlsFingerprintGolang, "chrome_auto": locales.WorkspaceHttpRequestTlsFingerprintChrome,
		"firefox_auto": locales.WorkspaceHttpRequestTlsFingerprintFirefox, "safari_auto": locales.WorkspaceHttpRequestTlsFingerprintSafari,
		"edge_auto": locales.WorkspaceHttpRequestTlsFingerprintEdge, "ios_auto": locales.WorkspaceHttpRequestTlsFingerprintIos,
		"android_11_okhttp": locales.WorkspaceHttpRequestTlsFingerprintAndroid, "randomized_alpn": locales.WorkspaceHttpRequestTlsFingerprintRandom,
	}
	options := make([]nui.Option[string], 0, len(logic.TLSClientHelloIDs))
	for _, id := range logic.TLSClientHelloIDs {
		label := id
		if name, ok := labels[id]; ok {
			label = name()
		}
		options = append(options, nui.Option[string]{Value: id, Label: label})
	}
	return options
}

func settingRow(c *ui.Context, theme nui.Theme, key, label string, control func()) {
	ui.Row(c.Key(key)).FillWidth().AlignItems(ui.Center).Gap(10).MinHeight(36).Children(func() {
		ui.Text(c, label).Width(170).Shrink(0).FontSize(apptheme.TextSize(c, 14)).TextColor(theme.Toned)
		ui.Box(c).Grow(1).Basis(0).MinWidth(0).Children(control)
	})
}

func selectRow(c *ui.Context, theme nui.Theme, key, label string, value *string, options []nui.Option[string], onChange func()) {
	settingRow(c, theme, key, label, func() {
		nui.Select(c.Key("select"), theme, value, options, nui.SelectOptions{Label: label, OnChange: onChange})
	})
}

// numberField edits an integer setting.
type numberField struct {
	value nui.Optional[float64]
	state nui.NumberState
	seen  int64
	ready bool
}

func (f *numberField) build(c *ui.Context, theme nui.Theme, key, label, placeholder string, target *int64, minimum float64, step float64) {
	if !f.ready || *target != f.seen {
		f.value, f.seen, f.ready = nui.Set(float64(*target)), *target, true
	}
	settingRow(c, theme, key, label, func() {
		nui.InputNumber(c.Key("number"), theme, &f.value, &f.state, nui.InputNumberOptions{
			Label: label, Placeholder: placeholder, Orientation: nui.Vertical, Min: nui.Set(minimum), Step: step,
		})
	})
	if f.value.Set {
		*target = int64(f.value.Value)
		f.seen = *target
	}
}
