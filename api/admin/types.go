package admin

import (
	"encoding/json"
	"time"
)

type ChannelInput struct {
	Name               string          `json:"name" v:"required|length:1,96"`
	Type               string          `json:"type" v:"required|length:1,64"`
	BaseURL            string          `json:"baseUrl" v:"required|url"`
	APIKey             *string         `json:"apiKey"`
	ManagementKey      *string         `json:"managementKey"`
	ProxyURL           *string         `json:"proxyUrl"`
	OrganizationID     string          `json:"organizationId"`
	ProjectID          string          `json:"projectId"`
	Status             int             `json:"status"`
	Priority           int             `json:"priority"`
	Weight             uint            `json:"weight"`
	HealthCheckModelID uint64          `json:"healthCheckModelId"`
	AutoDisableEnabled *bool           `json:"autoDisableEnabled"`
	CostQueryMode      string          `json:"costQueryMode"`
	CostQueryConfig    CostQueryConfig `json:"costQueryConfig"`
	AdvancedConfig     json.RawMessage `json:"advancedConfig"`
	GroupIDs           []uint64        `json:"groupIds"`
}

type ChannelCredentialInput struct {
	APIKey        string  `json:"apiKey" v:"required|length:1,4096"`
	ManagementKey *string `json:"managementKey"`
}

type ChannelCredentialManagementKeyInput struct {
	ManagementKey *string `json:"managementKey"`
}

type ChannelCredentialStatusInput struct {
	Status int `json:"status" v:"in:0,1"`
}

type ChannelStatusInput struct {
	Status int `json:"status" v:"in:0,1"`
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
	Config json.RawMessage `json:"config"`
}

type ChannelTypeStatusInput struct {
	Status int `json:"status" v:"in:0,1"`
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
	ModelNames []string            `json:"modelNames"`
	Models     []ModelMappingInput `json:"models"`
}

type ModelMappingInput struct {
	UpstreamName string `json:"upstreamName"`
	PublicName   string `json:"publicName"`
	// ClosedWindows 是「定时关闭时段」列表，任一窗口命中即处于关闭时段。
	// 字段缺省（nil）表示保持已有时段不变，传空数组表示清除；
	// 只在所选模型的映射关系上生效。
	ClosedWindows []ModelClosedWindowInput `json:"closedWindows"`
}

// ModelClosedWindowInput 描述「时区 + 星期 + 多时段」的定时关闭时间窗：
// weekdays 用 ISO 编号（1=周一…7=周日），空表示每天；ranges 为 ["HH:MM","HH:MM"]
// 列表，起止相同视为全天、start > end 视为跨零点；空表示不关闭。
type ModelClosedWindowInput struct {
	TZ       string     `json:"tz"`
	Weekdays []int      `json:"weekdays"`
	Ranges   [][]string `json:"ranges"`
}

type ModelTestInput struct {
	ModelID             uint64 `json:"modelId" v:"required|min:1"`
	ChannelCredentialID uint64 `json:"channelCredentialId"`
	Endpoint            string `json:"endpoint" v:"required|in:auto,chat,responses,embeddings,images,tts,asr"`
	Stream              bool   `json:"stream"`
}

type APIKeyInput struct {
	Name            string     `json:"name" v:"required|length:1,96"`
	ExpiresAt       *time.Time `json:"expiresAt"`
	SpendLimit      *float64   `json:"spendLimit" v:"min:0"`
	DailySpendLimit *float64   `json:"dailySpendLimit" v:"min:0"`
	AllowedModels   []string   `json:"allowedModels"`
	ChannelGroupIDs []uint64   `json:"channelGroupIds"`
}

type APIKeyUpdate struct {
	Name            string     `json:"name" v:"required|length:1,96"`
	Status          int        `json:"status" v:"in:0,1"`
	ExpiresAt       *time.Time `json:"expiresAt"`
	SpendLimit      *float64   `json:"spendLimit" v:"min:0"`
	DailySpendLimit *float64   `json:"dailySpendLimit" v:"min:0"`
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
	RetryStatusCodes               string   `json:"retryStatusCodes"`
	StreamFirstByteTimeoutSeconds  int      `json:"streamFirstByteTimeoutSeconds"`
	StreamIdleTimeoutSeconds       int      `json:"streamIdleTimeoutSeconds"`
	NonStreamTimeoutSeconds        int      `json:"nonStreamTimeoutSeconds"`
	HealthCheckEnabled             bool     `json:"healthCheckEnabled"`
	HealthCheckMode                string   `json:"healthCheckMode" v:"in:passive,all"`
	HealthCheckIntervalMinutes     int      `json:"healthCheckIntervalMinutes"`
	RecoveryEnabled                bool     `json:"recoveryEnabled"`
	AutoDisableEnabled             bool     `json:"autoDisableEnabled"`
	AutoDisableNotificationEnabled bool     `json:"autoDisableNotificationEnabled"`
	AutoDisableFailureThreshold    int      `json:"autoDisableFailureThreshold"`
	DisableLatencySeconds          int      `json:"disableLatencySeconds"`
	DisableStatusCodes             string   `json:"disableStatusCodes"`
	FailureKeywords                []string `json:"failureKeywords"`
	ModelQualityDetectionEnabled   bool     `json:"modelQualityDetectionEnabled"`
	// ProtocolConversionEnabled 控制网关是否允许在 Chat Completions 与 Responses
	// 之间自动转换。关闭后，除渠道高级配置显式覆盖的渠道外，请求一律直连客户端
	// 声明的端点，用于排查协议转换引入的问题。字段缺省（历史数据）按启用处理。
	ProtocolConversionEnabled bool `json:"protocolConversionEnabled"`
	// StreamFailureEventEnabled 控制流式响应在已经向客户端写出内容之后失败时，
	// 是否补发一个显式的错误事件与结束帧。关闭后保持历史行为（直接断开流），
	// 适合依赖「断流即重试」的客户端（如 Codex）。字段缺省（历史数据）按启用处理。
	StreamFailureEventEnabled bool `json:"streamFailureEventEnabled"`
}

