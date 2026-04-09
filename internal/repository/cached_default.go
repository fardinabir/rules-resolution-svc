package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/fardinabir/rules-resolution-svc/internal/cache"
	"github.com/fardinabir/rules-resolution-svc/internal/domain"
	log "github.com/sirupsen/logrus"
)

const defaultsCacheKey = "resolve:defaults:all"

// cachedDefaultRepo wraps a DefaultRepository with a Redis cache.
// The defaults table is effectively static reference data, so a long TTL is appropriate.
type cachedDefaultRepo struct {
	inner DefaultRepository
	cache cache.Cache
	ttl   time.Duration
}

// NewCachedDefaultRepository returns a DefaultRepository that caches GetAll.
func NewCachedDefaultRepository(inner DefaultRepository, c cache.Cache, ttl time.Duration) DefaultRepository {
	return &cachedDefaultRepo{inner: inner, cache: c, ttl: ttl}
}

func (r *cachedDefaultRepo) GetAll(ctx context.Context) (map[domain.StepTrait]any, error) {
	if raw, found, err := r.cache.Get(ctx, defaultsCacheKey); err == nil && found {
		if result, decErr := decodeDefaultsMap(raw); decErr == nil {
			log.Debug("cache HIT: resolve:defaults:all")
			return result, nil
		}
	}

	log.Debug("cache MISS: resolve:defaults:all")

	result, err := r.inner.GetAll(ctx)
	if err != nil {
		return nil, err
	}
	if raw, encErr := encodeDefaultsMap(result); encErr == nil {
		_ = r.cache.Set(ctx, defaultsCacheKey, raw, r.ttl)
	}
	return result, nil
}

// encodeDefaultsMap serialises map[StepTrait]any → JSON using "step:trait" string keys.
func encodeDefaultsMap(m map[domain.StepTrait]any) ([]byte, error) {
	flat := make(map[string]any, len(m))
	for k, v := range m {
		flat[k.StepKey+":"+k.TraitKey] = v
	}
	return json.Marshal(flat)
}

// decodeDefaultsMap deserialises JSON produced by encodeDefaultsMap back to map[StepTrait]any.
// Values are kept as their JSON-decoded types (float64 for numbers, []interface{} for arrays, etc.)
// and then normalised via domain.NormalizeTraitValue.
func decodeDefaultsMap(raw []byte) (map[domain.StepTrait]any, error) {
	var flat map[string]any
	if err := json.Unmarshal(raw, &flat); err != nil {
		return nil, err
	}
	result := make(map[domain.StepTrait]any, len(flat))
	for key, val := range flat {
		parts := strings.SplitN(key, ":", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("malformed defaults cache key %q", key)
		}
		st := domain.StepTrait{StepKey: parts[0], TraitKey: parts[1]}
		normalized, err := domain.NormalizeTraitValue(st.TraitKey, val)
		if err != nil {
			normalized = val
		}
		result[st] = normalized
	}
	return result, nil
}
