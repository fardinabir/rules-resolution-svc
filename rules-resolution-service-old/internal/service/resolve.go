package service

import (
	"context"
	"time"

	"github.com/abir/rules-resolution-service/internal/domain"
	"github.com/abir/rules-resolution-service/internal/repository"
)

type ResolveService struct {
	overrides repository.OverrideRepository
	defaults  repository.DefaultRepository
}

func NewResolveService(overrides repository.OverrideRepository, defaults repository.DefaultRepository) *ResolveService {
	return &ResolveService{overrides: overrides, defaults: defaults}
}

func (s *ResolveService) Resolve(ctx context.Context, req domain.CaseContext) (*domain.ResolvedConfig, error) {
	if req.AsOfDate.IsZero() {
		req.AsOfDate = time.Now().UTC()
	}
	candidates, err := s.overrides.FindMatchingOverrides(ctx, req)
	if err != nil {
		return nil, err
	}
	defaults, err := s.defaults.GetAll(ctx)
	if err != nil {
		return nil, err
	}
	result := domain.Resolve(req, candidates, defaults)
	return &result, nil
}

func (s *ResolveService) Explain(ctx context.Context, req domain.CaseContext) (*domain.ExplainResult, error) {
	if req.AsOfDate.IsZero() {
		req.AsOfDate = time.Now().UTC()
	}
	candidates, err := s.overrides.FindMatchingOverrides(ctx, req)
	if err != nil {
		return nil, err
	}
	defaults, err := s.defaults.GetAll(ctx)
	if err != nil {
		return nil, err
	}
	result := domain.Explain(req, candidates, defaults)
	return &result, nil
}
