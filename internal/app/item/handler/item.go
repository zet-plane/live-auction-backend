package handler

import (
	"strconv"

	"github.com/flamego/binding"
	"github.com/flamego/flamego"
	"github.com/zet-plane/live-auction-backend/internal/app/item/dto"
	itemmodel "github.com/zet-plane/live-auction-backend/internal/app/item/model"
	"github.com/zet-plane/live-auction-backend/internal/app/item/service"
	usermodel "github.com/zet-plane/live-auction-backend/internal/app/user/model"
	"github.com/zet-plane/live-auction-backend/internal/middleware/response"
	"github.com/zet-plane/live-auction-backend/internal/middleware/web"
	"github.com/zet-plane/live-auction-backend/pkg/errorx"
	"github.com/zet-plane/live-auction-backend/pkg/logx"
)

var svc *service.Service

func Init(s *service.Service) {
	svc = s
}

// CreateItem creates a draft auction item.
//
// @Summary 创建拍品
// @Tags items
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param body body dto.CreateItemRequest true "拍品创建请求"
// @Success 200 {object} response.Body{data=dto.CreateItemResult}
// @Failure 400 {object} response.Body
// @Failure 401 {object} response.Body
// @Failure 500 {object} response.Body
// @Router /api/v1/items [post]
func CreateItem(r flamego.Render, current *usermodel.User, body dto.CreateItemRequest, errs binding.Errors) {
	if web.BindingErrors(r, errs) {
		return
	}
	if svc == nil {
		response.Error(r, errorx.ErrInternal)
		return
	}
	result, err := svc.CreateItem(current, body.Input())
	if err != nil {
		logx.Warnw("CreateItem failed", "user_id", current.ID, "err", err)
		response.Error(r, err)
		return
	}
	response.OK(r, result)
}

// ListItems lists public auction items.
//
// @Summary 拍品列表
// @Tags items
// @Produce json
// @Param status query string false "拍品状态"
// @Param keyword query string false "关键词"
// @Param page query int false "页码"
// @Param page_size query int false "每页数量"
// @Success 200 {object} response.Body{data=dto.ItemListResult}
// @Failure 500 {object} response.Body
// @Router /api/v1/items [get]
func ListItems(r flamego.Render, c flamego.Context) {
	if svc == nil {
		response.Error(r, errorx.ErrInternal)
		return
	}
	result, err := svc.ListItems(listInput(c))
	if err != nil {
		logx.Warnw("ListItems failed", "err", err)
		response.Error(r, err)
		return
	}
	response.OK(r, result)
}

// ListMerchantItems lists the current merchant's auction items.
//
// @Summary 商家拍品列表
// @Tags items
// @Produce json
// @Security BearerAuth
// @Param status query string false "拍品状态"
// @Param keyword query string false "关键词"
// @Param page query int false "页码"
// @Param page_size query int false "每页数量"
// @Success 200 {object} response.Body{data=dto.MerchantItemListResult}
// @Failure 401 {object} response.Body
// @Failure 500 {object} response.Body
// @Router /api/v1/merchant/items [get]
func ListMerchantItems(r flamego.Render, c flamego.Context, current *usermodel.User) {
	if svc == nil {
		response.Error(r, errorx.ErrInternal)
		return
	}
	result, err := svc.ListMerchantItems(current, listInput(c))
	if err != nil {
		logx.Warnw("ListMerchantItems failed", "user_id", current.ID, "err", err)
		response.Error(r, err)
		return
	}
	response.OK(r, result)
}

// GetItem returns an auction item detail.
//
// @Summary 拍品详情
// @Tags items
// @Produce json
// @Param item_id path string true "拍品 ID"
// @Success 200 {object} response.Body{data=dto.ItemDetailDTO}
// @Failure 400 {object} response.Body
// @Failure 404 {object} response.Body
// @Failure 500 {object} response.Body
// @Router /api/v1/items/{item_id} [get]
func GetItem(r flamego.Render, c flamego.Context) {
	if svc == nil {
		response.Error(r, errorx.ErrInternal)
		return
	}
	result, err := svc.GetItem(c.Param("item_id"))
	if err != nil {
		logx.Warnw("GetItem failed", "item_id", c.Param("item_id"), "err", err)
		response.Error(r, err)
		return
	}
	response.OK(r, result)
}

