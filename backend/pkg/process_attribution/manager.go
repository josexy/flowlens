package processattribution

import (
	"container/list"
	"context"
	"image"
	"sync"
	"time"
)

type Manager struct {
	provider  Provider
	iconStore *IconStore
	options   Options

	ctx         context.Context
	cancel      context.CancelFunc
	lookupSlots chan struct{}
	iconSlots   chan struct{}

	lookupQueue chan *lookupState
	iconQueue   chan *iconLoadGroup
	workers     sync.WaitGroup
	active      sync.WaitGroup
	closeOnce   sync.Once

	mu           sync.Mutex
	closed       bool
	draining     bool
	inFlight     map[EndpointTuple]*lookupState
	tupleCache   map[EndpointTuple]tupleCacheEntry
	tupleLimit   int
	processCache map[processCacheKey]*processCacheEntry
	processLRU   *list.List
	iconInFlight map[processCacheKey]*iconLoadGroup
}

type Lookup struct {
	state *lookupState

	mu            sync.Mutex
	released      bool
	subscriptions map[uint64]struct{}
}

type lookupState struct {
	manager *Manager
	tuple   EndpointTuple
	parent  context.Context

	mu          sync.RWMutex
	result      Result
	final       bool
	nextID      uint64
	subscribers map[uint64]func(Result)
	refs        int
}

type iconWaiter struct {
	state  *lookupState
	result Result
}

type iconLoadGroup struct {
	key     processCacheKey
	keyed   bool
	result  Result
	waiters []iconWaiter
}

type tupleCacheEntry struct {
	result    Result
	expiresAt time.Time
}

type processCacheKey struct {
	pid        uint32
	startToken string
}

type processCacheEntry struct {
	result    Result
	expiresAt time.Time
	element   *list.Element
}

const minimumTupleCacheSize = 256

func NewManager(provider Provider, iconStore *IconStore, options Options) *Manager {
	options = normalizeOptions(options)
	if provider == nil {
		provider = &unavailableProvider{reason: "provider_unavailable"}
	}
	ctx, cancel := context.WithCancel(context.Background())
	manager := &Manager{
		provider:     provider,
		iconStore:    iconStore,
		options:      options,
		ctx:          ctx,
		cancel:       cancel,
		lookupSlots:  make(chan struct{}, options.Workers),
		iconSlots:    make(chan struct{}, options.IconWorkers),
		lookupQueue:  make(chan *lookupState, options.QueueSize),
		iconQueue:    make(chan *iconLoadGroup, options.IconQueueSize),
		inFlight:     make(map[EndpointTuple]*lookupState),
		tupleCache:   make(map[EndpointTuple]tupleCacheEntry),
		tupleLimit:   max(minimumTupleCacheSize, options.QueueSize+options.Workers),
		processCache: make(map[processCacheKey]*processCacheEntry),
		processLRU:   list.New(),
		iconInFlight: make(map[processCacheKey]*iconLoadGroup),
	}
	for range options.Workers {
		manager.workers.Add(1)
		go manager.runLookupWorker()
	}
	if iconStore != nil {
		for range options.IconWorkers {
			manager.workers.Add(1)
			go manager.runIconWorker()
		}
	}
	return manager
}

func (m *Manager) Start(ctx context.Context, tuple EndpointTuple) *Lookup {
	if ctx == nil {
		ctx = context.Background()
	}
	tuple = normalizeEndpointTuple(tuple)
	now := time.Now()

	m.mu.Lock()
	if m.closed || m.draining {
		m.mu.Unlock()
		return NewCompletedLookup(Result{Status: StatusNotFound, Reason: "manager_closed"})
	}
	if state, ok := m.inFlight[tuple]; ok {
		state.refs++
		m.mu.Unlock()
		return newLookup(state)
	}
	if cached, ok := m.cachedTupleLocked(tuple, now); ok {
		m.mu.Unlock()
		return NewCompletedLookup(cached)
	}

	state := &lookupState{
		manager:     m,
		tuple:       tuple,
		parent:      ctx,
		result:      Result{Status: StatusPending},
		subscribers: make(map[uint64]func(Result)),
		refs:        1,
	}
	m.inFlight[tuple] = state
	m.active.Add(1)
	select {
	case m.lookupQueue <- state:
		m.mu.Unlock()
		return newLookup(state)
	default:
		delete(m.inFlight, tuple)
		m.active.Done()
		m.mu.Unlock()
		state.complete(Result{Status: StatusNotFound, Reason: "queue_full"}, true)
		return newLookup(state)
	}
}

