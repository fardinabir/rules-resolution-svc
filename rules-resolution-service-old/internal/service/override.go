package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/abir/rules-resolution-service/internal/domain"
	"github.com/abir/rules-resolution-service/internal/repository"
)

type CreateOverrideRequest struct {
	ID            string          `json:"id"`
	StepKey       string          `json:"stepKey"`
	TraitKey      string          `json:"traitKey"`
	Selector      domain.Selector `json:"selector"`
	Value         json.RawMessage `json:"value"`
	EffectiveDate string          `json:"effectiveDate"`
	ExpiresDate   *string         `json:"expiresDate"`
	Status        string          `json:"status"`
	Description   string          `json:"description"`
	CreatedBy     string          `json:"createdBy"`
}

type UpdateOverrideRequest struct {
	StepKey       string          `json:"stepKey"`
	TraitKey      string          `json:"traitKey"`
	Selector      domain.Selector `json:"selector"`
	Value         json.RawMessage `json:"value"`
	EffectiveDate string          `json:"effectiveDate"`
	ExpiresDate   *string         `json:"expiresDate"`
	Description   string          `json:"description"`
	UpdatedBy     string          `json:"updatedBy"`
}

type UpdateStatusRequest struct {
	Status    string `json:"status"`
	UpdatedBy string `json:"updatedBy"`
}

var validTraitKeys = map[string]bool{
	"slaHours": true, "requiredDocuments": true, "feeAmount": true,
	"feeAuthRequired": true, "assignedRole": true, "templateId": true,
}

var validTransitions = map[string][]string{
	"draft":    {"active", "archived"},
	"active":   {"archived"},
	"archived": {},
}

type OverrideService struct {
	repo repository.OverrideRepository
}

func NewOverrideService(repo repository.OverrideRepository) *OverrideService {
	return &OverrideService{repo: repo}
}

func (s *OverrideService) GetByID(ctx context.Context, id string) (*domain.Override, error) {
	return s.repo.GetByID(ctx, id)
}

func (s *OverrideService) List(ctx context.Context, f repository.OverrideFilter) ([]domain.Override, error) {
	return s.repo.List(ctx, f)
}

func (s *OverrideService) Create(ctx context.Context, req CreateOverrideRequest) (*domain.Override, error) {
	if err := validateStepTrait(req.StepKey, req.TraitKey); err != nil {
		return nil, err
	}
	effDate, expiresDate, err := parseDates(req.EffectiveDate, req.ExpiresDate)
	if err != nil {
		return nil, err
	}

	var val any
	if err := json.Unmarshal(req.Value, &val); err != nil {
		return nil, fmt.Errorf("invalid value: %w", err)
	}

	status := req.Status
	if status == "" {
		status = "draft"
	}

	now := time.Now().UTC()
	id := req.ID
	if id == "" {
		id = generateID()
	}

	o := domain.Override{
		ID:            id,
		StepKey:       req.StepKey,
		TraitKey:      req.TraitKey,
		Selector:      req.Selector,
		Specificity:   req.Selector.Specificity(), // always computed
		Value:         val,
		EffectiveDate: effDate,
		ExpiresDate:   expiresDate,
		Status:        status,
		Description:   req.Description,
		CreatedAt:     now,
		CreatedBy:     req.CreatedBy,
		UpdatedAt:     now,
		UpdatedBy:     req.CreatedBy,
	}

	if err := s.repo.Create(ctx, o); err != nil {
		return nil, err
	}

	snapshot := overrideToMap(o)
	_ = s.repo.RecordHistory(ctx, domain.OverrideHistoryEntry{
		OverrideID:     o.ID,
		Action:         "created",
		ChangedBy:      o.CreatedBy,
		ChangedAt:      now,
		SnapshotBefore: nil,
		SnapshotAfter:  snapshot,
	})

	return &o, nil
}

