package reconcile

import (
	"context"
	"encoding/json"
	"net"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"nas-home/backend/internal/config"
	dockerclient "nas-home/backend/internal/docker"
	"nas-home/backend/internal/links"
	"nas-home/backend/internal/probe"
	"nas-home/backend/internal/store"
)

type cachedProbe struct {
	At           time.Time
	Status       string
	Reachability links.Reachability
	Error        string
}

type Reconciler struct {
	Config      config.Config
	Docker      *dockerclient.Client
	Store       *store.Store
	Prober      *probe.Prober
	mu          sync.Mutex
	probeCache  map[string]cachedProbe
	probeMu     sync.Mutex
	probeSem    chan struct{}
	LastSuccess time.Time
	LastError   string
}

type discoverResult struct {
	service store.Service
	ok      bool
}

func (r *Reconciler) Run(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	timeout := r.Config.ReconcileTimeout
	if timeout <= 0 {
		timeout = 8 * time.Second
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	containers, err := r.Docker.ListContainers(runCtx)
	if err != nil {
		r.LastError = err.Error()
		_ = r.Store.MarkStale(ctx, "Docker API unavailable")
		return err
	}
	if r.Prober == nil {
		r.Prober = probe.New(r.Config.ProbeTimeout)
	}
	if r.probeCache == nil {
		r.probeCache = make(map[string]cachedProbe)
	}
	if r.probeSem == nil {
		r.probeSem = make(chan struct{}, 8)
	}
	inspectSem := make(chan struct{}, 8)
	results := make(chan discoverResult, len(containers))
	for _, summary := range containers {
		go func(id string) {
			service, ok := r.discoverContainer(runCtx, id, inspectSem)
			results <- discoverResult{service: service, ok: ok}
		}(summary.ID)
	}
	services := make([]store.Service, 0, len(containers))
	complete := true
	for range containers {
		result := <-results
		if result.ok {
			services = append(services, result.service)
		} else {
			complete = false
		}
	}
	sort.Slice(services, func(i, j int) bool {
		if services[i].Group != services[j].Group {
			return services[i].Group < services[j].Group
		}
		if services[i].Order != services[j].Order {
			return services[i].Order < services[j].Order
		}
		return services[i].Name < services[j].Name
	})
	if err := r.Store.UpsertServices(ctx, services); err != nil {
		r.LastError = err.Error()
		return err
	}
	seen := make(map[string]bool, len(services))
	for _, service := range services {
		seen[service.ServiceKey] = true
	}
	if complete {
		if err := r.Store.PruneServices(ctx, keysOf(services)); err != nil {
			r.LastError = err.Error()
			return err
		}
		r.LastError = ""
	} else {
		r.LastError = "one or more container inspections failed"
		_ = r.Store.MarkUnseenStale(ctx, seen, "container inspection unavailable")
	}
	r.LastSuccess = time.Now().UTC()
	return nil
}

func nonNilPorts(ports []dockerclient.PublishedPort) []dockerclient.PublishedPort { if ports == nil { return []dockerclient.PublishedPort{} }; return ports }
func keysOf(services []store.Service) []string {
	keys := make([]string, len(services))
	for i := range services { keys[i] = services[i].ServiceKey }
	return keys
}

func (r *Reconciler) discoverContainer(ctx context.Context, id string, inspectSem chan struct{}) (store.Service, bool) {
	inspectSem <- struct{}{}
	inspect, err := r.Docker.InspectContainer(ctx, id)
	<-inspectSem
	if err != nil {
		return store.Service{}, false
	}
	labels := inspect.Config.Labels
	metadata := dockerclient.ContainerMetadata{ID: inspect.ID, Name: inspect.Name, Labels: labels}
	ports, err := dockerclient.ParsePublishedPorts(inspect.NetworkSettings.Ports)
	if err != nil {
		return store.Service{}, false
	}
	published := make([]links.PublishedPort, len(ports))
	for i, p := range ports {
		published[i] = links.PublishedPort{HostIP: p.HostIP, HostPort: p.HostPort, ContainerPort: p.ContainerPort, Protocol: p.Protocol}
	}
	resolved := links.Resolve(links.ResolveInput{PublicHost: r.Config.PublicHost, PublicScheme: r.Config.PublicScheme, PublishedPorts: published, LabelURL: labels["nas.home.url"]})
	probeRecords := make([]store.ProbeRecord, 0)
	for i := range resolved.Links {
		if resolved.Links[i].Source != links.SourcePublishedPort || resolved.Links[i].URL == "" || resolved.Links[i].LocalOnly {
			continue
		}
		record := r.probeResolved(ctx, &resolved.Links[i])
		probeRecords = append(probeRecords, record)
	}
	if resolved.Primary != nil {
		resolved.Reachability = resolved.Primary.Reachability
	}
	linksJSON, _ := json.Marshal(resolved.Links)
	portsJSON, _ := json.Marshal(nonNilPorts(ports))
	name := strings.TrimSpace(labels["nas.home.name"])
	if name == "" {
		name = dockerclient.CleanContainerName(inspect.Name)
	}
	order, _ := strconv.Atoi(strings.TrimSpace(labels["nas.home.order"]))
	serviceKey := dockerclient.StableServiceKey(metadata)
	service := store.Service{ServiceKey: serviceKey, Name: name, ContainerName: dockerclient.CleanContainerName(inspect.Name), Image: inspect.Config.Image, ComposeProject: labels["com.docker.compose.project"], ComposeService: labels["com.docker.compose.service"], ContainerState: normalizedState(inspect.State.Status, inspect.State.Restarting), Running: inspect.State.Running, Paused: inspect.State.Paused, Hidden: !dockerclient.IsEnabled(labels), Group: labels["nas.home.group"], Description: labels["nas.home.description"], Icon: labels["nas.home.icon"], Order: order, PrimaryURL: primaryURL(resolved.Primary), PrimaryLink: primaryPayload(resolved.Primary), Links: linksJSON, PublishedPorts: portsJSON, LinkSource: primarySource(resolved.Primary), Reachability: string(resolved.Reachability), LocalOnly: resolved.Primary != nil && resolved.Primary.LocalOnly, LastSeenAt: time.Now().UTC(), Stale: false}
	if previous, err := r.Store.GetService(ctx, serviceKey); err == nil {
		service.ProbeHistory = append(service.ProbeHistory, previous.ProbeHistory...)
		service.LastProbeAt = previous.LastProbeAt
		service.LastProbeStatus = previous.LastProbeStatus
		service.LastError = previous.LastError
	}
	for _, record := range probeRecords {
		service.ProbeHistory = appendProbe(service.ProbeHistory, record)
		at := record.At
		service.LastProbeAt = &at
		service.LastProbeStatus = record.Status
		if record.Error != "" {
			service.LastError = record.Error
		}
	}
	if override, err := r.Store.GetOverride(ctx, serviceKey); err == nil {
		service = r.applyOverride(service, override)
	}
	return service, true
}

func appendProbe(history []store.ProbeRecord, record store.ProbeRecord) []store.ProbeRecord {
	history = append(history, record)
	if len(history) > 3 {
		history = history[len(history)-3:]
	}
	return history
}

func (r *Reconciler) probeResolved(ctx context.Context, link *links.Link) store.ProbeRecord {
	record := store.ProbeRecord{URL: link.URL, At: time.Now().UTC(), Reachability: string(links.ReachabilityUnconfirmed)}
	r.probeMu.Lock()
	cached, ok := r.probeCache[link.URL]
	r.probeMu.Unlock()
	if ok && time.Since(cached.At) < 30*time.Second {
		link.Reachability = cached.Reachability
		record.Status, record.Reachability, record.Error = cached.Status, string(cached.Reachability), cached.Error
		return record
	}
	r.probeSem <- struct{}{}
	defer func() { <-r.probeSem }()
	parsed, err := url.Parse(link.URL)
	if err != nil || parsed.Port() == "" {
		record.Error = "invalid probe URL"
		return record
	}
	probeURL := *parsed
	probeURL.Host = net.JoinHostPort("host.docker.internal", parsed.Port())
	result := r.Prober.Probe(ctx, probeURL.String())
	if result.Err != nil && parsed.Scheme == "http" {
		probeURL.Scheme = "https"
		result = r.Prober.Probe(ctx, probeURL.String())
	}
	cached = cachedProbe{At: time.Now().UTC(), Status: result.Status, Reachability: links.ReachabilityUnconfirmed}
	if result.Reachable {
		switch {
		case result.StatusCode == 401 || result.StatusCode == 403:
			link.Reachability = links.ReachabilityRespondingAuthenticated
		case result.StatusCode >= 400:
			link.Reachability = links.ReachabilityRespondingError
		default:
			link.Reachability = links.ReachabilityReachable
		}
		cached.Reachability = link.Reachability
		record.Reachability = string(link.Reachability)
		record.Status = result.Status
	} else {
		record.Error = "probe failed"
		cached.Error = record.Error
		record.Status = "unconfirmed"
	}
	r.probeMu.Lock()
	r.probeCache[link.URL] = cached
	r.probeMu.Unlock()
	return record
}

func (r *Reconciler) ProbeService(ctx context.Context, key string) (store.Service, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.Prober == nil {
		r.Prober = probe.New(r.Config.ProbeTimeout)
	}
	if r.probeCache == nil {
		r.probeCache = make(map[string]cachedProbe)
	}
	if r.probeSem == nil {
		r.probeSem = make(chan struct{}, 8)
	}
	service, err := r.Store.GetService(ctx, key)
	if err != nil {
		return service, err
	}
	var serviceLinks []links.Link
	if len(service.Links) > 0 && string(service.Links) != "null" {
		if err := json.Unmarshal(service.Links, &serviceLinks); err != nil {
			return service, err
		}
	}
	for i := range serviceLinks {
		if serviceLinks[i].Source != links.SourcePublishedPort || serviceLinks[i].URL == "" || serviceLinks[i].LocalOnly {
			continue
		}
		r.probeMu.Lock()
		delete(r.probeCache, serviceLinks[i].URL)
		r.probeMu.Unlock()
		record := r.probeResolved(ctx, &serviceLinks[i])
		service.ProbeHistory = appendProbe(service.ProbeHistory, record)
		at := record.At
		service.LastProbeAt = &at
		service.LastProbeStatus = record.Status
		if record.Error != "" {
			service.LastError = record.Error
		}
	}
	service.Links, _ = json.Marshal(serviceLinks)
	if len(serviceLinks) > 0 && service.LinkSource == string(links.SourcePublishedPort) {
		service.PrimaryLink = primaryPayload(&serviceLinks[0])
		service.PrimaryURL = serviceLinks[0].URL
		service.Reachability = string(serviceLinks[0].Reachability)
	}
	service.Stale = false
	if err := r.Store.UpsertServices(ctx, []store.Service{service}); err != nil {
		return service, err
	}
	return service, nil
}

func (r *Reconciler) applyOverride(service store.Service, override store.Override) store.Service {
	if override.URL != nil {
		if parsed, err := links.ValidateExplicitURL(*override.URL); err == nil {
			service = r.Store.ApplyOverride(service, override)
			manual := links.Link{URL: parsed.String(), Label: "打开服务", Source: links.SourceManual, Reachability: links.ReachabilityReachable}
			service.Links, _ = json.Marshal([]links.Link{manual})
			service.PrimaryLink = primaryPayload(&manual)
			service.PrimaryURL = parsed.String()
			service.LinkSource = string(manual.Source)
			service.Reachability = string(manual.Reachability)
			service.LocalOnly = false
		} else {
			service.Reachability = string(links.ReachabilityInvalid)
			service.LastError = "invalid manual URL"
		}
		return service
	}
	return r.Store.ApplyOverride(service, override)
}
func normalizedState(state string, restarting bool) string {
	if restarting {
		return "restarting"
	}
	if state == "" {
		return "unknown"
	}
	return state
}
func primaryURL(l *links.Link) string {
	if l == nil {
		return ""
	}
	return l.URL
}
func primarySource(l *links.Link) string {
	if l == nil {
		return ""
	}
	return string(l.Source)
}
func primaryPayload(l *links.Link) map[string]any {
	if l == nil {
		return nil
	}
	return map[string]any{"url": l.URL, "label": l.Label, "source": l.Source, "reachability": l.Reachability, "localOnly": l.LocalOnly}
}
func (r *Reconciler) Loop(ctx context.Context) {
	ticker := time.NewTicker(r.Config.PollInterval)
	defer ticker.Stop()
	_ = r.Run(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = r.Run(ctx)
		}
	}
}
func (r *Reconciler) Status() (time.Time, string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.LastSuccess, r.LastError
}
