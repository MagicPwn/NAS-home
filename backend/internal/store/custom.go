package store

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

func (s *Store) GetSettings(ctx context.Context) (Settings, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT setting_key,setting_value FROM app_settings WHERE setting_key LIKE 'mock_ip:%'`)
	if err != nil {
		return Settings{}, err
	}
	defer rows.Close()
	settings := Settings{MockIPs: make(map[string]string)}
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return Settings{}, err
		}
		settings.MockIPs[strings.TrimPrefix(key, "mock_ip:")] = value
	}
	if err := rows.Err(); err != nil {
		return Settings{}, err
	}
	settings.MockIP = settings.MockIPs["frontend"]
	return settings, nil
}

func (s *Store) GetTabMockIP(ctx context.Context, tabID string) (string, error) {
	var value string
	err := s.db.QueryRowContext(ctx, `SELECT setting_value FROM app_settings WHERE setting_key=?`, "mock_ip:"+tabID).Scan(&value)
	if IsNotFound(err) {
		return "", nil
	}
	return value, err
}

func (s *Store) SetTabMockIP(ctx context.Context, tabID, mockIP string) error {
	if strings.TrimSpace(tabID) == "" {
		return sql.ErrNoRows
	}
	key := "mock_ip:" + strings.TrimSpace(tabID)
	if strings.TrimSpace(mockIP) == "" {
		_, err := s.db.ExecContext(ctx, `DELETE FROM app_settings WHERE setting_key=?`, key)
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx, `INSERT INTO app_settings(setting_key,setting_value,updated_at) VALUES(?,?,?) ON CONFLICT(setting_key) DO UPDATE SET setting_value=excluded.setting_value,updated_at=excluded.updated_at`, key, strings.TrimSpace(mockIP), now)
	return err
}

// SetMockIP preserves the original API semantics by assigning the value to the
// built-in frontend tab.
func (s *Store) SetMockIP(ctx context.Context, mockIP string) error {
	return s.SetTabMockIP(ctx, "frontend", mockIP)
}

func (s *Store) ListCustomTabs(ctx context.Context) ([]CustomTab, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,sort_order,created_at,updated_at FROM custom_tabs ORDER BY sort_order,name,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tabs := make([]CustomTab, 0)
	for rows.Next() {
		var tab CustomTab
		var created, updated string
		if err := rows.Scan(&tab.ID, &tab.Name, &tab.SortOrder, &created, &updated); err != nil {
			return nil, err
		}
		tab.CreatedAt = parseStoredTime(created)
		tab.UpdatedAt = parseStoredTime(updated)
		tab.Links, err = s.listCustomLinks(ctx, tab.ID)
		if err != nil {
			return nil, err
		}
		tabs = append(tabs, tab)
	}
	return tabs, rows.Err()
}

func (s *Store) GetCustomTab(ctx context.Context, id string) (CustomTab, error) {
	var tab CustomTab
	var created, updated string
	err := s.db.QueryRowContext(ctx, `SELECT id,name,sort_order,created_at,updated_at FROM custom_tabs WHERE id=?`, id).Scan(&tab.ID, &tab.Name, &tab.SortOrder, &created, &updated)
	if err != nil {
		return tab, err
	}
	tab.CreatedAt = parseStoredTime(created)
	tab.UpdatedAt = parseStoredTime(updated)
	tab.Links, err = s.listCustomLinks(ctx, id)
	return tab, err
}

func (s *Store) CreateCustomTab(ctx context.Context, name string, sortOrder int) (CustomTab, error) {
	now := time.Now().UTC()
	tab := CustomTab{ID: newID("tab"), Name: name, SortOrder: sortOrder, CreatedAt: now, UpdatedAt: now, Links: []CustomLink{}}
	_, err := s.db.ExecContext(ctx, `INSERT INTO custom_tabs(id,name,sort_order,created_at,updated_at) VALUES(?,?,?,?,?)`, tab.ID, tab.Name, tab.SortOrder, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	return tab, err
}

func (s *Store) UpdateCustomTab(ctx context.Context, id, name string, sortOrder int) (CustomTab, error) {
	now := time.Now().UTC()
	result, err := s.db.ExecContext(ctx, `UPDATE custom_tabs SET name=?,sort_order=?,updated_at=? WHERE id=?`, name, sortOrder, now.Format(time.RFC3339Nano), id)
	if err != nil {
		return CustomTab{}, err
	}
	if count, _ := result.RowsAffected(); count == 0 {
		return CustomTab{}, sql.ErrNoRows
	}
	return s.GetCustomTab(ctx, id)
}

func (s *Store) DeleteCustomTab(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM custom_links WHERE tab_id=?`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM app_settings WHERE setting_key=?`, "mock_ip:"+id); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM custom_tabs WHERE id=?`, id)
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count == 0 {
		return sql.ErrNoRows
	}
	return tx.Commit()
}

