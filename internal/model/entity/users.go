// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package entity

import (
	"time"
)

// Users is the golang structure for table users.
type Users struct {
	Id               uint64    `json:"id"               orm:"id"                ` //
	Name             string    `json:"name"             orm:"name"              ` //
	Role             string    `json:"role"             orm:"role"              ` //
	Status           int       `json:"status"           orm:"status"            ` //
	IdentityProvider string    `json:"identityProvider" orm:"identity_provider" ` //
	IdentitySubject  string    `json:"identitySubject"  orm:"identity_subject"  ` //
	AvatarUrl        string    `json:"avatarUrl"        orm:"avatar_url"        ` //
	GroupsJson       string    `json:"groupsJson"       orm:"groups_json"       ` //
	LastLoginAt      time.Time `json:"lastLoginAt"      orm:"last_login_at"     ` //
	CreatedAt        time.Time `json:"createdAt"        orm:"created_at"        ` //
	UpdatedAt        time.Time `json:"updatedAt"        orm:"updated_at"        ` //
	DeletedAt        time.Time `json:"deletedAt"        orm:"deleted_at"        ` //
}
