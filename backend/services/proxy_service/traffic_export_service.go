package proxyservice

import (
	"context"
	"errors"
)

func (s *ProxyService) GetTrafficExportGeneration() uint64 {
	s.captureLifecycleMu.RLock()
	defer s.captureLifecycleMu.RUnlock()
	return s.captureGeneration
}

func (s *ProxyService) validateTrafficExportGeneration(expected *uint64) error {
	if expected != nil && *expected != s.GetTrafficExportGeneration() {
		return errors.New("export: capture changed")
	}
	return nil
}

func (s *ProxyService) ExportTraffic(ctx context.Context, request TrafficExportRequest) (TrafficExportResult, error) {
	release := AcquireHARExport()
	defer release()
	s.clearDataMu.Lock()
	defer s.clearDataMu.Unlock()
	if err := s.validateTrafficExportGeneration(request.CaptureGeneration); err != nil {
		return TrafficExportResult{}, err
	}
	entries, _, err := s.harExportSnapshots(request.TrafficIDs)
	if err != nil {
		return TrafficExportResult{}, err
	}
	return WriteTrafficExport(ctx, request, len(entries), func(index int) (*TrafficEntry, error) {
		return entries[index], nil
	}, func(entry *TrafficEntry, _ string) (HARBody, HARBody, error) {
		requestBody, responseBody := s.trafficExportBodies(entry, request.Kind)
		return requestBody, responseBody, nil
	})
}

func (s *ProxyService) trafficExportBodies(entry *TrafficEntry, kind TrafficExportKind) (HARBody, HARBody) {
	requestSide, responseSide, _, _ := kind.sides()
	var requestBody, responseBody HARBody
	bodies := &TrafficBodies{}
	if value, exists := s.trafficBodies.Load(entry.ID); exists {
		bodies = value.(*TrafficBodies)
	}
	s.bodyCacheMu.RLock()
	load := func(message *HTTPMessage, request bool) HARBody {
		reader, size, _, err := s.loadBodyBytesAsReaderNoLock(bodies, entry.ID, request, s.bodyCache)
		if err != nil {
			if reader != nil {
				_ = reader.Close()
			}
			return HARBody{}
		}
		return HARBody{Reader: reader, Size: size, Available: harCurrentBodyAvailable(message, reader != nil)}
	}
	if requestSide {
		requestBody = load(entry.Request, true)
	}
	if responseSide {
		responseBody = load(entry.Response, false)
	}
	count := 0
	for _, body := range []*HARBody{&requestBody, &responseBody} {
		if body.Reader != nil {
			count++
		}
	}
	if count == 0 {
		s.bodyCacheMu.RUnlock()
	} else {
		lease := newBodyCacheReadLease(count, s.bodyCacheMu.RUnlock)
		for _, body := range []*HARBody{&requestBody, &responseBody} {
			if body.Reader != nil {
				body.Reader = &bodyCacheReadCloser{ReadCloser: body.Reader, release: lease.releaseOne}
			}
		}
	}
	return requestBody, responseBody
}
