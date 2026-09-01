package settingservice

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	bodycache "github.com/josexy/flowlens/backend/pkg/body_cache"
	"github.com/josexy/flowlens/backend/pkg/logger"
	appservice "github.com/josexy/flowlens/backend/services/app_service"
	"github.com/wailsapp/wails/v3/pkg/application"
)

const (
	defaultCACertCommonName      = "FlowLens Root CA"
	defaultCACertValidDays       = 3650
	defaultThemeMode             = "light"
	defaultLanguage              = "zh"
	defaultHistoryRetentionValue = 7
	minHistoryRetentionValue     = 1
	maxHistoryRetentionValue     = 9999
	defaultPythonHookTimeoutMs   = 5000
	minPythonHookTimeoutMs       = 100
	maxPythonHookTimeoutMs       = 60000
)

const (
	allProxyHostsValue     = "0.0.0.0"
	allProxyHostsLabel     = "ALL"
	defaultProxyHost       = "127.0.0.1"
	defaultProxyPort       = 8080
	defaultProxyCACertPath = "certs/ca.crt"
	defaultProxyCAKeyPath  = "certs/ca.key"
)

type SettingService struct {
	mu                    sync.RWMutex
	loadMu                sync.Mutex
	persistMu             sync.Mutex
	settings              *Settings
	activeWindowFrameMode WindowFrameMode
	repository            *settingRepository

	processAttributionEnabled atomic.Bool
	processAttributionReady   atomic.Bool
}

func New(db *sql.DB) *SettingService {
	return &SettingService{repository: newSettingRepository(db)}
}

func SetActiveWindowFrameMode(service *SettingService, mode WindowFrameMode) {
	if service == nil {
		return
	}
	service.setActiveWindowFrameMode(mode)
}

func (s *SettingService) ServiceStartup(ctx context.Context, _ application.ServiceOptions) error {
	s.mu.RLock()
	loaded := s.settings != nil
	s.mu.RUnlock()
	if loaded {
		return nil
	}
	return s.Load()
}

func (s *SettingService) ServiceShutdown() error {
	return nil
}

