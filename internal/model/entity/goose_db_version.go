// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package entity

import (
	"time"
)

// GooseDbVersion is the golang structure for table goose_db_version.
type GooseDbVersion struct {
	Id        uint64    `json:"id"        orm:"id"         description:""` //
	VersionId int64     `json:"versionId" orm:"version_id" description:""` //
	IsApplied int       `json:"isApplied" orm:"is_applied" description:""` //
	Tstamp    time.Time `json:"tstamp"    orm:"tstamp"     description:""` //
}
