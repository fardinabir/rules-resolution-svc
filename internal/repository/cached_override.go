package repository

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"

	"github.com/fardinabir/rules-resolution-svc/internal/cache"
	"github.com/fardinabir/rules-resolution-svc/internal/domain"
	log "github.com/sirupsen/logrus"
)

const (
	overridesCachePatternBulk = "resolve:bulk"
	overridesCachePattern     = "resolve:overrides"
)

// cachedOverrideRepo wraps an OverrideRepository with a Redis cache.
// FindMatchingOverrides results are cached per unique (state, client, investor, caseType, date) tuple.
// Any write operation (Create, Update, UpdateStatus) flushes all matching-overrides cache entries.
type cachedOverrideRepo struct {
	inner OverrideRepository
	cache cache.Cache
	ttl   time.Duration
}

// NewCachedOverrideRepository returns an OverrideRepository that caches FindMatchingOverrides.
func NewCachedOverrideRepository(inner OverrideRepository, c cache.Cache, ttl time.Duration) OverrideRepository {
	return &cachedOverrideRepo{inner: inner, cache: c, ttl: ttl}
}

func overridesCacheKey(caseCtx domain.CaseContext) string {
	return fmt.Sprintf("%s:%s:%s:%s:%s:%s",
		overridesCachePattern,
		caseCtx.State,
		caseCtx.Client,
		caseCtx.Investor,
		caseCtx.CaseType,
		caseCtx.AsOfDate.Format("2006-01-02"),
	)
}

func (r *cachedOverrideRepo) FindMatchingOverrides(ctx context.Context, caseCtx domain.CaseContext) ([]domain.Override, error) {
	key := overridesCacheKey(caseCtx)

	if raw, found, err := r.cache.Get(ctx, key); err == nil && found {
		var overrides []domain.Override
		if jsonErr := json.Unmarshal(raw, &overrides); jsonErr == nil {
			log.WithField("key", key).Debug("cache HIT: resolve:overrides")
			return overrides, nil
		}
	}

	log.WithField("key", key).Debug("cache MISS: resolve:overrides")

	result, err := r.inner.FindMatchingOverrides(ctx, caseCtx)
	if err != nil {
		return nil, err
	}
	if raw, jsonErr := json.Marshal(result); jsonErr == nil {
		_ = r.cache.Set(ctx, key, raw, r.ttl)
	}
	return result, nil
}

func (r *cachedOverrideRepo) Create(ctx context.Context, o domain.Override) error {
	err := r.inner.Create(ctx, o)
	if err == nil {
		r.flushOverridesCache(ctx)
	}
	return err
}

func (r *cachedOverrideRepo) Update(ctx context.Context, o domain.Override) error {
	err := r.inner.Update(ctx, o)
	if err == nil {
		r.flushOverridesCache(ctx)
	}
	return err
}

func (r *cachedOverrideRepo) UpdateStatus(ctx context.Context, id, status, updatedBy string) error {
	err := r.inner.UpdateStatus(ctx, id, status, updatedBy)
	if err == nil {
		r.flushOverridesCache(ctx)
	}
	return err
}

func (r *cachedOverrideRepo) flushOverridesCache(ctx context.Context) {
	for _, pattern := range []string{overridesCachePattern + ":*", overridesCachePatternBulk + ":*"} {
		if err := r.cache.FlushByPattern(ctx, pattern); err != nil {
			log.WithField("pattern", pattern).WithError(err).Warn("failed to flush cache")
		} else {
			log.WithField("pattern", pattern).Debug("cache EVICTED (override mutation)")
		}
	}
}

func bulkCacheKey(contexts []domain.CaseContext) string {
	h := sha256.New()
	for _, c := range contexts {
		fmt.Fprintf(h, "%s:%s:%s:%s:%s\n",
			c.State, c.Client, c.Investor, c.CaseType,
			c.AsOfDate.Format("2006-01-02"))
	}
	return fmt.Sprintf("%s:%x", overridesCachePatternBulk, h.Sum(nil))
}

func (r *cachedOverrideRepo) FindMatchingOverridesBatch(ctx context.Context, contexts []domain.CaseContext) ([][]domain.Override, error) {
	key := bulkCacheKey(contexts)

	if raw, found, err := r.cache.Get(ctx, key); err == nil && found {
		var result [][]domain.Override
		if jsonErr := json.Unmarshal(raw, &result); jsonErr == nil {
			log.WithField("key", key).Debug("cache HIT: resolve:bulk")
			return result, nil
		}
	}

	log.WithField("key", key).Debug("cache MISS: resolve:bulk")

	result, err := r.inner.FindMatchingOverridesBatch(ctx, contexts)
	if err != nil {
		return nil, err
	}
	if raw, jsonErr := json.Marshal(result); jsonErr == nil {
		_ = r.cache.Set(ctx, key, raw, r.ttl)
	}
	return result, nil
}

// Pass-through methods (no caching needed)

func (r *cachedOverrideRepo) GetByID(ctx context.Context, id string) (*domain.Override, error) {
	return r.inner.GetByID(ctx, id)
}

func (r *cachedOverrideRepo) List(ctx context.Context, filter OverrideFilter) ([]domain.Override, int64, error) {
	return r.inner.List(ctx, filter)
}

func (r *cachedOverrideRepo) GetHistory(ctx context.Context, overrideID string) ([]domain.OverrideHistoryEntry, error) {
	return r.inner.GetHistory(ctx, overrideID)
}

func (r *cachedOverrideRepo) RecordHistory(ctx context.Context, entry domain.OverrideHistoryEntry) error {
	return r.inner.RecordHistory(ctx, entry)
}

func (r *cachedOverrideRepo) FindConflicts(ctx context.Context) ([]domain.ConflictPair, error) {
	return r.inner.FindConflicts(ctx)
}
