package probe

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestProbeFallsBackFromHead405AndTreatsAuthAsResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()
	result := New(time.Second).Probe(context.Background(), server.URL)
	if !result.Reachable || result.StatusCode != http.StatusUnauthorized {
		t.Fatalf("probe = %#v", result)
	}
}

func TestProbeRejectsConnectionFailure(t *testing.T) {
	result := New(20*time.Millisecond).Probe(context.Background(), "http://127.0.0.1:1")
	if result.Reachable || result.Err == nil {
		t.Fatalf("expected failed probe, got %#v", result)
	}
}
