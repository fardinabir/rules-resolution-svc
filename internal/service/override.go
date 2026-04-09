package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/fardinabir/rules-resolution-svc/internal/domain"
	"github.com/fardinabir/rules-resolution-svc/internal/repository"
)

// OverrideService handles override CRUD, validation, and conflict detection.
type OverrideService interface {
	GetByID(ctx context.Context, id string) (*domain.Override, error)
	List(ctx context.Context, filter OverrideFilter) ([]domain.Override, int64, error)
	Create(ctx context.Context, req CreateOverrideRequest, actor string) (*domain.Override, error)
	Update(ctx context.Context, id string, req UpdateOverrideRequest, actor string) (*domain.Override, error)
	UpdateStatus(ctx context.Context, id, newStatus, actor string) error
	GetHistory(ctx context.Context, overrideID string) ([]domain.OverrideHistoryEntry, error)
	GetConflicts(ctx context.Context) ([]domain.ConflictPair, error)
}

// CreateOverrideRequest is the input DTO for creating an override.
type CreateOverrideRequest struct {
	StepKey       string          `json:"stepKey"       example:"file-complaint"`
	TraitKey      string          `json:"traitKey"      example:"slaHours"`
	Selector      domain.Selector `json:"selector"`
	Value         json.RawMessage `json:"value"         swaggertype:"string" example:"240"`
	EffectiveDate string          `json:"effectiveDate" example:"2025-06-01"`
	ExpiresDate   *string         `json:"expiresDate,omitempty" example:"2026-06-01"`
	Status        string          `json:"status"        example:"active"`
	Description   string          `json:"description"   example:"Chase FL filing — 10-day deadline"`
}

// UpdateOverrideRequest is the input DTO for updating an override.
type UpdateOverrideRequest struct {
	StepKey       string          `json:"stepKey"       example:"serve-borrower"`
	TraitKey      string          `json:"traitKey"      example:"assignedRole"`
	Selector      domain.Selector `json:"selector"`
	Value         json.RawMessage `json:"value"         swaggertype:"string" example:"\"attorney\""`
	EffectiveDate string          `json:"effectiveDate" example:"2025-06-01"`
	ExpiresDate   *string         `json:"expiresDate,omitempty" example:"2026-06-01"`
	Description   string          `json:"description"   example:"Chase FL filing — updated deadline"`
}

// OverrideFilter holds optional query parameters for listing overrides.
// Defined here so callers (controllers) don't need to import the repository package.
type OverrideFilter struct {
	StepKey  *string
	TraitKey *string
	State    *string
	Client   *string
	Investor *string
	CaseType *string
	Status   *string
	Page     int
	PageSize int
}

var validTransitions = map[string][]string{
	"draft":    {"active", "archived"},
	"active":   {"archived", "draft"},
	"archived": {"active", "draft"},
}

type overrideService struct {
	repo repository.OverrideRepository
}

func NewOverrideService(repo repository.OverrideRepository) OverrideService {
	return &overrideService{repo: repo}
}

func (s *overrideService) GetByID(ctx context.Context, id string) (*domain.Override, error) {
	return s.repo.GetByID(ctx, id)
}

func (s *overrideService) List(ctx context.Context, filter OverrideFilter) ([]domain.Override, int64, error) {
	repoFilter := repository.OverrideFilter{
		StepKey:  filter.StepKey,
		TraitKey: filter.TraitKey,
		State:    filter.State,
		Client:   filter.Client,
		Investor: filter.Investor,
		CaseType: filter.CaseType,
		Status:   filter.Status,
		Page:     filter.Page,
		PageSize: filter.PageSize,
	}
	return s.repo.List(ctx, repoFilter)
}

func (s *overrideService) Create(ctx context.Context, req CreateOverrideRequest, actor string) (*domain.Override, error) {
	if err := validateStepTrait(req.StepKey, req.TraitKey); err != nil {
		return nil, err
	}
	effDate, expiresDate, err := parseDates(req.EffectiveDate, req.ExpiresDate)
	if err != nil {
		return nil, err
	}
	var rawVal any
	if err := json.Unmarshal(req.Value, &rawVal); err != nil {
		return nil, fmt.Errorf("invalid value JSON: %w", err)
	}
	normalizedVal, err := domain.NormalizeTraitValue(req.TraitKey, rawVal)
	if err != nil {
		return nil, fmt.Errorf("value type mismatch: %w", err)
	}
	status := req.Status
	if status == "" {
		status = "draft"
	}
	now := time.Now().UTC()
	o := domain.Override{
		ID:            "ovr-" + uuid.New().String()[:8],
		StepKey:       req.StepKey,
		TraitKey:      req.TraitKey,
		Selector:      req.Selector,
		Specificity:   req.Selector.Specificity(),
		Value:         normalizedVal,
		EffectiveDate: effDate,
		ExpiresDate:   expiresDate,
		Status:        status,
		Description:   req.Description,
		CreatedAt:     now,
		CreatedBy:     actor,
		UpdatedAt:     now,
		UpdatedBy:     actor,
	}
	if err := s.repo.Create(ctx, o); err != nil {
		return nil, err
	}
	if err := s.repo.RecordHistory(ctx, domain.OverrideHistoryEntry{
		OverrideID:    o.ID,
		Action:        "created",
		ChangedBy:     actor,
		ChangedAt:     now,
		SnapshotAfter: overrideToMap(o),
	}); err != nil {
		return nil, err
	}
	return &o, nil
}

