package proxyservice

import (
	"fmt"

	appservice "github.com/josexy/flowlens/backend/services/app_service"
)

// ExportHAR exports all current capture entries when TrafficIDs is empty, or
// the requested entries in the supplied order otherwise.
func (s *ProxyService) ExportHAR(request HARExportRequest) (HARWriteResult, error) {
	release := AcquireHARExport()
	defer release()

	entries, err := s.harExportSnapshots(request.TrafficIDs)
	if err != nil {
		return HARWriteResult{}, err
	}

	writer, err := NewHARFileWriter(request.Path, appservice.APP_VERSION)
	if err != nil {
		return HARWriteResult{}, err
	}
	defer writer.Abort()

	for _, entry := range entries {
		input := s.currentHARExportEntry(entry)
		if err := writer.WriteEntry(input); err != nil {
			return HARWriteResult{}, err
		}
	}
	return writer.Close()
}

func (s *ProxyService) harExportSnapshots(ids []uint64) ([]*TrafficEntry, error) {
	s.captureLifecycleMu.RLock()
	defer s.captureLifecycleMu.RUnlock()
	if len(ids) == 0 {
		return s.trafficEntries.Values(), nil
	}
	entries := make([]*TrafficEntry, 0, len(ids))
	seen := make(map[uint64]struct{}, len(ids))
	for _, id := range ids {
		if _, duplicate := seen[id]; duplicate {
			continue
		}
		seen[id] = struct{}{}
		entry, ok := s.trafficEntries.Get(id)
		if !ok {
			return nil, fmt.Errorf("traffic entry not found: %d", id)
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func (s *ProxyService) currentHARExportEntry(entry *TrafficEntry) HARExportEntry {
	input := HARExportEntry{Entry: entry}
	if !harSupports(entry) {
		return input
	}
	bvi, _ := s.getTrafficBodyViewInner(entry.ID)
	// Body loading is intentionally best effort. getTrafficBodyViewInner can
	// return a readable request or response together with an error for the
	// other side; preserve that partial result in the HAR.
	input.RequestBody = HARBody{
		Reader:    bvi.RequestBodyReader,
		Size:      bvi.RequestBodySize,
		Encoding:  bvi.RequestBodyEncoding,
		Available: !bvi.RequestBodyUnavailable && harCurrentBodyAvailable(entry.Request, bvi.RequestBodyReader != nil),
	}
	input.ResponseBody = HARBody{
		Reader:    bvi.ResponseBodyReader,
		Size:      bvi.ResponseBodySize,
		Encoding:  bvi.ResponseBodyEncoding,
		Available: !bvi.ResponseBodyUnavailable && harCurrentBodyAvailable(entry.Response, bvi.ResponseBodyReader != nil),
	}
	return input
}

func harCurrentBodyAvailable(message *HTTPMessage, hasReader bool) bool {
	if hasReader {
		return true
	}
	return message != nil && message.Metrics != nil &&
		message.Metrics.State == HTTPMessageStateCompleted && message.Metrics.BodySize == 0
}
