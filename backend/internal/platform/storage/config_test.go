package storage

import "testing"

func TestLoadPostgresConfig_RequiresConnectionSettings(t *testing.T) {
	tests := map[string]PostgresConfig{
		"host":     {Database: "kora", User: "kora"},
		"database": {Host: "db", User: "kora"},
		"user":     {Host: "db", Database: "kora"},
	}
	for missing, cfg := range tests {
		t.Run(missing, func(t *testing.T) {
			t.Setenv("DB_PASSWORD", "secret")
			if err := loadPostgresConfig(&cfg); err == nil {
				t.Errorf("missing %s: expected an error", missing)
			}
		})
	}
}

func TestLoadPostgresConfig_ReadsPasswordFromEnv(t *testing.T) {
	t.Setenv("DB_PASSWORD", "secret")
	cfg := PostgresConfig{Host: "db", Database: "kora", User: "kora"}
	if err := loadPostgresConfig(&cfg); err != nil || cfg.Password != "secret" {
		t.Fatalf("loadPostgresConfig() = %v, password %q", err, cfg.Password)
	}
	t.Setenv("DB_PASSWORD", "")
	if err := loadPostgresConfig(&cfg); err == nil {
		t.Error("an empty DB_PASSWORD must fail")
	}
}

func TestLoadRedisConfig(t *testing.T) {
	t.Setenv("REDIS_PASSWORD", "secret")
	if err := loadRedisConfig(&RedisConfig{}); err == nil {
		t.Error("a missing redis.host must fail")
	}
	cfg := RedisConfig{Host: "redis"}
	if err := loadRedisConfig(&cfg); err != nil || cfg.Password != "secret" {
		t.Fatalf("loadRedisConfig() = %v, password %q", err, cfg.Password)
	}
}

func TestLocalConfig_PointsAtComposeServices(t *testing.T) {
	t.Setenv("APP_ENV", "local")
	t.Setenv("DB_PASSWORD", "secret")
	t.Setenv("REDIS_PASSWORD", "secret")
	cfg, err := LoadStorageConfig()
	if err != nil {
		t.Fatalf("LoadStorageConfig() error: %v", err)
	}
	if cfg.Postgres.Host != "localhost" || cfg.Redis.Host != "localhost" || cfg.Postgres.SSLMode != "disable" {
		t.Errorf("local storage config = %+v", cfg)
	}
}
