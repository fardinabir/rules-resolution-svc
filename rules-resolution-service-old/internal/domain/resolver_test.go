package domain

import (
	"encoding/json"
	"os"
	"sort"
	"testing"
	"time"
)

// helpers

func strPtr(s string) *string { return &s }

func mustDate(s string) time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return t
}

func defaultsMap() map[StepTrait]any {
	data, err := os.ReadFile("../../../sr_backend_assignment_data/defaults.json")
	if err != nil {
		panic(err)
	}
	var raw map[string]map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		panic(err)
	}
	m := make(map[StepTrait]any)
	for stepKey, traits := range raw {
		for traitKey, val := range traits {
			var v any
			_ = json.Unmarshal(val, &v)
			m[StepTrait{StepKey: stepKey, TraitKey: traitKey}] = v
		}
	}
	return m
}

// loadOverrides reads overrides.json and filters to those matching the context + asOfDate,
// returning them sorted as the DB would (specificity DESC, effective_date DESC).
func loadAndFilterOverrides(ctx CaseContext) []Override {
	data, err := os.ReadFile("../../../sr_backend_assignment_data/overrides.json")
	if err != nil {
		panic(err)
	}

	type rawOverride struct {
		ID            string          `json:"id"`
		StepKey       string          `json:"stepKey"`
		TraitKey      string          `json:"traitKey"`
		Selector      json.RawMessage `json:"selector"`
		Value         json.RawMessage `json:"value"`
		EffectiveDate string          `json:"effectiveDate"`
		ExpiresDate   *string         `json:"expiresDate"`
		Status        string          `json:"status"`
	}

	var raws []rawOverride
	if err := json.Unmarshal(data, &raws); err != nil {
		panic(err)
	}

	asOf := ctx.AsOfDate
	if asOf.IsZero() {
		asOf = time.Now()
	}

	var result []Override
	for _, r := range raws {
		if r.Status != "active" {
			continue
		}
		effDate := mustDate(r.EffectiveDate)
		if effDate.After(asOf) {
			continue
		}
		if r.ExpiresDate != nil {
			expDate := mustDate(*r.ExpiresDate)
			if !expDate.After(asOf) {
				continue
			}
		}

		var sel Selector
		_ = json.Unmarshal(r.Selector, &sel)

		// Check selector match
		if sel.State != nil && *sel.State != ctx.State {
			continue
		}
		if sel.Client != nil && *sel.Client != ctx.Client {
			continue
		}
		if sel.Investor != nil && *sel.Investor != ctx.Investor {
			continue
		}
		if sel.CaseType != nil && *sel.CaseType != ctx.CaseType {
			continue
		}

		var val any
		_ = json.Unmarshal(r.Value, &val)

		result = append(result, Override{
			ID:            r.ID,
			StepKey:       r.StepKey,
			TraitKey:      r.TraitKey,
			Selector:      sel,
			Specificity:   sel.Specificity(),
			Value:         val,
			EffectiveDate: effDate,
		})
	}

	// Sort exactly as the DB ORDER BY: step_key ASC, trait_key ASC, specificity DESC, effective_date DESC
	sort.SliceStable(result, func(i, j int) bool {
		a, b := result[i], result[j]
		if a.StepKey != b.StepKey {
			return a.StepKey < b.StepKey
		}
		if a.TraitKey != b.TraitKey {
			return a.TraitKey < b.TraitKey
		}
		if a.Specificity != b.Specificity {
			return a.Specificity > b.Specificity // DESC
		}
		return a.EffectiveDate.After(b.EffectiveDate) // DESC
	})

	return result
}

// --- Tests ---

func TestResolve_Spec3BeatsSpec2BeatsSpec1BeatsDefault(t *testing.T) {
	// Scenario 3: FL+Chase+FHA — specificity 3 override wins for slaHours
	ctx := CaseContext{State: "FL", Client: "Chase", Investor: "FHA", CaseType: "FC-Judicial"}
	candidates := loadAndFilterOverrides(ctx)
	defaults := defaultsMap()

	result := Resolve(ctx, candidates, defaults)

	step := result.Steps["file-complaint"]

	// slaHours: ovr-034 (spec 3) = 168
	sla := step["slaHours"]
	if sla.Source != "override" || sla.OverrideID != "ovr-034" {
		t.Errorf("slaHours: want override ovr-034, got source=%s overrideId=%s", sla.Source, sla.OverrideID)
	}
	if sla.Value.(float64) != 168 {
		t.Errorf("slaHours: want 168, got %v", sla.Value)
	}

	// feeAmount: ovr-035 (spec 3) = 55000
	fee := step["feeAmount"]
	if fee.Source != "override" || fee.OverrideID != "ovr-035" {
		t.Errorf("feeAmount: want override ovr-035, got source=%s overrideId=%s", fee.Source, fee.OverrideID)
	}

	// templateId: ovr-037 (spec 3)
	tmpl := step["templateId"]
	if tmpl.Source != "override" || tmpl.OverrideID != "ovr-037" {
		t.Errorf("templateId: want override ovr-037, got source=%s overrideId=%s", tmpl.Source, tmpl.OverrideID)
	}

	// feeAuthRequired: ovr-031 (spec 1, client=Chase) — only candidate for this trait
	feeAuth := step["feeAuthRequired"]
	if feeAuth.Source != "override" || feeAuth.OverrideID != "ovr-031" {
		t.Errorf("feeAuthRequired: want override ovr-031, got source=%s overrideId=%s", feeAuth.Source, feeAuth.OverrideID)
	}
}

