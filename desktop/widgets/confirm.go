package widgets

import (
	"github.com/egoist/mygo/ui"
	nui "github.com/josexy/nuxt-ui-go"
)

// ConfirmAction is a button in a confirmation dialog.
type ConfirmAction struct {
	Label string
	Color nui.Color
	Run   func()
}

// ConfirmRequest describes one confirmation. Dismissing the dialog counts as
// Cancel.
type ConfirmRequest struct {
	Title   string
	Message string
	Confirm ConfirmAction
	// Secondary is an optional extra choice, such as Discard beside Save.
	Secondary *ConfirmAction
	// CancelLabel names the cancel button. Empty hides the button.
	CancelLabel string
	OnCancel    func()
}

// ConfirmDialog shows one confirmation at a time for a window.
type ConfirmDialog struct {
	open    bool
	request ConfirmRequest
	chosen  bool
}

// Ask opens the dialog. A dialog already open is replaced and counts as
// canceled.
func (d *ConfirmDialog) Ask(request ConfirmRequest) {
	if d.open {
		d.finish(d.request.OnCancel)
	}
	d.request, d.open, d.chosen = request, true, false
}

// Accept chooses the confirm action, as its button does.
func (d *ConfirmDialog) Accept() {
	if d.open {
		d.finish(d.request.Confirm.Run)
	}
}

// Cancel dismisses the dialog, as its cancel button does.
func (d *ConfirmDialog) Cancel() {
	if d.open {
		d.finish(d.request.OnCancel)
	}
}

// Open reports whether a confirmation is showing.
func (d *ConfirmDialog) Open() bool { return d.open }

func (d *ConfirmDialog) finish(run func()) {
	if d.chosen {
		return
	}
	d.chosen = true
	d.open = false
	if run != nil {
		run()
	}
}

// Build shows the dialog while a confirmation is open. Build it once per frame.
func (d *ConfirmDialog) Build(c *ui.Context, theme nui.Theme) {
	if !d.open {
		return
	}
	request := d.request
	nui.ModalWithOptions(c.Key("confirm-dialog"), theme, &d.open, nui.ModalOptions{
		Title:   request.Title,
		Width:   nui.Set[float32](420),
		OnClose: func() { d.finish(request.OnCancel) },
		Footer: func(c *ui.Context, theme nui.Theme) {
			ui.Row(c).FillWidth().Gap(8).Justify(ui.End).Children(func() {
				if request.CancelLabel != "" {
					nui.Button(c.Key("cancel"), theme, request.CancelLabel, nui.ButtonOptions{Variant: nui.Outline, Color: nui.Neutral}).
						OnClick(func() { d.finish(request.OnCancel) })
				}
				if request.Secondary != nil {
					secondary := *request.Secondary
					nui.Button(c.Key("secondary"), theme, secondary.Label, nui.ButtonOptions{Variant: nui.Outline, Color: secondary.Color}).
						OnClick(func() { d.finish(secondary.Run) })
				}
				nui.Button(c.Key("confirm"), theme, request.Confirm.Label, nui.ButtonOptions{Color: request.Confirm.Color}).
					OnClick(func() { d.finish(request.Confirm.Run) })
			})
		},
	}, func(theme nui.Theme) {
		ui.Text(c, request.Message).TextColor(theme.Toned)
	})
}
