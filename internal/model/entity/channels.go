// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package entity

import (
	"time"
)

// Channels is the golang structure for table channels.
type Channels struct {
	Id                  uint64    `json:"id"                  orm:"id"                    description:""` //
	Name                string    `json:"name"                orm:"name"                  description:""` //
	Type                string    `json:"type"                orm:"type"                  description:""` //
	BaseUrl             string    `json:"baseUrl"             orm:"base_url"              description:""` //
	ApiKeyCipher        string    `json:"apiKeyCipher"        orm:"api_key_cipher"        description:""` //
	ManagementKeyCipher string    `json:"managementKeyCipher" orm:"management_key_cipher" description:""` //
	OrganizationId      string    `json:"organizationId"      orm:"organization_id"       description:""` //
	ProjectId           string    `json:"projectId"           orm:"project_id"            description:""` //
	Status              int       `json:"status"              orm:"status"                description:""` //
	Priority            int       `json:"priority"            orm:"priority"              description:""` //
	Weight              uint      `json:"weight"              orm:"weight"                description:""` //
	CostQueryMode       string    `json:"costQueryMode"       orm:"cost_query_mode"       description:""` //
	CostQueryConfig     string    `json:"costQueryConfig"     orm:"cost_query_config"     description:""` //
	LastTestStatus      string    `json:"lastTestStatus"      orm:"last_test_status"      description:""` //
	LastTestLatencyMs   uint      `json:"lastTestLatencyMs"   orm:"last_test_latency_ms"  description:""` //
	LastTestError       string    `json:"lastTestError"       orm:"last_test_error"       description:""` //
	LastTestAt          time.Time `json:"lastTestAt"          orm:"last_test_at"          description:""` //
	LastCostUsed        float64   `json:"lastCostUsed"        orm:"last_cost_used"        description:""` //
	LastCostRemaining   float64   `json:"lastCostRemaining"   orm:"last_cost_remaining"   description:""` //
	LastCostCurrency    string    `json:"lastCostCurrency"    orm:"last_cost_currency"    description:""` //
	LastCostAt          time.Time `json:"lastCostAt"          orm:"last_cost_at"          description:""` //
	CreatedAt           time.Time `json:"createdAt"           orm:"created_at"            description:""` //
	UpdatedAt           time.Time `json:"updatedAt"           orm:"updated_at"            description:""` //
	DeletedAt           time.Time `json:"deletedAt"           orm:"deleted_at"            description:""` //
}
