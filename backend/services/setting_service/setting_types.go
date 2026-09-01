package settingservice

type ProxyMode string

const (
	ProxyModeHTTP   ProxyMode = "http"
	ProxyModeSOCKS5 ProxyMode = "socks5"
)

type UpstreamProxyMode string

const (
	UpstreamProxyModeNone   UpstreamProxyMode = "none"
	UpstreamProxyModeSystem UpstreamProxyMode = "system"
	UpstreamProxyModeCustom UpstreamProxyMode = "custom"
)

type WindowFrameMode string

const (
	WindowFrameModeCustom WindowFrameMode = "custom"
	WindowFrameModeSystem WindowFrameMode = "system"
)

type MainWindowCloseBehavior string

const (
	MainWindowCloseBehaviorHideToTray MainWindowCloseBehavior = "hide_to_tray"
	MainWindowCloseBehaviorQuit       MainWindowCloseBehavior = "quit"
)

type HistoryRetentionUnit string

const (
	HistoryRetentionUnitHour  HistoryRetentionUnit = "hour"
	HistoryRetentionUnitDay   HistoryRetentionUnit = "day"
	HistoryRetentionUnitWeek  HistoryRetentionUnit = "week"
	HistoryRetentionUnitMonth HistoryRetentionUnit = "month"
	HistoryRetentionUnitYear  HistoryRetentionUnit = "year"
)

type ShortcutScope string

const (
	ShortcutScopeApplication ShortcutScope = "application"
	ShortcutScopeGlobal      ShortcutScope = "global"
)

type ShortcutModifier string

const (
	ShortcutModifierPrimary ShortcutModifier = "primary"
	ShortcutModifierControl ShortcutModifier = "control"
	ShortcutModifierAlt     ShortcutModifier = "alt"
	ShortcutModifierShift   ShortcutModifier = "shift"
	ShortcutModifierSuper   ShortcutModifier = "super"
)

type CommonConfig struct {
	LogLevel       string `json:"logLevel"`
	LogDisabled    bool   `json:"logDisabled"`
	AppFontFamily  string `json:"appFontFamily"`
	CodeFontFamily string `json:"codeFontFamily"`
	ThemeMode      string `json:"themeMode"`
	Language       string `json:"language"`
}

type ProxyConfig struct {
	Mode          ProxyMode          `json:"mode"`
	Host          string             `json:"host"`
	Port          int                `json:"port"`
	CACertPath    string             `json:"caCertPath"`
	CAKeyPath     string             `json:"caKeyPath"`
	UpstreamMode  UpstreamProxyMode  `json:"upstreamProxyMode"`
	UpstreamProxy string             `json:"upstreamProxy"`
	DisableProxy  bool               `json:"disableProxy"`
	DisableHTTP2  bool               `json:"disableHttp2"`
	SkipVerifyTLS bool               `json:"skipVerifyTls"`
	IncludeHosts  []string           `json:"includeHosts"`
	ExcludeHosts  []string           `json:"excludeHosts"`
	RootCAPaths   []string           `json:"rootCAPaths"`
	ClientCerts   []ClientCertConfig `json:"clientCerts"`
}

type ClientCertConfig struct {
	Enabled  bool   `json:"enabled"`
	Hostname string `json:"hostname"`
	CertPath string `json:"certPath"`
	KeyPath  string `json:"keyPath"`
}

type WindowConfig struct {
	PositionX               int                     `json:"positionX"`
	PositionY               int                     `json:"positionY"`
	Width                   int                     `json:"width"`
	Height                  int                     `json:"height"`
	HasPosition             bool                    `json:"hasPosition"`
	IsMaximized             bool                    `json:"isMaximized"`
	IsFullScreen            bool                    `json:"isFullScreen"`
	FrameMode               WindowFrameMode         `json:"frameMode"`
	MainWindowCloseBehavior MainWindowCloseBehavior `json:"mainWindowCloseBehavior"`
}

type CacheConfig struct {
	BodyCacheThresholdBytes int64 `json:"bodyCacheThresholdBytes"`
	MaxWsMessages           int   `json:"maxWsMessages"`
}

type HistoryRetentionConfig struct {
	Enabled bool                 `json:"enabled"`
	Value   int                  `json:"value"`
	Unit    HistoryRetentionUnit `json:"unit"`
}

type ProcessAttributionConfig struct {
	Enabled bool `json:"enabled"`
}

type TrafficTableConfig struct {
	HiddenColumns []string `json:"hiddenColumns"`
}

type PythonPluginConfig struct {
	Enabled         bool   `json:"enabled"`
	InterpreterPath string `json:"interpreterPath"`
	HookTimeoutMs   int    `json:"hookTimeoutMs"`
}

type ShortcutConfig struct {
	Overrides map[string]ShortcutOverride `json:"overrides"`
}

type ShortcutOverride struct {
	// A nil Binding represents an explicit disabled shortcut.
	Binding *ShortcutBinding `json:"binding"`
	Scope   ShortcutScope    `json:"scope"`
}

type ShortcutBinding struct {
	Modifiers []ShortcutModifier `json:"modifiers"`
	Key       string             `json:"key"`
}

type Settings struct {
	CommonConfig             *CommonConfig             `json:"commonConfig"`
	ProxyConfig              *ProxyConfig              `json:"proxyConfig"`
	WindowConfig             *WindowConfig             `json:"windowConfig"`
	CacheConfig              *CacheConfig              `json:"cacheConfig"`
	HistoryRetentionConfig   *HistoryRetentionConfig   `json:"historyRetentionConfig"`
	ProcessAttributionConfig *ProcessAttributionConfig `json:"processAttributionConfig"`
	TrafficTableConfig       *TrafficTableConfig       `json:"trafficTableConfig"`
	PythonPluginConfig       *PythonPluginConfig       `json:"pythonPluginConfig"`
	Shortcuts                *ShortcutConfig           `json:"shortcuts"`
}

type FontOption struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

type LocalIPAddress struct {
	Label         string `json:"label"`
	Value         string `json:"value"`
	InterfaceName string `json:"interfaceName,omitempty"`
}

type CACertificateInfo struct {
	CertPath          string `json:"certPath"`
	KeyPath           string `json:"keyPath"`
	CertExists        bool   `json:"certExists"`
	KeyExists         bool   `json:"keyExists"`
	ValidPair         bool   `json:"validPair"`
	IsCA              bool   `json:"isCa"`
	Subject           string `json:"subject"`
	Issuer            string `json:"issuer"`
	SerialNumber      string `json:"serialNumber"`
	NotBeforeMicros   int64  `json:"notBeforeMicros"`
	NotAfterMicros    int64  `json:"notAfterMicros"`
	SHA256Fingerprint string `json:"sha256Fingerprint"`
	Error             string `json:"error"`
}

type GenerateCACertificateRequest struct {
	Overwrite  bool   `json:"overwrite"`
	CommonName string `json:"commonName"`
	ValidDays  int    `json:"validDays"`
}

type LogConfig struct {
	Enabled bool
	Level   string
}
