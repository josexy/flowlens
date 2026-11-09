package logic

import (
	"slices"
	"strings"
	"testing"
)

func TestClassifyBody(t *testing.T) {
	for _, test := range []struct {
		contentType, encoding string
		want                  BodyCategory
	}{
		{"application/json; charset=utf-8", "", BodyJSON}, {"application/problem+json", "", BodyJSON},
		{"image/svg+xml", "", BodySVG}, {"image/png", "base64", BodyImage}, {"application/xml", "", BodyXML},
		{"TEXT/HTML", "", BodyHTML}, {"application/javascript", "", BodyJS}, {"text/css", "", BodyCSS},
		{"text/plain", "", BodyText}, {"application/octet-stream", "base64", BodyBinary}, {"", "", BodyText},
	} {
		if got := ClassifyBody(test.contentType, test.encoding); got != test.want {
			t.Errorf("%q: %s", test.contentType, got)
		}
	}
}

func TestBodyTabs(t *testing.T) {
	check := func(contentType, encoding, body string, sse bool, want ...BodyTab) {
		t.Helper()
		if got := BodyTabs(contentType, encoding, body, sse); !slices.Equal(got, want) {
			t.Fatalf("%q: %v", contentType, got)
		}
	}
	check("application/json", "", "{}", false, BodyTabFormatted, BodyTabRaw, BodyTabHex)
	check("image/png", "base64", "AA==", false, BodyTabImage, BodyTabRaw, BodyTabHex)
	check("image/svg+xml", "", "<svg/>", false, BodyTabFormatted, BodyTabImage, BodyTabRaw, BodyTabHex)
	check("text/event-stream", "", "", true, BodyTabRaw, BodyTabSSE, BodyTabHex)
	check("text/event-stream", "", "", false, BodyTabRaw, BodyTabHex)
	check("application/json", "", strings.Repeat("x", LargeTextThreshold), false, BodyTabRaw, BodyTabHex)
}

func TestNextBodyTab(t *testing.T) {
	sse := []BodyTab{BodyTabRaw, BodyTabSSE, BodyTabHex}
	raw := []BodyTab{BodyTabRaw, BodyTabHex}
	if NextBodyTab(BodyTabRaw, sse, nil) != BodyTabSSE {
		t.Fatal("default to SSE")
	}
	if NextBodyTab(BodyTabRaw, sse, sse) != BodyTabRaw {
		t.Fatal("manual choice")
	}
	if NextBodyTab(BodyTabSSE, raw, sse) != BodyTabRaw {
		t.Fatal("fallback")
	}
	if NextBodyTab(BodyTabRaw, []BodyTab{BodyTabImage, BodyTabRaw}, raw) != BodyTabImage {
		t.Fatal("image default")
	}
}

func TestFormatBody(t *testing.T) {
	if got := FormatBody(`{"b":1,"a":[2]}`, BodyJSON); got != "{\n  \"b\": 1,\n  \"a\": [\n    2\n  ]\n}" {
		t.Fatal(got)
	}
	if got := FormatBody("not json", BodyJSON); got != "not json" {
		t.Fatal(got)
	}
	if got := FormatBody("<a/>", BodyXML); got != "<a/>" {
		t.Fatal(got)
	}
}

func TestImageHelpers(t *testing.T) {
	if ClampImageScale(9) != ImageScaleMax || ClampImageScale(0.01) != ImageScaleMin || ClampImageScale(1.234) != 1.23 {
		t.Fatal("clamp")
	}
	for contentType, want := range map[string]string{"image/png; x=1": "PNG", "image/x-custom+zip": "X-CUSTOM", "text/plain": ""} {
		if got := ImageFormatLabel(contentType); got != want {
			t.Errorf("%q: %q", contentType, got)
		}
	}
}

func TestInferBodyFilename(t *testing.T) {
	for _, test := range []struct {
		path, contentType string
		category          BodyCategory
		want              string
	}{
		{"/a/report.json?x=1#h", "application/json", BodyJSON, "report.json"},
		{"/a/report", "application/json", BodyJSON, "report.json"},
		{"/a/data.txt", "application/json", BodyJSON, "data.json"},
		{"/", "image/png", BodyImage, "body.png"},
		{"/a<b>:c.", "text/plain", BodyText, "a-b--c.txt"},
		{"/x.bin", "application/unknown", BodyBinary, "x.bin"},
		{"", "application/octet-stream", BodyBinary, "body.bin"},
		{"/page.htm", "text/html", BodyHTML, "page.htm"},
	} {
		if got := InferBodyFilename(test.path, test.contentType, test.category, "body"); got != test.want {
			t.Errorf("%q: %q", test.path, got)
		}
	}
}