// UpdateItem updates a draft auction item.
//
// @Summary 更新拍品
// @Tags items
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param item_id path string true "拍品 ID"
// @Param body body dto.CreateItemRequest true "拍品更新请求"
// @Success 200 {object} response.Body
// @Failure 400 {object} response.Body
// @Failure 401 {object} response.Body
// @Failure 500 {object} response.Body
// @Router /api/v1/items/{item_id} [put]
func UpdateItem(r flamego.Render, c flamego.Context, current *usermodel.User, body dto.CreateItemRequest, errs binding.Errors) {
	if web.BindingErrors(r, errs) {
		return
	}
	if svc == nil {
		response.Error(r, errorx.ErrInternal)
		return
	}
	if err := svc.UpdateItem(current, c.Param("item_id"), body.Input()); err != nil {
		logx.Warnw("UpdateItem failed", "user_id", current.ID, "item_id", c.Param("item_id"), "err", err)
		response.Error(r, err)
		return
	}
	response.OK(r, nil)
}

// DeleteItem deletes an auction item.
//
// @Summary 删除拍品
// @Tags items
// @Produce json
// @Security BearerAuth
// @Param item_id path string true "拍品 ID"
// @Success 200 {object} response.Body
// @Failure 401 {object} response.Body
// @Failure 500 {object} response.Body
// @Router /api/v1/items/{item_id} [delete]
func DeleteItem(r flamego.Render, c flamego.Context, current *usermodel.User) {
	if svc == nil {
		response.Error(r, errorx.ErrInternal)
		return
	}
	if err := svc.DeleteItem(current, c.Param("item_id")); err != nil {
		logx.Warnw("DeleteItem failed", "user_id", current.ID, "item_id", c.Param("item_id"), "err", err)
		response.Error(r, err)
		return
	}
	response.OK(r, nil)
}

// PublishItem publishes a draft auction item.
//
// @Summary 发布拍品
// @Tags items
// @Produce json
// @Security BearerAuth
// @Param item_id path string true "拍品 ID"
// @Success 200 {object} response.Body
// @Failure 401 {object} response.Body
// @Failure 500 {object} response.Body
// @Router /api/v1/items/{item_id}/publish [post]
func PublishItem(r flamego.Render, c flamego.Context, current *usermodel.User) {
	statusAction(r, c, current, "PublishItem", svc.PublishItem)
}

// StartItem starts a published auction item.
//
// @Summary 开始拍品竞拍
// @Tags items
// @Produce json
// @Security BearerAuth
// @Param item_id path string true "拍品 ID"
// @Success 200 {object} response.Body
// @Failure 401 {object} response.Body
// @Failure 500 {object} response.Body
// @Router /api/v1/items/{item_id}/start [post]
func StartItem(r flamego.Render, c flamego.Context, current *usermodel.User) {
	statusAction(r, c, current, "StartItem", svc.StartItem)
}

// CancelItem cancels a published or ongoing auction item.
//
// @Summary 取消拍品竞拍
// @Tags items
// @Produce json
// @Security BearerAuth
// @Param item_id path string true "拍品 ID"
// @Success 200 {object} response.Body
// @Failure 401 {object} response.Body
// @Failure 500 {object} response.Body
// @Router /api/v1/items/{item_id}/cancel [post]
func CancelItem(r flamego.Render, c flamego.Context, current *usermodel.User) {
	statusAction(r, c, current, "CancelItem", svc.CancelItem)
}

func statusAction(r flamego.Render, c flamego.Context, current *usermodel.User, op string, action func(*usermodel.User, string) error) {
	if svc == nil {
		response.Error(r, errorx.ErrInternal)
		return
	}
	if err := action(current, c.Param("item_id")); err != nil {
		logx.Warnw(op+" failed", "user_id", current.ID, "item_id", c.Param("item_id"), "err", err)
		response.Error(r, err)
		return
	}
	response.OK(r, nil)
}

func listInput(c flamego.Context) dto.ListItemsInput {
	return dto.ListItemsInput{
		Status:   itemmodel.AuctionItemStatus(c.Query("status")),
		Keyword:  c.Query("keyword"),
		Page:     queryInt(c, "page"),
		PageSize: queryInt(c, "page_size"),
	}
}

func queryInt(c flamego.Context, key string) int {
	value := c.Query(key)
	if value == "" {
		return 0
	}
	n, _ := strconv.Atoi(value)
	return n
}
