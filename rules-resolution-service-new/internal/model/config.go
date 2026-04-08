// Package model provides the data models for the application.
package model

import "time"

// Config is the configuration for the application.
type Config struct {
	APIServer     Server
	SwaggerServer Server
	PostgreSQL    PostgreSQL
	Redis         Redis
	DefaultActor  string
}

// Server is the configuration for the server.
type Server struct {
	Enable bool
	Port   int
}

// PostgreSQL is the configuration for the PostgreSQL database.
type PostgreSQL struct {
	Host     string `validate:"required"`
	Port     int    `validate:"required"`
	User     string `validate:"required"`
	Password string `validate:"required"`
	DBName   string `validate:"required"`
	SSLMode  string `validate:"required"`
}

// Redis is the configuration for the Redis cache.
type Redis struct {
	Host        string `validate:"required"`
	Port        int    `validate:"required"`
	Password    string
	DB          int
	OverrideTTL time.Duration // TTL for matching-overrides cache entries (default 5m)
	DefaultTTL  time.Duration // TTL for defaults cache entry (default 1h)
}
