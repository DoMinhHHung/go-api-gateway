package middlewares

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAPIKeyAuth_ValidKey_Passes(t *testing.T) {
	handler := APIKeyAuth("correct-key")(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("X-API-Key", "correct-key")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestAPIKeyAuth_MissingKey_Rejected(t *testing.T) {
	handler := APIKeyAuth("correct-key")(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for missing key, got %d", rec.Code)
	}
}

func TestAPIKeyAuth_WrongKey_Rejected(t *testing.T) {
	handler := APIKeyAuth("correct-key")(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("X-API-Key", "wrong-key")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for wrong key, got %d", rec.Code)
	}
}

func TestAPIKeyAuth_WrongLengthKey_Rejected(t *testing.T) {
	handler := APIKeyAuth("a-fairly-long-correct-key")(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("X-API-Key", "short")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for length-mismatched key, got %d", rec.Code)
	}
}

func TestAPIKeyAuth_EmptyValidKey_AlwaysRejects(t *testing.T) {
	handler := APIKeyAuth("")(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("X-API-Key", "")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}
