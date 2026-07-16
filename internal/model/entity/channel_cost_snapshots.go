// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package entity

import (
	"time"
)

// ChannelCostSnapshots is the golang structure for table channel_cost_snapshots.
type ChannelCostSnapshots struct {
	Id              uint64    `json:"id"              orm:"id"               description:""` //
	ChannelId       uint64    `json:"channelId"       orm:"channel_id"       description:""` //
	Mode            string    `json:"mode"            orm:"mode"             description:""` //
	UsedAmount      float64   `json:"usedAmount"      orm:"used_amount"      description:""` //
	RemainingAmount float64   `json:"remainingAmount" orm:"remaining_amount" description:""` //
	Currency        string    `json:"currency"        orm:"currency"         description:""` //
	PeriodStart     time.Time `json:"periodStart"     orm:"period_start"     description:""` //
	PeriodEnd       time.Time `json:"periodEnd"       orm:"period_end"       description:""` //
	QueriedAt       time.Time `json:"queriedAt"       orm:"queried_at"       description:""` //
	CreatedAt       time.Time `json:"createdAt"       orm:"created_at"       description:""` //
}
