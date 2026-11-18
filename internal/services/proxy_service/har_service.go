package proxyservice

import (
	"path/filepath"

	appservice "github.com/josexy/flowlens/internal/services/app_service"
)

// ExportHAR exports all current capture entries when TrafficIDs is empty, or
// the requested entries in the supplied order otherwise.
func (s *ProxyService) ExportHAR(request HARExportRequest) (HARWriteResult, error) {
	release := AcquireHARExport()
	defer release()
	s.clearDataMu.Lock()
	defer s.clearDataMu.Unlock()
	if err := s.validateTrafficExportGeneration(request.CaptureGeneration); err != nil {
		return HARWriteResult{}, err
	}

	refs, sourceRefs, err := s.harExportSnapshots(request.TrafficIDs)
	if err != nil {
		return HARWriteResult{}, err
	}

	writer, err := NewHARFileWriter(request.Path, appservice.Version())
	if err != nil {
		return HARWriteResult{}, err
	}
	defer writer.Abort()

	// Register the full source, including unselected entries, so a reused
	// connection is not charged again. Spilled entries are decoded one at a
	// time, headers only.
	for _, ref := range sourceRefs {
		entry, err := ref.Entry()
		if err != nil {
			return HARWriteResult{}, err
		}
		writer.ObserveConnectionTimings(entry)
	}
	for _, ref := range refs {
		input, err := s.captureHARExportEntry(ref, filepath.Dir(request.Path))
		if err != nil {
			return HARWriteResult{}, err
		}
		if err := writer.WriteEntry(input); err != nil {
			return HARWriteResult{}, err
		}
	}
	return writer.Close()
}

// harExportSnapshots returns the entries to export and the whole capture, in
// memory or spilled, in ID order.
func (s *ProxyService) harExportSnapshots(ids []uint64) ([]captureEntryRef, []captureEntryRef, error) {
	return s.captureEntryRefsLocked(ids)
}

// captureHARExportEntry prepares one entry of the current capture for the HAR
// writer, decoding a spilled entry and its bodies only now.
func (s *ProxyService) captureHARExportEntry(ref captureEntryRef, temporaryDir string) (HARExportEntry, error) {
	if ref.entry != nil {
		return s.currentHARExportEntry(ref.entry), nil
	}
	entry, err := ref.Entry()
	if err != nil {
		return HARExportEntry{}, err
	}
	input := HARExportEntry{Entry: entry}
	if !harSupports(entry) {
		return input, nil
	}
	input.RequestBody, input.ResponseBody, err = ref.harBodies(temporaryDir)
	return input, err
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
		Encoding:  harEncodingForBinary(bvi.RequestBodyBinary),
		Available: !bvi.RequestBodyUnavailable && harCurrentBodyAvailable(entry.Request, bvi.RequestBodyReader != nil),
	}
	input.ResponseBody = HARBody{
		Reader:    bvi.ResponseBodyReader,
		Size:      bvi.ResponseBodySize,
		Encoding:  harEncodingForBinary(bvi.ResponseBodyBinary),
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
