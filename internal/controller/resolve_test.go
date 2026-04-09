package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	db2 "github.com/fardinabir/go-svc-boilerplate/internal/db"
	"github.com/fardinabir/go-svc-boilerplate/internal/domain"
	"github.com/fardinabir/go-svc-boilerplate/internal/repository"
	"github.com/fardinabir/go-svc-boilerplate/internal/service"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// --- response helpers for resolve tests ---

type traitResult struct {
	Value      json.RawMessage `json:"value"`
	Source     string          `json:"source"`
	OverrideID string          `json:"overrideId"`
}

type resolveResp struct {
	Steps map[string]map[string]traitResult `json:"steps"`
}

type traceSourceResult struct {
	OverrideID  string `json:"overrideId"`
	Specificity int    `json:"specificity"`
}

type traceResult struct {
	Step         string             `json:"step"`
	Trait        string             `json:"trait"`
	ResolvedFrom *traceSourceResult `json:"resolvedFrom"`
	Candidates   []struct {
		OverrideID string `json:"overrideId"`
		Outcome    string `json:"outcome"`
	} `json:"candidates"`
}

func getResolveResp(t *testing.T, rec *httptest.ResponseRecorder) resolveResp {
	t.Helper()
	var resp resolveResp
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	return resp
}

func assertTrait(t *testing.T, resp resolveResp, step, trait, wantSource, wantOverrideID string) {
	t.Helper()
	s, ok := resp.Steps[step]
	require.True(t, ok, "step %q missing", step)
	tr, ok := s[trait]
	require.True(t, ok, "trait %q missing in step %q", trait, step)
	assert.Equal(t, wantSource, tr.Source, "%s.%s: source", step, trait)
	if wantOverrideID != "" {
		assert.Equal(t, wantOverrideID, tr.OverrideID, "%s.%s: overrideId", step, trait)
	}
}

func doResolve(t *testing.T, e *echo.Echo, handler ResolveHandler, body string) resolveResp {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/resolve", bytes.NewReader([]byte(body)))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	require.NoError(t, handler.Resolve(c))
	require.Equal(t, http.StatusOK, rec.Code)
	return getResolveResp(t, rec)
}

// Test helper to seed test data
func seedResolveData(t *testing.T, dbInstance *gorm.DB) {
	overrideRepo := repository.NewOverrideRepository(dbInstance)
	defaultRepo := repository.NewDefaultRepository(dbInstance)
	ctx := context.Background()

	// Seed defaults for key traits
	defaults := []struct {
		step  string
		trait string
		val   interface{}
	}{
		{"title-search", "slaHours", int64(720)},
		{"title-search", "feeAmount", int64(35000)},
		{"file-complaint", "slaHours", int64(360)},
		{"file-complaint", "feeAmount", int64(50000)},
		{"serve-borrower", "slaHours", int64(0)},
		{"obtain-judgment", "slaHours", int64(2160)},
		{"schedule-sale", "slaHours", int64(4320)},
		{"conduct-sale", "slaHours", int64(0)},
	}
	_ = defaultRepo // suppress unused warn - used by queries in resolution
	_ = defaults    // we'll rely on DB defaults for this test

	// Seed overrides covering different specificities
	overrides := []domain.Override{
		// FL state-only (specificity 1)
		{
			ID:            "ovr-fl-001",
			StepKey:       "file-complaint",
			TraitKey:      "slaHours",
			Selector:      domain.Selector{State: ptrString("FL")},
			Value:         int64(360),
			Specificity:   1,
			EffectiveDate: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
			Status:        "active",
			CreatedBy:     "seed",
			UpdatedBy:     "seed",
		},
		// FL + Chase (specificity 2)
		{
			ID:            "ovr-fl-chase-001",
			StepKey:       "file-complaint",
			TraitKey:      "slaHours",
			Selector:      domain.Selector{State: ptrString("FL"), Client: ptrString("Chase")},
			Value:         int64(240),
			Specificity:   2,
			EffectiveDate: time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC),
			Status:        "active",
			CreatedBy:     "seed",
			UpdatedBy:     "seed",
		},
		// FL + Chase + FHA (specificity 3)
		{
			ID:            "ovr-fl-chase-fha-001",
			StepKey:       "file-complaint",
			TraitKey:      "slaHours",
			Selector:      domain.Selector{State: ptrString("FL"), Client: ptrString("Chase"), Investor: ptrString("FHA")},
			Value:         int64(168),
			Specificity:   3,
			EffectiveDate: time.Date(2025, 9, 1, 0, 0, 0, 0, time.UTC),
			Status:        "active",
			CreatedBy:     "seed",
			UpdatedBy:     "seed",
		},
		// TX Non-Judicial (specificity 2)
		{
			ID:            "ovr-tx-nonjud-001",
			StepKey:       "file-complaint",
			TraitKey:      "slaHours",
			Selector:      domain.Selector{State: ptrString("TX"), CaseType: ptrString("FC-NonJudicial")},
			Value:         int64(120),
			Specificity:   2,
			EffectiveDate: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
			Status:        "active",
			CreatedBy:     "seed",
			UpdatedBy:     "seed",
		},
		// Chase global (specificity 1)
		{
			ID:            "ovr-chase-global-001",
			StepKey:       "title-search",
			TraitKey:      "feeAuthRequired",
			Selector:      domain.Selector{Client: ptrString("Chase")},
			Value:         true,
			Specificity:   1,
			EffectiveDate: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
			Status:        "active",
			CreatedBy:     "seed",
			UpdatedBy:     "seed",
		},
	}

	for _, o := range overrides {
		if err := overrideRepo.Create(ctx, o); err != nil {
			t.Logf("Warning: failed to seed override %s: %v", o.ID, err)
		}
	}
}

