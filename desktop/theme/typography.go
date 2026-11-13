package theme

import "github.com/egoist/mygo/ui"

// TextSize scales custom interface text from the 14-DIP control baseline.
// The window's native theme already contains the user's interface size; DPI
// and accessibility zoom are applied by MyGo separately. Code and font-preview
// text use their own explicit sizes and must not pass through this function.
func TextSize(c *ui.Context, base float32) float32 {
	return base * c.Theme().FontSize / 14
}
