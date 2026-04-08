// Package cache provides a thin abstraction over a key-value store
// used to cache expensive read operations in the resolution hot path.
package cache

import (
	"context"
	"time"
)

// Cache is a simple get/set/delete interface backed by Redis in production
// and a no-op implementation in test contexts.
type Cache interface {
	// Get returns the cached bytes for key. found is false on a cache miss.
	Get(ctx context.Context, key string) (val []byte, found bool, err error)
	// Set stores val under key with the given TTL.
	Set(ctx context.Context, key string, val []byte, ttl time.Duration) error
	// Del removes one or more keys.
	Del(ctx context.Context, keys ...string) error
	// FlushByPattern deletes all keys matching the given glob pattern (e.g. "resolve:overrides:*").
	// Uses SCAN + DEL to avoid blocking the Redis server.
	FlushByPattern(ctx context.Context, pattern string) error
	// Ping checks connectivity to the backing store.
	Ping(ctx context.Context) error
}
