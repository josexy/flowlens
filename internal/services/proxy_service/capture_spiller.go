package proxyservice

import (
	"context"
	"fmt"

	"github.com/josexy/flowlens/internal/pkg/logger"
)

const (
	// captureMemoryWindow is how many of the newest entries a capture session
	// keeps in memory, matching the window the interface shows. Entries whose
	// exchange is still in progress stay as well.
	captureMemoryWindow = frontendTrafficEntryLimit
	// captureSpillBatch is how many settled entries one spill batch moves, and
	// how far the window may grow before the spiller wakes.
	captureSpillBatch = 256
	// captureHeldBackWarning is how many unsettled entries older than the
	// window are logged as held back.
	captureHeldBackWarning = 1000
)

// startCaptureSpiller starts the goroutine that moves settled entries beyond
// the memory window to the spill store. It stops when ctx ends.
func (s *ProxyService) startCaptureSpiller(ctx context.Context) {
	if ctx == nil || s.spillWake != nil {
		return
	}
	s.spillWake = make(chan struct{}, 1)
	s.spillDone = make(chan struct{})
	go s.runCaptureSpiller(ctx, s.spillWake, s.spillDone)
}

// waitCaptureSpiller waits for the spiller to stop after its context ended.
func (s *ProxyService) waitCaptureSpiller() {
	if s.spillDone != nil {
		<-s.spillDone
	}
}

// wakeSettledCaptureSpiller also spills a single old exchange that just
// finished. Waiting for a full batch here would retain it until new traffic.
func (s *ProxyService) wakeSettledCaptureSpiller() {
	s.wakeCaptureSpiller(captureMemoryWindow)
}

// wakeCaptureSpiller coalesces wakeups once memory exceeds limit. It never
// performs disk I/O on the producer or registration goroutine.
func (s *ProxyService) wakeCaptureSpiller(limit int) {
	if s.spillWake == nil || s.trafficEntries.Len() <= limit {
		return
	}
	select {
	case s.spillWake <- struct{}{}:
	default:
	}
}

func (s *ProxyService) runCaptureSpiller(ctx context.Context, wake <-chan struct{}, done chan<- struct{}) {
	defer close(done)
	for {
		select {
		case <-ctx.Done():
			return
		case <-wake:
			// Locks are released between batches, so traffic and user actions
			// interleave with a long run.
			for ctx.Err() == nil && s.spillCaptureBatch() {
			}
		}
	}
}

// spillCaptureBatch moves up to one batch of settled entries beyond the
// memory window to the spill store. It reports whether a full batch moved, so
// more may be waiting.
func (s *ProxyService) spillCaptureBatch() bool {
	s.clearDataMu.Lock()
	defer s.clearDataMu.Unlock()
	s.captureLifecycleMu.RLock()
	defer s.captureLifecycleMu.RUnlock()

	candidates := s.spillCandidatesLocked()
	if len(candidates) == 0 {
		return false
	}
	store := s.captureSpillStore()
	if store == nil {
		if s.spillDisabledWarned.CompareAndSwap(false, true) {
			logger.G().Warn("Body cache is unavailable; the capture keeps every entry in memory")
		}
		return false
	}
	written := make([]*TrafficEntry, 0, len(candidates))
	for _, entry := range candidates {
		bodyView, bodyErr := s.getTrafficBodyViewInner(entry.ID)
		if bodyErr != nil {
			// As in the history save, a side that cannot be read is marked
			// unavailable and the other side is kept.
			logger.G().Warnf("Spill entry %d with unavailable body: %v", entry.ID, bodyErr)
		}
		err := store.Append(entry, &bodyView)
		bodyView.closeReqBodyReaderSafely()
		bodyView.closeRspBodyReaderSafely()
		if err != nil {
			// Every new entry retries, so report a lasting failure once.
			if s.spillFailedWarned.CompareAndSwap(false, true) {
				logger.G().Warnf("Spill entry %d failed, keeping entries in memory: %v", entry.ID, err)
			}
			break
		}
		s.spillFailedWarned.Store(false)
		written = append(written, entry)
	}
	if s.spillBatchHook != nil {
		s.spillBatchHook()
	}
	s.replaceSpilledEntriesLocked(store, written)
	return len(written) == captureSpillBatch
}

// spillCandidatesLocked returns the oldest settled entries that have at least
// captureMemoryWindow newer entries, up to one batch.
func (s *ProxyService) spillCandidatesLocked() []*TrafficEntry {
	beyondWindow := s.trafficEntries.Len() - captureMemoryWindow
	if beyondWindow <= 0 {
		return nil
	}
	candidates := make([]*TrafficEntry, 0, min(beyondWindow, captureSpillBatch))
	heldBack := 0
	position := 0
	for _, entry := range s.trafficEntries.All() {
		if position >= beyondWindow || len(candidates) == captureSpillBatch {
			break
		}
		position++
		if !s.isCurrentTrafficEntryLocked(entry) {
			continue
		}
		if !entry.lifecycle.settled() {
			heldBack++
			continue
		}
		candidates = append(candidates, entry)
	}
	if heldBack > captureHeldBackWarning {
		if s.spillHeldBackWarned.CompareAndSwap(false, true) {
			logger.G().Warnf("%d capture entries beyond the memory window are still in progress", heldBack)
		}
	} else {
		s.spillHeldBackWarned.Store(false)
	}
	return candidates
}

