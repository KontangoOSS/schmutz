package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"git.konoss.org/kore/schmutz/neverland/internal/middleware"
)

func TestRateLimit_AllowsBurst(t *testing.T) {
	mw := middleware.RateLimitPerIP(10, time.Minute)
	final := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(204)
	}))

	for i := 0; i < 10; i++ {
		req := httptest.NewRequest("GET", "/x", nil)
		req.RemoteAddr = "10.0.0.1:1234"
		w := httptest.NewRecorder()
		final.ServeHTTP(w, req)
		if w.Code != 204 {
			t.Fatalf("burst attempt %d: expected 204, got %d", i, w.Code)
		}
	}
}

func TestRateLimit_BlocksAfterBurst(t *testing.T) {
	mw := middleware.RateLimitPerIP(2, time.Minute)
	final := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(204)
	}))

	mk := func() *http.Request {
		r := httptest.NewRequest("GET", "/x", nil)
		r.RemoteAddr = "10.0.0.2:1234"
		return r
	}

	w1 := httptest.NewRecorder(); final.ServeHTTP(w1, mk())
	w2 := httptest.NewRecorder(); final.ServeHTTP(w2, mk())
	w3 := httptest.NewRecorder(); final.ServeHTTP(w3, mk())

	if w1.Code != 204 || w2.Code != 204 {
		t.Fatalf("first two should pass: %d %d", w1.Code, w2.Code)
	}
	if w3.Code != 429 {
		t.Fatalf("third should be 429, got %d", w3.Code)
	}
}

func TestRateLimit_PerIPSeparate(t *testing.T) {
	mw := middleware.RateLimitPerIP(1, time.Minute)
	final := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) }))

	for _, ip := range []string{"10.0.0.10:1", "10.0.0.11:1", "10.0.0.12:1"} {
		r := httptest.NewRequest("GET", "/x", nil)
		r.RemoteAddr = ip
		w := httptest.NewRecorder()
		final.ServeHTTP(w, r)
		if w.Code != 204 {
			t.Fatalf("ip %s: expected 204, got %d", ip, w.Code)
		}
	}
}
