package admin

import "time"

type ChannelInput struct {
	Name            string          `json:"name" v:"required|length:1,96"`
	BaseURL         string          `json:"baseUrl" v:"required|url"`
	APIKey          *string         `json:"apiKey"`
	ManagementKey   *string         `json:"managementKey"`
	OrganizationID  string          `json:"organizationId"`
	ProjectID       string          `json:"projectId"`
	Status          int             `json:"status"`
	Priority        int             `json:"priority"`
	Weight          uint            `json:"weight"`
	CostQueryMode   string          `json:"costQueryMode"`
	CostQueryConfig CostQueryConfig `json:"costQueryConfig"`
}

type CostQueryConfig struct {
	URL           string `json:"url"`
	AuthType      string `json:"authType"`
	HeaderName    string `json:"headerName"`
	UsedPath      string `json:"usedPath"`
	RemainingPath string `json:"remainingPath"`
	CurrencyPath  string `json:"currencyPath"`
	FixedCurrency string `json:"fixedCurrency"`
}

type ModelInput struct {
	PublicName       string   `json:"publicName" v:"required|length:1,191"`
	UpstreamName     string   `json:"upstreamName" v:"required|length:1,191"`
	Enabled          bool     `json:"enabled"`
	InputPrice       *float64 `json:"inputPrice"`
	CachedInputPrice *float64 `json:"cachedInputPrice"`
	OutputPrice      *float64 `json:"outputPrice"`
}

type ModelSelectionInput struct {
	ModelNames []string `json:"modelNames"`
}

type ModelTestInput struct {
	ModelID  uint64 `json:"modelId" v:"required|min:1"`
	Endpoint string `json:"endpoint" v:"required|in:chat,responses,embeddings"`
}

type APIKeyInput struct {
	Name      string     `json:"name" v:"required|length:1,96"`
	ExpiresAt *time.Time `json:"expiresAt"`
}

type APIKeyUpdate struct {
	Name      string     `json:"name" v:"required|length:1,96"`
	Status    int        `json:"status" v:"in:0,1"`
	ExpiresAt *time.Time `json:"expiresAt"`
}

type CostQueryInput struct {
	StartDate string `json:"startDate"`
	EndDate   string `json:"endDate"`
}
