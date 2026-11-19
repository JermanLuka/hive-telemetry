// Package config loads service settings from environment variables.
package config

import "os"

// Config holds the settings shared by the hive-telemetry services.
type Config struct {
	DatabaseURL string
	HTTPAddr    string
}

// Load reads the configuration from the environment, falling back to
// defaults that match deploy/compose.yaml.
func Load() Config {
	return Config{
		DatabaseURL: getenv("DATABASE_URL", "postgres://hive:hive@localhost:5432/hive?sslmode=disable"),
		HTTPAddr:    getenv("HTTP_ADDR", ":8080"),
	}
}

func getenv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}
