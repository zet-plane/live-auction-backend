package server

import (
	"net/http"

	"github.com/flamego/flamego"
	httpSwagger "github.com/swaggo/http-swagger/v2"
	_ "github.com/zet-plane/live-auction-backend/internal/swaggerdocs"
)

func registerSwaggerRoutes(f *flamego.Flame) {
	handler := httpSwagger.Handler(httpSwagger.URL("/swagger/doc.json"))

	f.Get("/swagger/{**}", func(w http.ResponseWriter, req *http.Request) {
		handler.ServeHTTP(w, req)
	})
}
