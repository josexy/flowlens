package logic

import (
	"encoding/json"
	"os"
	"testing"

	proxyservice "github.com/josexy/flowlens/internal/services/proxy_service"
)

// TestRawPanelMatchesLogicalHeaderSizeFixtures checks the Raw panel and the
// displayed sizes against the fixtures the proxy service measures its logical
// header sizes with, so the two cannot drift apart.
func TestRawPanelMatchesLogicalHeaderSizeFixtures(t *testing.T) {
	data, err := os.ReadFile("../../services/proxy_service/testdata/logical_header_sizes.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name         string
		Entry        proxyservice.TrafficEntry
		RequestHead  string
		ResponseHead string
	}
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	if len(fixtures) == 0 {
		t.Fatal("no fixtures")
	}
	for _, fixture := range fixtures {
		t.Run(fixture.Name, func(t *testing.T) {
			entry := fixture.Entry
			if entry.Request == nil || entry.Response == nil {
				t.Fatal("fixture has no request or response")
			}
			request := FormatRawHTTPRequest(RawHTTPRequestInput{
				Method: entry.Method, URL: entry.URL, Host: entry.Host,
				Protocol: entry.Request.Proto, HeaderFields: entry.Request.HeaderFields,
			})
			if request != fixture.RequestHead {
				t.Errorf("request head = %q, want %q", request, fixture.RequestHead)
			}
			response := FormatRawHTTPResponse(RawHTTPResponseInput{
				Status: entry.Status, StatusCode: entry.StatusCode,
				Protocol: entry.Response.Proto, HeaderFields: entry.Response.HeaderFields,
			})
			if response != fixture.ResponseHead {
				t.Errorf("response head = %q, want %q", response, fixture.ResponseHead)
			}

			// Sizes count UTF-8 bytes, which len gives for a Go string.
			requestSize, responseSize := int64(len(fixture.RequestHead)), int64(len(fixture.ResponseHead))
			const responseBody = 7
			if got := SummarizeHTTPMessageSize(requestSize, 0, false).Header; got != requestSize {
				t.Errorf("request header size = %d, want %d", got, requestSize)
			}
			if got := SummarizeHTTPMessageSize(responseSize, responseBody, false).Total; got != responseSize+responseBody {
				t.Errorf("response total = %d, want %d", got, responseSize+responseBody)
			}
			requestMessage, responseMessage := *entry.Request, *entry.Response
			requestMessage.Metrics = &proxyservice.HTTPMessageMetrics{HeaderSize: requestSize}
			responseMessage.Metrics = &proxyservice.HTTPMessageMetrics{HeaderSize: responseSize, BodySize: responseBody}
			entry.Request, entry.Response = &requestMessage, &responseMessage
			if got, known := TrafficTotalSizeBytes(&entry); !known || got != requestSize+responseSize+responseBody {
				t.Errorf("exchange size = %d (known %v), want %d", got, known, requestSize+responseSize+responseBody)
			}
		})
	}
}
