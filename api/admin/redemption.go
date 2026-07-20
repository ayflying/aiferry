package admin

import "time"

type RedemptionCodeCreateInput struct {
	Name      string     `json:"name" v:"required|length:1,20"`
	Amount    float64    `json:"amount" v:"required|min:0.00000001"`
	ExpiresAt *time.Time `json:"expiresAt"`
	Quantity  int        `json:"quantity" v:"required|between:1,100"`
}

type RedemptionCodeRedeemInput struct {
	Code string `json:"code" v:"required|length:1,64"`
}
