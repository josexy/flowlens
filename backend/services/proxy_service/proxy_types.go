package proxyservice

import (
	"crypto/x509/pkix"
	"io"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/josexy/flowlens/backend/pkg/fs"
	settingservice "github.com/josexy/flowlens/backend/services/setting_service"
)

const UserAgentHeader = "FlowLens/1.0"

type ProxyStatus struct {
	Running      bool      `json:"running"`
	Address      string    `json:"address"`
	Started      time.Time `json:"started"`
	CaptureAlias string    `json:"captureAlias"`
}

type ProxyConfigApplyResult struct {
	Applied         bool     `json:"applied"`
	RestartRequired bool     `json:"restartRequired"`
	RestartReasons  []string `json:"restartReasons"`
}

type SystemProxyStatus struct {
	Supported     bool                     `json:"supported"`
	ModeSupported bool                     `json:"modeSupported"`
	Active        bool                     `json:"active"`
	Address       string                   `json:"address"`
	Mode          settingservice.ProxyMode `json:"mode"`
}

type ProcessStatus string

const (
	ProcessStatusPending          ProcessStatus = "pending"
	ProcessStatusResolved         ProcessStatus = "resolved"
	ProcessStatusRemote           ProcessStatus = "remote"
	ProcessStatusNotFound         ProcessStatus = "not_found"
	ProcessStatusPermissionDenied ProcessStatus = "permission_denied"
	ProcessStatusUnsupported      ProcessStatus = "unsupported"
	ProcessStatusAmbiguous        ProcessStatus = "ambiguous"
)

type ProcessInfo struct {
	Status             ProcessStatus `json:"status"`
	PID                uint32        `json:"pid,omitempty"`
	DisplayName        string        `json:"displayName,omitempty"`
	ProcessName        string        `json:"processName,omitempty"`
	ExecutablePath     string        `json:"executablePath,omitempty"`
	AppID              string        `json:"appId,omitempty"`
	IconKey            string        `json:"iconKey,omitempty"`
	Source             string        `json:"source,omitempty"`
	IdentityConfidence string        `json:"identityConfidence,omitempty"`
	UnavailableReason  string        `json:"unavailableReason,omitempty"`
}

type ProcessIconData struct {
	MIMEType   string `json:"mimeType"`
	DataBase64 string `json:"dataBase64"`
}

type Metadata struct {
	LocalSourceAddr               string             `json:"localSourceAddr,omitempty"`
	LocalDestinationAddr          string             `json:"localDestinationAddr,omitempty"`
	RemoteSourceAddr              string             `json:"remoteSourceAddr,omitempty"`
	RemoteDestinationAddr         string             `json:"remoteDestinationAddr,omitempty"`
	LocalConnectionEstablishedAt  time.Time          `json:"localConnectionEstablishedAt"`
	RemoteConnectionEstablishedAt time.Time          `json:"remoteConnectionEstablishedAt"`
	RequestProcessedAt            time.Time          `json:"requestProcessedAt"`
	SSLHandshakeCompletedAt       time.Time          `json:"sslHandshakeCompletedAt"`
	TLS                           *TLSState          `json:"tls,omitempty"`
	Certificate                   *ServerCertificate `json:"certificate,omitempty"`
	Process                       *ProcessInfo       `json:"process,omitempty"`
}

type TLSState struct {
	ServerName            string   `json:"serverName,omitempty"`
	SupportedALPN         []string `json:"supportedAlpn,omitempty"`
	SupportedVersion      []string `json:"supportedVersion,omitempty"`
	SupportedCipherSuites []string `json:"supportedCipherSuites,omitempty"`
	SelectedALPN          string   `json:"selectedAlpn,omitempty"`
	SelectedVersion       string   `json:"selectedVersion,omitempty"`
	SelectedCipherSuite   string   `json:"selectedCipherSuite,omitempty"`
}

