package observability

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHeadersMetricsAndErrorKinds(t *testing.T) {
	ctx := ContextWithRequestID(context.Background(), "req-1")
	req := httptest.NewRequest(http.MethodGet, "/sessions", nil)
	SetRequestHeaders(ctx, req)
	if req.Header.Get("x-api-version") != "1" || req.Header.Get("X-Request-ID") != "req-1" {
		t.Fatalf("headers = %#v", req.Header)
	}
	ConfigureLogger("session-manager")
	t.Setenv("CLOUDWATCH_NAMESPACE", "TestNamespace")
	EmitMetric("session-manager", nil, nil)
	EmitCount("session-manager", "Assignments", map[string]string{"Component": "store"})
	EmitMetric("session-manager", []Metric{{Name: "Latency", Unit: "Milliseconds", Value: 12}}, nil)
	if namespace() != "TestNamespace" {
		t.Fatalf("namespace() = %q", namespace())
	}
	t.Setenv("CLOUDWATCH_NAMESPACE", "")
	if namespace() != "DenDenMushi" {
		t.Fatalf("default namespace() = %q", namespace())
	}
	cases := []struct {
		err  error
		want string
	}{{nil, ""}, {context.DeadlineExceeded, "timeout"}, {context.Canceled, "cancelled"}, {errors.New("boom"), "error"}}
	for _, tc := range cases {
		if got := ErrorKind(tc.err); got != tc.want {
			t.Fatalf("ErrorKind(%v) = %q, want %q", tc.err, got, tc.want)
		}
	}
	if got := newRequestID(); len(got) != 32 {
		t.Fatalf("newRequestID() length = %d", len(got))
	}
}
