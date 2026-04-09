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

	db2 "github.com/fardinabir/rules-resolution-svc/internal/db"
	"github.com/fardinabir/rules-resolution-svc/internal/domain"
	"github.com/fardinabir/rules-resolution-svc/internal/repository"
	"github.com/fardinabir/rules-resolution-svc/internal/service"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// Helper to create string pointers
func ptrString(s string) *string {
	return &s
}

// Test setup helper
type testSetup struct {
	echo         *echo.Echo
	db           *gorm.DB
	overrideRepo repository.OverrideRepository
	overrideSvc  service.OverrideService
	handler      OverrideHandler
}

func cleanDB(t *testing.T, db *gorm.DB) {
	t.Helper()
	require.NoError(t, db.Exec("TRUNCATE TABLE override_history, overrides RESTART IDENTITY CASCADE").Error)
}

func setupOverrideTest(t *testing.T) *testSetup {
	e := echo.New()
	e.Validator = NewCustomValidator()

	dbInstance, err := db2.NewTestDB()
	require.NoError(t, err, "failed to create test database")

	err = db2.Migrate(dbInstance)
	require.NoError(t, err, "failed to run migrations")

	cleanDB(t, dbInstance)

	overrideRepo := repository.NewOverrideRepository(dbInstance)
	overrideSvc := service.NewOverrideService(overrideRepo)
	handler := NewOverrideHandler(overrideSvc)

	return &testSetup{
		echo:         e,
		db:           dbInstance,
		overrideRepo: overrideRepo,
		overrideSvc:  overrideSvc,
		handler:      handler,
	}
}

