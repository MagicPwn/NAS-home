package httpapi

import (
	"testing"
)

func TestReplacePublicHostOnlyRewritesConfiguredFrontendHost(t *testing.T) {
	if got := replacePublicHost("http://nas-host.example.invalid:4318/path", "nas-host.example.invalid", "192.168.1.50"); got != "http://192.168.1.50:4318/path" {
		t.Fatalf("rewritten URL = %q", got)
	}
	if got := replacePublicHost("http://nas-host.example.invalid:4318/path", "nas-host.example.invalid", "localhost"); got != "http://localhost:4318/path" {
		t.Fatalf("localhost URL = %q", got)
	}
	if got := replacePublicHost("http://other-host:4318", "nas-host.example.invalid", "192.168.1.50"); got != "http://other-host:4318" {
		t.Fatalf("unexpected rewrite = %q", got)
	}
}

func TestReplaceLinkHostRewritesCustomTabURL(t *testing.T) {
	cases := []struct {
		raw  string
		host string
		want string
	}{
		{raw: "http://192.168.1.12:8648/path", host: "localhost", want: "http://localhost:8648/path"},
		{raw: "https://example.com/app", host: "192.168.1.50", want: "https://192.168.1.50/app"},
	}
	for _, tc := range cases {
		if got := replaceLinkHost(tc.raw, tc.host); got != tc.want {
			t.Fatalf("replaceLinkHost(%q, %q) = %q, want %q", tc.raw, tc.host, got, tc.want)
		}
	}
}

func TestNormalizeMockHostAllowsLocalhostAndRejectsArbitraryHostname(t *testing.T) {
	if got, err := normalizeMockHost("LOCALHOST"); err != nil || got != "localhost" {
		t.Fatalf("localhost = %q, err=%v", got, err)
	}
	if got, err := normalizeMockHost("192.168.1.20"); err != nil || got != "192.168.1.20" {
		t.Fatalf("ip = %q, err=%v", got, err)
	}
	if _, err := normalizeMockHost("example.com"); err == nil {
		t.Fatal("arbitrary hostname accepted as mock host")
	}
}
