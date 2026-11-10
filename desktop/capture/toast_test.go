package capture

import (
	"errors"
	"testing"
	"time"

	proxyservice "github.com/josexy/flowlens/backend/services/proxy_service"
)

func TestHARImportFailureStaysEightSeconds(t *testing.T) {
	f := newHistoryFixture(t)
	f.history.import_ = func(path string) (proxyservice.HARImportResult, error) {
		if path == "/bad.har" {
			return proxyservice.HARImportResult{}, errors.New("corrupt")
		}
		return proxyservice.HARImportResult{Imported: 3, Metadata: &proxyservice.HistoryMetadata{Key: "k1"}}, nil
	}
	f.workspace.ImportHAR([]string{"/bad.har", "/good.har"})
	f.settle()
	f.requireText("corrupt")
	f.tester.Advance(7 * time.Second)
	f.requireText("corrupt")
	f.tester.Advance(2 * time.Second)
	f.requireNoText("corrupt")
}

func TestHARImportResultStaysFiveSeconds(t *testing.T) {
	f := newHistoryFixture(t)
	f.history.import_ = func(path string) (proxyservice.HARImportResult, error) {
		return proxyservice.HARImportResult{Imported: 1, Metadata: &proxyservice.HistoryMetadata{Key: "k1"}}, nil
	}
	f.workspace.ImportHAR([]string{"/good.har"})
	f.settle()
	f.requireText("Imported 1 files")
	f.tester.Advance(4 * time.Second)
	f.requireText("Imported 1 files")
	f.tester.Advance(2 * time.Second)
	f.requireNoText("Imported 1 files")
}