type ServerCertificate struct {
	Version            int       `json:"version,omitempty"`
	NotBeforeMicros    int64     `json:"notBeforeMicros,omitempty"`
	NotAfterMicros     int64     `json:"notAfterMicros,omitempty"`
	Subject            *PkixName `json:"subject,omitempty"`
	Issuer             *PkixName `json:"issuer,omitempty"`
	DNSNames           []string  `json:"dnsNames,omitempty"`
	IPAddresses        []string  `json:"ipAddresses,omitempty"`
	SerialNumber       string    `json:"serialNumber,omitempty"`
	SignatureAlgorithm string    `json:"signatureAlgorithm,omitempty"`
	Sha1Fingerprint    string    `json:"sha1Fingerprint,omitempty"`
	Sha256Fingerprint  string    `json:"sha256Fingerprint,omitempty"`
}

type RawTCPTunnelSource string

const (
	RawTCPTunnelSourceDirect      RawTCPTunnelSource = "direct"
	RawTCPTunnelSourceHTTPConnect RawTCPTunnelSource = "http_connect"
	RawTCPTunnelSourceSOCKS5      RawTCPTunnelSource = "socks5"
	RawTCPTunnelSourceUnknown     RawTCPTunnelSource = "unknown"
)

type RawTCPTunnelInfo struct {
	Source   RawTCPTunnelSource `json:"source"`
	HostPort string             `json:"hostPort"`
	TLS      bool               `json:"tls"`
}

type PkixName struct {
	Country            []string `json:"country,omitempty"`
	Organization       []string `json:"organization,omitempty"`
	OrganizationalUnit []string `json:"organizationalUnit,omitempty"`
	Locality           []string `json:"locality,omitempty"`
	Province           []string `json:"province,omitempty"`
	StreetAddress      []string `json:"streetAddress,omitempty"`
	PostalCode         []string `json:"postalCode,omitempty"`
	SerialNumber       string   `json:"serialNumber,omitempty"`
	CommonName         string   `json:"commonName,omitempty"`
}

type TrafficEntry struct {
	ID uint64 `json:"id"`
	// Revision orders the initial live snapshot and its later patch events. It
	// is runtime-only and intentionally omitted from HBIN encoding.
	Revision   uint64            `json:"revision,omitempty"`
	Type       string            `json:"type"` // "http", "https", "ws", "wss", "tcp"
	StartedAt  time.Time         `json:"startedAt"`
	Method     string            `json:"method"`
	URL        string            `json:"url"`
	Host       string            `json:"host"`
	Path       string            `json:"path"`
	StatusCode int               `json:"statusCode"`
	Status     string            `json:"status"`
	Metadata   *Metadata         `json:"metadata,omitempty"`
	RawTCP     *RawTCPTunnelInfo `json:"rawTcp,omitempty"`
	Request    *HTTPMessage      `json:"request,omitempty"`
	Response   *HTTPMessage      `json:"response,omitempty"`
	Error      *TrafficError     `json:"error,omitempty"`

	// captureGeneration is intentionally private: it isolates live interceptor
	// work without changing the Wails model or persisted traffic format.
	captureGeneration uint64
	lifecycle         *trafficEntryLifecycle
}

// trafficEntryLifecycle is shared by the interceptor's working entry and all
// immutable snapshots derived from it. Deletion flips the token once, closing
// the late-callback race without retaining every deleted ID for the rest of a
// capture session.
type trafficEntryLifecycle struct {
	deleted atomic.Bool
}

type TrafficError struct {
	Timestamp time.Time `json:"timestamp"`
	Error     string    `json:"error"`
}

type HTTPHeaderField struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type HTTPMessageState string

const (
	HTTPMessageStatePending   HTTPMessageState = "pending"
	HTTPMessageStateCompleted HTTPMessageState = "completed"
	HTTPMessageStateFailed    HTTPMessageState = "failed"
	HTTPMessageStateCanceled  HTTPMessageState = "canceled"
)