type ModelQualitySettingsInput struct {
	Enabled bool `json:"enabled"`
}

// CurrencyRateView 是当前生效的折算汇率与其来源，供管理端核对自动汇率是否取到。
type CurrencyRateView struct {
	// Display 是展示货币；Base 是汇率表基准货币。
	Display string `json:"display"`
	Base    string `json:"base"`
	// Rates 的语义是「1 个 Base 能换多少该币种」。
	Rates map[string]float64 `json:"rates"`
	// Source 取值 auto/manual/fallback：fallback 表示自动汇率取不到、正在用人工兜底值。
	Source    string `json:"source"`
	UpdatedAt string `json:"updatedAt,omitempty"`
}

type ModelQualityEventsInput struct {
	Page     int `json:"page"`
	PageSize int `json:"pageSize"`
}

type ModelQualityEventView struct {
	Id              uint64    `json:"id"`
	ChannelId       uint64    `json:"channelId"`
	ChannelName     string    `json:"channelName"`
	CredentialId    uint64    `json:"credentialId"`
	CredentialIndex uint      `json:"credentialIndex"`
	APIKeyName      string    `json:"apiKeyName"`
	RequestedModel  string    `json:"requestedModel"`
	ExpectedModel   string    `json:"expectedModel"`
	ObservedModel   string    `json:"observedModel"`
	Reasons         []string  `json:"reasons"`
	QuestionChars   uint      `json:"questionChars"`
	AnswerChars     uint      `json:"answerChars"`
	CreatedAt       time.Time `json:"createdAt"`
}

type ModelQualityEventList struct {
	Items []ModelQualityEventView `json:"items"`
	Total int                     `json:"total"`
}

type BaseSettingsInput struct {
	TimeZone string `json:"timeZone" v:"required|length:1,64"`
	// DisplayCurrency 是金额展示货币（USD/CNY）；非法值回落 USD。
	DisplayCurrency string `json:"displayCurrency"`
	// ExchangeRateMode 是汇率来源（auto/manual）；非法值回落 auto。
	ExchangeRateMode string `json:"exchangeRateMode"`
	// ManualUsdToCnyRate 是人工 USD→CNY 汇率，同时作为自动汇率不可用时的兜底值。
	ManualUsdToCnyRate float64 `json:"manualUsdToCnyRate"`
}

type SensitiveWordSettingsInput struct {
	ImageEnabled                  bool     `json:"imageEnabled"`
	Enabled                       bool     `json:"enabled"`
	CheckUserPrompt               bool     `json:"checkUserPrompt"`
	Keywords                      []string `json:"keywords"`
	SensitiveDataRedactionEnabled bool     `json:"sensitiveDataRedactionEnabled"`
	PasswordRedactionEnabled      bool     `json:"passwordRedactionEnabled"`
	TokenRedactionEnabled         bool     `json:"tokenRedactionEnabled"`
	PersonalDataRedactionEnabled  bool     `json:"personalDataRedactionEnabled"`
}

type RequestFirewallSettingsInput struct {
	Enabled                     bool `json:"enabled"`
	MaxConcurrentRequests       int  `json:"maxConcurrentRequests"`
	MaxConcurrentRequestsPerIP  int  `json:"maxConcurrentRequestsPerIp"`
	MaxConcurrentRequestsPerKey int  `json:"maxConcurrentRequestsPerKey"`
	RequestsPerMinutePerIP      int  `json:"requestsPerMinutePerIp"`
	RequestsPerMinutePerAPIKey  int  `json:"requestsPerMinutePerApiKey"`
}

type SystemInformationInput struct {
	SystemName            string `json:"systemName"`
	ServerURL             string `json:"serverUrl"`
	LogoURL               string `json:"logoUrl"`
	Footer                string `json:"footer"`
	About                 string `json:"about"`
	HomeContent           string `json:"homeContent"`
	UserAgreement         string `json:"userAgreement"`
	PrivacyPolicy         string `json:"privacyPolicy"`
	PublicHomepageEnabled bool   `json:"publicHomepageEnabled"`
}

type UserChannelGroupInput struct {
	ChannelGroupIDs []uint64 `json:"channelGroupIds"`
}

type MailSettingsInput struct {
	Enabled                  bool    `json:"enabled"`
	ChannelAlertEnabled      *bool   `json:"channelAlertEnabled"`
	ChannelBalanceThresholds *string `json:"channelBalanceThresholds"`
	Host                     string  `json:"host"`
	Port                     int     `json:"port"`
	Username                 string  `json:"username"`
	Password                 *string `json:"password"`
	From                     string  `json:"from"`
	Security                 string  `json:"security"`
	Threshold                float64 `json:"threshold"`
	SubjectTemplate          string  `json:"subjectTemplate"`
	BodyTemplate             string  `json:"bodyTemplate"`
}

type MailTestInput struct {
	Recipient string `json:"recipient" v:"required|email"`
}
