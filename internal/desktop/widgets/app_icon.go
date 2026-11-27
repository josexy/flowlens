package widgets

import (
	"sync"

	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/resources"
)

var appIcon = sync.OnceValue(func() *ui.Bitmap {
	icon, err := ui.DecodeBitmapScaled(resources.AppIcon, 256)
	if err != nil {
		return nil
	}
	return icon
})

// AppIcon returns the shared, at-most-256-pixel application bitmap, or nil if
// decoding failed. Consumers use it on the UI thread and must not mutate it.
func AppIcon() *ui.Bitmap { return appIcon() }
