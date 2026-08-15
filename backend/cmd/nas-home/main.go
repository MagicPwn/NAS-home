package main

import (
	"context"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"nas-home/backend/internal/config"
	dockerclient "nas-home/backend/internal/docker"
	"nas-home/backend/internal/httpapi"
	"nas-home/backend/internal/reconcile"
	"nas-home/backend/internal/store"
)

func prepareRuntime(dataDir, dockerHost string) error {
	if os.Geteuid() != 0 {
		return os.MkdirAll(dataDir, 0750)
	}
	if err := os.MkdirAll(dataDir, 0750); err != nil {
		return err
	}
	if err := os.Chown(dataDir, 10001, 10001); err != nil {
		return err
	}
	groups := []int{}
	if strings.HasPrefix(dockerHost, "unix://") {
		if info, err := os.Stat(strings.TrimPrefix(dockerHost, "unix://")); err == nil {
			if stat, ok := info.Sys().(*syscall.Stat_t); ok {
				groups = append(groups, int(stat.Gid))
			}
		}
	}
	if err := syscall.Setgroups(groups); err != nil {
		return err
	}
	if err := syscall.Setgid(10001); err != nil {
		return err
	}
	return syscall.Setuid(10001)
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		client := &http.Client{Timeout: 2 * time.Second}
		resp, err := client.Get("http://127.0.0.1:8080/api/health")
		if err != nil || resp.StatusCode != http.StatusOK {
			os.Exit(1)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		return
	}
	cfg := config.Load()
	if err := prepareRuntime(cfg.DataDir, cfg.DockerHost); err != nil {
		log.Fatal(err)
	}
	storePath := filepath.Join(cfg.DataDir, "nas-home.db")
	st, err := store.Open(storePath)
	if err != nil {
		log.Fatal(err)
	}
	defer st.Close()
	docker, err := dockerclient.NewClient(cfg.DockerHost, cfg.ProbeTimeout)
	if err != nil {
		log.Fatal(err)
	}
	r := &reconcile.Reconciler{Config: cfg, Docker: docker, Store: st}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go r.Loop(ctx)
	server := &http.Server{Addr: ":8080", Handler: (&httpapi.Server{Store: st, Reconciler: r}).Handler()}
	go func() { <-ctx.Done(); _ = server.Shutdown(context.Background()) }()
	log.Printf("NAS Home listening on %s", server.Addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
