package handler

import (
	"github.com/flamego/binding"
	"github.com/flamego/flamego"
	"github.com/zet-plane/live-auction-backend/internal/app/room/dto"
	"github.com/zet-plane/live-auction-backend/internal/app/room/model"
	"github.com/zet-plane/live-auction-backend/internal/app/room/service"
	usermodel "github.com/zet-plane/live-auction-backend/internal/app/user/model"
	"github.com/zet-plane/live-auction-backend/internal/middleware/response"
	"github.com/zet-plane/live-auction-backend/internal/middleware/web"
	"github.com/zet-plane/live-auction-backend/pkg/errorx"
)

var svc *service.Service

func Init(s *service.Service) { svc = s }

func ActivateRoom(r flamego.Render, current *usermodel.User, body dto.CreateRoomRequest, errs binding.Errors) {
	if web.BindingErrors(r, errs) {
		return
	}
	if svc == nil {
		response.Error(r, errorx.ErrInternal)
		return
	}
	result, err := svc.ActivateRoom(current, body.Input())
	if err != nil {
		response.Error(r, err)
		return
	}
	response.OK(r, result)
}

func GetMerchantRoom(r flamego.Render, current *usermodel.User) {
	if svc == nil {
		response.Error(r, errorx.ErrInternal)
		return
	}
	result, err := svc.GetMerchantRoom(current)
	if err != nil {
		response.Error(r, err)
		return
	}
	response.OK(r, result)
}

func StartRoom(r flamego.Render, c flamego.Context, current *usermodel.User) {
	if svc == nil {
		response.Error(r, errorx.ErrInternal)
		return
	}
	if err := svc.StartRoom(current, c.Param("room_id")); err != nil {
		response.Error(r, err)
		return
	}
	response.OK(r, nil)
}

func EndRoom(r flamego.Render, c flamego.Context, current *usermodel.User) {
	if svc == nil {
		response.Error(r, errorx.ErrInternal)
		return
	}
	if err := svc.EndRoom(current, c.Param("room_id")); err != nil {
		response.Error(r, err)
		return
	}
	response.OK(r, nil)
}

func GetRoom(r flamego.Render, c flamego.Context) {
	if svc == nil {
		response.Error(r, errorx.ErrInternal)
		return
	}
	result, err := svc.GetRoom(c.Param("room_id"))
	if err != nil {
		response.Error(r, err)
		return
	}
	response.OK(r, result)
}

func ListRooms(r flamego.Render, c flamego.Context) {
	if svc == nil {
		response.Error(r, errorx.ErrInternal)
		return
	}
	statusFilter := model.RoomStatus(c.Query("status"))
	result, err := svc.ListRooms(statusFilter)
	if err != nil {
		response.Error(r, err)
		return
	}
	response.OK(r, result)
}
