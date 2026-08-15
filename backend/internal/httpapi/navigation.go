package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"nas-home/backend/internal/links"
	"nas-home/backend/internal/probe"
	"nas-home/backend/internal/store"
)

type customTabInput struct {
	Name      string `json:"name"`
	SortOrder int    `json:"sortOrder"`
}

type customLinkInput struct {
	TabID       string `json:"tabId"`
	URL         string `json:"url"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Icon        string `json:"icon"`
	SortOrder   int    `json:"sortOrder"`
}

type settingsInput struct {
	TabID  string `json:"tabId"`
	MockIP string `json:"mockIp"`
}

func (s *Server) navigation(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	settings, err := s.Store.GetSettings(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "settings unavailable"})
		return
	}
	tabs, err := s.Store.ListCustomTabs(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "custom tabs unavailable"})
		return
	}
	for i := range tabs {
		tabs[i] = s.decorateCustomTab(r.Context(), tabs[i])
	}
	writeJSON(w, http.StatusOK, map[string]any{"settings": settings, "customTabs": tabs})
}

func (s *Server) settings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		if r.Method == http.MethodGet {
			settings, err := s.Store.GetSettings(r.Context())
			if err != nil {
				writeJSON(w, 500, map[string]string{"error": "settings unavailable"})
				return
			}
			writeJSON(w, 200, settings)
			return
		}
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var input settingsInput
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid settings payload"})
		return
	}
	input.TabID = strings.TrimSpace(input.TabID)
	if !isBuiltInTab(input.TabID) {
		if _, err := s.Store.GetCustomTab(r.Context(), input.TabID); store.IsNotFound(err) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "tabId does not exist"})
			return
		} else if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "tab lookup failed"})
			return
		}
	}
	mockIP, err := normalizeMockHost(input.MockIP)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := s.Store.SetTabMockIP(r.Context(), input.TabID, mockIP); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "settings save failed"})
		return
	}
	settings, err := s.Store.GetSettings(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "settings unavailable"})
		return
	}
	writeJSON(w, http.StatusOK, settings)
}

func (s *Server) customTabs(w http.ResponseWriter, r *http.Request) {
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/custom-tabs"), "/")
	parts := []string{}
	if path != "" {
		parts = strings.Split(path, "/")
	}
	if len(parts) == 0 {
		s.customTabsCollection(w, r)
		return
	}
	if len(parts) == 2 && parts[1] == "links" && r.Method == http.MethodPost {
		s.createCustomLink(w, r, parts[0])
		return
	}
	if len(parts) == 1 {
		s.customTabItem(w, r, parts[0])
		return
	}
	http.NotFound(w, r)
}

func (s *Server) customTabsCollection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		tabs, err := s.Store.ListCustomTabs(r.Context())
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": "custom tabs unavailable"})
			return
		}
		writeJSON(w, 200, map[string]any{"tabs": tabs})
	case http.MethodPost:
		var input customTabInput
		if err := decodeJSON(r, &input); err != nil {
			writeJSON(w, 400, map[string]string{"error": "invalid tab payload"})
			return
		}
		input.Name = strings.TrimSpace(input.Name)
		if input.Name == "" || len([]rune(input.Name)) > 80 {
			writeJSON(w, 400, map[string]string{"error": "tab name is required and must be at most 80 characters"})
			return
		}
		tab, err := s.Store.CreateCustomTab(r.Context(), input.Name, input.SortOrder)
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": "custom tab create failed"})
			return
		}
		writeJSON(w, 201, tab)
	default:
		http.Error(w, "method not allowed", 405)
	}
}

func (s *Server) customTabItem(w http.ResponseWriter, r *http.Request, id string) {
	switch r.Method {
	case http.MethodPatch:
		var input customTabInput
		if err := decodeJSON(r, &input); err != nil {
			writeJSON(w, 400, map[string]string{"error": "invalid tab payload"})
			return
		}
		input.Name = strings.TrimSpace(input.Name)
		if input.Name == "" || len([]rune(input.Name)) > 80 {
			writeJSON(w, 400, map[string]string{"error": "tab name is required and must be at most 80 characters"})
			return
		}
		tab, err := s.Store.UpdateCustomTab(r.Context(), id, input.Name, input.SortOrder)
		if store.IsNotFound(err) {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": "custom tab update failed"})
			return
		}
		writeJSON(w, 200, tab)
	case http.MethodDelete:
		err := s.Store.DeleteCustomTab(r.Context(), id)
		if store.IsNotFound(err) {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": "custom tab delete failed"})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "method not allowed", 405)
	}
}

func (s *Server) createCustomLink(w http.ResponseWriter, r *http.Request, tabID string) {
	if _, err := s.Store.GetCustomTab(r.Context(), tabID); store.IsNotFound(err) {
		http.NotFound(w, r)
		return
	} else if err != nil {
		writeJSON(w, 500, map[string]string{"error": "custom tab lookup failed"})
		return
	}
	var input customLinkInput
	if err := decodeJSON(r, &input); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid link payload"})
		return
	}
	input.TabID = tabID
	link, err := s.buildCustomLink(r.Context(), input, "")
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	created, err := s.Store.CreateCustomLink(r.Context(), link)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "custom link create failed"})
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) customLinks(w http.ResponseWriter, r *http.Request) {
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/custom-links"), "/")
	parts := []string{}
	if path != "" {
		parts = strings.Split(path, "/")
	}
	if len(parts) != 1 && !(len(parts) == 2 && parts[1] == "probe") {
		http.NotFound(w, r)
		return
	}
	id := parts[0]
	if len(parts) == 2 {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", 405)
			return
		}
		link, err := s.Store.GetCustomLink(r.Context(), id)
		if store.IsNotFound(err) {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": "custom link lookup failed"})
			return
		}
		link = s.probeCustomLink(r.Context(), link)
		updated, err := s.Store.UpdateCustomLink(r.Context(), link)
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": "custom link probe save failed"})
			return
		}
		writeJSON(w, 200, updated)
		return
	}
	switch r.Method {
	case http.MethodGet:
		link, err := s.Store.GetCustomLink(r.Context(), id)
		if store.IsNotFound(err) {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": "custom link lookup failed"})
			return
		}
		writeJSON(w, 200, s.decorateCustomLink(r.Context(), link))
	case http.MethodPatch:
		var input customLinkInput
		if err := decodeJSON(r, &input); err != nil {
			writeJSON(w, 400, map[string]string{"error": "invalid link payload"})
			return
		}
		old, err := s.Store.GetCustomLink(r.Context(), id)
		if store.IsNotFound(err) {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": "custom link lookup failed"})
			return
		}
		if input.TabID == "" {
			input.TabID = old.TabID
		}
		link, err := s.buildCustomLink(r.Context(), input, id)
		if err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		updated, err := s.Store.UpdateCustomLink(r.Context(), link)
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": "custom link update failed"})
			return
		}
		writeJSON(w, 200, s.decorateCustomLink(r.Context(), updated))
	case http.MethodDelete:
		err := s.Store.DeleteCustomLink(r.Context(), id)
		if store.IsNotFound(err) {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": "custom link delete failed"})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "method not allowed", 405)
	}
}

func (s *Server) buildCustomLink(ctx context.Context, input customLinkInput, id string) (store.CustomLink, error) {
	rawURL := strings.TrimSpace(input.URL)
	parsed, err := links.ValidateExplicitURL(rawURL)
	if err != nil {
		return store.CustomLink{}, err
	}
	if err := probe.ValidateExternalURL(rawURL); err != nil && !errors.Is(err, probe.ErrBlockedTarget) {
		return store.CustomLink{}, err
	}
	if len([]rune(input.Name)) > 120 || len([]rune(input.Description)) > 500 || len([]rune(input.Icon)) > 80 {
		return store.CustomLink{}, contextError("link text is too long")
	}
	link := store.CustomLink{ID: id, TabID: input.TabID, URL: parsed.String(), Name: strings.TrimSpace(input.Name), Description: strings.TrimSpace(input.Description), Icon: strings.TrimSpace(input.Icon), SortOrder: input.SortOrder, Reachability: "unconfirmed"}
	return s.probeCustomLink(ctx, link), nil
}

func (s *Server) probeCustomLink(ctx context.Context, link store.CustomLink) store.CustomLink {
	if s.tabSkipsProbe(ctx, link.TabID) {
		link.Reachability = "not-checked"
		link.LastProbeAt = timePtr(time.Now().UTC())
		link.LastProbeStatus = "skipped (localhost)"
		link.LastError = ""
		if link.Name == "" {
			link.Name = link.URL
		}
		return link
	}
	result := probe.NewExternal(3*time.Second).Probe(ctx, link.URL)
	link.LastProbeAt = timePtr(time.Now().UTC())
	link.LastProbeStatus = result.Status
	link.LastError = ""
	if result.Reachable {
		switch {
		case result.StatusCode == http.StatusUnauthorized || result.StatusCode == http.StatusForbidden:
			link.Reachability = "responding-authenticated"
		case result.StatusCode >= 400:
			link.Reachability = "responding-error"
		default:
			link.Reachability = "reachable"
		}
		link.PageTitle = result.Title
		link.IconURL = result.IconURL
		if link.Name == "" {
			link.Name = result.Title
		}
		if link.Name == "" {
			link.Name = link.URL
		}
		return link
	}
	link.Reachability = "unconfirmed"
	if errors.Is(result.Err, probe.ErrBlockedTarget) {
		link.LastProbeStatus = "blocked"
		link.LastError = "server-side probe blocked by network safety policy"
	} else {
		link.LastProbeStatus = "unconfirmed"
		link.LastError = "probe failed"
	}
	if link.Name == "" {
		link.Name = link.URL
	}
	return link
}

func isBuiltInTab(tabID string) bool {
	return tabID == "frontend" || tabID == "backend"
}

func normalizeMockHost(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", nil
	}
	if strings.EqualFold(value, "localhost") {
		return "localhost", nil
	}
	if net.ParseIP(value) == nil {
		return "", errors.New("mockIp must be an IPv4/IPv6 address or localhost")
	}
	return value, nil
}

func isLocalhostMockHost(value string) bool {
	return strings.EqualFold(strings.TrimSpace(value), "localhost")
}

func (s *Server) tabSkipsProbe(ctx context.Context, tabID string) bool {
	mockIP, err := s.Store.GetTabMockIP(ctx, tabID)
	return err == nil && isLocalhostMockHost(mockIP)
}

func (s *Server) decorateCustomTab(ctx context.Context, tab store.CustomTab) store.CustomTab {
	for i := range tab.Links {
		tab.Links[i] = s.decorateCustomLink(ctx, tab.Links[i])
	}
	return tab
}

func (s *Server) decorateCustomLink(ctx context.Context, link store.CustomLink) store.CustomLink {
	mockIP, err := s.Store.GetTabMockIP(ctx, link.TabID)
	if err != nil || strings.TrimSpace(mockIP) == "" {
		return link
	}
	link.OriginalURL = link.URL
	link.URL = replaceLinkHost(link.URL, mockIP)
	if isLocalhostMockHost(mockIP) {
		link.Reachability = string(links.ReachabilityNotChecked)
		link.LastProbeStatus = "skipped (localhost)"
		link.LastError = ""
	}
	return link
}

func decodeJSON(r *http.Request, target any) error {
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}

func contextError(message string) error { return &validationError{message: message} }

type validationError struct{ message string }

func (e *validationError) Error() string { return e.message }
func timePtr(value time.Time) *time.Time { return &value }

func (s *Server) decorateService(service store.Service) store.Service {
	mockIP, err := s.Store.GetTabMockIP(context.Background(), service.ServiceType)
	if err != nil || mockIP == "" {
		return service
	}
	publicHost := s.Reconciler.Config.PublicHost
	localhostMode := isLocalhostMockHost(mockIP)
	var serviceLinks []links.Link
	if len(service.Links) > 0 && string(service.Links) != "null" && json.Unmarshal(service.Links, &serviceLinks) == nil {
		for i := range serviceLinks {
			if serviceLinks[i].Source == links.SourcePublishedPort {
				serviceLinks[i].URL = replacePublicHost(serviceLinks[i].URL, publicHost, mockIP)
				if localhostMode {
					serviceLinks[i].Reachability = links.ReachabilityNotChecked
				}
			}
		}
		service.Links, _ = json.Marshal(serviceLinks)
	}
	service.PrimaryURL = replacePublicHost(service.PrimaryURL, publicHost, mockIP)
	if localhostMode {
		service.Reachability = string(links.ReachabilityNotChecked)
		service.LastError = ""
	}
	if service.PrimaryLink != nil {
		if raw, ok := service.PrimaryLink["url"].(string); ok {
			service.PrimaryLink["url"] = replacePublicHost(raw, publicHost, mockIP)
		}
		if localhostMode {
			service.PrimaryLink["reachability"] = links.ReachabilityNotChecked
		}
	}
	return service
}

func replacePublicHost(raw, publicHost, mockIP string) string {
	if raw == "" || publicHost == "" {
		return raw
	}
	u, err := url.Parse(raw)
	if err != nil || !strings.EqualFold(u.Hostname(), publicHost) {
		return raw
	}
	port := u.Port()
	if port == "" {
		u.Host = mockIP
	} else {
		u.Host = net.JoinHostPort(mockIP, port)
	}
	return u.String()
}

func replaceLinkHost(raw, host string) string {
	if raw == "" || strings.TrimSpace(host) == "" {
		return raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return raw
	}
	if port := u.Port(); port != "" {
		u.Host = net.JoinHostPort(host, port)
	} else {
		u.Host = host
	}
	return u.String()
}
