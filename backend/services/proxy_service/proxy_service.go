package proxyservice

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	bodycache "github.com/josexy/flowlens/backend/pkg/body_cache"
	bodyspool "github.com/josexy/flowlens/backend/pkg/body_spool"
	"github.com/josexy/flowlens/backend/pkg/fs"
	"github.com/josexy/flowlens/backend/pkg/logger"
	"github.com/josexy/flowlens/backend/pkg/orderedmap"
	processattribution "github.com/josexy/flowlens/backend/pkg/process_attribution"
	"github.com/josexy/flowlens/backend/pkg/systemproxy"
	settingservice "github.com/josexy/flowlens/backend/services/setting_service"
	"github.com/josexy/logx"
	"github.com/josexy/mitmproxy-go"
	"github.com/josexy/mitmproxy-go/metadata"
	"github.com/josexy/websocket"
	http "github.com/josexy/xhttp"
	"github.com/josexy/xhttp/httputil"
	"github.com/wailsapp/wails/v3/pkg/application"
)

const (
	trafficEventName               = "traffic:entry"
	trafficPatchEventName          = "traffic:patch"
	trafficLiveUpdateEventName     = "traffic:live-update"
	trafficResetEventName          = "traffic:reset"
	statusEventName                = "proxy:status"
	httpRequestEventName           = "http-request:event"
	webSocketSessionEventName      = "websocket-session:event"
	requestEditorFileDropEventName = "request-editor:file-drop"
	localDataClearedEventName      = "app:local-data-cleared"
	systemProxyStatusEventName     = "proxy:system-proxy-status"
	mainWindowName                 = "main"
	frontendEventFlushInterval     = 16 * time.Millisecond
	frontendEventMaxPending        = 512
	frontendEventMaxBytes          = 4 << 20
	frontendTrafficEntryLimit      = 5000
)

type TrafficEntryWithStatics struct {
	*orderedmap.OrderedMap[uint64, *TrafficEntry]
	Statistics *TrafficStatistics
}

type HistoryCleaner interface {
	ClearHistories() error
}

type ProxyService struct {
	// lifecycleOpsMu serializes operations that publish, tear down, or reset
	// the proxy lifecycle. The cross-domain order is lifecycleOpsMu,
	// requestDraftCacheOpsMu, clearDataMu, captureLifecycleMu, bodyCacheMu, then mu.
	lifecycleOpsMu       sync.Mutex
	mu                   sync.Mutex
	baseCtx              context.Context
	baseCancel           context.CancelFunc
	shutdownOnce         sync.Once
	shutdownErr          error
	app                  *application.App
	frontendEventBatcher *frontendEventBatcher
	running              bool
	address              string
	started              time.Time
	server               *http.Server
	ln                   net.Listener
	nextID               uint64

	captureLifecycleMu sync.RWMutex
	captureGeneration  uint64
	// trafficPublishMu keeps snapshot revisions and frontend event enqueueing in
	// the same order across transport and process-attribution goroutines.
	trafficPublishMu sync.Mutex

	proxyServerCancel context.CancelFunc
	proxyHandler      mitmproxy.DynamicMitmProxyHandler
	startProxyConfig  *settingservice.ProxyConfig
	settingService    *settingservice.SettingService

	trafficEntries          *TrafficEntryWithStatics
	trafficBodies           sync.Map
	trafficWsMsgs           sync.Map
	httpRequestStreams      sync.Map
	webSocketSessions       sync.Map
	requestDraftCacheOpsMu  sync.RWMutex
	httpRequestPluginMu     sync.RWMutex
	httpRequestPluginRunner HTTPRequestPluginRunner

	historyMetadataMu             sync.RWMutex
	currentHistoryMetadata        HistoryMetadata
	bgSyncOnce                    sync.Once
	enableFlushing                atomic.Bool
	historyDirty                  atomic.Bool
	flushGeneration               atomic.Uint64
	lastFlushGeneration           atomic.Uint64
	emitWebSocketSessionEventHook func(WebSocketSessionEvent)
	emitHTTPRequestEventHook      func(HTTPRequestStreamEvent)
	emitTrafficHook               func(*TrafficEntry)
	emitTrafficPatchHook          func(TrafficEntryPatch)
	emitTrafficLiveUpdateHook     func(TrafficLiveUpdate)
	liveTrafficDetailID           atomic.Uint64
	bodyCacheMu                   sync.RWMutex
	bodyCache                     *bodycache.BodyCache
	clearDataMu                   sync.Mutex
	historyCleanerMu              sync.RWMutex
	historyCleaner                HistoryCleaner
	systemProxyOpsMu              sync.Mutex
	systemProxyShuttingDown       bool
	systemProxy                   systemProxyController
	processAttributionMu          sync.Mutex
	processAttributionManager     *processattribution.Manager
	processIconStore              *processattribution.IconStore
	trafficAttributionMu          sync.Mutex
	trafficProcessBindings        sync.Map
	connectionProcessLookups      sync.Map
	localProcessAddressesMu       sync.RWMutex
	localProcessAddresses         map[netip.Addr]struct{}
	localProcessAddressesLastMiss time.Time
	localProcessAddressLoader     func() ([]net.Addr, error)
	// storeTrafficPrecheckHook is test-only synchronization used to
	// deterministically exercise deletion between its two tombstone checks.
	storeTrafficPrecheckHook func()
	// removeHistoryFile is test-only failure injection for current-history
	// pair deletion. Production uses os.Remove.
	removeHistoryFile func(string) error
	// renameHistoryFile and historyFlushStageHook are test-only fault injection
	// points for the two-file history transaction. Production uses os.Rename and
	// performs every stage without an injected error.
	renameHistoryFile     func(string, string) error
	historyFlushStageHook func(string) error
	// requestDraftCacheOperationHook is test-only synchronization invoked while a
	// request draft cache operation holds requestDraftCacheOpsMu.
	requestDraftCacheOperationHook func(string)
	// lifecycleOperationHook is test-only synchronization invoked at named
	// checkpoints while a lifecycle operation holds lifecycleOpsMu.
	lifecycleOperationHook func(string)
}

// TrafficBodies keeps the live body capture state for one traffic entry.
//
// Request and response bodies are intentionally stored as capturedBody instead
// of raw bytes.Buffer fields. The capturedBody hides whether the data is still
// in memory or has been spooled into body cache, so readers do not need a
// separate "memory first, disk fallback" code path.
type TrafficBodies struct {
	liveState int32
	// lockReqBody protects requestBody while the stream reader writes chunks
	// and UI/history/resend code opens readers from the captured state.
	lockReqBody sync.RWMutex
	// lockRespBody is the response-side equivalent of lockReqBody.
	lockRespBody sync.RWMutex
	// requestBody owns both request body storage states: in-memory while small,
	// or body-cache-backed after crossing the threshold.
	requestBody *capturedBody
	// responseBody is the response-side equivalent of requestBody.
	responseBody *capturedBody
}

type TrafficWsMsgs struct {
	liveState int32
	lock      sync.RWMutex
	Messages  []*WebSocketMessage
	Truncated bool
}

type webSocketSession struct {
	id      string
	target  string
	started time.Time
	conn    *websocket.Conn
	cancel  context.CancelFunc
}

type httpRequestStreamSession struct {
	id        string
	target    string
	started   time.Time
	cancel    context.CancelFunc
	response  *http.Response
	body      io.ReadCloser
	transport idleClosingRoundTripper
	offset    atomic.Int64
}

func New(settingService *settingservice.SettingService) *ProxyService {
	service := &ProxyService{
		settingService: settingService,
		systemProxy:    systemproxy.NewController(),
		trafficEntries: &TrafficEntryWithStatics{
			OrderedMap: orderedmap.NewWithCapacity[uint64, *TrafficEntry](128),
			Statistics: &TrafficStatistics{},
		},
	}
	service.resetCurrentHistoryMetadata()
	return service
}

// SetHTTPRequestPluginRunner installs the narrow HTTP Request plugin integration.
// The runner is optional so proxy capture and request editing remain available when the
// Python runtime is disabled or unavailable.
//
//wails:ignore
func (s *ProxyService) SetHTTPRequestPluginRunner(runner HTTPRequestPluginRunner) {
	if s == nil {
		return
	}
	s.httpRequestPluginMu.Lock()
	s.httpRequestPluginRunner = runner
	s.httpRequestPluginMu.Unlock()
}

// SetHistoryCleaner installs the history index owner used by local-data
// cleanup. Keeping this dependency narrow avoids importing history_service
// back into proxy_service, which would create a package cycle.
//
//wails:ignore
func (s *ProxyService) SetHistoryCleaner(cleaner HistoryCleaner) {
	if s == nil {
		return
	}
	s.historyCleanerMu.Lock()
	s.historyCleaner = cleaner
	s.historyCleanerMu.Unlock()
}

func (s *ProxyService) httpRequestRunner() HTTPRequestPluginRunner {
	s.httpRequestPluginMu.RLock()
	defer s.httpRequestPluginMu.RUnlock()
	return s.httpRequestPluginRunner
}

func (s *ProxyService) ServiceStartup(ctx context.Context, _ application.ServiceOptions) error {
	s.baseCtx, s.baseCancel = context.WithCancel(ctx)
	s.app = application.Get()
	s.frontendEventBatcher = newWindowFrontendEventBatcher(
		s.app,
		mainWindowName,
		frontendEventBatcherOptions{
			FlushInterval:    frontendEventFlushInterval,
			MaxPendingEvents: frontendEventMaxPending,
			MaxPendingBytes:  frontendEventMaxBytes,
		},
	)
	s.refreshLocalProcessAddresses()
	s.startBackgroundSyncIfReady()
	logger.G().Info("Proxy service started")
	return nil
}

func (s *ProxyService) ServiceShutdown() error {
	return s.Shutdown()
}

//wails:ignore
func (s *ProxyService) Shutdown() error {
	s.shutdownOnce.Do(func() {
		s.shutdownErr = s.shutdown()
	})
	return s.shutdownErr
}

func (s *ProxyService) shutdown() error {
	logger.G().Info("Proxy service stopping")
	if s.baseCancel != nil {
		s.baseCancel()
	}
	s.liveTrafficDetailID.Store(0)
	s.closeAllHTTPRequestStreams()
	s.closeAllWebSocketSessions()
	var shutdownErr error
	if err := s.restoreManagedSystemProxy(); err != nil {
		logger.G().Warnf("Restore system proxy on shutdown failed: %v", err)
		shutdownErr = errors.Join(shutdownErr, fmt.Errorf("restore managed system proxy: %w", err))
	}
	if _, err := s.Stop(); err != nil {
		logger.G().Warnf("Stop proxy on shutdown failed: %v", err)
		shutdownErr = errors.Join(shutdownErr, fmt.Errorf("stop proxy: %w", err))
	}
	if manager := s.processManager(); manager != nil {
		drainCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		manager.Drain(drainCtx)
		cancel()
	}
	if err := s.flushOnShutdown(); err != nil {
		shutdownErr = errors.Join(shutdownErr, err)
	}
	if manager := s.processManager(); manager != nil {
		manager.Close()
	}
	if s.frontendEventBatcher != nil {
		s.frontendEventBatcher.Close()
	}
	logger.G().Info("Proxy service stopped")
	return shutdownErr
}

func (s *ProxyService) startBackgroundSyncIfReady() {
	if s.app == nil || s.settingService == nil {
		return
	}
	s.bgSyncOnce.Do(func() {
		go s.syncHistoriesToDiskPeriodically()
	})
}

func (s *ProxyService) GetStatus() ProxyStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.currentStatusLocked()
}

func (s *ProxyService) appContext() context.Context {
	return s.baseCtx
}

func (s *ProxyService) getProxyConfig() (*settingservice.ProxyConfig, error) {
	if s.settingService == nil {
		return nil, errors.New("setting service is not available")
	}
	return s.settingService.GetProxyConfig()
}

func (s *ProxyService) Start() (ProxyStatus, error) {
	s.lifecycleOpsMu.Lock()
	defer s.lifecycleOpsMu.Unlock()
	s.runLifecycleOperationHook("start")

	s.mu.Lock()
	if s.running {
		status := s.currentStatusLocked()
		s.mu.Unlock()
		return status, nil
	}
	s.mu.Unlock()

	cfg, err := s.getProxyConfig()
	if err != nil {
		return ProxyStatus{}, err
	}

	proxyServerCtx, proxyServerCancel := context.WithCancel(context.Background())
	handler, err := s.buildHandlerLocked(proxyServerCtx, cfg)
	if err != nil {
		proxyServerCancel()
		logger.G().Errorf("Failed to build proxy handler: %v", err)
		return ProxyStatus{}, err
	}

	listenAddr := net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		handler.Cleanup()
		proxyServerCancel()
		logger.G().Errorf("Proxy listener bind failed on %s: %v", listenAddr, err)
		return ProxyStatus{}, err
	}

	s.ensureBodyCacheLocked()

	var server *http.Server
	var ln net.Listener
	var serve func()
	switch cfg.Mode {
	case settingservice.ProxyModeSOCKS5:
		ln = listener
		serve = func() {
			defer handler.Cleanup()
			for {
				conn, err := listener.Accept()
				if err != nil {
					if proxyServerCtx.Err() == nil {
						logger.G().Warnf("Proxy SOCKS5 listener accept failed: %v", err)
					}
					return
				}
				conn = s.attributeConnection(conn)
				go handler.ServeSOCKS5(proxyServerCtx, conn)
			}
		}
	case settingservice.ProxyModeHTTP:
		server = &http.Server{
			Addr:        listenAddr,
			Handler:     handler,
			BaseContext: func(l net.Listener) context.Context { return proxyServerCtx },
		}
		attributed := &attributedListener{Listener: listener, service: s}
		serve = func() {
			defer handler.Cleanup()
			if err := server.Serve(attributed); err != nil && !errors.Is(err, http.ErrServerClosed) {
				logger.G().Warnf("Proxy HTTP server stopped unexpectedly: %v", err)
			}
		}
	default:
		_ = listener.Close()
		handler.Cleanup()
		proxyServerCancel()
		return ProxyStatus{}, fmt.Errorf("unsupported proxy mode: %s", cfg.Mode)
	}

	// Publish a complete lifecycle snapshot atomically: once running becomes
	// observable, Stop can always find the matching cancel function and
	// server/listener. lifecycleOpsMu keeps teardown out until serve is started.
	s.mu.Lock()
	s.proxyServerCancel = proxyServerCancel
	s.proxyHandler = handler
	s.startProxyConfig = cloneProxyConfig(cfg)
	s.server = server
	s.ln = ln
	s.enableFlushing.Store(true)
	s.setStateLocked(true, listener.Addr().String())
	go serve()
	status := s.currentStatusLocked()
	s.mu.Unlock()
	s.runLifecycleOperationHook("start-published")

	logger.G().Infof(
		"Proxy started: mode=%s address=%s disableProxy=%t disableHTTP2=%t skipVerifyTLS=%t",
		cfg.Mode,
		listener.Addr().String(),
		cfg.DisableProxy,
		cfg.DisableHTTP2,
		cfg.SkipVerifyTLS,
	)
	return status, nil
}

func (s *ProxyService) setStateLocked(start bool, address string) {
	if start {
		s.running = true
		s.address = address
		s.started = time.Now()
	} else {
		s.running = false
		s.address = ""
		s.started = time.Time{}
	}
}

func (s *ProxyService) Stop() (ProxyStatus, error) {
	s.lifecycleOpsMu.Lock()
	defer s.lifecycleOpsMu.Unlock()
	s.runLifecycleOperationHook("stop")

	s.mu.Lock()
	if !s.running {
		status := s.currentStatusLocked()
		s.mu.Unlock()
		return status, nil
	}

	s.enableFlushing.Store(false)

	server := s.server
	ln := s.ln
	proxyServerCancel := s.proxyServerCancel
	s.server = nil
	s.ln = nil
	s.proxyServerCancel = nil
	s.proxyHandler = nil
	s.startProxyConfig = nil
	s.setStateLocked(false, "")
	s.mu.Unlock()

	if proxyServerCancel != nil {
		proxyServerCancel()
	}

	if server != nil {
		_ = server.Close()
	}
	if ln != nil {
		_ = ln.Close()
	}

	logger.G().Info("Proxy stopped")
	return s.GetStatus(), nil
}

func (s *ProxyService) currentStatusLocked() ProxyStatus {
	return ProxyStatus{
		Running:      s.running,
		Address:      s.address,
		Started:      s.started,
		CaptureAlias: s.currentHistoryAlias(),
	}
}

func (s *ProxyService) buildHandlerLocked(ctx context.Context, cfg *settingservice.ProxyConfig) (mitmproxy.DynamicMitmProxyHandler, error) {
	opts := []mitmproxy.Option{
		mitmproxy.WithStreamBaseContext(ctx),
		mitmproxy.WithLogger(logger.WailsLogger()),
		mitmproxy.WithIdleConnTimeout(time.Second * 60),
		mitmproxy.WithCertCachePool(2048, 90, 60),
		mitmproxy.WithCACertPath(cfg.CACertPath),
		mitmproxy.WithCAKeyPath(cfg.CAKeyPath),
		mitmproxy.WithUpstreamHTTPTrace(),
		mitmproxy.WithHTTPInterceptor(s.httpInterceptor(cfg)),
		mitmproxy.WithWebsocketInterceptor(s.websocketInterceptor(cfg)),
		mitmproxy.WithRawTCPInterceptor(s.rawTCPInterceptor()),
		mitmproxy.WithMaxWebsocketFramesPerForward(2048),
		mitmproxy.WithErrorHandler(func(ec mitmproxy.ErrorContext) {
			logger.G().Error("mitm proxy error",
				logx.String("remote_addr", ec.RemoteAddr),
				logx.String("hostport", ec.Hostport),
				logx.Error("error", ec.Error))
		}),
	}

	if cfg.DisableProxy {
		opts = append(opts, mitmproxy.WithDisableProxy())
	} else if upstreamProxy := s.resolveMITMUpstreamProxy(cfg); upstreamProxy != "" {
		opts = append(opts, mitmproxy.WithProxy(upstreamProxy))
	}

	if cfg.DisableHTTP2 {
		opts = append(opts, mitmproxy.WithDisableHTTP2())
	}

	if cfg.SkipVerifyTLS {
		opts = append(opts, mitmproxy.WithSkipVerifySSLFromServer())
	}

	if len(cfg.IncludeHosts) > 0 {
		opts = append(opts, mitmproxy.WithIncludeHosts(cfg.IncludeHosts...))
	}
	if len(cfg.ExcludeHosts) > 0 {
		opts = append(opts, mitmproxy.WithExcludeHosts(cfg.ExcludeHosts...))
	}
	if len(cfg.RootCAPaths) > 0 {
		opts = append(opts, mitmproxy.WithRootCAs(cfg.RootCAPaths...))
	}
	for _, cert := range cfg.ClientCerts {
		if !cert.Enabled {
			continue
		}
		opts = append(opts, mitmproxy.WithClientCert(cert.Hostname, mitmproxy.ClientCert{
			CertPath: cert.CertPath,
			KeyPath:  cert.KeyPath,
		}))
	}

	return mitmproxy.NewDynamicMitmProxyHandler(opts...)
}

func (s *ProxyService) ApplyCurrentProxyConfig() (ProxyConfigApplyResult, error) {
	s.lifecycleOpsMu.Lock()
	defer s.lifecycleOpsMu.Unlock()
	s.runLifecycleOperationHook("apply-config")

	cfg, err := s.getProxyConfig()
	if err != nil {
		return ProxyConfigApplyResult{}, err
	}
	if err := s.syncManagedSystemProxy(cfg); err != nil {
		logger.G().Errorf("Failed to synchronize managed system proxy: %v", err)
		return ProxyConfigApplyResult{}, err
	}

	s.mu.Lock()
	running := s.running
	handler := s.proxyHandler
	startCfg := cloneProxyConfig(s.startProxyConfig)
	s.mu.Unlock()

	if !running || handler == nil {
		return ProxyConfigApplyResult{Applied: false}, nil
	}

	if err := s.applyRuntimeProxyConfig(handler, cfg); err != nil {
		logger.G().Errorf("Failed to apply runtime proxy config: %v", err)
		return ProxyConfigApplyResult{}, err
	}

	logger.G().Infof(
		"Applied runtime proxy config: disableProxy=%t disableHTTP2=%t skipVerifyTLS=%t includeHosts=%d excludeHosts=%d",
		cfg.DisableProxy,
		cfg.DisableHTTP2,
		cfg.SkipVerifyTLS,
		len(cfg.IncludeHosts),
		len(cfg.ExcludeHosts),
	)
	return ProxyConfigApplyResult{
		Applied:         true,
		RestartRequired: proxyRestartRequired(startCfg, cfg),
		RestartReasons:  proxyRestartReasons(startCfg, cfg),
	}, nil
}