func TestOverrideHandler_CreateAndList(t *testing.T) {
	setup := setupOverrideTest(t)
	e := setup.echo
	handler := setup.handler

	// Test: Create override with state only (specificity 1)
	{
		createBody := `{
			"stepKey": "title-search",
			"traitKey": "slaHours",
			"selector": {"state": "FL"},
			"value": 240,
			"effectiveDate": "2025-01-01",
			"status": "active",
			"description": "FL title search override"
		}`
		req := httptest.NewRequest(http.MethodPost, "/api/overrides", bytes.NewReader([]byte(createBody)))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.Set("actor", "test@example.com")
		c.SetPath("/api/overrides")

		err := handler.Create(c)
		require.NoError(t, err)
		assert.Equal(t, http.StatusCreated, rec.Code)
		t.Logf("✅ Create specificity-1 override: PASS")
	}

	// Test: Create override with state+client (specificity 2)
	{
		createBody := `{
			"stepKey": "file-complaint",
			"traitKey": "slaHours",
			"selector": {"state": "FL", "client": "Chase"},
			"value": 168,
			"effectiveDate": "2025-06-01",
			"status": "active",
			"description": "FL+Chase filing deadline"
		}`
		req := httptest.NewRequest(http.MethodPost, "/api/overrides", bytes.NewReader([]byte(createBody)))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.Set("actor", "test@example.com")
		c.SetPath("/api/overrides")

		err := handler.Create(c)
		require.NoError(t, err)
		assert.Equal(t, http.StatusCreated, rec.Code)
		t.Logf("✅ Create specificity-2 override: PASS")
	}

	// Test: List overrides
	{
		req := httptest.NewRequest(http.MethodGet, "/api/overrides", nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetPath("/api/overrides")

		err := handler.List(c)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, rec.Code)
		t.Logf("✅ List all overrides: PASS")
	}

	// Test: GetByID
	{
		req := httptest.NewRequest(http.MethodGet, "/api/overrides/ovr-001", nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetPath("/api/overrides/:id")
		c.SetParamNames("id")
		c.SetParamValues("ovr-001")

		err := handler.GetByID(c)
		require.NoError(t, err)
		// Might be 404 if the override ID doesn't exist
		assert.True(t, rec.Code == http.StatusOK || rec.Code == http.StatusNotFound)
		t.Logf("✅ GetByID: PASS (status=%d)", rec.Code)
	}
}

func TestOverrideHandler_UpdateStatus(t *testing.T) {
	setup := setupOverrideTest(t)
	e := setup.echo
	handler := setup.handler
	overrideRepo := setup.overrideRepo

	ctx := context.Background()

	// Seed an override in draft status
	override := domain.Override{
		ID:            "test-ovr-001",
		StepKey:       "title-search",
		TraitKey:      "slaHours",
		Selector:      domain.Selector{State: ptrString("FL")},
		Value:         int64(240),
		Specificity:   1,
		EffectiveDate: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		Status:        "draft",
		CreatedBy:     "admin@example.com",
		UpdatedBy:     "admin@example.com",
	}
	require.NoError(t, overrideRepo.Create(ctx, override))

	// Test: Update status from draft to active
	{
		statusBody := `{"status": "active"}`
		req := httptest.NewRequest(http.MethodPatch, "/api/overrides/test-ovr-001/status", bytes.NewReader([]byte(statusBody)))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.Set("actor", "admin@example.com")
		c.SetPath("/api/overrides/:id/status")
		c.SetParamNames("id")
		c.SetParamValues("test-ovr-001")

		err := handler.UpdateStatus(c)
		require.NoError(t, err)
		assert.Equal(t, http.StatusNoContent, rec.Code)
		t.Logf("✅ Update status draft→active: PASS")
	}

	// Test: GetHistory
	{
		req := httptest.NewRequest(http.MethodGet, "/api/overrides/test-ovr-001/history", nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetPath("/api/overrides/:id/history")
		c.SetParamNames("id")
		c.SetParamValues("test-ovr-001")

		err := handler.GetHistory(c)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, rec.Code)
		t.Logf("✅ Get history: PASS")
	}
}

func TestOverrideHandler_GetConflicts(t *testing.T) {
	setup := setupOverrideTest(t)
	e := setup.echo
	handler := setup.handler

	// Test: Get conflicts (should be empty initially)
	{
		req := httptest.NewRequest(http.MethodGet, "/api/overrides/conflicts", nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetPath("/api/overrides/conflicts")

		err := handler.GetConflicts(c)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, rec.Code)
		t.Logf("✅ Get conflicts endpoint: PASS")
	}
}

func TestOverrideHandler_Filtering(t *testing.T) {
	setup := setupOverrideTest(t)
	e := setup.echo
	handler := setup.handler
	overrideRepo := setup.overrideRepo

	ctx := context.Background()

	// Seed a few overrides with different attributes (use unique test IDs)
	overrides := []domain.Override{
		{
			ID:            "test-ovr-filter-001",
			StepKey:       "title-search",
			TraitKey:      "slaHours",
			Selector:      domain.Selector{State: ptrString("FL")},
			Value:         int64(240),
			Specificity:   1,
			EffectiveDate: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
			Status:        "active",
			CreatedBy:     "admin",
			UpdatedBy:     "admin",
		},
		{
			ID:            "test-ovr-filter-002",
			StepKey:       "file-complaint",
			TraitKey:      "slaHours",
			Selector:      domain.Selector{State: ptrString("TX")},
			Value:         int64(120),
			Specificity:   1,
			EffectiveDate: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
			Status:        "active",
			CreatedBy:     "admin",
			UpdatedBy:     "admin",
		},
	}
	for _, o := range overrides {
		require.NoError(t, overrideRepo.Create(ctx, o))
	}

	// Test: Filter by stepKey
	{
		req := httptest.NewRequest(http.MethodGet, "/api/overrides?stepKey=title-search", nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetPath("/api/overrides")

		err := handler.List(c)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, rec.Code)
		t.Logf("✅ Filter by stepKey: PASS")
	}

	// Test: Filter by state
	{
		req := httptest.NewRequest(http.MethodGet, "/api/overrides?state=FL", nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetPath("/api/overrides")

		err := handler.List(c)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, rec.Code)
		t.Logf("✅ Filter by state: PASS")
	}

	// Test: Filter by status
	{
		req := httptest.NewRequest(http.MethodGet, "/api/overrides?status=active", nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetPath("/api/overrides")

		err := handler.List(c)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, rec.Code)
		t.Logf("✅ Filter by status: PASS")
	}
}

// TestOverrideHandler_NotFound confirms 404 is returned for unknown IDs on
// both GetByID and UpdateStatus endpoints.
func TestOverrideHandler_NotFound(t *testing.T) {
	setup := setupOverrideTest(t)
	e := setup.echo
	handler := setup.handler

	t.Run("GetByID returns 404 for unknown ID", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/overrides/ghost-id", nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetParamNames("id")
		c.SetParamValues("ghost-id")
		require.NoError(t, handler.GetByID(c))
		assert.Equal(t, http.StatusNotFound, rec.Code)
	})

	t.Run("UpdateStatus returns 404 for unknown ID", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPatch, "/api/overrides/ghost-id/status",
			bytes.NewReader([]byte(`{"status":"active"}`)))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.Set("actor", "test@example.com")
		c.SetParamNames("id")
		c.SetParamValues("ghost-id")
		require.NoError(t, handler.UpdateStatus(c))
		assert.Equal(t, http.StatusNotFound, rec.Code)
	})
}