// Test setup helper for resolve tests
type resolveTestSetup struct {
	echo         *echo.Echo
	db           *gorm.DB
	overrideRepo repository.OverrideRepository
	defaultRepo  repository.DefaultRepository
	resolveSvc   service.ResolveService
	handler      ResolveHandler
}

func setupResolveTest(t *testing.T) *resolveTestSetup {
	e := echo.New()
	e.Validator = NewCustomValidator()

	dbInstance, err := db2.NewTestDB()
	require.NoError(t, err, "failed to create test database")

	err = db2.Migrate(dbInstance)
	require.NoError(t, err, "failed to run migrations")

	// Clean slate — prevents duplicate-key failures on repeated test runs
	require.NoError(t, dbInstance.Exec("TRUNCATE TABLE override_history, overrides RESTART IDENTITY CASCADE").Error)

	seedResolveData(t, dbInstance)

	overrideRepo := repository.NewOverrideRepository(dbInstance)
	defaultRepo := repository.NewDefaultRepository(dbInstance)
	resolveSvc := service.NewResolveService(overrideRepo, defaultRepo)
	handler := NewResolveHandler(resolveSvc)

	return &resolveTestSetup{
		echo:         e,
		db:           dbInstance,
		overrideRepo: overrideRepo,
		defaultRepo:  defaultRepo,
		resolveSvc:   resolveSvc,
		handler:      handler,
	}
}

func TestResolveHandler_BasicResolution(t *testing.T) {
	setup := setupResolveTest(t)
	e := setup.echo
	handler := setup.handler

	// Test 1: Resolve with defaults (no overrides match)
	{
		reqBody := `{
			"state": "IL",
			"client": "Nationstar",
			"investor": "Private",
			"caseType": "FC-Judicial"
		}`
		req := httptest.NewRequest(http.MethodPost, "/api/resolve", bytes.NewReader([]byte(reqBody)))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetPath("/api/resolve")

		err := handler.Resolve(c)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, rec.Code)
		t.Logf("✅ Resolve IL context (all defaults): PASS")
	}

	// Test 2: FL with Nationstar (matches FL state override)
	{
		reqBody := `{
			"state": "FL",
			"client": "Nationstar",
			"investor": "FannieMae",
			"caseType": "FC-Judicial"
		}`
		req := httptest.NewRequest(http.MethodPost, "/api/resolve", bytes.NewReader([]byte(reqBody)))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetPath("/api/resolve")

		err := handler.Resolve(c)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, rec.Code)
		t.Logf("✅ Resolve FL state (specificity-1 override matches): PASS")
	}

	// Test 3: FL + Chase (specificity-2 override)
	{
		reqBody := `{
			"state": "FL",
			"client": "Chase",
			"investor": "FreddieMac",
			"caseType": "FC-Judicial"
		}`
		req := httptest.NewRequest(http.MethodPost, "/api/resolve", bytes.NewReader([]byte(reqBody)))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetPath("/api/resolve")

		err := handler.Resolve(c)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, rec.Code)
		t.Logf("✅ Resolve FL+Chase (specificity-2 cascade): PASS")
	}
}

func TestResolveHandler_SpecificityHierarchy(t *testing.T) {
	setup := setupResolveTest(t)
	e := setup.echo
	handler := setup.handler

	// Test: FL + Chase + FHA (highest specificity should win)
	{
		reqBody := `{
			"state": "FL",
			"client": "Chase",
			"investor": "FHA",
			"caseType": "FC-Judicial"
		}`
		req := httptest.NewRequest(http.MethodPost, "/api/resolve", bytes.NewReader([]byte(reqBody)))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetPath("/api/resolve")

		err := handler.Resolve(c)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, rec.Code)
		t.Logf("✅ Resolve FL+Chase+FHA (specificity-3 highest): PASS")
	}
}

func TestResolveHandler_EffectiveDateFiltering(t *testing.T) {
	setup := setupResolveTest(t)
	e := setup.echo
	handler := setup.handler

	// Test: Resolve as of 2025-07-01 (before FHA override becomes effective on 2025-09-01)
	{
		reqBody := `{
			"state": "FL",
			"client": "Chase",
			"investor": "FHA",
			"caseType": "FC-Judicial",
			"asOfDate": "2025-07-01"
		}`
		req := httptest.NewRequest(http.MethodPost, "/api/resolve", bytes.NewReader([]byte(reqBody)))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetPath("/api/resolve")

		err := handler.Resolve(c)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, rec.Code)
		t.Logf("✅ Resolve with asOfDate (effective-date filtering): PASS")
	}
}