func (s *ProxyService) applyRuntimeProxyConfig(handler mitmproxy.DynamicMitmProxyHandler, cfg *settingservice.ProxyConfig) error {
	if cfg == nil {
		return errors.New("proxy config cannot be nil")
	}
	if cfg.DisableProxy {
		if err := handler.SetProxyDisabled(true); err != nil {
			return err
		}
	} else {
		if err := handler.SetProxy(s.resolveMITMUpstreamProxy(cfg)); err != nil {
			return err
		}
	}
	handler.SetHTTP2Disabled(cfg.DisableHTTP2)
	handler.SetSkipVerifySSLFromServer(cfg.SkipVerifyTLS)
	handler.SetHostFilters(cfg.IncludeHosts, cfg.ExcludeHosts)
	if err := handler.SetRootCAs(cfg.RootCAPaths...); err != nil {
		return err
	}
	if err := handler.SetClientCerts(buildClientCertMap(cfg.ClientCerts)); err != nil {
		return err
	}
	return nil
}

func proxyRestartRequired(startCfg, currentCfg *settingservice.ProxyConfig) bool {
	return len(proxyRestartReasons(startCfg, currentCfg)) > 0
}

func proxyRestartReasons(startCfg, currentCfg *settingservice.ProxyConfig) []string {
	if startCfg == nil || currentCfg == nil {
		return nil
	}
	reasons := make([]string, 0, 4)
	if startCfg.Mode != currentCfg.Mode {
		reasons = append(reasons, "proxy mode changed")
	}
	if startCfg.Host != currentCfg.Host || startCfg.Port != currentCfg.Port {
		reasons = append(reasons, "listen address changed")
	}
	if startCfg.CACertPath != currentCfg.CACertPath || startCfg.CAKeyPath != currentCfg.CAKeyPath {
		reasons = append(reasons, "mitm ca changed")
	}
	return reasons
}

func buildClientCertMap(configs []settingservice.ClientCertConfig) map[string]mitmproxy.ClientCert {
	if len(configs) == 0 {
		return nil
	}
	certs := make(map[string]mitmproxy.ClientCert)
	for _, cfg := range configs {
		if !cfg.Enabled {
			continue
		}
		hostname := strings.TrimSpace(cfg.Hostname)
		if hostname == "" {
			continue
		}
		certs[hostname] = mitmproxy.ClientCert{
			CertPath: strings.TrimSpace(cfg.CertPath),
			KeyPath:  strings.TrimSpace(cfg.KeyPath),
		}
	}
	return certs
}

func cloneProxyConfig(cfg *settingservice.ProxyConfig) *settingservice.ProxyConfig {
	if cfg == nil {
		return nil
	}
	clone := *cfg
	clone.IncludeHosts = slices.Clone(cfg.IncludeHosts)
	clone.ExcludeHosts = slices.Clone(cfg.ExcludeHosts)
	clone.RootCAPaths = slices.Clone(cfg.RootCAPaths)
	clone.ClientCerts = slices.Clone(cfg.ClientCerts)
	return &clone
}

func (s *ProxyService) newTrafficEntry(entry TrafficEntry) *TrafficEntry {
	s.captureLifecycleMu.RLock()
	entry.ID = atomic.AddUint64(&s.nextID, 1)
	entry.captureGeneration = s.captureGeneration
	entry.lifecycle = &trafficEntryLifecycle{}
	s.captureLifecycleMu.RUnlock()
	return &entry
}

func (s *ProxyService) isCurrentTrafficEntryLocked(entry *TrafficEntry) bool {
	if entry == nil || entry.captureGeneration != s.captureGeneration {
		return false
	}
	return entry.lifecycle == nil || !entry.lifecycle.deleted.Load()
}

func (s *ProxyService) httpInterceptor(cfg *settingservice.ProxyConfig) mitmproxy.HTTPInterceptor {
	return func(ctx context.Context, req *http.Request, invoker mitmproxy.HTTPDelegatedInvoker) (*http.Response, error) {
		entry := s.newTrafficEntry(TrafficEntry{
			Type:   req.URL.Scheme,
			Method: req.Method,
			URL:    req.URL.String(),
			Host:   req.Host,
			Path:   req.URL.Path,
		})

		s.fillEntryMetadataFromContext(ctx, entry)
		s.fillRequestHTTPMessage(req, entry)
		exchange := newCaptureExchange(s, ctx, entry)
		requestBodyless := req.Body == nil || req.Body == http.NoBody
		exchange.setRequestBodyless(requestBodyless)
		if !mitmproxy.ObserveHTTPExchangeTiming(ctx, exchange.observeHTTPExchangeTiming) {
			logger.G().Warn("upstream HTTP timing observer is unavailable",
				logx.String("host", req.Host))
		}

		capturedRequestBody := s.newCaptureStreamBodyReader(
			req.Body,
			entry,
			req.Header.Get("Content-Encoding"),
			true,
		)
		req.Body = observeTransferBody(capturedRequestBody, req.ContentLength, exchange.requestBodyFinished)
		exchange.observeRetryRequestBodies(req)
		resp, err := invoker.Invoke(req)
		if err != nil {
			logger.G().Error("error invoking request", logx.String("host", req.Host), logx.String("error", err.Error()))
			exchange.fail(err)
			return resp, err
		}

		exchange.responseHeaders(resp)
		responseBodyless := responseHasNoEntityBody(req.Method, resp)
		if responseBodyless {
			exchange.markBodylessResponseSize()
			return resp, nil
		}

		if shouldStreamSSEUpdates(
			resp.Header.Get("Content-Type"),
			resp.Header.Get("Content-Encoding"),
		) {
			capturedResponseBody := s.newCaptureObservedStreamBodyReader(
				resp.Body,
				entry,
				resp.Header.Get("Content-Encoding"),
				false,
				func(offset int64, data []byte) {
					chunkOffset := offset
					s.emitCapturedTrafficLiveUpdate(entry, TrafficLiveUpdate{
						TrafficID:   entry.ID,
						Kind:        TrafficLiveUpdateSSEChunk,
						Offset:      &chunkOffset,
						ChunkBase64: base64.StdEncoding.EncodeToString(data),
					})
				},
				func() { exchange.fillResponseTrailers(resp) },
			)
			resp.Body = observeTransferBody(capturedResponseBody, resp.ContentLength, exchange.responseBodyFinished)
		} else {
			capturedResponseBody := s.newCaptureStreamBodyReader(
				resp.Body,
				entry,
				resp.Header.Get("Content-Encoding"),
				false,
				func() { exchange.fillResponseTrailers(resp) },
			)
			resp.Body = observeTransferBody(capturedResponseBody, resp.ContentLength, exchange.responseBodyFinished)
		}

		return resp, nil
	}
}

func responseHasNoEntityBody(requestMethod string, response *http.Response) bool {
	if response == nil {
		return true
	}
	if response.Body == nil || response.Body == http.NoBody || requestMethod == http.MethodHead {
		return true
	}
	return response.StatusCode >= 100 && response.StatusCode < 200 ||
		response.StatusCode == http.StatusNoContent ||
		response.StatusCode == http.StatusNotModified
}

func (s *ProxyService) rawTCPInterceptor() mitmproxy.RawTCPInterceptor {
	return func(ctx context.Context, event mitmproxy.RawTCPTunnelEvent) {
		source := mapRawTCPTunnelSource(event.Source)
		entry := s.newTrafficEntry(TrafficEntry{
			Type:      "tcp",
			StartedAt: time.Now(),
			URL:       "tcp://" + event.Hostport,
			Host:      event.Hostport,
			RawTCP: &RawTCPTunnelInfo{
				Source:   source,
				HostPort: event.Hostport,
				TLS:      event.TLS,
			},
		})
		if source == RawTCPTunnelSourceHTTPConnect {
			entry.Method = http.MethodConnect
			if event.Request != nil {
				s.fillRequestHTTPMessage(event.Request, entry)
			}
		}

		s.fillEntryMetadataFromContext(ctx, entry)
		s.trafficPublishMu.Lock()
		if s.storeTrafficEntry(entry) {
			s.emitTraffic(entry)
		}
		s.trafficPublishMu.Unlock()
	}
}

func mapRawTCPTunnelSource(source mitmproxy.RawTCPTunnelSource) RawTCPTunnelSource {
	switch source {
	case mitmproxy.RawTCPTunnelSourceDirect:
		return RawTCPTunnelSourceDirect
	case mitmproxy.RawTCPTunnelSourceHTTPConnect:
		return RawTCPTunnelSourceHTTPConnect
	case mitmproxy.RawTCPTunnelSourceSOCKS5:
		return RawTCPTunnelSourceSOCKS5
	default:
		return RawTCPTunnelSourceUnknown
	}
}

func (s *ProxyService) websocketInterceptor(cfg *settingservice.ProxyConfig) mitmproxy.WebsocketInterceptor {
	return func(ctx context.Context, req *http.Request, rsp *http.Response, fw mitmproxy.WebsocketFramesWatcher) {
		handshakeTiming, hasHandshakeTiming := mitmproxy.WebsocketHandshakeTimingFromContext(ctx)
		startedAt := time.Now()
		if hasHandshakeTiming && !handshakeTiming.RequestStartedAt.IsZero() {
			startedAt = handshakeTiming.RequestStartedAt
		}
		entry := s.newTrafficEntry(TrafficEntry{
			Type:      req.URL.Scheme,
			StartedAt: startedAt,
			Method:    req.Method,
			URL:       req.URL.String(),
			Host:      req.Host,
			Path:      req.URL.Path,
		})

		s.fillEntryMetadataFromContext(ctx, entry)
		s.fillRequestHTTPMessage(req, entry)

		entry.StatusCode = rsp.StatusCode
		entry.Status = rsp.Status
		s.fillResponseHTTPMessage(rsp, entry)
		requestBodySize := int64(0)
		if req.ContentLength > 0 {
			requestBodySize = req.ContentLength
		}
		entry.Request.Metrics = completedHandshakeMetrics(
			entry.Request,
			handshakeTiming.RequestStartedAt,
			handshakeTiming.RequestEndedAt,
			requestBodySize,
		)
		entry.Response.Metrics = completedHandshakeMetrics(
			entry.Response,
			handshakeTiming.ResponseStartedAt,
			handshakeTiming.ResponseEndedAt,
			0,
		)

		// send websocket basic info immediately
		s.trafficPublishMu.Lock()
		if s.storeTrafficEntry(entry) {
			s.emitTraffic(entry)
		}
		s.trafficPublishMu.Unlock()

		wsMsgs := s.newCaptureWebSocketMessages(entry)

		for {
			select {
			case <-ctx.Done():
				return
			case frame, ok := <-fw.Receive():
				if !ok {
					return
				}

				shouldStoreMessage := s.shouldStoreCaptureWebSocketMessage(entry, wsMsgs)
				var msg *WebSocketMessage
				if shouldStoreMessage {
					payload := frame.DataBuffer().Clone().Bytes()
					msg = normalizeWebSocketMessage(
						frame.Direction().String(),
						formatWebsocketMsgType(frame.MessageType()),
						payload,
					)
				}
				if err := frame.Invoke(); err != nil {
					logger.G().Error("frame invoke error", logx.String("error", err.Error()))
					if msg != nil {
						msg.Error = &TrafficError{
							Timestamp: time.Now(),
							Error:     err.Error(),
						}
					}
				}
				frame.Release()

				if !shouldStoreMessage || wsMsgs == nil {
					continue
				}
				if atomic.LoadInt32(&wsMsgs.liveState) == 0 {
					wsMsgs = nil
					continue
				}

				s.appendCaptureWebSocketMessage(entry, wsMsgs, msg)
			}
		}
	}
}

func (s *ProxyService) newCaptureWebSocketMessages(entry *TrafficEntry) *TrafficWsMsgs {
	s.captureLifecycleMu.RLock()
	defer s.captureLifecycleMu.RUnlock()
	if !s.isCurrentTrafficEntryLocked(entry) {
		return nil
	}
	value, _ := s.trafficWsMsgs.LoadOrStore(entry.ID, &TrafficWsMsgs{liveState: 1})
	return value.(*TrafficWsMsgs)
}

func (s *ProxyService) shouldStoreCaptureWebSocketMessage(entry *TrafficEntry, wsMsgs *TrafficWsMsgs) bool {
	s.captureLifecycleMu.RLock()
	defer s.captureLifecycleMu.RUnlock()
	if !s.isCurrentTrafficEntryLocked(entry) {
		return false
	}
	return s.shouldStoreWebSocketMessage(entry.ID, wsMsgs)
}

func (s *ProxyService) appendCaptureWebSocketMessage(entry *TrafficEntry, wsMsgs *TrafficWsMsgs, msg *WebSocketMessage) {
	s.captureLifecycleMu.RLock()
	defer s.captureLifecycleMu.RUnlock()
	if !s.isCurrentTrafficEntryLocked(entry) {
		return
	}
	s.appendCapturedWebSocketMessage(entry.ID, wsMsgs, msg)
}

func (s *ProxyService) shouldStoreWebSocketMessage(trafficID uint64, wsMsgs *TrafficWsMsgs) bool {
	if wsMsgs == nil || atomic.LoadInt32(&wsMsgs.liveState) == 0 {
		return false
	}

	wsMsgs.lock.Lock()
	maxMessages := s.getMaxWsMessages()
	if maxMessages <= 0 || len(wsMsgs.Messages) < maxMessages {
		wsMsgs.lock.Unlock()
		return true
	}
	newlyTruncated := !wsMsgs.Truncated
	wsMsgs.Truncated = true
	wsMsgs.lock.Unlock()
	if newlyTruncated {
		s.emitTrafficLiveUpdate(TrafficLiveUpdate{
			TrafficID: trafficID,
			Kind:      TrafficLiveUpdateWebSocketTruncated,
		})
	}
	return false
}

func (s *ProxyService) appendCapturedWebSocketMessage(trafficID uint64, wsMsgs *TrafficWsMsgs, msg *WebSocketMessage) {
	if wsMsgs == nil || msg == nil || atomic.LoadInt32(&wsMsgs.liveState) == 0 {
		return
	}

	wsMsgs.lock.Lock()
	maxMessages := s.getMaxWsMessages()
	if maxMessages > 0 && len(wsMsgs.Messages) >= maxMessages {
		newlyTruncated := !wsMsgs.Truncated
		wsMsgs.Truncated = true
		wsMsgs.lock.Unlock()
		if newlyTruncated {
			s.emitTrafficLiveUpdate(TrafficLiveUpdate{
				TrafficID: trafficID,
				Kind:      TrafficLiveUpdateWebSocketTruncated,
			})
		}
		return
	}
	messageIndex := len(wsMsgs.Messages)
	wsMsgs.Messages = append(wsMsgs.Messages, msg)
	wsMsgs.lock.Unlock()

	s.emitTrafficLiveUpdate(TrafficLiveUpdate{
		TrafficID:    trafficID,
		Kind:         TrafficLiveUpdateWebSocketMessage,
		MessageIndex: &messageIndex,
		Message:      msg,
	})
}

func (s *ProxyService) getCacheThreshold() int64 {
	if s.settingService == nil {
		return bodycache.MaxBodyCacheThresholdBytes
	}
	settings, err := s.settingService.Get()
	if err != nil || settings.CacheConfig == nil {
		return bodycache.MaxBodyCacheThresholdBytes
	}
	return settings.CacheConfig.BodyCacheThresholdBytes
}

func (s *ProxyService) getMaxWsMessages() int {
	if s.settingService == nil {
		return 1000
	}
	settings, err := s.settingService.Get()
	if err != nil || settings.CacheConfig == nil {
		return 1000
	}
	return settings.CacheConfig.MaxWsMessages
}

func (s *ProxyService) fillRequestHTTPMessage(req *http.Request, entry *TrafficEntry) {
	if entry.Request == nil {
		entry.Request = &HTTPMessage{}
	}
	entry.Request.Proto = req.Proto
	entry.Request.HeaderFields,
		entry.Request.HeadersTruncated,
		entry.Request.HeaderOrderUnavailable = completeRequestHeaderFields(
		req,
		mitmproxy.RequestWireHeaderBlocks(req),
	)
}

func (s *ProxyService) fillResponseHTTPMessage(rsp *http.Response, entry *TrafficEntry) {
	if entry.Response == nil {
		entry.Response = &HTTPMessage{}
	}
	entry.Response.Proto = rsp.Proto
	entry.Response.HeaderFields,
		entry.Response.HeadersTruncated,
		entry.Response.HeaderOrderUnavailable = completeResponseHeaderFields(
		rsp,
		mitmproxy.ResponseWireHeaderBlocks(rsp),
	)
}

func (s *ProxyService) fillResponseTrailers(rsp *http.Response, entry *TrafficEntry) bool {
	if rsp == nil || entry == nil || entry.Response == nil {
		return false
	}
	fields, truncated, orderUnavailable := completeResponseTrailerFields(
		rsp,
		mitmproxy.ResponseWireHeaderBlocks(rsp),
	)
	if fields == nil {
		return false
	}
	entry.Response.TrailerFields = fields
	entry.Response.TrailersTruncated = truncated
	entry.Response.TrailerOrderUnavailable = orderUnavailable
	s.trafficPublishMu.Lock()
	defer s.trafficPublishMu.Unlock()
	stored := s.storeTrafficResponseTrailers(entry)
	if stored == nil {
		return false
	}
	s.emitTrafficPatch(stored, newTrafficResponseTrailersPatch(stored))
	return true
}

func (s *ProxyService) fillEntryMetadataFromContext(ctx context.Context, entry *TrafficEntry) {
	_md, _ := metadata.FromContext(ctx)
	md := _md.MD()
	if entry.Metadata == nil {
		entry.Metadata = &Metadata{}
	}
	entry.Metadata.LocalSourceAddr = md.LocalAddrInfo.SourceAddr.String()
	entry.Metadata.LocalDestinationAddr = md.LocalAddrInfo.DestinationAddr.String()
	entry.Metadata.RemoteSourceAddr = md.RemoteAddrInfo.SourceAddr.String()
	entry.Metadata.RemoteDestinationAddr = md.RemoteAddrInfo.DestinationAddr.String()
	entry.Metadata.LocalConnectionEstablishedAt = md.LocalConnectionEstablishedTs
	entry.Metadata.RemoteConnectionEstablishedAt = md.RemoteConnectionEstablishedTs
	entry.Metadata.RequestProcessedAt = md.RequestProcessedTs

	if !md.SSLHandshakeCompletedTs.IsZero() {
		entry.Metadata.SSLHandshakeCompletedAt = md.SSLHandshakeCompletedTs
	}
	if md.TLSState != nil && md.ServerCertificate != nil {
		supportedVersions := make([]string, 0, len(md.TLSState.TLSVersions))
		for _, v := range md.TLSState.TLSVersions {
			supportedVersions = append(supportedVersions, formatSupportedTLSVersion(v))
		}
		supportedCipherSuites := make([]string, 0, len(md.TLSState.CipherSuites))
		for _, v := range md.TLSState.CipherSuites {
			supportedCipherSuites = append(supportedCipherSuites, formatSupportedTLSCipherSuite(v))
		}

		entry.Metadata.TLS = &TLSState{
			ServerName:            md.TLSState.ServerName,
			SupportedALPN:         md.TLSState.ALPN,
			SupportedVersion:      supportedVersions,
			SupportedCipherSuites: supportedCipherSuites,
			SelectedALPN:          md.TLSState.SelectedALPN,
			SelectedVersion:       tls.VersionName(md.TLSState.SelectedTLSVersion),
			SelectedCipherSuite:   tls.CipherSuiteName(md.TLSState.SelectedCipherSuite),
		}

		certIPAddrs := make([]string, 0, len(md.ServerCertificate.IPAddresses))
		for _, ip := range md.ServerCertificate.IPAddresses {
			certIPAddrs = append(certIPAddrs, ip.String())
		}
		entry.Metadata.Certificate = &ServerCertificate{
			Version:            md.ServerCertificate.Version,
			SerialNumber:       md.ServerCertificate.SerialNumberHex(),
			SignatureAlgorithm: md.ServerCertificate.SignatureAlgorithm.String(),
			Subject:            copyPkixName(&md.ServerCertificate.Subject),
			Issuer:             copyPkixName(&md.ServerCertificate.Issuer),
			NotBeforeMicros:    md.ServerCertificate.NotBefore.UnixMicro(),
			NotAfterMicros:     md.ServerCertificate.NotAfter.UnixMicro(),
			Sha1Fingerprint:    md.ServerCertificate.Sha1FingerprintHex(),
			Sha256Fingerprint:  md.ServerCertificate.Sha256FingerprintHex(),
			DNSNames:           md.ServerCertificate.DNSNames,
			IPAddresses:        certIPAddrs,
		}
	}
	s.fillEntryProcessFromTuple(processAttributionTuple(
		md.LocalAddrInfo.SourceAddr,
		md.LocalAddrInfo.DestinationAddr,
	), entry)
}

