package reconcile

import (
	"testing"

	dockerclient "nas-home/backend/internal/docker"
	"nas-home/backend/internal/links"
)

func TestDefaultServiceTypeUsesPublishedPorts(t *testing.T) {
	if got := defaultServiceType(nil); got != "backend" {
		t.Fatalf("without ports = %q", got)
	}
	if got := defaultServiceType([]dockerclient.PublishedPort{{HostPort: 8080}}); got != "frontend" {
		t.Fatalf("with port = %q", got)
	}
}

func TestChoosePrimaryUsesLowestResponsivePort(t *testing.T) {
	result := links.ResolveResult{Links: []links.Link{
		{URL: "http://nas:9000", HostPort: 9000, Source: links.SourcePublishedPort, Reachability: links.ReachabilityReachable, PageTitle: "Nine"},
		{URL: "http://nas:7000", HostPort: 7000, Source: links.SourcePublishedPort, Reachability: links.ReachabilityUnconfirmed},
		{URL: "http://nas:8000", HostPort: 8000, Source: links.SourcePublishedPort, Reachability: links.ReachabilityRespondingAuthenticated, PageTitle: "Eight"},
	}}
	choosePrimary(&result)
	if result.Primary == nil || result.Primary.HostPort != 8000 {
		t.Fatalf("primary = %#v", result.Primary)
	}
	if got := primaryPageTitle(result.Primary); got != "Eight" {
		t.Fatalf("page title = %q", got)
	}
}
