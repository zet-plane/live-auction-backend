package handler

import (
	"context"
	"net/http"

	"github.com/flamego/binding"
	"github.com/flamego/flamego"
	"github.com/zet-plane/live-auction-backend/internal/app/user/dto"
	"github.com/zet-plane/live-auction-backend/internal/app/user/model"
	"github.com/zet-plane/live-auction-backend/internal/app/user/service"
	"github.com/zet-plane/live-auction-backend/internal/middleware/response"
	"github.com/zet-plane/live-auction-backend/internal/middleware/web"
	"github.com/zet-plane/live-auction-backend/pkg/errorx"
	"github.com/zet-plane/live-auction-backend/pkg/logx"
)

var svc *service.Service

func Init(s *service.Service) {
	svc = s
}

func AuthenticateToken(ctx context.Context, token string) (any, error) {
	if svc == nil {
		return nil, errorx.ErrInternal
	}
	result, err := svc.Authenticate(ctx, token)
	if err != nil {
		logx.Warnw("AuthenticateToken failed", "err", err)
	}
	return result, err
}

func AuthenticateTokenClaims(ctx context.Context, token string) (any, error) {
	if svc == nil {
		return nil, errorx.ErrInternal
	}
	result, err := svc.AuthenticateClaims(ctx, token)
	if err != nil {
		logx.Warnw("AuthenticateTokenClaims failed", "err", err)
	}
	return result, err
}

// Register creates a user account.
//
// @Summary 用户注册
// @Tags auth
// @Accept json
// @Produce json
// @Param body body dto.RegisterRequest true "注册请求"
// @Success 200 {object} response.Body{data=dto.LoginResult}
// @Failure 400 {object} response.Body
// @Failure 500 {object} response.Body
// @Router /api/v1/auth/register [post]
func Register(r flamego.Render, req *http.Request, body dto.RegisterRequest, errs binding.Errors) {
	if web.BindingErrors(r, errs) {
		return
	}
	if svc == nil {
		response.Error(r, errorx.ErrInternal)
		return
	}
	result, err := svc.Register(req.Context(), dto.RegisterInput{
		Account:  body.Account,
		Password: body.Password,
	})
	if err != nil {
		logx.Warnw("Register failed", "account", body.Account, "err", err)
		response.Error(r, err)
		return
	}
	response.OK(r, result)
}

// Login authenticates a user account.
//
// @Summary 用户登录
// @Tags auth
// @Accept json
// @Produce json
// @Param body body dto.LoginRequest true "登录请求"
// @Success 200 {object} response.Body{data=dto.LoginResult}
// @Failure 400 {object} response.Body
// @Failure 401 {object} response.Body
// @Failure 500 {object} response.Body
// @Router /api/v1/auth/login [post]
func Login(r flamego.Render, req *http.Request, body dto.LoginRequest, errs binding.Errors) {
	if web.BindingErrors(r, errs) {
		return
	}
	if svc == nil {
		response.Error(r, errorx.ErrInternal)
		return
	}
	result, err := svc.Login(req.Context(), body.Account, body.Password)
	if err != nil {
		logx.Warnw("Login failed", "account", body.Account, "err", err)
		response.Error(r, err)
		return
	}
	response.OK(r, result)
}

// Me returns the current user profile.
//
// @Summary 获取当前用户
// @Tags users
// @Produce json
// @Security BearerAuth
// @Success 200 {object} response.Body{data=dto.UserDTO}
// @Failure 401 {object} response.Body
// @Failure 500 {object} response.Body
// @Router /api/v1/users/me [get]
func Me(r flamego.Render, current *model.User) {
	response.OK(r, dto.NewUserDTO(current))
}

// UpdateMe updates the current user profile.
//
// @Summary 更新当前用户
// @Tags users
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param body body dto.UpdateProfileRequest true "资料更新请求"
// @Success 200 {object} response.Body
// @Failure 400 {object} response.Body
// @Failure 401 {object} response.Body
// @Failure 500 {object} response.Body
// @Router /api/v1/users/me [put]
func UpdateMe(r flamego.Render, req *http.Request, current *model.User, body dto.UpdateProfileRequest, errs binding.Errors) {
	if web.BindingErrors(r, errs) {
		return
	}
	if svc == nil {
		response.Error(r, errorx.ErrInternal)
		return
	}
	err := svc.UpdateProfile(req.Context(), current, dto.UpdateProfileInput{
		Name:      body.Name,
		AvatarURL: body.AvatarURL,
		Motto:     body.Motto,
		Identity:  body.Identity,
	})
	if err != nil {
		logx.Warnw("UpdateMe failed", "user_id", current.ID, "err", err)
		response.Error(r, err)
		return
	}
	response.OK(r, nil)
}

// DeleteMe deletes the current user account.
//
// @Summary 删除当前用户
// @Tags users
// @Produce json
// @Security BearerAuth
// @Success 200 {object} response.Body
// @Failure 401 {object} response.Body
// @Failure 500 {object} response.Body
// @Router /api/v1/users/me [delete]
func DeleteMe(r flamego.Render, req *http.Request, current *model.User) {
	if svc == nil {
		response.Error(r, errorx.ErrInternal)
		return
	}
	if err := svc.DeleteMe(req.Context(), current); err != nil {
		logx.Warnw("DeleteMe failed", "user_id", current.ID, "err", err)
		response.Error(r, err)
		return
	}
	response.OK(r, nil)
}
