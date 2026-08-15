package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"

	"nas-home/backend/internal/links"
	"nas-home/backend/internal/reconcile"
	"nas-home/backend/internal/store"
)

type Server struct {
	Store      *store.Store
	Reconciler *reconcile.Reconciler
	StartedAt  time.Time
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", s.health)
	mux.HandleFunc("/api/v1/status", s.status)
	mux.HandleFunc("/api/v1/reconcile", s.reconcile)
	mux.HandleFunc("/api/v1/events/stream", s.eventsStream)
	mux.HandleFunc("/api/v1/services", s.services)
	mux.HandleFunc("/api/v1/services/", s.service)
	mux.Handle("/", http.FileServer(http.Dir("/app/frontend")))
	return mux
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "service": "nas-home"})
}
func (s *Server) status(w http.ResponseWriter, _ *http.Request) {
	last, errText := s.Reconciler.Status()
	writeJSON(w, http.StatusOK, map[string]any{"docker": map[string]any{"available": !last.IsZero() && errText == "", "lastError": errText}, "lastReconcileAt": last, "stale": last.IsZero() || errText != "" || time.Since(last) > 30*time.Second, "version": "0.1.0"})
}
func (s *Server) eventsStream(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "stream unsupported", 500)
		return
	}
	_, _ = w.Write([]byte("event: ready\ndata: {}\n\n"))
	flusher.Flush()
	<-r.Context().Done()
}

func (s *Server) reconcile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	if err := s.Reconciler.Run(r.Context()); err != nil {
		writeJSON(w, 503, map[string]string{"error": "reconcile failed"})
		return
	}
	writeJSON(w, 202, map[string]any{"ok": true})
}
func (s *Server) services(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", 405)
		return
	}
	services, err := s.Store.ListServices(r.Context())
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "store unavailable"})
		return
	}
	query := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	group := r.URL.Query().Get("group")
	state := r.URL.Query().Get("state")
	includeHidden := r.URL.Query().Get("include_hidden") == "true"
	reachableFilter := r.URL.Query().Get("reachable")
	sortBy := r.URL.Query().Get("sort")
	result := services[:0]
	for _, service := range services {
		haystack := strings.ToLower(strings.Join([]string{service.Name, service.ContainerName, service.Image, service.ComposeService}, " "))
		if query != "" && !strings.Contains(haystack, query) {
			continue
		}
		if group != "" && service.Group != group {
			continue
		}
		if !includeHidden && service.Hidden {
			continue
		}
		if state == "running" && !service.Running {
			continue
		}
		if state == "stopped" && service.Running {
			continue
		}
		isReachable := service.Reachability == "reachable" || service.Reachability == "responding-authenticated" || service.Reachability == "responding-error"
		if reachableFilter == "true" && !isReachable {
			continue
		}
		if reachableFilter == "false" && isReachable {
			continue
		}
		result = append(result, service)
	}
	sort.SliceStable(result, func(i, j int) bool {
		switch sortBy {
		case "name":
			return result[i].Name < result[j].Name
		case "last_seen":
			return result[i].LastSeenAt.Before(result[j].LastSeenAt)
		case "order":
			if result[i].Order != result[j].Order {
				return result[i].Order < result[j].Order
			}
		case "group":
			if result[i].Group != result[j].Group {
				return result[i].Group < result[j].Group
			}
		}
		if result[i].Group != result[j].Group {
			return result[i].Group < result[j].Group
		}
		if result[i].Order != result[j].Order {
			return result[i].Order < result[j].Order
		}
		return result[i].Name < result[j].Name
	})
	writeJSON(w, 200, map[string]any{"services": result, "count": len(result)})
}
func (s *Server) service(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimPrefix(r.URL.Path, "/api/v1/services/")
	if key == "" {
		http.NotFound(w, r)
		return
	}
	if strings.HasSuffix(key, "/probe") {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", 405)
			return
		}
		serviceKey := strings.TrimSuffix(key, "/probe")
		service, err := s.Reconciler.ProbeService(r.Context(), serviceKey)
		if store.IsNotFound(err) {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": "probe failed"})
			return
		}
		writeJSON(w, 200, service)
		return
	}
	if strings.HasSuffix(key, "/override") {
		s.override(w, r, strings.TrimSuffix(key, "/override"))
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", 405)
		return
	}
	service, err := s.Store.GetService(r.Context(), key)
	if errors.Is(err, context.Canceled) {
		return
	}
	if err != nil {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, 200, service)
}
func (s *Server) override(w http.ResponseWriter, r *http.Request, key string) {
	if key == "" {
		http.NotFound(w, r)
		return
	}
	switch r.Method {
	case http.MethodDelete:
		if err := s.Store.DeleteOverride(r.Context(), key); err != nil {
			writeJSON(w, 500, map[string]string{"error": "override delete failed"})
			return
		}
	case http.MethodPatch:
		var incoming store.Override
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&incoming); err != nil {
			writeJSON(w, 400, map[string]string{"error": "invalid override payload"})
			return
		}
		if incoming.URL != nil {
			if _, err := links.ValidateExplicitURL(*incoming.URL); err != nil {
				writeJSON(w, 400, map[string]string{"error": "invalid URL"})
				return
			}
		}
		current, err := s.Store.GetOverride(r.Context(), key)
		if err != nil && !store.IsNotFound(err) {
			writeJSON(w, 500, map[string]string{"error": "override read failed"})
			return
		}
		mergeOverride(&current, incoming)
		if err := s.Store.SaveOverride(r.Context(), key, current); err != nil {
			writeJSON(w, 500, map[string]string{"error": "override save failed"})
			return
		}
	default:
		http.Error(w, "method not allowed", 405)
		return
	}
	_ = s.Reconciler.Run(r.Context())
	service, err := s.Store.GetService(r.Context(), key)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, 200, service)
}
func mergeOverride(dst *store.Override, src store.Override) {
	if src.Name != nil {
		dst.Name = src.Name
	}
	if src.Group != nil {
		dst.Group = src.Group
	}
	if src.Description != nil {
		dst.Description = src.Description
	}
	if src.Icon != nil {
		dst.Icon = src.Icon
	}
	if src.URL != nil {
		dst.URL = src.URL
	}
	if src.Hidden != nil {
		dst.Hidden = src.Hidden
	}
	if src.SortOrder != nil {
		dst.SortOrder = src.SortOrder
	}
}
