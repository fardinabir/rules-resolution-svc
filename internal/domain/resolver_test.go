package domain_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/fardinabir/go-svc-boilerplate/internal/domain"
)

func strPtr(s string) *string { return &s }

func mustDate(s string) time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return t
}

func makeCtx(state, client, investor, caseType string) domain.CaseContext {
	return domain.CaseContext{
		State: state, Client: client, Investor: investor, CaseType: caseType,
		AsOfDate: mustDate("2025-10-01"),
	}
}

func makeOverride(id, step, trait string, specificity int, effDate string, sel domain.Selector, val any) domain.Override {
	return domain.Override{
		ID: id, StepKey: step, TraitKey: trait,
		Selector: sel, Specificity: specificity,
		Value:         val,
		EffectiveDate: mustDate(effDate),
		Status:        "active",
	}
}

func TestResolve_SpecificityWins(t *testing.T) {
	ctx := makeCtx("FL", "Chase", "FHA", "FC-Judicial")
	spec3 := makeOverride("ovr-3", "file-complaint", "slaHours", 3,
		"2025-01-01", domain.Selector{State: strPtr("FL"), Client: strPtr("Chase"), Investor: strPtr("FHA")}, int64(72))
	spec2 := makeOverride("ovr-2", "file-complaint", "slaHours", 2,
		"2025-01-01", domain.Selector{State: strPtr("FL"), Client: strPtr("Chase")}, int64(96))
	spec1 := makeOverride("ovr-1", "file-complaint", "slaHours", 1,
		"2025-01-01", domain.Selector{State: strPtr("FL")}, int64(120))
	defaults := map[domain.StepTrait]any{{StepKey: "file-complaint", TraitKey: "slaHours"}: int64(240)}

	candidates := []domain.Override{spec3, spec2, spec1} // pre-sorted by DB
	result := domain.Resolve(ctx, candidates, defaults)

	rt := result.Steps["file-complaint"]["slaHours"]
	assert.Equal(t, "override", rt.Source)
	assert.Equal(t, "ovr-3", rt.OverrideID)
	assert.Equal(t, int64(72), rt.Value)
}

func TestResolve_DefaultFallback(t *testing.T) {
	ctx := makeCtx("TX", "WellsFargo", "VA", "FC-Judicial")
	defaults := map[domain.StepTrait]any{{StepKey: "title-search", TraitKey: "slaHours"}: int64(720)}
	result := domain.Resolve(ctx, nil, defaults)

	rt := result.Steps["title-search"]["slaHours"]
	assert.Equal(t, "default", rt.Source)
	assert.Equal(t, int64(720), rt.Value)
}

func TestResolve_EqualSpecNewerDateWins(t *testing.T) {
	ctx := makeCtx("FL", "Chase", "FHA", "FC-Judicial")
	older := makeOverride("ovr-old", "file-complaint", "slaHours", 2,
		"2025-01-01", domain.Selector{State: strPtr("FL"), Client: strPtr("Chase")}, int64(96))
	newer := makeOverride("ovr-new", "file-complaint", "slaHours", 2,
		"2025-09-01", domain.Selector{State: strPtr("FL"), Client: strPtr("Chase")}, int64(72))
	defaults := map[domain.StepTrait]any{}

	// DB ORDER BY puts newer effective_date first
	candidates := []domain.Override{newer, older}
	result := domain.Resolve(ctx, candidates, defaults)

	rt := result.Steps["file-complaint"]["slaHours"]
	assert.Equal(t, "ovr-new", rt.OverrideID)
}

func TestResolve_TiebreakByCreatedAt(t *testing.T) {
	ctx := makeCtx("FL", "Chase", "FHA", "FC-Judicial")
	a := makeOverride("ovr-a", "file-complaint", "slaHours", 2,
		"2025-01-01", domain.Selector{State: strPtr("FL"), Client: strPtr("Chase")}, int64(96))
	b := makeOverride("ovr-b", "file-complaint", "slaHours", 2,
		"2025-01-01", domain.Selector{State: strPtr("FL"), Client: strPtr("Chase")}, int64(72))
	defaults := map[domain.StepTrait]any{}

	// DB ORDER BY createdAt DESC puts ovr-a first when spec and effectiveDate are equal
	candidates := []domain.Override{a, b}
	result := domain.Resolve(ctx, candidates, defaults)

	rt := result.Steps["file-complaint"]["slaHours"]
	// Winner is still deterministic (DB ORDER BY createdAt DESC) — no conflict flag
	assert.Equal(t, "override", rt.Source)
	assert.Equal(t, "ovr-a", rt.OverrideID)
}

