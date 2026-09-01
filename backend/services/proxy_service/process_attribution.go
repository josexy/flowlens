package proxyservice

import (
	"encoding/base64"
	"errors"
	"net"
	"net/netip"
	"sync"
	"time"

	"github.com/josexy/flowlens/backend/pkg/fs"
	"github.com/josexy/flowlens/backend/pkg/logger"
	processattribution "github.com/josexy/flowlens/backend/pkg/process_attribution"
	settingservice "github.com/josexy/flowlens/backend/services/setting_service"
)

type attributedConn struct {
	net.Conn
	service *ProxyService
	tuple   processattribution.EndpointTuple
	binding *connectionProcessLookup
	once    sync.Once
}

type attributedListener struct {
	net.Listener
	service *ProxyService
}

type connectionProcessLookup struct {
	lookup *processattribution.Lookup
}

type trafficProcessBinding struct {
	mu     sync.RWMutex
	latest *ProcessInfo
	active bool
	off    func()
}

const localProcessAddressMissRefreshInterval = 5 * time.Second

func (c *attributedConn) Close() error {
	err := c.Conn.Close()
	c.once.Do(func() {
		if c.service != nil && c.binding != nil {
			c.service.unregisterConnectionProcessLookup(c.tuple, c.binding)
		}
		if c.binding == nil || c.binding.lookup == nil {
			return
		}
		time.AfterFunc(time.Second, c.binding.lookup.Release)
	})
	return err
}

func (l *attributedListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return l.service.attributeConnection(conn), nil
}

func (s *ProxyService) attributeConnection(conn net.Conn) net.Conn {
	if conn == nil {
		return conn
	}
	tuple := processAttributionTupleFromConn(conn)
	s.processAttributionMu.Lock()
	lookup := s.startEndpointAttributionLocked(tuple)
	if lookup == nil {
		s.processAttributionMu.Unlock()
		return conn
	}
	if !tuple.Client.IsValid() || !tuple.Proxy.IsValid() {
		s.processAttributionMu.Unlock()
		lookup.Release()
		return conn
	}
	binding := s.registerConnectionProcessLookupLocked(tuple, lookup)
	s.processAttributionMu.Unlock()
	return &attributedConn{
		Conn:    conn,
		service: s,
		tuple:   tuple,
		binding: binding,
	}
}

func (s *ProxyService) startEndpointAttributionLocked(tuple processattribution.EndpointTuple) *processattribution.Lookup {
	enabled := true
	if s.settingService != nil {
		loaded, err := settingservice.ProcessAttributionEnabled(s.settingService)
		if err != nil {
			logger.G().Warnf("Process attribution setting unavailable, using enabled default: %v", err)
		} else {
			enabled = loaded
		}
	}
	if !enabled {
		return nil
	}

	if !tuple.Client.IsValid() || !tuple.Proxy.IsValid() {
		return processattribution.NewCompletedLookup(processattribution.Result{
			Status: processattribution.StatusNotFound,
			Reason: "invalid_endpoint",
		})
	}
	if !s.isLocalProcessAddress(tuple.Client.Addr()) {
		return processattribution.NewCompletedLookup(processattribution.Result{
			Status: processattribution.StatusRemote,
			Reason: "remote_client",
		})
	}

	manager := s.ensureProcessAttributionManagerLocked()
	if manager == nil {
		return processattribution.NewCompletedLookup(processattribution.Result{
			Status: processattribution.StatusUnsupported,
			Reason: "manager_unavailable",
		})
	}
	return manager.Start(s.appContext(), tuple)
}

func (s *ProxyService) registerConnectionProcessLookup(
	tuple processattribution.EndpointTuple,
	lookup *processattribution.Lookup,
) *connectionProcessLookup {
	s.processAttributionMu.Lock()
	defer s.processAttributionMu.Unlock()
	return s.registerConnectionProcessLookupLocked(tuple, lookup)
}

func (s *ProxyService) registerConnectionProcessLookupLocked(
	tuple processattribution.EndpointTuple,
	lookup *processattribution.Lookup,
) *connectionProcessLookup {
	if s == nil || lookup == nil || !tuple.Client.IsValid() || !tuple.Proxy.IsValid() {
		return nil
	}
	binding := &connectionProcessLookup{lookup: lookup}
	s.connectionProcessLookups.Store(tuple, binding)
	return binding
}