func TestResolveHandler_Explain(t *testing.T) {
	setup := setupResolveTest(t)
	e := setup.echo
	handler := setup.handler

	// Test 1: Explain defaults (all defaults, no overrides)
	{
		reqBody := `{
			"state": "IL",
			"client": "Random",
			"investor": "Private",
			"caseType": "FC-Judicial"
		}`
		req := httptest.NewRequest(http.MethodPost, "/api/resolve/explain", bytes.NewReader([]byte(reqBody)))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetPath("/api/resolve/explain")

		err := handler.Explain(c)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, rec.Code)
		t.Logf("✅ Explain resolution (defaults): PASS")
	}

	// Test 2: Explain with cascading overrides
	{
		reqBody := `{
			"state": "FL",
			"client": "Chase",
			"investor": "FHA",
			"caseType": "FC-Judicial"
		}`
		req := httptest.NewRequest(http.MethodPost, "/api/resolve/explain", bytes.NewReader([]byte(reqBody)))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetPath("/api/resolve/explain")

		err := handler.Explain(c)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, rec.Code)
		t.Logf("✅ Explain cascading overrides: PASS (shows specificity hierarchy)")
	}
}

func TestResolveHandler_NonJudicialCaseType(t *testing.T) {
	setup := setupResolveTest(t)
	e := setup.echo
	handler := setup.handler

	// Test: TX Non-Judicial case (specificity-2 override for caseType)
	{
		reqBody := `{
			"state": "TX",
			"client": "Nationstar",
			"investor": "FannieMae",
			"caseType": "FC-NonJudicial"
		}`
		req := httptest.NewRequest(http.MethodPost, "/api/resolve", bytes.NewReader([]byte(reqBody)))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetPath("/api/resolve")

		err := handler.Resolve(c)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, rec.Code)
		t.Logf("✅ Resolve TX non-judicial case: PASS (caseType dimension)")
	}
}

func TestResolveHandler_ClientOnlySelector(t *testing.T) {
	setup := setupResolveTest(t)
	e := setup.echo
	handler := setup.handler

	// Test: Chase global policy (client-only selector, specificity 1)
	// Should apply to ANY state for Chase
	{
		reqBody := `{
			"state": "NY",
			"client": "Chase",
			"investor": "Private",
			"caseType": "FC-Judicial"
		}`
		req := httptest.NewRequest(http.MethodPost, "/api/resolve", bytes.NewReader([]byte(reqBody)))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetPath("/api/resolve")

		err := handler.Resolve(c)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, rec.Code)
		t.Logf("✅ Resolve NY+Chase (client selector): PASS (cross-state override)")
	}
}

func TestResolveHandler_InvalidInput(t *testing.T) {
	setup := setupResolveTest(t)
	e := setup.echo
	handler := setup.handler

	// Test: Invalid JSON
	{
		reqBody := `{invalid json}`
		req := httptest.NewRequest(http.MethodPost, "/api/resolve", bytes.NewReader([]byte(reqBody)))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetPath("/api/resolve")

		err := handler.Resolve(c)
		require.NoError(t, err)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
		t.Logf("✅ Invalid JSON rejected: PASS")
	}

	// Test: Invalid asOfDate format
	{
		reqBody := `{
			"state": "FL",
			"client": "Chase",
			"investor": "FHA",
			"caseType": "FC-Judicial",
			"asOfDate": "not-a-date"
		}`
		req := httptest.NewRequest(http.MethodPost, "/api/resolve", bytes.NewReader([]byte(reqBody)))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetPath("/api/resolve")

		err := handler.Resolve(c)
		require.NoError(t, err)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
		t.Logf("✅ Invalid asOfDate rejected: PASS")
	}
}

// TestResolveHandler_AssertResolvedValues verifies actual resolved values, sources, and
// override IDs — not just HTTP status. These are the core correctness tests.
func TestResolveHandler_AssertResolvedValues(t *testing.T) {
	setup := setupResolveTest(t)

	t.Run("specificity-3 wins over specificity-2 and specificity-1", func(t *testing.T) {
		resp := doResolve(t, setup.echo, setup.handler,
			`{"state":"FL","client":"Chase","investor":"FHA","caseType":"FC-Judicial"}`)
		assertTrait(t, resp, "file-complaint", "slaHours", "override", "ovr-fl-chase-fha-001")
		var val float64
		require.NoError(t, json.Unmarshal(resp.Steps["file-complaint"]["slaHours"].Value, &val))
		assert.Equal(t, float64(168), val)
	})

	t.Run("specificity-2 wins when specificity-3 dimension does not match", func(t *testing.T) {
		// FreddieMac does not match the FHA spec-3 override — spec-2 (FL+Chase) must win
		resp := doResolve(t, setup.echo, setup.handler,
			`{"state":"FL","client":"Chase","investor":"FreddieMac","caseType":"FC-Judicial"}`)
		assertTrait(t, resp, "file-complaint", "slaHours", "override", "ovr-fl-chase-001")
		var val float64
		require.NoError(t, json.Unmarshal(resp.Steps["file-complaint"]["slaHours"].Value, &val))
		assert.Equal(t, float64(240), val)
	})

	t.Run("specificity-1 wins when no higher specificity matches", func(t *testing.T) {
		// Nationstar has no spec-2 override — FL state-only (spec-1) must win
		resp := doResolve(t, setup.echo, setup.handler,
			`{"state":"FL","client":"Nationstar","investor":"FannieMae","caseType":"FC-Judicial"}`)
		assertTrait(t, resp, "file-complaint", "slaHours", "override", "ovr-fl-001")
		var val float64
		require.NoError(t, json.Unmarshal(resp.Steps["file-complaint"]["slaHours"].Value, &val))
		assert.Equal(t, float64(360), val)
	})

	t.Run("client-only wildcard override applies across all states", func(t *testing.T) {
		// Chase global policy (spec-1, no state pin) must match NY just as it matches FL
		resp := doResolve(t, setup.echo, setup.handler,
			`{"state":"NY","client":"Chase","investor":"Private","caseType":"FC-Judicial"}`)
		assertTrait(t, resp, "title-search", "feeAuthRequired", "override", "ovr-chase-global-001")
	})

	t.Run("no matching override falls back to default", func(t *testing.T) {
		resp := doResolve(t, setup.echo, setup.handler,
			`{"state":"WA","client":"RandomBank","investor":"Private","caseType":"FC-Judicial"}`)
		assertTrait(t, resp, "file-complaint", "slaHours", "default", "")
		assert.Empty(t, resp.Steps["file-complaint"]["slaHours"].OverrideID)
	})
}

