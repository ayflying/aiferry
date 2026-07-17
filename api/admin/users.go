package admin

type UserBalanceInput struct {
	Balance float64 `json:"balance" v:"min:0"`
}
