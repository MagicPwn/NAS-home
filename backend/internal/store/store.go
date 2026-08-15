package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	_ "modernc.org/sqlite"
)

type ProbeRecord struct {
	URL          string    `json:"url"`
	At           time.Time `json:"at"`
	Status       string    `json:"status,omitempty"`
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
	Order           int             `json:"order"`
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
	_, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS discovered_services (service_key TEXT PRIMARY KEY, service_json TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL); CREATE TABLE IF NOT EXISTS service_overrides (service_key TEXT PRIMARY KEY, override_json TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL);`)
	return err
}
func (s *Store) UpsertServices(ctx context.Context, services []Service) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO discovered_services(service_key,service_json,created_at,updated_at) VALUES(?,?,?,?) ON CONFLICT(service_key) DO UPDATE SET service_json=excluded.service_json,updated_at=excluded.updated_at`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, service := range services {
		data, err := json.Marshal(service)
		if err != nil {
			return err
		}
		if _, err = stmt.ExecContext(ctx, service.ServiceKey, data, now, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}
func (s *Store) ListServices(ctx context.Context) ([]Service, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT service_json FROM discovered_services`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []Service
	for rows.Next() {
		var data []byte
		if err := rows.Scan(&data); err != nil {
			return nil, err
		}
		var service Service
		if err := json.Unmarshal(data, &service); err != nil {
			return nil, err
		}
		result = append(result, service)
	}
	return result, rows.Err()
}
func (s *Store) GetService(ctx context.Context, key string) (Service, error) {
	var data []byte
	err := s.db.QueryRowContext(ctx, `SELECT service_json FROM discovered_services WHERE service_key=?`, key).Scan(&data)
	var service Service
	if err != nil {
		return service, err
	}
	err = json.Unmarshal(data, &service)
	return service, err
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
