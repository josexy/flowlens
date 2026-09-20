package historyservice

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/josexy/flowlens/backend/pkg/fs"
	proxyservice "github.com/josexy/flowlens/backend/services/proxy_service"
)

type TrafficExportRequest struct {
	Key        string                         `json:"key"`
	Kind       proxyservice.TrafficExportKind `json:"kind"`
	Path       string                         `json:"path"`
	TargetType string                         `json:"targetType"`
	TrafficIDs []uint64                       `json:"trafficIds,omitempty"`
}

func (s *HistoryService) ExportTraffic(ctx context.Context, request TrafficExportRequest) (proxyservice.TrafficExportResult, error) {
	release := proxyservice.AcquireHARExport()
	defer release()
	s.storageMu.RLock()
	defer s.storageMu.RUnlock()
	s.mu.RLock()
	defer s.mu.RUnlock()
	fileIndex := s.indexMap[request.Key]
	if fileIndex == nil || fileIndex.entries == nil {
		return proxyservice.TrafficExportResult{}, fmt.Errorf("history not found: %s", request.Key)
	}
	if fileIndex.formatVersion != 1 && fileIndex.formatVersion != 2 {
		return proxyservice.TrafficExportResult{}, fmt.Errorf("hbin: unsupported version %d", fileIndex.formatVersion)
	}
	indices, err := historyHARIndices(fileIndex.entries, request.TrafficIDs)
	if err != nil {
		return proxyservice.TrafficExportResult{}, err
	}
	directory, err := getHistoryStoragePath()
	if err != nil {
		return proxyservice.TrafficExportResult{}, err
	}
	file, err := os.Open(filepath.Join(directory, fs.GetHBinFileName(request.Key)))
	if err != nil {
		return proxyservice.TrafficExportResult{}, err
	}
	defer file.Close()
	return proxyservice.WriteTrafficExport(ctx, proxyservice.TrafficExportRequest{
		Kind: request.Kind, Path: request.Path, TargetType: request.TargetType, TrafficIDs: request.TrafficIDs,
	}, len(indices), func(index int) (*proxyservice.TrafficEntry, error) {
		if _, err := file.Seek(int64(indices[index].headerIndex), io.SeekStart); err != nil {
			return nil, err
		}
		return proxyservice.DecodeTrafficEntryWithVersion(file, fileIndex.formatVersion)
	}, func(entry *proxyservice.TrafficEntry, temporaryDirectory string) (proxyservice.HARBody, proxyservice.HARBody, error) {
		index, exists := fileIndex.entries.Get(entry.ID)
		if !exists {
			return proxyservice.HARBody{}, proxyservice.HARBody{}, fmt.Errorf("traffic entry not found: %d", entry.ID)
		}
		if _, err := file.Seek(int64(index.bodyIndex), io.SeekStart); err != nil {
			return proxyservice.HARBody{}, proxyservice.HARBody{}, err
		}
		return proxyservice.DecodeTrafficHARBodyReaders(file, temporaryDirectory)
	})
}