type HTTPMessageMetrics struct {
	StartedAtMicros int64            `json:"startedAtMicros"`
	EndedAtMicros   int64            `json:"endedAtMicros"`
	HeaderSize      int64            `json:"headerSize"`
	BodySize        int64            `json:"bodySize"`
	State           HTTPMessageState `json:"state"`
}

// TrafficEntryPatch is the strongly typed incremental event sent after the
// initial traffic:entry snapshot. A patch may update more than one related
// section atomically, for example response headers together with the timing
// values known at that transport milestone.
type TrafficEntryPatch struct {
	TrafficID        uint64                        `json:"trafficId"`
	Revision         uint64                        `json:"revision"`
	ResponseHeaders  *TrafficResponseHeadersPatch  `json:"responseHeaders,omitempty"`
	ResponseTrailers *TrafficResponseTrailersPatch `json:"responseTrailers,omitempty"`
	Metrics          *TrafficMetricsPatch          `json:"metrics,omitempty"`
	Process          *ProcessInfo                  `json:"process,omitempty"`
	Error            *TrafficError                 `json:"error,omitempty"`
}

type TrafficResponseHeadersPatch struct {
	StatusCode             int               `json:"statusCode"`
	Status                 string            `json:"status"`
	Proto                  string            `json:"proto"`
	HeaderFields           []HTTPHeaderField `json:"headerFields"`
	HeadersTruncated       bool              `json:"headersTruncated"`
	HeaderOrderUnavailable bool              `json:"headerOrderUnavailable"`
}

type TrafficResponseTrailersPatch struct {
	TrailerFields           []HTTPHeaderField `json:"trailerFields"`
	TrailersTruncated       bool              `json:"trailersTruncated"`
	TrailerOrderUnavailable bool              `json:"trailerOrderUnavailable"`
}

type TrafficMetricsPatch struct {
	Request  *HTTPMessageMetrics `json:"request,omitempty"`
	Response *HTTPMessageMetrics `json:"response,omitempty"`
}

type HTTPMessage struct {
	Proto                   string              `json:"proto"`
	HeaderFields            []HTTPHeaderField   `json:"headerFields"`
	HeadersTruncated        bool                `json:"headersTruncated,omitempty"`
	HeaderOrderUnavailable  bool                `json:"headerOrderUnavailable,omitempty"`
	TrailerFields           []HTTPHeaderField   `json:"trailerFields"`
	TrailersTruncated       bool                `json:"trailersTruncated,omitempty"`
	TrailerOrderUnavailable bool                `json:"trailerOrderUnavailable,omitempty"`
	Metrics                 *HTTPMessageMetrics `json:"metrics,omitempty"`
}

type WebSocketMessage struct {
	Direction string        `json:"direction"` // "send" or "receive"
	MsgType   string        `json:"msgType"`   // "text" or "binary"
	Data      string        `json:"data"`
	DataSize  int           `json:"dataSize"`
	Error     *TrafficError `json:"error,omitempty"`
}

type TrafficLiveUpdateKind string

const (
	TrafficLiveUpdateSSEChunk           TrafficLiveUpdateKind = "sse-chunk"
	TrafficLiveUpdateWebSocketMessage   TrafficLiveUpdateKind = "websocket-message"
	TrafficLiveUpdateWebSocketTruncated TrafficLiveUpdateKind = "websocket-truncated"
)

// TrafficLiveUpdate is emitted for the traffic entry currently displayed in
// the capture detail panel.
type TrafficLiveUpdate struct {
	TrafficID    uint64                `json:"trafficId"`
	Kind         TrafficLiveUpdateKind `json:"kind"`
	Offset       *int64                `json:"offset,omitempty"`
	ChunkBase64  string                `json:"chunkBase64,omitempty"`
	MessageIndex *int                  `json:"messageIndex,omitempty"`
	Message      *WebSocketMessage     `json:"message,omitempty"`
}

