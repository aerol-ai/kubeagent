package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config holds agent runtime configuration.
type Config struct {
	PlatformURL          string
	Token                string
	ReconnectInterval    time.Duration
	MaxReconnectInterval time.Duration
	HeartbeatInterval    time.Duration
	CommandTimeout       time.Duration
	InCluster            bool
	KubeConfigPath       string
	AgentID              string
	LogLevel             string
	AutoUpgradeEnabled   bool
	AutoUpgradeInterval  time.Duration
	HelmChartVersion     string
	AgentImageRepo       string
	PodNamespace         string
	DeploymentName       string
}

// LoadFromEnv reads configuration from environment variables.
func LoadFromEnv() (*Config, error) {
	cfg := &Config{
		PlatformURL:          getEnv("PLATFORM_URL", ""),
		Token:                getEnv("AGENT_TOKEN", ""),
		ReconnectInterval:    getDurationEnv("RECONNECT_INTERVAL", 5*time.Second),
		MaxReconnectInterval: getDurationEnv("MAX_RECONNECT_INTERVAL", 5*time.Minute),
		HeartbeatInterval:    getDurationEnv("HEARTBEAT_INTERVAL", 30*time.Second),
		CommandTimeout:       getDurationEnv("COMMAND_TIMEOUT", 60*time.Second),
		InCluster:            getBoolEnv("IN_CLUSTER", true),
		KubeConfigPath:       getEnv("KUBECONFIG", ""),
		AgentID:              getEnv("AGENT_ID", ""),
		LogLevel:             getEnv("LOG_LEVEL", "info"),
		AutoUpgradeEnabled:   getBoolEnv("AUTO_UPGRADE_ENABLED", false),
		AutoUpgradeInterval:  getDurationEnv("AUTO_UPGRADE_INTERVAL", 60*time.Second),
		HelmChartVersion:     getEnv("HELM_CHART_VERSION", ""),
		AgentImageRepo:       getEnv("AGENT_IMAGE_REPO", ""),
		PodNamespace:         getEnv("POD_NAMESPACE", ""),
		DeploymentName:       getEnv("DEPLOYMENT_NAME", ""),
	}
	if cfg.PlatformURL == "" {
		return nil, fmt.Errorf("PLATFORM_URL is required")
	}
	if cfg.Token == "" {
		return nil, fmt.Errorf("AGENT_TOKEN is required")
	}
	return cfg, nil
}

func getEnv(key, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return fallback
}

func getDurationEnv(key string, fallback time.Duration) time.Duration {
	val := os.Getenv(key)
	if val == "" {
		return fallback
	}
	secs, err := strconv.Atoi(val)
	if err != nil {
		return fallback
	}
	return time.Duration(secs) * time.Second
}

func getBoolEnv(key string, fallback bool) bool {
	val := os.Getenv(key)
	if val == "" {
		return fallback
	}
	b, err := strconv.ParseBool(val)
	if err != nil {
		return fallback
	}
	return b
}
