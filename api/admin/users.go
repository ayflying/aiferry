package admin

type UserRoleInput struct {
	IsAdmin bool `json:"isAdmin"`
}

type UserBalanceInput struct {
	Balance float64 `json:"balance" v:"min:0"`
}
