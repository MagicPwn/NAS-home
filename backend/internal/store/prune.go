package store

import (
	"context"
	"strings"
)

func (s *Store) PruneServices(ctx context.Context, keys []string) error {
	if len(keys) == 0 {
		_, err := s.db.ExecContext(ctx, `DELETE FROM discovered_services`)
		return err
	}
	placeholders := strings.TrimRight(strings.Repeat("?,", len(keys)), ",")
	args := make([]any, len(keys))
	for i, key := range keys {
		args[i] = key
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM discovered_services WHERE service_key NOT IN (`+placeholders+`)`, args...)
	return err
}

func (s *Store) MarkUnseenStale(ctx context.Context, seenKeys map[string]bool, message string) error {
	services, err := s.ListServices(ctx)
	if err != nil {
		return err
	}
	changed := false
	for i := range services {
		if !seenKeys[services[i].ServiceKey] {
			services[i].Stale = true
			services[i].LastError = message
			changed = true
		}
	}
	if !changed {
		return nil
	}
	return s.UpsertServices(ctx, services)
}