func NewCompletedLookup(result Result) *Lookup {
	state := &lookupState{
		result:      result,
		final:       true,
		subscribers: make(map[uint64]func(Result)),
		refs:        1,
	}
	return newLookup(state)
}

func (m *Manager) Drain(ctx context.Context) {
	if m == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	m.mu.Lock()
	m.draining = true
	m.mu.Unlock()
	done := make(chan struct{})
	go func() {
		m.active.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
	}
}

func (m *Manager) Close() {
	if m == nil {
		return
	}
	m.closeOnce.Do(func() {
		m.mu.Lock()
		m.closed = true
		m.draining = true
		m.mu.Unlock()
		m.cancel()
		m.workers.Wait()
		m.active.Wait()
	})
}

func (l *Lookup) Snapshot() Result {
	if l == nil || l.state == nil {
		return Result{Status: StatusUnsupported, Reason: "lookup_unavailable"}
	}
	return l.state.snapshot()
}

func (l *Lookup) InvalidateIcon() {
	if l == nil || l.state == nil {
		return
	}
	l.state.invalidateIcon()
}

// RestoreIcon rebuilds a missing disk entry from cached process identity.
// Registering the recovery with active makes Drain and Close wait for it, while
// the manager context cancels it before an explicit cache reset removes files.
func (m *Manager) RestoreIcon(ctx context.Context, iconKey string, fallback Result) (string, error) {
	if m == nil || m.iconStore == nil {
		return "", &IconUnavailableError{Reason: "icon_store_unavailable"}
	}
	if ctx == nil {
		ctx = context.Background()
	}

	m.mu.Lock()
	if m.closed || m.draining {
		m.mu.Unlock()
		return "", &IconUnavailableError{Reason: "manager_closed"}
	}
	result, found := m.cachedResultForIconKeyLocked(iconKey, time.Now())
	if !found && fallback.Status == StatusResolved && fallback.IconKey == iconKey {
		result = fallback
		found = true
	}
	if !found {
		m.mu.Unlock()
		return "", &IconUnavailableError{Reason: "icon_identity_unavailable"}
	}
	m.active.Add(1)
	m.mu.Unlock()
	defer m.active.Done()

	restoreCtx, cancel := context.WithTimeout(m.ctx, m.options.LookupTimeout)
	defer cancel()
	select {
	case m.iconSlots <- struct{}{}:
	case <-ctx.Done():
		return "", &IconUnavailableError{Reason: "icon_restore_cancelled"}
	case <-restoreCtx.Done():
		return "", &IconUnavailableError{Reason: "icon_restore_timeout"}
	}
	loaded := make(chan iconLoadResult, 1)
	go func() {
		defer func() { <-m.iconSlots }()
		icon, err := m.provider.LoadIcon(restoreCtx, result)
		loaded <- iconLoadResult{icon: icon, err: err}
	}()
	select {
	case loadedResult := <-loaded:
		if loadedResult.err != nil {
			return "", loadedResult.err
		}
		if loadedResult.icon == nil {
			return "", &IconUnavailableError{Reason: "icon_source_unavailable"}
		}
		key, err := m.iconStore.Put(loadedResult.icon)
		if err != nil {
			return "", err
		}
		result.IconKey = key
		m.storeProcessResult(result)
		return key, nil
	case <-ctx.Done():
		return "", &IconUnavailableError{Reason: "icon_restore_cancelled"}
	case <-restoreCtx.Done():
		return "", &IconUnavailableError{Reason: "icon_restore_timeout"}
	}
}

