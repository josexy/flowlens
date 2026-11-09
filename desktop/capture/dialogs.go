package capture

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	proxyservice "github.com/josexy/flowlens/backend/services/proxy_service"
	settingservice "github.com/josexy/flowlens/backend/services/setting_service"
	"github.com/josexy/flowlens/desktop/runtime"
	"github.com/josexy/flowlens/desktop/store"
	"github.com/josexy/flowlens/desktop/widgets"
	"github.com/josexy/flowlens/locales"
	nui "github.com/josexy/nuxt-ui-go"
	"github.com/josexy/nuxt-ui-go/icons/lucide"
)

const (
	maxResendCount  = 1000
	maxAliasLength  = 120
	jobResendPrefix = "capture:resend:"
)

type hostFilterRow struct {
	id    int
	value string
}

type hostFilterDialog struct {
	open             bool
	include, exclude []hostFilterRow
	nextID           int
}

func (d *hostFilterDialog) rows(values []string) []hostFilterRow {
	rows := make([]hostFilterRow, len(values))
	for index, value := range values {
		d.nextID++
		rows[index] = hostFilterRow{id: d.nextID, value: value}
	}
	return rows
}

func (d *hostFilterDialog) show(config *settingservice.ProxyConfig) {
	d.include, d.exclude = d.rows(config.IncludeHosts), d.rows(config.ExcludeHosts)
	d.open = true
}

func (d *hostFilterDialog) add(rows *[]hostFilterRow) {
	d.nextID++
	*rows = append(*rows, hostFilterRow{id: d.nextID})
}

func normalizeHostRows(rows []hostFilterRow) []string {
	values := make([]string, 0, len(rows))
	for _, row := range rows {
		if value := strings.TrimSpace(row.value); value != "" {
			values = append(values, value)
		}
	}
	return values
}

func (d *hostFilterDialog) section(c *ui.Context, theme nui.Theme, key, title string, rows *[]hostFilterRow, saving bool) {
	ui.Column(c.Key(key)).FillWidth().Gap(8).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
			ui.Text(c, title).Bold().FontSize(14).TextColor(theme.Highlighted)
			nui.Badge(c, theme, strconv.Itoa(len(*rows)), nui.BadgeOptions{Color: nui.Neutral, Variant: nui.Soft, Size: nui.XS})
			ui.Box(c).Grow(1)
			add := nui.Button(c.Key(key+"-add"), theme, locales.SettingsAddDomainFilter(), nui.ButtonOptions{Color: nui.Neutral, Variant: nui.Outline, Size: nui.SM, Leading: lucide.Plus(), Disabled: saving})
			if !saving {
				add.OnClick(func() { d.add(rows) })
			}
		})
		if len(*rows) == 0 {
			return
		}
		ui.Column(c).FillWidth().Gap(8).Padding(10).MaxHeight(220).Children(func() {
			for index := range *rows {
				row := &(*rows)[index]
				id := row.id
				ui.Row(c.Key(id)).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Box(c).Grow(1).Basis(0).MinWidth(0).Children(func() {
						nui.Input(c, theme, &row.value, nui.InputOptions{
							Label: title, Placeholder: locales.SettingsHostFilterPlaceholder(), Disabled: saving,
							OnSubmit: func(string) { d.add(rows) },
						})
					})
					remove := nui.Button(c.Key("remove"), theme, "", nui.ButtonOptions{Variant: nui.Ghost, Color: nui.Neutral, Square: true, Leading: lucide.Trash(), AccessibleLabel: locales.SettingsRemoveDomainFilter(), Disabled: saving})
					if !saving {
						remove.OnClick(func() { removeHostRow(rows, id) })
					}
				})
			}
		})
	})
}

func removeHostRow(rows *[]hostFilterRow, id int) {
	next := make([]hostFilterRow, 0, len(*rows))
	for _, row := range *rows {
		if row.id != id {
			next = append(next, row)
		}
	}
	*rows = next
}

