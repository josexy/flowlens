package historyservice

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	proxyservice "github.com/josexy/flowlens/backend/services/proxy_service"
)

const historyImportFixture = `{"log":{"version":"1.2","entries":[{"startedDateTime":"2020-01-01T00:00:00.123456Z","time":3,"request":{"method":"POST","url":"https://example.test/a","httpVersion":"HTTP/1.1","headers":[],"bodySize":3,"postData":{"text":"abc"}},"response":{"status":200,"statusText":"OK","httpVersion":"HTTP/1.1","headers":[],"bodySize":3,"content":{"text":"xyz","size":3}},"timings":{"send":1,"wait":1,"receive":1}}]}}`

func TestImportHARPersistsIndependentHistories(t *testing.T) {
	setupHistoryTestStorage(t)
	path := filepath.Join(t.TempDir(), "browser.HAR")
	if err := os.WriteFile(path, []byte(historyImportFixture), 0600); err != nil {
		t.Fatal(err)
	}
	service := New(nil, nil)
	before := time.Now().UnixMilli()
	first, err := service.ImportHAR(context.Background(), HARImportRequest{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.ImportHAR(context.Background(), HARImportRequest{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	if first.Metadata.Key == second.Metadata.Key || first.Metadata.Alias != "browser" || first.Metadata.CreatedAt < before {
		t.Fatal(first, second)
	}
	if err := validateHistoryKey(first.Metadata.Key); err != nil {
		t.Fatal(err)
	}
	// A new service discovers imports without relying on the importer's index.
	reopened := New(nil, nil)
	list, err := reopened.ListHistoryKeys()
	if err != nil || len(list) != 2 {
		t.Fatal(list, err)
	}
	entries, err := reopened.GetHistory(first.Metadata.Key)
	if err != nil || len(entries) != 1 || entries[0].StartedAt.Year() != 2020 {
		t.Fatal(entries, err)
	}
	if md := entries[0].Metadata; !md.LocalConnectionEstablishedAt.IsZero() || !md.RemoteConnectionEstablishedAt.IsZero() || !md.RequestProcessedAt.IsZero() || !md.SSLHandshakeCompletedAt.IsZero() {
		t.Fatalf("missing connection dates became real timestamps: %+v", md)
	}
	view, err := reopened.GetHistoryTrafficBodyView(first.Metadata.Key, entries[0].ID)
	if err != nil || view.RequestBody != "abc" || view.ResponseBody != "xyz" {
		t.Fatal(view, err)
	}
	copyPath := filepath.Join(t.TempDir(), "roundtrip.har")
	if _, err := reopened.ExportHAR(HARExportRequest{Key: first.Metadata.Key, Path: copyPath}); err != nil {
		t.Fatal(err)
	}
	third, err := reopened.ImportHAR(context.Background(), HARImportRequest{Path: copyPath})
	if err != nil {
		t.Fatal(err)
	}
	roundtrip, err := reopened.GetHistory(third.Metadata.Key)
	if err != nil || *roundtrip[0].Request.Metrics != *entries[0].Request.Metrics || *roundtrip[0].Response.Metrics != *entries[0].Response.Metrics {
		t.Fatal(roundtrip, err)
	}
	if err := reopened.DeleteHistory(first.Metadata.Key); err != nil {
		t.Fatal(err)
	}
	if err := reopened.ClearHistories(); err != nil {
		t.Fatal(err)
	}
	list, err = reopened.ListHistoryKeys()
	if err != nil || len(list) != 0 {
		t.Fatal(list, err)
	}
	original, _ := os.ReadFile(path)
	if string(original) != historyImportFixture {
		t.Fatal("source changed")
	}
}

func TestImportHARRejectsInvalidPathsAndOversizedFiles(t *testing.T) {
	storage := setupHistoryTestStorage(t)
	service := New(nil, nil)
	large := filepath.Join(t.TempDir(), "large.har")
	f, err := os.Create(large)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(proxyservice.HARImportMaxFileSize + 1); err != nil {
		f.Close()
		t.Fatal(err)
	}
	f.Close()
	for _, path := range []string{"", "relative.har", filepath.Join(t.TempDir(), "missing.har"), t.TempDir(), large} {
		if _, err := service.ImportHAR(context.Background(), HARImportRequest{Path: path}); err == nil {
			t.Fatalf("accepted %q", path)
		}
	}
	files, _ := os.ReadDir(storage)
	if len(files) != 0 {
		t.Fatal(files)
	}
}

func TestImportHARConcurrentRefreshAndDelete(t *testing.T) {
	setupHistoryTestStorage(t)
	path := filepath.Join(t.TempDir(), "concurrent.har")
	if err := os.WriteFile(path, []byte(historyImportFixture), 0600); err != nil {
		t.Fatal(err)
	}
	service := New(nil, nil)
	old, err := service.ImportHAR(context.Background(), HARImportRequest{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	var jobs sync.WaitGroup
	for range 8 {
		jobs.Go(func() {
			if _, err := service.ImportHAR(context.Background(), HARImportRequest{Path: path}); err != nil {
				t.Error(err)
			}
		})
	}
	jobs.Go(func() {
		for range 10 {
			if _, err := service.ListHistoryKeys(); err != nil {
				t.Error(err)
			}
		}
	})
	jobs.Go(func() {
		if err := service.DeleteHistory(old.Metadata.Key); err != nil {
			t.Error(err)
		}
	})
	jobs.Wait()
	list, err := service.ListHistoryKeys()
	if err != nil || len(list) != 8 {
		t.Fatal(list, err)
	}
	for _, md := range list {
		entries, err := service.GetHistory(md.Key)
		if err != nil || len(entries) != 1 {
			t.Fatalf("incomplete history after concurrent refresh: %v %v", entries, err)
		}
	}
}

func TestImportHARShutdownCancelsAndWaits(t *testing.T) {
	service := New(nil, nil)
	ctx, done, err := service.beginHARImport(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	finished := make(chan struct{})
	go func() { _ = service.Shutdown(); close(finished) }()
	select {
	case <-ctx.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("shutdown did not cancel import")
	}
	select {
	case <-finished:
		t.Fatal("shutdown did not wait for cleanup")
	default:
	}
	done()
	select {
	case <-finished:
	case <-time.After(3 * time.Second):
		t.Fatal("shutdown did not finish")
	}
	if _, _, err := service.beginHARImport(context.Background()); err == nil {
		t.Fatal("new import accepted after shutdown")
	}
}

func TestImportHARCanceledWhileWaitingForStorage(t *testing.T) {
	setupHistoryTestStorage(t)
	path := filepath.Join(t.TempDir(), "waiting.har")
	if err := os.WriteFile(path, []byte(historyImportFixture), 0600); err != nil {
		t.Fatal(err)
	}
	service := New(nil, nil)
	service.storageMu.Lock()
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { _, err := service.ImportHAR(ctx, HARImportRequest{Path: path}); result <- err }()
	cancel()
	service.storageMu.Unlock()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("canceled import hung")
	}
	list, err := service.ListHistoryKeys()
	if err != nil || len(list) != 0 {
		t.Fatal(list, err)
	}
}
