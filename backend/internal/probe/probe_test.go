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
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`<html><head><title>需要登录</title><link rel="icon" href="/assets/app.svg"></head></html>`))
	}))
	defer server.Close()
	result := New(time.Second).Probe(context.Background(), server.URL)
	if !result.Reachable || result.StatusCode != http.StatusUnauthorized {
		t.Fatalf("probe = %#v", result)
	}
	if result.Title != "需要登录" {
		t.Fatalf("title = %q", result.Title)
	}
	if result.IconURL != server.URL+"/assets/app.svg" {
		t.Fatalf("icon = %q", result.IconURL)
	}
}

func TestProbeRejectsConnectionFailure(t *testing.T) {
	result := New(20*time.Millisecond).Probe(context.Background(), "http://127.0.0.1:1")
	if result.Reachable || result.Err == nil {
		t.Fatalf("expected failed probe, got %#v", result)
	}
}
