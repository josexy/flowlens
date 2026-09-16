package historyservice

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/josexy/flowlens/backend/pkg/fs"
	proxyservice "github.com/josexy/flowlens/backend/services/proxy_service"
)

type HARImportRequest struct {
	Path string `json:"path"`
}

func (s *HistoryService) beginHARImport(parent context.Context) (context.Context, func(), error) {
	s.importMu.Lock()
	defer s.importMu.Unlock()
	if s.importsClosed {
		return nil, nil, errors.New("history service is shutting down")
	}
	ctx, cancel := context.WithCancel(parent)
	if s.imports == nil {
		s.imports = make(map[uint64]context.CancelFunc)
	}
	s.importSequence++
	id := s.importSequence
	s.imports[id] = cancel
	s.importWG.Add(1)
	return ctx, func() {
		cancel()
		s.importMu.Lock()
		delete(s.imports, id)
		s.importMu.Unlock()
		s.importWG.Done()
	}, nil
}

// ImportHAR commits one archive as a new history, independently of live capture.
func (s *HistoryService) ImportHAR(callCtx context.Context, request HARImportRequest) (proxyservice.HARImportResult, error) {
	ctx, done, err := s.beginHARImport(callCtx)
	if err != nil {
		return proxyservice.HARImportResult{}, err
	}
	defer done()
	if err := ctx.Err(); err != nil {
		return proxyservice.HARImportResult{}, err
	}
	if request.Path == "" || !filepath.IsAbs(request.Path) || !strings.EqualFold(filepath.Ext(request.Path), ".har") {
		return proxyservice.HARImportResult{}, errors.New("har: select a .har file using an absolute path")
	}
	source, err := os.Open(request.Path)
	if err != nil {
		return proxyservice.HARImportResult{}, fmt.Errorf("har: open file: %w", err)
	}
	defer source.Close()
	info, err := source.Stat()
	if err != nil {
		return proxyservice.HARImportResult{}, err
	}
	if !info.Mode().IsRegular() {
		return proxyservice.HARImportResult{}, errors.New("har: input must be a regular file")
	}
	if info.Size() > proxyservice.HARImportMaxFileSize {
		return proxyservice.HARImportResult{}, errors.New("har: file exceeds 512 MiB")
	}

	s.storageMu.Lock()
	defer s.storageMu.Unlock()
	if err := ctx.Err(); err != nil {
		return proxyservice.HARImportResult{}, err
	}
	directory, err := getHistoryStoragePath()
	if err != nil {
		return proxyservice.HARImportResult{}, err
	}
	alias := strings.TrimSuffix(filepath.Base(request.Path), filepath.Ext(request.Path))
	metadata := proxyservice.HistoryMetadata{Key: uuid.NewString(), Alias: alias, CreatedAt: time.Now().UnixMilli()}
	result, err := proxyservice.ImportHARHistory(ctx, source, directory, metadata)
	if err != nil || result.Metadata == nil {
		return result, err
	}
	s.mu.Lock()
	err = s.initializeIndexMapLocked(metadata.Key, filepath.Join(directory, fs.GetHIdxFileName(metadata.Key)))
	s.mu.Unlock()
	if err != nil {
		paths, pathErr := historyFilePaths(directory, metadata.Key)
		_, cleanupErr := deleteHistoryFiles(paths...)
		return proxyservice.HARImportResult{}, errors.Join(err, pathErr, cleanupErr)
	}
	return result, nil
}