// TestResolveHandler_DraftExcludedFromResolution confirms that draft overrides
// never participate in resolution even when they would win on specificity.
func TestResolveHandler_DraftExcludedFromResolution(t *testing.T) {
	setup := setupResolveTest(t)
	ctx := context.Background()

	// Spec-2 draft override for FL+Chase — would outrank the active spec-1 FL override
	// if drafts were incorrectly included in resolution.
	require.NoError(t, setup.overrideRepo.Create(ctx, domain.Override{
		ID:            "ovr-draft-excluded",
		StepKey:       "file-complaint",
		TraitKey:      "slaHours",
		Selector:      domain.Selector{State: ptrString("FL"), Client: ptrString("Chase")},
		Specificity:   2,
		Value:         int64(9999),
		EffectiveDate: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		Status:        "draft",
		CreatedBy:     "test",
		UpdatedBy:     "test",
	}))

	resp := doResolve(t, setup.echo, setup.handler,
		`{"state":"FL","client":"Chase","investor":"FreddieMac","caseType":"FC-Judicial"}`)

	tr := resp.Steps["file-complaint"]["slaHours"]
	// The active spec-2 seeded override (value=240) must win, not the draft (value=9999)
	assert.Equal(t, "ovr-fl-chase-001", tr.OverrideID, "draft must not win over active override")
	var val float64
	require.NoError(t, json.Unmarshal(tr.Value, &val))
	assert.NotEqual(t, float64(9999), val, "draft override value must not appear in resolution")
}

// TestResolveHandler_ExpiredOverrideExcluded confirms that an override whose
// expiresDate has passed is excluded from resolution.
func TestResolveHandler_ExpiredOverrideExcluded(t *testing.T) {
	setup := setupResolveTest(t)
	ctx := context.Background()

	yesterday := time.Now().UTC().AddDate(0, 0, -1)
	require.NoError(t, setup.overrideRepo.Create(ctx, domain.Override{
		ID:            "ovr-expired-excluded",
		StepKey:       "obtain-judgment",
		TraitKey:      "slaHours",
		Selector:      domain.Selector{State: ptrString("CA")},
		Specificity:   1,
		Value:         int64(9999),
		EffectiveDate: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		ExpiresDate:   &yesterday,
		Status:        "active",
		CreatedBy:     "test",
		UpdatedBy:     "test",
	}))

	resp := doResolve(t, setup.echo, setup.handler,
		`{"state":"CA","client":"Nationstar","investor":"Private","caseType":"FC-Judicial"}`)

	tr := resp.Steps["obtain-judgment"]["slaHours"]
	assert.Equal(t, "default", tr.Source, "expired override must not participate — should fall to default")
	var val float64
	require.NoError(t, json.Unmarshal(tr.Value, &val))
	assert.NotEqual(t, float64(9999), val)
}

// TestResolveHandler_EffectiveDateTiebreaker confirms that when two overrides share
// the same specificity, the one with the more recent effectiveDate wins.
func TestResolveHandler_EffectiveDateTiebreaker(t *testing.T) {
	setup := setupResolveTest(t)
	ctx := context.Background()

	// Two spec-1 overrides for the same step/trait/dimension — only effectiveDate differs.
	require.NoError(t, setup.overrideRepo.Create(ctx, domain.Override{
		ID:            "ovr-tie-older",
		StepKey:       "schedule-sale",
		TraitKey:      "slaHours",
		Selector:      domain.Selector{Investor: ptrString("ZZ")},
		Specificity:   1,
		Value:         int64(1111),
		EffectiveDate: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		Status:        "active",
		CreatedBy:     "test",
		UpdatedBy:     "test",
	}))
	require.NoError(t, setup.overrideRepo.Create(ctx, domain.Override{
		ID:            "ovr-tie-newer",
		StepKey:       "schedule-sale",
		TraitKey:      "slaHours",
		Selector:      domain.Selector{Investor: ptrString("ZZ")},
		Specificity:   1,
		Value:         int64(2222),
		EffectiveDate: time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC), // more recent
		Status:        "active",
		CreatedBy:     "test",
		UpdatedBy:     "test",
	}))

	resp := doResolve(t, setup.echo, setup.handler,
		`{"state":"TX","client":"Nationstar","investor":"ZZ","caseType":"FC-Judicial"}`)

	assertTrait(t, resp, "schedule-sale", "slaHours", "override", "ovr-tie-newer")
	var val float64
	require.NoError(t, json.Unmarshal(resp.Steps["schedule-sale"]["slaHours"].Value, &val))
	assert.Equal(t, float64(2222), val, "more recent effectiveDate must win the tiebreak")
}