func TestSelector_WildcardMatchesAny(t *testing.T) {
	sel := domain.Selector{State: strPtr("FL")} // client, investor, caseType are nil
	ctx := makeCtx("FL", "AnyClient", "AnyInvestor", "AnyType")
	assert.True(t, sel.Matches(ctx))
}

func TestSelector_PinnedDimensionMismatch(t *testing.T) {
	sel := domain.Selector{State: strPtr("FL"), Client: strPtr("Chase")}
	ctx := makeCtx("FL", "WellsFargo", "FHA", "FC-Judicial")
	assert.False(t, sel.Matches(ctx))
}

func TestSelector_Specificity(t *testing.T) {
	tests := []struct {
		sel  domain.Selector
		want int
	}{
		{domain.Selector{}, 0},
		{domain.Selector{State: strPtr("FL")}, 1},
		{domain.Selector{State: strPtr("FL"), Client: strPtr("Chase")}, 2},
		{domain.Selector{State: strPtr("FL"), Client: strPtr("Chase"), Investor: strPtr("FHA"), CaseType: strPtr("FC-Judicial")}, 4},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, tt.sel.Specificity())
	}
}

func TestResolve_AllStepsAllTraits(t *testing.T) {
	ctx := makeCtx("FL", "Chase", "FHA", "FC-Judicial")
	defaults := map[domain.StepTrait]any{}
	for _, step := range domain.AllSteps {
		for _, trait := range domain.AllTraits {
			defaults[domain.StepTrait{StepKey: step, TraitKey: trait}] = "default-val"
		}
	}
	result := domain.Resolve(ctx, nil, defaults)
	require.Len(t, result.Steps, 6)
	for _, step := range domain.AllSteps {
		require.Len(t, result.Steps[step], 6, "step %s should have 6 traits", step)
	}
}

func TestExplain_CandidatesAnnotated(t *testing.T) {
	spec2 := makeOverride("ovr-2", "file-complaint", "slaHours", 2,
		"2025-01-01", domain.Selector{State: strPtr("FL"), Client: strPtr("Chase")}, int64(96))
	spec1 := makeOverride("ovr-1", "file-complaint", "slaHours", 1,
		"2025-01-01", domain.Selector{State: strPtr("FL")}, int64(120))
	defaults := map[domain.StepTrait]any{}

	traces := domain.Explain([]domain.Override{spec2, spec1}, defaults)
	var trace *domain.TraitTrace
	for i := range traces {
		if traces[i].Step == "file-complaint" && traces[i].Trait == "slaHours" {
			trace = &traces[i]
			break
		}
	}
	require.NotNil(t, trace)
	require.Len(t, trace.Candidates, 2)
	assert.Contains(t, trace.Candidates[0].Outcome, "SELECTED")
	assert.Contains(t, trace.Candidates[1].Outcome, "SHADOWED")
}

func TestExplain_DefaultTraceHasNoCandidates(t *testing.T) {
	defaults := map[domain.StepTrait]any{
		{StepKey: "title-search", TraitKey: "slaHours"}: int64(720),
	}
	traces := domain.Explain(nil, defaults)
	for _, trace := range traces {
		if trace.Step == "title-search" && trace.Trait == "slaHours" {
			assert.Nil(t, trace.ResolvedFrom)
			assert.Len(t, trace.Candidates, 0)
			assert.Equal(t, int64(720), trace.ResolvedValue)
			return
		}
	}
	t.Fatal("trace not found")
}

func TestNormalizeTraitValue_FloatToInt(t *testing.T) {
	val, err := domain.NormalizeTraitValue("slaHours", float64(720))
	require.NoError(t, err)
	assert.Equal(t, int64(720), val)
}

func TestNormalizeTraitValue_WrongType(t *testing.T) {
	_, err := domain.NormalizeTraitValue("slaHours", "not-a-number")
	assert.Error(t, err)
}

func TestNormalizeTraitValue_StringSlice(t *testing.T) {
	raw := []interface{}{"doc1", "doc2"}
	val, err := domain.NormalizeTraitValue("requiredDocuments", raw)
	require.NoError(t, err)
	assert.Equal(t, []string{"doc1", "doc2"}, val)
}
