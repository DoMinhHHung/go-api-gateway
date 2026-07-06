package middlewares

import (
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	metricsOnce  = sync.Once{}
	requestTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "gateway_http_requests_total",
			Help: "Total number of gateway requests by route, method and status.",
		},
		[]string{"route", "method", "status"},
	)
	requestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "gateway_http_request_duration_seconds",
			Help:    "Request latency by route and method.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"route", "method"},
	)
)

func ensureMetricsRegistered() {
	metricsOnce.Do(func() {
		prometheus.MustRegister(requestTotal, requestDuration)
	})
}

func ObserveRequest(routeID, method string, status int, duration time.Duration) {
	ensureMetricsRegistered()
	requestTotal.WithLabelValues(routeID, method, strconv.Itoa(status)).Inc()
	requestDuration.WithLabelValues(routeID, method).Observe(duration.Seconds())
}

func Metrics(routeID string) Middleware {
	ensureMetricsRegistered()
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			wrapped := wrapResponseWriter(w)
			defer func() {
				ObserveRequest(routeID, r.Method, wrapped.status, time.Since(start))
			}()
			next.ServeHTTP(wrapped, r)
		})
	}
}