func (d *hostFilterDialog) build(c *ui.Context, theme nui.Theme, w *Workspace, _ *settingservice.ProxyConfig) {
	if !d.open {
		return
	}
	w.Dispatcher.NoteModal()
	saving := w.toolbar.applying
	nui.ModalWithOptions(c.Key("host-filter-modal"), theme, &d.open, nui.ModalOptions{
		Title: locales.ToolbarHostFilterTitle(), Dismissible: nui.Set(!saving), CloseButton: nui.Set(!saving),
		Footer: func(c *ui.Context, palette nui.Theme) {
			ui.Row(c).FillWidth().Justify(ui.End).Gap(8).Children(func() {
				cancel := nui.Button(c.Key("host-cancel"), palette, locales.ToolbarCancel(), nui.ButtonOptions{Color: nui.Neutral, Variant: nui.Outline, Disabled: saving})
				if !saving {
					cancel.OnClick(func() { d.open = false })
				}
				save := nui.Button(c.Key("host-save"), palette, locales.ToolbarSave(), nui.ButtonOptions{Loading: saving, Disabled: saving})
				if !saving {
					save.OnClick(func() {
						include, exclude := normalizeHostRows(d.include), normalizeHostRows(d.exclude)
						w.updateProxy(func(target *settingservice.ProxyConfig) {
							target.IncludeHosts, target.ExcludeHosts = include, exclude
						}, func(error) { d.open = false })
					})
				}
			})
		},
	}, func(palette nui.Theme) {
		ui.Column(c).FillWidth().Gap(18).Children(func() {
			d.section(c, palette, "include", locales.SettingsIncludeHosts(), &d.include, saving)
			d.section(c, palette, "exclude", locales.SettingsExcludeHosts(), &d.exclude, saving)
		})
	})
}

type aliasDialog struct {
	open   bool
	saving bool
	draft  string
}

// ShowAliasDialog asks for a name for the current session.
func (w *Workspace) ShowAliasDialog() {
	w.alias.open, w.alias.draft = true, w.Data.Proxy.Status.CaptureAlias
}

func (d *aliasDialog) save(w *Workspace) {
	if d.saving {
		return
	}
	alias := strings.TrimSpace(d.draft)
	d.saving = true
	w.Data.Proxy.UpdateHistoryAlias(alias, func(err error) {
		d.saving = false
		if err != nil {
			w.Notifier.Error(locales.CaptureAliasSaveFailed(err.Error()))
			return
		}
		d.open = false
		if alias == "" {
			w.Notifier.Success(locales.CaptureAliasCleared())
			return
		}
		w.Notifier.Success(locales.CaptureAliasSaved())
	})
}

func (d *aliasDialog) build(c *ui.Context, theme nui.Theme, w *Workspace) {
	if !d.open {
		return
	}
	w.Dispatcher.NoteModal()
	saving := d.saving
	nui.ModalWithOptions(c.Key("alias-modal"), theme, &d.open, nui.ModalOptions{
		Title: locales.CaptureSetAlias(), Dismissible: nui.Set(!saving), CloseButton: nui.Set(!saving),
		Footer: func(c *ui.Context, palette nui.Theme) {
			ui.Row(c).FillWidth().Justify(ui.End).Gap(8).Children(func() {
				cancel := nui.Button(c.Key("alias-cancel"), palette, locales.HistoryCancel(), nui.ButtonOptions{Color: nui.Neutral, Variant: nui.Outline, Disabled: saving})
				if !saving {
					cancel.OnClick(func() { d.open = false })
				}
				save := nui.Button(c.Key("alias-save"), palette, locales.CaptureSaveAlias(), nui.ButtonOptions{Loading: saving, Disabled: saving})
				if !saving {
					save.OnClick(func() { d.save(w) })
				}
			})
		},
	}, func(palette nui.Theme) {
		nui.Input(c.Key("alias-input"), palette, &d.draft, nui.InputOptions{
			Label: locales.CaptureSetAlias(), Placeholder: locales.CaptureAliasPlaceholder(), MaxLength: maxAliasLength, Disabled: saving,
			OnSubmit: func(string) { d.save(w) },
		})
	})
}

