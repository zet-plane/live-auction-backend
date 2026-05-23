package response

import (
	"errors"
	"net/http"

	"github.com/flamego/flamego"
	"github.com/zet-plane/live-auction-backend/pkg/errorx"
)

type Body struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data"`
}

func OK(r flamego.Render, data any) {
	Success(r, httpStatusOK, "ok", data)
}

func Success(r flamego.Render, status int, message string, data any) {
	r.JSON(status, Body{
		Code:    0,
		Message: message,
		Data:    data,
	})
}

func Fail(r flamego.Render, status int, code int, message string) {
	r.JSON(status, Body{
		Code:    code,
		Message: message,
		Data:    nil,
	})
}

// Error renders a *errorx.CodeError as an HTTP JSON response.
// Any unrecognised error falls back to 500 internal server error.
func Error(r flamego.Render, err error) {
	var ce *errorx.CodeError
	if errors.As(err, &ce) {
		Fail(r, ce.HTTPStatus, ce.Code, ce.Msg)
		return
	}
	Fail(r, http.StatusInternalServerError, errorx.ErrInternal.Code, errorx.ErrInternal.Msg)
}

const httpStatusOK = 200