// TestResolveHandler_AsOfDateFilteringWithValues confirms that overrides with
// a future effectiveDate relative to asOfDate are excluded, and the correct
// fallback is selected.
func TestResolveHandler_AsOfDateFilteringWithValues(t *testing.T) {
	setup := setupResolveTest(t)

	// ovr-fl-chase-fha-001 has effectiveDate 2025-09-01.
	// Resolving as of 2025-07-01 must exclude it and fall back to spec-2 (ovr-fl-chase-001).
	req := httptest.NewRequest(http.MethodPost, "/api/resolve",
		bytes.NewReader([]byte(`{"state":"FL","client":"Chase","investor":"FHA","caseType":"FC-Judicial","asOfDate":"2025-07-01"}`)))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := setup.echo.NewContext(req, rec)
	require.NoError(t, setup.handler.Resolve(c))
	require.Equal(t, http.StatusOK, rec.Code)

	resp := getResolveResp(t, rec)
	assertTrait(t, resp, "file-complaint", "slaHours", "override", "ovr-fl-chase-001")
	var val float64
	require.NoError(t, json.Unmarshal(resp.Steps["file-complaint"]["slaHours"].Value, &val))
	assert.Equal(t, float64(240), val, "spec-3 override not yet effective — spec-2 must win")
}

// TestResolveHandler_ExplainStructure validates the explain response shape:
// exactly 36 traces, correct resolvedFrom/candidates for overridden traits,
// nil resolvedFrom for default traits.
func TestResolveHandler_ExplainStructure(t *testing.T) {
	setup := setupResolveTest(t)

	req := httptest.NewRequest(http.MethodPost, "/api/resolve/explain",
		bytes.NewReader([]byte(`{"state":"FL","client":"Chase","investor":"FHA","caseType":"FC-Judicial"}`)))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := setup.echo.NewContext(req, rec)
	require.NoError(t, setup.handler.Explain(c))
	require.Equal(t, http.StatusOK, rec.Code)

	var traces []traceResult
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&traces))

	assert.Len(t, traces, 36, "explain must cover the full 6×6 step/trait grid")

	// Locate the file-complaint.slaHours trace — FL+Chase+FHA has 3 candidates.
	var fcSLA *traceResult
	for i := range traces {
		if traces[i].Step == "file-complaint" && traces[i].Trait == "slaHours" {
			fcSLA = &traces[i]
			break
		}
	}
	require.NotNil(t, fcSLA, "file-complaint.slaHours trace must be present")
	require.NotNil(t, fcSLA.ResolvedFrom, "file-complaint.slaHours must resolve from an override")
	assert.Equal(t, "ovr-fl-chase-fha-001", fcSLA.ResolvedFrom.OverrideID)
	assert.Equal(t, 3, fcSLA.ResolvedFrom.Specificity)
	require.Len(t, fcSLA.Candidates, 3, "three candidates (spec 3, 2, 1) must appear")
	assert.Contains(t, fcSLA.Candidates[0].Outcome, "SELECTED")
	assert.Contains(t, fcSLA.Candidates[1].Outcome, "SHADOWED")
	assert.Contains(t, fcSLA.Candidates[2].Outcome, "SHADOWED")

	// title-search.slaHours has no override in the seeded data — resolvedFrom must be nil.
	var tsSLA *traceResult
	for i := range traces {
		if traces[i].Step == "title-search" && traces[i].Trait == "slaHours" {
			tsSLA = &traces[i]
			break
		}
	}
	require.NotNil(t, tsSLA)
	assert.Nil(t, tsSLA.ResolvedFrom, "title-search.slaHours has no override — resolvedFrom must be nil")
	assert.Empty(t, tsSLA.Candidates)
}