func (s *Store) listCustomLinks(ctx context.Context, tabID string) ([]CustomLink, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,tab_id,url,name,description,icon,icon_url,page_title,reachability,last_probe_at,last_probe_status,last_error,sort_order,created_at,updated_at FROM custom_links WHERE tab_id=? ORDER BY sort_order,name,id`, tabID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	links := make([]CustomLink, 0)
	for rows.Next() {
		link, err := scanCustomLink(rows)
		if err != nil {
			return nil, err
		}
		links = append(links, link)
	}
	return links, rows.Err()
}

type customLinkScanner interface{ Scan(dest ...any) error }

func scanCustomLink(scanner customLinkScanner) (CustomLink, error) {
	var link CustomLink
	var lastProbeAt sql.NullString
	var created, updated string
	err := scanner.Scan(&link.ID, &link.TabID, &link.URL, &link.Name, &link.Description, &link.Icon, &link.IconURL, &link.PageTitle, &link.Reachability, &lastProbeAt, &link.LastProbeStatus, &link.LastError, &link.SortOrder, &created, &updated)
	if err != nil {
		return link, err
	}
	link.CreatedAt = parseStoredTime(created)
	link.UpdatedAt = parseStoredTime(updated)
	if lastProbeAt.Valid && lastProbeAt.String != "" {
		value := parseStoredTime(lastProbeAt.String)
		link.LastProbeAt = &value
	}
	return link, nil
}

func (s *Store) GetCustomLink(ctx context.Context, id string) (CustomLink, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id,tab_id,url,name,description,icon,icon_url,page_title,reachability,last_probe_at,last_probe_status,last_error,sort_order,created_at,updated_at FROM custom_links WHERE id=?`, id)
	return scanCustomLink(row)
}

func (s *Store) CreateCustomLink(ctx context.Context, link CustomLink) (CustomLink, error) {
	now := time.Now().UTC()
	if link.ID == "" {
		link.ID = newID("link")
	}
	link.CreatedAt = now
	link.UpdatedAt = now
	_, err := s.db.ExecContext(ctx, `INSERT INTO custom_links(id,tab_id,url,name,description,icon,icon_url,page_title,reachability,last_probe_at,last_probe_status,last_error,sort_order,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, link.ID, link.TabID, link.URL, link.Name, link.Description, link.Icon, link.IconURL, link.PageTitle, link.Reachability, formatStoredTime(link.LastProbeAt), link.LastProbeStatus, link.LastError, link.SortOrder, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	return link, err
}

func (s *Store) UpdateCustomLink(ctx context.Context, link CustomLink) (CustomLink, error) {
	now := time.Now().UTC()
	result, err := s.db.ExecContext(ctx, `UPDATE custom_links SET tab_id=?,url=?,name=?,description=?,icon=?,icon_url=?,page_title=?,reachability=?,last_probe_at=?,last_probe_status=?,last_error=?,sort_order=?,updated_at=? WHERE id=?`, link.TabID, link.URL, link.Name, link.Description, link.Icon, link.IconURL, link.PageTitle, link.Reachability, formatStoredTime(link.LastProbeAt), link.LastProbeStatus, link.LastError, link.SortOrder, now.Format(time.RFC3339Nano), link.ID)
	if err != nil {
		return CustomLink{}, err
	}
	if count, _ := result.RowsAffected(); count == 0 {
		return CustomLink{}, sql.ErrNoRows
	}
	return s.GetCustomLink(ctx, link.ID)
}

func (s *Store) DeleteCustomLink(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM custom_links WHERE id=?`, id)
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func parseStoredTime(value string) time.Time {
	parsed, _ := time.Parse(time.RFC3339Nano, value)
	return parsed
}

func formatStoredTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return value.UTC().Format(time.RFC3339Nano)
}