func formatSupportedTLSVersion(version uint16) string {
	if isTLSGreaseValue(version) {
		return formatTLSGreaseValue(version)
	}
	return tls.VersionName(version)
}

func formatSupportedTLSCipherSuite(cipherSuite uint16) string {
	if isTLSGreaseValue(cipherSuite) {
		return formatTLSGreaseValue(cipherSuite)
	}
	return tls.CipherSuiteName(cipherSuite)
}

func isTLSGreaseValue(value uint16) bool {
	return value&0x0f0f == 0x0a0a && value&0xff == value>>8
}

func formatTLSGreaseValue(value uint16) string {
	return fmt.Sprintf("GREASE (0x%04X)", value)
}

// emitTraffic publishes the one full snapshot that creates a live frontend
// entry. Callers that store immediately before emitting hold trafficPublishMu
// so this event stays ordered with all later patches.
func (s *ProxyService) emitTraffic(entry *TrafficEntry) {
	s.captureLifecycleMu.RLock()
	defer s.captureLifecycleMu.RUnlock()
	if !s.isCurrentTrafficEntryLocked(entry) {
		return
	}
	// Always emit the immutable snapshot held by trafficEntries. Interceptors
	// continue building their local copy while bodies and trailers stream, so
	// publishing that mutable pointer would race with JSON serialization and
	// history flushing.
	if stored, ok := s.trafficEntries.Get(entry.ID); ok {
		entry = stored
	}
	if s.emitTrafficHook != nil {
		s.emitTrafficHook(entry)
	}
	s.emitHighFrequencyFrontendEvent(trafficEventName, entry)
}

// emitTrafficPatch publishes a strongly typed incremental update. The entry is
// the immutable snapshot from which the patch was built.
func (s *ProxyService) emitTrafficPatch(entry *TrafficEntry, patch TrafficEntryPatch) {
	s.captureLifecycleMu.RLock()
	defer s.captureLifecycleMu.RUnlock()
	if !s.isCurrentTrafficEntryLocked(entry) || patch.TrafficID != entry.ID || patch.Revision != entry.Revision {
		return
	}
	if s.emitTrafficPatchHook != nil {
		s.emitTrafficPatchHook(patch)
	}
	s.emitHighFrequencyFrontendEvent(trafficPatchEventName, patch)
}

func (s *ProxyService) emitCapturedTrafficLiveUpdate(entry *TrafficEntry, update TrafficLiveUpdate) {
	s.captureLifecycleMu.RLock()
	defer s.captureLifecycleMu.RUnlock()
	if !s.isCurrentTrafficEntryLocked(entry) || update.TrafficID != entry.ID {
		return
	}
	s.emitTrafficLiveUpdate(update)
}

func (s *ProxyService) emitTrafficLiveUpdate(update TrafficLiveUpdate) {
	if update.TrafficID == 0 || s.liveTrafficDetailID.Load() != update.TrafficID {
		return
	}
	if s.emitTrafficLiveUpdateHook != nil {
		s.emitTrafficLiveUpdateHook(update)
	}
	s.emitHighFrequencyFrontendEvent(trafficLiveUpdateEventName, update)
}

// emitTrafficResetLocked publishes an unbounded marker while the caller holds
// captureLifecycleMu for writing. Traffic publishers hold the read lock until
// their batch publication completes, so the shared batch channel orders every
// old-generation event before this marker and every new-generation event after
// it, including when they are flushed in separate batches.
func (s *ProxyService) emitTrafficResetLocked() {
	s.emitUnboundedFrontendEvent(trafficResetEventName, map[string]uint64{
		"captureGeneration": s.captureGeneration,
	})
}

func (s *ProxyService) emitStatus() {
	if s.app == nil {
		return
	}
	_ = s.app.Event.Emit(statusEventName, s.GetStatus())
}

// markHistoryDirty marks the in-memory history as needing a new disk flush.
// Besides the dirty flag, it also bumps the flush generation so periodic and
// shutdown flushes can tell that the current history snapshot has changed.
func (s *ProxyService) markHistoryDirty() {
	s.flushGeneration.Add(1)
	s.historyDirty.Store(true)
}

func (s *ProxyService) runLifecycleOperationHook(operation string) {
	if s.lifecycleOperationHook != nil {
		s.lifecycleOperationHook(operation)
	}
}

func (s *ProxyService) runRequestDraftCacheOperationHook(operation string) {
	if s.requestDraftCacheOperationHook != nil {
		s.requestDraftCacheOperationHook(operation)
	}
}

//wails:ignore
func (s *ProxyService) CurrentHistoryKey() string {
	s.historyMetadataMu.RLock()
	defer s.historyMetadataMu.RUnlock()
	return s.currentHistoryMetadata.Key
}

func (s *ProxyService) currentHistoryAlias() string {
	s.historyMetadataMu.RLock()
	defer s.historyMetadataMu.RUnlock()
	return s.currentHistoryMetadata.Alias
}

func (s *ProxyService) currentHistoryMetadataSnapshot() HistoryMetadata {
	s.historyMetadataMu.RLock()
	defer s.historyMetadataMu.RUnlock()
	return s.currentHistoryMetadata
}

func (s *ProxyService) resetCurrentHistoryMetadata() {
	s.historyMetadataMu.Lock()
	defer s.historyMetadataMu.Unlock()
	s.currentHistoryMetadata = HistoryMetadata{
		Key:       generateHistoryKey(),
		Alias:     "",
		CreatedAt: time.Now().UnixMilli(),
	}
}

func (s *ProxyService) ensureBodyCacheLocked() {
	s.bodyCacheMu.Lock()
	defer s.bodyCacheMu.Unlock()
	s.ensureBodyCacheNoLock()
}

func (s *ProxyService) ensureBodyCacheNoLock() {
	if s.bodyCache != nil {
		return
	}
	if err := s.initializeBodyCacheNoLock(); err != nil {
		logger.G().Warnf("body cache: init failed: %v", err)
	}
}

func (s *ProxyService) initializeBodyCacheNoLock() error {
	baseDir, err := fs.GetBaseStorageDir()
	if err != nil {
		return fmt.Errorf("get storage dir: %w", err)
	}
	cacheRoot := filepath.Join(baseDir, "cache")
	if err := fs.EnsurePrivateDir(cacheRoot); err != nil {
		return fmt.Errorf("secure cache directory: %w", err)
	}
	cache, cacheErr := bodycache.New(filepath.Join(cacheRoot, uuid.New().String()))
	if cacheErr != nil {
		return cacheErr
	}
	s.bodyCache = cache
	return nil
}

func (s *ProxyService) emitWebSocketSessionEvent(event WebSocketSessionEvent) {
	if s.emitWebSocketSessionEventHook != nil {
		s.emitWebSocketSessionEventHook(event)
	}
	s.emitUnboundedFrontendEvent(webSocketSessionEventName, event)
}

func (s *ProxyService) emitHTTPRequestEvent(event HTTPRequestStreamEvent) {
	if s.emitHTTPRequestEventHook != nil {
		s.emitHTTPRequestEventHook(event)
	}
	s.emitUnboundedFrontendEvent(httpRequestEventName, event)
}

func (s *ProxyService) emitHighFrequencyFrontendEvent(name string, data any) {
	if s.frontendEventBatcher != nil {
		err := s.frontendEventBatcher.Publish(name, data)
		if err != nil &&
			!errors.Is(err, errFrontendEventDropped) &&
			!errors.Is(err, errFrontendEventBatcherClosed) {
			logger.G().Warnf("Failed to enqueue frontend event %s: %v", name, err)
		}
		return
	}
	if s.app != nil {
		_ = s.app.Event.Emit(name, data)
	}
}

func (s *ProxyService) emitUnboundedFrontendEvent(name string, data any) {
	if s.frontendEventBatcher != nil {
		err := s.frontendEventBatcher.PublishUnbounded(name, data)
		if err != nil && !errors.Is(err, errFrontendEventBatcherClosed) {
			logger.G().Warnf("Failed to enqueue frontend event %s: %v", name, err)
		}
		return
	}
	if s.app != nil {
		_ = s.app.Event.Emit(name, data)
	}
}

//wails:ignore
func (s *ProxyService) EmitRequestEditorFileDrop(paths []string, dataFileDropTarget string) {
	if s.app == nil || len(paths) == 0 {
		return
	}
	_ = s.app.Event.Emit(requestEditorFileDropEventName, RequestEditorFileDropEvent{
		Paths:              paths,
		DataFileDropTarget: dataFileDropTarget,
	})
}

// GetTraffic returns the newest frontend-sized window (without bodies). Older
// entries remain retained for the complete history file and HAR export.
func (s *ProxyService) GetTraffic() []*TrafficEntry {
	s.captureLifecycleMu.RLock()
	defer s.captureLifecycleMu.RUnlock()
	return s.trafficEntries.TailValues(frontendTrafficEntryLimit)
}

func (s *ProxyService) getAllTraffic() []*TrafficEntry {
	s.captureLifecycleMu.RLock()
	defer s.captureLifecycleMu.RUnlock()
	return s.trafficEntries.Values()
}

// SetLiveTrafficDetail selects the only traffic entry whose live body or
// WebSocket updates should be delivered to the frontend. ID zero disables it.
func (s *ProxyService) SetLiveTrafficDetail(id uint64) {
	s.liveTrafficDetailID.Store(id)
}

// DeleteTraffic deletes traffic entries
func (s *ProxyService) DeleteTraffic(id []int64) {
	s.captureLifecycleMu.RLock()
	defer s.captureLifecycleMu.RUnlock()
	for _, i := range id {
		s.deleteTrafficEntryCaptureLocked(uint64(i))
	}
}

func (s *ProxyService) deleteTrafficEntry(id uint64) {
	s.captureLifecycleMu.RLock()
	defer s.captureLifecycleMu.RUnlock()
	s.deleteTrafficEntryCaptureLocked(id)
}

func (s *ProxyService) deleteTrafficEntryCaptureLocked(id uint64) {
	s.liveTrafficDetailID.CompareAndSwap(id, 0)
	s.trafficAttributionMu.Lock()
	off := s.deactivateTrafficProcessBinding(id)
	old, exist := s.trafficEntries.Delete(id)
	if exist && old.lifecycle != nil {
		old.lifecycle.deleted.Store(true)
	}
	s.trafficAttributionMu.Unlock()
	if off != nil {
		off()
	}
	if !exist {
		return
	}
	if v, loaded := s.trafficBodies.LoadAndDelete(id); loaded {
		bodies := v.(*TrafficBodies)
		atomic.StoreInt32(&bodies.liveState, 0)
		bodies.lockReqBody.Lock()
		if bodies.requestBody != nil {
			bodies.requestBody.Abort()
			bodies.requestBody = nil
		}
		bodies.lockReqBody.Unlock()
		bodies.lockRespBody.Lock()
		if bodies.responseBody != nil {
			bodies.responseBody.Abort()
			bodies.responseBody = nil
		}
		bodies.lockRespBody.Unlock()
	}
	if v, loaded := s.trafficWsMsgs.LoadAndDelete(id); loaded {
		wsMsgs := v.(*TrafficWsMsgs)
		atomic.StoreInt32(&wsMsgs.liveState, 0)
		wsMsgs.lock.Lock()
		wsMsgs.Messages = nil
		wsMsgs.Truncated = false
		wsMsgs.lock.Unlock()
	}
	s.bodyCacheMu.RLock()
	if s.bodyCache != nil {
		s.bodyCache.Delete(id)
	}
	s.bodyCacheMu.RUnlock()
	atomic.AddInt64(&s.trafficEntries.Statistics.Total, -1)
	switch old.Type {
	case "http", "https":
		atomic.AddInt64(&s.trafficEntries.Statistics.TotalHTTP, -1)
	case "ws", "wss":
		atomic.AddInt64(&s.trafficEntries.Statistics.TotalWS, -1)
	case "tcp":
		atomic.AddInt64(&s.trafficEntries.Statistics.TotalTCP, -1)
	}
	s.markHistoryDirty()
}

func (s *ProxyService) loadBodyBytesAsReaderNoLock(bodies *TrafficBodies, id uint64, isRequest bool, cache *bodycache.BodyCache) (io.ReadCloser, int64, bool, error) {
	if isRequest {
		bodies.lockReqBody.RLock()
		if bodies.requestBody != nil {
			reader, size, cacheBacked, err := bodies.requestBody.Reader()
			bodies.lockReqBody.RUnlock()
			return reader, size, cacheBacked, err
		}
		bodies.lockReqBody.RUnlock()

		if cache != nil && cache.Has(id, bodycache.KindRequest) {
			reader, size, err := cache.Reader(id, bodycache.KindRequest)
			if err != nil {
				return nil, 0, false, err
			}
			return reader, size, true, nil
		}
		return nil, 0, false, nil
	}

	bodies.lockRespBody.RLock()
	if bodies.responseBody != nil {
		reader, size, cacheBacked, err := bodies.responseBody.Reader()
		bodies.lockRespBody.RUnlock()
		return reader, size, cacheBacked, err
	}
	bodies.lockRespBody.RUnlock()

	if cache != nil && cache.Has(id, bodycache.KindResponse) {
		reader, size, err := cache.Reader(id, bodycache.KindResponse)
		if err != nil {
			return nil, 0, false, err
		}
		return reader, size, true, nil
	}
	return nil, 0, false, nil
}

func (s *ProxyService) loadBodyBytesAsReader(bodies *TrafficBodies, id uint64, isRequest bool) (io.ReadCloser, int64, error) {
	s.bodyCacheMu.RLock()
	reader, size, cacheBacked, err := s.loadBodyBytesAsReaderNoLock(bodies, id, isRequest, s.bodyCache)
	if err != nil {
		s.bodyCacheMu.RUnlock()
		return nil, 0, err
	}
	if cacheBacked {
		return &bodyCacheReadCloser{ReadCloser: reader, release: s.bodyCacheMu.RUnlock}, size, nil
	}
	s.bodyCacheMu.RUnlock()
	return reader, size, nil
}

type bodyCacheReadCloser struct {
	io.ReadCloser
	release func()
	once    sync.Once
	err     error
}

func (r *bodyCacheReadCloser) Close() error {
	r.once.Do(func() {
		r.err = r.ReadCloser.Close()
		r.release()
	})
	return r.err
}

type bodyCacheReadLease struct {
	remaining atomic.Int32
	release   func()
}

func newBodyCacheReadLease(count int, release func()) *bodyCacheReadLease {
	lease := &bodyCacheReadLease{release: release}
	lease.remaining.Store(int32(count))
	return lease
}

func (l *bodyCacheReadLease) releaseOne() {
	if l.remaining.Add(-1) == 0 {
		l.release()
	}
}

func (s *ProxyService) getTrafficBodyViewInner(id uint64) (trafficBodyViewInner, error) {
	var result trafficBodyViewInner
	var bodyErr error
	entry, exist := s.trafficEntries.Get(id)
	if !exist {
		return result, fmt.Errorf("traffic entry %d not found", id)
	}
	if v, ok := s.trafficBodies.Load(id); ok {
		bodies := v.(*TrafficBodies)
		s.bodyCacheMu.RLock()
		reqBodyReader, reqBodySize, reqCacheBacked, reqErr := s.loadBodyBytesAsReaderNoLock(bodies, id, true, s.bodyCache)
		if reqErr != nil {
			if reqBodyReader != nil {
				_ = reqBodyReader.Close()
			}
			reqBodyReader = nil
			reqBodySize = 0
			reqCacheBacked = false
			result.RequestBodyUnavailable = true
		}
		respBodyReader, respBodySize, respCacheBacked, respErr := s.loadBodyBytesAsReaderNoLock(bodies, id, false, s.bodyCache)
		if respErr != nil {
			if respBodyReader != nil {
				_ = respBodyReader.Close()
			}
			respBodyReader = nil
			respBodySize = 0
			respCacheBacked = false
			result.ResponseBodyUnavailable = true
		}
		cacheReaderCount := 0
		if reqCacheBacked {
			cacheReaderCount++
		}
		if respCacheBacked {
			cacheReaderCount++
		}
		if cacheReaderCount == 0 {
			s.bodyCacheMu.RUnlock()
		} else {
			lease := newBodyCacheReadLease(cacheReaderCount, s.bodyCacheMu.RUnlock)
			if reqCacheBacked {
				reqBodyReader = &bodyCacheReadCloser{ReadCloser: reqBodyReader, release: lease.releaseOne}
			}
			if respCacheBacked {
				respBodyReader = &bodyCacheReadCloser{ReadCloser: respBodyReader, release: lease.releaseOne}
			}
		}

		result.RequestBodySize = reqBodySize
		result.ResponseBodySize = respBodySize
		bodies.lockReqBody.RLock()
		requestBodyUTF8Valid := bodies.requestBody == nil || bodies.requestBody.UTF8Valid()
		bodies.lockReqBody.RUnlock()
		bodies.lockRespBody.RLock()
		responseBodyUTF8Valid := bodies.responseBody == nil || bodies.responseBody.UTF8Valid()
		bodies.lockRespBody.RUnlock()

		reqCT := ""
		reqCE := ""
		if entry.Request != nil {
			reqCT = firstHeaderFieldValue(entry.Request.HeaderFields, "Content-Type")
			reqCE = firstHeaderFieldValue(entry.Request.HeaderFields, "Content-Encoding")
		}
		respCT := ""
		respCE := ""
		if entry.Response != nil {
			respCT = firstHeaderFieldValue(entry.Response.HeaderFields, "Content-Type")
			respCE = firstHeaderFieldValue(entry.Response.HeaderFields, "Content-Encoding")
		}

		if shouldEncodeBodyForTrafficView(reqCT, reqCE, reqBodySize) || !requestBodyUTF8Valid {
			result.RequestBodyEncoding = "base64"
		}
		result.RequestBodyReader = reqBodyReader

		if shouldEncodeBodyForTrafficView(respCT, respCE, respBodySize) || !responseBodyUTF8Valid {
			result.ResponseBodyEncoding = "base64"
		}
		result.ResponseBodyReader = respBodyReader

		if reqErr != nil {
			bodyErr = errors.Join(bodyErr, fmt.Errorf("read request body: %w", reqErr))
		}
		if respErr != nil {
			bodyErr = errors.Join(bodyErr, fmt.Errorf("read response body: %w", respErr))
		}
	}
	if wsMsgsValue, ok := s.trafficWsMsgs.Load(id); ok {
		wsMsgs := wsMsgsValue.(*TrafficWsMsgs)
		wsMsgs.lock.RLock()
		result.WebSocketMessages = append([]*WebSocketMessage(nil), wsMsgs.Messages...)
		result.WsMsgsTruncated = wsMsgs.Truncated
		wsMsgs.lock.RUnlock()
	}
	return result, bodyErr
}

