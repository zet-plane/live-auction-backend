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
)

var svc *service.Service

func Init(s *service.Service) {
	svc = s
}

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
		response.Error(r, err)
		return
	}
	response.OK(r, result)
}

func ListItems(r flamego.Render, c flamego.Context) {
	if svc == nil {
		response.Error(r, errorx.ErrInternal)
		return
	}
	result, err := svc.ListItems(listInput(c))
	if err != nil {
		response.Error(r, err)
		return
	}
	response.OK(r, result)
}

func ListMerchantItems(r flamego.Render, c flamego.Context, current *usermodel.User) {
	if svc == nil {
		response.Error(r, errorx.ErrInternal)
		return
	}
	result, err := svc.ListMerchantItems(current, listInput(c))
	if err != nil {
		response.Error(r, err)
		return
	}
	response.OK(r, result)
}

func GetItem(r flamego.Render, c flamego.Context) {
	if svc == nil {
		response.Error(r, errorx.ErrInternal)
		return
	}
	result, err := svc.GetItem(c.Param("item_id"))
	if err != nil {
		response.Error(r, err)
		return
	}
	response.OK(r, result)
}

func UpdateItem(r flamego.Render, c flamego.Context, current *usermodel.User, body dto.CreateItemRequest, errs binding.Errors) {
	if web.BindingErrors(r, errs) {
		return
	}
	if svc == nil {
		response.Error(r, errorx.ErrInternal)
		return
	}
	if err := svc.UpdateItem(current, c.Param("item_id"), body.Input()); err != nil {
		response.Error(r, err)
		return
	}
	response.OK(r, nil)
}

func DeleteItem(r flamego.Render, c flamego.Context, current *usermodel.User) {
	if svc == nil {
		response.Error(r, errorx.ErrInternal)
		return
	}
	if err := svc.DeleteItem(current, c.Param("item_id")); err != nil {
		response.Error(r, err)
		return
	}
	response.OK(r, nil)
}

func PublishItem(r flamego.Render, c flamego.Context, current *usermodel.User) {
	statusAction(r, c, current, svc.PublishItem)
}

func StartItem(r flamego.Render, c flamego.Context, current *usermodel.User) {
	statusAction(r, c, current, svc.StartItem)
}

func CancelItem(r flamego.Render, c flamego.Context, current *usermodel.User) {
	statusAction(r, c, current, svc.CancelItem)
}

func statusAction(r flamego.Render, c flamego.Context, current *usermodel.User, action func(*usermodel.User, string) error) {
	if svc == nil {
		response.Error(r, errorx.ErrInternal)
		return
	}
	if err := action(current, c.Param("item_id")); err != nil {
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
