package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
)

const defaultMaxFileBytes int64 = 64 * 1024 * 1024

type Config struct {
	Listen                 string `json:"listen"`
	AdminListen            string `json:"admin_listen"`
	DatabasePath           string `json:"database_path"`
	AdminTokenFile         string `json:"admin_token_file"`
	LogPath                string `json:"log_path"`
	MaxFileBytes           int64  `json:"max_file_bytes"`
	DefaultVaultQuotaBytes int64  `json:"default_vault_quota_bytes"`
	DefaultVaultMaxFiles   int64  `json:"default_vault_max_files"`
	MinFreeBytes           int64  `json:"min_free_bytes"`
	RateRequestsPerMinute  int    `json:"rate_requests_per_minute"`
	RateBytesPerMinute     int64  `json:"rate_bytes_per_minute"`
	AllowNonLoopback       bool   `json:"allow_non_loopback"`
	AllowAdminNonLoopback  bool   `json:"allow_admin_non_loopback"`
}

func Default() Config {
	return Config{
		Listen:                "127.0.0.1:8787",
		AdminListen:           "127.0.0.1:8788",
		DatabasePath:          "data/sync.db",
		MaxFileBytes:          defaultMaxFileBytes,
		MinFreeBytes:          512 * 1024 * 1024,
		RateRequestsPerMinute: 600,
		RateBytesPerMinute:    512 * 1024 * 1024,
	}
}

func Load(path string) (Config, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}
	cfg := Default()
	if err := json.Unmarshal(content, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse config: %w", err)
	}
	base := filepath.Dir(path)
	if cfg.DatabasePath != "" && !filepath.IsAbs(cfg.DatabasePath) {
		cfg.DatabasePath = filepath.Join(base, cfg.DatabasePath)
	}
	if cfg.AdminTokenFile != "" && !filepath.IsAbs(cfg.AdminTokenFile) {
		cfg.AdminTokenFile = filepath.Join(base, cfg.AdminTokenFile)
	}
	if cfg.LogPath != "" && !filepath.IsAbs(cfg.LogPath) {
		cfg.LogPath = filepath.Join(base, cfg.LogPath)
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) Validate() error {
	if c.Listen == "" {
		return errors.New("listen address is required")
	}
	host, _, err := net.SplitHostPort(c.Listen)
	if err != nil {
		return fmt.Errorf("invalid listen address: %w", err)
	}
	if !c.AllowNonLoopback && host != "localhost" {
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			return errors.New("listen address must use localhost or a loopback IP unless allow_non_loopback is explicitly enabled")
		}
	}
	if c.DatabasePath == "" {
		return errors.New("database_path is required")
	}
	if c.Listen == c.AdminListen {
		return errors.New("public and admin listen addresses must be different")
	}
	if c.MaxFileBytes < 1024 {
		return errors.New("max_file_bytes must be at least 1024")
	}
	adminHost, _, err := net.SplitHostPort(c.AdminListen)
	if err != nil {
		return fmt.Errorf("invalid admin listen address: %w", err)
	}
	adminIP := net.ParseIP(adminHost)
	if !c.AllowAdminNonLoopback && adminHost != "localhost" && (adminIP == nil || !adminIP.IsLoopback()) {
		return errors.New("admin listen address must be loopback-only")
	}
	if c.DefaultVaultQuotaBytes < 0 || c.DefaultVaultMaxFiles < 0 || c.MinFreeBytes < 0 {
		return errors.New("resource limits cannot be negative")
	}
	if c.RateRequestsPerMinute < 1 || c.RateBytesPerMinute < 1 {
		return errors.New("rate limits must be positive")
	}
	return nil
}

func (c Config) ResolveAdminToken() (string, error) {
	if value := strings.TrimSpace(os.Getenv("OBSIDIAN_SYNC_ADMIN_TOKEN")); value != "" {
		return value, nil
	}
	if c.AdminTokenFile == "" {
		return "", errors.New("set admin_token_file or OBSIDIAN_SYNC_ADMIN_TOKEN")
	}
	content, err := os.ReadFile(c.AdminTokenFile)
	if err != nil {
		return "", fmt.Errorf("read admin token file: %w", err)
	}
	value := strings.TrimSpace(string(content))
	if len(value) < 32 {
		return "", errors.New("admin token must contain at least 32 characters")
	}
	return value, nil
}
