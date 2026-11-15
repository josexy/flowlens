package pythonpluginservice

type ValidationStatus string

const (
	ValidationStatusUnavailable ValidationStatus = "unavailable"
	ValidationStatusValid       ValidationStatus = "valid"
	ValidationStatusInvalid     ValidationStatus = "invalid"
)

type Plugin struct {
	ID               string           `json:"id"`
	Name             string           `json:"name"`
	Description      string           `json:"description"`
	Enabled          bool             `json:"enabled"`
	SortOrder        int              `json:"sortOrder"`
	ParamsJSON       string           `json:"paramsJson"`
	ActiveRevision   string           `json:"activeRevision"`
	LastGoodRevision string           `json:"lastGoodRevision"`
	ValidationStatus ValidationStatus `json:"validationStatus"`
	ValidationError  string           `json:"validationError"`
	CreatedAt        int64            `json:"createdAt"`
	UpdatedAt        int64            `json:"updatedAt"`
	Rules            []*Rule          `json:"rules"`
}

type Rule struct {
	ID         string `json:"id"`
	PluginID   string `json:"pluginId"`
	Enabled    bool   `json:"enabled"`
	Method     string `json:"method"`
	URLPattern string `json:"urlPattern"`
	SortOrder  int    `json:"sortOrder"`
	CreatedAt  int64  `json:"createdAt"`
	UpdatedAt  int64  `json:"updatedAt"`
}

type CreatePluginInput struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	ParamsJSON  string `json:"paramsJson"`
}

type UpdatePluginInput struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	ParamsJSON  string `json:"paramsJson"`
}

type CreateRuleInput struct {
	ID         string `json:"id"`
	Enabled    bool   `json:"enabled"`
	Method     string `json:"method"`
	URLPattern string `json:"urlPattern"`
}

type UpdateRuleInput struct {
	Enabled    bool   `json:"enabled"`
	Method     string `json:"method"`
	URLPattern string `json:"urlPattern"`
}