func (l *Lookup) Retain() *Lookup {
	if l == nil || l.state == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.released {
		return nil
	}
	if l.state.manager != nil {
		l.state.manager.retain(l.state)
	}
	return newLookup(l.state)
}

func (l *Lookup) Subscribe(callback func(Result)) func() {
	if l == nil || l.state == nil || callback == nil {
		return func() {}
	}

	l.mu.Lock()
	if l.released {
		l.mu.Unlock()
		return func() {}
	}
	id, ok := l.state.subscribe(callback)
	if !ok {
		l.mu.Unlock()
		return func() {}
	}
	l.subscriptions[id] = struct{}{}
	l.mu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			l.mu.Lock()
			delete(l.subscriptions, id)
			l.mu.Unlock()
			l.state.unsubscribe(id)
		})
	}
}

func (l *Lookup) Release() {
	if l == nil || l.state == nil {
		return
	}
	l.mu.Lock()
	if l.released {
		l.mu.Unlock()
		return
	}
	l.released = true
	ids := make([]uint64, 0, len(l.subscriptions))
	for id := range l.subscriptions {
		ids = append(ids, id)
	}
	clear(l.subscriptions)
	l.mu.Unlock()
	for _, id := range ids {
		l.state.unsubscribe(id)
	}
	if l.state.manager != nil {
		l.state.manager.release(l.state)
	}
}

func normalizeOptions(options Options) Options {
	defaults := DefaultOptions()
	if options.Workers <= 0 {
		options.Workers = defaults.Workers
	}
	if options.QueueSize <= 0 {
		options.QueueSize = defaults.QueueSize
	}
	if options.LookupTimeout <= 0 {
		options.LookupTimeout = defaults.LookupTimeout
	}
	if options.SocketSnapshotTTL <= 0 {
		options.SocketSnapshotTTL = defaults.SocketSnapshotTTL
	}
	if options.ProcessCacheSize <= 0 {
		options.ProcessCacheSize = defaults.ProcessCacheSize
	}
	if options.ProcessCacheTTL <= 0 {
		options.ProcessCacheTTL = defaults.ProcessCacheTTL
	}
	if options.NegativeCacheTTL <= 0 {
		options.NegativeCacheTTL = defaults.NegativeCacheTTL
	}
	if options.IconWorkers <= 0 {
		options.IconWorkers = defaults.IconWorkers
	}
	if options.IconQueueSize <= 0 {
		options.IconQueueSize = defaults.IconQueueSize
	}
	return options
}

func newLookup(state *lookupState) *Lookup {
	return &Lookup{state: state, subscriptions: make(map[uint64]struct{})}
}

func (m *Manager) runLookupWorker() {
	defer m.workers.Done()
	for {
		select {
		case state := <-m.lookupQueue:
			m.processLookup(state)
		case <-m.ctx.Done():
			m.rejectQueuedLookups()
			return
		}
	}
}

func (m *Manager) rejectQueuedLookups() {
	for {
		select {
		case state := <-m.lookupQueue:
			m.finishLookup(state, Result{Status: StatusNotFound, Reason: "manager_closed"}, true)
		default:
			return
		}
	}
}

func (m *Manager) processLookup(state *lookupState) {
	result := m.callLookup(state)
	if result.Status == "" || result.Status == StatusPending {
		result = Result{Status: StatusNotFound, Reason: "provider_invalid_result"}
	}
	if result.Status == StatusResolved {
		result = m.mergeProcessCache(result)
	}
	m.storeTupleResult(state.tuple, result)

	if result.Status != StatusResolved {
		m.finishLookup(state, result, true)
		return
	}
	state.publish(result)
	if result.IconKey != "" || m.iconStore == nil {
		m.finishLookup(state, result, false)
		return
	}
	m.scheduleIcon(state, result)
}