// GetTrafficBodyView returns a single entry with bodies
func (s *ProxyService) GetTrafficBodyView(id uint64) (*TrafficBodyView, error) {
	bvi, err := s.getTrafficBodyViewInner(id)
	defer func() {
		bvi.closeReqBodyReaderSafely()
		bvi.closeRspBodyReaderSafely()
	}()
	if err != nil {
		return nil, err
	}
	return convertToTrafficBodyView(&bvi)
}

// isBinaryContentType returns true for non-text content types that should be base64-encoded
func isBinaryContentType(ct string) bool {
	if ct == "" {
		return false
	}
	ct = strings.ToLower(ct)
	// Strip parameters (e.g. "; charset=utf-8")
	if idx := strings.Index(ct, ";"); idx != -1 {
		ct = ct[:idx]
	}
	ct = strings.TrimSpace(ct)
	// image/* except SVG (which is XML text)
	if strings.HasPrefix(ct, "image/") && ct != "image/svg+xml" {
		return true
	}
	// audio and video
	if strings.HasPrefix(ct, "audio/") || strings.HasPrefix(ct, "video/") {
		return true
	}
	// common binary types
	switch ct {
	case "application/octet-stream",
		"application/pdf",
		"application/zip",
		"application/x-zip-compressed",
		"application/gzip",
		"application/x-gzip",
		"application/x-tar",
		"application/x-rar-compressed",
		"application/wasm",
		"application/x-protobuf",
		"application/protobuf",
		"font/woff",
		"font/woff2",
		"font/ttf",
		"font/otf":
		return true
	}
	return false
}

func isServerSentEventsContentType(contentType string) bool {
	mediaType, _, err := mime.ParseMediaType(contentType)
	return err == nil && strings.EqualFold(mediaType, "text/event-stream")
}

func shouldStreamSSEUpdates(contentType, contentEncoding string) bool {
	return isServerSentEventsContentType(contentType) && !hasUnsupportedContentEncoding(contentEncoding)
}

// ClearTraffic removes all stored traffic and its persisted current snapshot.
// The disk pair is deleted before the in-memory reset so a deletion failure
// cannot make cleared traffic reappear after a crash or restart.
func (s *ProxyService) ClearTraffic() error {
	s.clearDataMu.Lock()
	defer s.clearDataMu.Unlock()
	s.captureLifecycleMu.Lock()
	defer s.captureLifecycleMu.Unlock()
	if err := s.removeCurrentHistoryFiles(s.CurrentHistoryKey()); err != nil {
		return fmt.Errorf("clear current history snapshot: %w", err)
	}
	s.captureGeneration++
	s.bodyCacheMu.RLock()
	defer s.bodyCacheMu.RUnlock()
	s.clearTrafficWithBodyCache(s.bodyCache)
	s.historyDirty.Store(false)
	s.lastFlushGeneration.Store(s.flushGeneration.Load())
	s.emitTrafficResetLocked()
	return nil
}

func (s *ProxyService) clearTrafficWithBodyCache(cache *bodycache.BodyCache) {
	s.liveTrafficDetailID.Store(0)
	s.trafficAttributionMu.Lock()
	offs := s.deactivateAllTrafficProcessBindings()
	s.trafficEntries.Clear()
	s.trafficAttributionMu.Unlock()
	for _, off := range offs {
		off()
	}
	s.trafficBodies.Range(func(key, value any) bool {
		bodies := value.(*TrafficBodies)
		atomic.StoreInt32(&bodies.liveState, 0)
		bodies.lockReqBody.Lock()
		if bodies.requestBody != nil {
			bodies.requestBody.Abort()
			bodies.requestBody = nil
		}
		bodies.lockReqBody.Unlock()
		bodies.lockRespBody.Lock()
		if bodies.responseBody != nil {
			bodies.responseBody.Abort()
			bodies.responseBody = nil
		}
		bodies.lockRespBody.Unlock()
		id, ok := key.(uint64)
		if ok && cache != nil {
			cache.Delete(id)
		}
		return true
	})
	s.trafficBodies.Clear()
	s.trafficWsMsgs.Range(func(key, value any) bool {
		wsMsgs := value.(*TrafficWsMsgs)
		atomic.StoreInt32(&wsMsgs.liveState, 0)
		wsMsgs.lock.Lock()
		wsMsgs.Messages = nil
		wsMsgs.Truncated = false
		wsMsgs.lock.Unlock()
		return true
	})
	s.trafficWsMsgs.Clear()
	s.resetStatistics()
}

func (s *ProxyService) resetStatistics() {
	atomic.StoreInt64(&s.trafficEntries.Statistics.Total, 0)
	atomic.StoreInt64(&s.trafficEntries.Statistics.TotalHTTP, 0)
	atomic.StoreInt64(&s.trafficEntries.Statistics.TotalWS, 0)
	atomic.StoreInt64(&s.trafficEntries.Statistics.TotalTCP, 0)
}

// GetStatistics returns current traffic statistics
func (s *ProxyService) GetStatistics() TrafficStatistics {
	s.captureLifecycleMu.RLock()
	defer s.captureLifecycleMu.RUnlock()
	return TrafficStatistics{
		Total:     atomic.LoadInt64(&s.trafficEntries.Statistics.Total),
		TotalHTTP: atomic.LoadInt64(&s.trafficEntries.Statistics.TotalHTTP),
		TotalWS:   atomic.LoadInt64(&s.trafficEntries.Statistics.TotalWS),
		TotalTCP:  atomic.LoadInt64(&s.trafficEntries.Statistics.TotalTCP),
	}
}

//wails:ignore
func (s *ProxyService) ResendRequestWithTrafficEntry(ctx context.Context, cfg ResendConfig, entry *TrafficEntry, reqBodyBytes []byte) (ResendResult, error) {
	if entry == nil {
		return ResendResult{}, errors.New("traffic entry is required")
	}
	if entry.Type != "http" && entry.Type != "https" {
		return ResendResult{}, fmt.Errorf("can only resend HTTP/HTTPS requests, got %s", entry.Type)
	}

	proxyConfig, err := s.getProxyConfig()
	if err != nil {
		return ResendResult{}, fmt.Errorf("failed to get proxy config: %w", err)
	}
	reqBodyBytes, err = encodeDecodedRequestBodyForResend(entry, reqBodyBytes)
	if err != nil {
		return ResendResult{}, err
	}
	parsedURL, err := url.ParseRequestURI(strings.TrimSpace(entry.URL))
	if err != nil {
		return ResendResult{}, fmt.Errorf("invalid resend url: %w", err)
	}
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return ResendResult{}, fmt.Errorf("unsupported resend url scheme: %s", parsedURL.Scheme)
	}
	protocol := inferOrderedRequestProtocol(entry.Request)
	resendFields := []HTTPHeaderField(nil)
	hasRequestBody := len(reqBodyBytes) > 0
	if entry.Request != nil {
		resendFields = ordinaryRequestHeaderFields(entry.Request.HeaderFields)
		hasRequestBody = hasRequestBody ||
			firstHeaderFieldValue(entry.Request.HeaderFields, "Content-Length") != "" ||
			firstHeaderFieldValue(entry.Request.HeaderFields, "Transfer-Encoding") != ""
	}
	resendFields, err = normalizeSyntheticHeaderFields(
		strings.ToUpper(entry.Method),
		parsedURL,
		protocol,
		resendFields,
		"",
	)
	if err != nil {
		return ResendResult{}, fmt.Errorf("prepare resend headers: %w", err)
	}
	resendFields = reconcileSyntheticBodyFraming(
		resendFields,
		protocol,
		int64(len(reqBodyBytes)),
		hasRequestBody,
	)

	s.mu.Lock()
	running := s.running
	s.mu.Unlock()

	var proxyURL *url.URL
	if cfg.UseProxy && running {
		proxyURL, _ = url.Parse(fmt.Sprintf("http://%s:%d", proxyConfig.Host, proxyConfig.Port))
	} else if !cfg.UseProxy && cfg.UpstreamProxy != "" {
		proxyURL, _ = url.Parse(cfg.UpstreamProxy)
	}

	transport := newSyntheticRoundTripper(proxyConfig, protocol, proxyURL, TLSClientHelloGolang)
	defer transport.CloseIdleConnections()
	if protocol == SendRequestProtocolHTTP2 && parsedURL.Scheme == "http" && proxyURL != nil && !isSOCKSProxyURL(proxyURL) {
		return ResendResult{}, fmt.Errorf("HTTP/2 prior-knowledge requests over http:// do not support HTTP forward proxies")
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
	}

	count := cfg.Count
	if count <= 0 {
		count = 1
	}

	logTarget := formatRawURLLogTarget(entry.URL)
	started := time.Now()
	logger.G().Infof(
		"Resend request start: traffic_id=%d method=%s target=%s count=%d delay_ms=%d interval_ms=%d use_mitm_proxy=%t custom_upstream=%t body_bytes=%d",
		entry.ID,
		entry.Method,
		logTarget,
		count,
		cfg.DelayMs,
		cfg.IntervalMs,
		cfg.UseProxy,
		strings.TrimSpace(cfg.UpstreamProxy) != "",
		len(reqBodyBytes),
	)

	if cfg.DelayMs > 0 {
		if err := waitForResendDelay(ctx, time.Duration(cfg.DelayMs)*time.Millisecond); err != nil {
			return ResendResult{}, err
		}
	}

	var result ResendResult
	for i := 0; i < count; i++ {
		if i > 0 && cfg.IntervalMs > 0 {
			if err := waitForResendDelay(ctx, time.Duration(cfg.IntervalMs)*time.Millisecond); err != nil {
				return result, err
			}
		}
		if err := ctx.Err(); err != nil {
			return result, err
		}

		var bodyReader io.Reader
		if hasRequestBody {
			bodyReader = bytes.NewReader(reqBodyBytes)
		}

		req, err := http.NewRequestWithContext(ctx, entry.Method, entry.URL, bodyReader)
		if err != nil {
			result.Failed++
			logger.G().Warnf(
				"Resend request build failed: traffic_id=%d target=%s attempt=%d/%d error=%v",
				entry.ID,
				logTarget,
				i+1,
				count,
				err,
			)
			continue
		}

		applySyntheticHeaderFields(req, resendFields)
		if protocol == SendRequestProtocolAuto {
			applyFallbackUserAgent(req.Header)
			req, err = http.WithRequestHeaderOrder(req, http.HeaderOrder{
				Headers: syntheticHeaderNameOrder(resendFields),
			})
		} else {
			req, err = http.WithRequestHeaderBlocks(req, syntheticRequestHeaderBlock(protocol, resendFields), nil)
		}
		if err != nil {
			result.Failed++
			logger.G().Warnf(
				"Resend request header preparation failed: traffic_id=%d target=%s attempt=%d/%d error=%v",
				entry.ID,
				logTarget,
				i+1,
				count,
				err,
			)
			continue
		}

		resp, err := client.Do(req)
		if err != nil {
			if contextErr := ctx.Err(); contextErr != nil {
				return result, contextErr
			}
			result.Failed++
			logger.G().Warnf(
				"Resend request send failed: traffic_id=%d target=%s attempt=%d/%d error=%v",
				entry.ID,
				logTarget,
				i+1,
				count,
				err,
			)
			continue
		}
		_, copyErr := io.Copy(io.Discard, resp.Body)
		closeErr := resp.Body.Close()
		if contextErr := ctx.Err(); contextErr != nil {
			return result, contextErr
		}
		if copyErr != nil || closeErr != nil {
			result.Failed++
			logger.G().Warnf(
				"Resend response body read failed: traffic_id=%d target=%s attempt=%d/%d copy_error=%v close_error=%v",
				entry.ID,
				logTarget,
				i+1,
				count,
				copyErr,
				closeErr,
			)
			continue
		}
		result.Success++
		logger.G().Debugf(
			"Resend request attempt succeeded: traffic_id=%d target=%s attempt=%d/%d status_code=%d",
			entry.ID,
			logTarget,
			i+1,
			count,
			resp.StatusCode,
		)
	}
	logger.G().Infof(
		"Resend request completed: traffic_id=%d target=%s success=%d failed=%d duration=%s",
		entry.ID,
		logTarget,
		result.Success,
		result.Failed,
		time.Since(started).Round(time.Millisecond),
	)
	return result, nil
}

func waitForResendDelay(ctx context.Context, delay time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *ProxyService) ResendRequest(callCtx context.Context, id uint64, cfg ResendConfig) (ResendResult, error) {
	entry, exist := s.trafficEntries.Get(id)
	if !exist {
		return ResendResult{}, fmt.Errorf("traffic entry %d not found", id)
	}
	// Snapshot request body while holding the lock
	var reqBodyBytes []byte
	if v, ok := s.trafficBodies.Load(id); ok {
		bodies := v.(*TrafficBodies)
		reqBodyReader, _, err := s.loadBodyBytesAsReader(bodies, id, true)
		if err != nil {
			return ResendResult{}, err
		}
		if reqBodyReader != nil {
			reqBodyBytes, err = io.ReadAll(reqBodyReader)
			closeErr := reqBodyReader.Close()
			if err != nil {
				return ResendResult{}, err
			}
			if closeErr != nil {
				return ResendResult{}, closeErr
			}
		}
	}

	result, err := s.ResendRequestWithTrafficEntry(callCtx, cfg, entry, reqBodyBytes)
	if errors.Is(err, context.Canceled) {
		// Wails has already rejected the frontend CancellablePromise with CancelError.
		// Resolve the backend call quietly so the late result is discarded without a
		// CancelledRejectionError in the browser runtime.
		return result, nil
	}
	return result, err
}