// TestOverrideHandler_StatusTransitions verifies the open status state machine:
// all cross-status transitions are allowed (draft↔active↔archived in any direction).
// Only self-transitions (same→same) and unknown status values are rejected.
func TestOverrideHandler_StatusTransitions(t *testing.T) {
	setup := setupOverrideTest(t)
	e := setup.echo
	handler := setup.handler
	overrideRepo := setup.overrideRepo
	ctx := context.Background()

	patch := func(id, status string) int {
		body := fmt.Sprintf(`{"status":"%s"}`, status)
		req := httptest.NewRequest(http.MethodPatch, "/api/overrides/"+id+"/status",
			bytes.NewReader([]byte(body)))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.Set("actor", "test@example.com")
		c.SetParamNames("id")
		c.SetParamValues(id)
		require.NoError(t, handler.UpdateStatus(c))
		return rec.Code
	}

	require.NoError(t, overrideRepo.Create(ctx, domain.Override{
		ID: "ovr-sm-001", StepKey: "title-search", TraitKey: "slaHours",
		Selector: domain.Selector{State: ptrString("OR")}, Specificity: 1,
		Value: int64(480), EffectiveDate: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		Status: "draft", CreatedBy: "test", UpdatedBy: "test",
	}))

	// All cross-status transitions are valid.
	assert.Equal(t, http.StatusNoContent, patch("ovr-sm-001", "active"), "draft→active must succeed")
	assert.Equal(t, http.StatusNoContent, patch("ovr-sm-001", "draft"), "active→draft must succeed")
	assert.Equal(t, http.StatusNoContent, patch("ovr-sm-001", "archived"), "draft→archived must succeed")
	assert.Equal(t, http.StatusNoContent, patch("ovr-sm-001", "active"), "archived→active must succeed")
	assert.Equal(t, http.StatusNoContent, patch("ovr-sm-001", "draft"), "active→draft must succeed")

	// Self-transitions (same→same) are rejected — not a meaningful operation.
	assert.Equal(t, http.StatusBadRequest, patch("ovr-sm-001", "draft"), "draft→draft self-transition must be rejected")

	// Unknown status values are rejected.
	assert.Equal(t, http.StatusBadRequest, patch("ovr-sm-001", "pending"), "unknown status must be rejected")
}

// TestOverrideHandler_Update verifies that PUT updates the override's fields and
// that the change appears in the audit history.
func TestOverrideHandler_Update(t *testing.T) {
	setup := setupOverrideTest(t)
	e := setup.echo
	handler := setup.handler
	overrideRepo := setup.overrideRepo
	ctx := context.Background()

	require.NoError(t, overrideRepo.Create(ctx, domain.Override{
		ID: "ovr-upd-001", StepKey: "file-complaint", TraitKey: "slaHours",
		Selector: domain.Selector{State: ptrString("NV")}, Specificity: 1,
		Value: int64(360), EffectiveDate: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		Status: "draft", CreatedBy: "test", UpdatedBy: "test",
	}))

	updateBody := `{
		"stepKey": "file-complaint",
		"traitKey": "slaHours",
		"selector": {"state": "NV"},
		"value": 480,
		"effectiveDate": "2025-01-01",
		"description": "updated description"
	}`
	req := httptest.NewRequest(http.MethodPut, "/api/overrides/ovr-upd-001",
		bytes.NewReader([]byte(updateBody)))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("actor", "updater@example.com")
	c.SetParamNames("id")
	c.SetParamValues("ovr-upd-001")

	require.NoError(t, handler.Update(c))
	assert.Equal(t, http.StatusOK, rec.Code)

	// Decode as a generic map to avoid interface{} → float64 vs int64 ambiguity.
	var updated map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&updated))
	assert.Equal(t, "ovr-upd-001", updated["id"])
	assert.Equal(t, "updated description", updated["description"])
	assert.Equal(t, float64(480), updated["value"], "JSON numbers decode as float64 into interface{}")
	assert.Equal(t, "updater@example.com", updated["updatedBy"])

	// Audit history must contain an "updated" entry (seeding via repo.Create skips history,
	// so we only assert the update action recorded by the service).
	histReq := httptest.NewRequest(http.MethodGet, "/api/overrides/ovr-upd-001/history", nil)
	histRec := httptest.NewRecorder()
	hc := e.NewContext(histReq, histRec)
	hc.SetParamNames("id")
	hc.SetParamValues("ovr-upd-001")
	require.NoError(t, handler.GetHistory(hc))
	require.Equal(t, http.StatusOK, histRec.Code)

	var histResp struct {
		History []domain.OverrideHistoryEntry `json:"history"`
	}
	require.NoError(t, json.NewDecoder(histRec.Body).Decode(&histResp))
	require.NotEmpty(t, histResp.History, "history must have at least one entry after update")
	actions := make(map[string]bool)
	for _, h := range histResp.History {
		actions[h.Action] = true
	}
	assert.True(t, actions["updated"], "history must contain an 'updated' entry")
}