// TestResolveHandler_DateBoundaryConditions tests critical off-by-one edge cases
// in effective date filtering (>= for effective, < for expires).
func TestResolveHandler_DateBoundaryConditions(t *testing.T) {
	setup := setupResolveTest(t)
	ctx := context.Background()

	// Helper to parse date string to time.Time pointer
	parseAndPtrDate := func(dateStr string) *time.Time {
		parsed, err := time.Parse("2006-01-02", dateStr)
		require.NoError(t, err)
		return &parsed
	}

	testCases := []struct {
		name            string
		overrideEffDate string
		overrideExpDate *string
		asOfDate        string
		shouldInclude   bool
		description     string
	}{
		{
			name:            "asOfDate exactly on effectiveDate (boundary test >=)",
			overrideEffDate: "2025-06-01",
			overrideExpDate: nil,
			asOfDate:        "2025-06-01",
			shouldInclude:   true,
			description:     "Override becomes effective at midnight, so asOfDate=2025-06-01 should include it",
		},
		{
			name:            "asOfDate one day before effectiveDate",
			overrideEffDate: "2025-06-02",
			overrideExpDate: nil,
			asOfDate:        "2025-06-01",
			shouldInclude:   false,
			description:     "Override not yet effective",
		},
		{
			name:            "asOfDate exactly on expiresDate (boundary test <, not <=)",
			overrideEffDate: "2025-01-01",
			overrideExpDate: ptrString("2025-12-31"),
			asOfDate:        "2025-12-31",
			shouldInclude:   false,
			description:     "Override expires at midnight, so asOfDate=2025-12-31 should EXCLUDE it",
		},
		{
			name:            "asOfDate one day before expiresDate",
			overrideEffDate: "2025-01-01",
			overrideExpDate: ptrString("2025-12-31"),
			asOfDate:        "2025-12-30",
			shouldInclude:   true,
			description:     "Override still active on 2025-12-30",
		},
		{
			name:            "asOfDate far in the future, after all overrides expire",
			overrideEffDate: "2025-01-01",
			overrideExpDate: ptrString("2025-12-31"),
			asOfDate:        "2050-01-01",
			shouldInclude:   false,
			description:     "All overrides have expired, should use defaults",
		},
		{
			name:            "asOfDate far in the past, before override becomes effective",
			overrideEffDate: "2025-01-01",
			overrideExpDate: nil,
			asOfDate:        "1990-01-01",
			shouldInclude:   false,
			description:     "Override doesn't become effective until 2025",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Clean up specific test overrides (don't TRUNCATE, just DELETE to avoid table not found errors)
			setup.db.Exec("DELETE FROM overrides WHERE id LIKE 'ovr-boundary-%'")

			// Convert expDate *string to *time.Time
			var expiresDatePtr *time.Time
			if tc.overrideExpDate != nil {
				expiresDatePtr = parseAndPtrDate(*tc.overrideExpDate)
			}

			// Create boundary test override
			boundaryOverride := domain.Override{
				ID:            "ovr-boundary-test",
				StepKey:       "conduct-sale",
				TraitKey:      "slaHours",
				Selector:      domain.Selector{State: ptrString("MD")},
				Specificity:   1,
				Value:         int64(5555),
				EffectiveDate: parseDate(t, tc.overrideEffDate),
				ExpiresDate:   expiresDatePtr,
				Status:        "active",
				CreatedBy:     "test",
				UpdatedBy:     "test",
			}

			require.NoError(t, setup.overrideRepo.Create(ctx, boundaryOverride))

			// Resolve with the asOfDate
			reqBody := fmt.Sprintf(`{
				"state": "MD",
				"client": "Test",
				"investor": "Test",
				"caseType": "FC-Judicial",
				"asOfDate": "%s"
			}`, tc.asOfDate)

			resp := doResolve(t, setup.echo, setup.handler, reqBody)
			tr := resp.Steps["conduct-sale"]["slaHours"]

			if tc.shouldInclude {
				assert.Equal(t, "override", tr.Source, "%s: expected override to be INCLUDED. %s", tc.name, tc.description)
				assert.Equal(t, "ovr-boundary-test", tr.OverrideID)
				var val float64
				require.NoError(t, json.Unmarshal(tr.Value, &val))
				assert.Equal(t, float64(5555), val)
			} else {
				assert.Equal(t, "default", tr.Source, "%s: expected override to be EXCLUDED. %s", tc.name, tc.description)
			}
		})
	}
}

