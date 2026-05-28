package handler

import (
	"net/http"

	"github.com/flamego/binding"
	"github.com/flamego/flamego"
	"github.com/zet-plane/live-auction-backend/internal/app/item/dto"
	usermodel "github.com/zet-plane/live-auction-backend/internal/app/user/model"
	"github.com/zet-plane/live-auction-backend/internal/middleware/response"
	"github.com/zet-plane/live-auction-backend/internal/middleware/web"
	"github.com/zet-plane/live-auction-backend/pkg/errorx"
	"github.com/zet-plane/live-auction-backend/pkg/logx"
)

// PlaceBid places a bid for an ongoing auction item.
//
// @Summary 出价
// @Tags bids
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param item_id path string true "拍品 ID"
// @Param body body dto.PlaceBidRequest true "出价请求"
// @Success 200 {object} response.Body{data=dto.PlaceBidResult}
// @Failure 400 {object} response.Body
// @Failure 401 {object} response.Body
// @Failure 500 {object} response.Body
// @Router /api/v1/items/{item_id}/bids [post]
func PlaceBid(r flamego.Render, req *http.Request, c flamego.Context, current *usermodel.User, body dto.PlaceBidRequest, errs binding.Errors) {
	if web.BindingErrors(r, errs) {
		return
	}
	if svc == nil {
		response.Error(r, errorx.ErrInternal)
		return
	}
	result, err := svc.PlaceBid(req.Context(), current, c.Param("item_id"), body.Input(current.Name))
	if err != nil {
		logx.Warnw("PlaceBid failed", "user_id", current.ID, "item_id", c.Param("item_id"), "price", body.Price, "err", err)
		response.Error(r, err)
		return
	}
	response.OK(r, result)
}

// GetRanking returns bid ranking for an auction item.
//
// @Summary 出价排行榜
// @Tags bids
// @Produce json
// @Param item_id path string true "拍品 ID"
// @Param page query int false "页码"
// @Param page_size query int false "每页数量"
// @Success 200 {object} response.Body{data=dto.RankingResult}
// @Failure 400 {object} response.Body
// @Failure 500 {object} response.Body
// @Router /api/v1/items/{item_id}/ranking [get]
func GetRanking(r flamego.Render, req *http.Request, c flamego.Context) {
	if svc == nil {
		response.Error(r, errorx.ErrInternal)
		return
	}
	result, err := svc.GetRanking(req.Context(), c.Param("item_id"), queryInt(c, "page"), queryInt(c, "page_size"))
	if err != nil {
		logx.Warnw("GetRanking failed", "item_id", c.Param("item_id"), "err", err)
		response.Error(r, err)
		return
	}
	response.OK(r, result)
}
