package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateRequiresLoopback(t *testing.T) {
	t.Parallel()
	for _, address := range []string{"127.0.0.1:8787", "localhost:8787", "[::1]:8787"} {
		cfg := Default()
		cfg.Listen = address
		if err := cfg.Validate(); err != nil {
			t.Errorf("expected %s to be valid: %v", address, err)
		}
	}
	for _, address := range []string{"0.0.0.0:8787", "192.168.1.2:8787", ":8787"} {
		cfg := Default()
		cfg.Listen = address
		if err := cfg.Validate(); err == nil {
			t.Errorf("expected %s to be rejected", address)
		}
	}
	cfg := Default()
	cfg.Listen = "0.0.0.0:8787"
	cfg.AllowNonLoopback = true
	if err := cfg.Validate(); err != nil {
		t.Fatalf("container listener should be valid with explicit opt-in: %v", err)
	}
}

func TestAdminListenerRequiresIndependentLoopbackOrContainerOptIn(t *testing.T) {
	t.Parallel()
	cfg := Default()
	cfg.AdminListen = "0.0.0.0:8788"
	if err := cfg.Validate(); err == nil {
		t.Fatal("non-loopback admin listener was accepted")
	}
	cfg.AllowAdminNonLoopback = true
	if err := cfg.Validate(); err != nil {
		t.Fatalf("container admin listener: %v", err)
	}
	cfg.Listen = cfg.AdminListen
	if err := cfg.Validate(); err == nil {
		t.Fatal("shared public/admin listener was accepted")
	}
}

func TestResourceLimitValidation(t *testing.T) {
	t.Parallel()
	for _, mutate := range []func(*Config){
		func(cfg *Config) { cfg.MinFreeBytes = -1 },
		func(cfg *Config) { cfg.DefaultVaultQuotaBytes = -1 },
		func(cfg *Config) { cfg.RateRequestsPerMinute = 0 },
		func(cfg *Config) { cfg.RateBytesPerMinute = 0 },
	} {
		cfg := Default()
		mutate(&cfg)
		if err := cfg.Validate(); err == nil {
			t.Fatal("invalid resource limit was accepted")
		}
	}
}

func TestResolveAdminTokenIsOptionalByDefault(t *testing.T) {
	t.Setenv("OBSIDIAN_SYNC_ADMIN_TOKEN", strings.Repeat("x", 64))
	cfg := Default()
	resolved, err := cfg.ResolveAdminToken()
	if err != nil || resolved != "" {
		t.Fatalf("default token resolution=%q err=%v", resolved, err)
	}
}

func TestResolveAdminTokenModeRequiresASecretFile(t *testing.T) {
	t.Setenv("OBSIDIAN_SYNC_ADMIN_TOKEN", strings.Repeat("x", 64))
	cfg := Default()
	cfg.AdminAuth = "token"
	if _, err := cfg.ResolveAdminToken(); err == nil {
		t.Fatal("environment-only Admin Token was accepted")
	}

	token := strings.Repeat("t", 48)
	path := filepath.Join(t.TempDir(), "admin-token.txt")
	if err := os.WriteFile(path, []byte(token+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg.AdminTokenFile = path
	resolved, err := cfg.ResolveAdminToken()
	if err != nil {
		t.Fatal(err)
	}
	if resolved != token {
		t.Fatal("Admin Token file was not trimmed and returned exactly")
	}
}
