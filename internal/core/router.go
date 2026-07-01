package core

import (
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"

	"github.com/DoMinhHHung/go-api-gateway/internal/config"
)

func SetupRoutes(cfg *config.AppConfig) *http.ServeMux {
	mux := http.NewServeMux()

	for _, route := range cfg.Routes {
		if len(route.Backends) == 0 {
			log.Printf("[Cảnh báo] Route %s không có backend nào, bỏ qua.", route.ID)
			continue
		}

		backendURLStr := route.Backends[0].URL
		targetURL, err := url.Parse(backendURLStr)
		if err != nil {
			log.Fatalf("[Lỗi] Backend URL không hợp lệ ở route %s: %v", route.ID, err)
		}

		proxy := httputil.NewSingleHostReverseProxy(targetURL)

		for _, method := range route.Methods {
			pattern := method + " " + route.Path

			mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
				log.Printf("[%s] Forwarding %s %s -> %s", route.ID, r.Method, r.URL.Path, targetURL.String())
				proxy.ServeHTTP(w, r)
			})
		}
	}

	return mux
}
