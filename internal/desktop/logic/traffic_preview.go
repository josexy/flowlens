package logic

import "unicode/utf8"

// TrafficCellPreviewBytes bounds text before shaping, including the ellipsis.
const TrafficCellPreviewBytes = 256

// TrafficCellPreview is only for table labels. Search, sorting and actions must
// keep using the original value, including text beyond the displayed prefix.
func TrafficCellPreview(text string) string {
	if len(text) <= TrafficCellPreviewBytes {
		return text
	}
	const ellipsis = "…"
	end := TrafficCellPreviewBytes - len(ellipsis)
	for end > 0 && !utf8.RuneStart(text[end]) {
		end--
	}
	return text[:end] + ellipsis
}