type TrafficBodyView struct {
	RequestBody          string              `json:"reqBody"`
	ResponseBody         string              `json:"rspBody"`
	RequestBodyEncoding  string              `json:"reqBodyEnc,omitempty"` // "" or "base64"
	ResponseBodyEncoding string              `json:"rspBodyEnc,omitempty"` // "" or "base64"
	WebSocketMessages    []*WebSocketMessage `json:"wsMsgs,omitempty"`
	WsMsgsTruncated      bool                `json:"wsMsgsTruncated,omitempty"`
}

type trafficBodyViewInner struct {
	RequestBodyReader       io.ReadCloser
	ResponseBodyReader      io.ReadCloser
	RequestBodySize         int64
	ResponseBodySize        int64
	RequestBodyEncoding     string
	ResponseBodyEncoding    string
	RequestBodyUnavailable  bool
	ResponseBodyUnavailable bool
	WebSocketMessages       []*WebSocketMessage
	WsMsgsTruncated         bool
}

func (v *trafficBodyViewInner) closeReqBodyReaderSafely() {
	if v != nil && v.RequestBodyReader != nil {
		v.RequestBodyReader.Close()
		v.RequestBodyReader = nil
	}
}

func (v *trafficBodyViewInner) closeRspBodyReaderSafely() {
	if v != nil && v.ResponseBodyReader != nil {
		v.ResponseBodyReader.Close()
		v.ResponseBodyReader = nil
	}
}

type TrafficStatistics struct {
	Total     int64 `json:"total"`
	TotalHTTP int64 `json:"totalHttp"`
	TotalWS   int64 `json:"totalWs"`
	TotalTCP  int64 `json:"totalTcp"`
}

type LocalDataSize struct {
	CacheBytes   int64 `json:"cacheBytes"`
	HistoryBytes int64 `json:"historyBytes"`
}

type SendRequestProxyMode string

const (
	SendRequestProxyModeNone   SendRequestProxyMode = "none"
	SendRequestProxyModeSystem SendRequestProxyMode = "system"
	SendRequestProxyModeMITM   SendRequestProxyMode = "mitm"
	SendRequestProxyModeCustom SendRequestProxyMode = "custom"
)

type SendRequestProtocol string

const (
	SendRequestProtocolAuto  SendRequestProtocol = "auto"
	SendRequestProtocolHTTP1 SendRequestProtocol = "http1"
	SendRequestProtocolHTTP2 SendRequestProtocol = "http2"
)

type SendRequestBodyType string

type TLSClientHelloID string

const (
	TLSClientHelloGolang          TLSClientHelloID = "golang"
	TLSClientHelloChromeAuto      TLSClientHelloID = "chrome_auto"
	TLSClientHelloFirefoxAuto     TLSClientHelloID = "firefox_auto"
	TLSClientHelloSafariAuto      TLSClientHelloID = "safari_auto"
	TLSClientHelloEdgeAuto        TLSClientHelloID = "edge_auto"
	TLSClientHelloIOSAuto         TLSClientHelloID = "ios_auto"
	TLSClientHelloAndroid11OkHTTP TLSClientHelloID = "android_11_okhttp"
	TLSClientHelloRandomizedALPN  TLSClientHelloID = "randomized_alpn"
)

const (
	SendRequestBodyTypeNone       SendRequestBodyType = "none"
	SendRequestBodyTypeJSON       SendRequestBodyType = "json"
	SendRequestBodyTypeText       SendRequestBodyType = "text"
	SendRequestBodyTypeXML        SendRequestBodyType = "xml"
	SendRequestBodyTypeBinary     SendRequestBodyType = "binary"
	SendRequestBodyTypeFile       SendRequestBodyType = "file"
	SendRequestBodyTypeFormData   SendRequestBodyType = "form-data"
	SendRequestBodyTypeURLEncoded SendRequestBodyType = "urlencoded"
)

type SendRequestFile struct {
	Path string `json:"path"`
	Name string `json:"name"`
	Size int64  `json:"size"`
}

type RequestDraftFile struct {
	Path string `json:"path"`
	Name string `json:"name"`
	Size int64  `json:"size"`
}

