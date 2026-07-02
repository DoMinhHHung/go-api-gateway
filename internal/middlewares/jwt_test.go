package middlewares

import (
	"crypto/rand"
	"crypto/rsa"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func genKeyPair(t *testing.T) (*rsa.PrivateKey, *rsa.PublicKey) {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("gen key: %v", err)
	}
	return priv, &priv.PublicKey
}

func signRS256(t *testing.T, priv *rsa.PrivateKey, claims jwt.MapClaims) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	s, err := tok.SignedString(priv)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return s
}

func newTestHandler(hit *bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*hit = true
		w.WriteHeader(http.StatusOK)
	})
}

func TestJWTAuth_ValidToken_Passes(t *testing.T) {
	priv, pub := genKeyPair(t)
	token := signRS256(t, priv, jwt.MapClaims{
		"user_id": "u123",
		"role":    "admin",
		"exp":     time.Now().Add(time.Hour).Unix(),
	})

	hit := false
	var capturedReq *http.Request
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true
		capturedReq = r
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	JWTAuth(pub)(inner).ServeHTTP(rec, req)

	if !hit || rec.Code != http.StatusOK {
		t.Fatalf("expected 200 and handler hit, got %d hit=%v", rec.Code, hit)
	}
	if capturedReq.Header.Get("X-User-Id") != "u123" {
		t.Fatalf("expected X-User-Id=u123, got %q", capturedReq.Header.Get("X-User-Id"))
	}
}

// TestJWTAuth_RejectsAlgConfusion is the critical security test: an attacker
// who knows the RSA *public* key can forge an HS256 token by using the
// public key bytes as the HMAC secret. jwt.Parse's keyfunc MUST check
// token.Method and reject anything that isn't RS256, otherwise the library
// will happily "verify" the forged token against the public key.
func TestJWTAuth_RejectsAlgConfusion(t *testing.T) {
	_, pub := genKeyPair(t)

	claims := jwt.MapClaims{"user_id": "attacker", "role": "admin"}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	forged, err := tok.SignedString([]byte("some-guessable-or-known-string"))
	if err != nil {
		t.Fatalf("sign forged: %v", err)
	}

	hit := false
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("Authorization", "Bearer "+forged)
	rec := httptest.NewRecorder()

	JWTAuth(pub)(newTestHandler(&hit)).ServeHTTP(rec, req)

	if hit || rec.Code != http.StatusUnauthorized {
		t.Fatalf("alg-confusion token must be rejected, got status=%d hit=%v", rec.Code, hit)
	}
}

func TestJWTAuth_StripsClientSuppliedIdentityHeaders(t *testing.T) {
	priv, pub := genKeyPair(t)
	token := signRS256(t, priv, jwt.MapClaims{
		"user_id": "real-user",
		"exp":     time.Now().Add(time.Hour).Unix(),
	})

	var capturedReq *http.Request
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedReq = r
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-User-Id", "spoofed-admin") // attacker-supplied, must be overwritten
	rec := httptest.NewRecorder()

	JWTAuth(pub)(inner).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d — token setup is broken, check exp claim", rec.Code)
	}
	if got := capturedReq.Header.Get("X-User-Id"); got != "real-user" {
		t.Fatalf("expected spoofed header to be overwritten with token claim, got %q", got)
	}
}

func TestJWTAuth_MissingHeader_Rejected(t *testing.T) {
	_, pub := genKeyPair(t)
	hit := false
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	rec := httptest.NewRecorder()

	JWTAuth(pub)(newTestHandler(&hit)).ServeHTTP(rec, req)

	if hit || rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 with no handler hit, got %d hit=%v", rec.Code, hit)
	}
}

// TestJWTAuth_RejectsTokenWithoutExpClaim guards the jwt.WithExpirationRequired()
// fix. jwt/v5 only validates exp *if present* — without this option a token
// with no exp claim never expires.
func TestJWTAuth_RejectsTokenWithoutExpClaim(t *testing.T) {
	priv, pub := genKeyPair(t)
	// no "exp" key at all
	token := signRS256(t, priv, jwt.MapClaims{"user_id": "u123"})

	hit := false
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	JWTAuth(pub)(newTestHandler(&hit)).ServeHTTP(rec, req)

	if hit || rec.Code != http.StatusUnauthorized {
		t.Fatalf("token without exp must be rejected, got status=%d hit=%v", rec.Code, hit)
	}
}
