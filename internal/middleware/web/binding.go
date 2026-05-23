package web

import (
	"github.com/flamego/binding"
	"github.com/flamego/flamego"
	"github.com/zet-plane/live-auction-backend/internal/middleware/response"
	"github.com/zet-plane/live-auction-backend/pkg/errorx"
)

func BindingErrors(r flamego.Render, errs binding.Errors) bool {
	if len(errs) == 0 {
		return false
	}
	response.Error(r, errorx.ErrInvalidRequest)
	return true
}
