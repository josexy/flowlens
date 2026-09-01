//go:build !windows

package settingservice

import (
	"os/exec"
	"strings"
)

// listSystemFontFamilies tries to enumerate installed fonts via fc-list
// (fontconfig). If fc-list is unavailable or returns no usable output,
// nil is returned and the caller falls back to defaultFontFamilies().
func listSystemFontFamilies() []string {
	// --format prints one family value per font file.
	// Some entries are comma-separated when a font has multiple family names.
	out, err := exec.Command("fc-list", "--format=%{family}\\n").Output()
	if err != nil || len(out) == 0 {
		return nil
	}

	seen := make(map[string]struct{})
	for _, line := range strings.Split(string(out), "\n") {
		// fc-list may return "Family A,Family B" for a single font file.
		for _, fam := range strings.Split(line, ",") {
			fam = strings.TrimSpace(fam)
			if fam != "" {
				seen[fam] = struct{}{}
			}
		}
	}

	fonts := make([]string, 0, len(seen))
	for fam := range seen {
		fonts = append(fonts, fam)
	}
	return fonts
}