type SaveBodyToFileRequest struct {
	Path         string `json:"path"`
	Body         string `json:"body"`
	BodyEncoding string `json:"bodyEncoding"`
	ContentType  string `json:"contentType"`
}

type HARExportRequest struct {
	Path       string   `json:"path"`
	TrafficIDs []uint64 `json:"trafficIds,omitempty"`
}

type SendRequestFormDataItem struct {
	Enabled  bool             `json:"enabled"`
	Name     string           `json:"name"`
	ItemType string           `json:"itemType"` // "text" or "file"
	Value    string           `json:"value"`
	File     *SendRequestFile `json:"file,omitempty"`
	Filename string           `json:"filename,omitempty"`
}

type SendRequestURLEncodedItem struct {
	Enabled bool   `json:"enabled"`
	Name    string `json:"name"`
	Value   string `json:"value"`
}

type SendRequestBody struct {
	BodyType   SendRequestBodyType          `json:"bodyType"`
	Text       string                       `json:"text"`
	File       *SendRequestFile             `json:"file,omitempty"`
	FormData   []*SendRequestFormDataItem   `json:"formData,omitempty"`
	URLEncoded []*SendRequestURLEncodedItem `json:"urlEncoded,omitempty"`
}

type InlinePythonScript struct {
	Enabled bool   `json:"enabled"`
	Source  string `json:"source"`
}

type RequestBodyRecoveryFormDataItem struct {
	Enabled  bool              `json:"enabled"`
	Name     string            `json:"name"`
	ItemType string            `json:"itemType"` // "text" or "file"
	Value    string            `json:"value"`
	File     *RequestDraftFile `json:"file,omitempty"`
}

type RequestBodyRecoveryResult struct {
	BodyType   SendRequestBodyType                `json:"bodyType"`
	Text       string                             `json:"text"`
	File       *RequestDraftFile                  `json:"file,omitempty"`
	FormData   []*RequestBodyRecoveryFormDataItem `json:"formData,omitempty"`
	URLEncoded []*SendRequestURLEncodedItem       `json:"urlEncoded,omitempty"`
	Warnings   []string                           `json:"warnings,omitempty"`
}

type SendRequestConfig struct {
	ProxyMode          SendRequestProxyMode `json:"proxyMode"`   // none / system / mitm / custom
	Protocol           SendRequestProtocol  `json:"protocol"`    // auto / http1 / http2
	CustomProxy        string               `json:"customProxy"` // used when ProxyMode=custom
	TimeoutMs          int64                `json:"timeoutMs"`   // <=0 means no timeout
	TLSClientHelloID   TLSClientHelloID     `json:"tlsClientHelloId"`
	HTTP2Fingerprint   string               `json:"http2Fingerprint"`
	DisablePlugins     bool                 `json:"disablePlugins"`
	PluginExecutionID  string               `json:"pluginExecutionId"`
	InlinePythonScript *InlinePythonScript  `json:"inlinePythonScript,omitempty"`
}

type SendRequestResponse struct {
	Outcome                 string                      `json:"outcome"`
	PluginExecution         *HTTPRequestPluginExecution `json:"pluginExecution,omitempty"`
	StatusCode              int                         `json:"statusCode"`
	StatusText              string                      `json:"statusText"`
	Protocol                string                      `json:"protocol"`
	HeaderFields            []HTTPHeaderField           `json:"headerFields"`
	HeadersTruncated        bool                        `json:"headersTruncated,omitempty"`
	HeaderOrderUnavailable  bool                        `json:"headerOrderUnavailable,omitempty"`
	TrailerFields           []HTTPHeaderField           `json:"trailerFields"`
	TrailersTruncated       bool                        `json:"trailersTruncated,omitempty"`
	TrailerOrderUnavailable bool                        `json:"trailerOrderUnavailable,omitempty"`
	Body                    string                      `json:"body"`
	BodyEncoding            string                      `json:"bodyEncoding"` // "" or "base64"
	Streaming               bool                        `json:"streaming,omitempty"`
	StreamSessionID         string                      `json:"streamSessionId,omitempty"`
}

