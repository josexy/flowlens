package logic

import (
	"bytes"
	"encoding/json"
	"math"
	"regexp"
	"slices"
	"strings"
)

// BodyCategory is what kind of content a body holds.
type BodyCategory string

// Body categories.
const (
	BodyJSON   BodyCategory = "json"
	BodyXML    BodyCategory = "xml"
	BodyHTML   BodyCategory = "html"
	BodyJS     BodyCategory = "js"
	BodyCSS    BodyCategory = "css"
	BodyText   BodyCategory = "text"
	BodyImage  BodyCategory = "image"
	BodySVG    BodyCategory = "svg"
	BodyBinary BodyCategory = "binary"
)

// BodyTab names a body viewing mode.
type BodyTab string

// Body viewing modes.
const (
	BodyTabFormatted BodyTab = "formatted"
	BodyTabImage     BodyTab = "image"
	BodyTabRaw       BodyTab = "raw"
	BodyTabSSE       BodyTab = "sse"
	BodyTabHex       BodyTab = "hex"
)

// LargeTextThreshold is the size above which text is shown plain.
const LargeTextThreshold = 512 * 1024

// Image zoom limits.
const (
	ImageScaleMin  = 0.2
	ImageScaleMax  = 5.0
	ImageScaleStep = 0.1
)

func mediaType(contentType string) string {
	media, _, _ := strings.Cut(strings.ToLower(contentType), ";")
	return strings.TrimSpace(media)
}

// ClassifyBody returns the category of a body.
func ClassifyBody(contentType, encoding string) BodyCategory {
	media := mediaType(contentType)
	switch {
	case strings.Contains(media, "json"):
		return BodyJSON
	case media == "image/svg+xml":
		return BodySVG
	case strings.HasPrefix(media, "image/"):
		return BodyImage
	case strings.Contains(media, "xml"):
		return BodyXML
	case strings.Contains(media, "html"):
		return BodyHTML
	case strings.Contains(media, "javascript"):
		return BodyJS
	case strings.Contains(media, "css"):
		return BodyCSS
	case strings.HasPrefix(media, "text/"):
		return BodyText
	case encoding == base64Encoding:
		return BodyBinary
	default:
		return BodyText
	}
}

// BodyTabs returns the viewing modes for a body, in display order.
func BodyTabs(contentType, encoding, body string, enableSSE bool) []BodyTab {
	category := ClassifyBody(contentType, encoding)
	var tabs []BodyTab
	formattable := slices.Contains([]BodyCategory{BodyJSON, BodyXML, BodyHTML, BodyJS, BodyCSS, BodySVG}, category)
	if formattable && EstimateDecodedByteLength(body, encoding) < LargeTextThreshold {
		tabs = append(tabs, BodyTabFormatted)
	}
	if category == BodyImage || category == BodySVG {
		tabs = append(tabs, BodyTabImage)
	}
	tabs = append(tabs, BodyTabRaw)
	if enableSSE && IsEventStream(contentType) {
		tabs = append(tabs, BodyTabSSE)
	}
	return append(tabs, BodyTabHex)
}

// NextBodyTab returns the tab to show after the available tabs changed.
func NextBodyTab(active BodyTab, tabs, previous []BodyTab) BodyTab {
	switch {
	case slices.Contains(tabs, BodyTabSSE) && !slices.Contains(previous, BodyTabSSE):
		return BodyTabSSE
	case slices.Contains(tabs, BodyTabImage) && !slices.Contains(previous, BodyTabImage):
		return BodyTabImage
	case len(tabs) > 0 && !slices.Contains(tabs, active):
		return tabs[0]
	default:
		return active
	}
}

// EditorLanguage returns the code view language of a category.
func EditorLanguage(category BodyCategory) string {
	switch category {
	case BodyJSON:
		return "json"
	case BodyXML, BodySVG:
		return "xml"
	case BodyHTML:
		return "html"
	case BodyJS:
		return "javascript"
	case BodyCSS:
		return "css"
	default:
		return "plaintext"
	}
}

// FormatBody returns a body in its formatted view.
func FormatBody(body string, category BodyCategory) string {
	if category != BodyJSON || body == "" {
		return body
	}
	var out bytes.Buffer
	if err := json.Indent(&out, []byte(body), "", "  "); err != nil {
		return body
	}
	return out.String()
}

// ClampImageScale limits a zoom to two decimals within the allowed range.
func ClampImageScale(scale float64) float64 {
	return math.Min(ImageScaleMax, math.Max(ImageScaleMin, math.Round(scale*100)/100))
}

// ImageFormatLabel returns a short name for an image type.
func ImageFormatLabel(contentType string) string {
	media := mediaType(contentType)
	switch media {
	case "image/svg+xml":
		return "SVG"
	case "image/png":
		return "PNG"
	case "image/jpeg":
		return "JPEG"
	case "image/gif":
		return "GIF"
	case "image/webp":
		return "WEBP"
	case "image/avif":
		return "AVIF"
	case "image/bmp":
		return "BMP"
	case "image/x-icon", "image/vnd.microsoft.icon":
		return "ICO"
	}
	if name, ok := strings.CutPrefix(media, "image/"); ok {
		base, _, _ := strings.Cut(name, "+")
		return strings.ToUpper(base)
	}
	return ""
}

var (
	extensionsByMedia = map[string][]string{
		"application/json": {".json"}, "application/xml": {".xml"}, "text/xml": {".xml"},
		"text/html": {".html", ".htm"}, "application/javascript": {".js", ".mjs", ".cjs"},
		"text/javascript": {".js", ".mjs", ".cjs"}, "text/css": {".css"}, "image/svg+xml": {".svg"},
		"image/png": {".png"}, "image/jpeg": {".jpg", ".jpeg"}, "image/gif": {".gif"},
		"image/webp": {".webp"}, "image/avif": {".avif"},
	}
	unsafeFilenamePattern = regexp.MustCompile(`[<>:"/\\|?*\x00-\x1f]`)
)

func inferExtension(contentType string, category BodyCategory) string {
	if extensions, ok := extensionsByMedia[mediaType(contentType)]; ok {
		return extensions[0]
	}
	if category == BodyImage || category == BodyBinary {
		return ".bin"
	}
	return ".txt"
}

// InferBodyFilename suggests a file name for saving a body.
func InferBodyFilename(sourcePath, contentType string, category BodyCategory, fallback string) string {
	path, _, _ := strings.Cut(sourcePath, "?")
	path, _, _ = strings.Cut(path, "#")
	base := fallback
	segments := strings.FieldsFunc(path, func(r rune) bool { return r == '/' })
	if len(segments) > 0 {
		base = segments[len(segments)-1]
	}
	base = strings.TrimRight(strings.TrimSpace(unsafeFilenamePattern.ReplaceAllString(base, "-")), ". \t\r\n")
	if base == "" {
		base = fallback
	}
	inferred := inferExtension(contentType, category)
	dot := strings.LastIndexByte(base, '.')
	if dot <= 0 || dot == len(base)-1 {
		return base + inferred
	}
	stem, extension := base[:dot], strings.ToLower(base[dot:])
	if known, ok := extensionsByMedia[mediaType(contentType)]; !ok || slices.Contains(known, extension) {
		return base
	}
	return stem + inferred
}
