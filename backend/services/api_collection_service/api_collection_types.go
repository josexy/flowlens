package apicollectionservice

import proxyservice "github.com/josexy/flowlens/backend/services/proxy_service"

type APICollectionNodeType string

const (
	APICollectionNodeTypeFolder    APICollectionNodeType = "folder"
	APICollectionNodeTypeHTTP      APICollectionNodeType = "http"
	APICollectionNodeTypeWebSocket APICollectionNodeType = "websocket"
)

const apiCollectionSchemaVersion = 2

type APICollectionTree struct {
	SchemaVersion int                    `json:"schemaVersion"`
	Folders       []*APICollectionFolder `json:"folders"`
	Nodes         []*APICollectionNode   `json:"-"`
}

type APICollectionFolder struct {
	ID        string                  `json:"id"`
	Name      string                  `json:"name"`
	SortOrder int                     `json:"sortOrder"`
	CreatedAt int64                   `json:"createdAt"`
	UpdatedAt int64                   `json:"updatedAt"`
	Folders   []*APICollectionFolder  `json:"folders"`
	Requests  []*APICollectionRequest `json:"requests"`
}

type APICollectionRequest struct {
	ID        string                 `json:"id"`
	ParentID  string                 `json:"-"`
	Type      APICollectionNodeType  `json:"type"`
	Name      string                 `json:"name"`
	SortOrder int                    `json:"sortOrder"`
	CreatedAt int64                  `json:"createdAt"`
	UpdatedAt int64                  `json:"updatedAt"`
	HTTP      *SavedHTTPRequest      `json:"http,omitempty"`
	WebSocket *SavedWebSocketRequest `json:"websocket,omitempty"`
}

type APICollectionEntry struct {
	Folder  *APICollectionFolder  `json:"folder,omitempty"`
	Request *APICollectionRequest `json:"request,omitempty"`
}

type APICollectionNode = APICollectionRequest

type apiCollectionStore = APICollectionTree

type SavedHTTPRequest struct {
	Method             string                            `json:"method"`
	URL                string                            `json:"url"`
	Params             []*SavedKeyValue                  `json:"params,omitempty"`
	Headers            []*SavedKeyValue                  `json:"headers,omitempty"`
	BodyType           string                            `json:"bodyType"`
	BodyText           string                            `json:"bodyText"`
	BodyFile           *SavedFile                        `json:"bodyFile,omitempty"`
	BodyFormData       []*SavedFormDataItem              `json:"bodyFormData,omitempty"`
	BodyURLEncoded     []*SavedKeyValue                  `json:"bodyUrlEncoded,omitempty"`
	InlineScriptSource *string                           `json:"inlineScriptSource,omitempty"`
	ProxyMode          proxyservice.SendRequestProxyMode `json:"proxyMode"`
	Protocol           proxyservice.SendRequestProtocol  `json:"protocol,omitempty"`
	CustomProxy        string                            `json:"customProxy,omitempty"`
	TimeoutMs          int64                             `json:"timeoutMs"`
	TLSClientHelloID   proxyservice.TLSClientHelloID     `json:"tlsClientHelloId,omitempty"`
	HTTP2Fingerprint   string                            `json:"http2Fingerprint,omitempty"`
}

type SavedWebSocketRequest struct {
	URL              string                            `json:"url"`
	Params           []*SavedKeyValue                  `json:"params,omitempty"`
	Headers          []*SavedKeyValue                  `json:"headers,omitempty"`
	DraftType        string                            `json:"draftType"`
	DraftText        string                            `json:"draftText,omitempty"`
	DraftFile        *SavedFile                        `json:"draftFile,omitempty"`
	ProxyMode        proxyservice.SendRequestProxyMode `json:"proxyMode"`
	CustomProxy      string                            `json:"customProxy,omitempty"`
	TimeoutMs        int64                             `json:"timeoutMs"`
	TLSClientHelloID proxyservice.TLSClientHelloID     `json:"tlsClientHelloId,omitempty"`
}

type SavedKeyValue struct {
	Key     string `json:"key"`
	Value   string `json:"value"`
	Enabled bool   `json:"enabled"`
}

type SavedFile struct {
	Path string `json:"path"`
	Name string `json:"name"`
	Size int64  `json:"size"`
}

type SavedFormDataItem struct {
	ID       string     `json:"id"`
	Enabled  bool       `json:"enabled"`
	Name     string     `json:"name"`
	ItemType string     `json:"itemType"`
	Value    string     `json:"value"`
	File     *SavedFile `json:"file,omitempty"`
}
