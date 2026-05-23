package web

import (
	"net/http"
	"strings"

	"github.com/flamego/flamego"
	"github.com/zet-plane/live-auction-backend/internal/middleware/response"
	"github.com/zet-plane/live-auction-backend/pkg/errorx"
)

// Authorization extracts the Bearer token, calls verify, and maps the
// returned user value into the flamego DI container.
// verify must return a concrete pointer type (e.g. *model.User) so that
// flamego can inject it by type into downstream handlers.
func Authorization(verify func(string) (any, error)) flamego.Handler {
	return func(c flamego.Context, r flamego.Render, req *http.Request) {
		header := req.Header.Get("Authorization")
		token := strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
		if header == "" || token == header || token == "" {
			response.Error(r, errorx.ErrUnauthorized)
			return
		}
		u, err := verify(token)
		if err != nil {
			response.Error(r, err)
			return
		}
		c.Map(u)
		c.Next()
	}
}
