package auth

type ConfigView struct {
	Enabled   bool   `json:"enabled"`
	Provider  string `json:"provider"`
	LoginPath string `json:"loginPath"`
}

type UserView struct {
	Id        uint64   `json:"id"`
	Name      string   `json:"name"`
	Role      string   `json:"role"`
	IsAdmin   bool     `json:"isAdmin"`
	AvatarURL string   `json:"avatarUrl"`
}
