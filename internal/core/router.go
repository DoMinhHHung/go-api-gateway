package core

import (
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"time"

	"github.com/DoMinhHHung/go-api-gateway/internal/config"
	"github.com/DoMinhHHung/go-api-gateway/internal/middlewares"
)

func SetupRoutes(cfg *config.AppConfig) *http.ServeMux {
	mux := http.NewServeMux()
	registry := middlewares.InitRegistry(cfg)

	for _, route := range cfg.Routes {
		if len(route.Backends) == 0 {
			continue
		}

		targetURL, _ := url.Parse(route.Backends[0].URL)
		proxy := httputil.NewSingleHostReverseProxy(targetURL)

		proxy.Transport = &http.Transport{
			MaxIdleConns:          100,
			MaxIdleConnsPerHost:   100,
			MaxConnsPerHost:       200,
			IdleConnTimeout:       90 * time.Second,
			ResponseHeaderTimeout: 5 * time.Second,
		}

		for _, method := range route.Methods {
			pattern := method + " " + route.Path

			var handler http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				log.Printf("[%s] Forwarding %s %s", route.ID, r.Method, r.URL.Path)
				proxy.ServeHTTP(w, r)
			})

			if len(route.Middlewares) > 0 {
				handler = middlewares.ApplyChain(handler, route.Middlewares, registry)
			}

			mux.Handle(pattern, handler)
		}
	}

	return mux
}
