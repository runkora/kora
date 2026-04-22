package storage

import "testing"

func TestLoadRedisConfigRequiresUsername(t *testing.T) {
	t.Setenv("REDIS_HOST", "localhost")
	t.Setenv("REDIS_PORT", "6380")
	t.Setenv("REDIS_PASSWORD", "orbit")

	cfg := &RedisConfig{Port: 6379}

	err := loadRedisConfig(cfg)
	if err == nil {
		t.Fatal("expected REDIS_USERNAME validation error")
	}
	if err.Error() != "REDIS_USERNAME environment variable is required" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadRedisConfigAppliesUsernamePasswordAndOverrides(t *testing.T) {
	t.Setenv("REDIS_HOST", "redis.internal")
	t.Setenv("REDIS_USERNAME", "orbit")
	t.Setenv("REDIS_PASSWORD", "secret")
	t.Setenv("REDIS_PORT", "6380")
	t.Setenv("ORBIT_REDIS_TLS_ENABLED", "true")

	cfg := &RedisConfig{
		Port:       6379,
		TLSEnabled: false,
	}

	if err := loadRedisConfig(cfg); err != nil {
		t.Fatalf("loadRedisConfig: %v", err)
	}

	if cfg.Host != "redis.internal" {
		t.Fatalf("unexpected host: %q", cfg.Host)
	}
	if cfg.Username != "orbit" {
		t.Fatalf("unexpected username: %q", cfg.Username)
	}
	if cfg.Password != "secret" {
		t.Fatalf("unexpected password: %q", cfg.Password)
	}
	if cfg.Port != 6380 {
		t.Fatalf("unexpected port: %d", cfg.Port)
	}
	if !cfg.TLSEnabled {
		t.Fatal("expected TLS to be enabled from env override")
	}
}