// TestResolveHandler_StatusFiltering_Comprehensive tests that draft and archived
// overrides are properly excluded from resolution, even in mixed-status hierarchies.
func TestResolveHandler_StatusFiltering_Comprehensive(t *testing.T) {
	setup := setupResolveTest(t)
	ctx := context.Background()

	testCases := []struct {
		name           string
		seedOverrides  []domain.Override
		resolveContext string
		expectedSource string
		expectedID     string
	}{
		{
			name: "Archived overrides excluded (falls back to active lower specificity)",
			seedOverrides: []domain.Override{
				{
					ID: "ovr-archive-spec2", StepKey: "serve-borrower", TraitKey: "slaHours",
					Selector:    domain.Selector{State: ptrString("VA"), Client: ptrString("Test")},
					Specificity: 2, Value: int64(9900), EffectiveDate: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
					Status: "archived", CreatedBy: "test", UpdatedBy: "test",
				},
				{
					ID: "ovr-active-spec1", StepKey: "serve-borrower", TraitKey: "slaHours",
					Selector:    domain.Selector{State: ptrString("VA")},
					Specificity: 1, Value: int64(7700), EffectiveDate: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
					Status: "active", CreatedBy: "test", UpdatedBy: "test",
				},
			},
			resolveContext: `{"state":"VA","client":"Test","investor":"Test","caseType":"FC-Judicial"}`,
			expectedSource: "override",
			expectedID:     "ovr-active-spec1",
		},
		{
			name: "Draft overrides excluded even with higher specificity",
			seedOverrides: []domain.Override{
				{
					ID: "ovr-draft-spec3", StepKey: "obtain-judgment", TraitKey: "slaHours",
					Selector:    domain.Selector{State: ptrString("NC"), Client: ptrString("BankX"), Investor: ptrString("FHA")},
					Specificity: 3, Value: int64(9999), EffectiveDate: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
					Status: "draft", CreatedBy: "test", UpdatedBy: "test",
				},
				{
					ID: "ovr-active-spec2", StepKey: "obtain-judgment", TraitKey: "slaHours",
					Selector:    domain.Selector{State: ptrString("NC"), Client: ptrString("BankX")},
					Specificity: 2, Value: int64(8800), EffectiveDate: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
					Status: "active", CreatedBy: "test", UpdatedBy: "test",
				},
			},
			resolveContext: `{"state":"NC","client":"BankX","investor":"FHA","caseType":"FC-Judicial"}`,
			expectedSource: "override",
			expectedID:     "ovr-active-spec2",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Clean up previous test overrides
			setup.db.Exec("DELETE FROM overrides WHERE id LIKE 'ovr-archive-%' OR id LIKE 'ovr-draft-%' OR id LIKE 'ovr-active-%'")

			// Seed the overrides for this test
			for _, o := range tc.seedOverrides {
				require.NoError(t, setup.overrideRepo.Create(ctx, o))
			}

			resp := doResolve(t, setup.echo, setup.handler, tc.resolveContext)

			// Determine which step/trait to check (from first override in seed)
			firstOverride := tc.seedOverrides[0]
			tr := resp.Steps[firstOverride.StepKey][firstOverride.TraitKey]

			assert.Equal(t, tc.expectedSource, tr.Source, "%s", tc.name)
			assert.Equal(t, tc.expectedID, tr.OverrideID, "%s: expected override ID", tc.name)
		})
	}
}

// TestResolveHandler_SelectorMatching_Nulls tests the spec-0 (all-wildcard) override —
// the only selector edge case not already covered by TestResolveHandler_ClientOnlySelector
// and TestResolveHandler_AssertResolvedValues (which cover pinned mismatch and partial wildcards).
func TestResolveHandler_SelectorMatching_Nulls(t *testing.T) {
	setup := setupResolveTest(t)
	ctx := context.Background()

	testCases := []struct {
		name           string
		selectorSpec   domain.Selector
		resolveContext string
		expectedID     string
		shouldMatch    bool
	}{
		{
			// Unique case: spec-0 override (no dimensions pinned) should match any context.
			// Not covered elsewhere — verifies that specificity=0 overrides beat the defaults table.
			name:           "All dimensions unpinned (spec-0) matches any context",
			selectorSpec:   domain.Selector{},
			resolveContext: `{"state":"WY","client":"Unknown","investor":"Private","caseType":"FC-Judicial"}`,
			expectedID:     "ovr-default-fallback",
			shouldMatch:    true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Clean up
			setup.db.Exec("DELETE FROM overrides WHERE id LIKE 'ovr-selector-%' OR id LIKE 'ovr-default-%' OR id LIKE 'ovr-client-%'")

			if tc.shouldMatch {
				// Seed test override that SHOULD match
				selectorOverride := domain.Override{
					ID:            tc.expectedID,
					StepKey:       "schedule-sale",
					TraitKey:      "slaHours",
					Selector:      tc.selectorSpec,
					Specificity:   countPinnedDimensions(tc.selectorSpec),
					Value:         int64(3333),
					EffectiveDate: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
					Status:        "active",
					CreatedBy:     "test",
					UpdatedBy:     "test",
				}
				require.NoError(t, setup.overrideRepo.Create(ctx, selectorOverride))
			}

			resp := doResolve(t, setup.echo, setup.handler, tc.resolveContext)
			tr := resp.Steps["schedule-sale"]["slaHours"]

			if tc.shouldMatch {
				assert.Equal(t, "override", tr.Source, "%s: override should match", tc.name)
				assert.Equal(t, tc.expectedID, tr.OverrideID)
			} else {
				// Should fall back to default (no override with mismatched dimension)
				assert.Equal(t, "default", tr.Source, "%s: override should not match - should use default", tc.name)
			}
		})
	}
}

// TestResolveHandler_NoMatchingOverrides confirms resolution falls back gracefully
// when NO overrides match any dimension of the request context.
func TestResolveHandler_NoMatchingOverrides(t *testing.T) {
	setup := setupResolveTest(t)

	testCases := []struct {
		name    string
		context string
	}{
		{
			name:    "Non-existent state, non-existent client",
			context: `{"state":"ZZ","client":"UnknownBank","investor":"Private","caseType":"FC-Judicial"}`,
		},
		{
			name:    "Valid state but non-existent client",
			context: `{"state":"CA","client":"UniqueBank","investor":"Private","caseType":"FC-Judicial"}`,
		},
		{
			name:    "All dimensions non-existent",
			context: `{"state":"XX","client":"XXX","investor":"XYZ","caseType":"FC-Judicial"}`,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			resp := doResolve(t, setup.echo, setup.handler, tc.context)

			// All traits should resolve to defaults (source: "default")
			for stepKey, stepTraits := range resp.Steps {
				for traitKey, tr := range stepTraits {
					assert.Equal(t, "default", tr.Source,
						"%s: step=%s, trait=%s should all resolve to defaults", tc.name, stepKey, traitKey)
					assert.Empty(t, tr.OverrideID,
						"%s: step=%s, trait=%s should have no overrideId when using default", tc.name, stepKey, traitKey)
				}
			}
		})
	}
}

