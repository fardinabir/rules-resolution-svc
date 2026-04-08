package domain

import (
	"fmt"
	"time"
)

// Resolve runs the resolution algorithm as a pure function.
// candidates: overrides pre-filtered and pre-sorted by the DB query
//
//	(specificity DESC, effective_date DESC, created_at DESC)
//
// defaults: map[StepTrait]any loaded from the defaults table
func Resolve(ctx CaseContext, candidates []Override, defaults map[StepTrait]any) ResolvedConfig {
	grouped := groupByStepTrait(candidates)

	result := ResolvedConfig{
		Context:    ctx,
		ResolvedAt: time.Now().UTC(),
		Steps:      make(map[string]ResolvedStep),
	}

	for _, step := range AllSteps {
		rs := make(ResolvedStep)
		for _, trait := range AllTraits {
			k := StepTrait{step, trait}
			rs[trait] = pickWinner(grouped[k], defaults[k])
		}
		result.Steps[step] = rs
	}
	return result
}

// Explain returns a per-trait resolution trace for all 36 step/trait cells.
func Explain(candidates []Override, defaults map[StepTrait]any) []TraitTrace {
	grouped := groupByStepTrait(candidates)
	var traces []TraitTrace

	for _, step := range AllSteps {
		for _, trait := range AllTraits {
			k := StepTrait{step, trait}
			traces = append(traces, buildTrace(step, trait, grouped[k], defaults[k]))
		}
	}
	return traces
}

func groupByStepTrait(candidates []Override) map[StepTrait][]Override {
	m := make(map[StepTrait][]Override)
	for _, o := range candidates {
		k := StepTrait{o.StepKey, o.TraitKey}
		m[k] = append(m[k], o)
	}
	return m
}

// pickWinner selects the best override from a pre-sorted group.
// group[0] is always the winner (DB ORDER BY guarantees specificity DESC, effective_date DESC, created_at DESC).
// A conflict is detected when group[1] has the same specificity AND effectiveDate as the winner.
func pickWinner(group []Override, defaultVal any) ResolvedTrait {
	if len(group) == 0 {
		return ResolvedTrait{Value: defaultVal, Source: "default"}
	}
	winner := group[0]
	return ResolvedTrait{
		Value:      winner.Value,
		Source:     "override",
		OverrideID: winner.ID,
	}
}

func buildTrace(step, trait string, group []Override, defaultVal any) TraitTrace {
	trace := TraitTrace{Step: step, Trait: trait}
	if len(group) == 0 {
		trace.ResolvedValue = defaultVal
		return trace
	}

	winner := group[0]
	trace.ResolvedValue = winner.Value
	trace.ResolvedFrom = &TraceSource{
		OverrideID:    winner.ID,
		Selector:      winner.Selector,
		Specificity:   winner.Specificity,
		EffectiveDate: winner.EffectiveDate,
	}

	for i, c := range group {
		outcome := outcomeLabel(i, c, winner, group)
		trace.Candidates = append(trace.Candidates, TraceCandidate{
			OverrideID:    c.ID,
			Selector:      c.Selector,
			Specificity:   c.Specificity,
			EffectiveDate: c.EffectiveDate,
			Value:         c.Value,
			Outcome:       outcome,
		})
	}
	return trace
}

func outcomeLabel(i int, c, winner Override, group []Override) string {
	if i == 0 {
		if len(group) > 1 && group[1].Specificity == winner.Specificity {
			if group[1].EffectiveDate.Equal(winner.EffectiveDate) {
				return "SELECTED — tiebreak by createdAt (conflict flagged)"
			}
			return "SELECTED — tiebreak by effectiveDate"
		}
		return "SELECTED — highest specificity"
	}
	if c.Specificity < winner.Specificity {
		return fmt.Sprintf("SHADOWED — lower specificity (%d < %d)", c.Specificity, winner.Specificity)
	}
	if c.EffectiveDate.Before(winner.EffectiveDate) {
		return fmt.Sprintf("SHADOWED — older effectiveDate (%s < %s)",
			c.EffectiveDate.Format("2006-01-02"), winner.EffectiveDate.Format("2006-01-02"))
	}
	return "SHADOWED — later createdAt lost tiebreak"
}
