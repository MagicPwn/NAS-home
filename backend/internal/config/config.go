package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Port             string
	PublicHost       string
	PublicScheme     string
	PollInterval     time.Duration
	ProbeTimeout     time.Duration
	ReconcileTimeout time.Duration
	DataDir          string
	DockerHost       string
	LogLevel         string
}

func Load() Config {
	return Config{
		Port:             getenv("NAS_HOME_PORT", "9080"),
		PublicHost:       getenv("NAS_HOME_PUBLIC_HOST", ""),
		PublicScheme:     getenv("NAS_HOME_PUBLIC_SCHEME", "http"),
		PollInterval:     duration("NAS_HOME_POLL_INTERVAL", 10*time.Second),
		ProbeTimeout:     duration("NAS_HOME_PROBE_TIMEOUT", 3*time.Second),
		ReconcileTimeout: duration("NAS_HOME_RECONCILE_TIMEOUT", 8*time.Second),
		DataDir:          getenv("NAS_HOME_DATA_DIR", "/data"),
		DockerHost:       getenv("DOCKER_HOST", "tcp://docker-socket-proxy:2375"),
		LogLevel:         getenv("NAS_HOME_LOG_LEVEL", "info"),
	}
}

func getenv(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func duration(key string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func PortNumber(port string) int { n, _ := strconv.Atoi(port); return n }