func (s *ProxyService) unregisterConnectionProcessLookup(
	tuple processattribution.EndpointTuple,
	binding *connectionProcessLookup,
) {
	if s == nil || binding == nil {
		return
	}
	s.processAttributionMu.Lock()
	defer s.processAttributionMu.Unlock()
	s.connectionProcessLookups.CompareAndDelete(tuple, binding)
}

func (s *ProxyService) fillEntryProcessFromTuple(
	tuple processattribution.EndpointTuple,
	entry *TrafficEntry,
) {
	if s == nil || entry == nil || !tuple.Client.IsValid() || !tuple.Proxy.IsValid() {
		return
	}
	s.captureLifecycleMu.RLock()
	defer s.captureLifecycleMu.RUnlock()
	if !s.isCurrentTrafficEntryLocked(entry) {
		return
	}
	s.processAttributionMu.Lock()
	value, ok := s.connectionProcessLookups.Load(tuple)
	if !ok {
		s.processAttributionMu.Unlock()
		return
	}
	connection, ok := value.(*connectionProcessLookup)
	if !ok || connection == nil || connection.lookup == nil {
		s.processAttributionMu.Unlock()
		return
	}
	lookup := connection.lookup.Retain()
	s.processAttributionMu.Unlock()
	if lookup == nil {
		return
	}
	if entry.Metadata == nil {
		entry.Metadata = &Metadata{}
	}

	binding := &trafficProcessBinding{
		latest: mapProcessAttributionResult(lookup.Snapshot()),
		active: true,
	}
	actual, loaded := s.trafficProcessBindings.LoadOrStore(entry.ID, binding)
	if loaded {
		lookup.Release()
		binding = actual.(*trafficProcessBinding)
	} else {
		off := lookup.Subscribe(func(result processattribution.Result) {
			process := mapProcessAttributionResult(result)
			if binding.update(process) {
				s.updateTrafficProcess(entry.ID, binding)
			}
		})
		cleanup := func() {
			off()
			lookup.Release()
		}
		if !binding.installOff(cleanup) {
			cleanup()
		}
		binding.update(mapProcessAttributionResult(lookup.Snapshot()))
	}
	entry.Metadata.Process = binding.snapshot()
}

func processAttributionTupleFromConn(conn net.Conn) processattribution.EndpointTuple {
	if conn == nil {
		return processattribution.EndpointTuple{}
	}
	return processattribution.EndpointTuple{
		Client: normalizedAddrPort(conn.RemoteAddr()),
		Proxy:  normalizedAddrPort(conn.LocalAddr()),
	}
}

func processAttributionTuple(client, proxy netip.AddrPort) processattribution.EndpointTuple {
	return processattribution.EndpointTuple{
		Client: normalizeProcessAddrPort(client),
		Proxy:  normalizeProcessAddrPort(proxy),
	}
}

func normalizeProcessAddrPort(address netip.AddrPort) netip.AddrPort {
	if !address.IsValid() {
		return address
	}
	return netip.AddrPortFrom(normalizeProcessAddress(address.Addr()), address.Port())
}

func (s *ProxyService) updateTrafficProcess(id uint64, binding *trafficProcessBinding) {
	s.trafficPublishMu.Lock()
	defer s.trafficPublishMu.Unlock()
	s.captureLifecycleMu.RLock()
	s.trafficAttributionMu.Lock()
	currentBinding, ok := s.trafficProcessBindings.Load(id)
	if !ok || currentBinding != binding || !binding.isActive() {
		s.trafficAttributionMu.Unlock()
		s.captureLifecycleMu.RUnlock()
		return
	}
	current, ok := s.trafficEntries.Get(id)
	if !ok || !s.isCurrentTrafficEntryLocked(current) {
		s.trafficAttributionMu.Unlock()
		s.captureLifecycleMu.RUnlock()
		return
	}
	updated := cloneTrafficEntryForProcess(current)
	if updated.Metadata == nil {
		updated.Metadata = &Metadata{}
	}
	updated.Metadata.Process = binding.snapshot()
	updated.Revision = current.Revision + 1
	s.trafficEntries.Set(id, updated)
	s.trafficAttributionMu.Unlock()

	s.markHistoryDirty()
	s.captureLifecycleMu.RUnlock()
	s.emitTrafficPatch(updated, newTrafficProcessPatch(updated))
}

