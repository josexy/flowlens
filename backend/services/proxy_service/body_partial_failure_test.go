package proxyservice

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/josexy/flowlens/backend/pkg/body_cache"
	"github.com/josexy/flowlens/backend/pkg/fs"
)

func TestExportHARKeepsReadableBodyWhenOtherCacheFileIsMissing(t *testing.T) {
	for _, missingKind := range []string{bodycache.KindRequest, bodycache.KindResponse} {
		t.Run(missingKind, func(t *testing.T) {
			service, entry, cache, requestData, responseData := newPartialBodyFailureFixture(t)
			removeCachedBodyFileWithoutUpdatingIndex(t, cache, entry.ID, missingKind)

			target := filepath.Join(t.TempDir(), "partial.har")
			result, err := service.ExportHAR(HARExportRequest{Path: target})
			if err != nil {
				t.Fatalf("ExportHAR: %v", err)
			}
			if result != (HARWriteResult{Exported: 1, MissingBodies: 1}) {
				t.Fatalf("result = %+v", result)
			}

			data, err := os.ReadFile(target)
			if err != nil {
				t.Fatalf("ReadFile HAR: %v", err)
			}
			var document struct {
				Log struct {
					Entries []struct {
						Request struct {
							PostData *struct {
								Text string `json:"text"`
							} `json:"postData"`
						} `json:"request"`
						Response struct {
							Content struct {
								Size int64   `json:"size"`
								Text *string `json:"text"`
							} `json:"content"`
						} `json:"response"`
					} `json:"entries"`
				} `json:"log"`
			}
			if err := json.Unmarshal(data, &document); err != nil {
				t.Fatalf("Unmarshal HAR: %v", err)
			}
			if len(document.Log.Entries) != 1 {
				t.Fatalf("entries = %d", len(document.Log.Entries))
			}
			got := document.Log.Entries[0]
			if missingKind == bodycache.KindRequest {
				if got.Request.PostData != nil {
					t.Fatalf("missing request postData = %+v, want omitted", got.Request.PostData)
				}
				if got.Response.Content.Text == nil || *got.Response.Content.Text != string(responseData) || got.Response.Content.Size != int64(len(responseData)) {
					t.Fatalf("readable response content = %+v", got.Response.Content)
				}
			} else {
				if got.Request.PostData == nil || got.Request.PostData.Text != string(requestData) {
					t.Fatalf("readable request postData = %+v", got.Request.PostData)
				}
				if got.Response.Content.Text != nil || got.Response.Content.Size != -1 {
					t.Fatalf("missing response content = %+v", got.Response.Content)
				}
			}
		})
	}
}

func TestHistoryFlushKeepsReadableBodyWhenOtherCacheFileIsMissing(t *testing.T) {
	for _, missingKind := range []string{bodycache.KindRequest, bodycache.KindResponse} {
		t.Run(missingKind, func(t *testing.T) {
			configDir := t.TempDir()
			t.Setenv("APPDATA", configDir)
			t.Setenv("XDG_CONFIG_HOME", configDir)
			service, entry, cache, requestData, responseData := newPartialBodyFailureFixture(t)
			removeCachedBodyFileWithoutUpdatingIndex(t, cache, entry.ID, missingKind)

			if err := service.flushHistoryToDisk(true); err != nil {
				t.Fatalf("flushHistoryToDisk: %v", err)
			}
			historyDir, err := getHistoryStoragePath()
			if err != nil {
				t.Fatalf("getHistoryStoragePath: %v", err)
			}
			hbinPath := filepath.Join(historyDir, fs.GetHBinFileName(service.currentHistoryMetadata.Key))
			file, err := os.Open(hbinPath)
			if err != nil {
				t.Fatalf("Open history: %v", err)
			}
			defer file.Close()

			metadata, err := DecodeHistoryMetadata(file)
			if err != nil {
				t.Fatalf("DecodeHistoryMetadata: %v", err)
			}
			if metadata.FormatVersion != hbinVersionCurrent || metadata.Total != 1 {
				t.Fatalf("metadata = %+v", metadata)
			}
			persistedEntry, err := DecodeTrafficEntryWithVersion(file, metadata.FormatVersion)
			if err != nil {
				t.Fatalf("DecodeTrafficEntryWithVersion: %v", err)
			}
			if persistedEntry.ID != entry.ID {
				t.Fatalf("persisted entry ID = %d, want %d", persistedEntry.ID, entry.ID)
			}
			requestBody, responseBody, err := DecodeTrafficHARBodies(file)
			if err != nil {
				t.Fatalf("DecodeTrafficHARBodies: %v", err)
			}
			if missingKind == bodycache.KindRequest {
				if requestBody.Available || responseBody.Available != true || string(responseBody.Data) != string(responseData) {
					t.Fatalf("persisted bodies = request %+v, response %+v", requestBody, responseBody)
				}
			} else {
				if !requestBody.Available || string(requestBody.Data) != string(requestData) || responseBody.Available {
					t.Fatalf("persisted bodies = request %+v, response %+v", requestBody, responseBody)
				}
			}
		})
	}
}