func (m *Manager) callLookup(state *lookupState) Result {
	ctx, cancel := context.WithTimeout(m.ctx, m.options.LookupTimeout)
	defer cancel()
	select {
	case m.lookupSlots <- struct{}{}:
	case <-state.parent.Done():
		return Result{Status: StatusNotFound, Reason: "lookup_cancelled"}
	case <-ctx.Done():
		return m.lookupContextResult(ctx, state.parent)
	}
	results := make(chan Result, 1)
	go func() {
		defer func() { <-m.lookupSlots }()
		results <- m.provider.Lookup(ctx, state.tuple)
	}()
	select {
	case result := <-results:
		return result
	case <-state.parent.Done():
		return Result{Status: StatusNotFound, Reason: "lookup_cancelled"}
	case <-ctx.Done():
		return m.lookupContextResult(ctx, state.parent)
	}
}

func (m *Manager) lookupContextResult(ctx, parent context.Context) Result {
	if m.ctx.Err() != nil {
		return Result{Status: StatusNotFound, Reason: "manager_closed"}
	}
	if parent != nil && parent.Err() != nil {
		return Result{Status: StatusNotFound, Reason: "lookup_cancelled"}
	}
	if ctx.Err() == context.DeadlineExceeded {
		return Result{Status: StatusNotFound, Reason: "lookup_timeout"}
	}
	return Result{Status: StatusNotFound, Reason: "lookup_cancelled"}
}

func (m *Manager) runIconWorker() {
	defer m.workers.Done()
	for {
		select {
		case group := <-m.iconQueue:
			m.processIcon(group)
		case <-m.ctx.Done():
			m.rejectQueuedIcons()
			return
		}
	}
}

func (m *Manager) rejectQueuedIcons() {
	for {
		select {
		case group := <-m.iconQueue:
			m.finishIconGroup(group, "")
		default:
			return
		}
	}
}

func (m *Manager) processIcon(group *iconLoadGroup) {
	ctx, cancel := context.WithTimeout(m.ctx, m.options.LookupTimeout)
	defer cancel()
	select {
	case m.iconSlots <- struct{}{}:
	case <-ctx.Done():
		m.finishIconGroup(group, "")
		return
	}
	loaded := make(chan iconLoadResult, 1)
	go func() {
		defer func() { <-m.iconSlots }()
		icon, err := m.provider.LoadIcon(ctx, group.result)
		loaded <- iconLoadResult{icon: icon, err: err}
	}()
	select {
	case result := <-loaded:
		if result.err != nil || result.icon == nil {
			m.finishIconGroup(group, "")
			return
		}
		key, err := m.iconStore.Put(result.icon)
		if err != nil {
			m.finishIconGroup(group, "")
			return
		}
		m.finishIconGroup(group, key)
	case <-ctx.Done():
		m.finishIconGroup(group, "")
	}
}

type iconLoadResult struct {
	icon image.Image
	err  error
}

func (m *Manager) finishLookup(state *lookupState, result Result, publish bool) {
	if publish {
		state.complete(result, true)
	} else {
		state.complete(result, false)
	}
	m.mu.Lock()
	if m.inFlight[state.tuple] == state {
		delete(m.inFlight, state.tuple)
	}
	m.mu.Unlock()
	m.active.Done()
}

func (m *Manager) release(state *lookupState) {
	m.mu.Lock()
	if state.refs > 0 {
		state.refs--
	}
	m.mu.Unlock()
}

func (m *Manager) retain(state *lookupState) {
	m.mu.Lock()
	state.refs++
	m.mu.Unlock()
}

func (m *Manager) scheduleIcon(state *lookupState, result Result) {
	key := processCacheKey{pid: result.PID, startToken: result.StartToken}
	keyed := result.PID != 0 && result.StartToken != ""
	waiter := iconWaiter{state: state, result: result}

	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		m.finishLookup(state, result, false)
		return
	}
	if keyed {
		if iconKey, ok := m.cachedProcessIconLocked(key, time.Now()); ok {
			result.IconKey = iconKey
			m.mu.Unlock()
			m.storeTupleResult(state.tuple, result)
			m.finishLookup(state, result, true)
			return
		}
		if group := m.iconInFlight[key]; group != nil {
			group.waiters = append(group.waiters, waiter)
			m.mu.Unlock()
			return
		}
	}

	group := &iconLoadGroup{
		key:     key,
		keyed:   keyed,
		result:  result,
		waiters: []iconWaiter{waiter},
	}
	if keyed {
		m.iconInFlight[key] = group
	}
	select {
	case m.iconQueue <- group:
		m.mu.Unlock()
		return
	default:
		if keyed && m.iconInFlight[key] == group {
			delete(m.iconInFlight, key)
		}
		m.mu.Unlock()
		m.finishLookup(state, result, false)
	}
}

