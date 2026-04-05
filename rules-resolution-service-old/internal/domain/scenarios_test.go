package domain

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"testing"
	"time"
)

// --- scenario data structures ---

type scenarioContext struct {
	State    string `json:"state"`
	Client   string `json:"client"`
	Investor string `json:"investor"`
	CaseType string `json:"caseType"`
	AsOfDate string `json:"asOfDate,omitempty"`
}

type expectedResolution struct {
	StepKey            string          `json:"stepKey"`
	TraitKey           string          `json:"traitKey"`
	ExpectedValue      json.RawMessage `json:"expectedValue"`
	ExpectedSource     string          `json:"expectedSource"`
	ExpectedOverrideID string          `json:"expectedOverrideId"`
	Explanation        string          `json:"explanation"`
}

type scenario struct {
	Name                string               `json:"name"`
	Description         string               `json:"description"`
	Context             scenarioContext       `json:"context"`
	ExpectedResolutions []expectedResolution `json:"expectedResolutions"`
}

// TestAllScenarios runs all 12 acceptance scenarios from test_scenarios.json.
func TestAllScenarios(t *testing.T) {
	data, err := os.ReadFile("../../../sr_backend_assignment_data/test_scenarios.json")
	if err != nil {
		t.Fatalf("read test_scenarios.json: %v", err)
	}
	var scenarios []scenario
	if err := json.Unmarshal(data, &scenarios); err != nil {
		t.Fatalf("parse test_scenarios.json: %v", err)
	}

	defaults := defaultsMap()

	for i, sc := range scenarios {
		sc := sc
		t.Run(fmt.Sprintf("scenario_%02d_%s", i+1, sc.Name), func(t *testing.T) {
			ctx := CaseContext{
				State:    sc.Context.State,
				Client:   sc.Context.Client,
				Investor: sc.Context.Investor,
				CaseType: sc.Context.CaseType,
			}
			if sc.Context.AsOfDate != "" {
				ctx.AsOfDate, err = time.Parse("2006-01-02", sc.Context.AsOfDate)
				if err != nil {
					t.Fatalf("parse asOfDate: %v", err)
				}
			}

			candidates := loadAndFilterOverrides(ctx)
			result := Resolve(ctx, candidates, defaults)

			for _, exp := range sc.ExpectedResolutions {
				trait, ok := result.Steps[exp.StepKey][exp.TraitKey]
				if !ok {
					t.Errorf("[%s.%s] not found in result", exp.StepKey, exp.TraitKey)
					continue
				}

				// Check source
				if trait.Source != exp.ExpectedSource {
					t.Errorf("[%s.%s] source: want %q got %q — %s",
						exp.StepKey, exp.TraitKey, exp.ExpectedSource, trait.Source, exp.Explanation)
				}

				// Check overrideId (only when source=override)
				if exp.ExpectedSource == "override" && trait.OverrideID != exp.ExpectedOverrideID {
					t.Errorf("[%s.%s] overrideId: want %q got %q — %s",
						exp.StepKey, exp.TraitKey, exp.ExpectedOverrideID, trait.OverrideID, exp.Explanation)
				}

				// Check value — decode expected into native Go and compare
				var expVal any
				if err := json.Unmarshal(exp.ExpectedValue, &expVal); err != nil {
					t.Errorf("[%s.%s] bad expectedValue JSON: %v", exp.StepKey, exp.TraitKey, err)
					continue
				}
				if !valuesEqual(expVal, trait.Value) {
					t.Errorf("[%s.%s] value: want %v got %v — %s",
						exp.StepKey, exp.TraitKey, expVal, trait.Value, exp.Explanation)
				}
			}
		})
	}
}

// valuesEqual compares two any values produced by json.Unmarshal.
// JSON numbers unmarshal to float64; arrays to []any; booleans to bool; strings to string.
func valuesEqual(expected, actual any) bool {
	// Both nil
	if expected == nil && actual == nil {
		return true
	}
	// Normalize both through JSON round-trip to get consistent types
	expJSON, _ := json.Marshal(expected)
	actJSON, _ := json.Marshal(actual)
	var expNorm, actNorm any
	_ = json.Unmarshal(expJSON, &expNorm)
	_ = json.Unmarshal(actJSON, &actNorm)
	return reflect.DeepEqual(expNorm, actNorm)
}
