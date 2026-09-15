package auth

type ConfigView struct {
	Enabled   bool                  `json:"enabled"`
	Provider  string                `json:"provider"`
	LoginPath string                `json:"loginPath"`
	TimeZone  string                `json:"timeZone"`
	Currency  CurrencyView          `json:"currency"`
	System    SystemInformationView `json:"system"`
}

// CurrencyView 把「展示货币 + 折算汇率」下发给前端：
// 前端的金额渲染统一走同一份汇率，避免每个页面各自换算导致口径不一致。
type CurrencyView struct {
	// Display 是展示货币；Base 是汇率表的基准货币。
	Display string `json:"display"`
	Base    string `json:"base"`
	// Rates 的语义是「1 个 Base 能换多少该币种」。
	Rates map[string]float64 `json:"rates"`
	// Source 标明汇率来源（auto/manual/fallback），UpdatedAt 是汇率发布时间。
	Source    string `json:"source"`
	UpdatedAt string `json:"updatedAt,omitempty"`
}

type SystemInformationView struct {
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

type UserView struct {
	Id        uint64   `json:"id"`
	Name      string   `json:"name"`
	Role      string   `json:"role"`
	IsAdmin   bool     `json:"isAdmin"`
	AvatarURL string   `json:"avatarUrl"`
}