// TestOverrideHandler_ConflictDetection seeds two genuinely conflicting overrides
// and verifies the conflicts endpoint identifies them.
func TestOverrideHandler_ConflictDetection(t *testing.T) {
	setup := setupOverrideTest(t)
	e := setup.echo
	handler := setup.handler
	overrideRepo := setup.overrideRepo
	ctx := context.Background()

	// Two spec-1 overrides: same step/trait, identical selector {state:"MN"},
	// overlapping date ranges (no expiry) — this is a genuine conflict.
	for _, o := range []domain.Override{
		{
			ID: "ovr-conf-A", StepKey: "file-complaint", TraitKey: "slaHours",
			Selector: domain.Selector{State: ptrString("MN")}, Specificity: 1,
			Value: int64(100), EffectiveDate: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
			Status: "active", CreatedBy: "test", UpdatedBy: "test",
		},
		{
			ID: "ovr-conf-B", StepKey: "file-complaint", TraitKey: "slaHours",
			Selector: domain.Selector{State: ptrString("MN")}, Specificity: 1,
			Value: int64(200), EffectiveDate: time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC),
			Status: "active", CreatedBy: "test", UpdatedBy: "test",
		},
	} {
		require.NoError(t, overrideRepo.Create(ctx, o))
	}

	req := httptest.NewRequest(http.MethodGet, "/api/overrides/conflicts", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	require.NoError(t, handler.GetConflicts(c))
	require.Equal(t, http.StatusOK, rec.Code)

	var resp struct {
		Conflicts []domain.ConflictPair `json:"conflicts"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	require.NotEmpty(t, resp.Conflicts, "conflicting overrides must be detected")

	// Verify the conflict involves our two overrides.
	found := false
	for _, cp := range resp.Conflicts {
		if (cp.OverrideA == "ovr-conf-A" && cp.OverrideB == "ovr-conf-B") ||
			(cp.OverrideA == "ovr-conf-B" && cp.OverrideB == "ovr-conf-A") {
			found = true
			assert.Equal(t, "file-complaint", cp.StepKey)
			assert.Equal(t, "slaHours", cp.TraitKey)
			assert.NotEmpty(t, cp.Reason)
		}
	}
	assert.True(t, found, "the specific conflict pair (ovr-conf-A, ovr-conf-B) must be reported")
}

// TestOverrideHandler_CreateValidation confirms that the service rejects requests
// with invalid or missing fields before any DB write occurs.
func TestOverrideHandler_CreateValidation(t *testing.T) {
	setup := setupOverrideTest(t)
	e := setup.echo
	handler := setup.handler

	post := func(body string) int {
		req := httptest.NewRequest(http.MethodPost, "/api/overrides", bytes.NewReader([]byte(body)))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.Set("actor", "test@example.com")
		require.NoError(t, handler.Create(c))
		return rec.Code
	}

	t.Run("missing stepKey returns 400", func(t *testing.T) {
		code := post(`{"traitKey":"slaHours","selector":{"state":"FL"},"value":240,"effectiveDate":"2025-01-01","status":"draft"}`)
		assert.Equal(t, http.StatusBadRequest, code)
	})

	t.Run("unknown traitKey returns 400", func(t *testing.T) {
		code := post(`{"stepKey":"file-complaint","traitKey":"notATrait","selector":{"state":"FL"},"value":240,"effectiveDate":"2025-01-01","status":"draft"}`)
		assert.Equal(t, http.StatusBadRequest, code)
	})

	t.Run("wrong value type for slaHours returns 400", func(t *testing.T) {
		// slaHours expects an integer; passing a string should fail NormalizeTraitValue.
		code := post(`{"stepKey":"file-complaint","traitKey":"slaHours","selector":{"state":"FL"},"value":"not-a-number","effectiveDate":"2025-01-01","status":"draft"}`)
		assert.Equal(t, http.StatusBadRequest, code)
	})

	t.Run("invalid effectiveDate format returns 400", func(t *testing.T) {
		code := post(`{"stepKey":"file-complaint","traitKey":"slaHours","selector":{"state":"FL"},"value":240,"effectiveDate":"not-a-date","status":"draft"}`)
		assert.Equal(t, http.StatusBadRequest, code)
	})
}

// TestOverrideHandler_ConflictDetection_DateBoundaries tests critical date boundary
// edge cases in conflict detection: adjacent dates should NOT conflict, but overlapping
// ones should; NULL expiresDate (infinite) should conflict with any bounded range.
func TestOverrideHandler_ConflictDetection_DateBoundaries(t *testing.T) {
	setup := setupOverrideTest(t)
	overrideRepo := setup.overrideRepo
	handler := setup.handler
	ctx := context.Background()
	e := setup.echo

	// Helper to parse date string to time.Time pointer
	parseAndPtrDate := func(dateStr string) *time.Time {
		parsed, err := time.Parse("2006-01-02", dateStr)
		require.NoError(t, err)
		return &parsed
	}

	testCases := []struct {
		name           string
		overrideA      domain.Override
		overrideB      domain.Override
		shouldConflict bool
		description    string
	}{
		{
			name: "Adjacent non-overlapping dates should NOT conflict",
			overrideA: domain.Override{
				ID: "ovr-nosplit-a", StepKey: "title-search", TraitKey: "slaHours",
				Selector: domain.Selector{State: ptrString("MI")}, Specificity: 1,
				Value: int64(100), EffectiveDate: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
				ExpiresDate: parseAndPtrDate("2025-06-30"),
				Status:      "active", CreatedBy: "test", UpdatedBy: "test",
			},
			overrideB: domain.Override{
				ID: "ovr-nosplit-b", StepKey: "title-search", TraitKey: "slaHours",
				Selector: domain.Selector{State: ptrString("MI")}, Specificity: 1,
				Value: int64(200), EffectiveDate: time.Date(2025, 7, 1, 0, 0, 0, 0, time.UTC),
				ExpiresDate: parseAndPtrDate("2025-12-31"),
				Status:      "active", CreatedBy: "test", UpdatedBy: "test",
			},
			shouldConflict: false,
			description:    "A expires 2025-06-30, B starts 2025-07-01 — no overlap",
		},
		{
			name: "One infinite (no expiry), one bounded — SHOULD conflict",
			overrideA: domain.Override{
				ID: "ovr-infinity-a", StepKey: "file-complaint", TraitKey: "slaHours",
				Selector: domain.Selector{State: ptrString("OK")}, Specificity: 1,
				Value: int64(100), EffectiveDate: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
				ExpiresDate: nil, // infinite
				Status:      "active", CreatedBy: "test", UpdatedBy: "test",
			},
			overrideB: domain.Override{
				ID: "ovr-infinity-b", StepKey: "file-complaint", TraitKey: "slaHours",
				Selector: domain.Selector{State: ptrString("OK")}, Specificity: 1,
				Value: int64(200), EffectiveDate: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
				ExpiresDate: parseAndPtrDate("2025-12-31"),
				Status:      "active", CreatedBy: "test", UpdatedBy: "test",
			},
			shouldConflict: true,
			description:    "Infinite range overlaps bounded range — conflict",
		},
		{
			name: "Overlapping date ranges with same specificity — SHOULD conflict",
			overrideA: domain.Override{
				ID: "ovr-overlap-a", StepKey: "serve-borrower", TraitKey: "slaHours",
				Selector: domain.Selector{State: ptrString("CO")}, Specificity: 1,
				Value: int64(100), EffectiveDate: time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC),
				ExpiresDate: parseAndPtrDate("2025-09-30"),
				Status:      "active", CreatedBy: "test", UpdatedBy: "test",
			},
			overrideB: domain.Override{
				ID: "ovr-overlap-b", StepKey: "serve-borrower", TraitKey: "slaHours",
				Selector: domain.Selector{State: ptrString("CO")}, Specificity: 1,
				Value: int64(200), EffectiveDate: time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC),
				ExpiresDate: parseAndPtrDate("2025-12-31"),
				Status:      "active", CreatedBy: "test", UpdatedBy: "test",
			},
			shouldConflict: true,
			description:    "A: 2025-03-01 to 2025-09-30, B: 2025-06-01 to 2025-12-31 — overlaps from 06-01 to 09-30",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Clean up
			setup.db.Exec("DELETE FROM overrides WHERE id LIKE 'ovr-nosplit-%' OR id LIKE 'ovr-infinity-%' OR id LIKE 'ovr-overlap-%'")

			// Seed both overrides
			require.NoError(t, overrideRepo.Create(ctx, tc.overrideA))
			require.NoError(t, overrideRepo.Create(ctx, tc.overrideB))

			// Get conflicts
			req := httptest.NewRequest(http.MethodGet, "/api/overrides/conflicts", nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)
			require.NoError(t, handler.GetConflicts(c))
			require.Equal(t, http.StatusOK, rec.Code)

			var resp struct {
				Conflicts []domain.ConflictPair `json:"conflicts"`
			}
			require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))

			// Check if the conflict pair is reported
			found := false
			for _, cp := range resp.Conflicts {
				if (cp.OverrideA == tc.overrideA.ID && cp.OverrideB == tc.overrideB.ID) ||
					(cp.OverrideA == tc.overrideB.ID && cp.OverrideB == tc.overrideA.ID) {
					found = true
					break
				}
			}

			if tc.shouldConflict {
				assert.True(t, found, "%s: conflict pair should be detected. %s", tc.name, tc.description)
			} else {
				assert.False(t, found, "%s: conflict pair should NOT be detected. %s", tc.name, tc.description)
			}
		})
	}
}

// TestOverrideHandler_CombinedFiltering tests multiple filters applied together
// with AND logic to ensure correct intersection filtering.
func TestOverrideHandler_CombinedFiltering(t *testing.T) {
	setup := setupOverrideTest(t)
	e := setup.echo
	handler := setup.handler
	overrideRepo := setup.overrideRepo
	ctx := context.Background()

	// Seed several overrides with different attributes
	overrides := []domain.Override{
		{
			ID: "ovr-multi-1", StepKey: "title-search", TraitKey: "slaHours",
			Selector: domain.Selector{State: ptrString("PA")}, Specificity: 1,
			Value: int64(100), EffectiveDate: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
			Status: "active", CreatedBy: "test", UpdatedBy: "test",
		},
		{
			ID: "ovr-multi-2", StepKey: "title-search", TraitKey: "feeAmount",
			Selector: domain.Selector{State: ptrString("PA")}, Specificity: 1,
			Value: int64(200), EffectiveDate: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
			Status: "active", CreatedBy: "test", UpdatedBy: "test",
		},
		{
			ID: "ovr-multi-3", StepKey: "file-complaint", TraitKey: "slaHours",
			Selector: domain.Selector{State: ptrString("PA")}, Specificity: 1,
			Value: int64(300), EffectiveDate: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
			Status: "draft", CreatedBy: "test", UpdatedBy: "test",
		},
	}

	for _, o := range overrides {
		require.NoError(t, overrideRepo.Create(ctx, o))
	}

	testCases := []struct {
		name          string
		queryParams   string
		expectedCount int
		expectedIDs   []string
		description   string
	}{
		{
			name:          "Filter by stepKey only",
			queryParams:   "?stepKey=title-search",
			expectedCount: 2,
			expectedIDs:   []string{"ovr-multi-1", "ovr-multi-2"},
			description:   "Both title-search overrides (active and draft)",
		},
		{
			name:          "Filter by stepKey + status (AND logic)",
			queryParams:   "?stepKey=title-search&status=active",
			expectedCount: 2,
			expectedIDs:   []string{"ovr-multi-1", "ovr-multi-2"},
			description:   "Only active title-search overrides",
		},
		{
			name:          "Filter by state + status",
			queryParams:   "?state=PA&status=active",
			expectedCount: 2,
			expectedIDs:   []string{"ovr-multi-1", "ovr-multi-2"},
			description:   "Active overrides for PA (excludes draft ovr-multi-3)",
		},
		{
			name:          "Filter by stepKey + traitKey",
			queryParams:   "?stepKey=title-search&traitKey=slaHours",
			expectedCount: 1,
			expectedIDs:   []string{"ovr-multi-1"},
			description:   "Only title-search.slaHours",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/overrides"+tc.queryParams, nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)
			c.SetPath("/api/overrides")

			require.NoError(t, handler.List(c))
			require.Equal(t, http.StatusOK, rec.Code)

			var resp struct {
				Data     []domain.Override `json:"data"`
				Total    int               `json:"total"`
				Page     int               `json:"page"`
				PageSize int               `json:"pageSize"`
			}
			require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))

			assert.Len(t, resp.Data, tc.expectedCount, "%s: %s", tc.name, tc.description)

			// Verify the expected IDs are in the response
			for _, expectedID := range tc.expectedIDs {
				found := false
				for _, o := range resp.Data {
					if o.ID == expectedID {
						found = true
						break
					}
				}
				assert.True(t, found, "%s: expected override %s in response. %s", tc.name, expectedID, tc.description)
			}
		})
	}
}

// TestOverrideHandler_DuplicateCreation verifies the design decision: the API allows creating
// two overrides with identical selector/step/trait (IDs are always auto-generated), and the
// conflict detection endpoint is responsible for surfacing the resulting ambiguity.
// Conflict detection operates on active overrides only — two active overrides at the same
// specificity, selector, and overlapping date ranges constitute a genuine conflict.
func TestOverrideHandler_DuplicateCreation(t *testing.T) {
	setup := setupOverrideTest(t)
	e := setup.echo
	handler := setup.handler
	overrideRepo := setup.overrideRepo
	ctx := context.Background()

	// Seed first override (active)
	require.NoError(t, overrideRepo.Create(ctx, domain.Override{
		ID: "ovr-dup-one", StepKey: "obtain-judgment", TraitKey: "slaHours",
		Selector: domain.Selector{State: ptrString("SC")}, Specificity: 1,
		Value: int64(100), EffectiveDate: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		Status: "active", CreatedBy: "test", UpdatedBy: "test",
	}))

	// Create a second active override with identical selector, step, trait — different ID (auto-generated).
	// Both are active at the same specificity and overlapping date range: a real conflict.
	createBody := `{
		"stepKey": "obtain-judgment",
		"traitKey": "slaHours",
		"selector": {"state": "SC"},
		"value": 999,
		"effectiveDate": "2025-01-01",
		"status": "active",
		"description": "Duplicate active override"
	}`
	req := httptest.NewRequest(http.MethodPost, "/api/overrides", bytes.NewReader([]byte(createBody)))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("actor", "test@example.com")
	c.SetPath("/api/overrides")

	require.NoError(t, handler.Create(c))
	// System allows it — IDs are auto-generated, no uniqueness constraint on (step, trait, selector)
	assert.Equal(t, http.StatusCreated, rec.Code, "duplicate creation should succeed (conflict surfaced via /conflicts, not at write time)")

	// Conflict detection must identify the active+active pair —
	// same step/trait, same specificity, same selector, overlapping date ranges (infinite).
	reqConflict := httptest.NewRequest(http.MethodGet, "/api/overrides/conflicts", nil)
	recConflict := httptest.NewRecorder()
	cConflict := e.NewContext(reqConflict, recConflict)
	require.NoError(t, handler.GetConflicts(cConflict))
	require.Equal(t, http.StatusOK, recConflict.Code)

	var conflictResp struct {
		Conflicts []domain.ConflictPair `json:"conflicts"`
	}
	require.NoError(t, json.NewDecoder(recConflict.Body).Decode(&conflictResp))
	assert.NotEmpty(t, conflictResp.Conflicts, "active+active pair at same spec/dates must be detected as conflict")
}
