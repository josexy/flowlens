package widgets

import (
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	nui "github.com/josexy/nuxt-ui-go/ui"
)

// InputNumber keeps nui's parsing, keyboard, validation and stepper behavior.
// Unfocused valid values show digit grouping; editing and invalid drafts keep
// the native text intact, so formatting never changes the stored value/caret.
func InputNumber(c *ui.Context, theme nui.Theme, value *nui.Optional[float64], state *nui.NumberState, options nui.InputNumberOptions) ui.Element {
	style := options.Style
	options.Style = func(root, input, leading, trailing ui.Element) {
		if style != nil {
			style(root, input, leading, trailing)
		}
		if !value.Set || state.Handle.Focused(c) || state.Error != "" || value.Value > -1000 && value.Value < 1000 {
			return
		}
		input.TextColor(ui.Transparent)
		root.Children(func() {
			ui.Row(c.Key("grouped-number")).Absolute().Left(10).Right(36).Top(0).Bottom(0).AlignItems(ui.Center).IgnoreInput(true).AccessHidden(true).Children(func() {
				ui.Text(c, groupedNumber(value.Value)).SingleLine().MinWidth(0).Font(theme.Font).FontSize(theme.FontSize).TextColor(theme.Text)
			})
		})
	}
	return nui.InputNumber(c, theme, value, state, options)
}

func groupedNumber(value float64) string {
	whole, fraction, decimal := strings.Cut(strconv.FormatFloat(value, 'f', -1, 64), ".")
	sign := ""
	if strings.HasPrefix(whole, "-") {
		sign, whole = "-", whole[1:]
	}
	for index := len(whole) - 3; index > 0; index -= 3 {
		whole = whole[:index] + "," + whole[index:]
	}
	if decimal {
		whole += "." + fraction
	}
	return sign + whole
}
