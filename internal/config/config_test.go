package config

import "testing"

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
