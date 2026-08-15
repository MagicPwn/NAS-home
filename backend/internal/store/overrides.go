package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"
)

type Override struct {
	Name        *string `json:"name,omitempty"`
	Group       *string `json:"group,omitempty"`
	Description *string `json:"description,omitempty"`
	Icon        *string `json:"icon,omitempty"`
	URL         *string `json:"url,omitempty"`
	Hidden      *bool   `json:"hidden,omitempty"`
	SortOrder   *int    `json:"sortOrder,omitempty"`
	ServiceType *string `json:"serviceType,omitempty"`
}

func (s *Store) GetOverride(ctx context.Context, key string) (Override, error) {
	var data []byte
	err := s.db.QueryRowContext(ctx, `SELECT override_json FROM service_overrides WHERE service_key=?`, key).Scan(&data)
	if err != nil {
		return Override{}, err
	}
	var override Override
	return override, json.Unmarshal(data, &override)
}

func (s *Store) SaveOverride(ctx context.Context, key string, override Override) error {
	data, err := json.Marshal(override)
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = s.db.ExecContext(ctx, `INSERT INTO service_overrides(service_key,override_json,created_at,updated_at) VALUES(?,?,?,?) ON CONFLICT(service_key) DO UPDATE SET override_json=excluded.override_json,updated_at=excluded.updated_at`, key, data, now, now)
	return err
}

func (s *Store) DeleteOverride(ctx context.Context, key string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM service_overrides WHERE service_key=?`, key)
	return err
}

func (s *Store) ApplyOverride(service Service, override Override) Service {
	if override.Name != nil {
		service.Name = *override.Name
	}
	if override.Group != nil {
		service.Group = *override.Group
	}
	if override.Description != nil {
		service.Description = *override.Description
	}
	if override.Icon != nil {
		service.Icon = *override.Icon
	}
	if override.URL != nil {
		service.PrimaryURL = *override.URL
		service.LinkSource = "manual"
	}
	if override.Hidden != nil {
		service.Hidden = *override.Hidden
	}
	if override.SortOrder != nil {
		service.Order = *override.SortOrder
	}
	if override.ServiceType != nil {
		service.ServiceType = normalizeServiceType(*override.ServiceType)
	}
	return service
}

func IsNotFound(err error) bool { return err == sql.ErrNoRows }
