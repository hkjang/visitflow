package platform

import "testing"

func TestLoadConfig(t *testing.T) {
	t.Setenv("POSTGRES_DSN", "postgres://example/visitflow")
	t.Setenv("BOOTSTRAP_ADMIN", "admin")
	t.Setenv("BOOTSTRAP_ADMIN_PASSWORD", "very-long-password")
	t.Setenv("ENCRYPTION_KEY", "WlpaWlpaWlpaWlpaWlpaWlpaWlpaWlpaWlpaWlpaWlo=")
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.BootstrapAdmin != "admin" {
		t.Fatalf("unexpected admin: %s", cfg.BootstrapAdmin)
	}
}

func TestLoadConfigRejectsShortPassword(t *testing.T) {
	t.Setenv("POSTGRES_DSN", "postgres://example/visitflow")
	t.Setenv("BOOTSTRAP_ADMIN", "admin")
	t.Setenv("BOOTSTRAP_ADMIN_PASSWORD", "short")
	t.Setenv("ENCRYPTION_KEY", "WlpaWlpaWlpaWlpaWlpaWlpaWlpaWlpaWlpaWlpaWlo=")
	if _, err := LoadConfig(); err == nil {
		t.Fatal("expected password validation error")
	}
}
