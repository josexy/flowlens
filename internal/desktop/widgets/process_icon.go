package widgets

import (
	"context"
	"regexp"

	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/internal/desktop/runtime"
	proxyservice "github.com/josexy/flowlens/internal/services/proxy_service"
	"github.com/josexy/nuxt-ui-go/icons/lucide"
)

const processIconCacheSize = 256

var processIconKeyPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// ProcessIconSource reads stored process icons.
type ProcessIconSource func(key string) (*proxyservice.ProcessIconData, error)

// ProcessIcons caches process icons for every window.
type ProcessIcons struct {
	cache *IconCache[*ui.Bitmap]
}

// NewProcessIcons returns an empty icon store that loads through jobs.
func NewProcessIcons(jobs *runtime.Jobs, source ProcessIconSource) *ProcessIcons {
	icons := &ProcessIcons{}
	icons.cache = NewIconCache(processIconCacheSize, func(key string, finish func(*ui.Bitmap, bool)) {
		runtime.Run(jobs, "process-icon/"+key, func(context.Context) (*ui.Bitmap, error) {
			return decodeProcessIcon(source, key)
		}, func(bitmap *ui.Bitmap, err error) {
			finish(bitmap, err == nil && bitmap != nil)
		})
	})
	return icons
}

func decodeProcessIcon(source ProcessIconSource, key string) (*ui.Bitmap, error) {
	data, err := source(key)
	if err != nil || data == nil || data.MIMEType != "image/png" || len(data.Data) == 0 {
		return nil, err
	}
	return ui.DecodeBitmap(data.Data)
}

// Bitmap returns the icon for key, loading it if needed.
func (icons *ProcessIcons) Bitmap(key string) *ui.Bitmap {
	if !processIconKeyPattern.MatchString(key) {
		return nil
	}
	bitmap, _ := icons.cache.Get(key)
	return bitmap
}

// Clear forgets all icons, as after local data is cleared.
func (icons *ProcessIcons) Clear() { icons.cache.Clear() }

// ProcessIcon shows a process's icon, or a placeholder until it is available.
func ProcessIcon(c *ui.Context, icons *ProcessIcons, key string, size float32, muted ui.Color) ui.Element {
	if bitmap := icons.Bitmap(key); bitmap != nil {
		return ui.Image(c, bitmap).Size(size, size).Fit(ui.Contain)
	}
	return ui.Icon(c, lucide.AppWindow()).Size(size, size).TextColor(muted)
}