func (m *Manager) cachedProcessIconLocked(key processCacheKey, now time.Time) (string, bool) {
	cached, ok := m.processCache[key]
	if !ok {
		return "", false
	}
	if !now.Before(cached.expiresAt) {
		m.removeProcessCacheEntryLocked(key, cached)
		return "", false
	}
	if cached.result.IconKey == "" {
		return "", false
	}
	m.processLRU.MoveToFront(cached.element)
	return cached.result.IconKey, true
}

func (m *Manager) cachedResultForIconKeyLocked(iconKey string, now time.Time) (Result, bool) {
	for key, cached := range m.processCache {
		if !now.Before(cached.expiresAt) {
			m.removeProcessCacheEntryLocked(key, cached)
			continue
		}
		if cached.result.IconKey == iconKey {
			m.processLRU.MoveToFront(cached.element)
			return cached.result, true
		}
	}
	return Result{}, false
}

func (m *Manager) finishIconGroup(group *iconLoadGroup, iconKey string) {
	if group == nil {
		return
	}
	if iconKey != "" {
		result := group.result
		result.IconKey = iconKey
		m.storeProcessResult(result)
	}

	m.mu.Lock()
	if group.keyed && m.iconInFlight[group.key] == group {
		delete(m.iconInFlight, group.key)
	}
	waiters := group.waiters
	group.waiters = nil
	m.mu.Unlock()

	for _, waiter := range waiters {
		result := waiter.result
		if iconKey == "" {
			m.finishLookup(waiter.state, result, false)
			continue
		}
		result.IconKey = iconKey
		m.storeTupleResult(waiter.state.tuple, result)
		m.finishLookup(waiter.state, result, true)
	}
}

func (m *Manager) cachedTupleLocked(tuple EndpointTuple, now time.Time) (Result, bool) {
	entry, ok := m.tupleCache[tuple]
	if !ok {
		return Result{}, false
	}
	if !now.Before(entry.expiresAt) {
		delete(m.tupleCache, tuple)
		return Result{}, false
	}
	return entry.result, true
}

func (m *Manager) storeTupleResult(tuple EndpointTuple, result Result) {
	ttl := m.options.SocketSnapshotTTL
	if result.Status != StatusResolved {
		ttl = m.options.NegativeCacheTTL
	}
	now := time.Now()
	m.mu.Lock()
	if _, exists := m.tupleCache[tuple]; !exists && len(m.tupleCache) >= m.tupleLimit {
		m.pruneTupleCacheLocked(now)
	}
	if _, exists := m.tupleCache[tuple]; !exists && len(m.tupleCache) >= m.tupleLimit {
		m.evictEarliestTupleLocked()
	}
	m.tupleCache[tuple] = tupleCacheEntry{result: result, expiresAt: now.Add(ttl)}
	m.mu.Unlock()
}

func (m *Manager) pruneTupleCacheLocked(now time.Time) {
	for tuple, entry := range m.tupleCache {
		if !now.Before(entry.expiresAt) {
			delete(m.tupleCache, tuple)
		}
	}
}

func (m *Manager) evictEarliestTupleLocked() {
	var earliestTuple EndpointTuple
	var earliestExpiry time.Time
	found := false
	for tuple, entry := range m.tupleCache {
		if !found || entry.expiresAt.Before(earliestExpiry) {
			earliestTuple = tuple
			earliestExpiry = entry.expiresAt
			found = true
		}
	}
	if found {
		delete(m.tupleCache, earliestTuple)
	}
}

