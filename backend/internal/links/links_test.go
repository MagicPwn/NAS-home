package links

import "testing"

func TestClassifyBindAddress(t *testing.T) {
	tests := []struct {
		name string
		ip   string
		want BindClassification
	}{
		{"wildcard ipv4", "0.0.0.0", BindWildcard},
		{"wildcard ipv6", "::", BindWildcard},
		{"empty", "", BindWildcard},
		{"local ipv4", "127.0.0.1", BindLocalOnly},
		{"local ipv6", "::1", BindLocalOnly},
		{"explicit", "192.168.1.20", BindExplicit},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ClassifyBindAddress(tt.ip); got != tt.want {
				t.Fatalf("ClassifyBindAddress(%q) = %q, want %q", tt.ip, got, tt.want)
			}
		})
	}
}

func TestValidateExplicitURL(t *testing.T) {
	valid := []string{
		"http://nas.example:8080/app?tab=home",
		"https://nas.example/path",
	}
	for _, raw := range valid {
		if _, err := ValidateExplicitURL(raw); err != nil {
			t.Errorf("ValidateExplicitURL(%q) unexpected error: %v", raw, err)
		}
	}
	invalid := []string{
		"javascript:alert(1)",
		"http://user:pass@nas.example/",
		"https://nas.example/#token",
		"file:///etc/passwd",
		"http://",
	}
	for _, raw := range invalid {
		if _, err := ValidateExplicitURL(raw); err == nil {
			t.Errorf("ValidateExplicitURL(%q) expected error", raw)
		}
	}
}

func TestResolveLinkPriorityAndPublishedPortRules(t *testing.T) {
	ports := []PublishedPort{
		{HostIP: "0.0.0.0", HostPort: 18080, ContainerPort: 8080, Protocol: "tcp"},
		{HostIP: "127.0.0.1", HostPort: 18081, ContainerPort: 8081, Protocol: "tcp"},
		{HostIP: "", HostPort: 18082, ContainerPort: 8082, Protocol: "udp"},
		{HostIP: "", HostPort: 0, ContainerPort: 8083, Protocol: "tcp"},
	}
	got := Resolve(ResolveInput{
		PublicHost:     "nas.example",
		PublicScheme:   "http",
		PublishedPorts: ports,
		LabelURL:       "http://nas.example:18080/app",
	})
	if got.Primary == nil || got.Primary.URL != "http://nas.example:18080/app" || got.Primary.Source != SourceLabel {
		t.Fatalf("label URL should win, got %#v", got.Primary)
	}
	if len(got.Links) != 2 {
		t.Fatalf("expected 2 TCP published links, got %d", len(got.Links))
	}
	if got.Links[1].LocalOnly != true || got.Links[1].Reachability != ReachabilityLocalOnly {
		t.Fatalf("local-only port was not classified: %#v", got.Links[1])
	}
}

func TestResolveDoesNotInventLinkForUnpublishedPort(t *testing.T) {
	got := Resolve(ResolveInput{PublicHost: "nas.example", PublicScheme: "http", PublishedPorts: []PublishedPort{{ContainerPort: 8080, Protocol: "tcp"}}})
	if got.Primary != nil || got.Reachability != ReachabilityNotPublished {
		t.Fatalf("expected no link for unpublished port, got %#v", got)
	}
}

func TestInvalidLabelURLDoesNotFallBackToAutomaticLink(t *testing.T) {
	got := Resolve(ResolveInput{PublicHost: "nas.example", PublicScheme: "http", LabelURL: "javascript:alert(1)", PublishedPorts: []PublishedPort{{HostPort: 18080, ContainerPort: 8080, Protocol: "tcp"}}})
	if got.Primary != nil || got.Reachability != ReachabilityInvalid {
		t.Fatalf("invalid label should block primary link, got %#v", got)
	}
}
