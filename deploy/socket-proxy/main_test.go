package main

import "testing"

func TestAllowedPathOnlyExposesReadEndpoints(t *testing.T) {
	allowed := []string{"/_ping", "/version", "/info", "/containers/json", "/containers/abc123/json"}
	for _, path := range allowed {
		if !allowedPath(path) {
			t.Errorf("allowedPath(%q) = false", path)
		}
	}
	denied := []string{"/containers/abc/start", "/containers/abc/exec", "/images/json", "/events", "/containers/../version"}
	for _, path := range denied {
		if allowedPath(path) {
			t.Errorf("allowedPath(%q) = true", path)
		}
	}
}
