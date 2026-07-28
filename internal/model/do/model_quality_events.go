// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package do

import (
	"github.com/gogf/gf/v2/frame/g"
)

// ModelQualityEvents is the golang structure of table model_quality_events for DAO operations like Where/Data.
type ModelQualityEvents struct {
	g.Meta         `orm:"table:model_quality_events, do:true"`
	Id             any //
	RequestId      any //
	ChannelId      any //
	CredentialId   any //
	RequestedModel any //
	ExpectedModel  any //
	ObservedModel  any //
	ReasonsJson    any //
	QuestionChars  any //
	AnswerChars    any //
	CreatedAt      any //
}