// Restart begins a new capture session, asking what to do with existing
// records.
func (w *Workspace) Restart() {
	restart := func(saveCurrent bool, success string) {
		w.Data.Proxy.RestartCapture(saveCurrent, func(err error) {
			if err != nil {
				w.Notifier.Error(locales.CaptureRestartFailed(err.Error()))
				return
			}
			w.Data.Workbench.SelectCaptureItem(store.CaptureItemLive)
			w.Notifier.Success(success)
		})
	}
	if w.Data.Traffic.Statistics.Total == 0 {
		restart(false, locales.CaptureRestartSuccess())
		return
	}
	w.Confirm.Ask(widgets.ConfirmRequest{
		Title: locales.CaptureRestartTitle(), Message: locales.CaptureRestartConfirm(),
		Confirm:     widgets.ConfirmAction{Label: locales.CaptureRestartSave(), Run: func() { restart(true, locales.CaptureRestartSavedSuccess()) }},
		Secondary:   &widgets.ConfirmAction{Label: locales.CaptureRestartDiscard(), Color: nui.Error, Run: func() { restart(false, locales.CaptureRestartDiscardedSuccess()) }},
		CancelLabel: locales.HistoryCancel(),
	})
}

// resendDialog sends one request repeatedly.
type resendDialog struct {
	open, sending, cancelled bool
	entryID                  uint64
	sequence                 uint64
	cancel                   context.CancelFunc

	count, delay, interval                nui.Optional[float64]
	countState, delayState, intervalState nui.NumberState
	useProxy                              bool
	upstream                              string
}

func validResendCount(value nui.Optional[float64]) bool {
	return value.Set && value.Value == float64(int(value.Value)) && value.Value >= 1 && value.Value <= maxResendCount
}

func (d *resendDialog) show(entryID uint64) {
	if d.sending {
		return
	}
	*d = resendDialog{
		open: true, entryID: entryID, sequence: d.sequence, useProxy: true,
		count: nui.Set[float64](3), delay: nui.Set[float64](0), interval: nui.Set[float64](1000),
	}
}

func (d *resendDialog) stop() {
	if d.cancel != nil {
		d.cancel()
	}
}

func (d *resendDialog) config() proxyservice.ResendConfig {
	config := proxyservice.ResendConfig{
		Count: int(d.count.Value), DelayMs: int64(d.delay.Value), IntervalMs: int64(d.interval.Value), UseProxy: d.useProxy,
	}
	if !d.useProxy {
		config.UpstreamProxy = d.upstream
	}
	return config
}

func (d *resendDialog) start(t *Table) {
	if d.sending || !validResendCount(d.count) || t.deps.Resend == nil {
		return
	}
	config := d.config()
	id := d.entryID
	ctx, cancel := context.WithCancel(context.Background())
	d.sending, d.cancelled, d.cancel = true, false, cancel
	d.sequence++
	runtime.Run(t.deps.Runtime.Jobs, jobResendPrefix+strconv.FormatUint(d.sequence, 10), func(context.Context) (proxyservice.ResendResult, error) {
		return t.deps.Resend(ctx, id, config)
	}, func(result proxyservice.ResendResult, err error) {
		cancel()
		d.sending, d.cancel = false, nil
		switch {
		case errors.Is(err, context.Canceled):
			d.cancelled = d.open
		case err != nil:
			t.deps.Notifier.Error(locales.ResendModalError(err.Error()))
		default:
			t.deps.Notifier.Success(locales.ResendModalSuccess(strconv.Itoa(result.Failed), strconv.Itoa(result.Success)))
			d.open = false
		}
	})
}

// quick sends a request with a fixed configuration, reporting the result
// without the dialog.
func (d *resendDialog) quick(t *Table, id uint64, config proxyservice.ResendConfig) {
	d.sequence++
	runtime.Run(t.deps.Runtime.Jobs, jobResendPrefix+strconv.FormatUint(d.sequence, 10), func(ctx context.Context) (proxyservice.ResendResult, error) {
		return t.deps.Resend(ctx, id, config)
	}, func(result proxyservice.ResendResult, err error) {
		if err != nil {
			t.deps.Notifier.Error(locales.ResendModalError(err.Error()))
			return
		}
		t.deps.Notifier.Success(locales.ResendModalSuccess(strconv.Itoa(result.Failed), strconv.Itoa(result.Success)))
	})
}

