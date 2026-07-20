// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package do

import (
	"github.com/gogf/gf/v2/frame/g"
)

// UsageLogs is the golang structure of table usage_logs for DAO operations like Where/Data.
type UsageLogs struct {
	g.Meta              `orm:"table:usage_logs, do:true"`
	Id                  any //
	RequestId           any //
	UserId              any //
	ApiKeyId            any //
	ChannelId           any //
	ChannelCredentialId any //
	Endpoint            any //
	UpstreamEndpoint    any //
	ProtocolConversion  any //
	ClientIp            any //
	IpLocation          any //
	RequestedModel      any //
	UpstreamModel       any //
	HttpStatus          any //
	IsStream            any //
	InputTokens         any //
	CachedInputTokens   any //
	OutputTokens        any //
	TotalTokens         any //
	EstimatedCost       any //
	BillingDetailsJson  any //
	DurationMs          any //
	FirstTokenMs        any //
	Attempts            any //
	ErrorMessage        any //
	CreatedAt           any //
}
