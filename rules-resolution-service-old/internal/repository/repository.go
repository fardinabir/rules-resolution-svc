package repository

import (
	"context"

	"github.com/abir/rules-resolution-service/internal/domain"
)

type OverrideFilter struct {
	StepKey  *string
	TraitKey *string
	State    *string
	Client   *string
	Investor *string
	CaseType *string
	Status   *string
}

type OverrideRepository interface {
	FindMatchingOverrides(ctx context.Context, caseCtx domain.CaseContext) ([]domain.Override, error)
	GetByID(ctx context.Context, id string) (*domain.Override, error)
	List(ctx context.Context, f OverrideFilter) ([]domain.Override, error)
	Create(ctx context.Context, o domain.Override) error
	Update(ctx context.Context, o domain.Override) error
	UpdateStatus(ctx context.Context, id, status, updatedBy string) error
	GetHistory(ctx context.Context, overrideID string) ([]domain.OverrideHistoryEntry, error)
	RecordHistory(ctx context.Context, e domain.OverrideHistoryEntry) error
	FindConflictCandidates(ctx context.Context) ([]domain.Override, error)
}

type DefaultRepository interface {
	GetAll(ctx context.Context) (map[domain.StepTrait]any, error)
}

type StepRepository interface {
	GetAll(ctx context.Context) ([]domain.Step, error)
}
