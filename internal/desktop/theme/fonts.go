// Package theme builds the interface theme from the user's appearance settings.
package theme

import (
	"fmt"
	"sync"

	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/resources"
)

// Bundled font families, used when the user has not chosen a font.
const (
	DefaultFontFamily = "DM Sans"
	DefaultCodeFamily = "JetBrains Mono"
)

var (
	fontsOnce sync.Once
	fontsErr  error
)

// RegisterFonts makes the bundled fonts available. Only the first call does
// any work.
func RegisterFonts() error {
	fontsOnce.Do(func() {
		for _, font := range []struct {
			data   []byte
			family string
		}{
			{resources.DMSans, DefaultFontFamily},
			{resources.JetBrainsMono, DefaultCodeFamily},
		} {
			if err := ui.RegisterFont(font.data, font.family); err != nil {
				fontsErr = fmt.Errorf("register font %q: %w", font.family, err)
				return
			}
		}
	})
	return fontsErr
}