func (s *ProxyService) deactivateTrafficProcessBinding(id uint64) func() {
	value, ok := s.trafficProcessBindings.LoadAndDelete(id)
	if !ok {
		return nil
	}
	return value.(*trafficProcessBinding).deactivate()
}

func (s *ProxyService) deactivateAllTrafficProcessBindings() []func() {
	offs := make([]func(), 0)
	s.trafficProcessBindings.Range(func(key, value any) bool {
		id, ok := key.(uint64)
		if !ok {
			return true
		}
		if off := s.deactivateTrafficProcessBinding(id); off != nil {
			offs = append(offs, off)
		}
		return true
	})
	return offs
}

func (s *ProxyService) processBindingSnapshot(id uint64) (*ProcessInfo, bool) {
	value, ok := s.trafficProcessBindings.Load(id)
	if !ok {
		return nil, false
	}
	binding := value.(*trafficProcessBinding)
	if !binding.isActive() {
		return nil, false
	}
	return binding.snapshot(), true
}

func (s *ProxyService) ensureProcessAttributionManager() *processattribution.Manager {
	s.processAttributionMu.Lock()
	defer s.processAttributionMu.Unlock()
	return s.ensureProcessAttributionManagerLocked()
}

func (s *ProxyService) ensureProcessAttributionManagerLocked() *processattribution.Manager {
	if s.processAttributionManager != nil {
		return s.processAttributionManager
	}
	store := s.ensureProcessIconStoreLocked()
	s.processAttributionManager = processattribution.NewManager(
		processattribution.NewPlatformProvider(),
		store,
		processattribution.DefaultOptions(),
	)
	return s.processAttributionManager
}

func (s *ProxyService) ensureProcessIconStore() (*processattribution.IconStore, error) {
	s.processAttributionMu.Lock()
	defer s.processAttributionMu.Unlock()
	store := s.ensureProcessIconStoreLocked()
	if store == nil {
		return nil, errors.New("process icon store is not available")
	}
	return store, nil
}

func (s *ProxyService) ensureProcessIconStoreLocked() *processattribution.IconStore {
	if s.processIconStore != nil {
		return s.processIconStore
	}
	baseDir, err := fs.GetBaseStorageDir()
	if err != nil {
		logger.G().Warnf("Process icon store base directory unavailable: %v", err)
		return nil
	}
	store, err := processattribution.NewIconStore(baseDir)
	if err != nil {
		logger.G().Warnf("Process icon store initialization failed: %v", err)
		return nil
	}
	s.processIconStore = store
	return store
}

func (s *ProxyService) processManager() *processattribution.Manager {
	s.processAttributionMu.Lock()
	defer s.processAttributionMu.Unlock()
	return s.processAttributionManager
}

func (s *ProxyService) GetProcessIcon(iconKey string) (*ProcessIconData, error) {
	store, err := s.ensureProcessIconStore()
	if err != nil {
		return nil, err
	}
	data, found, err := store.Get(iconKey)
	if err != nil {
		return nil, err
	}
	if !found {
		// Recovery is intentionally read-triggered. The frontend may keep showing
		// its cached data URL after an external file deletion and will reach this
		// path only after that window-local entry is cleared, evicted, or reloaded.
		manager, fallback := s.processIconRecoveryState(iconKey)
		if manager == nil {
			return nil, nil
		}
		restoredKey, restoreErr := manager.RestoreIcon(s.appContext(), iconKey, fallback)
		if restoreErr != nil {
			return nil, nil
		}
		data, found, err = store.Get(restoredKey)
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, nil
		}
	}
	return &ProcessIconData{
		MIMEType:   "image/png",
		DataBase64: base64.StdEncoding.EncodeToString(data),
	}, nil
}

