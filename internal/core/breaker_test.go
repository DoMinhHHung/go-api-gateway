package core

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DoMinhHHung/go-api-gateway/internal/config"
	"github.com/sony/gobreaker"
)

type fakeRoundTripper struct {
	responses []*http.Response
	errs      []error
	calls     int
}

func (f *fakeRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	i := f.calls
	f.calls++
	if i < len(f.errs) && f.errs[i] != nil {
		return nil, f.errs[i]
	}
	if i < len(f.responses) {
		return f.responses[i], nil
	}
	return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody}, nil
}

func resp(status int) *http.Response {
	return &http.Response{StatusCode: status, Body: http.NoBody}
}

func TestNewBreakerTransport_Disabled_ReturnsOriginalTransport(t *testing.T) {
	base := &fakeRoundTripper{}
	cfg := config.CircuitBreakerConfig{Enabled: false}

	rt := NewBreakerTransport(base, "route1", cfg)

	if _, ok := rt.(*BreakerTransport); ok {
		t.Fatal("expected disabled breaker to return the base transport untouched, got a BreakerTransport wrapper")
	}
}

func TestBreakerTransport_ClosedState_PassesRequestsThrough(t *testing.T) {
	base := &fakeRoundTripper{responses: []*http.Response{resp(200), resp(200)}}
	cfg := config.CircuitBreakerConfig{Enabled: true, MaxRequests: 1, Interval: 10, Timeout: 1}
	rt := NewBreakerTransport(base, "route-closed", cfg)

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	res, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.StatusCode)
	}
	if base.calls != 1 {
		t.Fatalf("expected backend to be called once, got %d", base.calls)
	}
}

func TestBreakerTransport_OpensAfterFailureThreshold(t *testing.T) {
	base := &fakeRoundTripper{
		responses: []*http.Response{resp(500), resp(500), resp(500), resp(200)},
	}
	cfg := config.CircuitBreakerConfig{Enabled: true, MaxRequests: 1, Interval: 10, Timeout: 30}
	rt := NewBreakerTransport(base, "route-opens", cfg)

	for i := 0; i < 3; i++ {
		req := httptest.NewRequest(http.MethodGet, "/x", nil)
		res, err := rt.RoundTrip(req)
		if err != nil {
			t.Fatalf("call %d: expected backend 500 to be forwarded without transport error, got %v", i, err)
		}
		if res == nil || res.StatusCode != http.StatusInternalServerError {
			t.Fatalf("call %d: expected 500 response to be returned, got %#v", i, res)
		}
	}

	callsBefore := base.calls
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	_, err := rt.RoundTrip(req)
	if !errors.Is(err, gobreaker.ErrOpenState) {
		t.Fatalf("expected gobreaker.ErrOpenState, got %v", err)
	}
	if base.calls != callsBefore {
		t.Fatalf("breaker is open but backend was still called (calls %d -> %d)", callsBefore, base.calls)
	}
}

func TestBreakerTransport_HalfOpen_AllowsProbeAfterTimeout(t *testing.T) {
	base := &fakeRoundTripper{
		responses: []*http.Response{resp(500), resp(500), resp(500), resp(200)},
	}
	cfg := config.CircuitBreakerConfig{Enabled: true, MaxRequests: 1, Interval: 10, Timeout: 1}
	rt := NewBreakerTransport(base, "route-halfopen", cfg)

	for i := 0; i < 3; i++ {
		rt.RoundTrip(httptest.NewRequest(http.MethodGet, "/x", nil))
	}
	if _, err := rt.RoundTrip(httptest.NewRequest(http.MethodGet, "/x", nil)); !errors.Is(err, gobreaker.ErrOpenState) {
		t.Fatalf("expected breaker open before timeout, got %v", err)
	}

	time.Sleep(1100 * time.Millisecond)

	res, err := rt.RoundTrip(httptest.NewRequest(http.MethodGet, "/x", nil))
	if err != nil {
		t.Fatalf("expected half-open probe to succeed, got err=%v", err)
	}
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 on successful probe, got %d", res.StatusCode)
	}
}

func TestBreakerTransport_NetworkError_CountsAsFailureAndPropagates(t *testing.T) {
	base := &fakeRoundTripper{errs: []error{errors.New("connection refused")}}
	cfg := config.CircuitBreakerConfig{Enabled: true, MaxRequests: 1, Interval: 10, Timeout: 30}
	rt := NewBreakerTransport(base, "route-neterr", cfg)

	_, err := rt.RoundTrip(httptest.NewRequest(http.MethodGet, "/x", nil))
	if err == nil || errors.Is(err, gobreaker.ErrOpenState) {
		t.Fatalf("expected the underlying network error to propagate, got %v", err)
	}
}