func TestResolve_Spec2BeatsSpec1(t *testing.T) {
	// Scenario 2: FL+Chase (spec 2) beats FL-only (spec 1) for slaHours
	ctx := CaseContext{State: "FL", Client: "Chase", Investor: "FreddieMac", CaseType: "FC-Judicial"}
	candidates := loadAndFilterOverrides(ctx)
	defaults := defaultsMap()

	result := Resolve(ctx, candidates, defaults)
	step := result.Steps["file-complaint"]

	sla := step["slaHours"]
	if sla.Source != "override" || sla.OverrideID != "ovr-020" {
		t.Errorf("slaHours: want ovr-020 (FL+Chase spec 2), got source=%s overrideId=%s", sla.Source, sla.OverrideID)
	}
	if sla.Value.(float64) != 240 {
		t.Errorf("slaHours: want 240, got %v", sla.Value)
	}
}

func TestResolve_AsOfDateFiltering(t *testing.T) {
	// Scenario 11: FL+Chase+FHA as of 2025-07-01
	// spec-3 overrides have effectiveDate 2025-09-01 — must NOT apply
	// falls back to FL+Chase spec-2 (effectiveDate 2025-06-01)
	ctx := CaseContext{
		State:    "FL",
		Client:   "Chase",
		Investor: "FHA",
		CaseType: "FC-Judicial",
		AsOfDate: mustDate("2025-07-01"),
	}
	candidates := loadAndFilterOverrides(ctx)
	defaults := defaultsMap()

	result := Resolve(ctx, candidates, defaults)
	step := result.Steps["file-complaint"]

	sla := step["slaHours"]
	if sla.Source != "override" || sla.OverrideID != "ovr-020" {
		t.Errorf("slaHours: want ovr-020 (spec 2, effective 2025-06-01), got source=%s overrideId=%s val=%v", sla.Source, sla.OverrideID, sla.Value)
	}
	if sla.Value.(float64) != 240 {
		t.Errorf("slaHours: want 240, got %v", sla.Value)
	}

	feeAmt := step["feeAmount"]
	if feeAmt.Source != "override" || feeAmt.OverrideID != "ovr-053" {
		t.Errorf("feeAmount: want ovr-053, got source=%s overrideId=%s", feeAmt.Source, feeAmt.OverrideID)
	}
}

func TestResolve_EqualSpecificityNewerEffectiveDateWins(t *testing.T) {
	// Scenario 10: FL+WellsFargo+VA
	// file-complaint.requiredDocuments: ovr-039 (investor=VA, spec 1, eff 2025-03-01)
	// vs ovr-002 (state=FL, spec 1, eff 2025-01-01)
	// ovr-039 wins because newer effectiveDate
	ctx := CaseContext{State: "FL", Client: "WellsFargo", Investor: "VA", CaseType: "FC-Judicial"}
	candidates := loadAndFilterOverrides(ctx)
	defaults := defaultsMap()

	result := Resolve(ctx, candidates, defaults)
	step := result.Steps["file-complaint"]

	docs := step["requiredDocuments"]
	if docs.Source != "override" || docs.OverrideID != "ovr-039" {
		t.Errorf("requiredDocuments: want ovr-039 (VA investor, newer date), got source=%s overrideId=%s", docs.Source, docs.OverrideID)
	}
}

