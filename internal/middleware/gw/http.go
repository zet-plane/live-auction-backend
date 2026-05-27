package gw

import (
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/flamego/flamego"
	"github.com/zet-plane/live-auction-backend/pkg/logx"
)

func RequestLog() flamego.Handler {
	return func(c flamego.Context, req *http.Request) {
		start := time.Now()
		c.Next()
		logx.Infof("%s %s %d %s", req.Method, sanitizeRequestURI(req.URL), c.ResponseWriter().Status(), time.Since(start))
	}
}

func sanitizeRequestURI(u *url.URL) string {
	if u == nil {
		return ""
	}
	if u.RawQuery == "" {
		return u.RequestURI()
	}
	redacted := *u
	values := redacted.Query()
	for key := range values {
		if isSensitiveQueryKey(key) {
			values[key] = []string{"REDACTED"}
		}
	}
	redacted.RawQuery = values.Encode()
	return redacted.RequestURI()
}

func isSensitiveQueryKey(key string) bool {
	switch strings.ToLower(key) {
	case "ticket", "token", "access_token", "refresh_token", "jwt":
		return true
	default:
		return false
	}
}
