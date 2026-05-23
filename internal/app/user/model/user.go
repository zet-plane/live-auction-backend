package model

import (
	"time"

	"gorm.io/gorm"
)

type UserIdentity string

const (
	IdentityUser     UserIdentity = "user"
	IdentityMerchant UserIdentity = "merchant"
)

type User struct {
	ID        string       `gorm:"primaryKey;size:64" json:"id"`
	Account   string       `gorm:"uniqueIndex;size:64;not null" json:"account"`
	Name      string       `gorm:"size:64" json:"name"`
	AvatarURL string       `gorm:"size:512" json:"avatar_url"`
	Password  string       `gorm:"size:255" json:"-"`
	Motto     string       `gorm:"size:255" json:"motto"`
	Identity  UserIdentity `gorm:"size:32;not null" json:"identity"`

	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
}
