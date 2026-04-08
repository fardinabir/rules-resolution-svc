package cache

import (
	"context"
	"time"
)

// NoopCache is a Cache implementation that never stores anything.
// Used in test contexts where a Redis instance is not available.
type NoopCache struct{}

func (NoopCache) Get(_ context.Context, _ string) ([]byte, bool, error)            { return nil, false, nil }
func (NoopCache) Set(_ context.Context, _ string, _ []byte, _ time.Duration) error { return nil }
func (NoopCache) Del(_ context.Context, _ ...string) error                         { return nil }
func (NoopCache) FlushByPattern(_ context.Context, _ string) error                 { return nil }
func (NoopCache) Ping(_ context.Context) error                                     { return nil }
