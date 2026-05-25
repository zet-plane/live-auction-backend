package handler

import (
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

func AuthenticateToken(token string) (any, error) {
	if svc == nil {
		return nil, errorx.ErrInternal
	}
	result, err := svc.Authenticate(token)
	if err != nil {
		logx.Warnw("AuthenticateToken failed", "err", err)
	}
	return result, err
}

func Register(r flamego.Render, body dto.RegisterRequest, errs binding.Errors) {
	if web.BindingErrors(r, errs) {
		return
	}
	if svc == nil {
		response.Error(r, errorx.ErrInternal)
		return
	}
	result, err := svc.Register(dto.RegisterInput{
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

func Login(r flamego.Render, body dto.LoginRequest, errs binding.Errors) {
	if web.BindingErrors(r, errs) {
		return
	}
	if svc == nil {
		response.Error(r, errorx.ErrInternal)
		return
	}
	result, err := svc.Login(body.Account, body.Password)
	if err != nil {
		logx.Warnw("Login failed", "account", body.Account, "err", err)
		response.Error(r, err)
		return
	}
	response.OK(r, result)
}

func Me(r flamego.Render, current *model.User) {
	response.OK(r, dto.NewUserDTO(current))
}

func UpdateMe(r flamego.Render, current *model.User, body dto.UpdateProfileRequest, errs binding.Errors) {
	if web.BindingErrors(r, errs) {
		return
	}
	if svc == nil {
		response.Error(r, errorx.ErrInternal)
		return
	}
	err := svc.UpdateProfile(current, dto.UpdateProfileInput{
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

func DeleteMe(r flamego.Render, current *model.User) {
	if svc == nil {
		response.Error(r, errorx.ErrInternal)
		return
	}
	if err := svc.DeleteMe(current); err != nil {
		logx.Warnw("DeleteMe failed", "user_id", current.ID, "err", err)
		response.Error(r, err)
		return
	}
	response.OK(r, nil)
}
