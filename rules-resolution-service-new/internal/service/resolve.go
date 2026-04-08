package service

import (
	"context"
	"time"

	"github.com/fardinabir/go-svc-boilerplate/internal/domain"
	"github.com/fardinabir/go-svc-boilerplate/internal/repository"
)

// ResolveService orchestrates resolution: fetch overrides + defaults, run pure resolver.
type ResolveService interface {
	Resolve(ctx context.Context, caseCtx domain.CaseContext) (*domain.ResolvedConfig, error)
	Explain(ctx context.Context, caseCtx domain.CaseContext) ([]domain.TraitTrace, error)
	ResolveBulk(ctx context.Context, contexts []domain.CaseContext) ([]domain.ResolvedConfig, error)
}

type resolveService struct {
	overrides repository.OverrideRepository
	defaults  repository.DefaultRepository
}

func NewResolveService(overrides repository.OverrideRepository, defaults repository.DefaultRepository) ResolveService {
	return &resolveService{overrides: overrides, defaults: defaults}
}

func (s *resolveService) Resolve(ctx context.Context, caseCtx domain.CaseContext) (*domain.ResolvedConfig, error) {
	if caseCtx.AsOfDate.IsZero() {
		caseCtx.AsOfDate = time.Now().UTC()
	}
	candidates, err := s.overrides.FindMatchingOverrides(ctx, caseCtx)
	if err != nil {
		return nil, err
	}
	defaults, err := s.defaults.GetAll(ctx)
	if err != nil {
		return nil, err
	}
	result := domain.Resolve(caseCtx, candidates, defaults)
	return &result, nil
}

func (s *resolveService) Explain(ctx context.Context, caseCtx domain.CaseContext) ([]domain.TraitTrace, error) {
	if caseCtx.AsOfDate.IsZero() {
		caseCtx.AsOfDate = time.Now().UTC()
	}
	candidates, err := s.overrides.FindMatchingOverrides(ctx, caseCtx)
	if err != nil {
		return nil, err
	}
	defaults, err := s.defaults.GetAll(ctx)
	if err != nil {
		return nil, err
	}
	return domain.Explain(candidates, defaults), nil
}

func (s *resolveService) ResolveBulk(ctx context.Context, contexts []domain.CaseContext) ([]domain.ResolvedConfig, error) {
	now := time.Now().UTC()
	for i := range contexts {
		if contexts[i].AsOfDate.IsZero() {
			contexts[i].AsOfDate = now
		}
	}

	allOverrides, err := s.overrides.FindMatchingOverridesBatch(ctx, contexts)
	if err != nil {
		return nil, err
	}
	defaults, err := s.defaults.GetAll(ctx)
	if err != nil {
		return nil, err
	}

	results := make([]domain.ResolvedConfig, len(contexts))
	for i, caseCtx := range contexts {
		results[i] = domain.Resolve(caseCtx, allOverrides[i], defaults)
	}
	return results, nil
}
