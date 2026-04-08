package controller

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	db2 "github.com/fardinabir/go-svc-boilerplate/internal/db"
	"github.com/fardinabir/go-svc-boilerplate/internal/repository"
	"github.com/fardinabir/go-svc-boilerplate/internal/service"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// dataDir resolves the path to sr_backend_assignment_data/ relative to this file.
func dataDir() string {
	_, filename, _, _ := runtime.Caller(0)
	// internal/controller/ → ../.. → rules-resolution-service-new/ → .. → sr_backend_assignment/ → sr_backend_assignment_data/
	return filepath.Join(filepath.Dir(filename), "..", "..", "..", "sr_backend_assignment_data")
}

// --- JSON shapes for scenario file parsing ---

type scenarioContext struct {
	State    string `json:"state"`
	Client   string `json:"client"`
	Investor string `json:"investor"`
	CaseType string `json:"caseType"`
	AsOfDate string `json:"asOfDate,omitempty"`
}

type expectedResolution struct {
	StepKey            string      `json:"stepKey"`
	TraitKey           string      `json:"traitKey"`
	ExpectedValue      interface{} `json:"expectedValue"`
	ExpectedSource     string      `json:"expectedSource"`
	ExpectedOverrideId string      `json:"expectedOverrideId,omitempty"`
	Explanation        string      `json:"explanation"`
}

type testScenario struct {
	Name                string               `json:"name"`
	Description         string               `json:"description"`
	Context             scenarioContext      `json:"context"`
	ExpectedResolutions []expectedResolution `json:"expectedResolutions"`
}

// --- Seed helpers for production data ---

type seedOverrideJSON struct {
	ID            string          `json:"id"`
	StepKey       string          `json:"stepKey"`
	TraitKey      string          `json:"traitKey"`
	Selector      selectorJSONs   `json:"selector"`
	Value         json.RawMessage `json:"value"`
	EffectiveDate string          `json:"effectiveDate"`
	ExpiresDate   *string         `json:"expiresDate,omitempty"`
	Status        string          `json:"status"`
	Description   string          `json:"description"`
	CreatedBy     string          `json:"createdBy"`
}

type selectorJSONs struct {
	State    *string `json:"state,omitempty"`
	Client   *string `json:"client,omitempty"`
	Investor *string `json:"investor,omitempty"`
	CaseType *string `json:"caseType,omitempty"`
}

