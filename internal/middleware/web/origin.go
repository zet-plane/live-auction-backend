package web

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/flamego/flamego"
)

type OriginPolicy struct {
	allowed            map[string]struct{}
	localhostWildcards map[string]struct{}
	allowAny           bool
	allowLocalhost     bool
}

func NewOriginPolicy(mode string, origins []string) OriginPolicy {
	policy := OriginPolicy{
		allowed:            make(map[string]struct{}),
		localhostWildcards: make(map[string]struct{}),
	}
	for _, origin := range origins {
		origin = normalizeOrigin(origin)
		if origin == "" {
			continue
		}
		if origin == "*" {
			policy.allowAny = true
			continue
		}
		if key, ok := parseLocalhostWildcard(origin); ok {
			policy.localhostWildcards[key] = struct{}{}
			continue
		}
		policy.allowed[origin] = struct{}{}
	}
	policy.allowLocalhost = !policy.allowAny && len(policy.allowed) == 0 && !isProductionMode(mode)
	return policy
}

func (p OriginPolicy) Allows(origin string) bool {
	origin = normalizeOrigin(origin)
	if origin == "" {
		return true
	}
	if p.allowAny {
		return true
	}
	if _, ok := p.allowed[origin]; ok {
		return true
	}
	if p.allowsLocalhostWildcard(origin) {
		return true
	}
	if p.allowLocalhost && isLocalhostOrigin(origin) {
		return true
	}
	return false
}

func CORSMiddleware(policy OriginPolicy) flamego.Handler {
	methods := strings.Join([]string{
		http.MethodGet,
		http.MethodPost,
		http.MethodPut,
		http.MethodPatch,
		http.MethodDelete,
		http.MethodOptions,
	}, ",")

	return flamego.ContextInvoker(func(ctx flamego.Context) {
		origin := ctx.Request().Header.Get("Origin")
		if origin == "" {
			return
		}
		if !policy.Allows(origin) {
			http.Error(ctx.ResponseWriter(), fmt.Sprintf("CORS request from prohibited origin %s", origin), http.StatusBadRequest)
			return
		}

		headers := map[string]string{
			"Access-Control-Allow-Origin":      normalizeOrigin(origin),
			"Access-Control-Allow-Credentials": "true",
			"Access-Control-Allow-Methods":     methods,
			"Access-Control-Allow-Headers":     ctx.Request().Header.Get("Access-Control-Request-Headers"),
			"Access-Control-Max-Age":           fmt.Sprintf("%.0f", (10 * time.Minute).Seconds()),
			"Vary":                             "Origin",
		}
		ctx.ResponseWriter().Before(func(w flamego.ResponseWriter) {
			for k, v := range headers {
				w.Header().Set(k, v)
			}
		})

		if ctx.Request().Method == http.MethodOptions {
			ctx.ResponseWriter().WriteHeader(http.StatusOK)
		}
	})
}

func normalizeOrigin(origin string) string {
	return strings.TrimRight(strings.TrimSpace(origin), "/")
}

func isLocalhostOrigin(origin string) bool {
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return isLocalhostHost(u.Hostname())
}

func isLocalhostHost(host string) bool {
	switch strings.ToLower(host) {
	case "localhost", "127.0.0.1", "::1":
		return true
	default:
		return false
	}
}

func parseLocalhostWildcard(origin string) (string, bool) {
	if !strings.HasSuffix(origin, ":*") {
		return "", false
	}
	u, err := url.Parse(strings.TrimSuffix(origin, ":*"))
	if err != nil || u.Scheme == "" || !isLocalhostHost(u.Hostname()) {
		return "", false
	}
	return strings.ToLower(u.Scheme) + "://" + strings.ToLower(u.Hostname()), true
}

func (p OriginPolicy) allowsLocalhostWildcard(origin string) bool {
	if len(p.localhostWildcards) == 0 {
		return false
	}
	u, err := url.Parse(origin)
	if err != nil || u.Scheme == "" || !isLocalhostHost(u.Hostname()) {
		return false
	}
	key := strings.ToLower(u.Scheme) + "://" + strings.ToLower(u.Hostname())
	_, ok := p.localhostWildcards[key]
	return ok
}

func isProductionMode(mode string) bool {
	mode = strings.ToLower(strings.TrimSpace(mode))
	return mode == "release" || mode == string(flamego.EnvTypeProd)
}
