package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type ProbeRecord struct {
	URL          string    `json:"url"`
	At           time.Time `json:"at"`
	Status       string    `json:"status,omitempty"`
	Title        string    `json:"title,omitempty"`
	IconURL      string    `json:"iconUrl,omitempty"`
	Reachability string    `json:"reachability"`
	Error        string    `json:"error,omitempty"`
}

type Service struct {
	ServiceKey      string          `json:"serviceKey"`
	Name            string          `json:"name"`
	ContainerName   string          `json:"containerName"`
	Image           string          `json:"image"`
	ComposeProject  string          `json:"composeProject,omitempty"`
	ComposeService  string          `json:"composeService,omitempty"`
	ContainerState  string          `json:"containerState"`
	Running         bool            `json:"running"`
	Paused          bool            `json:"paused"`
	Hidden          bool            `json:"hidden"`
	Group           string          `json:"group"`
	Description     string          `json:"description"`
	Icon            string          `json:"icon"`
	IconURL         string          `json:"iconUrl,omitempty"`
	Order           int             `json:"order"`
	ServiceType     string          `json:"serviceType"`
	PageTitle       string          `json:"pageTitle,omitempty"`
	PrimaryURL      string          `json:"primaryUrl,omitempty"`
	PrimaryLink     map[string]any  `json:"primaryLink,omitempty"`
	Links           json.RawMessage `json:"links"`
	PublishedPorts  json.RawMessage `json:"publishedPorts"`
	LinkSource      string          `json:"linkSource,omitempty"`
	Reachability    string          `json:"reachability"`
	LocalOnly       bool            `json:"localOnly"`
	Stale           bool            `json:"stale"`
	LastSeenAt      time.Time       `json:"lastSeenAt"`
	ProbeHistory    []ProbeRecord   `json:"probeHistory,omitempty"`
	LastProbeAt     *time.Time      `json:"lastProbeAt,omitempty"`
	LastProbeStatus string          `json:"lastProbeStatus,omitempty"`
	LastError       string          `json:"lastError,omitempty"`
}

type Settings struct {
	MockIP  string            `json:"mockIp,omitempty"`
	MockIPs map[string]string `json:"mockIps"`
}

type CustomTab struct {
	ID        string       `json:"id"`
	Name      string       `json:"name"`
	SortOrder int          `json:"sortOrder"`
	CreatedAt time.Time    `json:"createdAt"`
	UpdatedAt time.Time    `json:"updatedAt"`
	Links     []CustomLink `json:"links"`
}

type CustomLink struct {
	ID              string     `json:"id"`
	TabID           string     `json:"tabId"`
	URL             string     `json:"url"`
	OriginalURL     string     `json:"originalUrl,omitempty"`
	Name            string     `json:"name"`
	Description     string     `json:"description"`
	Icon            string     `json:"icon"`
	IconURL         string     `json:"iconUrl,omitempty"`
	PageTitle       string     `json:"pageTitle,omitempty"`
	Reachability    string     `json:"reachability"`
	LastProbeAt     *time.Time `json:"lastProbeAt,omitempty"`
	LastProbeStatus string     `json:"lastProbeStatus,omitempty"`
	LastError       string     `json:"lastError,omitempty"`
	SortOrder       int        `json:"sortOrder"`
	CreatedAt       time.Time  `json:"createdAt"`
	UpdatedAt       time.Time  `json:"updatedAt"`
}

type Store struct{ db *sql.DB }

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}
func (s *Store) Close() error { return s.db.Close() }
func (s *Store) migrate() error {
	if _, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS discovered_services (service_key TEXT PRIMARY KEY, service_json TEXT NOT NULL, service_type TEXT NOT NULL DEFAULT 'frontend', created_at TEXT NOT NULL, updated_at TEXT NOT NULL); CREATE TABLE IF NOT EXISTS service_overrides (service_key TEXT PRIMARY KEY, override_json TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL); CREATE TABLE IF NOT EXISTS app_settings (setting_key TEXT PRIMARY KEY, setting_value TEXT NOT NULL, updated_at TEXT NOT NULL); CREATE TABLE IF NOT EXISTS custom_tabs (id TEXT PRIMARY KEY, name TEXT NOT NULL, sort_order INTEGER NOT NULL DEFAULT 0, created_at TEXT NOT NULL, updated_at TEXT NOT NULL); CREATE TABLE IF NOT EXISTS custom_links (id TEXT PRIMARY KEY, tab_id TEXT NOT NULL, url TEXT NOT NULL, name TEXT NOT NULL DEFAULT '', description TEXT NOT NULL DEFAULT '', icon TEXT NOT NULL DEFAULT '', page_title TEXT NOT NULL DEFAULT '', reachability TEXT NOT NULL DEFAULT 'unconfirmed', last_probe_at TEXT, last_probe_status TEXT NOT NULL DEFAULT '', last_error TEXT NOT NULL DEFAULT '', sort_order INTEGER NOT NULL DEFAULT 0, created_at TEXT NOT NULL, updated_at TEXT NOT NULL);`); err != nil {
		return err
	}
	if _, err := s.db.Exec(`INSERT INTO app_settings(setting_key,setting_value,updated_at) SELECT 'mock_ip:frontend',setting_value,updated_at FROM app_settings WHERE setting_key='mock_ip' AND NOT EXISTS (SELECT 1 FROM app_settings WHERE setting_key='mock_ip:frontend'); DELETE FROM app_settings WHERE setting_key='mock_ip';`); err != nil {
		return err
	}
	hasServiceType := false
	rows, err := s.db.Query(`PRAGMA table_info(discovered_services)`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var cid, notNull, pk int
		var name, columnType string
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &pk); err != nil {
			rows.Close()
			return err
		}
		if name == "service_type" {
			hasServiceType = true
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	if !hasServiceType {
		if _, err := s.db.Exec(`ALTER TABLE discovered_services ADD COLUMN service_type TEXT NOT NULL DEFAULT 'frontend'`); err != nil {
			return err
		}
	}
	if err := s.ensureCustomLinkIconURLColumn(); err != nil {
		return err
	}
	return s.backfillLegacyServiceTypes()
}

func (s *Store) ensureCustomLinkIconURLColumn() error {
	rows, err := s.db.Query(`PRAGMA table_info(custom_links)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var cid, notNull, pk int
		var name, columnType string
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &pk); err != nil {
			return err
		}
		if name == "icon_url" {
			return rows.Err()
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	_, err = s.db.Exec(`ALTER TABLE custom_links ADD COLUMN icon_url TEXT NOT NULL DEFAULT ''`)
	return err
}

