// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package entity

import (
	"time"
)

// Channels is the golang structure for table channels.
type Channels struct {
	Id                  uint64    `json:"id"                  orm:"id"                    ` //
	Name                string    `json:"name"                orm:"name"                  ` //
	Type                string    `json:"type"                orm:"type"                  ` //
	BaseUrl             string    `json:"baseUrl"             orm:"base_url"              ` //
	ApiKeyCipher        string    `json:"apiKeyCipher"        orm:"api_key_cipher"        ` //
	ManagementKeyCipher string    `json:"managementKeyCipher" orm:"management_key_cipher" ` //
	OrganizationId      string    `json:"organizationId"      orm:"organization_id"       ` //
	ProjectId           string    `json:"projectId"           orm:"project_id"            ` //
	Status              int       `json:"status"              orm:"status"                ` //
	Priority            int       `json:"priority"            orm:"priority"              ` //
	Weight              uint      `json:"weight"              orm:"weight"                ` //
	CostQueryMode       string    `json:"costQueryMode"       orm:"cost_query_mode"       ` //
	CostQueryConfig     string    `json:"costQueryConfig"     orm:"cost_query_config"     ` //
	LastTestStatus      string    `json:"lastTestStatus"      orm:"last_test_status"      ` //
	LastTestLatencyMs   uint      `json:"lastTestLatencyMs"   orm:"last_test_latency_ms"  ` //
	LastTestError       string    `json:"lastTestError"       orm:"last_test_error"       ` //
	LastTestAt          time.Time `json:"lastTestAt"          orm:"last_test_at"          ` //
	LastCostUsed        float64   `json:"lastCostUsed"        orm:"last_cost_used"        ` //
	LastCostRemaining   float64   `json:"lastCostRemaining"   orm:"last_cost_remaining"   ` //
	LastCostCurrency    string    `json:"lastCostCurrency"    orm:"last_cost_currency"    ` //
	LastCostAt          time.Time `json:"lastCostAt"          orm:"last_cost_at"          ` //
	CreatedAt           time.Time `json:"createdAt"           orm:"created_at"            ` //
	UpdatedAt           time.Time `json:"updatedAt"           orm:"updated_at"            ` //
	DeletedAt           time.Time `json:"deletedAt"           orm:"deleted_at"            ` //
}
