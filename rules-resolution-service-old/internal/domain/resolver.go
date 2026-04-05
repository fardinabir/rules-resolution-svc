package domain

import (
	"fmt"
	"time"
)

// Resolve runs the resolution algorithm against pre-fetched, pre-ordered candidates.
// candidates must be ordered by (step_key, trait_key, specificity DESC, effective_date DESC, created_at DESC)
// — the DB query guarantees this so the first item per group is always the winner.
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
			k := StepTrait{StepKey: step, TraitKey: trait}
			rs[trait] = pickWinner(grouped[k], defaults[k])
		}
		result.Steps[step] = rs
	}
	return result
}

// Explain runs the same algorithm as Resolve but returns a full trace of every
// candidate considered for each step/trait cell.
func Explain(ctx CaseContext, candidates []Override, defaults map[StepTrait]any) ExplainResult {
	grouped := groupByStepTrait(candidates)
	result := ExplainResult{
		Context:    ctx,
		ResolvedAt: time.Now().UTC(),
	}
	for _, step := range AllSteps {
		for _, trait := range AllTraits {
			k := StepTrait{StepKey: step, TraitKey: trait}
			result.Traces = append(result.Traces, buildTrace(step, trait, grouped[k], defaults[k]))
		}
	}
	return result
}

func groupByStepTrait(candidates []Override) map[StepTrait][]Override {
	m := make(map[StepTrait][]Override)
	for _, o := range candidates {
		k := StepTrait{StepKey: o.StepKey, TraitKey: o.TraitKey}
		m[k] = append(m[k], o)
	}
	return m
}

// pickWinner selects the best override from a pre-sorted group.
// group[0] is always the winner (highest specificity, then most recent effectiveDate, then most recent createdAt).
// If group[1] has the same specificity AND same effectiveDate as group[0], a conflict is flagged.
func pickWinner(group []Override, defaultVal any) ResolvedTrait {
	if len(group) == 0 {
		return ResolvedTrait{Value: defaultVal, Source: "default"}
	}
	winner := group[0]
	rt := ResolvedTrait{
		Value:      winner.Value,
		Source:     "override",
		OverrideID: winner.ID,
	}
	if len(group) > 1 {
		runner := group[1]
		if runner.Specificity == winner.Specificity && runner.EffectiveDate.Equal(winner.EffectiveDate) {
			rt.Conflict = true
			rt.ConflictWith = runner.ID
		}
	}
	return rt
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
			return fmt.Sprintf("SELECTED — tiebreak by effectiveDate (%s > %s)",
				winner.EffectiveDate.Format("2006-01-02"),
				group[1].EffectiveDate.Format("2006-01-02"))
		}
		return "SELECTED — highest specificity"
	}
	if c.Specificity < winner.Specificity {
		return fmt.Sprintf("SHADOWED — lower specificity (%d < %d)", c.Specificity, winner.Specificity)
	}
	if c.EffectiveDate.Before(winner.EffectiveDate) {
		return fmt.Sprintf("SHADOWED — older effectiveDate (%s < %s)",
			c.EffectiveDate.Format("2006-01-02"),
			winner.EffectiveDate.Format("2006-01-02"))
	}
	return "SHADOWED — lost createdAt tiebreak"
}