func newPartialBodyFailureFixture(t *testing.T) (*ProxyService, *TrafficEntry, *bodycache.BodyCache, []byte, []byte) {
	t.Helper()
	service := newTestProxyService(t, nil)
	cache, err := bodycache.NewWithDir(t.TempDir())
	if err != nil {
		t.Fatalf("bodycache.NewWithDir: %v", err)
	}
	service.bodyCache = cache

	requestData := []byte("request payload")
	responseData := []byte("response payload")
	started := time.Date(2026, 8, 12, 18, 0, 0, 0, time.UTC)
	entry := service.newTrafficEntry(TrafficEntry{
		Type:       "https",
		StartedAt:  started,
		Method:     "POST",
		URL:        "https://example.test/data",
		Host:       "example.test",
		Path:       "/data",
		StatusCode: 200,
		Status:     "200 OK",
		Request: &HTTPMessage{
			Proto:        "HTTP/1.1",
			HeaderFields: []HTTPHeaderField{{Name: "Content-Type", Value: "text/plain; charset=utf-8"}},
			Metrics: &HTTPMessageMetrics{
				StartedAtMicros: started.UnixMicro(),
				EndedAtMicros:   started.Add(time.Millisecond).UnixMicro(),
				HeaderSize:      41,
				BodySize:        int64(len(requestData)),
				State:           HTTPMessageStateCompleted,
			},
		},
		Response: &HTTPMessage{
			Proto:        "HTTP/1.1",
			HeaderFields: []HTTPHeaderField{{Name: "Content-Type", Value: "text/plain; charset=utf-8"}},
			Metrics: &HTTPMessageMetrics{
				StartedAtMicros: started.Add(2 * time.Millisecond).UnixMicro(),
				EndedAtMicros:   started.Add(3 * time.Millisecond).UnixMicro(),
				HeaderSize:      41,
				BodySize:        int64(len(responseData)),
				State:           HTTPMessageStateCompleted,
			},
		},
	})
	service.storeTrafficEntry(entry)
	service.trafficBodies.Store(entry.ID, &TrafficBodies{})
	if err := cache.Write(entry.ID, bodycache.KindRequest, requestData); err != nil {
		t.Fatalf("cache.Write request: %v", err)
	}
	if err := cache.Write(entry.ID, bodycache.KindResponse, responseData); err != nil {
		t.Fatalf("cache.Write response: %v", err)
	}
	return service, entry, cache, requestData, responseData
}

func removeCachedBodyFileWithoutUpdatingIndex(t *testing.T, cache *bodycache.BodyCache, id uint64, kind string) {
	t.Helper()
	path := filepath.Join(cache.SessionDir(), fmt.Sprintf("%d_%s.body", id, kind))
	if err := os.Remove(path); err != nil {
		t.Fatalf("Remove cache file: %v", err)
	}
	if !cache.Has(id, kind) {
		t.Fatalf("cache index no longer records %s after external file removal", kind)
	}
}