func (s *OverrideService) Update(ctx context.Context, id string, req UpdateOverrideRequest) (*domain.Override, error) {
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

	var val any
	if err := json.Unmarshal(req.Value, &val); err != nil {
		return nil, fmt.Errorf("invalid value: %w", err)
	}

	now := time.Now().UTC()
	o := *before
	o.StepKey = req.StepKey
	o.TraitKey = req.TraitKey
	o.Selector = req.Selector
	o.Specificity = req.Selector.Specificity()
	o.Value = val
	o.EffectiveDate = effDate
	o.ExpiresDate = expiresDate
	o.Description = req.Description
	o.UpdatedAt = now
	o.UpdatedBy = req.UpdatedBy

	if err := s.repo.Update(ctx, o); err != nil {
		return nil, err
	}

	_ = s.repo.RecordHistory(ctx, domain.OverrideHistoryEntry{
		OverrideID:     o.ID,
		Action:         "updated",
		ChangedBy:      req.UpdatedBy,
		ChangedAt:      now,
		SnapshotBefore: overrideToMap(*before),
		SnapshotAfter:  overrideToMap(o),
	})

	return &o, nil
}

func (s *OverrideService) UpdateStatus(ctx context.Context, id string, req UpdateStatusRequest) error {
	current, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return err
	}

	allowed := validTransitions[current.Status]
	valid := false
	for _, a := range allowed {
		if a == req.Status {
			valid = true
			break
		}
	}
	if !valid {
		return fmt.Errorf("invalid status transition: %s → %s", current.Status, req.Status)
	}

	now := time.Now().UTC()
	if err := s.repo.UpdateStatus(ctx, id, req.Status, req.UpdatedBy); err != nil {
		return err
	}

	after := *current
	after.Status = req.Status
	after.UpdatedAt = now
	after.UpdatedBy = req.UpdatedBy

	_ = s.repo.RecordHistory(ctx, domain.OverrideHistoryEntry{
		OverrideID:     id,
		Action:         "status_changed",
		ChangedBy:      req.UpdatedBy,
		ChangedAt:      now,
		SnapshotBefore: overrideToMap(*current),
		SnapshotAfter:  overrideToMap(after),
	})

	return nil
}

func (s *OverrideService) GetHistory(ctx context.Context, id string) ([]domain.OverrideHistoryEntry, error) {
	return s.repo.GetHistory(ctx, id)
}

// GetConflicts detects overrides that conflict with each other.
// Two overrides conflict when they target the same step/trait, have the same specificity,
// overlapping effective date ranges, and compatible selectors (no pinned dimension disagrees).
func (s *OverrideService) GetConflicts(ctx context.Context) ([]domain.ConflictPair, error) {
	overrides, err := s.repo.FindConflictCandidates(ctx)
	if err != nil {
		return nil, err
	}

	// Group by (step_key, trait_key, specificity)
	type groupKey struct {
		StepKey     string
		TraitKey    string
		Specificity int
	}
	groups := make(map[groupKey][]domain.Override)
	for _, o := range overrides {
		k := groupKey{o.StepKey, o.TraitKey, o.Specificity}
		groups[k] = append(groups[k], o)
	}

	var conflicts []domain.ConflictPair
	for _, group := range groups {
		for i := 0; i < len(group); i++ {
			for j := i + 1; j < len(group); j++ {
				a, b := group[i], group[j]
				if datesOverlap(a, b) && selectorsCompatible(a.Selector, b.Selector) {
					conflicts = append(conflicts, domain.ConflictPair{
						OverrideA: a.ID,
						OverrideB: b.ID,
						StepKey:   a.StepKey,
						TraitKey:  a.TraitKey,
						Reason: fmt.Sprintf(
							"Same step/trait (%s/%s), same specificity (%d), overlapping effective dates, compatible selectors %s and %s",
							a.StepKey, a.TraitKey, a.Specificity,
							formatSelector(a.Selector), formatSelector(b.Selector),
						),
					})
				}
			}
		}
	}
	return conflicts, nil
}

