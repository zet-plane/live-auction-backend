package web

import (
	"github.com/flamego/flamego"
	"github.com/zet-plane/live-auction-backend/pkg/page"
)

// InjectPaginate reads ?page=&pageSize= from the query string and maps
// a page.PageRequest into the flamego DI container.
// Handlers receive it as: func(req page.PageRequest) { db.Scopes(req.Scope()) }
func InjectPaginate() flamego.Handler {
	return func(c flamego.Context) {
		c.Map(page.PageRequest{
			Page:     c.QueryInt("page"),
			PageSize: c.QueryInt("pageSize"),
		})
	}
}