func (s *ProxyService) SendHTTPRequest(
	callCtx context.Context,
	cfg SendRequestConfig,
	method, targetURL string,
	headerFields []HTTPHeaderField,
	body SendRequestBody,
) (SendRequestResponse, error) {
	method, parsedURL, err := normalizeSyntheticMethodAndURL(method, targetURL)
	if err != nil {
		return SendRequestResponse{}, err
	}
	targetURL = parsedURL.String()
	protocol, err := normalizeSendRequestProtocol(cfg.Protocol)
	if err != nil {
		return SendRequestResponse{}, err
	}
	var http2Fingerprint *http.Fingerprint
	if fingerprintValue := strings.TrimSpace(cfg.HTTP2Fingerprint); protocol != SendRequestProtocolHTTP1 && fingerprintValue != "" {
		fingerprint, parseErr := http.ParseFingerprint(fingerprintValue)
		if parseErr != nil {
			return SendRequestResponse{}, fmt.Errorf("invalid HTTP/2 fingerprint: %w", parseErr)
		}
		http2Fingerprint = &fingerprint
	}

	var pluginSession HTTPRequestPluginSession
	var requestBodyFile *HTTPRequestPluginBodyFile
	pluginRequestSnapshot := HTTPRequestPluginRequest{
		Method: method, URL: targetURL,
		HeaderFields: append([]HTTPHeaderField(nil), headerFields...),
		Body:         cloneSendRequestBody(body),
	}
	inlineScriptEnabled := cfg.InlinePythonScript != nil && cfg.InlinePythonScript.Enabled
	if !cfg.DisablePlugins || inlineScriptEnabled {
		executionID, executionIDErr := normalizeHTTPRequestPluginExecutionID(cfg.PluginExecutionID)
		if executionIDErr != nil {
			return SendRequestResponse{}, executionIDErr
		}
		if executionID == "" {
			executionID = uuid.NewString()
		}
		if runner := s.httpRequestRunner(); runner != nil {
			pluginSession, err = runner.BeginRequest(callCtx, HTTPRequestPluginBeginRequest{
				ExecutionID:           executionID,
				Timestamp:             time.Now().UnixMicro(),
				OriginalMethod:        method,
				OriginalURL:           targetURL,
				DisableManagedPlugins: cfg.DisablePlugins,
				InlinePythonScript:    cfg.InlinePythonScript,
				Transport: HTTPRequestPluginTransport{
					Protocol: protocol, ProxyMode: cfg.ProxyMode,
					TLSClientHelloID: cfg.TLSClientHelloID,
					HTTP2Fingerprint: strings.TrimSpace(cfg.HTTP2Fingerprint),
				},
			})
			if err != nil {
				return httpRequestPluginStartFailure(executionID, err), nil
			}
		} else if inlineScriptEnabled {
			return httpRequestPluginStartFailure(executionID, errors.New("Python plugin runner is unavailable")), nil
		}
	}
	if pluginSession != nil {
		defer pluginSession.Close()
		pluginResult := pluginSession.RunRequest(callCtx, HTTPRequestPluginRequest{
			Method: method, URL: targetURL,
			HeaderFields: append([]HTTPHeaderField(nil), headerFields...),
			Body:         cloneSendRequestBody(body),
		})
		if pluginResult.Blocked {
			return SendRequestResponse{
				Outcome: RequestOutcomeBlockedRequest, PluginExecution: pluginSession.Execution(),
			}, nil
		}
		if pluginResult.Failed {
			return SendRequestResponse{
				Outcome: RequestOutcomePluginFailed, PluginExecution: pluginSession.Execution(),
			}, nil
		}
		method = pluginResult.Request.Method
		targetURL = pluginResult.Request.URL
		headerFields = append([]HTTPHeaderField(nil), pluginResult.Request.HeaderFields...)
		body = cloneSendRequestBody(pluginResult.Request.Body)
		requestBodyFile = cloneHTTPRequestPluginBodyFile(pluginResult.Request.BodyFile)
		pluginRequestSnapshot = cloneHTTPRequestPluginRequest(pluginResult.Request)
		method, parsedURL, err = normalizeSyntheticMethodAndURL(method, targetURL)
		if err != nil {
			return httpRequestPluginRequestValidationFailure(pluginSession.Execution(), err), nil
		}
		targetURL = parsedURL.String()
	}
	logTarget := formatURLLogTarget(parsedURL)

	proxyConfig, err := s.getProxyConfig()
	if err != nil {
		return SendRequestResponse{}, fmt.Errorf("failed to get proxy config: %w", err)
	}

	requestBody, err := s.buildSendRequestBody(callCtx, body, requestBodyFile)
	if err != nil {
		logger.G().Warnf(
			"HTTP request body build failed: method=%s target=%s body_type=%s error=%v",
			method,
			logTarget,
			body.BodyType,
			err,
		)
		return SendRequestResponse{}, err
	}
	if err := encodeSendRequestBodyForContentEncoding(headerFields, requestBody); err != nil {
		if requestBody.close != nil {
			requestBody.close() //nolint:errcheck
		}
		logger.G().Warnf(
			"HTTP request body encoding failed: method=%s target=%s body_type=%s error=%v",
			method,
			logTarget,
			body.BodyType,
			err,
		)
		return SendRequestResponse{}, err
	}
	if requestBody.close != nil {
		defer requestBody.close()
	}
	headerFields, err = normalizeSyntheticHeaderFields(
		strings.ToUpper(method),
		parsedURL,
		protocol,
		headerFields,
		requestBody.contentType,
	)
	if err != nil {
		return SendRequestResponse{}, err
	}
	if protocol == SendRequestProtocolAuto {
		if err := validateSyntheticAutoHeaderOrder(headerFields); err != nil {
			return SendRequestResponse{}, err
		}
	}
	headerFields = reconcileSyntheticBodyFraming(
		headerFields,
		protocol,
		requestBody.contentLength,
		requestBody.reader != nil,
	)
	headerFields = applyFallbackUserAgentField(headerFields, protocol)

	proxyURL, err := s.resolveSendRequestProxy(cfg, proxyConfig)
	if err != nil {
		logger.G().Warnf(
			"HTTP request proxy resolve failed: method=%s target=%s proxy_mode=%s error=%v",
			method,
			logTarget,
			cfg.ProxyMode,
			err,
		)
		return SendRequestResponse{}, err
	}
	if protocol == SendRequestProtocolHTTP2 && parsedURL.Scheme == "http" && proxyURL != nil && !isSOCKSProxyURL(proxyURL) {
		return SendRequestResponse{}, fmt.Errorf("HTTP/2 prior-knowledge requests over http:// do not support HTTP forward proxies")
	}

	transport := newSyntheticRoundTripper(proxyConfig, protocol, proxyURL, cfg.TLSClientHelloID)
	streamingResponse := false
	defer func() {
		if !streamingResponse {
			transport.CloseIdleConnections()
		}
	}()
	client := &http.Client{
		Transport: transport,
	}
	if http2Fingerprint != nil && protocol == SendRequestProtocolHTTP2 {
		redirectTemplate := slices.Clone(headerFields)
		client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return errors.New("stopped after 10 redirects")
			}
			rebuilt, rebuildErr := rebuildSyntheticHTTP2FingerprintRedirectRequest(
				req,
				redirectTemplate,
				http2Fingerprint,
			)
			if rebuildErr != nil {
				return fmt.Errorf("rebuild HTTP/2 fingerprint redirect headers: %w", rebuildErr)
			}
			*req = *rebuilt
			return nil
		}
	}
	if cfg.TimeoutMs > 0 {
		client.Timeout = time.Duration(cfg.TimeoutMs) * time.Millisecond
	}

	started := time.Now()
	logger.G().Infof(
		"HTTP request start: method=%s target=%s proxy_mode=%s protocol=%s timeout_ms=%d body_type=%s body_bytes=%d header_fields=%d",
		strings.ToUpper(method),
		logTarget,
		cfg.ProxyMode,
		protocol,
		cfg.TimeoutMs,
		body.BodyType,
		requestBody.contentLength,
		len(headerFields),
	)

	requestCtx, cancelRequest, detachCallCancellation := s.newRequestOperationContext(callCtx)
	defer detachCallCancellation()
	defer func() {
		if !streamingResponse {
			cancelRequest()
		}
	}()
	req, err := http.NewRequestWithContext(requestCtx, strings.ToUpper(method), targetURL, requestBody.reader)
	if err != nil {
		logger.G().Warnf(
			"HTTP request build failed: method=%s target=%s duration=%s error=%v",
			strings.ToUpper(method),
			logTarget,
			time.Since(started).Round(time.Millisecond),
			err,
		)
		return SendRequestResponse{}, err
	}
	if requestBody.contentLength >= 0 {
		req.ContentLength = requestBody.contentLength
	}
	if requestBody.getBody != nil {
		req.GetBody = requestBody.getBody
	}

	applySyntheticHeaderFields(req, headerFields)
	if http2Fingerprint != nil && protocol != SendRequestProtocolHTTP1 {
		if protocol == SendRequestProtocolAuto {
			req, err = http.WithRequestHeaderOrder(req, http.HeaderOrder{
				Headers: syntheticHeaderNameOrder(headerFields),
			})
		}
		if err == nil {
			req, err = http.WithRequestFingerprint(req, *http2Fingerprint)
		}
		if err == nil {
			if protocol == SendRequestProtocolAuto {
				req, err = http.WithRequestHeaderOrder(req, http.HeaderOrder{
					Headers: syntheticHeaderNameOrder(headerFields),
				})
			} else {
				req, err = http.WithRequestHeaderBlocks(
					req,
					syntheticFingerprintRequestHeaderBlock(headerFields, http2Fingerprint.PseudoHeaderOrder),
					nil,
				)
			}
		}
	} else if protocol == SendRequestProtocolAuto {
		req, err = http.WithRequestHeaderOrder(req, http.HeaderOrder{
			Headers: syntheticHeaderNameOrder(headerFields),
		})
	} else {
		req, err = http.WithRequestHeaderBlocks(req, syntheticRequestHeaderBlock(protocol, headerFields), nil)
	}
	if err != nil {
		return SendRequestResponse{}, fmt.Errorf("prepare ordered HTTP request headers: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		logger.G().Warnf(
			"HTTP request failed: method=%s target=%s proxy_mode=%s duration=%s error=%v",
			strings.ToUpper(method),
			logTarget,
			cfg.ProxyMode,
			time.Since(started).Round(time.Millisecond),
			err,
		)
		return SendRequestResponse{}, err
	}
	defer func() {
		if !streamingResponse {
			_ = resp.Body.Close()
		}
	}()

	bodyReader := resp.Body
	contentEncoding := resp.Header.Get("Content-Encoding")
	if contentEncoding != "" {
		bodyReader, err = getDecodedReader(resp.Body, contentEncoding)
		if err != nil {
			dumpResponse, _ := httputil.DumpResponse(resp, false)
			logger.G().Warnf(
				"HTTP response decode failed: method=%s target=%s status_code=%d content_encoding=%s duration=%s error=%v",
				strings.ToUpper(method),
				logTarget,
				resp.StatusCode,
				contentEncoding,
				time.Since(started).Round(time.Millisecond),
				err,
			)
			return SendRequestResponse{}, fmt.Errorf("failed to get decoded response body reader: %w, response dump: %s", err, string(dumpResponse))
		}
		defer func() {
			if !streamingResponse {
				_ = bodyReader.Close()
			}
		}()
	}

	response := SendRequestResponse{
		Outcome:    RequestOutcomeCompleted,
		StatusCode: resp.StatusCode,
		StatusText: resp.Status,
		Protocol:   resp.Proto,
	}
	response.HeaderFields,
		response.HeadersTruncated,
		response.HeaderOrderUnavailable = completeResponseHeaderFields(
		resp,
		syntheticResponseHeaderBlocks(resp),
	)

	responseContentType := resp.Header.Get("Content-Type")
	if isServerSentEventsContentType(responseContentType) {
		if pluginSession != nil {
			originalHeaderFields := append([]HTTPHeaderField(nil), response.HeaderFields...)
			pluginResult := pluginSession.RunResponse(requestCtx, HTTPRequestPluginResponse{
				StatusCode:    response.StatusCode,
				StatusText:    response.StatusText,
				Protocol:      response.Protocol,
				HeaderFields:  append([]HTTPHeaderField(nil), response.HeaderFields...),
				TrailerFields: []HTTPHeaderField{},
				BodyKind:      "unavailable", BodyAvailable: false, Streaming: true,
				ContentType: responseContentType,
				Request:     cloneHTTPRequestPluginRequest(pluginRequestSnapshot),
			})
			if pluginResult.Blocked {
				return SendRequestResponse{
					Outcome: RequestOutcomeBlockedResponse, PluginExecution: pluginSession.Execution(),
				}, nil
			}
			if pluginResult.Failed {
				response.Outcome = RequestOutcomeCompletedWithPluginError
			} else {
				response.StatusCode = pluginResult.Response.StatusCode
				response.StatusText = httpRequestPluginStatusText(pluginResult.Response.StatusCode)
				response.HeaderFields = append([]HTTPHeaderField(nil), pluginResult.Response.HeaderFields...)
				if !slices.Equal(originalHeaderFields, response.HeaderFields) {
					response.HeadersTruncated = false
					response.HeaderOrderUnavailable = false
				}
			}
			response.PluginExecution = pluginSession.Execution()
		}
		sessionID := generateHistoryKey()
		if hasUnsupportedContentEncoding(contentEncoding) {
			response.BodyEncoding = "base64"
		}
		response.Streaming = true
		response.StreamSessionID = sessionID
		session := &httpRequestStreamSession{
			id:        sessionID,
			target:    logTarget,
			started:   started,
			cancel:    cancelRequest,
			response:  resp,
			body:      bodyReader,
			transport: transport,
		}
		s.httpRequestStreams.Store(sessionID, session)
		logger.G().Infof(
			"HTTP request SSE stream opened: session_id=%s method=%s target=%s status_code=%d protocol=%s duration=%s",
			sessionID,
			strings.ToUpper(method),
			logTarget,
			resp.StatusCode,
			resp.Proto,
			time.Since(started).Round(time.Millisecond),
		)
		go s.runHTTPRequestStreamReadLoop(session)
		if err := detachRequestCallCancellation(callCtx, detachCallCancellation, cancelRequest); err != nil {
			s.finalizeHTTPRequestStream(sessionID, "closed", nil)
			return SendRequestResponse{}, err
		}
		streamingResponse = true
		return response, nil
	}

	var respBodyBytes []byte
	var respBodyFile *HTTPRequestPluginBodyFile
	if pluginSession == nil {
		respBodyBytes, err = io.ReadAll(bodyReader)
	} else {
		responseStore, storeErr := bodyspool.New("flowlens-python-body-")
		if storeErr != nil {
			return SendRequestResponse{}, fmt.Errorf("create Python response body storage: %w", storeErr)
		}
		defer func() {
			if closeErr := responseStore.Close(); closeErr != nil {
				logger.G().Warnf("Python request plugin response Body temporary storage cleanup failed: %T", closeErr)
			}
		}()
		payload, readErr := responseStore.Read(requestCtx, bodyReader, bodyspool.DefaultInlineLimit)
		if readErr == nil {
			respBodyBytes = payload.Inline
			if payload.File != nil {
				respBodyFile = &HTTPRequestPluginBodyFile{
					Path: payload.File.Path, Name: "response-body",
					Size: payload.File.Size, ReadOnly: true,
				}
			}
		}
		err = readErr
	}
	if err != nil {
		logger.G().Warnf(
			"HTTP response read failed: method=%s target=%s status_code=%d duration=%s error=%v",
			strings.ToUpper(method),
			logTarget,
			resp.StatusCode,
			time.Since(started).Round(time.Millisecond),
			err,
		)
		return SendRequestResponse{}, err
	}

	response.TrailerFields,
		response.TrailersTruncated,
		response.TrailerOrderUnavailable = completeResponseTrailerFields(
		resp,
		syntheticResponseHeaderBlocks(resp),
	)

	pluginBodyKind := ""
	if pluginSession != nil {
		if respBodyFile != nil {
			pluginBodyKind, err = httpRequestPluginResponseBodyKindFromFile(requestCtx, responseContentType, respBodyFile)
		} else {
			pluginBodyKind = httpRequestPluginResponseBodyKind(responseContentType, respBodyBytes)
		}
		if err != nil {
			return SendRequestResponse{}, fmt.Errorf("classify Python response body: %w", err)
		}
		originalStatusCode := response.StatusCode
		originalStatusText := response.StatusText
		originalHeaderFields := append([]HTTPHeaderField(nil), response.HeaderFields...)
		originalTrailerFields := append([]HTTPHeaderField(nil), response.TrailerFields...)
		originalHeadersTruncated := response.HeadersTruncated
		originalHeaderOrderUnavailable := response.HeaderOrderUnavailable
		originalTrailersTruncated := response.TrailersTruncated
		originalTrailerOrderUnavailable := response.TrailerOrderUnavailable
		originalBodyBytes := append([]byte(nil), respBodyBytes...)
		originalBodyFile := cloneHTTPRequestPluginBodyFile(respBodyFile)
		originalBodyKind := pluginBodyKind
		var responseStageErr error
		pluginResult := pluginSession.RunResponse(requestCtx, HTTPRequestPluginResponse{
			StatusCode:    response.StatusCode,
			StatusText:    response.StatusText,
			Protocol:      response.Protocol,
			HeaderFields:  append([]HTTPHeaderField(nil), response.HeaderFields...),
			TrailerFields: append([]HTTPHeaderField(nil), response.TrailerFields...),
			Body:          append([]byte(nil), respBodyBytes...), BodyFile: cloneHTTPRequestPluginBodyFile(respBodyFile),
			BodyKind: pluginBodyKind, BodyAvailable: true,
			ContentType: responseContentType,
			Request:     cloneHTTPRequestPluginRequest(pluginRequestSnapshot),
		})
		if pluginResult.Blocked {
			return SendRequestResponse{
				Outcome: RequestOutcomeBlockedResponse, PluginExecution: pluginSession.Execution(),
			}, nil
		}
		if !pluginResult.Failed {
			pluginResult.Response, responseStageErr = ValidateHTTPRequestPluginResponseContext(requestCtx, pluginResult.Response)
			pluginResult.Failed = responseStageErr != nil
		}
		if pluginResult.Failed {
			response.Outcome = RequestOutcomeCompletedWithPluginError
			response.StatusCode = originalStatusCode
			response.StatusText = originalStatusText
			response.HeaderFields = originalHeaderFields
			response.TrailerFields = originalTrailerFields
			response.HeadersTruncated = originalHeadersTruncated
			response.HeaderOrderUnavailable = originalHeaderOrderUnavailable
			response.TrailersTruncated = originalTrailersTruncated
			response.TrailerOrderUnavailable = originalTrailerOrderUnavailable
			respBodyBytes = originalBodyBytes
			respBodyFile = originalBodyFile
			pluginBodyKind = originalBodyKind
		} else {
			response.StatusCode = pluginResult.Response.StatusCode
			response.StatusText = httpRequestPluginStatusText(pluginResult.Response.StatusCode)
			response.HeaderFields = append([]HTTPHeaderField(nil), pluginResult.Response.HeaderFields...)
			response.TrailerFields = append([]HTTPHeaderField(nil), pluginResult.Response.TrailerFields...)
			respBodyBytes = append([]byte(nil), pluginResult.Response.Body...)
			respBodyFile = cloneHTTPRequestPluginBodyFile(pluginResult.Response.BodyFile)
			pluginBodyKind = pluginResult.Response.BodyKind
			if !slices.Equal(originalHeaderFields, response.HeaderFields) {
				response.HeadersTruncated = false
				response.HeaderOrderUnavailable = false
			}
			if !slices.Equal(originalTrailerFields, response.TrailerFields) {
				response.TrailersTruncated = false
				response.TrailerOrderUnavailable = false
			}
		}
		response.PluginExecution = pluginSession.Execution()
		if responseStageErr != nil {
			response.PluginExecution = appendHTTPRequestPluginResponseDiagnostic(
				response.PluginExecution, "invalid_result", responseStageErr,
			)
		}

		encodeBodyAsBase64 := pluginBodyKind == "binary"
		// The current Wails DTO exposes Body as a string, so even file-backed
		// plugin results must be materialized once here. Keeping files through the
		// hook chain avoids every earlier full-body byte slice and Worker frame.
		bodyValue, bodySize, materializeErr := materializeHTTPRequestPluginResponseBody(
			requestCtx, respBodyBytes, respBodyFile, encodeBodyAsBase64,
		)
		if materializeErr != nil && response.Outcome == RequestOutcomeCompleted {
			response.Outcome = RequestOutcomeCompletedWithPluginError
			response.StatusCode = originalStatusCode
			response.StatusText = originalStatusText
			response.HeaderFields = originalHeaderFields
			response.TrailerFields = originalTrailerFields
			response.HeadersTruncated = originalHeadersTruncated
			response.HeaderOrderUnavailable = originalHeaderOrderUnavailable
			response.TrailersTruncated = originalTrailersTruncated
			response.TrailerOrderUnavailable = originalTrailerOrderUnavailable
			respBodyBytes = originalBodyBytes
			respBodyFile = originalBodyFile
			pluginBodyKind = originalBodyKind
			response.PluginExecution = appendHTTPRequestPluginResponseDiagnostic(
				response.PluginExecution, "body_materialization_failed", materializeErr,
			)
			encodeBodyAsBase64 = pluginBodyKind == "binary"
			bodyValue, bodySize, materializeErr = materializeHTTPRequestPluginResponseBody(
				requestCtx, respBodyBytes, respBodyFile, encodeBodyAsBase64,
			)
		}
		if materializeErr != nil {
			return SendRequestResponse{}, fmt.Errorf("materialize HTTP response body: %w", materializeErr)
		}
		response.Body = bodyValue
		if encodeBodyAsBase64 && bodySize > 0 {
			response.BodyEncoding = "base64"
		}
		logger.G().Infof(
			"HTTP request completed: method=%s target=%s status_code=%d protocol=%s response_bytes=%d body_encoding=%s duration=%s",
			strings.ToUpper(method), logTarget, resp.StatusCode, resp.Proto,
			bodySize, response.BodyEncoding, time.Since(started).Round(time.Millisecond),
		)
		return response, nil
	}

	encodeBodyAsBase64 := isBinaryContentType(responseContentType)
	response.Body, _, err = materializeHTTPRequestPluginResponseBody(requestCtx, respBodyBytes, nil, encodeBodyAsBase64)
	if err != nil {
		return SendRequestResponse{}, err
	}
	if encodeBodyAsBase64 && len(respBodyBytes) > 0 {
		response.BodyEncoding = "base64"
	}
	logger.G().Infof(
		"HTTP request completed: method=%s target=%s status_code=%d protocol=%s response_bytes=%d body_encoding=%s duration=%s",
		strings.ToUpper(method),
		logTarget,
		resp.StatusCode,
		resp.Proto,
		len(respBodyBytes),
		response.BodyEncoding,
		time.Since(started).Round(time.Millisecond),
	)
	return response, nil
}

func (s *ProxyService) DisconnectHTTPRequestStream(sessionID string) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID != "" {
		logger.G().Infof("HTTP request SSE disconnect requested: session_id=%s", sessionID)
	}
	s.finalizeHTTPRequestStream(sessionID, "closed", nil)
	return nil
}

func (s *ProxyService) runHTTPRequestStreamReadLoop(session *httpRequestStreamSession) {
	buffer := make([]byte, CHUNK_SIZE)
	for {
		n, err := session.body.Read(buffer)
		if n > 0 {
			startOffset := session.offset.Load()
			nextOffset := startOffset + int64(n)
			session.offset.Store(nextOffset)
			eventOffset := startOffset
			s.emitHTTPRequestEvent(HTTPRequestStreamEvent{
				SessionID:   session.id,
				EventType:   "chunk",
				Offset:      &eventOffset,
				ChunkBase64: base64.StdEncoding.EncodeToString(buffer[:n]),
			})
		}
		if err == nil {
			continue
		}
		if errors.Is(err, io.EOF) {
			s.finalizeHTTPRequestStream(session.id, "complete", nil)
			return
		}
		s.finalizeHTTPRequestStream(session.id, "error", err)
		return
	}
}

func (s *ProxyService) finalizeHTTPRequestStream(sessionID, eventType string, streamErr error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return
	}
	value, ok := s.httpRequestStreams.LoadAndDelete(sessionID)
	if !ok {
		return
	}
	session := value.(*httpRequestStreamSession)
	session.cancel()
	if session.body != nil && (session.response == nil || session.body != session.response.Body) {
		_ = session.body.Close()
	}
	if session.response != nil && session.response.Body != nil {
		_ = session.response.Body.Close()
	}
	if session.transport != nil {
		session.transport.CloseIdleConnections()
	}

	endOffset := session.offset.Load()
	event := HTTPRequestStreamEvent{
		SessionID: sessionID,
		EventType: eventType,
		Offset:    &endOffset,
	}
	if eventType == "complete" && session.response != nil {
		event.TrailerFields,
			event.TrailersTruncated,
			event.TrailerOrderUnavailable = completeResponseTrailerFields(
			session.response,
			syntheticResponseHeaderBlocks(session.response),
		)
	}
	if streamErr != nil {
		event.Error = streamErr.Error()
	}
	s.emitHTTPRequestEvent(event)

	duration := time.Since(session.started).Round(time.Millisecond)
	if streamErr != nil {
		logger.G().Warnf(
			"HTTP request SSE stream ended: session_id=%s target=%s event_type=%s response_bytes=%d duration=%s error=%v",
			sessionID,
			session.target,
			eventType,
			endOffset,
			duration,
			streamErr,
		)
		return
	}
	logger.G().Infof(
		"HTTP request SSE stream ended: session_id=%s target=%s event_type=%s response_bytes=%d duration=%s",
		sessionID,
		session.target,
		eventType,
		endOffset,
		duration,
	)
}

func (s *ProxyService) closeAllHTTPRequestStreams() {
	var sessionIDs []string
	s.httpRequestStreams.Range(func(key, _ any) bool {
		if sessionID, ok := key.(string); ok {
			sessionIDs = append(sessionIDs, sessionID)
		}
		return true
	})
	for _, sessionID := range sessionIDs {
		s.finalizeHTTPRequestStream(sessionID, "closed", nil)
	}
}

func (s *ProxyService) closeAllWebSocketSessions() {
	var sessionIDs []string
	s.webSocketSessions.Range(func(key, _ any) bool {
		if sessionID, ok := key.(string); ok {
			sessionIDs = append(sessionIDs, sessionID)
		}
		return true
	})
	for _, sessionID := range sessionIDs {
		s.finalizeWebSocketSession(sessionID, "closed", nil, "closed")
	}
}

