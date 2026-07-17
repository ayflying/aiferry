package admin

import (
	"encoding/json"
	"time"
)

type ChannelInput struct {
	Name            string          `json:"name" v:"required|length:1,96"`
	Type            string          `json:"type" v:"required|length:1,64"`
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
	GroupIDs        []uint64        `json:"groupIds"`
}

type ChannelGroupInput struct {
	Name        string   `json:"name" v:"required|length:1,96"`
	Code        string   `json:"code" v:"required|length:2,64"`
	Description string   `json:"description" v:"length:0,255"`
	Status      int      `json:"status" v:"in:0,1"`
	ChannelIDs  []uint64 `json:"channelIds"`
}

type ChannelTypeInput struct {
	Name   string          `json:"name" v:"required|length:1,96"`
	Code   string          `json:"code" v:"required|length:2,64"`
	Status int             `json:"status" v:"in:0,1"`
	Config json.RawMessage `json:"config" v:"required"`
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
	BillingMode      string   `json:"billingMode"`
	InputPrice       *float64 `json:"inputPrice"`
	CachedInputPrice *float64 `json:"cachedInputPrice"`
	CacheWritePrice  *float64 `json:"cacheWritePrice"`
	OutputPrice      *float64 `json:"outputPrice"`
	ImageInputPrice  *float64 `json:"imageInputPrice"`
	AudioInputPrice  *float64 `json:"audioInputPrice"`
	AudioOutputPrice *float64 `json:"audioOutputPrice"`
	RequestPrice     *float64 `json:"requestPrice"`
}

type ModelPriceInput struct {
	BillingMode      string   `json:"billingMode"`
	InputPrice       *float64 `json:"inputPrice"`
	CachedInputPrice *float64 `json:"cachedInputPrice"`
	CacheWritePrice  *float64 `json:"cacheWritePrice"`
	OutputPrice      *float64 `json:"outputPrice"`
	ImageInputPrice  *float64 `json:"imageInputPrice"`
	AudioInputPrice  *float64 `json:"audioInputPrice"`
	AudioOutputPrice *float64 `json:"audioOutputPrice"`
	RequestPrice     *float64 `json:"requestPrice"`
}

type PriceRuleInput struct {
	Name       string          `json:"name" v:"required|length:1,96"`
	Source     string          `json:"source" v:"required|in:manual,sync"`
	SourceRef  string          `json:"sourceRef" v:"length:0,512"`
	Priority   int             `json:"priority"`
	Currency   string          `json:"currency" v:"required|length:3,12"`
	Conditions json.RawMessage `json:"conditions"`
	Rates      json.RawMessage `json:"rates" v:"required"`
	Status     int             `json:"status" v:"in:0,1"`
}

type ModelSelectionInput struct {
	ModelNames []string `json:"modelNames"`
}

type ModelTestInput struct {
	ModelID  uint64 `json:"modelId" v:"required|min:1"`
	Endpoint string `json:"endpoint" v:"required|in:auto,chat,responses,embeddings,images"`
	Stream   bool   `json:"stream"`
}

type APIKeyInput struct {
	Name            string     `json:"name" v:"required|length:1,96"`
	ExpiresAt       *time.Time `json:"expiresAt"`
	SpendLimit      *float64   `json:"spendLimit" v:"min:0"`
	AllowedModels   []string   `json:"allowedModels"`
	ChannelGroupIDs []uint64   `json:"channelGroupIds"`
}

type APIKeyUpdate struct {
	Name            string     `json:"name" v:"required|length:1,96"`
	Status          int        `json:"status" v:"in:0,1"`
	ExpiresAt       *time.Time `json:"expiresAt"`
	SpendLimit      *float64   `json:"spendLimit" v:"min:0"`
	AllowedModels   []string   `json:"allowedModels"`
	ChannelGroupIDs []uint64   `json:"channelGroupIds"`
}

type CostQueryInput struct {
	StartDate string `json:"startDate"`
	EndDate   string `json:"endDate"`
}

type PriceSyncInput struct {
	ChannelID     uint64 `json:"channelId"`
	PriceSourceID uint64 `json:"priceSourceId"`
}

type PriceSourceInput struct {
	Name   string          `json:"name" v:"required|length:1,96"`
	Code   string          `json:"code" v:"required|length:2,64"`
	Status int             `json:"status" v:"in:0,1"`
	Config json.RawMessage `json:"config" v:"required"`
}

type SystemResilienceSettingsInput struct {
	MaxFailoverAttempts        int      `json:"maxFailoverAttempts"`
	RetryStatusCodes           string   `json:"retryStatusCodes"`
	HealthCheckEnabled         bool     `json:"healthCheckEnabled"`
	HealthCheckMode            string   `json:"healthCheckMode" v:"in:passive,all"`
	HealthCheckIntervalMinutes int      `json:"healthCheckIntervalMinutes"`
	RecoveryEnabled            bool     `json:"recoveryEnabled"`
	AutoDisableEnabled         bool     `json:"autoDisableEnabled"`
	DisableLatencySeconds      int      `json:"disableLatencySeconds"`
	DisableStatusCodes         string   `json:"disableStatusCodes"`
	FailureKeywords            []string `json:"failureKeywords"`
}
