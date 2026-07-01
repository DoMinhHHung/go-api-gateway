package core

import (
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/DoMinhHHung/go-api-gateway/internal/config"
	"github.com/sony/gobreaker"
)

type BreakerTransport struct {
	RoundTripper http.RoundTripper
	Breaker      *gobreaker.CircuitBreaker
}

func (bt *BreakerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	res, err := bt.Breaker.Execute(func() (interface{}, error) {
		resp, err := bt.RoundTripper.RoundTrip(req)
		if err != nil {
			return nil, err
		}

		if resp.StatusCode >= 500 {
			return resp, errors.New("Service is unavailable!")
		}

		return resp, nil
	})

	if err != nil {
		if err == gobreaker.ErrOpenState {
			return nil, err
		}

		if res != nil {
			return res.(*http.Response), nil
		}
		return nil, err
	}

	return res.(*http.Response), nil
}

func NewBreakerTransport(rt http.RoundTripper, routeID string, cfg config.CircuitBreakerConfig) http.RoundTripper {
	if !cfg.Enabled {
		return rt
	}

	cb := gobreaker.NewCircuitBreaker(gobreaker.Settings{
		Name:        routeID,
		MaxRequests: cfg.MaxRequests,
		Interval:    time.Duration(cfg.Interval) * time.Second,
		Timeout:     time.Duration(cfg.Timeout) * time.Second,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			failureRatio := float64(counts.TotalFailures) / float64(counts.Requests)
			return counts.Requests >= 3 && failureRatio >= 0.6
		},
		OnStateChange: func(name string, from gobreaker.State, to gobreaker.State) {
			log.Printf("[CircuitBreaker] %s status changed: %s -> %s", name, from, to)
		},
	})

	return &BreakerTransport{
		RoundTripper: rt,
		Breaker:      cb,
	}
}
