package gw

import (
	"net/http"
	"time"

	"github.com/flamego/flamego"
	"github.com/zet-plane/live-auction-backend/pkg/logx"
)

func RequestLog() flamego.Handler {
	return func(c flamego.Context, req *http.Request) {
		start := time.Now()
		c.Next()
		logx.Infof("%s %s %d %s", req.Method, req.URL.RequestURI(), c.ResponseWriter().Status(), time.Since(start))
	}
}
