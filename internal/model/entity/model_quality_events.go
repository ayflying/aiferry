// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package entity

import (
	"time"
)

// ModelQualityEvents is the golang structure for table model_quality_events.
type ModelQualityEvents struct {
	Id             uint64    `json:"id"             orm:"id"              description:""` //
	RequestId      string    `json:"requestId"      orm:"request_id"      description:""` //
	ChannelId      uint64    `json:"channelId"      orm:"channel_id"      description:""` //
	CredentialId   uint64    `json:"credentialId"   orm:"credential_id"   description:""` //
	RequestedModel string    `json:"requestedModel" orm:"requested_model" description:""` //
	ExpectedModel  string    `json:"expectedModel"  orm:"expected_model"  description:""` //
	ObservedModel  string    `json:"observedModel"  orm:"observed_model"  description:""` //
	ReasonsJson    string    `json:"reasonsJson"    orm:"reasons_json"    description:""` //
	QuestionChars  uint      `json:"questionChars"  orm:"question_chars"  description:""` //
	AnswerChars    uint      `json:"answerChars"    orm:"answer_chars"    description:""` //
	CreatedAt      time.Time `json:"createdAt"      orm:"created_at"      description:""` //
}