func newID(prefix string) string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err == nil {
		return prefix + "_" + hex.EncodeToString(buf)
	}
	return fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
}
func (s *Store) UpsertServices(ctx context.Context, services []Service) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO discovered_services(service_key,service_json,service_type,created_at,updated_at) VALUES(?,?,?,?,?) ON CONFLICT(service_key) DO UPDATE SET service_json=excluded.service_json,service_type=excluded.service_type,updated_at=excluded.updated_at`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, service := range services {
		data, err := json.Marshal(service)
		if err != nil {
			return err
		}
		if _, err = stmt.ExecContext(ctx, service.ServiceKey, data, normalizeServiceType(service.ServiceType), now, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}
func (s *Store) ListServices(ctx context.Context) ([]Service, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT service_json,service_type FROM discovered_services`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []Service
	for rows.Next() {
		var data []byte
		var serviceType string
		if err := rows.Scan(&data, &serviceType); err != nil {
			return nil, err
		}
		var service Service
		if err := json.Unmarshal(data, &service); err != nil {
			return nil, err
		}
		service.ServiceType = normalizeServiceType(serviceType)
		result = append(result, service)
	}
	return result, rows.Err()
}
func (s *Store) GetService(ctx context.Context, key string) (Service, error) {
	var data []byte
	var serviceType string
	err := s.db.QueryRowContext(ctx, `SELECT service_json,service_type FROM discovered_services WHERE service_key=?`, key).Scan(&data, &serviceType)
	var service Service
	if err != nil {
		return service, err
	}
	err = json.Unmarshal(data, &service)
	service.ServiceType = normalizeServiceType(serviceType)
	return service, err
}

func normalizeServiceType(value string) string {
	if value == "backend" {
		return "backend"
	}
	return "frontend"
}

func hasPublishedTCPPorts(raw json.RawMessage) bool {
	var ports []struct {
		Protocol string `json:"protocol"`
	}
	if len(raw) == 0 || string(raw) == "null" || json.Unmarshal(raw, &ports) != nil {
		return false
	}
	for _, port := range ports {
		if strings.EqualFold(port.Protocol, "tcp") {
			return true
		}
	}
	return false
}

func inferredServiceType(service Service) string {
	if hasPublishedTCPPorts(service.PublishedPorts) {
		return "frontend"
	}
	return "backend"
}

func (s *Store) backfillLegacyServiceTypes() error {
	rows, err := s.db.Query(`SELECT service_key,service_json,service_type FROM discovered_services`)
	if err != nil {
		return err
	}
	var keys []string
	for rows.Next() {
		var key, data, typeName string
		if err := rows.Scan(&key, &data, &typeName); err != nil {
			return err
		}
		if normalizeServiceType(typeName) == "backend" {
			continue
		}
		var service Service
		if err := json.Unmarshal([]byte(data), &service); err != nil {
			return err
		}
		if inferredServiceType(service) == "backend" {
			keys = append(keys, key)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	rows.Close()
	for _, key := range keys {
		if _, err := s.db.Exec(`UPDATE discovered_services SET service_type='backend' WHERE service_key=?`, key); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) SetServiceType(ctx context.Context, key, serviceType string) error {
	service, err := s.GetService(ctx, key)
	if err != nil {
		return err
	}
	service.ServiceType = normalizeServiceType(serviceType)
	return s.UpsertServices(ctx, []Service{service})
}

func (s *Store) ResetServiceType(ctx context.Context, key string) error {
	service, err := s.GetService(ctx, key)
	if err != nil {
		return err
	}
	service.ServiceType = inferredServiceType(service)
	return s.UpsertServices(ctx, []Service{service})
}
func (s *Store) MarkStale(ctx context.Context, message string) error {
	services, err := s.ListServices(ctx)
	if err != nil {
		return err
	}
	for i := range services {
		services[i].Stale = true
		services[i].LastError = message
	}
	return s.UpsertServices(ctx, services)
}
