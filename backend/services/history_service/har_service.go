package historyservice

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/josexy/flowlens/backend/pkg/fs"
	appservice "github.com/josexy/flowlens/backend/services/app_service"
	proxyservice "github.com/josexy/flowlens/backend/services/proxy_service"
)

type HARExportRequest struct {
	Key        string   `json:"key"`
	Path       string   `json:"path"`
	TrafficIDs []uint64 `json:"trafficIds,omitempty"`
}

// ExportHAR exports a supported HBIN history.
func (s *HistoryService) ExportHAR(request HARExportRequest) (proxyservice.HARWriteResult, error) {
	release := proxyservice.AcquireHARExport()
	defer release()

	s.storageMu.RLock()
	defer s.storageMu.RUnlock()
	s.mu.RLock()
	defer s.mu.RUnlock()

	fileIndex := s.indexMap[request.Key]
	if fileIndex == nil || fileIndex.entries == nil {
		return proxyservice.HARWriteResult{}, fmt.Errorf("history not found: %s", request.Key)
	}
	indices, err := historyHARIndices(fileIndex.entries, request.TrafficIDs)
	if err != nil {
		return proxyservice.HARWriteResult{}, err
	}
	historyStorageDir, err := getHistoryStoragePath()
	if err != nil {
		return proxyservice.HARWriteResult{}, err
	}
	hbinFile, err := os.Open(filepath.Join(historyStorageDir, fs.GetHBinFileName(request.Key)))
	if err != nil {
		return proxyservice.HARWriteResult{}, err
	}
	defer hbinFile.Close()

	writer, err := proxyservice.NewHARFileWriter(request.Path, appservice.APP_VERSION)
	if err != nil {
		return proxyservice.HARWriteResult{}, err
	}
	defer writer.Abort()

	for _, index := range indices {
		if _, err := hbinFile.Seek(int64(index.headerIndex), io.SeekStart); err != nil {
			return proxyservice.HARWriteResult{}, err
		}
		entry, err := proxyservice.DecodeTrafficEntryWithVersion(hbinFile, fileIndex.formatVersion)
		if err != nil {
			return proxyservice.HARWriteResult{}, err
		}

		input := proxyservice.HARExportEntry{Entry: entry}
		if entry.Type != "tcp" {
			if _, err := hbinFile.Seek(int64(index.bodyIndex), io.SeekStart); err != nil {
				return proxyservice.HARWriteResult{}, err
			}
			input.RequestBody, input.ResponseBody, err = proxyservice.DecodeTrafficHARBodyReaders(
				hbinFile,
				filepath.Dir(request.Path),
			)
			if err != nil {
				return proxyservice.HARWriteResult{}, err
			}
		}
		if err := writer.WriteEntry(input); err != nil {
			return proxyservice.HARWriteResult{}, err
		}
	}
	return writer.Close()
}

func historyHARIndices(entries *orderedIndexList, ids []uint64) ([]historyIndex, error) {
	if len(ids) == 0 {
		indices := make([]historyIndex, 0, entries.Len())
		entries.ForEachValue(func(index historyIndex) bool {
			indices = append(indices, index)
			return true
		})
		return indices, nil
	}

	indices := make([]historyIndex, 0, len(ids))
	seen := make(map[uint64]struct{}, len(ids))
	for _, id := range ids {
		if _, duplicate := seen[id]; duplicate {
			continue
		}
		seen[id] = struct{}{}
		index, ok := entries.Get(id)
		if !ok {
			return nil, fmt.Errorf("traffic entry not found: %d", id)
		}
		indices = append(indices, index)
	}
	return indices, nil
}
