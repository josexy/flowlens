//go:build windows

package settingservice

import (
	"strings"

	"golang.org/x/sys/windows/registry"
)

func listSystemFontFamilies() []string {
	seen := make(map[string]struct{})
	collectWindowsFontKey(registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Windows NT\CurrentVersion\Fonts`, seen)
	collectWindowsFontKey(registry.CURRENT_USER, `SOFTWARE\Microsoft\Windows NT\CurrentVersion\Fonts`, seen)

	fonts := make([]string, 0, len(seen))
	for font := range seen {
		fonts = append(fonts, font)
	}
	return fonts
}

func collectWindowsFontKey(root registry.Key, path string, seen map[string]struct{}) {
	key, err := registry.OpenKey(root, path, registry.READ)
	if err != nil {
		return
	}
	defer key.Close()

	names, err := key.ReadValueNames(0)
	if err != nil {
		return
	}
	for _, name := range names {
		family := normalizeWindowsFontName(name)
		if family == "" {
			continue
		}
		seen[family] = struct{}{}
	}
}

func normalizeWindowsFontName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	if idx := strings.LastIndex(name, " ("); idx > 0 {
		name = name[:idx]
	}
	replacements := []string{
		" Bold Italic",
		" Bold Oblique",
		" Semibold Italic",
		" Semibold",
		" DemiBold Italic",
		" DemiBold",
		" Italic",
		" Oblique",
		" Bold",
		" Light",
		" Regular",
		" Medium",
		" Black",
	}
	for _, suffix := range replacements {
		name = strings.TrimSuffix(name, suffix)
	}
	return strings.TrimSpace(name)
}