func (s *ProxyService) ConnectWebSocket(
	callCtx context.Context,
	req WebSocketConnectRequest,
) (WebSocketConnectResponse, error) {
	targetURL := strings.TrimSpace(req.URL)
	if targetURL == "" {
		return WebSocketConnectResponse{}, fmt.Errorf("url is required")
	}

	parsedURL, err := url.ParseRequestURI(targetURL)
	if err != nil {
		return WebSocketConnectResponse{}, fmt.Errorf("invalid url: %w", err)
	}
	if parsedURL.Scheme != "ws" && parsedURL.Scheme != "wss" {
		return WebSocketConnectResponse{}, fmt.Errorf("unsupported url scheme: %s", parsedURL.Scheme)
	}
	if err := normalizeSyntheticURLHostname(parsedURL); err != nil {
		return WebSocketConnectResponse{}, fmt.Errorf("invalid url host: %w", err)
	}
	targetURL = parsedURL.String()
	logTarget := formatWebSocketLogTarget(parsedURL)

	proxyConfig, err := s.getProxyConfig()
	if err != nil {
		return WebSocketConnectResponse{}, fmt.Errorf("failed to get proxy config: %w", err)
	}

	proxyURL, err := s.resolveSendRequestProxy(SendRequestConfig{
		ProxyMode:   req.ProxyMode,
		CustomProxy: req.CustomProxy,
		TimeoutMs:   req.TimeoutMs,
	}, proxyConfig)
	if err != nil {
		logger.G().Warnf(
			"WebSocket session proxy resolve failed: target=%s proxy_mode=%s error=%v",
			logTarget,
			req.ProxyMode,
			err,
		)
		return WebSocketConnectResponse{}, err
	}

	requestHeaderFields := applyFallbackUserAgentField(req.HeaderFields, SendRequestProtocolHTTP1)
	headers, requestHeaderOrder, err := buildWebSocketHeaders(requestHeaderFields)
	if err != nil {
		return WebSocketConnectResponse{}, err
	}
	logger.G().Infof(
		"WebSocket session connect start: target=%s proxy_mode=%s timeout_ms=%d headers=%d",
		logTarget,
		req.ProxyMode,
		req.TimeoutMs,
		len(headers),
	)
	dialer := websocket.Dialer{RequestHeaderOrder: requestHeaderOrder}
	if proxyURL != nil {
		dialer.Proxy = http.ProxyURL(proxyURL)
	}
	sessionCtx, cancel, detachCallCancellation := s.newRequestOperationContext(callCtx)
	defer detachCallCancellation()
	handshakeCtx := sessionCtx
	cancelHandshakeTimeout := func() {}
	if req.TimeoutMs > 0 {
		handshakeCtx, cancelHandshakeTimeout = context.WithTimeout(
			sessionCtx,
			time.Duration(req.TimeoutMs)*time.Millisecond,
		)
	}
	defer cancelHandshakeTimeout()

	var detachHandshakeCancellation func() bool
	netDialer := &net.Dialer{}
	dialer.NetDialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		address, dialErr := syntheticASCIIAddress(address)
		if dialErr != nil {
			return nil, dialErr
		}
		netConn, dialErr := netDialer.DialContext(ctx, network, address)
		if dialErr != nil {
			return nil, dialErr
		}
		// The websocket dialer does not interrupt its response-header read when
		// ctx is cancelled, so close the underlying connection during handshake.
		detachHandshakeCancellation = context.AfterFunc(ctx, func() {
			_ = netConn.Close()
		})
		return netConn, nil
	}
	profileID := resolveUTLSClientHelloID(req.TLSClientHelloID)
	if parsedURL.Scheme == "wss" {
		// Dial the proxy tunnel and target TLS connection as one operation so the
		// selected ClientHello is also used after CONNECT.
		dialer.Proxy = nil
		dialer.NetDialTLSContext = func(ctx context.Context, network, address string) (net.Conn, error) {
			tlsConn, dialErr := dialSyntheticTLS(
				ctx,
				network,
				address,
				parsedURL.Hostname(),
				proxyURL,
				profileID,
				[]string{syntheticALPNHTTP1},
				true,
				proxyConfig.SkipVerifyTLS,
			)
			if dialErr != nil {
				return nil, dialErr
			}
			detachHandshakeCancellation = context.AfterFunc(ctx, func() {
				_ = tlsConn.Close()
			})
			return tlsConn, nil
		}
	} else if proxyURL != nil && normalizedProxyScheme(proxyURL) == "https" {
		// For ws:// over an HTTPS proxy, the only TLS hop is the proxy itself.
		dialer.NetDialTLSContext = func(ctx context.Context, network, address string) (net.Conn, error) {
			tlsConn, dialErr := dialSyntheticTLSDirect(
				ctx,
				network,
				address,
				proxyURL.Hostname(),
				profileID,
				[]string{syntheticALPNHTTP1},
				true,
				proxyConfig.SkipVerifyTLS,
			)
			if dialErr != nil {
				return nil, dialErr
			}
			detachHandshakeCancellation = context.AfterFunc(ctx, func() {
				_ = tlsConn.Close()
			})
			return tlsConn, nil
		}
	}

	conn, resp, err := dialer.DialContext(handshakeCtx, targetURL, headers)
	if detachHandshakeCancellation != nil {
		detachHandshakeCancellation()
	}
	responseHeaderFields := []HTTPHeaderField{}
	var responseHeadersTruncated bool
	responseHeaderOrderUnavailable := true
	if resp != nil {
		responseHeaderFields,
			responseHeadersTruncated,
			responseHeaderOrderUnavailable = completeResponseHeaderFields(
			resp,
			http.ResponseHeaderBlocks(resp),
		)
	}
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		err = normalizeWebSocketHandshakeError(handshakeCtx, err)
		cancel()
		logger.G().Errorf("WebSocket session connect failed: target=%s error=%v", logTarget, err)
		return WebSocketConnectResponse{}, err
	}
	cancelHandshakeTimeout()
	context.AfterFunc(sessionCtx, func() {
		_ = conn.Close()
	})

	responseStatusCode := 0
	responseStatusText := ""
	responseProtocol := ""
	if resp != nil {
		responseStatusCode = resp.StatusCode
		responseStatusText = resp.Status
		responseProtocol = resp.Proto
	}

	sessionID := generateHistoryKey()
	session := &webSocketSession{
		id:      sessionID,
		target:  logTarget,
		started: time.Now(),
		conn:    conn,
		cancel:  cancel,
	}
	s.webSocketSessions.Store(sessionID, session)
	logger.G().Infof(
		"WebSocket session connected: session_id=%s target=%s status_code=%d protocol=%s",
		sessionID,
		logTarget,
		responseStatusCode,
		responseProtocol,
	)
	s.emitWebSocketSessionEvent(WebSocketSessionEvent{
		SessionID: sessionID,
		EventType: "connected",
		Status:    "connected",
	})

	go s.runWebSocketReadLoop(sessionCtx, session)
	if err := detachRequestCallCancellation(callCtx, detachCallCancellation, cancel); err != nil {
		s.finalizeWebSocketSession(sessionID, "closed", nil, "closed")
		return WebSocketConnectResponse{}, err
	}

	return WebSocketConnectResponse{
		SessionID:              sessionID,
		Status:                 "connected",
		StatusCode:             responseStatusCode,
		StatusText:             responseStatusText,
		Protocol:               responseProtocol,
		HeaderFields:           responseHeaderFields,
		HeadersTruncated:       responseHeadersTruncated,
		HeaderOrderUnavailable: responseHeaderOrderUnavailable,
	}, nil
}

func (s *ProxyService) SendWebSocketMessage(req WebSocketSendRequest) error {
	session, ok := s.getWebSocketSession(req.SessionID)
	if !ok {
		logger.G().Warnf("WebSocket session send failed: session_id=%s error=session not found", strings.TrimSpace(req.SessionID))
		return fmt.Errorf("websocket session not found")
	}

	messageType, payload, normalizedType, err := resolveWebSocketSendPayload(req)
	if err != nil {
		logger.G().Warnf(
			"WebSocket session send payload failed: session_id=%s target=%s msg_type=%s error=%v",
			session.id,
			session.target,
			req.MsgType,
			err,
		)
		return err
	}

	if err := session.conn.WriteMessage(messageType, payload); err != nil {
		logger.G().Errorf(
			"WebSocket session send failed: session_id=%s target=%s msg_type=%s bytes=%d error=%v",
			session.id,
			session.target,
			normalizedType,
			len(payload),
			err,
		)
		s.finalizeWebSocketSession(req.SessionID, "error", err, "error")
		return err
	}
	logger.G().Debugf(
		"WebSocket session message sent: session_id=%s target=%s msg_type=%s bytes=%d",
		session.id,
		session.target,
		normalizedType,
		len(payload),
	)

	s.emitWebSocketSessionEvent(WebSocketSessionEvent{
		SessionID: req.SessionID,
		EventType: "message",
		Status:    "connected",
		Message:   normalizeWebSocketMessage("send", normalizedType, payload),
	})
	return nil
}

func (s *ProxyService) DisconnectWebSocket(sessionID string) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID != "" {
		logger.G().Infof("WebSocket session disconnect requested: session_id=%s", sessionID)
	}
	s.finalizeWebSocketSession(strings.TrimSpace(sessionID), "closed", nil, "closed")
	return nil
}

func normalizeWebSocketHandshakeError(ctx context.Context, err error) error {
	if contextErr := ctx.Err(); contextErr != nil {
		return contextErr
	}
	if deadline, ok := ctx.Deadline(); ok && !time.Now().Before(deadline) {
		return context.DeadlineExceeded
	}
	return err
}

func (s *ProxyService) newRequestOperationContext(
	callCtx context.Context,
) (context.Context, context.CancelFunc, func() bool) {
	operationCtx, cancel := context.WithCancel(s.appContext())
	return operationCtx, cancel, context.AfterFunc(callCtx, cancel)
}

func detachRequestCallCancellation(
	callCtx context.Context,
	detach func() bool,
	cancel context.CancelFunc,
) error {
	if err := callCtx.Err(); err != nil {
		cancel()
		return err
	}
	if detach() {
		return nil
	}
	cancel()
	if err := callCtx.Err(); err != nil {
		return err
	}
	return context.Canceled
}

type sendRequestBodyResult struct {
	reader        io.Reader
	getBody       func() (io.ReadCloser, error)
	contentType   string
	contentLength int64
	close         func() error
}

func imageBodyFileExtension(contentType string) string {
	mediaType := strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	switch mediaType {
	case "image/png":
		return ".png"
	case "image/jpeg":
		return ".jpg"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	case "image/svg+xml":
		return ".svg"
	case "image/avif":
		return ".avif"
	default:
		return ".bin"
	}
}

func textBodyFileExtension(contentType string) string {
	mediaType := strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	switch mediaType {
	case "application/json":
		return ".json"
	case "application/xml", "text/xml":
		return ".xml"
	case "text/html":
		return ".html"
	case "text/css":
		return ".css"
	case "application/javascript", "text/javascript":
		return ".js"
	case "image/svg+xml":
		return ".svg"
	default:
		return ".txt"
	}
}

func bodyToFileExtension(contentType string) string {
	if isBinaryContentType(contentType) {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0])), "image/") {
			return imageBodyFileExtension(contentType)
		}
		return ".bin"
	}
	return textBodyFileExtension(contentType)
}

func normalizeBodyToFileSavePath(contentType string, path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if filepath.Ext(path) != "" {
		return path
	}
	return path + bodyToFileExtension(contentType)
}

func decodeBodyToFileBytes(req SaveBodyToFileRequest) ([]byte, error) {
	body := req.Body
	if body == "" {
		return nil, fmt.Errorf("body is empty")
	}
	if req.BodyEncoding == "base64" {
		decoded, err := base64.StdEncoding.DecodeString(body)
		if err != nil {
			return nil, fmt.Errorf("decode body: %w", err)
		}
		return decoded, nil
	}
	return string2Bytes(body), nil
}

func writeBodyToFile(path string, data []byte) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("save path is required")
	}
	if len(data) == 0 {
		return fmt.Errorf("body is empty")
	}
	return os.WriteFile(path, data, 0o644)
}

func (s *ProxyService) SaveBodyToFile(req SaveBodyToFileRequest) error {
	data, err := decodeBodyToFileBytes(req)
	if err != nil {
		return err
	}

	selectedPath := normalizeBodyToFileSavePath(req.ContentType, req.Path)
	if selectedPath == "" {
		return fmt.Errorf("save path is required")
	}

	if err := writeBodyToFile(selectedPath, data); err != nil {
		logger.G().Warnf("Save body to file failed: path=%s bytes=%d error=%v", selectedPath, len(data), err)
		return err
	}
	logger.G().Infof("Saved body to file: path=%s bytes=%d", selectedPath, len(data))
	return nil
}

func (s *ProxyService) ResolveRequestDraftFile(path string) (*RequestDraftFile, error) {
	s.requestDraftCacheOpsMu.RLock()
	defer s.requestDraftCacheOpsMu.RUnlock()
	s.runRequestDraftCacheOperationHook("resolve")

	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("file path is required")
	}

	stat, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("failed to stat selected file: %w", err)
	}
	if stat.IsDir() {
		return nil, fmt.Errorf("selected path is a directory")
	}

	return &RequestDraftFile{
		Path: path,
		Name: filepath.Base(path),
		Size: stat.Size(),
	}, nil
}

func (s *ProxyService) RecoverRequestBodyForEditing(
	requestURL string,
	headerFields []HTTPHeaderField,
	bodyView TrafficBodyView,
) (RequestBodyRecoveryResult, error) {
	s.requestDraftCacheOpsMu.RLock()
	defer s.requestDraftCacheOpsMu.RUnlock()
	s.runRequestDraftCacheOperationHook("recover")

	contentType := firstHeaderFieldValue(headerFields, "Content-Type")
	bodyBytes, decodeWarnings := decodeTrafficRequestBody(bodyView)

	result := RequestBodyRecoveryResult{
		BodyType: inferRequestRecoveryBodyType(contentType),
		Text:     bodyView.RequestBody,
		Warnings: append([]string(nil), decodeWarnings...),
	}

	if len(bodyBytes) == 0 {
		if len(result.Warnings) == 0 && strings.TrimSpace(bodyView.RequestBody) == "" {
			result.Text = ""
		}
		return result, nil
	}

	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil && strings.TrimSpace(contentType) != "" {
		result.Warnings = append(result.Warnings, fmt.Sprintf("invalid content-type %q: %v", contentType, err))
	}

	if strings.EqualFold(mediaType, "multipart/form-data") {
		multipartResult, warnings := s.recoverRequestMultipartBody(bodyBytes, params)
		result.BodyType = SendRequestBodyTypeFormData
		result.Text = ""
		result.FormData = multipartResult
		result.Warnings = append(result.Warnings, warnings...)
		return result, nil
	}

	if strings.EqualFold(mediaType, "application/x-www-form-urlencoded") {
		urlEncodedResult, warnings := recoverRequestURLEncodedBody(bodyBytes)
		result.BodyType = SendRequestBodyTypeURLEncoded
		result.Text = ""
		result.URLEncoded = urlEncodedResult
		result.Warnings = append(result.Warnings, warnings...)
		return result, nil
	}

	if strings.TrimSpace(contentType) == "" {
		result.BodyType = SendRequestBodyTypeText
		result.Text = bytes2String(bodyBytes)
		return result, nil
	}

	if shouldRecoverRequestSingleFile(requestURL, contentType, bodyView, bodyBytes) {
		fileResult, warnings := s.recoverRequestSingleFile(bodyBytes, requestURL, contentType)
		result.BodyType = SendRequestBodyTypeFile
		result.Text = ""
		result.File = fileResult
		result.Warnings = append(result.Warnings, warnings...)
		return result, nil
	}

	result.Text = bytes2String(bodyBytes)
	return result, nil
}

func (s *ProxyService) buildSendRequestBody(
	ctx context.Context,
	body SendRequestBody,
	bodyFile *HTTPRequestPluginBodyFile,
) (*sendRequestBodyResult, error) {
	if bodyFile != nil {
		if err := validateHTTPRequestPluginBodyFile(ctx, bodyFile, string(body.BodyType)); err != nil {
			return nil, err
		}
		contentType, err := sendRequestBodyContentType(body.BodyType)
		if err != nil {
			return nil, err
		}
		return buildFileBackedSendRequestBody(bodyFile.Path, bodyFile.Size, contentType)
	}
	switch body.BodyType {
	case "", SendRequestBodyTypeNone:
		return &sendRequestBodyResult{
			contentLength: 0,
		}, nil
	case SendRequestBodyTypeJSON:
		return buildReplayableMemoryBody([]byte(body.Text), "application/json"), nil
	case SendRequestBodyTypeText:
		return buildReplayableMemoryBody([]byte(body.Text), "text/plain; charset=utf-8"), nil
	case SendRequestBodyTypeXML:
		return buildReplayableMemoryBody([]byte(body.Text), "application/xml"), nil
	case SendRequestBodyTypeBinary:
		value, err := base64.StdEncoding.DecodeString(body.Text)
		if err != nil {
			return nil, fmt.Errorf("binary body must contain Base64 data: %w", err)
		}
		return buildReplayableMemoryBody(value, "application/octet-stream"), nil
	case SendRequestBodyTypeFile:
		if body.File == nil || strings.TrimSpace(body.File.Path) == "" {
			return nil, fmt.Errorf("file body requires file path")
		}
		path := strings.TrimSpace(body.File.Path)
		stat, err := os.Stat(path)
		if err != nil {
			return nil, fmt.Errorf("invalid file body path: %w", err)
		}
		if stat.IsDir() {
			return nil, fmt.Errorf("invalid file body path: is a directory")
		}
		return buildFileBackedSendRequestBody(path, stat.Size(), "application/octet-stream")
	case SendRequestBodyTypeFormData:
		return buildStreamingMultipartBody(body.FormData)
	case SendRequestBodyTypeURLEncoded:
		return buildURLEncodedBody(body.URLEncoded), nil
	default:
		return nil, fmt.Errorf("unsupported body type: %s", body.BodyType)
	}
}

func sendRequestBodyContentType(bodyType SendRequestBodyType) (string, error) {
	switch bodyType {
	case SendRequestBodyTypeJSON:
		return "application/json", nil
	case SendRequestBodyTypeText:
		return "text/plain; charset=utf-8", nil
	case SendRequestBodyTypeXML:
		return "application/xml", nil
	case SendRequestBodyTypeBinary, SendRequestBodyTypeFile:
		return "application/octet-stream", nil
	default:
		return "", fmt.Errorf("body type %q cannot use file-backed storage", bodyType)
	}
}

func buildReplayableMemoryBody(value []byte, contentType string) *sendRequestBodyResult {
	getBody := func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(value)), nil
	}
	return &sendRequestBodyResult{
		reader:        bytes.NewReader(value),
		getBody:       getBody,
		contentType:   contentType,
		contentLength: int64(len(value)),
	}
}

func buildFileBackedSendRequestBody(
	path string,
	size int64,
	contentType string,
) (*sendRequestBodyResult, error) {
	getBody := func() (io.ReadCloser, error) {
		return os.Open(path)
	}
	reader, err := getBody()
	if err != nil {
		return nil, err
	}
	return &sendRequestBodyResult{
		reader:        reader,
		getBody:       getBody,
		contentType:   contentType,
		contentLength: size,
		close:         reader.Close,
	}, nil
}

func buildURLEncodedBody(items []*SendRequestURLEncodedItem) *sendRequestBodyResult {
	encoded := encodeURLEncodedItems(items)
	return &sendRequestBodyResult{
		reader:        strings.NewReader(encoded),
		contentType:   "application/x-www-form-urlencoded",
		contentLength: int64(len(encoded)),
	}
}

func encodeURLEncodedItems(items []*SendRequestURLEncodedItem) string {
	parts := make([]string, 0, len(items))
	for _, item := range items {
		if item == nil || !item.Enabled {
			continue
		}
		name := strings.TrimSpace(item.Name)
		if name == "" {
			continue
		}
		parts = append(parts, url.QueryEscape(name)+"="+url.QueryEscape(item.Value))
	}
	return strings.Join(parts, "&")
}

