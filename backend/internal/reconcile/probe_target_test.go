package reconcile

import (
	"context"
	"testing"

	"nas-home/backend/internal/links"
)

func TestProbeHostForLinkUsesExplicitBindIP(t *testing.T) {
	cases := []struct {
		name string
		link links.Link
		want string
	}{
		{name: "explicit", link: links.Link{HostIP: "192.168.1.11"}, want: "192.168.1.11"},
		{name: "wildcard", link: links.Link{HostIP: "0.0.0.0"}, want: "host.docker.internal"},
		{name: "empty", link: links.Link{}, want: "host.docker.internal"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := probeHostForLink(&tc.link); got != tc.want {
				t.Fatalf("probe host = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestProbeResolvedSkipsAccessibilityCheckForLocalhostMode(t *testing.T) {
	link := links.Link{URL: "http://localhost:4318", Source: links.SourcePublishedPort, HostPort: 4318}
	reconciler := &Reconciler{}
	record := reconciler.probeResolved(context.Background(), &link, true)
	if link.Reachability != links.ReachabilityNotChecked {
		t.Fatalf("link reachability = %q", link.Reachability)
	}
	if record.Status != "skipped (localhost)" || record.Error != "" {
		t.Fatalf("probe record = %#v", record)
	}
}