func (s *SettingService) GetProxyConfig() (*ProxyConfig, error) {
	if err := s.ensureLoaded(); err != nil {
		return nil, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.settings == nil || s.settings.ProxyConfig == nil {
		return nil, errors.New("proxy config not loaded")
	}
	return s.settings.ProxyConfig, nil
}

func (s *SettingService) GetWindowConfig() (*WindowConfig, error) {
	if err := s.ensureLoaded(); err != nil {
		return nil, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.settings == nil || s.settings.WindowConfig == nil {
		return nil, errors.New("window config not loaded")
	}
	return s.settings.WindowConfig, nil
}

func GetMainWindowCloseBehavior(service *SettingService) (MainWindowCloseBehavior, error) {
	if service == nil {
		return MainWindowCloseBehaviorHideToTray, errors.New("setting service is not available")
	}
	if err := service.ensureLoaded(); err != nil {
		return MainWindowCloseBehaviorHideToTray, err
	}

	service.mu.RLock()
	defer service.mu.RUnlock()
	if service.settings == nil || service.settings.WindowConfig == nil {
		return MainWindowCloseBehaviorHideToTray, errors.New("window config not loaded")
	}
	behavior := service.settings.WindowConfig.MainWindowCloseBehavior
	if !isValidMainWindowCloseBehavior(behavior) {
		return MainWindowCloseBehaviorHideToTray, nil
	}
	return behavior, nil
}

func (s *SettingService) GetActiveWindowFrameMode() WindowFrameMode {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !isValidWindowFrameMode(s.activeWindowFrameMode) {
		return WindowFrameModeCustom
	}
	return s.activeWindowFrameMode
}

func (s *SettingService) setActiveWindowFrameMode(mode WindowFrameMode) {
	if !isValidWindowFrameMode(mode) {
		mode = WindowFrameModeCustom
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.activeWindowFrameMode = mode
}

func (s *SettingService) Get() (*Settings, error) {
	if err := s.ensureLoaded(); err != nil {
		return nil, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.settings == nil {
		return nil, errors.New("settings not loaded")
	}
	return s.settings, nil
}

func (s *SettingService) UpdateWindowConfig(windowConfig *WindowConfig) error {
	if windowConfig == nil {
		return errors.New("window config cannot be nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.settings == nil {
		s.settings = new(Settings)
	}
	s.settings.WindowConfig = windowConfig
	s.setupDefaultSettingsLocked()
	return nil
}

func (s *SettingService) SetThemeMode(mode string) error {
	if !isValidThemeMode(mode) {
		return fmt.Errorf("invalid theme mode: %s", mode)
	}
	if err := s.ensureLoaded(); err != nil {
		return err
	}

	s.mu.Lock()
	s.settings.CommonConfig.ThemeMode = mode
	s.mu.Unlock()
	return nil
}

func (s *SettingService) SetLanguage(language string) error {
	if !isValidLanguage(language) {
		return fmt.Errorf("invalid language: %s", language)
	}
	if err := s.ensureLoaded(); err != nil {
		return err
	}

	s.mu.Lock()
	s.settings.CommonConfig.Language = language
	s.mu.Unlock()
	return nil
}

func GetLogConfig(service *SettingService) (LogConfig, error) {
	if service == nil {
		return LogConfig{}, errors.New("setting service is not available")
	}
	return service.getLogConfig()
}

func GetHistoryRetentionConfig(service *SettingService) (HistoryRetentionConfig, error) {
	if service == nil {
		return HistoryRetentionConfig{}, errors.New("setting service is not available")
	}
	if err := service.ensureLoaded(); err != nil {
		return HistoryRetentionConfig{}, err
	}

	service.mu.RLock()
	defer service.mu.RUnlock()
	if service.settings == nil || service.settings.HistoryRetentionConfig == nil {
		return HistoryRetentionConfig{}, errors.New("history retention config not loaded")
	}
	return *service.settings.HistoryRetentionConfig, nil
}

func GetProcessAttributionConfig(service *SettingService) (ProcessAttributionConfig, error) {
	if service == nil {
		return ProcessAttributionConfig{}, errors.New("setting service is not available")
	}
	if err := service.ensureLoaded(); err != nil {
		return ProcessAttributionConfig{}, err
	}

	service.mu.RLock()
	defer service.mu.RUnlock()
	if service.settings == nil || service.settings.ProcessAttributionConfig == nil {
		return ProcessAttributionConfig{}, errors.New("process attribution config not loaded")
	}
	return *service.settings.ProcessAttributionConfig, nil
}

func GetPythonPluginConfig(service *SettingService) (PythonPluginConfig, error) {
	if service == nil {
		return PythonPluginConfig{}, errors.New("setting service is not available")
	}
	if err := service.ensureLoaded(); err != nil {
		return PythonPluginConfig{}, err
	}

	service.mu.RLock()
	defer service.mu.RUnlock()
	if service.settings == nil || service.settings.PythonPluginConfig == nil {
		return PythonPluginConfig{}, errors.New("python plugin config not loaded")
	}
	return *cloneAndSanitizePythonPluginConfig(service.settings.PythonPluginConfig), nil
}

// ProcessAttributionEnabled returns the process-attribution switch through an
// atomic fast path after the settings snapshot has been initialized.
func ProcessAttributionEnabled(service *SettingService) (bool, error) {
	if service == nil {
		return false, errors.New("setting service is not available")
	}
	if !service.processAttributionReady.Load() {
		if err := service.ensureLoaded(); err != nil {
			return false, err
		}
	}
	if !service.processAttributionReady.Load() {
		return false, errors.New("process attribution config not loaded")
	}
	return service.processAttributionEnabled.Load(), nil
}

func (s *SettingService) getLogConfig() (LogConfig, error) {
	if err := s.ensureLoaded(); err != nil {
		return LogConfig{}, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.settings == nil || s.settings.CommonConfig == nil {
		return LogConfig{}, errors.New("common config not loaded")
	}

	cfg := s.settings.CommonConfig
	return LogConfig{
		Enabled: !cfg.LogDisabled,
		Level:   cfg.LogLevel,
	}, nil
}

func SetLogEnabled(service *SettingService, enabled bool) error {
	if service == nil {
		return errors.New("setting service is not available")
	}
	return service.setLogEnabled(enabled)
}

func (s *SettingService) setLogEnabled(enabled bool) error {
	if err := s.ensureLoaded(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.settings.CommonConfig.LogDisabled = !enabled
	s.setupDefaultSettingsLocked()
	return nil
}

func SetLogLevel(service *SettingService, level string) error {
	if service == nil {
		return errors.New("setting service is not available")
	}
	return service.setLogLevel(level)
}

func (s *SettingService) setLogLevel(level string) error {
	normalizedLevel, err := logger.ParseLogLevel(level)
	if err != nil {
		return err
	}
	if err := s.ensureLoaded(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.settings.CommonConfig.LogLevel = string(normalizedLevel)
	s.setupDefaultSettingsLocked()
	return nil
}

// Update is retained for backend callers that replace the complete in-memory
// snapshot. Frontend settings must use UpdatePreservingShortcuts.
//
//wails:ignore
func (s *SettingService) Update(settings *Settings) error {
	if settings == nil {
		return errors.New("settings cannot be nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.settings = settings
	s.setupDefaultSettingsLocked()
	return nil
}

// UpdatePreservingShortcuts replaces ordinary settings sections while retaining
// the backend-latest shortcut and Python plugin runtime configurations. Both are
// written through narrow services and must not be overwritten by a settings
// window based on an older Get snapshot.
func (s *SettingService) UpdatePreservingShortcuts(settings *Settings) error {
	if settings == nil {
		return errors.New("settings cannot be nil")
	}
	if err := s.ensureLoaded(); err != nil {
		return err
	}

	s.mu.Lock()
	shortcuts := cloneAndSanitizeShortcutConfig(s.settings.Shortcuts)
	pythonPlugins := cloneAndSanitizePythonPluginConfig(s.settings.PythonPluginConfig)
	s.settings = settings
	s.setupDefaultSettingsLocked()
	s.settings.Shortcuts = shortcuts
	s.settings.PythonPluginConfig = pythonPlugins
	s.mu.Unlock()
	return nil
}

func (s *SettingService) ensureLoaded() error {
	if s.isLoaded() {
		return nil
	}

	s.loadMu.Lock()
	defer s.loadMu.Unlock()

	if s.isLoaded() {
		return nil
	}

	return s.loadLocked()
}

func (s *SettingService) isLoaded() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.settings != nil
}

func (s *SettingService) ListSystemFonts() []FontOption {
	fonts := listSystemFontFamilies()
	if len(fonts) == 0 {
		fonts = defaultFontFamilies()
	}
	sort.Strings(fonts)

	options := make([]FontOption, 0, len(fonts))
	for _, font := range fonts {
		font = strings.TrimSpace(font)
		if font == "" {
			continue
		}
		options = append(options, FontOption{Label: font, Value: font})
	}
	return options
}

type localInterfaceAddress struct {
	InterfaceName string
	IP            net.IP
}

func (s *SettingService) ListLocalIPv4Addresses() []LocalIPAddress {
	interfaces, err := net.Interfaces()
	if err != nil {
		return defaultLocalIPv4AddressOptions()
	}

	addresses := make([]localInterfaceAddress, 0)
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, addrErr := iface.Addrs()
		if addrErr != nil {
			continue
		}
		for _, addr := range addrs {
			ip := ipFromAddr(addr)
			if ip == nil {
				continue
			}
			addresses = append(addresses, localInterfaceAddress{
				InterfaceName: iface.Name,
				IP:            ip,
			})
		}
	}

	return buildLocalIPv4AddressOptions(addresses)
}

func ipFromAddr(addr net.Addr) net.IP {
	switch value := addr.(type) {
	case *net.IPNet:
		return value.IP
	case *net.IPAddr:
		return value.IP
	default:
		return nil
	}
}

func buildLocalIPv4AddressOptions(addresses []localInterfaceAddress) []LocalIPAddress {
	options := defaultLocalIPv4AddressOptions()
	seen := map[string]bool{allProxyHostsValue: true, defaultProxyHost: true}

	sort.SliceStable(addresses, func(i, j int) bool {
		if addresses[i].InterfaceName == addresses[j].InterfaceName {
			return addresses[i].IP.String() < addresses[j].IP.String()
		}
		return addresses[i].InterfaceName < addresses[j].InterfaceName
	})

	for _, addr := range addresses {
		ipv4 := addr.IP.To4()
		if ipv4 == nil || ipv4.IsLoopback() || ipv4.IsUnspecified() {
			continue
		}
		value := ipv4.String()
		if seen[value] {
			continue
		}
		seen[value] = true

		label := value
		if strings.TrimSpace(addr.InterfaceName) != "" {
			label = fmt.Sprintf("%s (%s)", value, addr.InterfaceName)
		}
		options = append(options, LocalIPAddress{
			Label:         label,
			Value:         value,
			InterfaceName: addr.InterfaceName,
		})
	}

	return options
}

func defaultLocalIPv4AddressOptions() []LocalIPAddress {
	return []LocalIPAddress{
		{Label: allProxyHostsLabel, Value: allProxyHostsValue},
		{Label: defaultProxyHost, Value: defaultProxyHost},
	}
}

func (s *SettingService) GetCACertificateInfo() (*CACertificateInfo, error) {
	certPath, keyPath, err := s.currentCAPaths()
	if err != nil {
		return nil, err
	}
	return inspectCACertificate(certPath, keyPath), nil
}

func (s *SettingService) GenerateCurrentCACertificate(req GenerateCACertificateRequest) (info *CACertificateInfo, err error) {
	certPath, keyPath, err := s.currentCAPaths()
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(certPath) == "" || strings.TrimSpace(keyPath) == "" {
		return nil, errors.New("ca cert path and ca key path cannot be empty")
	}

	if req.ValidDays <= 0 {
		req.ValidDays = defaultCACertValidDays
	}
	if strings.TrimSpace(req.CommonName) == "" {
		req.CommonName = defaultCACertCommonName
	}

	logger.G().Infof(
		"CA certificate generation started: cert=%s key=%s overwrite=%t commonName=%s validDays=%d",
		certPath,
		keyPath,
		req.Overwrite,
		req.CommonName,
		req.ValidDays,
	)
	defer func() {
		if err != nil {
			logger.G().Errorf("CA certificate generation failed: %v", err)
		}
	}()

	certExists := pathExists(certPath)
	keyExists := pathExists(keyPath)
	if (certExists || keyExists) && !req.Overwrite {
		return nil, errors.New("ca certificate or key already exists")
	}

	certPEM, keyPEM, err := generateCACertificatePEM(req.CommonName, req.ValidDays)
	if err != nil {
		return nil, err
	}

	if err = os.MkdirAll(filepath.Dir(certPath), 0755); err != nil {
		return nil, err
	}
	if err = os.MkdirAll(filepath.Dir(keyPath), 0755); err != nil {
		return nil, err
	}

	if req.Overwrite {
		timestamp := time.Now().Format("20060102150405")
		if certExists {
			logger.G().Infof("Backing up existing CA certificate: %s", certPath)
			if err = backupFile(certPath, timestamp); err != nil {
				return nil, err
			}
		}
		if keyExists {
			logger.G().Infof("Backing up existing CA key: %s", keyPath)
			if err = backupFile(keyPath, timestamp); err != nil {
				return nil, err
			}
		}
	}

	if err = atomicWriteFile(certPath, certPEM, 0644); err != nil {
		return nil, err
	}
	if err = atomicWriteFile(keyPath, keyPEM, 0600); err != nil {
		return nil, err
	}
	info = inspectCACertificate(certPath, keyPath)
	logger.G().Infof("CA certificate generation succeeded: cert=%s key=%s", certPath, keyPath)
	return info, nil
}

func (s *SettingService) Save() error {
	if s.repository == nil || s.repository.db == nil {
		return errors.New("settings database is not available")
	}
	if err := s.ensureLoaded(); err != nil {
		return err
	}

	s.persistMu.Lock()
	defer s.persistMu.Unlock()

	s.mu.RLock()
	s.publishProcessAttributionStateLocked()
	payload, err := json.Marshal(s.settings)
	s.mu.RUnlock()
	if err != nil {
		return fmt.Errorf("snapshot application settings: %w", err)
	}
	snapshot := new(Settings)
	if err := json.Unmarshal(payload, snapshot); err != nil {
		return fmt.Errorf("decode application settings snapshot: %w", err)
	}
	sanitizeShortcutSettings(snapshot)
	sanitizePythonPluginConfig(snapshot.PythonPluginConfig)
	return s.repository.save(context.Background(), snapshot)
}

// SaveTrafficTableConfig persists only the traffic_table section and updates
// the in-memory snapshot after the database write succeeds. The persistence
// mutex serializes this narrow database write with ordinary whole-settings Save
// calls.
func (s *SettingService) SaveTrafficTableConfig(config *TrafficTableConfig) error {
	if config == nil {
		return errors.New("traffic table config cannot be nil")
	}
	if s.repository == nil || s.repository.db == nil {
		return errors.New("settings database is not available")
	}
	if err := s.ensureLoaded(); err != nil {
		return err
	}

	normalized := cloneAndSanitizeTrafficTableConfig(config)
	s.persistMu.Lock()
	defer s.persistMu.Unlock()
	if err := s.repository.saveTrafficTableConfig(context.Background(), normalized); err != nil {
		return err
	}

	s.mu.Lock()
	s.settings.TrafficTableConfig = cloneAndSanitizeTrafficTableConfig(normalized)
	s.mu.Unlock()
	return nil
}

// SavePythonPluginConfig persists only the Python runtime section and updates
// the in-memory snapshot after the database write succeeds. The persistence
// mutex prevents whole-settings saves from racing this authoritative update.
//
//wails:ignore
func (s *SettingService) SavePythonPluginConfig(config *PythonPluginConfig) error {
	if config == nil {
		return errors.New("python plugin config cannot be nil")
	}
	if s.repository == nil || s.repository.db == nil {
		return errors.New("settings database is not available")
	}
	if err := s.ensureLoaded(); err != nil {
		return err
	}

	normalized := cloneAndSanitizePythonPluginConfig(config)
	s.persistMu.Lock()
	defer s.persistMu.Unlock()
	if err := s.repository.savePythonPluginConfig(context.Background(), normalized); err != nil {
		return err
	}

	s.mu.Lock()
	s.settings.PythonPluginConfig = cloneAndSanitizePythonPluginConfig(normalized)
	s.mu.Unlock()
	return nil
}

// GetShortcutConfig returns an isolated, sanitized shortcut configuration.
// Callers may safely mutate the returned value without changing the in-memory
// settings snapshot.
//
//wails:ignore
func (s *SettingService) GetShortcutConfig() (*ShortcutConfig, error) {
	if err := s.ensureLoaded(); err != nil {
		return nil, err
	}

	s.mu.RLock()
	config := cloneAndSanitizeShortcutConfig(s.settings.Shortcuts)
	s.mu.RUnlock()
	return config, nil
}

// SaveShortcutConfig persists only the shortcuts section and updates the
// in-memory snapshot after the database write succeeds. The persistence mutex
// serializes this narrow write with ordinary whole-settings Save calls so a
// stale snapshot cannot overwrite a newer shortcut configuration.
//
//wails:ignore
func (s *SettingService) SaveShortcutConfig(config *ShortcutConfig) error {
	if config == nil {
		return errors.New("shortcut config cannot be nil")
	}
	if s.repository == nil || s.repository.db == nil {
		return errors.New("settings database is not available")
	}
	if err := s.ensureLoaded(); err != nil {
		return err
	}

	normalized := cloneAndSanitizeShortcutConfig(config)
	s.persistMu.Lock()
	defer s.persistMu.Unlock()
	if err := s.repository.saveShortcutConfig(context.Background(), normalized); err != nil {
		return err
	}

	s.mu.Lock()
	s.settings.Shortcuts = cloneAndSanitizeShortcutConfig(normalized)
	s.mu.Unlock()
	return nil
}

// NormalizeShortcutConfig creates an isolated configuration using the same
// tolerant validation rules as settings persistence.
func NormalizeShortcutConfig(config *ShortcutConfig) *ShortcutConfig {
	return cloneAndSanitizeShortcutConfig(config)
}

func cloneAndSanitizeShortcutConfig(config *ShortcutConfig) *ShortcutConfig {
	clone := defaultShortcutConfig()
	if config == nil {
		return clone
	}
	for commandID, override := range config.Overrides {
		clonedOverride := ShortcutOverride{Scope: override.Scope}
		if override.Binding != nil {
			clonedOverride.Binding = &ShortcutBinding{
				Modifiers: append([]ShortcutModifier(nil), override.Binding.Modifiers...),
				Key:       override.Binding.Key,
			}
		}
		clone.Overrides[commandID] = clonedOverride
	}
	sanitizeShortcutConfig(clone)
	return clone
}

func (s *SettingService) Load() error {
	s.loadMu.Lock()
	defer s.loadMu.Unlock()

	return s.loadLocked()
}

func (s *SettingService) loadLocked() error {
	if s.repository == nil || s.repository.db == nil {
		return errors.New("settings database is not available")
	}
	settings, err := s.repository.load(context.Background())
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.settings = settings
	s.setupDefaultSettingsLocked()
	s.mu.Unlock()
	return nil
}

func (s *SettingService) setupDefaultSettingsLocked() {
	if s.settings.CommonConfig == nil {
		s.settings.CommonConfig = &CommonConfig{}
	}
	s.settings.CommonConfig.LogLevel = string(logger.NormalizeLogLevel(s.settings.CommonConfig.LogLevel))
	if !isValidThemeMode(s.settings.CommonConfig.ThemeMode) {
		s.settings.CommonConfig.ThemeMode = defaultThemeMode
	}
	if !isValidLanguage(s.settings.CommonConfig.Language) {
		s.settings.CommonConfig.Language = defaultLanguage
	}
	if s.settings.ProxyConfig == nil {
		s.settings.ProxyConfig = &ProxyConfig{
			Mode:          ProxyModeHTTP,
			Host:          defaultProxyHost,
			Port:          defaultProxyPort,
			CACertPath:    defaultProxyCACertPath,
			CAKeyPath:     defaultProxyCAKeyPath,
			UpstreamMode:  UpstreamProxyModeSystem,
			UpstreamProxy: "",
			DisableProxy:  false,
			DisableHTTP2:  false,
			SkipVerifyTLS: false,
		}
	}
	setupDefaultProxyConfig(s.settings.ProxyConfig)
	if s.settings.WindowConfig == nil {
		s.settings.WindowConfig = &WindowConfig{}
	}
	if !isValidWindowFrameMode(s.settings.WindowConfig.FrameMode) {
		s.settings.WindowConfig.FrameMode = WindowFrameModeCustom
	}
	if !isValidMainWindowCloseBehavior(s.settings.WindowConfig.MainWindowCloseBehavior) {
		s.settings.WindowConfig.MainWindowCloseBehavior = MainWindowCloseBehaviorHideToTray
	}
	if s.settings.WindowConfig.Width < appservice.MIN_WINDOW_WIDTH {
		s.settings.WindowConfig.Width = appservice.DEFAULT_WINDOW_WIDTH
	}
	if s.settings.WindowConfig.Height < appservice.MIN_WINDOW_HEIGHT {
		s.settings.WindowConfig.Height = appservice.DEFAULT_WINDOW_HEIGHT
	}
	if s.settings.CacheConfig == nil {
		s.settings.CacheConfig = &CacheConfig{
			BodyCacheThresholdBytes: bodycache.MaxBodyCacheThresholdBytes,
			MaxWsMessages:           1000,
		}
	} else {
		if s.settings.CacheConfig.BodyCacheThresholdBytes <= 0 {
			s.settings.CacheConfig.BodyCacheThresholdBytes = bodycache.MaxBodyCacheThresholdBytes
		}
		if s.settings.CacheConfig.MaxWsMessages <= 0 {
			s.settings.CacheConfig.MaxWsMessages = 1000
		}
	}
	if s.settings.HistoryRetentionConfig == nil {
		s.settings.HistoryRetentionConfig = defaultHistoryRetentionConfig()
	} else {
		sanitizeHistoryRetentionConfig(s.settings.HistoryRetentionConfig)
	}
	if s.settings.ProcessAttributionConfig == nil {
		s.settings.ProcessAttributionConfig = &ProcessAttributionConfig{Enabled: true}
	}
	if s.settings.TrafficTableConfig == nil {
		s.settings.TrafficTableConfig = &TrafficTableConfig{}
	}
	sanitizeTrafficTableConfig(s.settings.TrafficTableConfig)
	if s.settings.PythonPluginConfig == nil {
		s.settings.PythonPluginConfig = defaultPythonPluginConfig()
	} else {
		sanitizePythonPluginConfig(s.settings.PythonPluginConfig)
	}
	sanitizeShortcutSettings(s.settings)
	s.publishProcessAttributionStateLocked()
}

func (s *SettingService) publishProcessAttributionStateLocked() {
	enabled := true
	if s.settings != nil && s.settings.ProcessAttributionConfig != nil {
		enabled = s.settings.ProcessAttributionConfig.Enabled
	}
	s.processAttributionEnabled.Store(enabled)
	s.processAttributionReady.Store(true)
}

// Keep in sync with frontend/src/utils/traffic-table-columns.ts.
var trafficTableColumnOrder = []string{
	"id",
	"method",
	"host",
	"path",
	"process",
	"statusCode",
	"type",
	"destination",
	"protocol",
	"duration",
	"size",
}

func sanitizeTrafficTableConfig(config *TrafficTableConfig) {
	hidden := make(map[string]struct{}, len(config.HiddenColumns))
	valid := make(map[string]struct{}, len(trafficTableColumnOrder))
	for _, key := range trafficTableColumnOrder {
		valid[key] = struct{}{}
	}
	for _, key := range config.HiddenColumns {
		key = strings.TrimSpace(key)
		if _, ok := valid[key]; ok {
			hidden[key] = struct{}{}
		}
	}

	if len(hidden) == len(trafficTableColumnOrder) {
		delete(hidden, "id")
	}
	config.HiddenColumns = make([]string, 0, len(hidden))
	for _, key := range trafficTableColumnOrder {
		if _, ok := hidden[key]; ok {
			config.HiddenColumns = append(config.HiddenColumns, key)
		}
	}
}

func cloneAndSanitizeTrafficTableConfig(config *TrafficTableConfig) *TrafficTableConfig {
	clone := new(TrafficTableConfig)
	if config != nil {
		clone.HiddenColumns = append([]string(nil), config.HiddenColumns...)
	}
	sanitizeTrafficTableConfig(clone)
	return clone
}

func defaultPythonPluginConfig() *PythonPluginConfig {
	return &PythonPluginConfig{HookTimeoutMs: defaultPythonHookTimeoutMs}
}

func sanitizePythonPluginConfig(config *PythonPluginConfig) {
	if config == nil {
		return
	}
	config.InterpreterPath = strings.TrimSpace(config.InterpreterPath)
	switch {
	case config.HookTimeoutMs == 0:
		config.HookTimeoutMs = defaultPythonHookTimeoutMs
	case config.HookTimeoutMs < minPythonHookTimeoutMs:
		config.HookTimeoutMs = minPythonHookTimeoutMs
	case config.HookTimeoutMs > maxPythonHookTimeoutMs:
		config.HookTimeoutMs = maxPythonHookTimeoutMs
	}
}

func cloneAndSanitizePythonPluginConfig(config *PythonPluginConfig) *PythonPluginConfig {
	clone := defaultPythonPluginConfig()
	if config != nil {
		*clone = *config
	}
	sanitizePythonPluginConfig(clone)
	return clone
}

func sanitizeShortcutSettings(settings *Settings) {
	if settings.Shortcuts == nil {
		settings.Shortcuts = defaultShortcutConfig()
		return
	}
	sanitizeShortcutConfig(settings.Shortcuts)
}

func defaultShortcutConfig() *ShortcutConfig {
	return &ShortcutConfig{Overrides: make(map[string]ShortcutOverride)}
}

func decodeShortcutConfig(payload []byte) *ShortcutConfig {
	var raw struct {
		Overrides map[string]json.RawMessage `json:"overrides"`
	}
	if err := json.Unmarshal(payload, &raw); err != nil {
		return defaultShortcutConfig()
	}

	config := defaultShortcutConfig()
	for commandID, rawOverride := range raw.Overrides {
		override, ok := decodeShortcutOverride(rawOverride)
		if !ok {
			continue
		}
		config.Overrides[commandID] = override
	}
	sanitizeShortcutConfig(config)
	return config
}

func decodeShortcutOverride(payload json.RawMessage) (ShortcutOverride, bool) {
	var raw struct {
		Binding json.RawMessage `json:"binding"`
		Scope   ShortcutScope   `json:"scope"`
	}
	if err := json.Unmarshal(payload, &raw); err != nil || raw.Binding == nil {
		return ShortcutOverride{}, false
	}

	override := ShortcutOverride{Scope: raw.Scope}
	if string(raw.Binding) == "null" {
		return override, true
	}
	var binding ShortcutBinding
	if err := json.Unmarshal(raw.Binding, &binding); err != nil {
		return ShortcutOverride{}, false
	}
	override.Binding = &binding
	return override, true
}

func sanitizeShortcutConfig(config *ShortcutConfig) {
	if config == nil {
		return
	}
	if config.Overrides == nil {
		config.Overrides = make(map[string]ShortcutOverride)
		return
	}

	commandIDs := make([]string, 0, len(config.Overrides))
	for commandID := range config.Overrides {
		commandIDs = append(commandIDs, commandID)
	}
	sort.Strings(commandIDs)

	overrides := make(map[string]ShortcutOverride, min(len(commandIDs), maxShortcutOverrides))
	for _, commandID := range commandIDs {
		if len(overrides) == maxShortcutOverrides {
			break
		}
		canonicalCommandID, ok := sanitizeShortcutCommandID(commandID)
		if !ok {
			continue
		}
		override, ok := sanitizeShortcutOverride(config.Overrides[commandID])
		if !ok {
			continue
		}
		overrides[canonicalCommandID] = override
	}
	config.Overrides = overrides
}

const (
	maxShortcutOverrides = 256
	maxShortcutCommandID = 128
	maxShortcutModifiers = 5
	maxShortcutKeyLength = 32
)

func sanitizeShortcutCommandID(commandID string) (string, bool) {
	if utf8.RuneCountInString(commandID) == 0 || utf8.RuneCountInString(commandID) > maxShortcutCommandID {
		return "", false
	}
	for segment := range strings.SplitSeq(commandID, ".") {
		if !isValidShortcutCommandSegment(segment) {
			return "", false
		}
	}
	return commandID, true
}

func isValidShortcutCommandSegment(segment string) bool {
	if segment == "" || !isASCIIAlpha(segment[0]) {
		return false
	}
	for i := 1; i < len(segment); i++ {
		if !isASCIIAlpha(segment[i]) && !isASCIIDigit(segment[i]) && segment[i] != '_' && segment[i] != '-' {
			return false
		}
	}
	return true
}

func isASCIIAlpha(value byte) bool {
	return value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z'
}

func isASCIIDigit(value byte) bool {
	return value >= '0' && value <= '9'
}

func sanitizeShortcutOverride(override ShortcutOverride) (ShortcutOverride, bool) {
	override.Scope = ShortcutScope(canonicalizeShortcutString(strings.TrimSpace(string(override.Scope))))
	if override.Scope != ShortcutScopeApplication && override.Scope != ShortcutScopeGlobal {
		return ShortcutOverride{}, false
	}
	if override.Binding == nil {
		return override, true
	}
	binding, ok := sanitizeShortcutBinding(*override.Binding)
	if !ok {
		return ShortcutOverride{}, false
	}
	override.Binding = &binding
	return override, true
}

func sanitizeShortcutBinding(binding ShortcutBinding) (ShortcutBinding, bool) {
	key := binding.Key
	if key != " " {
		key = strings.TrimSpace(key)
	}
	key = canonicalizeShortcutKey(key)
	if utf8.RuneCountInString(key) == 0 || utf8.RuneCountInString(key) > maxShortcutKeyLength {
		return ShortcutBinding{}, false
	}

	seen := make(map[ShortcutModifier]bool, maxShortcutModifiers)
	for _, modifier := range binding.Modifiers {
		modifier = ShortcutModifier(canonicalizeShortcutString(strings.TrimSpace(string(modifier))))
		if !isRecognizedShortcutModifier(modifier) {
			continue
		}
		seen[modifier] = true
	}

	modifiers := make([]ShortcutModifier, 0, len(seen))
	for _, modifier := range []ShortcutModifier{
		ShortcutModifierPrimary,
		ShortcutModifierControl,
		ShortcutModifierAlt,
		ShortcutModifierShift,
		ShortcutModifierSuper,
	} {
		if seen[modifier] {
			modifiers = append(modifiers, modifier)
		}
	}
	binding.Modifiers = modifiers
	binding.Key = key
	return binding, true
}

func isRecognizedShortcutModifier(modifier ShortcutModifier) bool {
	switch modifier {
	case ShortcutModifierPrimary,
		ShortcutModifierControl,
		ShortcutModifierAlt,
		ShortcutModifierShift,
		ShortcutModifierSuper:
		return true
	default:
		return false
	}
}

func canonicalizeShortcutString(value string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'A' && r <= 'Z' {
			return r + ('a' - 'A')
		}
		return r
	}, value)
}

func canonicalizeShortcutKey(key string) string {
	if len(key) == 1 && key[0] >= 'A' && key[0] <= 'Z' {
		return string(key[0] + ('a' - 'A'))
	}
	if canonical, ok := canonicalShortcutNamedKeys[strings.ToLower(key)]; ok {
		return canonical
	}
	return key
}

var canonicalShortcutNamedKeys = func() map[string]string {
	keys := []string{
		"Alt", "AltGraph", "CapsLock", "Control", "Fn", "FnLock", "Hyper", "Meta", "NumLock", "ScrollLock", "Shift", "Super", "Symbol", "SymbolLock",
		"Enter", "Tab",
		"ArrowDown", "ArrowLeft", "ArrowRight", "ArrowUp", "End", "Home", "PageDown", "PageUp",
		"Backspace", "Clear", "Copy", "CrSel", "Cut", "Delete", "EraseEof", "ExSel", "Insert", "Paste", "Redo", "Undo",
		"Accept", "Again", "Attn", "Cancel", "ContextMenu", "Escape", "Execute", "Find", "Finish", "Help", "Pause", "Play", "Props", "Select", "ZoomIn", "ZoomOut",
		"BrightnessDown", "BrightnessUp", "Eject", "LogOff", "Power", "PowerOff", "PrintScreen", "Hibernate", "Standby", "WakeUp",
		"AudioVolumeDown", "AudioVolumeMute", "AudioVolumeUp", "MediaTrackNext", "MediaTrackPrevious", "MediaPlayPause", "MediaStop",
		"LaunchApplication1", "LaunchApplication2", "LaunchMail", "LaunchMediaPlayer",
	}
	for i := 1; i <= 24; i++ {
		keys = append(keys, fmt.Sprintf("F%d", i))
	}
	canonicalKeys := make(map[string]string, len(keys))
	for _, key := range keys {
		canonicalKeys[strings.ToLower(key)] = key
	}
	return canonicalKeys
}()

func defaultHistoryRetentionConfig() *HistoryRetentionConfig {
	return &HistoryRetentionConfig{
		Enabled: false,
		Value:   defaultHistoryRetentionValue,
		Unit:    HistoryRetentionUnitDay,
	}
}

func sanitizeHistoryRetentionConfig(cfg *HistoryRetentionConfig) {
	if cfg == nil {
		return
	}
	if cfg.Value >= minHistoryRetentionValue &&
		cfg.Value <= maxHistoryRetentionValue &&
		isValidHistoryRetentionUnit(cfg.Unit) {
		return
	}
	logger.G().Warnf("Invalid history retention config, disabling cleanup: value=%d unit=%q", cfg.Value, cfg.Unit)
	*cfg = *defaultHistoryRetentionConfig()
}

func isValidHistoryRetentionUnit(unit HistoryRetentionUnit) bool {
	switch unit {
	case HistoryRetentionUnitHour,
		HistoryRetentionUnitDay,
		HistoryRetentionUnitWeek,
		HistoryRetentionUnitMonth,
		HistoryRetentionUnitYear:
		return true
	default:
		return false
	}
}

func setupDefaultProxyConfig(cfg *ProxyConfig) {
	if cfg == nil {
		return
	}
	if cfg.Mode != ProxyModeHTTP && cfg.Mode != ProxyModeSOCKS5 {
		cfg.Mode = ProxyModeHTTP
	}
	if strings.TrimSpace(cfg.Host) == "" {
		cfg.Host = defaultProxyHost
	}
	if cfg.Port <= 0 || cfg.Port > 65535 {
		cfg.Port = defaultProxyPort
	}
	if strings.TrimSpace(cfg.CACertPath) == "" {
		cfg.CACertPath = defaultProxyCACertPath
	}
	if strings.TrimSpace(cfg.CAKeyPath) == "" {
		cfg.CAKeyPath = defaultProxyCAKeyPath
	}
	cfg.UpstreamMode = sanitizeUpstreamProxyMode(cfg.UpstreamMode, cfg.UpstreamProxy, cfg.DisableProxy)
	cfg.IncludeHosts = sanitizeStringList(cfg.IncludeHosts)
	cfg.ExcludeHosts = sanitizeStringList(cfg.ExcludeHosts)
	cfg.RootCAPaths = sanitizeStringList(cfg.RootCAPaths)
	cfg.ClientCerts = sanitizeClientCerts(cfg.ClientCerts)
}

func sanitizeUpstreamProxyMode(mode UpstreamProxyMode, upstreamProxy string, disableProxy bool) UpstreamProxyMode {
	switch mode {
	case UpstreamProxyModeNone, UpstreamProxyModeSystem, UpstreamProxyModeCustom:
		return mode
	}
	if strings.TrimSpace(upstreamProxy) != "" {
		return UpstreamProxyModeCustom
	}
	if disableProxy {
		return UpstreamProxyModeNone
	}
	return UpstreamProxyModeSystem
}

func sanitizeStringList(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		out = append(out, value)
	}
	return out
}

func sanitizeClientCerts(values []ClientCertConfig) []ClientCertConfig {
	if len(values) == 0 {
		return nil
	}
	out := make([]ClientCertConfig, 0, len(values))
	for _, value := range values {
		value.Hostname = strings.TrimSpace(value.Hostname)
		value.CertPath = strings.TrimSpace(value.CertPath)
		value.KeyPath = strings.TrimSpace(value.KeyPath)
		if value.Hostname == "" && value.CertPath == "" && value.KeyPath == "" {
			continue
		}
		out = append(out, value)
	}
	return out
}

func isValidThemeMode(mode string) bool {
	return mode == "auto" || mode == "light" || mode == "dark"
}

func isValidLanguage(language string) bool {
	return language == "zh" || language == "en"
}

func isValidWindowFrameMode(mode WindowFrameMode) bool {
	return mode == WindowFrameModeCustom || mode == WindowFrameModeSystem
}

func isValidMainWindowCloseBehavior(behavior MainWindowCloseBehavior) bool {
	return behavior == MainWindowCloseBehaviorHideToTray || behavior == MainWindowCloseBehaviorQuit
}

func (s *SettingService) currentCAPaths() (string, string, error) {
	if err := s.ensureLoaded(); err != nil {
		return "", "", err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.settings == nil || s.settings.ProxyConfig == nil {
		return "", "", errors.New("proxy config not loaded")
	}
	return s.settings.ProxyConfig.CACertPath, s.settings.ProxyConfig.CAKeyPath, nil
}

func inspectCACertificate(certPath, keyPath string) *CACertificateInfo {
	info := &CACertificateInfo{
		CertPath:   certPath,
		KeyPath:    keyPath,
		CertExists: pathExists(certPath),
		KeyExists:  pathExists(keyPath),
	}
	if !info.CertExists || !info.KeyExists {
		info.Error = "ca certificate or key does not exist"
		return info
	}

	pair, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		info.Error = err.Error()
		return info
	}
	if len(pair.Certificate) == 0 {
		info.Error = "ca certificate is empty"
		return info
	}

	cert, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		info.Error = err.Error()
		return info
	}
	info.ValidPair = true
	info.IsCA = cert.IsCA
	info.Subject = cert.Subject.String()
	info.Issuer = cert.Issuer.String()
	info.SerialNumber = strings.ToUpper(cert.SerialNumber.Text(16))
	info.NotBeforeMicros = cert.NotBefore.UnixMicro()
	info.NotAfterMicros = cert.NotAfter.UnixMicro()
	fingerprint := sha256.Sum256(cert.Raw)
	info.SHA256Fingerprint = colonHex(fingerprint[:])
	return info
}

func generateCACertificatePEM(commonName string, validDays int) ([]byte, []byte, error) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, err
	}
	if err = privateKey.Validate(); err != nil {
		return nil, nil, err
	}
	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, err
	}
	now := time.Now()
	template := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			CommonName: commonName,
		},
		NotBefore:             now,
		NotAfter:              now.AddDate(0, 0, validDays),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &privateKey.PublicKey, privateKey)
	if err != nil {
		return nil, nil, err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(privateKey)})
	return certPEM, keyPEM, nil
}

func backupFile(path, timestamp string) error {
	backupPath := fmt.Sprintf("%s.%s.bak", path, timestamp)
	return os.Rename(path, backupPath)
}

func atomicWriteFile(path string, content []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if _, err = tmp.Write(content); err != nil {
		tmp.Close()
		return err
	}
	if err = tmp.Chmod(perm); err != nil {
		tmp.Close()
		return err
	}
	if err = tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func colonHex(bytes []byte) string {
	raw := strings.ToUpper(hex.EncodeToString(bytes))
	parts := make([]string, 0, len(raw)/2)
	for i := 0; i < len(raw); i += 2 {
		parts = append(parts, raw[i:i+2])
	}
	return strings.Join(parts, ":")
}