// replaceSpilledEntriesLocked swaps written entries for their spilled copies.
// An entry that changed, was deleted, or got a new producer since it was
// written stays in memory, and its spilled copy is removed.
func (s *ProxyService) replaceSpilledEntriesLocked(store *captureSpillStore, written []*TrafficEntry) {
	moved := make([]uint64, 0, len(written))
	var offs []func()
	s.trafficAttributionMu.Lock()
	for _, entry := range written {
		current, ok := s.trafficEntries.Get(entry.ID)
		if !ok || current != entry || !s.isCurrentTrafficEntryLocked(current) || !current.lifecycle.settled() {
			store.Delete(entry.ID)
			continue
		}
		if current.lifecycle != nil {
			current.lifecycle.spilled.Store(true)
		}
		s.trafficEntries.Delete(entry.ID)
		if off := s.deactivateTrafficProcessBinding(entry.ID); off != nil {
			offs = append(offs, off)
		}
		moved = append(moved, entry.ID)
	}
	s.trafficAttributionMu.Unlock()
	for _, off := range offs {
		off()
	}
	for _, id := range moved {
		if value, ok := s.trafficBodies.LoadAndDelete(id); ok {
			abortTrafficBodies(value.(*TrafficBodies))
		}
		if value, ok := s.trafficWsMsgs.LoadAndDelete(id); ok {
			releaseTrafficWsMsgs(value.(*TrafficWsMsgs))
		}
		s.deleteTrafficBodyCacheFiles(id)
	}
}

// captureEntryRef is an entry of the current capture, held in memory (entry)
// or in the spill store (spill). Spilled entries are decoded only when used.
type captureEntryRef struct {
	id    uint64
	entry *TrafficEntry
	spill *captureSpillStore
}

// resolveCaptureEntry finds id in memory first, then in the spill store. An
// entry only ever moves from memory to the spill store, so this order cannot
// miss one that moves meanwhile.
func (s *ProxyService) resolveCaptureEntry(id uint64) (captureEntryRef, bool) {
	if entry, ok := s.trafficEntries.Get(id); ok {
		return captureEntryRef{id: id, entry: entry}, true
	}
	if store := s.captureSpillStore(); store.Has(id) {
		return captureEntryRef{id: id, spill: store}, true
	}
	return captureEntryRef{}, false
}

// movedToSpill reports whether an in-memory entry was spilled after it was
// resolved, so bodies read from memory may be incomplete.
func (r captureEntryRef) movedToSpill() bool {
	return r.entry != nil && r.entry.lifecycle != nil && r.entry.lifecycle.spilled.Load()
}

// Entry returns the entry, decoding it when it is spilled.
func (r captureEntryRef) Entry() (*TrafficEntry, error) {
	if r.entry != nil {
		return r.entry, nil
	}
	return r.spill.Entry(r.id)
}

// harBodies decodes the HAR request and response bodies of a spilled entry,
// spooling large ones under temporaryDir.
func (r captureEntryRef) harBodies(temporaryDir string) (HARBody, HARBody, error) {
	reader, err := r.spill.BodyReader(r.id)
	if err != nil {
		return HARBody{}, HARBody{}, err
	}
	return DecodeTrafficHARBodyReaders(reader, temporaryDir)
}

// captureEntryRefsLocked returns the current capture's entries in ID order:
// all of them when ids is empty, otherwise the requested ones in the order
// given. The caller holds clearDataMu, so the set cannot change while it is
// used.
func (s *ProxyService) captureEntryRefsLocked(ids []uint64) (selected, source []captureEntryRef, err error) {
	s.captureLifecycleMu.RLock()
	defer s.captureLifecycleMu.RUnlock()
	return s.captureEntryRefsCaptureLocked(ids)
}

// captureEntryRefsCaptureLocked is captureEntryRefsLocked for a caller that
// already holds either side of captureLifecycleMu.
func (s *ProxyService) captureEntryRefsCaptureLocked(ids []uint64) (selected, source []captureEntryRef, err error) {
	store := s.captureSpillStore()
	inMemory := s.trafficEntries.Values()
	spilled := store.IDs()
	source = make([]captureEntryRef, 0, len(inMemory)+len(spilled))
	for len(inMemory) > 0 || len(spilled) > 0 {
		if len(spilled) == 0 || len(inMemory) > 0 && inMemory[0].ID < spilled[0] {
			source = append(source, captureEntryRef{id: inMemory[0].ID, entry: inMemory[0]})
			inMemory = inMemory[1:]
			continue
		}
		source = append(source, captureEntryRef{id: spilled[0], spill: store})
		spilled = spilled[1:]
	}
	if len(ids) == 0 {
		return source, source, nil
	}
	byID := make(map[uint64]captureEntryRef, len(ids))
	for _, id := range ids {
		byID[id] = captureEntryRef{}
	}
	for _, ref := range source {
		if _, wanted := byID[ref.id]; wanted {
			byID[ref.id] = ref
		}
	}
	selected = make([]captureEntryRef, 0, len(ids))
	seen := make(map[uint64]struct{}, len(ids))
	for _, id := range ids {
		if _, duplicate := seen[id]; duplicate {
			continue
		}
		seen[id] = struct{}{}
		ref := byID[id]
		if ref.entry == nil && ref.spill == nil {
			return nil, nil, fmt.Errorf("traffic entry not found: %d", id)
		}
		selected = append(selected, ref)
	}
	return selected, source, nil
}
