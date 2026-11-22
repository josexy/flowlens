package proxyservice

import "errors"

// SetRecordingPaused changes admission only. Exchanges that already have an
// entry keep recording to completion; skipped exchanges never acquire an entry.
func (s *ProxyService) SetRecordingPaused(paused bool) (ProxyStatus, error) {
	s.lifecycleOpsMu.Lock()
	defer s.lifecycleOpsMu.Unlock()
	s.trafficPublishMu.Lock()
	s.mu.Lock()
	if !s.running {
		status := s.currentStatusLocked()
		s.mu.Unlock()
		s.trafficPublishMu.Unlock()
		return status, errors.New("proxy is not running")
	}
	if s.recordingPaused.Swap(paused) != paused {
		s.statusRevision++
	}
	status := s.currentStatusLocked()
	s.mu.Unlock()
	s.trafficPublishMu.Unlock()
	if s.events != nil {
		_ = s.events.Emit(statusEventName, status)
	}
	return status, nil
}

// Restart applies listener configuration without briefly admitting traffic
// while recording was paused. Explicit Start instead always enables recording.
func (s *ProxyService) Restart() (ProxyStatus, error) {
	s.lifecycleOpsMu.Lock()
	defer s.lifecycleOpsMu.Unlock()
	paused := s.recordingPaused.Load()
	if _, err := s.stopLocked(); err != nil {
		return s.GetStatus(), err
	}
	status, err := s.startLocked(paused)
	if err != nil {
		return s.GetStatus(), err
	}
	return status, nil
}