func (s *ProxyService) processIconRecoveryState(iconKey string) (*processattribution.Manager, processattribution.Result) {
	s.processAttributionMu.Lock()
	defer s.processAttributionMu.Unlock()
	manager := s.processAttributionManager
	if manager == nil {
		return nil, processattribution.Result{}
	}
	var fallback processattribution.Result
	s.connectionProcessLookups.Range(func(_, value any) bool {
		binding, ok := value.(*connectionProcessLookup)
		if !ok || binding == nil || binding.lookup == nil {
			return true
		}
		result := binding.lookup.Snapshot()
		if result.IconKey != iconKey {
			return true
		}
		fallback = result
		return false
	})
	return manager, fallback
}

func (s *ProxyService) resetProcessIconStore(baseDir string) error {
	s.processAttributionMu.Lock()
	defer s.processAttributionMu.Unlock()
	if s.processIconStore == nil {
		store, err := processattribution.NewIconStore(baseDir)
		if err != nil {
			return err
		}
		s.processIconStore = store
	}
	store := s.processIconStore
	if s.processAttributionManager != nil {
		s.processAttributionManager.Close()
		s.processAttributionManager = nil
	}
	s.connectionProcessLookups.Range(func(_, value any) bool {
		binding, ok := value.(*connectionProcessLookup)
		if ok && binding != nil && binding.lookup != nil {
			binding.lookup.InvalidateIcon()
		}
		return true
	})
	return store.Reset()
}

func (s *ProxyService) refreshLocalProcessAddresses() {
	s.localProcessAddressesMu.Lock()
	defer s.localProcessAddressesMu.Unlock()
	s.refreshLocalProcessAddressesLocked()
}

func (s *ProxyService) refreshLocalProcessAddressesAfterMiss() {
	now := time.Now()
	s.localProcessAddressesMu.Lock()
	defer s.localProcessAddressesMu.Unlock()
	if !s.localProcessAddressesLastMiss.IsZero() &&
		now.Sub(s.localProcessAddressesLastMiss) < localProcessAddressMissRefreshInterval {
		return
	}
	s.localProcessAddressesLastMiss = now
	s.refreshLocalProcessAddressesLocked()
}

func (s *ProxyService) refreshLocalProcessAddressesLocked() {
	addresses := map[netip.Addr]struct{}{
		netip.MustParseAddr("127.0.0.1"): {},
		netip.MustParseAddr("::1"):       {},
	}
	interfaceAddresses, err := s.loadLocalProcessAddresses()
	if err != nil {
		if s.localProcessAddresses == nil {
			s.localProcessAddresses = addresses
		}
		return
	}
	for _, address := range interfaceAddresses {
		ip := ipFromNetworkAddress(address)
		if ip.IsValid() {
			addresses[normalizeProcessAddress(ip)] = struct{}{}
		}
	}
	s.localProcessAddresses = addresses
}

func (s *ProxyService) loadLocalProcessAddresses() ([]net.Addr, error) {
	if s.localProcessAddressLoader != nil {
		return s.localProcessAddressLoader()
	}
	return net.InterfaceAddrs()
}

func (s *ProxyService) isLocalProcessAddress(address netip.Addr) bool {
	address = normalizeProcessAddress(address)
	if !address.IsValid() {
		return false
	}
	if address.IsLoopback() {
		return true
	}
	s.localProcessAddressesMu.RLock()
	_, ok := s.localProcessAddresses[address]
	s.localProcessAddressesMu.RUnlock()
	if ok {
		return true
	}
	s.refreshLocalProcessAddressesAfterMiss()
	s.localProcessAddressesMu.RLock()
	_, ok = s.localProcessAddresses[address]
	s.localProcessAddressesMu.RUnlock()
	return ok
}

func normalizedAddrPort(address net.Addr) netip.AddrPort {
	if address == nil {
		return netip.AddrPort{}
	}
	if tcpAddress, ok := address.(*net.TCPAddr); ok {
		return netip.AddrPortFrom(
			normalizeProcessAddress(tcpAddress.AddrPort().Addr()),
			tcpAddress.AddrPort().Port(),
		)
	}
	parsed, err := netip.ParseAddrPort(address.String())
	if err != nil {
		return netip.AddrPort{}
	}
	return netip.AddrPortFrom(normalizeProcessAddress(parsed.Addr()), parsed.Port())
}