func buildStreamingMultipartBody(items []*SendRequestFormDataItem) (*sendRequestBodyResult, error) {
	for _, item := range items {
		if item == nil || !item.Enabled {
			continue
		}
		name := strings.TrimSpace(item.Name)
		if name == "" || item.ItemType != "file" {
			continue
		}
		if item.File == nil || strings.TrimSpace(item.File.Path) == "" {
			return nil, fmt.Errorf("form-data file item %q requires file path", name)
		}
		filePath := strings.TrimSpace(item.File.Path)
		stat, err := os.Stat(filePath)
		if err != nil {
			return nil, fmt.Errorf("invalid form-data file %q: %w", name, err)
		}
		if stat.IsDir() {
			return nil, fmt.Errorf("invalid form-data file %q: is a directory", name)
		}
	}

	pipeReader, pipeWriter := io.Pipe()
	writer := multipart.NewWriter(pipeWriter)
	contentType := writer.FormDataContentType()

	go func() {
		writeErr := writeMultipartFormData(writer, items)
		if closeErr := writer.Close(); writeErr == nil && closeErr != nil {
			writeErr = closeErr
		}
		_ = pipeWriter.CloseWithError(writeErr)
	}()

	return &sendRequestBodyResult{
		reader:        pipeReader,
		contentType:   contentType,
		contentLength: -1,
		close:         pipeReader.Close,
	}, nil
}

func writeMultipartFormData(writer *multipart.Writer, items []*SendRequestFormDataItem) error {
	for _, item := range items {
		if item == nil || !item.Enabled {
			continue
		}
		name := strings.TrimSpace(item.Name)
		if name == "" {
			continue
		}

		if item.ItemType != "file" {
			if err := writer.WriteField(name, item.Value); err != nil {
				return err
			}
			continue
		}

		filePath := strings.TrimSpace(item.File.Path)
		file, err := os.Open(filePath)
		if err != nil {
			return fmt.Errorf("open form-data file %q: %w", name, err)
		}

		filename := strings.TrimSpace(item.Filename)
		if filename == "" {
			filename = strings.TrimSpace(item.File.Name)
		}
		if filename == "" {
			filename = filepath.Base(filePath)
		}

		part, err := writer.CreateFormFile(name, filename)
		if err != nil {
			file.Close()
			return err
		}
		if _, err = io.Copy(part, file); err != nil {
			file.Close()
			return err
		}
		if err := file.Close(); err != nil {
			return err
		}
	}

	return nil
}

func recoverRequestURLEncodedBody(bodyBytes []byte) ([]*SendRequestURLEncodedItem, []string) {
	rawBody := bytes2String(bodyBytes)
	if rawBody == "" {
		return nil, nil
	}

	segments := strings.Split(rawBody, "&")
	items := make([]*SendRequestURLEncodedItem, 0, len(segments))
	warnings := make([]string, 0)

	for _, segment := range segments {
		namePart, valuePart, hasValue := strings.Cut(segment, "=")
		name, nameErr := url.QueryUnescape(namePart)
		if nameErr != nil {
			warnings = append(warnings, fmt.Sprintf("failed to decode urlencoded field name %q: %v", namePart, nameErr))
			name = namePart
		}

		value := ""
		if hasValue {
			decodedValue, valueErr := url.QueryUnescape(valuePart)
			if valueErr != nil {
				warnings = append(warnings, fmt.Sprintf("failed to decode urlencoded field value for %q: %v", name, valueErr))
				value = valuePart
			} else {
				value = decodedValue
			}
		}

		items = append(items, &SendRequestURLEncodedItem{
			Enabled: true,
			Name:    name,
			Value:   value,
		})
	}

	return items, warnings
}

func decodeTrafficRequestBody(bodyView TrafficBodyView) ([]byte, []string) {
	if bodyView.RequestBodyEncoding != "base64" {
		return string2Bytes(bodyView.RequestBody), nil
	}

	decoded, err := base64.StdEncoding.DecodeString(bodyView.RequestBody)
	if err != nil {
		return nil, []string{fmt.Sprintf("failed to decode base64 request body: %v", err)}
	}
	return decoded, nil
}

func shouldEncodeBodyForTrafficView(contentType, contentEncoding string, bodySize int64) bool {
	if bodySize == 0 {
		return false
	}
	if isMultipartFormDataContentType(contentType) {
		return true
	}
	if isBinaryContentType(contentType) {
		return true
	}
	if hasUnsupportedContentEncoding(contentEncoding) {
		return true
	}
	return false
}

func shouldEncodeRequestBodyForTrafficView(contentType, contentEncoding string, bodySize int64) bool {
	return shouldEncodeBodyForTrafficView(contentType, contentEncoding, bodySize)
}

func hasUnsupportedContentEncoding(contentEncoding string) bool {
	for _, encoding := range normalizedContentEncodingTokens(contentEncoding) {
		if !isSupportedDecodedContentEncoding(encoding) {
			return true
		}
	}
	return false
}

func normalizedContentEncodingTokens(contentEncoding string) []string {
	if contentEncoding == "" {
		return nil
	}

	encodings := make([]string, 0, 1)
	for value := range strings.SplitSeq(contentEncoding, ",") {
		encoding := normalizeContentEncodingToken(value)
		if encoding == "" || encoding == "identity" {
			continue
		}
		encodings = append(encodings, encoding)
	}
	return encodings
}

func normalizeContentEncodingToken(contentEncoding string) string {
	return strings.ToLower(strings.TrimSpace(contentEncoding))
}

func isSupportedDecodedContentEncoding(contentEncoding string) bool {
	switch normalizeContentEncodingToken(contentEncoding) {
	case "gzip", "br", "snappy", "deflate", "zstd":
		return true
	default:
		return false
	}
}

func inferRequestRecoveryBodyType(contentType string) SendRequestBodyType {
	contentType = strings.ToLower(strings.TrimSpace(contentType))
	if contentType == "" {
		return SendRequestBodyTypeNone
	}
	if strings.Contains(contentType, "multipart/form-data") {
		return SendRequestBodyTypeFormData
	}
	if strings.Contains(contentType, "application/x-www-form-urlencoded") {
		return SendRequestBodyTypeURLEncoded
	}
	if strings.Contains(contentType, "json") {
		return SendRequestBodyTypeJSON
	}
	if strings.Contains(contentType, "xml") {
		return SendRequestBodyTypeXML
	}
	if isBinaryContentType(contentType) {
		return SendRequestBodyTypeFile
	}
	return SendRequestBodyTypeText
}

func shouldRecoverRequestSingleFile(
	requestURL string,
	contentType string,
	bodyView TrafficBodyView,
	bodyBytes []byte,
) bool {
	if len(bodyBytes) == 0 {
		return false
	}
	if isMultipartFormDataContentType(contentType) {
		return false
	}
	if bodyView.RequestBodyEncoding == "base64" || isBinaryContentType(contentType) {
		return true
	}

	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		mediaType = strings.ToLower(strings.TrimSpace(contentType))
	}
	if mediaType == "" {
		return false
	}
	if isKnownTextRequestContentType(mediaType) {
		return false
	}
	if strings.HasPrefix(strings.ToLower(mediaType), "text/") {
		return hasFilenameLikeRequestPath(requestURL)
	}
	return true
}

func isKnownTextRequestContentType(mediaType string) bool {
	mediaType = strings.ToLower(strings.TrimSpace(mediaType))
	if strings.HasSuffix(mediaType, "+json") || strings.HasSuffix(mediaType, "+xml") {
		return true
	}
	switch mediaType {
	case "application/json",
		"text/json",
		"application/xml",
		"text/xml",
		"text/plain",
		"text/html",
		"application/x-www-form-urlencoded":
		return true
	default:
		return false
	}
}

func isMultipartFormDataContentType(contentType string) bool {
	if contentType == "" {
		return false
	}
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return strings.Contains(strings.ToLower(contentType), "multipart/form-data")
	}
	return strings.EqualFold(mediaType, "multipart/form-data")
}

func hasFilenameLikeRequestPath(requestURL string) bool {
	parsedURL, err := url.Parse(requestURL)
	if err != nil {
		return false
	}
	baseName := strings.TrimSpace(filepath.Base(parsedURL.Path))
	if baseName == "" || baseName == "." || baseName == "/" {
		return false
	}
	ext := filepath.Ext(baseName)
	return ext != "" && ext != "."
}

func (s *ProxyService) recoverRequestMultipartBody(
	bodyBytes []byte,
	params map[string]string,
) ([]*RequestBodyRecoveryFormDataItem, []string) {
	boundary := strings.TrimSpace(params["boundary"])
	if boundary == "" {
		return nil, []string{"multipart/form-data boundary is missing"}
	}

	reader := multipart.NewReader(bytes.NewReader(bodyBytes), boundary)
	items := make([]*RequestBodyRecoveryFormDataItem, 0)
	warnings := make([]string, 0)

	for {
		part, err := reader.NextPart()
		if err != nil {
			if err == io.EOF {
				break
			}
			warnings = append(warnings, fmt.Sprintf("failed to parse multipart body: %v", err))
			break
		}

		partName := part.FormName()
		filename := part.FileName()
		partBytes, readErr := io.ReadAll(part)
		closeErr := part.Close()
		if readErr != nil {
			warnings = append(warnings, fmt.Sprintf("failed to read multipart part %q: %v", partName, readErr))
			continue
		}
		if closeErr != nil {
			warnings = append(warnings, fmt.Sprintf("failed to close multipart part %q: %v", partName, closeErr))
		}

		item := &RequestBodyRecoveryFormDataItem{
			Enabled: true,
			Name:    partName,
		}
		if filename == "" {
			item.ItemType = "text"
			item.Value = bytes2String(partBytes)
			items = append(items, item)
			continue
		}

		item.ItemType = "file"
		file, persistWarnings := s.persistRequestRecoveryFile(partBytes, filename)
		item.File = file
		if file == nil {
			item.Value = ""
		}
		warnings = append(warnings, persistWarnings...)
		items = append(items, item)
	}

	return items, warnings
}

func (s *ProxyService) recoverRequestSingleFile(
	bodyBytes []byte,
	requestURL string,
	contentType string,
) (*RequestDraftFile, []string) {
	filename := deriveRequestRecoveryFilename(requestURL, contentType)
	file, warnings := s.persistRequestRecoveryFile(bodyBytes, filename)
	if file == nil {
		return nil, warnings
	}
	return file, warnings
}

func (s *ProxyService) persistRequestRecoveryFile(bodyBytes []byte, suggestedName string) (*RequestDraftFile, []string) {
	cacheDir, err := getRequestDraftCacheStoragePath()
	if err != nil {
		return nil, []string{fmt.Sprintf("failed to resolve request draft cache directory: %v", err)}
	}

	baseName := sanitizeRequestRecoveryName(suggestedName)
	ext := filepath.Ext(baseName)
	prefix := strings.TrimSuffix(baseName, ext)
	if prefix == "" {
		prefix = "request-body"
	}

	file, err := os.CreateTemp(cacheDir, prefix+"-*"+ext)
	if err != nil {
		return nil, []string{fmt.Sprintf("failed to create request draft cache file: %v", err)}
	}

	if _, err = file.Write(bodyBytes); err != nil {
		file.Close()
		_ = os.Remove(file.Name())
		return nil, []string{fmt.Sprintf("failed to write request draft cache file: %v", err)}
	}
	if err = file.Close(); err != nil {
		_ = os.Remove(file.Name())
		return nil, []string{fmt.Sprintf("failed to finalize request draft cache file: %v", err)}
	}

	return &RequestDraftFile{
		Path: file.Name(),
		Name: baseName,
		Size: int64(len(bodyBytes)),
	}, nil
}

func getRequestDraftCacheStoragePath() (string, error) {
	baseDir, err := fs.GetBaseStorageDir()
	if err != nil {
		return "", err
	}
	cacheDir := filepath.Join(baseDir, "request-draft-cache")
	if err := fs.CreateDirIfNotExists(cacheDir); err != nil {
		return "", err
	}
	return cacheDir, nil
}

func deriveRequestRecoveryFilename(requestURL string, contentType string) string {
	if parsedURL, err := url.Parse(requestURL); err == nil {
		name := strings.TrimSpace(filepath.Base(parsedURL.Path))
		if name != "" && name != "." && name != "/" {
			return name
		}
	}

	exts, _ := mime.ExtensionsByType(strings.TrimSpace(contentType))
	if len(exts) > 0 {
		return "request-body" + exts[0]
	}
	return "request-body.bin"
}

func sanitizeRequestRecoveryName(name string) string {
	name = strings.TrimSpace(filepath.Base(name))
	if name == "" || name == "." || name == string(filepath.Separator) {
		return "request-body.bin"
	}
	return name
}

func buildWebSocketHeaders(fields []HTTPHeaderField) (http.Header, http.HeaderOrder, error) {
	result := make(http.Header)
	firstSpellingByName := make(map[string]string, len(fields))
	seenGroups := make(map[string]struct{}, len(fields))
	order := []string{
		"Host",
		"Upgrade",
		"Connection",
		"Sec-WebSocket-Key",
		"Sec-WebSocket-Version",
	}
	currentName := ""
	// HeaderOrder orders case-insensitive names rather than individual field
	// occurrences. Group repeated names under their first spelling while
	// retaining their value order. Reject layouts that this API cannot express
	// instead of silently changing their occurrence sequence or casing.
	for _, field := range fields {
		normalized, include, err := normalizeUserRequestHeaderField(field)
		if err != nil {
			return nil, http.HeaderOrder{}, err
		}
		if !include || shouldSkipWebSocketHeader(normalized.Name) {
			continue
		}
		name := normalized.Name
		lowerName := strings.ToLower(name)
		firstSpelling, ok := firstSpellingByName[lowerName]
		if ok && firstSpelling != name {
			return nil, http.HeaderOrder{}, fmt.Errorf(
				"WebSocket session cannot preserve per-occurrence casing for header %q; use the same casing for repeated fields",
				name,
			)
		}
		if lowerName != currentName {
			if _, seen := seenGroups[lowerName]; seen {
				return nil, http.HeaderOrder{}, fmt.Errorf(
					"WebSocket session cannot preserve interleaved occurrences of header %q; keep repeated fields adjacent",
					name,
				)
			}
			seenGroups[lowerName] = struct{}{}
			currentName = lowerName
		}
		if !ok {
			firstSpelling = name
			firstSpellingByName[lowerName] = name
			order = append(order, name)
		}
		result[firstSpelling] = append(result[firstSpelling], normalized.Value)
	}
	if len(result) == 0 {
		result = nil
	}
	return result, http.HeaderOrder{Headers: order}, nil
}

func formatWebSocketLogTarget(parsedURL *url.URL) string {
	return formatURLLogTarget(parsedURL)
}

func formatRawURLLogTarget(rawURL string) string {
	parsedURL, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsedURL.Scheme == "" || parsedURL.Host == "" {
		return "(invalid-url)"
	}
	return formatURLLogTarget(parsedURL)
}

func formatURLLogTarget(parsedURL *url.URL) string {
	if parsedURL == nil {
		return ""
	}

	path := parsedURL.EscapedPath()
	if path == "" {
		path = "/"
	}
	return fmt.Sprintf("%s://%s%s", parsedURL.Scheme, parsedURL.Host, path)
}

type headerValueSetter interface {
	Get(string) string
	Set(string, string)
}

func applyFallbackUserAgent(headers headerValueSetter) {
	if headers == nil {
		return
	}
	if strings.TrimSpace(headers.Get("User-Agent")) != "" {
		return
	}
	headers.Set("User-Agent", UserAgentHeader)
}

func shouldSkipWebSocketHeader(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case
		"connection",
		"content-length",
		"host",
		"keep-alive",
		"proxy-connection",
		"sec-websocket-accept",
		"sec-websocket-extensions",
		"sec-websocket-key",
		"sec-websocket-version",
		"transfer-encoding",
		"upgrade":
		return true
	default:
		return false
	}
}

func (s *ProxyService) resolveSendRequestProxy(
	cfg SendRequestConfig,
	proxyConfig *settingservice.ProxyConfig,
) (*url.URL, error) {
	switch cfg.ProxyMode {
	case "", SendRequestProxyModeNone:
		return nil, nil
	case SendRequestProxyModeSystem:
		systemProxy := resolveSystemUpstreamProxy()
		if systemProxy == "" {
			return nil, nil
		}
		proxyURL, err := url.Parse(systemProxy)
		if err != nil {
			return nil, fmt.Errorf("invalid system proxy: %w", err)
		}
		return proxyURL, nil
	case SendRequestProxyModeMITM:
		s.mu.Lock()
		running := s.running
		s.mu.Unlock()
		if !running {
			return nil, fmt.Errorf("mitm proxy is not running")
		}
		proxyURL, err := url.Parse(fmt.Sprintf("http://%s:%d", proxyConfig.Host, proxyConfig.Port))
		if err != nil {
			return nil, err
		}
		return proxyURL, nil
	case SendRequestProxyModeCustom:
		customProxy := strings.TrimSpace(cfg.CustomProxy)
		if customProxy == "" {
			return nil, fmt.Errorf("custom proxy is required when proxy mode is custom")
		}
		proxyURL, err := url.Parse(customProxy)
		if err != nil {
			return nil, fmt.Errorf("invalid custom proxy: %w", err)
		}
		return proxyURL, nil
	default:
		return nil, fmt.Errorf("unsupported proxy mode: %s", cfg.ProxyMode)
	}
}

func (s *ProxyService) getWebSocketSession(sessionID string) (*webSocketSession, bool) {
	value, ok := s.webSocketSessions.Load(strings.TrimSpace(sessionID))
	if !ok {
		return nil, false
	}
	return value.(*webSocketSession), true
}

func resolveWebSocketSendPayload(
	req WebSocketSendRequest,
) (messageType int, payload []byte, normalizedType string, err error) {
	switch strings.ToLower(strings.TrimSpace(req.MsgType)) {
	case "", "text":
		return websocket.TextMessage, []byte(req.Text), "text", nil
	case "binary":
		if req.File == nil || strings.TrimSpace(req.File.Path) == "" {
			return 0, nil, "", fmt.Errorf("file is required for binary websocket message")
		}
		payload, err := os.ReadFile(req.File.Path)
		if err != nil {
			return 0, nil, "", err
		}
		return websocket.BinaryMessage, payload, "binary", nil
	default:
		return 0, nil, "", fmt.Errorf("unsupported websocket message type: %s", req.MsgType)
	}
}

func (s *ProxyService) runWebSocketReadLoop(
	ctx context.Context,
	session *webSocketSession,
) {
	for {
		msgType, payload, err := session.conn.ReadMessage()
		if err != nil {
			if ctx.Err() != nil {
				s.finalizeWebSocketSession(session.id, "closed", nil, "closed")
				return
			}

			status := "error"
			eventType := "error"
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				status = "closed"
				eventType = "closed"
			}
			s.finalizeWebSocketSession(session.id, status, err, eventType)
			return
		}

		msgTypeName := formatWebsocketMsgType(msgType)
		logger.G().Debugf(
			"WebSocket session message received: session_id=%s target=%s msg_type=%s bytes=%d",
			session.id,
			session.target,
			msgTypeName,
			len(payload),
		)
		s.emitWebSocketSessionEvent(WebSocketSessionEvent{
			SessionID: session.id,
			EventType: "message",
			Status:    "connected",
			Message:   normalizeWebSocketMessage("receive", msgTypeName, payload),
		})
	}
}

func (s *ProxyService) finalizeWebSocketSession(
	sessionID string,
	status string,
	err error,
	eventType string,
) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return
	}

	value, ok := s.webSocketSessions.LoadAndDelete(sessionID)
	if !ok {
		return
	}

	session := value.(*webSocketSession)
	session.cancel()
	_ = session.conn.Close()
	duration := time.Since(session.started).Round(time.Millisecond)
	if status == "error" {
		logger.G().Warnf(
			"WebSocket session ended: session_id=%s target=%s status=%s event_type=%s duration=%s error=%v",
			sessionID,
			session.target,
			status,
			eventType,
			duration,
			err,
		)
	} else {
		if err != nil {
			logger.G().Infof(
				"WebSocket session ended: session_id=%s target=%s status=%s event_type=%s duration=%s error=%v",
				sessionID,
				session.target,
				status,
				eventType,
				duration,
				err,
			)
		} else {
			logger.G().Infof(
				"WebSocket session ended: session_id=%s target=%s status=%s event_type=%s duration=%s",
				sessionID,
				session.target,
				status,
				eventType,
				duration,
			)
		}
	}

	s.emitWebSocketSessionEvent(WebSocketSessionEvent{
		SessionID: sessionID,
		EventType: eventType,
		Status:    status,
		Error:     errorString(err),
	})
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func (s *ProxyService) storeTrafficEntry(entry *TrafficEntry) bool {
	return s.storeTrafficSnapshot(entry, cloneTrafficEntryForAttribution) != nil
}

