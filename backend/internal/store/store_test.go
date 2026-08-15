package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestServiceTypePersistsInSQLiteAndOverride(t *testing.T) {
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "nas-home.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service := Service{ServiceKey: "worker", Name: "Worker", ServiceType: "backend"}
	if err := store.UpsertServices(ctx, []Service{service}); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetService(ctx, "worker")
	if err != nil {
		t.Fatal(err)
	}
	if got.ServiceType != "backend" {
		t.Fatalf("service type = %q", got.ServiceType)
	}
	frontend := "frontend"
	if err := store.SaveOverride(ctx, "worker", Override{ServiceType: &frontend}); err != nil {
		t.Fatal(err)
	}
	override, err := store.GetOverride(ctx, "worker")
	if err != nil {
		t.Fatal(err)
	}
	got = store.ApplyOverride(got, override)
	if got.ServiceType != "frontend" {
		t.Fatalf("overridden service type = %q", got.ServiceType)
	}
	if err := store.SetServiceType(ctx, "worker", "backend"); err != nil {
		t.Fatal(err)
	}
	got, err = store.GetService(ctx, "worker")
	if err != nil || got.ServiceType != "backend" {
		t.Fatalf("set service type = %#v, %v", got, err)
	}
}

func TestLegacyNoPortSnapshotMigratesToBackend(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE discovered_services (service_key TEXT PRIMARY KEY, service_json TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL); CREATE TABLE service_overrides (service_key TEXT PRIMARY KEY, override_json TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL);`); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(Service{ServiceKey: "legacy-worker", Name: "Legacy worker", PublishedPorts: json.RawMessage(`[]`)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO discovered_services(service_key,service_json,created_at,updated_at) VALUES(?,?,?,?)`, "legacy-worker", data, "now", "now"); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	got, err := store.GetService(context.Background(), "legacy-worker")
	if err != nil {
		t.Fatal(err)
	}
	if got.ServiceType != "backend" {
		t.Fatalf("legacy service type = %q", got.ServiceType)
	}
}

func TestSettingsAndCustomLinksPersist(t *testing.T) {
	ctx := context.Background()
	st, err := Open(filepath.Join(t.TempDir(), "custom.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.SetMockIP(ctx, "192.168.1.50"); err != nil {
		t.Fatal(err)
	}
	settings, err := st.GetSettings(ctx)
	if err != nil || settings.MockIP != "192.168.1.50" {
		t.Fatalf("settings = %#v, err=%v", settings, err)
	}
	tab, err := st.CreateCustomTab(ctx, "外部服务", 1)
	if err != nil {
		t.Fatal(err)
	}
	link, err := st.CreateCustomLink(ctx, CustomLink{TabID: tab.ID, URL: "https://example.com", Name: "Example", Reachability: "reachable"})
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := st.GetCustomLink(ctx, link.ID)
	if err != nil || loaded.TabID != tab.ID || loaded.URL != link.URL {
		t.Fatalf("link = %#v, err=%v", loaded, err)
	}
	tabs, err := st.ListCustomTabs(ctx)
	if err != nil || len(tabs) != 1 || len(tabs[0].Links) != 1 {
		t.Fatalf("tabs = %#v, err=%v", tabs, err)
	}
	if err := st.DeleteCustomTab(ctx, tab.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetCustomLink(ctx, link.ID); !IsNotFound(err) {
		t.Fatalf("deleted link err = %v", err)
	}
}

func TestLegacyGlobalMockIPMigratesToFrontendTab(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy-settings.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE app_settings (setting_key TEXT PRIMARY KEY, setting_value TEXT NOT NULL, updated_at TEXT NOT NULL); INSERT INTO app_settings(setting_key,setting_value,updated_at) VALUES('mock_ip','localhost','now');`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	settings, err := st.GetSettings(context.Background())
	if err != nil || settings.MockIPs["frontend"] != "localhost" {
		t.Fatalf("migrated settings = %#v, err=%v", settings, err)
	}
}

func TestMockIPIsStoredPerNavigationTab(t *testing.T) {
	ctx := context.Background()
	st, err := Open(filepath.Join(t.TempDir(), "tab-settings.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.SetTabMockIP(ctx, "frontend", "localhost"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTabMockIP(ctx, "backend", "192.168.1.20"); err != nil {
		t.Fatal(err)
	}
	frontend, err := st.GetTabMockIP(ctx, "frontend")
	if err != nil || frontend != "localhost" {
		t.Fatalf("frontend mock ip = %q, err=%v", frontend, err)
	}
	backend, err := st.GetTabMockIP(ctx, "backend")
	if err != nil || backend != "192.168.1.20" {
		t.Fatalf("backend mock ip = %q, err=%v", backend, err)
	}
	settings, err := st.GetSettings(ctx)
	if err != nil || settings.MockIPs["frontend"] != "localhost" || settings.MockIPs["backend"] != "192.168.1.20" {
		t.Fatalf("per-tab settings = %#v, err=%v", settings, err)
	}
}