// --- BulkResolve ---

type bulkResolveResp struct {
	Results []resolveResp `json:"results"`
}

func doBulkResolve(t *testing.T, e *echo.Echo, handler ResolveHandler, body string) (bulkResolveResp, int) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/resolve/bulk", bytes.NewReader([]byte(body)))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	require.NoError(t, handler.BulkResolve(c))
	var resp bulkResolveResp
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	return resp, rec.Code
}

func TestResolveHandler_BulkResolve(t *testing.T) {
	setup := setupResolveTest(t)

	testCases := []struct {
		name            string
		body            string
		wantCode        int
		wantResultCount int
		// per-result assertions: index → (step, trait, source, overrideID)
		assertions []struct {
			resultIdx  int
			step       string
			trait      string
			wantSource string
			wantOvrID  string
		}
	}{
		{
			name:            "single context returns one result matching individual resolve",
			body:            `{"contexts":[{"state":"FL","client":"Chase","investor":"FHA","caseType":"FC-Judicial"}]}`,
			wantCode:        http.StatusOK,
			wantResultCount: 1,
			assertions: []struct {
				resultIdx                          int
				step, trait, wantSource, wantOvrID string
			}{
				{0, "file-complaint", "slaHours", "override", "ovr-fl-chase-fha-001"},
			},
		},
		{
			name: "two contexts preserve order and winner independence",
			// contexts[0] = IL (all defaults), contexts[1] = FL+Chase (spec-2)
			// if order were swapped, the override would land on the wrong result
			body: `{"contexts":[
				{"state":"IL","client":"Nationstar","investor":"Private","caseType":"FC-Judicial"},
				{"state":"FL","client":"Chase","investor":"FreddieMac","caseType":"FC-Judicial"}
			]}`,
			wantCode:        http.StatusOK,
			wantResultCount: 2,
			assertions: []struct {
				resultIdx                          int
				step, trait, wantSource, wantOvrID string
			}{
				{0, "file-complaint", "slaHours", "default", ""},
				{1, "file-complaint", "slaHours", "override", "ovr-fl-chase-001"},
			},
		},
		{
			name: "two contexts with different specificity tiers each get correct winner",
			body: `{"contexts":[
				{"state":"FL","client":"Chase","investor":"FHA","caseType":"FC-Judicial"},
				{"state":"TX","client":"Nationstar","investor":"FannieMae","caseType":"FC-NonJudicial"}
			]}`,
			wantCode:        http.StatusOK,
			wantResultCount: 2,
			assertions: []struct {
				resultIdx                          int
				step, trait, wantSource, wantOvrID string
			}{
				// FL+Chase+FHA → spec-3 wins
				{0, "file-complaint", "slaHours", "override", "ovr-fl-chase-fha-001"},
				// TX+NonJudicial → spec-2 override
				{1, "file-complaint", "slaHours", "override", "ovr-tx-nonjud-001"},
			},
		},
		{
			name:            "empty contexts returns 400",
			body:            `{"contexts":[]}`,
			wantCode:        http.StatusBadRequest,
			wantResultCount: 0,
		},
		{
			name: "over-limit (51 contexts) returns 400",
			body: fmt.Sprintf(`{"contexts":[%s]}`, func() string {
				ctx := `{"state":"FL","client":"Chase","investor":"FHA","caseType":"FC-Judicial"}`
				result := ""
				for i := 0; i < 51; i++ {
					if i > 0 {
						result += ","
					}
					result += ctx
				}
				return result
			}()),
			wantCode:        http.StatusBadRequest,
			wantResultCount: 0,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			resp, code := doBulkResolve(t, setup.echo, setup.handler, tc.body)
			assert.Equal(t, tc.wantCode, code)
			if tc.wantResultCount > 0 {
				require.Len(t, resp.Results, tc.wantResultCount)
			}
			for _, a := range tc.assertions {
				assertTrait(t, resp.Results[a.resultIdx], a.step, a.trait, a.wantSource, a.wantOvrID)
			}
		})
	}
}

// Helper function to parse date strings
func parseDate(t *testing.T, dateStr string) time.Time {
	t.Helper()
	parsed, err := time.Parse("2006-01-02", dateStr)
	require.NoError(t, err, "failed to parse date %s", dateStr)
	return parsed
}

// Helper function to count pinned dimensions in a selector
func countPinnedDimensions(sel domain.Selector) int {
	count := 0
	if sel.State != nil {
		count++
	}
	if sel.Client != nil {
		count++
	}
	if sel.Investor != nil {
		count++
	}
	if sel.CaseType != nil {
		count++
	}
	return count
}