type HTTPRequestStreamEvent struct {
	SessionID               string            `json:"sessionId"`
	EventType               string            `json:"eventType"` // "chunk", "complete", "closed", or "error"
	Offset                  *int64            `json:"offset,omitempty"`
	ChunkBase64             string            `json:"chunkBase64,omitempty"`
	TrailerFields           []HTTPHeaderField `json:"trailerFields"`
	TrailersTruncated       bool              `json:"trailersTruncated,omitempty"`
	TrailerOrderUnavailable bool              `json:"trailerOrderUnavailable,omitempty"`
	Error                   string            `json:"error,omitempty"`
}

type WebSocketConnectRequest struct {
	URL              string               `json:"url"`
	HeaderFields     []HTTPHeaderField    `json:"headerFields"`
	ProxyMode        SendRequestProxyMode `json:"proxyMode"`
	CustomProxy      string               `json:"customProxy"`
	TimeoutMs        int64                `json:"timeoutMs"`
	TLSClientHelloID TLSClientHelloID     `json:"tlsClientHelloId"`
}

type WebSocketConnectResponse struct {
	SessionID              string            `json:"sessionId"`
	Status                 string            `json:"status"`
	StatusCode             int               `json:"statusCode"`
	StatusText             string            `json:"statusText"`
	Protocol               string            `json:"protocol"`
	HeaderFields           []HTTPHeaderField `json:"headerFields"`
	HeadersTruncated       bool              `json:"headersTruncated,omitempty"`
	HeaderOrderUnavailable bool              `json:"headerOrderUnavailable,omitempty"`
}

type WebSocketSendRequest struct {
	SessionID string           `json:"sessionId"`
	MsgType   string           `json:"msgType"` // "text" or "binary"
	Text      string           `json:"text"`
	File      *SendRequestFile `json:"file,omitempty"`
}

type WebSocketSessionEvent struct {
	SessionID string            `json:"sessionId"`
	EventType string            `json:"eventType"`
	Status    string            `json:"status"`
	Message   *WebSocketMessage `json:"message,omitempty"`
	Error     string            `json:"error,omitempty"`
}

type ResendConfig struct {
	DelayMs       int64  `json:"delayMs"`       // delay before first request (ms)
	IntervalMs    int64  `json:"intervalMs"`    // interval between requests (ms)
	Count         int    `json:"count"`         // number of times to send
	UseProxy      bool   `json:"useProxy"`      // route through the running MITM proxy
	UpstreamProxy string `json:"upstreamProxy"` // upstream proxy URL (used when UseProxy=false)
}

type ResendResult struct {
	Success int `json:"success"`
	Failed  int `json:"failed"`
}

type HistoryMetadata struct {
	Key           string `json:"key"`
	Alias         string `json:"alias"`
	CreatedAt     int64  `json:"createdAt"` // Unix milliseconds timestamp
	Total         int    `json:"total"`
	FormatVersion uint16 `json:"formatVersion"`
}

type RequestEditorFileDropEvent struct {
	Paths              []string `json:"paths,omitempty"`
	DataFileDropTarget string   `json:"dataFileDropTarget,omitempty"`
}

func getHistoryStoragePath() (string, error) {
	userConfigDir, err := fs.GetBaseStorageDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(userConfigDir, "histories"), nil
}

func generateHistoryKey() string {
	uuidStr := uuid.New().String()
	return uuidStr
}

func copyPkixName(src *pkix.Name) *PkixName {
	if src == nil {
		return nil
	}
	return &PkixName{
		Country:            src.Country,
		Organization:       src.Organization,
		OrganizationalUnit: src.OrganizationalUnit,
		Locality:           src.Locality,
		Province:           src.Province,
		StreetAddress:      src.StreetAddress,
		PostalCode:         src.PostalCode,
		SerialNumber:       src.SerialNumber,
		CommonName:         src.CommonName,
	}
}
