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

const defaultMaxUploadBytes int64 = 64 * 1024 * 1024

type Config struct {
	Listen           string `json:"listen"`
	DatabasePath     string `json:"database_path"`
	TokenFile        string `json:"token_file"`
	LogPath          string `json:"log_path"`
	MaxUploadBytes   int64  `json:"max_upload_bytes"`
	AllowNonLoopback bool   `json:"allow_non_loopback"`
}

func Default() Config {
	return Config{
		Listen:         "127.0.0.1:8787",
		DatabasePath:   "data/sync.db",
		MaxUploadBytes: defaultMaxUploadBytes,
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
	if cfg.TokenFile != "" && !filepath.IsAbs(cfg.TokenFile) {
		cfg.TokenFile = filepath.Join(base, cfg.TokenFile)
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
	if c.MaxUploadBytes < 1024 {
		return errors.New("max_upload_bytes must be at least 1024")
	}
	return nil
}

func (c Config) ResolveToken() (string, error) {
	if value := strings.TrimSpace(os.Getenv("OBSIDIAN_SYNC_TOKEN")); value != "" {
		return value, nil
	}
	if c.TokenFile == "" {
		return "", errors.New("set token_file or OBSIDIAN_SYNC_TOKEN")
	}
	content, err := os.ReadFile(c.TokenFile)
	if err != nil {
		return "", fmt.Errorf("read token file: %w", err)
	}
	value := strings.TrimSpace(string(content))
	if len(value) < 32 {
		return "", errors.New("bearer token must contain at least 32 characters")
	}
	return value, nil
}
