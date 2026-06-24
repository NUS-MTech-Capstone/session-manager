package observability

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequestIDMiddlewareAndLogRequests(t *testing.T) {
	handler := RequestIDMiddleware(LogRequests(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if id := RequestID(r.Context()); id != "req-1" {
			t.Fatalf("RequestID() = %q", id)
		}
		w.WriteHeader(http.StatusCreated)
	})))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/sessions", nil)
	req.Header.Set("X-Request-ID", "req-1")

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated || rec.Header().Get("X-Request-ID") != "req-1" {
		t.Fatalf("status/header = %d/%q", rec.Code, rec.Header().Get("X-Request-ID"))
	}
}

func TestRequestIDMiddlewareGeneratesID(t *testing.T) {
	handler := RequestIDMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if RequestID(r.Context()) == "" {
			t.Fatal("RequestID() = empty")
		}
	}))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if rec.Header().Get("X-Request-ID") == "" {
		t.Fatal("X-Request-ID response header is empty")
	}
}