func normalizeProcessAddress(address netip.Addr) netip.Addr {
	if !address.IsValid() {
		return address
	}
	return address.Unmap().WithZone("")
}

func ipFromNetworkAddress(address net.Addr) netip.Addr {
	switch value := address.(type) {
	case *net.IPNet:
		if parsed, ok := netip.AddrFromSlice(value.IP); ok {
			return parsed
		}
	case *net.IPAddr:
		if parsed, ok := netip.AddrFromSlice(value.IP); ok {
			return parsed
		}
	}
	return netip.Addr{}
}

func mapProcessAttributionResult(result processattribution.Result) *ProcessInfo {
	return &ProcessInfo{
		Status:             ProcessStatus(result.Status),
		PID:                result.PID,
		DisplayName:        result.DisplayName,
		ProcessName:        result.ProcessName,
		ExecutablePath:     result.ExecutablePath,
		AppID:              result.AppID,
		IconKey:            result.IconKey,
		Source:             result.Source,
		IdentityConfidence: result.IdentityConfidence,
		UnavailableReason:  result.Reason,
	}
}

func cloneTrafficEntryForAttribution(entry *TrafficEntry) *TrafficEntry {
	if entry == nil {
		return nil
	}
	clone := *entry
	clone.Metadata = cloneTrafficMetadata(entry.Metadata)
	clone.Request = cloneTrafficHTTPMessage(entry.Request)
	clone.Response = cloneTrafficHTTPMessage(entry.Response)
	if entry.Error != nil {
		errorCopy := *entry.Error
		clone.Error = &errorCopy
	}
	return &clone
}

// cloneTrafficEntryForProcess performs a copy-on-write update of Metadata.Process.
// All other fields come from an immutable stored snapshot and remain shared.
func cloneTrafficEntryForProcess(entry *TrafficEntry) *TrafficEntry {
	if entry == nil {
		return nil
	}
	clone := *entry
	clone.Metadata = cloneTrafficMetadataForMetrics(entry.Metadata)
	return &clone
}

// cloneTrafficEntryForMetrics creates an immutable snapshot for a change that
// only touched HTTPMessageMetrics. Headers, trailers, and connection metadata
// are immutable after publication, so sharing those backing arrays avoids
// reallocating them at every transport milestone. The containing structs and
// metric values are still copied so later callbacks cannot race with readers.
func cloneTrafficEntryForMetrics(entry *TrafficEntry) *TrafficEntry {
	if entry == nil {
		return nil
	}
	clone := *entry
	clone.Metadata = cloneTrafficMetadataForMetrics(entry.Metadata)
	clone.Request = cloneTrafficHTTPMessageForMetrics(entry.Request)
	clone.Response = cloneTrafficHTTPMessageForMetrics(entry.Response)
	if entry.Error != nil {
		errorCopy := *entry.Error
		clone.Error = &errorCopy
	}
	return &clone
}

// cloneTrafficEntryForResponseHeaders copies only the newly published response
// header collection. Request headers, connection metadata, TLS details, and
// certificates are immutable at this point and can be shared with the working
// entry without being reallocated.
func cloneTrafficEntryForResponseHeaders(entry *TrafficEntry) *TrafficEntry {
	clone := cloneTrafficEntryForMetrics(entry)
	if clone == nil || entry.Response == nil {
		return clone
	}
	clone.Response.HeaderFields = append([]HTTPHeaderField(nil), entry.Response.HeaderFields...)
	return clone
}

// cloneTrafficEntryForResponseTrailers shares the already immutable response
// headers and copies only the trailer collection that has just become final.
func cloneTrafficEntryForResponseTrailers(entry *TrafficEntry) *TrafficEntry {
	clone := cloneTrafficEntryForMetrics(entry)
	if clone == nil || entry.Response == nil {
		return clone
	}
	clone.Response.TrailerFields = append([]HTTPHeaderField(nil), entry.Response.TrailerFields...)
	return clone
}