func selectorSpecificity(s selectorJSONs) int {
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

func seedProductionData(t *testing.T, db *gorm.DB) {
	t.Helper()
	dir := dataDir()

	// Truncate mutable tables first; keep steps + defaults (reference data)
	require.NoError(t, db.Exec("TRUNCATE TABLE override_history, overrides RESTART IDENTITY CASCADE").Error)

	// Seed overrides from overrides.json
	raw, err := os.ReadFile(filepath.Join(dir, "overrides.json"))
	require.NoError(t, err, "read overrides.json")

	var overrides []seedOverrideJSON
	require.NoError(t, json.Unmarshal(raw, &overrides), "parse overrides.json")

	now := time.Now().UTC()
	for _, o := range overrides {
		effDate, err := time.Parse("2006-01-02", o.EffectiveDate)
		require.NoError(t, err, "parse effectiveDate for %s", o.ID)

		var expiresArg interface{}
		if o.ExpiresDate != nil {
			exp, err := time.Parse("2006-01-02", *o.ExpiresDate)
			require.NoError(t, err, "parse expiresDate for %s", o.ID)
			expiresArg = exp
		}

		spec := selectorSpecificity(o.Selector)
		createdBy := o.CreatedBy
		if createdBy == "" {
			createdBy = "test-seed"
		}

		err = db.Exec(`
			INSERT INTO overrides
			  (id, step_key, trait_key, state, client, investor, case_type,
			   specificity, value, effective_date, expires_date,
			   status, description, created_at, created_by, updated_at, updated_by)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$14,$16)
			ON CONFLICT (id) DO NOTHING`,
			o.ID, o.StepKey, o.TraitKey,
			o.Selector.State, o.Selector.Client, o.Selector.Investor, o.Selector.CaseType,
			spec, o.Value, effDate, expiresArg,
			o.Status, o.Description, now, createdBy, "test-seed",
		).Error
		require.NoError(t, err, "insert override %s", o.ID)
	}
}

// setupScenariosTest wires up an Echo + ResolveHandler backed by the test DB
// and seeds the full production dataset.
func setupScenariosTest(t *testing.T) ResolveHandler {
	t.Helper()

	dbInstance, err := db2.NewTestDB()
	require.NoError(t, err, "create test DB")
	require.NoError(t, db2.Migrate(dbInstance), "run migrations")

	seedProductionData(t, dbInstance)

	overrideRepo := repository.NewOverrideRepository(dbInstance)
	defaultRepo := repository.NewDefaultRepository(dbInstance)
	resolveSvc := service.NewResolveService(overrideRepo, defaultRepo)
	return NewResolveHandler(resolveSvc)
}

// valuesEqual compares an expectedValue (parsed from JSON — numbers become float64,
// arrays become []interface{}) against the resolved value (a json.RawMessage).
func valuesEqual(t *testing.T, expected interface{}, actual json.RawMessage, label string) {
	t.Helper()
	var actualDecoded interface{}
	require.NoError(t, json.Unmarshal(actual, &actualDecoded), "unmarshal actual for %s", label)

	// Normalise: JSON numbers come back as float64; compare via JSON round-trip.
	expJSON, err := json.Marshal(expected)
	require.NoError(t, err)
	actJSON, err := json.Marshal(actualDecoded)
	require.NoError(t, err)
	assert.JSONEq(t, string(expJSON), string(actJSON), label)
}

// TestResolveHandler_TestScenarios runs every assertion in test_scenarios.json
// against the full production seed dataset.
func TestResolveHandler_TestScenarios(t *testing.T) {
	handler := setupScenariosTest(t)

	e := echo.New()
	e.Validator = NewCustomValidator()

	// Load test_scenarios.json
	raw, err := os.ReadFile(filepath.Join(dataDir(), "test_scenarios.json"))
	require.NoError(t, err, "read test_scenarios.json")

	var scenarios []testScenario
	require.NoError(t, json.Unmarshal(raw, &scenarios), "parse test_scenarios.json")

	for _, sc := range scenarios {
		sc := sc // capture
		t.Run(sc.Name, func(t *testing.T) {
			// Build request body
			reqMap := map[string]string{
				"state":    sc.Context.State,
				"client":   sc.Context.Client,
				"investor": sc.Context.Investor,
				"caseType": sc.Context.CaseType,
			}
			if sc.Context.AsOfDate != "" {
				reqMap["asOfDate"] = sc.Context.AsOfDate
			}
			bodyBytes, err := json.Marshal(reqMap)
			require.NoError(t, err)

			req := httptest.NewRequest(http.MethodPost, "/api/resolve", bytes.NewReader(bodyBytes))
			req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			require.NoError(t, handler.Resolve(c))
			require.Equal(t, http.StatusOK, rec.Code, "HTTP status for scenario %q", sc.Name)

			var resp resolveResp
			require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))

			for _, exp := range sc.ExpectedResolutions {
				label := sc.Name + " / " + exp.StepKey + "." + exp.TraitKey

				stepTraits, ok := resp.Steps[exp.StepKey]
				require.True(t, ok, "%s: step %q missing in response", label, exp.StepKey)

				tr, ok := stepTraits[exp.TraitKey]
				require.True(t, ok, "%s: trait %q missing in response", label, exp.TraitKey)

				assert.Equal(t, exp.ExpectedSource, tr.Source, "%s: source mismatch — %s", label, exp.Explanation)

				if exp.ExpectedOverrideId != "" {
					assert.Equal(t, exp.ExpectedOverrideId, tr.OverrideID, "%s: overrideId mismatch — %s", label, exp.Explanation)
				}

				valuesEqual(t, exp.ExpectedValue, tr.Value, label+" value")
			}
		})
	}
}
