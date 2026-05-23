package dto

import (
	"time"

	"github.com/zet-plane/live-auction-backend/internal/app/user/model"
)

type UserDTO struct {
	ID        string             `json:"id"`
	Account   string             `json:"account"`
	Name      string             `json:"name"`
	AvatarURL string             `json:"avatar_url"`
	Motto     string             `json:"motto"`
	Identity  model.UserIdentity `json:"identity"`
	CreatedAt time.Time          `json:"created_at,omitempty"`
	UpdatedAt time.Time          `json:"updated_at,omitempty"`
}

func NewUserDTO(u *model.User) UserDTO {
	return UserDTO{
		ID:        u.ID,
		Account:   u.Account,
		Name:      u.Name,
		AvatarURL: u.AvatarURL,
		Motto:     u.Motto,
		Identity:  u.Identity,
		CreatedAt: u.CreatedAt,
		UpdatedAt: u.UpdatedAt,
	}
}

type LoginResult struct {
	Token string  `json:"token"`
	User  UserDTO `json:"user"`
}

type UpdateProfileInput struct {
	Name      *string
	AvatarURL *string
	Motto     *string
	Identity  *model.UserIdentity
}

type RegisterInput struct {
	Account  string
	Password string
}

type RegisterRequest struct {
	Account  string `json:"account"  binding:"required,min=3,max=64"`
	Password string `json:"password" binding:"required,min=6,max=72"`
}

type LoginRequest struct {
	Account  string `json:"account"  binding:"required"`
	Password string `json:"password" binding:"required"`
}

type UpdateProfileRequest struct {
	Name      *string             `json:"name"       binding:"omitempty,min=1,max=64"`
	AvatarURL *string             `json:"avatar_url" binding:"omitempty,max=512"`
	Motto     *string             `json:"motto"      binding:"omitempty,max=255"`
	Identity  *model.UserIdentity `json:"identity"   binding:"omitempty,oneof=user merchant"`
}