// datesOverlap returns true if the date ranges [a.EffectiveDate, a.ExpiresDate) and
// [b.EffectiveDate, b.ExpiresDate) overlap. A nil ExpiresDate means open-ended (infinity).
func datesOverlap(a, b domain.Override) bool {
	maxDate := time.Date(9999, 12, 31, 0, 0, 0, 0, time.UTC)
	aEnd := maxDate
	if a.ExpiresDate != nil {
		aEnd = *a.ExpiresDate
	}
	bEnd := maxDate
	if b.ExpiresDate != nil {
		bEnd = *b.ExpiresDate
	}
	return a.EffectiveDate.Before(bEnd) && b.EffectiveDate.Before(aEnd)
}

// selectorsCompatible returns true if there is no dimension where both overrides
// pin a different value — meaning a case exists that would match both.
func selectorsCompatible(a, b domain.Selector) bool {
	if a.State != nil && b.State != nil && *a.State != *b.State {
		return false
	}
	if a.Client != nil && b.Client != nil && *a.Client != *b.Client {
		return false
	}
	if a.Investor != nil && b.Investor != nil && *a.Investor != *b.Investor {
		return false
	}
	if a.CaseType != nil && b.CaseType != nil && *a.CaseType != *b.CaseType {
		return false
	}
	return true
}

func formatSelector(s domain.Selector) string {
	parts := []string{}
	if s.State != nil {
		parts = append(parts, "state: "+*s.State)
	}
	if s.Client != nil {
		parts = append(parts, "client: "+*s.Client)
	}
	if s.Investor != nil {
		parts = append(parts, "investor: "+*s.Investor)
	}
	if s.CaseType != nil {
		parts = append(parts, "caseType: "+*s.CaseType)
	}
	if len(parts) == 0 {
		return "{}"
	}
	return "{" + strings.Join(parts, ", ") + "}"
}

// --- helpers ---

func validateStepTrait(stepKey, traitKey string) error {
	validStep := false
	for _, s := range domain.AllSteps {
		if s == stepKey {
			validStep = true
			break
		}
	}
	if !validStep {
		return fmt.Errorf("unknown stepKey: %s", stepKey)
	}
	if !validTraitKeys[traitKey] {
		return fmt.Errorf("unknown traitKey: %s", traitKey)
	}
	return nil
}

func parseDates(effectiveDate string, expiresDate *string) (time.Time, *time.Time, error) {
	effDate, err := time.Parse("2006-01-02", effectiveDate)
	if err != nil {
		return time.Time{}, nil, fmt.Errorf("invalid effectiveDate: %w", err)
	}
	var expDate *time.Time
	if expiresDate != nil {
		t, err := time.Parse("2006-01-02", *expiresDate)
		if err != nil {
			return time.Time{}, nil, fmt.Errorf("invalid expiresDate: %w", err)
		}
		if !t.After(effDate) {
			return time.Time{}, nil, errors.New("expiresDate must be after effectiveDate")
		}
		expDate = &t
	}
	return effDate, expDate, nil
}

func generateID() string {
	return fmt.Sprintf("ovr-%d", time.Now().UnixNano())
}

func overrideToMap(o domain.Override) map[string]any {
	m := map[string]any{
		"id":            o.ID,
		"stepKey":       o.StepKey,
		"traitKey":      o.TraitKey,
		"selector":      o.Selector,
		"specificity":   o.Specificity,
		"value":         o.Value,
		"effectiveDate": o.EffectiveDate.Format("2006-01-02"),
		"status":        o.Status,
		"description":   o.Description,
		"createdAt":     o.CreatedAt,
		"createdBy":     o.CreatedBy,
		"updatedAt":     o.UpdatedAt,
		"updatedBy":     o.UpdatedBy,
	}
	if o.ExpiresDate != nil {
		m["expiresDate"] = o.ExpiresDate.Format("2006-01-02")
	}
	return m
}