func TestResolve_NonJudicial(t *testing.T) {
	// Scenario 5: TX+Nationstar+FannieMae+FC-NonJudicial
	// file-complaint.slaHours: ovr-042 (TX+NonJudicial spec 2) beats ovr-005 (TX spec 1)
	// obtain-judgment.slaHours: ovr-043 (caseType=NonJudicial spec 1) = 0
	ctx := CaseContext{State: "TX", Client: "Nationstar", Investor: "FannieMae", CaseType: "FC-NonJudicial"}
	candidates := loadAndFilterOverrides(ctx)
	defaults := defaultsMap()

	result := Resolve(ctx, candidates, defaults)

	sla := result.Steps["file-complaint"]["slaHours"]
	if sla.Source != "override" || sla.OverrideID != "ovr-042" {
		t.Errorf("file-complaint slaHours: want ovr-042, got source=%s overrideId=%s", sla.Source, sla.OverrideID)
	}
	if sla.Value.(float64) != 240 {
		t.Errorf("file-complaint slaHours: want 240, got %v", sla.Value)
	}

	judgSla := result.Steps["obtain-judgment"]["slaHours"]
	if judgSla.Source != "override" || judgSla.OverrideID != "ovr-043" {
		t.Errorf("obtain-judgment slaHours: want ovr-043, got source=%s overrideId=%s", judgSla.Source, judgSla.OverrideID)
	}
	if judgSla.Value.(float64) != 0 {
		t.Errorf("obtain-judgment slaHours: want 0, got %v", judgSla.Value)
	}
}

func TestResolve_MostlyDefaults(t *testing.T) {
	// Scenario 9: IL+Nationstar — almost all defaults
	ctx := CaseContext{State: "IL", Client: "Nationstar", Investor: "Private", CaseType: "FC-Judicial"}
	candidates := loadAndFilterOverrides(ctx)
	defaults := defaultsMap()

	result := Resolve(ctx, candidates, defaults)

	// file-complaint.slaHours — no IL-only or Nationstar override → default 480
	sla := result.Steps["file-complaint"]["slaHours"]
	if sla.Source != "default" {
		t.Errorf("file-complaint slaHours: want default, got source=%s overrideId=%s", sla.Source, sla.OverrideID)
	}
	if sla.Value.(float64) != 480 {
		t.Errorf("file-complaint slaHours: want 480, got %v", sla.Value)
	}

	// title-search.requiredDocuments — ovr-052 (IL spec 1) still applies
	docs := result.Steps["title-search"]["requiredDocuments"]
	if docs.Source != "override" || docs.OverrideID != "ovr-052" {
		t.Errorf("title-search requiredDocuments: want ovr-052, got source=%s overrideId=%s", docs.Source, docs.OverrideID)
	}
}

func TestResolve_FourDimensionSpecificity(t *testing.T) {
	// Scenario 4: FL+Chase+FannieMae+FC-Judicial
	// file-complaint.templateId: ovr-047 (all 4 dims, spec 4)
	ctx := CaseContext{State: "FL", Client: "Chase", Investor: "FannieMae", CaseType: "FC-Judicial"}
	candidates := loadAndFilterOverrides(ctx)
	defaults := defaultsMap()

	result := Resolve(ctx, candidates, defaults)

	tmpl := result.Steps["file-complaint"]["templateId"]
	if tmpl.Source != "override" || tmpl.OverrideID != "ovr-047" {
		t.Errorf("templateId: want ovr-047 (spec 4), got source=%s overrideId=%s", tmpl.Source, tmpl.OverrideID)
	}
	if tmpl.Value.(string) != "complaint-fl-chase-fnma-judicial-v3" {
		t.Errorf("templateId: wrong value %v", tmpl.Value)
	}

	// slaHours falls back to spec-2 (no FannieMae sla override)
	sla := result.Steps["file-complaint"]["slaHours"]
	if sla.Source != "override" || sla.OverrideID != "ovr-020" {
		t.Errorf("slaHours: want ovr-020 (spec 2 fallback), got source=%s overrideId=%s", sla.Source, sla.OverrideID)
	}
}

func TestResolve_ConflictFlagged(t *testing.T) {
	// Inject two conflicting overrides (same spec, same effectiveDate) into candidates directly
	candidates := []Override{
		{
			ID: "ovr-X", StepKey: "file-complaint", TraitKey: "slaHours",
			Specificity: 1, Value: float64(240),
			EffectiveDate: mustDate("2025-01-01"),
			Selector:      Selector{State: strPtr("FL")},
		},
		{
			ID: "ovr-Y", StepKey: "file-complaint", TraitKey: "slaHours",
			Specificity: 1, Value: float64(360),
			EffectiveDate: mustDate("2025-01-01"),
			Selector:      Selector{State: strPtr("FL")},
		},
	}
	defaults := defaultsMap()
	ctx := CaseContext{State: "FL", Client: "Chase", Investor: "FHA", CaseType: "FC-Judicial"}

	result := Resolve(ctx, candidates, defaults)
	sla := result.Steps["file-complaint"]["slaHours"]

	if !sla.Conflict {
		t.Error("expected conflict flag to be set")
	}
	if sla.ConflictWith != "ovr-Y" {
		t.Errorf("expected conflictsWith=ovr-Y, got %s", sla.ConflictWith)
	}
	if sla.OverrideID != "ovr-X" {
		t.Errorf("expected winner=ovr-X, got %s", sla.OverrideID)
	}
}
