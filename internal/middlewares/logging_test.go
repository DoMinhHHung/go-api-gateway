package middlewares

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLogging_PassesThroughRequest(t *testing.T) {
	hit := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true
		w.WriteHeader(http.StatusCreated)
	})

	handler := Logging()(inner)

	req := httptest.NewRequest(http.MethodPost, "/x", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !hit {
		t.Fatal("inner handler was not called")
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 to pass through untouched, got %d", rec.Code)
	}
}

func TestResponseWriter_DefaultsTo200WhenWriteHeaderNeverCalled(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})

	rw := wrapResponseWriter(httptest.NewRecorder())
	inner.ServeHTTP(rw, httptest.NewRequest(http.MethodGet, "/x", nil))

	if rw.status != http.StatusOK {
		t.Fatalf("expected default status 200, got %d", rw.status)
	}
}

func TestResponseWriter_CapturesExplicitStatus(t *testing.T) {
	rw := wrapResponseWriter(httptest.NewRecorder())
	rw.WriteHeader(http.StatusTeapot)

	if rw.status != http.StatusTeapot {
		t.Fatalf("expected 418 captured, got %d", rw.status)
	}
}

func TestResponseWriter_IgnoresDuplicateWriteHeaderCalls(t *testing.T) {
	rw := wrapResponseWriter(httptest.NewRecorder())
	rw.WriteHeader(http.StatusOK)
	rw.WriteHeader(http.StatusInternalServerError)

	if rw.status != http.StatusOK {
		t.Fatalf("expected first WriteHeader (200) to win, got %d", rw.status)
	}
}
