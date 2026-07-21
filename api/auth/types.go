package auth

type ConfigView struct {
	Enabled   bool                  `json:"enabled"`
	Provider  string                `json:"provider"`
	LoginPath string                `json:"loginPath"`
	TimeZone  string                `json:"timeZone"`
	System    SystemInformationView `json:"system"`
}

type SystemInformationView struct {
	SystemName    string `json:"systemName"`
	ServerURL     string `json:"serverUrl"`
	LogoURL       string `json:"logoUrl"`
	Footer        string `json:"footer"`
	About         string `json:"about"`
	HomeContent   string `json:"homeContent"`
	UserAgreement string `json:"userAgreement"`
	PrivacyPolicy string `json:"privacyPolicy"`
}

type UserView struct {
	Id        uint64   `json:"id"`
	Name      string   `json:"name"`
	Role      string   `json:"role"`
	IsAdmin   bool     `json:"isAdmin"`
	AvatarURL string   `json:"avatarUrl"`
}