func (s *overrideService) Update(ctx context.Context, id string, req UpdateOverrideRequest, actor string) (*domain.Override, error) {
	before, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := validateStepTrait(req.StepKey, req.TraitKey); err != nil {
		return nil, err
	}
	effDate, expiresDate, err := parseDates(req.EffectiveDate, req.ExpiresDate)
	if err != nil {
		return nil, err
	}
	var rawVal any
	if err := json.Unmarshal(req.Value, &rawVal); err != nil {
		return nil, fmt.Errorf("invalid value JSON: %w", err)
	}
	normalizedVal, err := domain.NormalizeTraitValue(req.TraitKey, rawVal)
	if err != nil {
		return nil, fmt.Errorf("value type mismatch: %w", err)
	}
	beforeMap := overrideToMap(*before)
	now := time.Now().UTC()
	updated := *before
	updated.StepKey = req.StepKey
	updated.TraitKey = req.TraitKey
	updated.Selector = req.Selector
	updated.Specificity = req.Selector.Specificity()
	updated.Value = normalizedVal
	updated.EffectiveDate = effDate
	updated.ExpiresDate = expiresDate
	updated.Description = req.Description
	updated.UpdatedAt = now
	updated.UpdatedBy = actor

	if err := s.repo.Update(ctx, updated); err != nil {
		return nil, err
	}
	if err := s.repo.RecordHistory(ctx, domain.OverrideHistoryEntry{
		OverrideID:     id,
		Action:         "updated",
		ChangedBy:      actor,
		ChangedAt:      now,
		SnapshotBefore: beforeMap,
		SnapshotAfter:  overrideToMap(updated),
	}); err != nil {
		return nil, err
	}
	return &updated, nil
}

func (s *overrideService) UpdateStatus(ctx context.Context, id, newStatus, actor string) error {
	current, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return err
	}
	valid := validTransitions[current.Status]
	allowed := false
	for _, v := range valid {
		if v == newStatus {
			allowed = true
			break
		}
	}
	if !allowed {
		return fmt.Errorf("invalid status transition: %s → %s (allowed: %v)",
			current.Status, newStatus, valid)
	}
	beforeMap := overrideToMap(*current)
	if err := s.repo.UpdateStatus(ctx, id, newStatus, actor); err != nil {
		return err
	}
	now := time.Now().UTC()
	current.Status = newStatus
	return s.repo.RecordHistory(ctx, domain.OverrideHistoryEntry{
		OverrideID:     id,
		Action:         "status_changed",
		ChangedBy:      actor,
		ChangedAt:      now,
		SnapshotBefore: beforeMap,
		SnapshotAfter:  overrideToMap(*current),
	})
}

func (s *overrideService) GetHistory(ctx context.Context, overrideID string) ([]domain.OverrideHistoryEntry, error) {
	return s.repo.GetHistory(ctx, overrideID)
}

func (s *overrideService) GetConflicts(ctx context.Context) ([]domain.ConflictPair, error) {
	return s.repo.FindConflicts(ctx)
}

func validateStepTrait(stepKey, traitKey string) error {
	validStep := false
	for _, s := range domain.AllSteps {
		if s == stepKey {
			validStep = true
			break
		}
	}
	if !validStep {
		return fmt.Errorf("unknown stepKey %q", stepKey)
	}
	if _, ok := domain.ValidTraitKeys[traitKey]; !ok {
		return fmt.Errorf("unknown traitKey %q", traitKey)
	}
	return nil
}

func parseDates(effStr string, expStr *string) (time.Time, *time.Time, error) {
	eff, err := time.Parse("2006-01-02", effStr)
	if err != nil {
		return time.Time{}, nil, fmt.Errorf("invalid effectiveDate %q: must be YYYY-MM-DD", effStr)
	}
	if expStr == nil {
		return eff, nil, nil
	}
	exp, err := time.Parse("2006-01-02", *expStr)
	if err != nil {
		return time.Time{}, nil, fmt.Errorf("invalid expiresDate %q: must be YYYY-MM-DD", *expStr)
	}
	if !exp.After(eff) {
		return time.Time{}, nil, fmt.Errorf("expiresDate must be after effectiveDate")
	}
	return eff, &exp, nil
}

func overrideToMap(o domain.Override) map[string]any {
	m := map[string]any{
		"id": o.ID, "stepKey": o.StepKey, "traitKey": o.TraitKey,
		"specificity": o.Specificity, "value": o.Value,
		"effectiveDate": o.EffectiveDate.Format("2006-01-02"),
		"status":        o.Status, "description": o.Description,
		"createdAt": o.CreatedAt, "createdBy": o.CreatedBy,
		"updatedAt": o.UpdatedAt, "updatedBy": o.UpdatedBy,
	}
	if o.ExpiresDate != nil {
		m["expiresDate"] = o.ExpiresDate.Format("2006-01-02")
	}
	sel := map[string]any{}
	if o.Selector.State != nil {
		sel["state"] = *o.Selector.State
	}
	if o.Selector.Client != nil {
		sel["client"] = *o.Selector.Client
	}
	if o.Selector.Investor != nil {
		sel["investor"] = *o.Selector.Investor
	}
	if o.Selector.CaseType != nil {
		sel["caseType"] = *o.Selector.CaseType
	}
	m["selector"] = sel
	return m
}
