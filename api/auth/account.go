package auth

type ProfileUpdateInput struct {
	Nickname string `json:"nickname" v:"required|length:1,64"`
	Email    string `json:"email"`
}
