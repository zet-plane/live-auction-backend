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
	"github.com/zet-plane/live-auction-backend/pkg/logx"
)

var svc *service.Service

func Init(s *service.Service) { svc = s }

// ActivateRoom creates or updates the current merchant's live room.
//
// @Summary 开通商家直播间
// @Tags rooms
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param body body dto.CreateRoomRequest true "直播间请求"
// @Success 200 {object} response.Body{data=dto.MerchantRoomDTO}
// @Failure 400 {object} response.Body
// @Failure 401 {object} response.Body
// @Failure 500 {object} response.Body
// @Router /api/v1/merchant/room [post]
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
		logx.Warnw("ActivateRoom failed", "user_id", current.ID, "err", err)
		response.Error(r, err)
		return
	}
	response.OK(r, result)
}

// GetMerchantRoom returns the current merchant's live room.
//
// @Summary 获取商家直播间
// @Tags rooms
// @Produce json
// @Security BearerAuth
// @Success 200 {object} response.Body{data=dto.MerchantRoomDTO}
// @Failure 401 {object} response.Body
// @Failure 500 {object} response.Body
// @Router /api/v1/merchant/room [get]
func GetMerchantRoom(r flamego.Render, current *usermodel.User) {
	if svc == nil {
		response.Error(r, errorx.ErrInternal)
		return
	}
	result, err := svc.GetMerchantRoom(current)
	if err != nil {
		logx.Warnw("GetMerchantRoom failed", "user_id", current.ID, "err", err)
		response.Error(r, err)
		return
	}
	response.OK(r, result)
}

// StartRoom starts a live room.
//
// @Summary 开始直播间
// @Tags rooms
// @Produce json
// @Security BearerAuth
// @Param room_id path string true "直播间 ID"
// @Success 200 {object} response.Body
// @Failure 401 {object} response.Body
// @Failure 500 {object} response.Body
// @Router /api/v1/rooms/{room_id}/start [post]
func StartRoom(r flamego.Render, c flamego.Context, current *usermodel.User) {
	if svc == nil {
		response.Error(r, errorx.ErrInternal)
		return
	}
	if err := svc.StartRoom(current, c.Param("room_id")); err != nil {
		logx.Warnw("StartRoom failed", "user_id", current.ID, "room_id", c.Param("room_id"), "err", err)
		response.Error(r, err)
		return
	}
	response.OK(r, nil)
}

// EndRoom ends a live room.
//
// @Summary 结束直播间
// @Tags rooms
// @Produce json
// @Security BearerAuth
// @Param room_id path string true "直播间 ID"
// @Success 200 {object} response.Body
// @Failure 401 {object} response.Body
// @Failure 500 {object} response.Body
// @Router /api/v1/rooms/{room_id}/end [post]
func EndRoom(r flamego.Render, c flamego.Context, current *usermodel.User) {
	if svc == nil {
		response.Error(r, errorx.ErrInternal)
		return
	}
	if err := svc.EndRoom(current, c.Param("room_id")); err != nil {
		logx.Warnw("EndRoom failed", "user_id", current.ID, "room_id", c.Param("room_id"), "err", err)
		response.Error(r, err)
		return
	}
	response.OK(r, nil)
}

// GetRoom returns a public live room detail.
//
// @Summary 直播间详情
// @Tags rooms
// @Produce json
// @Param room_id path string true "直播间 ID"
// @Success 200 {object} response.Body{data=dto.RoomDetailDTO}
// @Failure 400 {object} response.Body
// @Failure 500 {object} response.Body
// @Router /api/v1/rooms/{room_id} [get]
func GetRoom(r flamego.Render, c flamego.Context) {
	if svc == nil {
		response.Error(r, errorx.ErrInternal)
		return
	}
	result, err := svc.GetRoom(c.Param("room_id"))
	if err != nil {
		logx.Warnw("GetRoom failed", "room_id", c.Param("room_id"), "err", err)
		response.Error(r, err)
		return
	}
	response.OK(r, result)
}

// ListRooms lists public live rooms.
//
// @Summary 直播间列表
// @Tags rooms
// @Produce json
// @Param status query string false "直播间状态"
// @Success 200 {object} response.Body{data=[]dto.RoomDetailDTO}
// @Failure 500 {object} response.Body
// @Router /api/v1/rooms [get]
func ListRooms(r flamego.Render, c flamego.Context) {
	if svc == nil {
		response.Error(r, errorx.ErrInternal)
		return
	}
	statusFilter := model.RoomStatus(c.Query("status"))
	result, err := svc.ListRooms(statusFilter)
	if err != nil {
		logx.Warnw("ListRooms failed", "status", statusFilter, "err", err)
		response.Error(r, err)
		return
	}
	response.OK(r, result)
}
