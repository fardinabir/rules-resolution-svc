package config

import (
	"log"
	"os"

	"gopkg.in/yaml.v3"
)

type ServerConfig struct {
	Enable bool   `yaml:"enable"`
	Port   string `yaml:"port"`
}

type Config struct {
	DatabaseURL   string       `yaml:"database_url"`
	LogLevel      string       `yaml:"log_level"`
	APIServer     ServerConfig `yaml:"api_server"`
	SwaggerServer ServerConfig `yaml:"swagger_server"`
}

// Load reads config from env.yaml. The path can be overridden via CONFIG_PATH.
func Load() Config {
	path := "env.yaml"
	if p := os.Getenv("CONFIG_PATH"); p != "" {
		path = p
	}

	data, err := os.ReadFile(path)
	if err != nil {
		log.Fatalf("config: read %s: %v", path, err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		log.Fatalf("config: parse %s: %v", path, err)
	}

	// DATABASE_URL env var overrides the yaml value (useful in Docker where the hostname differs)
	if dbURL := os.Getenv("DATABASE_URL"); dbURL != "" {
		cfg.DatabaseURL = dbURL
	}
	if cfg.DatabaseURL == "" {
		cfg.DatabaseURL = "postgres://rrs:rrs@localhost:5432/rrs?sslmode=disable"
	}
	if cfg.LogLevel == "" {
		cfg.LogLevel = "info"
	}
	if cfg.APIServer.Port == "" {
		cfg.APIServer.Port = "8080"
	}
	if cfg.SwaggerServer.Port == "" {
		cfg.SwaggerServer.Port = "8081"
	}

	return cfg
}
