// Package config loads the server runtime configuration from environment
// variables.
//
// Environment variables:
//
//	FT_HTTP_PORT      TCP port to bind on 127.0.0.1. "0" (default) = ephemeral port.
//	FT_SESSION_TOKEN  Bearer token required by every /api/v1 endpoint. When
//	                  empty the auth middleware rejects every request (secure
//	                  default); the Tauri host always provides a token.
//	FT_DATA_ROOT      Data directory holding floattranslate.db and logs.
//	                  Defaults to "<cwd>/data".
package config

import (
	"os"
	"path/filepath"
)

// Config is the resolved runtime configuration of the backend sidecar.
type Config struct {
	// HTTPPort is the TCP port to listen on; "0" means an ephemeral port.
	HTTPPort string
	// SessionToken is the local session bearer token required by all
	// /api/v1 endpoints. Never logged.
	SessionToken string
	// DataRoot is the directory holding floattranslate.db and the logs dir.
	DataRoot string
	// Version is the application version reported by /health and the ready line.
	Version string
}

const (
	envHTTPPort     = "FT_HTTP_PORT"
	envSessionToken = "FT_SESSION_TOKEN"
	envDataRoot     = "FT_DATA_ROOT"

	// DefaultVersion is reported by /health and the startup ready line.
	// Keep in sync with src-tauri/tauri.conf.json (scripts/check-versions.mjs
	// asserts this file carries the same string).
	DefaultVersion = "1.1.0"
)

// Load reads configuration from the process environment and applies defaults.
func Load() (Config, error) {
	cfg := Config{
		HTTPPort:     os.Getenv(envHTTPPort),
		SessionToken: os.Getenv(envSessionToken),
		DataRoot:     os.Getenv(envDataRoot),
		Version:      DefaultVersion,
	}
	if cfg.HTTPPort == "" {
		cfg.HTTPPort = "0"
	}
	if cfg.DataRoot == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return Config{}, err
		}
		cfg.DataRoot = filepath.Join(cwd, "data")
	}
	return cfg, nil
}
