// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package entity

import (
	"time"
)

// Users is the golang structure for table users.
type Users struct {
	Id               uint64    `json:"id"               orm:"id"                description:""` //
	Name             string    `json:"name"             orm:"name"              description:""` //
	Role             string    `json:"role"             orm:"role"              description:""` //
	Status           int       `json:"status"           orm:"status"            description:""` //
	IdentityProvider string    `json:"identityProvider" orm:"identity_provider" description:""` //
	IdentitySubject  string    `json:"identitySubject"  orm:"identity_subject"  description:""` //
	AvatarUrl        string    `json:"avatarUrl"        orm:"avatar_url"        description:""` //
	GroupsJson       string    `json:"groupsJson"       orm:"groups_json"       description:""` //
	LastLoginAt      time.Time `json:"lastLoginAt"      orm:"last_login_at"     description:""` //
	CreatedAt        time.Time `json:"createdAt"        orm:"created_at"        description:""` //
	UpdatedAt        time.Time `json:"updatedAt"        orm:"updated_at"        description:""` //
	DeletedAt        time.Time `json:"deletedAt"        orm:"deleted_at"        description:""` //
}