func cloneTrafficMetadataForMetrics(metadata *Metadata) *Metadata {
	if metadata == nil {
		return nil
	}
	clone := *metadata
	// ProcessInfo values are immutable snapshots. Attribution replaces the
	// pointer under trafficAttributionMu instead of mutating a published value.
	return &clone
}

func cloneTrafficHTTPMessageForMetrics(message *HTTPMessage) *HTTPMessage {
	if message == nil {
		return nil
	}
	clone := *message
	if message.Metrics != nil {
		metrics := *message.Metrics
		clone.Metrics = &metrics
	}
	return &clone
}

func cloneTrafficMetadata(metadata *Metadata) *Metadata {
	if metadata == nil {
		return nil
	}
	clone := *metadata
	if metadata.Process != nil {
		process := *metadata.Process
		clone.Process = &process
	}
	if metadata.TLS != nil {
		tlsState := *metadata.TLS
		tlsState.SupportedALPN = append([]string(nil), metadata.TLS.SupportedALPN...)
		tlsState.SupportedVersion = append([]string(nil), metadata.TLS.SupportedVersion...)
		tlsState.SupportedCipherSuites = append([]string(nil), metadata.TLS.SupportedCipherSuites...)
		clone.TLS = &tlsState
	}
	if metadata.Certificate != nil {
		certificate := *metadata.Certificate
		certificate.DNSNames = append([]string(nil), metadata.Certificate.DNSNames...)
		certificate.IPAddresses = append([]string(nil), metadata.Certificate.IPAddresses...)
		certificate.Subject = clonePkixName(metadata.Certificate.Subject)
		certificate.Issuer = clonePkixName(metadata.Certificate.Issuer)
		clone.Certificate = &certificate
	}
	return &clone
}

func clonePkixName(name *PkixName) *PkixName {
	if name == nil {
		return nil
	}
	clone := *name
	clone.Country = append([]string(nil), name.Country...)
	clone.Organization = append([]string(nil), name.Organization...)
	clone.OrganizationalUnit = append([]string(nil), name.OrganizationalUnit...)
	clone.Locality = append([]string(nil), name.Locality...)
	clone.Province = append([]string(nil), name.Province...)
	clone.StreetAddress = append([]string(nil), name.StreetAddress...)
	clone.PostalCode = append([]string(nil), name.PostalCode...)
	return &clone
}

func cloneTrafficHTTPMessage(message *HTTPMessage) *HTTPMessage {
	if message == nil {
		return nil
	}
	clone := *message
	if message.Metrics != nil {
		metrics := *message.Metrics
		clone.Metrics = &metrics
	}
	if message.HeaderFields != nil {
		clone.HeaderFields = make([]HTTPHeaderField, len(message.HeaderFields))
		copy(clone.HeaderFields, message.HeaderFields)
	}
	if message.TrailerFields != nil {
		clone.TrailerFields = make([]HTTPHeaderField, len(message.TrailerFields))
		copy(clone.TrailerFields, message.TrailerFields)
	}
	return &clone
}

func (b *trafficProcessBinding) update(process *ProcessInfo) bool {
	if process == nil {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.active || processProgress(process) < processProgress(b.latest) || processEqual(process, b.latest) {
		return false
	}
	copy := *process
	b.latest = &copy
	return true
}

func (b *trafficProcessBinding) snapshot() *ProcessInfo {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.latest == nil {
		return nil
	}
	copy := *b.latest
	return &copy
}

func (b *trafficProcessBinding) isActive() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.active
}

func (b *trafficProcessBinding) installOff(off func()) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.active {
		return false
	}
	b.off = off
	return true
}

func (b *trafficProcessBinding) deactivate() func() {
	b.mu.Lock()
	b.active = false
	off := b.off
	b.off = nil
	b.mu.Unlock()
	return off
}

func processProgress(process *ProcessInfo) int {
	if process == nil {
		return -1
	}
	if process.Status == ProcessStatusPending {
		return 0
	}
	if process.Status == ProcessStatusResolved && process.IconKey != "" {
		return 2
	}
	return 1
}

func processEqual(left, right *ProcessInfo) bool {
	if left == nil || right == nil {
		return left == right
	}
	return *left == *right
}
