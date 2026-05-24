package handler

import (
	"github.com/flamego/flamego"
	"github.com/zet-plane/live-auction-backend/internal/app/deposit/service"
	usermodel "github.com/zet-plane/live-auction-backend/internal/app/user/model"
	"github.com/zet-plane/live-auction-backend/internal/middleware/response"
	"github.com/zet-plane/live-auction-backend/pkg/errorx"
)

var svc *service.Service

func Init(s *service.Service) {
	svc = s
}

func PayDeposit(r flamego.Render, c flamego.Context, current *usermodel.User) {
	if svc == nil {
		response.Error(r, errorx.ErrInternal)
		return
	}
	result, err := svc.PayDeposit(current, c.Param("item_id"))
	if err != nil {
		response.Error(r, err)
		return
	}
	response.OK(r, result)
}

func GetMyDeposit(r flamego.Render, c flamego.Context, current *usermodel.User) {
	if svc == nil {
		response.Error(r, errorx.ErrInternal)
		return
	}
	result, err := svc.GetMyDeposit(current, c.Param("item_id"))
	if err != nil {
		response.Error(r, err)
		return
	}
	response.OK(r, result)
}
