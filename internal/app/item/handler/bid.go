package handler

import (
	"strconv"

	"github.com/flamego/binding"
	"github.com/flamego/flamego"
	"github.com/zet-plane/live-auction-backend/internal/app/item/dto"
	usermodel "github.com/zet-plane/live-auction-backend/internal/app/user/model"
	"github.com/zet-plane/live-auction-backend/internal/middleware/response"
	"github.com/zet-plane/live-auction-backend/internal/middleware/web"
	"github.com/zet-plane/live-auction-backend/pkg/errorx"
)

func PlaceBid(r flamego.Render, c flamego.Context, current *usermodel.User, body dto.PlaceBidRequest, errs binding.Errors) {
	if web.BindingErrors(r, errs) {
		return
	}
	if svc == nil {
		response.Error(r, errorx.ErrInternal)
		return
	}
	result, err := svc.PlaceBid(current, c.Param("item_id"), body.Input(current.Name))
	if err != nil {
		response.Error(r, err)
		return
	}
	response.OK(r, result)
}

func GetRanking(r flamego.Render, c flamego.Context) {
	if svc == nil {
		response.Error(r, errorx.ErrInternal)
		return
	}
	page, _ := strconv.Atoi(c.Query("page"))
	pageSize, _ := strconv.Atoi(c.Query("page_size"))
	result, err := svc.GetRanking(c.Param("item_id"), page, pageSize)
	if err != nil {
		response.Error(r, err)
		return
	}
	response.OK(r, result)
}
