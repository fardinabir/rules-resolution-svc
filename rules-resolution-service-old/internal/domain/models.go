package domain

import "time"

var AllSteps = []string{
	"title-search", "file-complaint", "serve-borrower",
	"obtain-judgment", "schedule-sale", "conduct-sale",
}

var AllTraits = []string{
	"slaHours", "requiredDocuments", "feeAmount",
	"feeAuthRequired", "assignedRole", "templateId",
}

type CaseContext struct {
	State    string    `json:"state"`
	Client   string    `json:"client"`
	Investor string    `json:"investor"`
	CaseType string    `json:"caseType"`
	AsOfDate time.Time `json:"asOfDate,omitempty"`
}

type Selector struct {
	State    *string `json:"state,omitempty"`
	Client   *string `json:"client,omitempty"`
	Investor *string `json:"investor,omitempty"`
	CaseType *string `json:"caseType,omitempty"`
}

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

type Override struct {
	ID            string
	StepKey       string
	TraitKey      string
	Selector      Selector
	Specificity   int
	Value         any
	EffectiveDate time.Time
	ExpiresDate   *time.Time
	Status        string
	Description   string
	CreatedAt     time.Time
	CreatedBy     string
	UpdatedAt     time.Time
	UpdatedBy     string
}

type StepTrait struct {
	StepKey  string
	TraitKey string
}

type Step struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Position    int    `json:"position"`
}

type ResolvedConfig struct {
	Context    CaseContext              `json:"context"`
	ResolvedAt time.Time               `json:"resolvedAt"`
	Steps      map[string]ResolvedStep `json:"steps"`
}

type ResolvedStep map[string]ResolvedTrait

type ResolvedTrait struct {
	Value        any    `json:"value"`
	Source       string `json:"source"`
	OverrideID   string `json:"overrideId,omitempty"`
	Conflict     bool   `json:"conflict,omitempty"`
	ConflictWith string `json:"conflictsWith,omitempty"`
}

type ExplainResult struct {
	Context    CaseContext  `json:"context"`
	ResolvedAt time.Time   `json:"resolvedAt"`
	Traces     []TraitTrace `json:"traces"`
}

type TraitTrace struct {
	Step          string           `json:"step"`
	Trait         string           `json:"trait"`
	ResolvedValue any              `json:"resolvedValue"`
	ResolvedFrom  *TraceSource     `json:"resolvedFrom"`
	Candidates    []TraceCandidate `json:"candidates"`
}

type TraceSource struct {
	OverrideID    string    `json:"overrideId"`
	Selector      Selector  `json:"selector"`
	Specificity   int       `json:"specificity"`
	EffectiveDate time.Time `json:"effectiveDate"`
}

type TraceCandidate struct {
	OverrideID    string    `json:"overrideId"`
	Selector      Selector  `json:"selector"`
	Specificity   int       `json:"specificity"`
	EffectiveDate time.Time `json:"effectiveDate"`
	Value         any       `json:"value"`
	Outcome       string    `json:"outcome"`
}

type OverrideHistoryEntry struct {
	ID             int64
	OverrideID     string
	Action         string
	ChangedBy      string
	ChangedAt      time.Time
	SnapshotBefore any
	SnapshotAfter  any
}

type ConflictPair struct {
	OverrideA string `json:"overrideA"`
	OverrideB string `json:"overrideB"`
	StepKey   string `json:"stepKey"`
	TraitKey  string `json:"traitKey"`
	Reason    string `json:"reason"`
}