func (m *Manager) mergeProcessCache(result Result) Result {
	if result.PID == 0 || result.StartToken == "" {
		return result
	}
	key := processCacheKey{pid: result.PID, startToken: result.StartToken}
	now := time.Now()
	m.mu.Lock()
	if cached, ok := m.processCache[key]; ok {
		if now.Before(cached.expiresAt) {
			m.processLRU.MoveToFront(cached.element)
			result = mergeCachedProcessResult(result, cached.result)
		} else {
			m.removeProcessCacheEntryLocked(key, cached)
		}
	}
	m.storeProcessResultLocked(result, now)
	m.mu.Unlock()
	return result
}

func (m *Manager) storeProcessResult(result Result) {
	if result.Status != StatusResolved || result.PID == 0 || result.StartToken == "" {
		return
	}
	m.mu.Lock()
	m.storeProcessResultLocked(result, time.Now())
	m.mu.Unlock()
}

func (m *Manager) storeProcessResultLocked(result Result, now time.Time) {
	key := processCacheKey{pid: result.PID, startToken: result.StartToken}
	if existing, ok := m.processCache[key]; ok {
		existing.result = result
		existing.expiresAt = now.Add(m.options.ProcessCacheTTL)
		m.processLRU.MoveToFront(existing.element)
		return
	}
	element := m.processLRU.PushFront(key)
	m.processCache[key] = &processCacheEntry{
		result:    result,
		expiresAt: now.Add(m.options.ProcessCacheTTL),
		element:   element,
	}
	for len(m.processCache) > m.options.ProcessCacheSize {
		oldest := m.processLRU.Back()
		if oldest == nil {
			break
		}
		oldestKey := oldest.Value.(processCacheKey)
		m.removeProcessCacheEntryLocked(oldestKey, m.processCache[oldestKey])
	}
}

func (m *Manager) removeProcessCacheEntryLocked(key processCacheKey, entry *processCacheEntry) {
	delete(m.processCache, key)
	if entry != nil && entry.element != nil {
		m.processLRU.Remove(entry.element)
	}
}

func mergeCachedProcessResult(result, cached Result) Result {
	if result.DisplayName == "" {
		result.DisplayName = cached.DisplayName
	}
	if result.ProcessName == "" {
		result.ProcessName = cached.ProcessName
	}
	if result.ExecutablePath == "" {
		result.ExecutablePath = cached.ExecutablePath
	}
	if result.AppID == "" {
		result.AppID = cached.AppID
	}
	if result.IconKey == "" {
		result.IconKey = cached.IconKey
	}
	if result.IdentityConfidence == "" || result.IdentityConfidence == "none" {
		result.IdentityConfidence = cached.IdentityConfidence
	}
	if result.Reason == "" {
		result.Reason = cached.Reason
	}
	return result
}

func (s *lookupState) snapshot() Result {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.result
}

func (s *lookupState) invalidateIcon() {
	s.mu.Lock()
	s.result.IconKey = ""
	s.mu.Unlock()
}

func (s *lookupState) subscribe(callback func(Result)) (uint64, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.final {
		return 0, false
	}
	s.nextID++
	s.subscribers[s.nextID] = callback
	return s.nextID, true
}

func (s *lookupState) unsubscribe(id uint64) {
	s.mu.Lock()
	delete(s.subscribers, id)
	s.mu.Unlock()
}

func (s *lookupState) publish(result Result) {
	callbacks := s.update(result, false)
	callSubscribers(callbacks, result)
}

func (s *lookupState) complete(result Result, publish bool) {
	callbacks := s.update(result, true)
	if publish {
		callSubscribers(callbacks, result)
	}
}

func (s *lookupState) update(result Result, final bool) []func(Result) {
	s.mu.Lock()
	if s.final {
		s.mu.Unlock()
		return nil
	}
	s.result = result
	callbacks := make([]func(Result), 0, len(s.subscribers))
	for _, callback := range s.subscribers {
		callbacks = append(callbacks, callback)
	}
	if final {
		s.final = true
		clear(s.subscribers)
	}
	s.mu.Unlock()
	return callbacks
}

func callSubscribers(callbacks []func(Result), result Result) {
	for _, callback := range callbacks {
		callback(result)
	}
}
