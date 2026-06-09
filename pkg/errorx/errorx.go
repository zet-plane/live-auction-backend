package errorx

import "net/http"

// CodeError carries the HTTP status, application error code, and message
// that should be sent to the client. Any error returned through the service
// boundary should be (or wrap) a *CodeError so the handler layer can render
// it without knowing which specific error occurred.
type CodeError struct {
	HTTPStatus int
	Code       int
	Msg        string
}

func (e *CodeError) Error() string { return e.Msg }

func New(httpStatus, code int, msg string) *CodeError {
	return &CodeError{HTTPStatus: httpStatus, Code: code, Msg: msg}
}

var (
	ErrInvalidRequest     = New(http.StatusBadRequest, 40001, "invalid request")
	ErrInvalidIdentity    = New(http.StatusBadRequest, 40001, "invalid identity")
	ErrUnauthorized       = New(http.StatusUnauthorized, 40101, "unauthorized")
	ErrNotFound           = New(http.StatusNotFound, 40401, "not found")
	ErrServiceUnavailable = New(http.StatusServiceUnavailable, 50301, "service unavailable")
	ErrInternal           = New(http.StatusInternalServerError, 50001, "internal server error")
)
