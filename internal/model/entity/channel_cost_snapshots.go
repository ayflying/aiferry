// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package entity

import (
	"time"
)

// ChannelCostSnapshots is the golang structure for table channel_cost_snapshots.
type ChannelCostSnapshots struct {
	Id              uint64    `json:"id"              orm:"id"               ` //
	ChannelId       uint64    `json:"channelId"       orm:"channel_id"       ` //
	Mode            string    `json:"mode"            orm:"mode"             ` //
	UsedAmount      float64   `json:"usedAmount"      orm:"used_amount"      ` //
	RemainingAmount float64   `json:"remainingAmount" orm:"remaining_amount" ` //
	Currency        string    `json:"currency"        orm:"currency"         ` //
	PeriodStart     time.Time `json:"periodStart"     orm:"period_start"     ` //
	PeriodEnd       time.Time `json:"periodEnd"       orm:"period_end"       ` //
	QueriedAt       time.Time `json:"queriedAt"       orm:"queried_at"       ` //
	CreatedAt       time.Time `json:"createdAt"       orm:"created_at"       ` //
}
