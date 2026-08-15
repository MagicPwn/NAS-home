package docker

import "testing"

func TestCleanContainerNameAndStableKey(t *testing.T) {
	if got := CleanContainerName("/nas-home"); got != "nas-home" {
		t.Fatalf("CleanContainerName = %q", got)
	}
	if got := StableServiceKey(ContainerMetadata{Labels: map[string]string{"nas.home.key": "custom"}, Name: "container", ID: "abcdef"}); got != "custom" {
		t.Fatalf("label key priority = %q", got)
	}
	if got := StableServiceKey(ContainerMetadata{Labels: map[string]string{"com.docker.compose.project": "proj", "com.docker.compose.service": "web"}, Name: "container", ID: "abcdef"}); got != "proj/web" {
		t.Fatalf("compose key = %q", got)
	}
	if got := StableServiceKey(ContainerMetadata{Name: "container", ID: "abcdef123456"}); got != "container" {
		t.Fatalf("name key = %q", got)
	}
	if got := StableServiceKey(ContainerMetadata{ID: "abcdef123456789"}); got != "abcdef123456789" {
		t.Fatalf("id fallback = %q", got)
	}
}

func TestParsePublishedPortsUsesHostPortAndFiltersUnpublished(t *testing.T) {
	input := map[string][]PortBinding{
		"8080/tcp": {{HostIP: "0.0.0.0", HostPort: "18080"}, {HostIP: "::", HostPort: "18080"}},
		"5353/udp": {{HostIP: "0.0.0.0", HostPort: "15353"}},
		"9000/tcp": nil,
	}
	got, err := ParsePublishedPorts(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("expected one published TCP entry, got %#v", got)
	}
	if got[0].HostPort != 18080 || got[0].ContainerPort != 8080 || got[0].Protocol != "tcp" {
		t.Fatalf("host/container port parsing wrong: %#v", got[0])
	}
}

func TestEnabledLabel(t *testing.T) {
	if IsEnabled(map[string]string{"nas.home.enabled": "false"}) {
		t.Fatal("false label should hide service")
	}
	if IsEnabled(map[string]string{"nas.home.enabled": "TRUE"}) != true {
		t.Fatal("TRUE label should enable service")
	}
	if IsEnabled(map[string]string{}) != true {
		t.Fatal("missing label should be enabled")
	}
}