func (s *ProxyService) storeTrafficMetrics(entry *TrafficEntry) *TrafficEntry {
	return s.storeTrafficSnapshot(entry, cloneTrafficEntryForMetrics)
}

func (s *ProxyService) storeTrafficResponseHeaders(entry *TrafficEntry) *TrafficEntry {
	return s.storeTrafficSnapshot(entry, cloneTrafficEntryForResponseHeaders)
}

func (s *ProxyService) storeTrafficResponseTrailers(entry *TrafficEntry) *TrafficEntry {
	return s.storeTrafficSnapshot(entry, cloneTrafficEntryForResponseTrailers)
}

func (s *ProxyService) storeTrafficFailure(entry *TrafficEntry) *TrafficEntry {
	return s.storeTrafficSnapshot(entry, cloneTrafficEntryForMetrics)
}

func (s *ProxyService) storeTrafficSnapshot(
	entry *TrafficEntry,
	cloneEntry func(*TrafficEntry) *TrafficEntry,
) *TrafficEntry {
	s.captureLifecycleMu.RLock()
	defer s.captureLifecycleMu.RUnlock()
	if !s.isCurrentTrafficEntryLocked(entry) {
		return nil
	}
	if s.storeTrafficPrecheckHook != nil {
		s.storeTrafficPrecheckHook()
	}
	s.trafficAttributionMu.Lock()
	// Deletion installs the tombstone under trafficAttributionMu. Recheck after
	// taking that lock to close the gap between the lifecycle precheck above and
	// publishing the immutable snapshot below.
	if !s.isCurrentTrafficEntryLocked(entry) {
		s.trafficAttributionMu.Unlock()
		return nil
	}
	if entry.lifecycle == nil {
		entry.lifecycle = &trafficEntryLifecycle{}
	}
	// The caller may keep updating its working copy as the exchange advances.
	// Store an immutable snapshot. Each cloner owns the collections changed at
	// that milestone and shares only data that is already immutable.
	stored := cloneEntry(entry)
	nextRevision := entry.Revision + 1
	if current, ok := s.trafficEntries.Get(entry.ID); ok && current.Revision >= nextRevision {
		nextRevision = current.Revision + 1
	}
	stored.Revision = nextRevision
	entry.Revision = nextRevision
	if process, attributed := s.processBindingSnapshot(entry.ID); attributed {
		if stored.Metadata == nil {
			stored.Metadata = &Metadata{}
		}
		stored.Metadata.Process = process
	}
	inserted := s.trafficEntries.Set(entry.ID, stored)
	s.trafficAttributionMu.Unlock()
	s.markHistoryDirty()
	if inserted {
		atomic.AddInt64(&s.trafficEntries.Statistics.Total, 1)
		switch entry.Type {
		case "http", "https":
			atomic.AddInt64(&s.trafficEntries.Statistics.TotalHTTP, 1)
		case "ws", "wss":
			atomic.AddInt64(&s.trafficEntries.Statistics.TotalWS, 1)
		case "tcp":
			atomic.AddInt64(&s.trafficEntries.Statistics.TotalTCP, 1)
		}
	}
	return stored
}

//wails:ignore
func (s *ProxyService) ForceFlushHistory() {
	_ = s.flushHistoryToDisk(true)
}

func (s *ProxyService) UpdateHistoryAlias(alias string) {
	normalized := strings.TrimSpace(alias)
	s.historyMetadataMu.Lock()
	if s.currentHistoryMetadata.Alias == normalized {
		s.historyMetadataMu.Unlock()
		return
	}
	s.currentHistoryMetadata.Alias = normalized
	s.historyMetadataMu.Unlock()
	if s.trafficEntries.Len() > 0 {
		s.markHistoryDirty()
	}
	s.emitStatus()
}

func (s *ProxyService) flushHistoryToDisk(force bool) error {
	s.clearDataMu.Lock()
	defer s.clearDataMu.Unlock()
	s.captureLifecycleMu.RLock()
	defer s.captureLifecycleMu.RUnlock()
	return s.flushHistoryToDiskCaptureLocked(force)
}

// flushHistoryToDiskCaptureLocked writes one capture snapshot while the caller
// holds clearDataMu and either side of captureLifecycleMu. Transition paths use
// the write lock so no entry can be published between this snapshot and reset.
func (s *ProxyService) flushHistoryToDiskCaptureLocked(force bool) error {
	if !force && !s.enableFlushing.Load() {
		return nil
	}

	generation := s.flushGeneration.Load()
	if !force && !s.historyDirty.Load() && generation == s.lastFlushGeneration.Load() {
		return nil
	}
	if s.trafficEntries.Len() == 0 {
		if err := s.removeCurrentHistoryFiles(s.CurrentHistoryKey()); err != nil {
			return err
		}
		s.markHistoryGenerationFlushed(generation)
		return nil
	}

	historyStorageDir, err := getHistoryStoragePath()
	if err != nil {
		return err
	}
	if err := fs.CreateDirIfNotExists(historyStorageDir); err != nil {
		return err
	}

	md := s.currentHistoryMetadataSnapshot()
	historyBinFilePath := filepath.Join(historyStorageDir, fs.GetHBinFileName(md.Key))
	historyIndexFilePath := filepath.Join(historyStorageDir, fs.GetHIdxFileName(md.Key))
	nowTs := time.Now()
	trafficEntries := s.trafficEntries.Values()
	err = s.writeAndCommitHistoryFilePair(
		historyFilePairPaths{data: historyBinFilePath, index: historyIndexFilePath},
		func(hbinFile, hindexFile *os.File) error {
			if err := encodeHistoryMetadata(hbinFile, md); err != nil {
				return err
			}
			if err := binary.Write(hbinFile, binary.BigEndian, uint32(len(trafficEntries))); err != nil {
				return err
			}
			if err := s.historyFlushCheckpoint(historyFlushStageWriteIndex); err != nil {
				return fmt.Errorf("history flush checkpoint %s: %w", historyFlushStageWriteIndex, err)
			}
			if err := binary.Write(hindexFile, binary.BigEndian, uint32(len(trafficEntries))); err != nil {
				return err
			}
			for _, entry := range trafficEntries {
				bodyView, bodyViewErr := s.getTrafficBodyViewInner(entry.ID)
				if bodyViewErr != nil {
					logger.G().Errorf("Failed to get body view for entry %d: %v", entry.ID, bodyViewErr)
					// getTrafficBodyViewInner returns any readable side together with
					// per-side unavailability, so one corrupt cache file must not discard
					// the other payload while preserving the entry.
				}
				encodeErr := encodeTrafficEntry(hindexFile, hbinFile, entry, &bodyView)
				bodyView.closeReqBodyReaderSafely()
				bodyView.closeRspBodyReaderSafely()
				if encodeErr != nil {
					logger.G().Errorf("Failed to encode traffic entry %d: %v", entry.ID, encodeErr)
					return encodeErr
				}
			}
			return nil
		},
	)
	if err != nil {
		logger.G().Errorf("Failed to flush history to disk: %v", err)
		return err
	}
	s.markHistoryGenerationFlushed(generation)
	logger.G().Infof(
		"Flushed history to disk: %s, entries: %d, duration: %s",
		historyBinFilePath,
		len(trafficEntries),
		time.Since(nowTs),
	)
	return nil
}

func (s *ProxyService) removeCurrentHistoryFiles(key string) error {
	historyStorageDir, err := getHistoryStoragePath()
	if err != nil {
		return err
	}
	paths, err := ManagedHistoryFilePaths(historyStorageDir, key)
	if err != nil {
		return err
	}
	removeFile := s.removeHistoryFile
	if removeFile == nil {
		removeFile = os.Remove
	}
	for _, path := range paths {
		if err := removeFile(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			// The data file is deliberately first. If it cannot be removed,
			// retain the index so HistoryService can discover and retry the
			// still-sensitive pair after the current capture key rotates.
			return fmt.Errorf("delete current history file %s: %w", path, err)
		}
	}
	return nil
}

// markHistoryGenerationFlushed clears the dirty flag only when no newer
// snapshot was published while the flush was encoding headers or streaming
// bodies. The second generation check closes the window where markHistoryDirty
// can race between the first check and Store(false).
func (s *ProxyService) markHistoryGenerationFlushed(generation uint64) {
	s.lastFlushGeneration.Store(generation)
	if s.flushGeneration.Load() != generation {
		s.historyDirty.Store(true)
		return
	}
	s.historyDirty.Store(false)
	if s.flushGeneration.Load() != generation {
		s.historyDirty.Store(true)
	}
}

func (s *ProxyService) GetLocalDataSize() (LocalDataSize, error) {
	var result LocalDataSize

	baseDir, err := fs.GetBaseStorageDir()
	if err != nil {
		return result, err
	}
	result.CacheBytes = fs.DirSize(filepath.Join(baseDir, "cache"))
	s.requestDraftCacheOpsMu.RLock()
	if requestDraftCacheDir, requestErr := getRequestDraftCacheStoragePath(); requestErr == nil {
		result.CacheBytes += fs.DirSize(requestDraftCacheDir)
	}
	s.requestDraftCacheOpsMu.RUnlock()

	historyDir, err := getHistoryStoragePath()
	if err == nil {
		result.HistoryBytes = fs.DirSize(historyDir)
	}

	return result, nil
}

func (s *ProxyService) flushOnShutdown() error {
	if !s.historyDirty.Load() && s.flushGeneration.Load() == s.lastFlushGeneration.Load() {
		return nil
	}
	if err := s.flushHistoryToDisk(true); err != nil {
		logger.G().Warnf("final history flush on shutdown failed: %v", err)
		return fmt.Errorf("flush final history: %w", err)
	}
	return nil
}

func (s *ProxyService) reinitializeBodyCacheNoLock() error {
	s.bodyCache = nil
	if !s.running {
		return nil
	}
	return s.initializeBodyCacheNoLock()
}

func (s *ProxyService) finalizeCaptureRestartLocked() {
	s.captureLifecycleMu.Lock()
	defer s.captureLifecycleMu.Unlock()
	s.finalizeCaptureRestartCaptureLocked()
}

func (s *ProxyService) finalizeCaptureRestartCaptureLocked() {
	s.captureGeneration++
	s.resetCurrentHistoryMetadata()
	s.bodyCacheMu.Lock()
	s.clearTrafficWithBodyCache(s.bodyCache)
	if err := s.reinitializeBodyCacheNoLock(); err != nil {
		logger.G().Warnf("Capture restart body cache initialization failed: %v", err)
	}
	s.bodyCacheMu.Unlock()
	s.historyDirty.Store(false)
	s.lastFlushGeneration.Store(s.flushGeneration.Load())
	s.emitTrafficResetLocked()
}

func (s *ProxyService) RestartCapture(saveCurrent bool) error {
	s.lifecycleOpsMu.Lock()
	defer s.lifecycleOpsMu.Unlock()
	s.runLifecycleOperationHook("restart-capture")

	s.clearDataMu.Lock()
	defer s.clearDataMu.Unlock()
	s.captureLifecycleMu.Lock()
	defer s.captureLifecycleMu.Unlock()

	entries := s.trafficEntries.Len()
	logger.G().Infof("Capture restart requested: save_current=%t entries=%d", saveCurrent, entries)
	if saveCurrent {
		if err := s.flushHistoryToDiskCaptureLocked(true); err != nil {
			logger.G().Warnf("Capture restart history flush failed: error=%v", err)
			return err
		}
	}
	if !saveCurrent {
		if err := s.removeCurrentHistoryFiles(s.CurrentHistoryKey()); err != nil {
			return fmt.Errorf("discard current history snapshot: %w", err)
		}
	}

	s.finalizeCaptureRestartCaptureLocked()
	s.emitStatus()
	logger.G().Info("Capture restarted")
	return nil
}

type storageDirectoryRemoval struct {
	name string
	path string
}

func removeStorageDirectories(removeAll func(string) error, targets ...storageDirectoryRemoval) error {
	var removeErrors []error
	for _, target := range targets {
		if err := removeAll(target.path); err != nil && !os.IsNotExist(err) {
			removeErrors = append(removeErrors, fmt.Errorf("remove %s directory: %w", target.name, err))
		}
	}
	return errors.Join(removeErrors...)
}

func (s *ProxyService) configuredHistoryCleaner() HistoryCleaner {
	s.historyCleanerMu.RLock()
	defer s.historyCleanerMu.RUnlock()
	return s.historyCleaner
}

func (s *ProxyService) emitLocalDataCleared(scope, requestDraftCacheRoot string, historyCleared bool) {
	if s.app == nil {
		return
	}
	_ = s.app.Event.Emit(localDataClearedEventName, map[string]any{
		"scope":                 scope,
		"requestDraftCacheRoot": requestDraftCacheRoot,
		"historyCleared":        historyCleared,
	})
}

type storedCaptureClearResult struct {
	stateCleared          bool
	historyCleared        bool
	requestDraftCacheRoot string
}

func (s *ProxyService) clearStoredCaptureDataLocked(includeHistory, preserveCurrent bool) (storedCaptureClearResult, error) {
	s.clearDataMu.Lock()
	defer s.clearDataMu.Unlock()

	baseDir, err := fs.GetBaseStorageDir()
	if err != nil {
		return storedCaptureClearResult{}, fmt.Errorf("get base storage directory: %w", err)
	}
	result := storedCaptureClearResult{
		requestDraftCacheRoot: filepath.Clean(filepath.Join(baseDir, "request-draft-cache")),
	}

	wasEnabled := s.enableFlushing.Swap(false)
	defer s.enableFlushing.Store(wasEnabled)

	s.captureLifecycleMu.Lock()
	if preserveCurrent {
		if err := s.flushHistoryToDiskCaptureLocked(true); err != nil {
			s.captureLifecycleMu.Unlock()
			return result, fmt.Errorf("preserve current history before clearing cache: %w", err)
		}
	}
	s.captureGeneration++
	s.bodyCacheMu.Lock()
	s.clearTrafficWithBodyCache(s.bodyCache)
	s.bodyCache = nil
	result.stateCleared = true
	previousHistoryKey := s.CurrentHistoryKey()

	var clearErrors []error
	if err := removeStorageDirectories(os.RemoveAll,
		storageDirectoryRemoval{name: "cache", path: filepath.Join(baseDir, "cache")},
		storageDirectoryRemoval{name: "request draft cache", path: result.requestDraftCacheRoot},
	); err != nil {
		clearErrors = append(clearErrors, err)
	}

	if includeHistory {
		result.historyCleared = true
		if err := s.removeCurrentHistoryFiles(previousHistoryKey); err != nil {
			result.historyCleared = false
			clearErrors = append(clearErrors, fmt.Errorf("remove current history: %w", err))
		}
	}
	// Rotate before HistoryService refreshes its index. If deleting the old
	// current pair failed, it is no longer excluded by CurrentHistoryKey and
	// remains visible for partial-clear recovery and later retries.
	s.resetCurrentHistoryMetadata()
	if includeHistory {
		if cleaner := s.configuredHistoryCleaner(); cleaner != nil {
			if err := cleaner.ClearHistories(); err != nil {
				result.historyCleared = false
				clearErrors = append(clearErrors, fmt.Errorf("clear indexed histories: %w", err))
			}
		} else {
			result.historyCleared = false
			clearErrors = append(clearErrors, errors.New("clear indexed histories: history cleaner is not configured"))
		}
	}
	if err := s.reinitializeBodyCacheNoLock(); err != nil {
		clearErrors = append(clearErrors, fmt.Errorf("reinitialize body cache: %w", err))
	}
	s.bodyCacheMu.Unlock()
	s.historyDirty.Store(false)
	s.lastFlushGeneration.Store(s.flushGeneration.Load())
	s.emitTrafficResetLocked()
	s.captureLifecycleMu.Unlock()

	if err := s.resetProcessIconStore(baseDir); err != nil {
		clearErrors = append(clearErrors, fmt.Errorf("reset process icon store: %w", err))
	}
	return result, errors.Join(clearErrors...)
}

// clearStoredCaptureData is retained for focused package tests. Production
// entry points keep both locks through frontend notification so recovery and
// cache removal have one observable order.
func (s *ProxyService) clearStoredCaptureData(includeHistory bool) (storedCaptureClearResult, error) {
	s.lifecycleOpsMu.Lock()
	defer s.lifecycleOpsMu.Unlock()
	s.requestDraftCacheOpsMu.Lock()
	defer s.requestDraftCacheOpsMu.Unlock()
	s.runRequestDraftCacheOperationHook("clear")
	return s.clearStoredCaptureDataLocked(includeHistory, false)
}

func (s *ProxyService) ClearCacheFiles() error {
	s.lifecycleOpsMu.Lock()
	defer s.lifecycleOpsMu.Unlock()
	s.runLifecycleOperationHook("clear-cache")

	logger.G().Infof("Clear cache files requested: entries=%d", s.trafficEntries.Len())
	s.requestDraftCacheOpsMu.Lock()
	defer s.requestDraftCacheOpsMu.Unlock()
	s.runRequestDraftCacheOperationHook("clear")
	// Cache-only clearing rotates the active key. Preserve and clear under one
	// capture write lock so traffic cannot fall between the disk snapshot and
	// the generation reset.
	result, err := s.clearStoredCaptureDataLocked(false, true)
	if result.stateCleared {
		s.emitLocalDataCleared("cache", result.requestDraftCacheRoot, false)
		s.emitStatus()
	}
	if err != nil {
		logger.G().Warnf("Clear cache files completed with errors: %v", err)
		return err
	}
	logger.G().Info("Clear cache files completed")
	return nil
}

func (s *ProxyService) ClearCacheAndHistory() error {
	s.lifecycleOpsMu.Lock()
	defer s.lifecycleOpsMu.Unlock()
	s.runLifecycleOperationHook("clear-cache-and-history")
	s.requestDraftCacheOpsMu.Lock()
	defer s.requestDraftCacheOpsMu.Unlock()
	s.runRequestDraftCacheOperationHook("clear")

	logger.G().Infof("Clear cache and history requested: entries=%d", s.trafficEntries.Len())
	result, err := s.clearStoredCaptureDataLocked(true, false)
	if result.stateCleared {
		s.emitLocalDataCleared("cache-and-history", result.requestDraftCacheRoot, result.historyCleared)
	}
	if err != nil {
		s.emitStatus()
		logger.G().Warnf("Clear cache and history completed with errors: %v", err)
		return err
	}
	s.emitStatus()
	logger.G().Info("Clear cache and history completed")
	return nil
}

func (s *ProxyService) syncHistoriesToDiskPeriodically() error {
	interval := time.Second * 60
	timer := time.NewTimer(interval)
	defer timer.Stop()
	for {
		select {
		case <-s.appContext().Done(): // first context cancellation and then ServiceShutdown be called
			logger.G().Info("HistoryService context canceled, stopping periodic sync")
			return nil
		case <-timer.C:
			_ = s.flushHistoryToDisk(false)
			timer.Reset(interval)
		}
	}
}

func formatWebsocketMsgType(msgType int) string {
	switch msgType {
	case 1:
		return "text"
	case 2:
		return "binary"
	default:
		return fmt.Sprintf("unknown(%d)", msgType)
	}
}

func normalizeWebSocketMessage(direction, msgType string, payload []byte) *WebSocketMessage {
	message := &WebSocketMessage{
		Direction: strings.ToLower(direction),
		MsgType:   strings.ToLower(msgType),
		DataSize:  len(payload),
	}
	if message.MsgType == "binary" {
		message.Data = base64.StdEncoding.EncodeToString(payload)
		return message
	}
	message.Data = string(payload)
	return message
}
