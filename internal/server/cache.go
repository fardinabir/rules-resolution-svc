package server

import (
	"time"

	"github.com/fardinabir/rules-resolution-svc/internal/cache"
	"github.com/fardinabir/rules-resolution-svc/internal/model"
	log "github.com/sirupsen/logrus"
)

// initCache connects to Redis and returns a Cache. Falls back to NoopCache if Redis
// is not configured or unavailable so the service starts without caching.
func initCache(cfg model.Redis) cache.Cache {
	if cfg.Host == "" {
		log.Info("Redis not configured — running without cache")
		return cache.NoopCache{}
	}

	rc, err := cache.NewRedisCache(cache.RedisConfig{
		Host:     cfg.Host,
		Port:     cfg.Port,
		Password: cfg.Password,
		DB:       cfg.DB,
	})
	if err != nil {
		log.WithError(err).Warn("Redis unavailable — running without cache")
		return cache.NoopCache{}
	}

	log.Info("Redis cache connected")
	return rc
}

// ttlOr returns d if non-zero, otherwise fallback.
func ttlOr(d, fallback time.Duration) time.Duration {
	if d == 0 {
		return fallback
	}
	return d
}
