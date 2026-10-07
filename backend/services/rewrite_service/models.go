// Package rewriteservice owns persisted, revisioned HTTP rewrite rules. It has
// no network dependencies; proxy_service applies matched actions to traffic.
package rewriteservice

const (
	ConfigVersion  = 1
	MaxBodyBytes   = 8 * 1024 * 1024
	ChangedEvent   = "rewrite:changed"
	ActionRedirect = "redirect"
	ActionRequest  = "request"
	ActionResponse = "response"
)

type FieldOperation struct {
	Operation string `json:"operation"`
	Name      string `json:"name"`
	Value     string `json:"value"`
}
type BodyAction struct {
	Mode        string `json:"mode"`
	Text        string `json:"text"`
	Pattern     string `json:"pattern"`
	Replacement string `json:"replacement"`
}
type Action struct {
	Version    int              `json:"version"`
	Type       string           `json:"type"`
	TargetURL  string           `json:"targetURL"`
	HostPolicy string           `json:"hostPolicy"`
	Headers    []FieldOperation `json:"headers"`
	Query      []FieldOperation `json:"query"`
	Body       BodyAction       `json:"body"`
}
type Rule struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	Enabled           bool   `json:"enabled"`
	Method            string `json:"method"`
	URLPattern        string `json:"urlPattern"`
	Action            Action `json:"action"`
	CreatedAt         int64  `json:"createdAt"`
	UpdatedAt         int64  `json:"updatedAt"`
	UnavailableReason string `json:"unavailableReason"`
}
type State struct {
	Revision int64  `json:"revision"`
	Enabled  bool   `json:"enabled"`
	Rules    []Rule `json:"rules"`
}
type Changed struct {
	Revision int64 `json:"revision"`
}
type PreviewResult struct {
	Matched   bool     `json:"matched"`
	Captures  []string `json:"captures"`
	TargetURL string   `json:"targetURL"`
}
