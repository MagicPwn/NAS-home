package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"
)

var containerID = regexp.MustCompile(`^/containers/[A-Za-z0-9][A-Za-z0-9_.-]*/json$`)

func allowedPath(path string) bool {
	if path == "/_ping" || path == "/version" || path == "/info" {
		return true
	}
	if path == "/containers/json" {
		return true
	}
	return containerID.MatchString(path)
}

func handler(socket string) http.Handler {
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, "unix", socket)
	}}
	client := &http.Client{Transport: transport, Timeout: 10 * time.Second}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || !allowedPath(r.URL.Path) {
			http.Error(w, "docker API endpoint is not allowed", http.StatusForbidden)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/containers/json") && r.URL.Query().Get("all") != "true" {
			r.URL.RawQuery = "all=true"
		}
		upstreamURL := "http://docker" + r.URL.RequestURI()
		request, err := http.NewRequestWithContext(r.Context(), http.MethodGet, upstreamURL, nil)
		if err != nil {
			http.Error(w, "bad request", 400)
			return
		}
		response, err := client.Do(request)
		if err != nil {
			http.Error(w, "docker unavailable", 503)
			return
		}
		defer response.Body.Close()
		for key, values := range response.Header {
			for _, value := range values {
				w.Header().Add(key, value)
			}
		}
		w.WriteHeader(response.StatusCode)
		_, _ = io.Copy(w, response.Body)
	})
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		client := &http.Client{Timeout: time.Second}
		response, err := client.Get("http://127.0.0.1:2375/_ping")
		if err != nil || response.StatusCode != http.StatusOK {
			os.Exit(1)
		}
		response.Body.Close()
		return
	}
	socket := os.Getenv("DOCKER_SOCKET")
	if socket == "" {
		socket = "/var/run/docker.sock"
	}
	server := &http.Server{Addr: ":2375", Handler: handler(socket), ReadHeaderTimeout: 2 * time.Second, IdleTimeout: 30 * time.Second}
	fmt.Printf("Docker read-only proxy listening on %s\n", server.Addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		os.Exit(1)
	}
}
