package domain

import "time"

// AllSteps and AllTraits define the canonical 6×6 resolution grid.
var AllSteps = []string{
	"title-search", "file-complaint", "serve-borrower",
	"obtain-judgment", "schedule-sale", "conduct-sale",
}
var AllTraits = []string{
	"slaHours", "requiredDocuments", "feeAmount",
	"feeAuthRequired", "assignedRole", "templateId",
}

// CaseContext is the query context for a resolution request.
type CaseContext struct {
	State    string    `json:"state"`
	Client   string    `json:"client"`
	Investor string    `json:"investor"`
	CaseType string    `json:"caseType"`
	AsOfDate time.Time `json:"asOfDate,omitempty"` // defaults to time.Now() if zero
}

// Selector holds the 0–4 pinned dimensions of an override rule.
// A nil field is a wildcard — it matches any value in that dimension.
type Selector struct {
	State    *string `json:"state,omitempty"`
	Client   *string `json:"client,omitempty"`
	Investor *string `json:"investor,omitempty"`
	CaseType *string `json:"caseType,omitempty"`
}

// Specificity returns the count of non-nil selector dimensions (0–4).
func (s Selector) Specificity() int {
	n := 0
	if s.State != nil {
		n++
	}
	if s.Client != nil {
		n++
	}
	if s.Investor != nil {
		n++
	}
	if s.CaseType != nil {
		n++
	}
	return n
}

// Matches returns true when every pinned dimension of s equals the corresponding
// field in ctx. Nil dimensions are wildcards and always match.
func (s Selector) Matches(ctx CaseContext) bool {
	if s.State != nil && *s.State != ctx.State {
		return false
	}
	if s.Client != nil && *s.Client != ctx.Client {
		return false
	}
	if s.Investor != nil && *s.Investor != ctx.Investor {
		return false
	}
	if s.CaseType != nil && *s.CaseType != ctx.CaseType {
		return false
	}
	return true
}

// StepTrait is the composite key used to group overrides and defaults.
type StepTrait struct {
	StepKey  string
	TraitKey string
}

// Override represents a single configuration rule stored in the overrides table.
type Override struct {
	ID            string     `json:"id"`
	StepKey       string     `json:"stepKey"`
	TraitKey      string     `json:"traitKey"`
	Selector      Selector   `json:"selector"`
	Specificity   int        `json:"specificity"`
	Value         any        `json:"value"` // int64, bool, string, []string — determined by TraitKey
	EffectiveDate time.Time  `json:"effectiveDate"`
	ExpiresDate   *time.Time `json:"expiresDate"`
	Status        string     `json:"status"`
	Description   string     `json:"description"`
	CreatedAt     time.Time  `json:"createdAt"`
	CreatedBy     string     `json:"createdBy"`
	UpdatedAt     time.Time  `json:"updatedAt"`
	UpdatedBy     string     `json:"updatedBy"`
}

// Step is a foreclosure process step (reference data).
type Step struct {
	Key         string
	Name        string
	Description string
	Position    int
}

// ResolvedConfig is the full 6×6 resolution output.
type ResolvedConfig struct {
	Context    CaseContext             `json:"context"`
	ResolvedAt time.Time               `json:"resolvedAt"`
	Steps      map[string]ResolvedStep `json:"steps"`
}

// ResolvedStep maps traitKey → resolved trait for one step.
type ResolvedStep map[string]ResolvedTrait

// ResolvedTrait holds the winning value and its source.
type ResolvedTrait struct {
	Value      any    `json:"value"`
	Source     string `json:"source"` // "default" or "override"
	OverrideID string `json:"overrideId,omitempty"`
}

// TraitTrace contains the resolution trace for one step/trait cell.
type TraitTrace struct {
	Step          string           `json:"step"`
	Trait         string           `json:"trait"`
	ResolvedValue any              `json:"resolvedValue"`
	ResolvedFrom  *TraceSource     `json:"resolvedFrom"` // nil = resolved from default
	Candidates    []TraceCandidate `json:"candidates"`
}

// TraceSource identifies which override won.
type TraceSource struct {
	OverrideID    string    `json:"overrideId"`
	Selector      Selector  `json:"selector"`
	Specificity   int       `json:"specificity"`
	EffectiveDate time.Time `json:"effectiveDate"`
}

// TraceCandidate is one override that was evaluated for this step/trait.
type TraceCandidate struct {
	OverrideID    string    `json:"overrideId"`
	Selector      Selector  `json:"selector"`
	Specificity   int       `json:"specificity"`
	EffectiveDate time.Time `json:"effectiveDate"`
	Value         any       `json:"value"`
	Outcome       string    `json:"outcome"` // "SELECTED — ...", "SHADOWED — ..."
}

// OverrideHistoryEntry is one entry in the audit ledger for an override.
type OverrideHistoryEntry struct {
	ID             int64          `json:"id"`
	OverrideID     string         `json:"overrideId"`
	Action         string         `json:"action"` // "created", "updated", "status_changed"
	ChangedBy      string         `json:"changedBy"`
	ChangedAt      time.Time      `json:"changedAt"`
	SnapshotBefore map[string]any `json:"snapshotBefore,omitempty"` // nil for "created"
	SnapshotAfter  map[string]any `json:"snapshotAfter,omitempty"`
}

// ConflictPair describes two overrides that conflict with each other.
type ConflictPair struct {
	OverrideA string `json:"overrideA"`
	OverrideB string `json:"overrideB"`
	StepKey   string `json:"stepKey"`
	TraitKey  string `json:"traitKey"`
	Reason    string `json:"reason"`
}