func (d *resendDialog) numberRow(c *ui.Context, theme nui.Theme, key, label string, value *nui.Optional[float64], state *nui.NumberState, options nui.InputNumberOptions) {
	ui.Row(c.Key(key)).FillWidth().AlignItems(ui.Center).Gap(10).Children(func() {
		ui.Text(c, label).Width(120).Shrink(0).FontSize(14).TextColor(theme.Toned)
		ui.Box(c).Grow(1).Basis(0).MinWidth(0).Children(func() {
			options.InputOptions = nui.InputOptions{Label: label, Disabled: d.sending}
			options.AllowInvalid = true
			nui.InputNumber(c, theme, value, state, options)
		})
	})
}

func (d *resendDialog) build(c *ui.Context, theme nui.Theme, t *Table) {
	if !d.open {
		return
	}
	sending := d.sending
	wasOpen := d.open
	valid := validResendCount(d.count)
	nui.ModalWithOptions(c.Key("resend-modal"), theme, &d.open, nui.ModalOptions{
		Title: locales.ResendModalTitle(), Dismissible: nui.Set(!sending), CloseButton: nui.Set(!sending),
		Footer: func(c *ui.Context, palette nui.Theme) {
			ui.Row(c).FillWidth().Justify(ui.End).Gap(8).Children(func() {
				cancel := nui.Button(c.Key("resend-cancel"), palette, locales.ResendModalCancel(), nui.ButtonOptions{Color: nui.Neutral, Variant: nui.Outline, Disabled: sending})
				if !sending {
					cancel.OnClick(func() { d.open = false })
				}
				if sending {
					nui.Button(c.Key("resend-stop"), palette, locales.ResendModalStop(), nui.ButtonOptions{Color: nui.Error, Leading: lucide.Square()}).OnClick(d.stop)
					return
				}
				confirm := nui.Button(c.Key("resend-confirm"), palette, locales.ResendModalConfirm(), nui.ButtonOptions{Disabled: !valid})
				if valid {
					confirm.OnClick(func() { d.start(t) })
				}
			})
		},
	}, func(palette nui.Theme) {
		ui.Column(c).FillWidth().Gap(12).Children(func() {
			d.numberRow(c, palette, "count", locales.ResendModalCount(), &d.count, &d.countState, nui.InputNumberOptions{Min: nui.Set[float64](1), Max: nui.Set[float64](maxResendCount), Step: 1})
			d.numberRow(c, palette, "delay", locales.ResendModalDelay(), &d.delay, &d.delayState, nui.InputNumberOptions{Min: nui.Set[float64](0), Step: 100})
			d.numberRow(c, palette, "interval", locales.ResendModalInterval(), &d.interval, &d.intervalState, nui.InputNumberOptions{Min: nui.Set[float64](0), Step: 100})
			ui.Row(c.Key("use-proxy")).FillWidth().AlignItems(ui.Center).Gap(10).Children(func() {
				ui.Text(c, locales.ResendModalUseProxy()).Width(120).Shrink(0).FontSize(14).TextColor(palette.Toned)
				nui.Switch(c, palette, &d.useProxy, nui.SwitchOptions{Label: locales.ResendModalUseProxy(), Disabled: sending})
			})
			if !d.useProxy {
				ui.Row(c.Key("upstream")).FillWidth().AlignItems(ui.Center).Gap(10).Children(func() {
					ui.Text(c, locales.ResendModalUpstreamProxy()).Width(120).Shrink(0).FontSize(14).TextColor(palette.Toned)
					ui.Box(c).Grow(1).Basis(0).MinWidth(0).Children(func() {
						nui.Input(c, palette, &d.upstream, nui.InputOptions{Label: locales.ResendModalUpstreamProxy(), Placeholder: locales.ResendModalUpstreamProxyPlaceholder(), Disabled: sending})
					})
				})
			}
			if d.cancelled {
				nui.Alert(c, palette, nui.AlertOptions{Title: locales.ResendModalCancelled(), Color: nui.Neutral, Variant: nui.Subtle, Icon: lucide.CircleStop()})
			}
		})
	})
	if wasOpen && !d.open && sending {
		d.stop()
	}
}
