package gw

import (
	"log"
	"net/http"
	"time"

	"github.com/flamego/flamego"
)

func RequestLog() flamego.Handler {
	return func(c flamego.Context, req *http.Request) {
		start := time.Now()
		c.Next()
		log.Printf("%s %s %d %s", req.Method, req.URL.RequestURI(), c.ResponseWriter().Status(), time.Since(start))
	}
}
