package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/fardinabir/rules-resolution-svc/internal/domain"
)

// OverrideRepository defines all DB operations for the overrides table.
type OverrideRepository interface {
	FindMatchingOverrides(ctx context.Context, caseCtx domain.CaseContext) ([]domain.Override, error)
	FindMatchingOverridesBatch(ctx context.Context, contexts []domain.CaseContext) ([][]domain.Override, error)
	GetByID(ctx context.Context, id string) (*domain.Override, error)
	List(ctx context.Context, filter OverrideFilter) ([]domain.Override, int64, error)
	Create(ctx context.Context, o domain.Override) error
	Update(ctx context.Context, o domain.Override) error
	UpdateStatus(ctx context.Context, id, status, updatedBy string) error
	GetHistory(ctx context.Context, overrideID string) ([]domain.OverrideHistoryEntry, error)
	RecordHistory(ctx context.Context, entry domain.OverrideHistoryEntry) error
	FindConflicts(ctx context.Context) ([]domain.ConflictPair, error)
}

// OverrideFilter holds optional query parameters for listing overrides.
type OverrideFilter struct {
	StepKey  *string
	TraitKey *string
	State    *string
	Client   *string
	Investor *string
	CaseType *string
	Status   *string
	Page     int
	PageSize int
}

type pgOverrideRepo struct{ db *gorm.DB }

// NewOverrideRepository creates a new OverrideRepository backed by GORM.
func NewOverrideRepository(db *gorm.DB) OverrideRepository {
	return &pgOverrideRepo{db: db}
}

// overrideRow is an intermediate scan struct for raw SQL queries.
type overrideRow struct {
	ID            string          `gorm:"column:id"`
	StepKey       string          `gorm:"column:step_key"`
	TraitKey      string          `gorm:"column:trait_key"`
	State         *string         `gorm:"column:state"`
	Client        *string         `gorm:"column:client"`
	Investor      *string         `gorm:"column:investor"`
	CaseType      *string         `gorm:"column:case_type"`
	Specificity   int             `gorm:"column:specificity"`
	Value         json.RawMessage `gorm:"column:value"`
	EffectiveDate time.Time       `gorm:"column:effective_date"`
	ExpiresDate   *time.Time      `gorm:"column:expires_date"`
	Status        string          `gorm:"column:status"`
	Description   string          `gorm:"column:description"`
	CreatedAt     time.Time       `gorm:"column:created_at"`
	CreatedBy     string          `gorm:"column:created_by"`
	UpdatedAt     time.Time       `gorm:"column:updated_at"`
	UpdatedBy     string          `gorm:"column:updated_by"`
}

func (r *pgOverrideRepo) FindMatchingOverrides(ctx context.Context, caseCtx domain.CaseContext) ([]domain.Override, error) {
	const q = `
SELECT id, step_key, trait_key, state, client, investor, case_type,
       specificity, value, effective_date, expires_date,
       status, description, created_at, created_by, updated_at, updated_by
FROM overrides
WHERE status = 'active'
  AND effective_date <= $1
  AND (expires_date IS NULL OR expires_date > $1)
  AND (state     IS NULL OR state     = $2)
  AND (client    IS NULL OR client    = $3)
  AND (investor  IS NULL OR investor  = $4)
  AND (case_type IS NULL OR case_type = $5)
ORDER BY step_key, trait_key, specificity DESC, effective_date DESC, created_at DESC`

	var rows []overrideRow
	if err := r.db.WithContext(ctx).Raw(q,
		caseCtx.AsOfDate, caseCtx.State, caseCtx.Client, caseCtx.Investor, caseCtx.CaseType,
	).Scan(&rows).Error; err != nil {
		return nil, err
	}
	return rowsToOverrides(rows)
}

// / batchOverrideRow extends overrideRow with the context index for partitioning.
type batchOverrideRow struct {
	CtxIdx        int             `gorm:"column:ctx_idx"`
	ID            string          `gorm:"column:id"`
	StepKey       string          `gorm:"column:step_key"`
	TraitKey      string          `gorm:"column:trait_key"`
	State         *string         `gorm:"column:state"`
	Client        *string         `gorm:"column:client"`
	Investor      *string         `gorm:"column:investor"`
	CaseType      *string         `gorm:"column:case_type"`
	Specificity   int             `gorm:"column:specificity"`
	Value         json.RawMessage `gorm:"column:value"`
	EffectiveDate time.Time       `gorm:"column:effective_date"`
	ExpiresDate   *time.Time      `gorm:"column:expires_date"`
	Status        string          `gorm:"column:status"`
	Description   string          `gorm:"column:description"`
	CreatedAt     time.Time       `gorm:"column:created_at"`
	CreatedBy     string          `gorm:"column:created_by"`
	UpdatedAt     time.Time       `gorm:"column:updated_at"`
	UpdatedBy     string          `gorm:"column:updated_by"`
}

func (b batchOverrideRow) toOverrideRow() overrideRow {
	return overrideRow{
		ID: b.ID, StepKey: b.StepKey, TraitKey: b.TraitKey,
		State: b.State, Client: b.Client, Investor: b.Investor, CaseType: b.CaseType,
		Specificity: b.Specificity, Value: b.Value,
		EffectiveDate: b.EffectiveDate, ExpiresDate: b.ExpiresDate,
		Status: b.Status, Description: b.Description,
		CreatedAt: b.CreatedAt, CreatedBy: b.CreatedBy,
		UpdatedAt: b.UpdatedAt, UpdatedBy: b.UpdatedBy,
	}
}

// buildBulkQuery constructs a VALUES CTE query and its positional argument slice for the
// given contexts. Parameters are positional scalars ($1..$n*5) — no array types, safe with GORM.
// Query and args are built together so a length mismatch is impossible.
func buildBulkQuery(contexts []domain.CaseContext) (string, []any) {
	const selectCols = `
SELECT c.ctx_idx,
       o.id, o.step_key, o.trait_key, o.state, o.client, o.investor, o.case_type,
       o.specificity, o.value, o.effective_date, o.expires_date,
       o.status, o.description, o.created_at, o.created_by, o.updated_at, o.updated_by
FROM contexts c
JOIN overrides o ON (
    o.status = 'active'
    AND o.effective_date  <= c.as_of_date
    AND (o.expires_date IS NULL OR o.expires_date > c.as_of_date)
    AND (o.state     IS NULL OR o.state     = c.state)
    AND (o.client    IS NULL OR o.client    = c.client)
    AND (o.investor  IS NULL OR o.investor  = c.investor)
    AND (o.case_type IS NULL OR o.case_type = c.case_type)
)
ORDER BY c.ctx_idx, o.step_key, o.trait_key,
         o.specificity DESC, o.effective_date DESC, o.created_at DESC`

	rows := make([]string, len(contexts))
	args := make([]any, 0, len(contexts)*5)
	p := 1
	for i, c := range contexts {
		rows[i] = fmt.Sprintf("(%d, $%d::text, $%d::text, $%d::text, $%d::text, $%d::timestamptz)",
			i, p, p+1, p+2, p+3, p+4)
		args = append(args, c.State, c.Client, c.Investor, c.CaseType, c.AsOfDate)
		p += 5
	}
	cte := "WITH contexts(ctx_idx, state, client, investor, case_type, as_of_date) AS (\n    VALUES\n        " +
		strings.Join(rows, ",\n        ") + "\n)" + selectCols

	return cte, args
}

func (r *pgOverrideRepo) FindMatchingOverridesBatch(ctx context.Context, contexts []domain.CaseContext) ([][]domain.Override, error) {
	n := len(contexts)
	if n == 0 {
		return nil, nil
	}

	query, args := buildBulkQuery(contexts)

	var rows []batchOverrideRow
	if err := r.db.WithContext(ctx).Raw(query, args...).Scan(&rows).Error; err != nil {
		return nil, err
	}

	result := make([][]domain.Override, n)
	for i := range result {
		result[i] = []domain.Override{}
	}
	for _, row := range rows {
		o, err := rowToOverride(row.toOverrideRow())
		if err != nil {
			return nil, err
		}
		result[row.CtxIdx] = append(result[row.CtxIdx], o)
	}
	return result, nil
}

func (r *pgOverrideRepo) GetByID(ctx context.Context, id string) (*domain.Override, error) {
	var row overrideRow
	res := r.db.WithContext(ctx).Raw(`
		SELECT id, step_key, trait_key, state, client, investor, case_type,
		       specificity, value, effective_date, expires_date,
		       status, description, created_at, created_by, updated_at, updated_by
		FROM overrides WHERE id = $1`, id,
	).Scan(&row)
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, domain.ErrNotFound
	}
	o, err := rowToOverride(row)
	if err != nil {
		return nil, err
	}
	return &o, nil
}

func (r *pgOverrideRepo) List(ctx context.Context, f OverrideFilter) ([]domain.Override, int64, error) {
	// Build WHERE clause and args shared by both the data query and count query.
	where := " WHERE 1=1"
	var filterArgs []interface{}
	argN := 1

	if f.StepKey != nil {
		where += fmt.Sprintf(" AND step_key = $%d", argN)
		filterArgs = append(filterArgs, *f.StepKey)
		argN++
	}
	if f.TraitKey != nil {
		where += fmt.Sprintf(" AND trait_key = $%d", argN)
		filterArgs = append(filterArgs, *f.TraitKey)
		argN++
	}
	if f.State != nil {
		where += fmt.Sprintf(" AND state = $%d", argN)
		filterArgs = append(filterArgs, *f.State)
		argN++
	}
	if f.Client != nil {
		where += fmt.Sprintf(" AND client = $%d", argN)
		filterArgs = append(filterArgs, *f.Client)
		argN++
	}
	if f.Investor != nil {
		where += fmt.Sprintf(" AND investor = $%d", argN)
		filterArgs = append(filterArgs, *f.Investor)
		argN++
	}
	if f.CaseType != nil {
		where += fmt.Sprintf(" AND case_type = $%d", argN)
		filterArgs = append(filterArgs, *f.CaseType)
		argN++
	}
	if f.Status != nil {
		where += fmt.Sprintf(" AND status = $%d", argN)
		filterArgs = append(filterArgs, *f.Status)
		argN++
	}

	page := f.Page
	if page < 1 {
		page = 1
	}
	pageSize := f.PageSize
	if pageSize < 1 || pageSize > 200 {
		pageSize = 50
	}
	offset := (page - 1) * pageSize

	// Data query with window function for total count.
	dataArgs := append(filterArgs, pageSize, offset) //nolint:gocritic
	dataQ := `SELECT id, step_key, trait_key, state, client, investor, case_type,
	             specificity, value::text AS value, effective_date, expires_date,
	             status, description, created_at, created_by, updated_at, updated_by,
	             COUNT(*) OVER() AS total_count
	      FROM overrides` + where +
		fmt.Sprintf(" ORDER BY created_at DESC LIMIT $%d OFFSET $%d", argN, argN+1)

	type listRow struct {
		ID            string     `gorm:"column:id"`
		StepKey       string     `gorm:"column:step_key"`
		TraitKey      string     `gorm:"column:trait_key"`
		State         *string    `gorm:"column:state"`
		Client        *string    `gorm:"column:client"`
		Investor      *string    `gorm:"column:investor"`
		CaseType      *string    `gorm:"column:case_type"`
		Specificity   int        `gorm:"column:specificity"`
		ValueText     string     `gorm:"column:value"`
		EffectiveDate time.Time  `gorm:"column:effective_date"`
		ExpiresDate   *time.Time `gorm:"column:expires_date"`
		Status        string     `gorm:"column:status"`
		Description   string     `gorm:"column:description"`
		CreatedAt     time.Time  `gorm:"column:created_at"`
		CreatedBy     string     `gorm:"column:created_by"`
		UpdatedAt     time.Time  `gorm:"column:updated_at"`
		UpdatedBy     string     `gorm:"column:updated_by"`
		TotalCount    int64      `gorm:"column:total_count"`
	}

	var rows []listRow
	if err := r.db.WithContext(ctx).Raw(dataQ, dataArgs...).Scan(&rows).Error; err != nil {
		return nil, 0, err
	}
	if len(rows) == 0 {
		return nil, 0, nil
	}

	var total int64 = rows[0].TotalCount
	overrides := make([]domain.Override, 0, len(rows))
	for _, rr := range rows {
		if rr.ID == "" {
			continue
		}
		rawVal := json.RawMessage(rr.ValueText)
		var anyVal any
		if err := json.Unmarshal(rawVal, &anyVal); err != nil {
			return nil, 0, fmt.Errorf("unmarshal value for override %s: %w", rr.ID, err)
		}
		normalized, err := domain.NormalizeTraitValue(rr.TraitKey, anyVal)
		if err != nil {
			normalized = anyVal
		}
		overrides = append(overrides, domain.Override{
			ID:            rr.ID,
			StepKey:       rr.StepKey,
			TraitKey:      rr.TraitKey,
			Selector:      domain.Selector{State: rr.State, Client: rr.Client, Investor: rr.Investor, CaseType: rr.CaseType},
			Specificity:   rr.Specificity,
			Value:         normalized,
			EffectiveDate: rr.EffectiveDate,
			ExpiresDate:   rr.ExpiresDate,
			Status:        rr.Status,
			Description:   rr.Description,
			CreatedAt:     rr.CreatedAt,
			CreatedBy:     rr.CreatedBy,
			UpdatedAt:     rr.UpdatedAt,
			UpdatedBy:     rr.UpdatedBy,
		})
	}
	return overrides, total, nil
}

func (r *pgOverrideRepo) Create(ctx context.Context, o domain.Override) error {
	valJSON, err := json.Marshal(o.Value)
	if err != nil {
		return err
	}
	return r.db.WithContext(ctx).Exec(`
		INSERT INTO overrides
		  (id, step_key, trait_key, state, client, investor, case_type,
		   specificity, value, effective_date, expires_date,
		   status, description, created_at, created_by, updated_at, updated_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)`,
		o.ID, o.StepKey, o.TraitKey,
		o.Selector.State, o.Selector.Client, o.Selector.Investor, o.Selector.CaseType,
		o.Specificity, valJSON, o.EffectiveDate, o.ExpiresDate,
		o.Status, o.Description, o.CreatedAt, o.CreatedBy, o.UpdatedAt, o.UpdatedBy,
	).Error
}

func (r *pgOverrideRepo) Update(ctx context.Context, o domain.Override) error {
	valJSON, err := json.Marshal(o.Value)
	if err != nil {
		return err
	}
	res := r.db.WithContext(ctx).Exec(`
		UPDATE overrides SET
		  step_key=$2, trait_key=$3, state=$4, client=$5, investor=$6, case_type=$7,
		  specificity=$8, value=$9, effective_date=$10, expires_date=$11,
		  description=$12, updated_at=$13, updated_by=$14
		WHERE id=$1`,
		o.ID, o.StepKey, o.TraitKey,
		o.Selector.State, o.Selector.Client, o.Selector.Investor, o.Selector.CaseType,
		o.Specificity, valJSON, o.EffectiveDate, o.ExpiresDate,
		o.Description, o.UpdatedAt, o.UpdatedBy,
	)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *pgOverrideRepo) UpdateStatus(ctx context.Context, id, status, updatedBy string) error {
	res := r.db.WithContext(ctx).Exec(`
		UPDATE overrides SET status=$2, updated_at=NOW(), updated_by=$3 WHERE id=$1`,
		id, status, updatedBy,
	)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *pgOverrideRepo) RecordHistory(ctx context.Context, entry domain.OverrideHistoryEntry) error {
	beforeJSON, err := json.Marshal(entry.SnapshotBefore)
	if err != nil {
		return err
	}
	afterJSON, err := json.Marshal(entry.SnapshotAfter)
	if err != nil {
		return err
	}
	var beforeArg interface{}
	if entry.SnapshotBefore != nil {
		beforeArg = beforeJSON
	}
	return r.db.WithContext(ctx).Exec(`
		INSERT INTO override_history (override_id, action, changed_by, changed_at, snapshot_before, snapshot_after)
		VALUES ($1,$2,$3,$4,$5,$6)`,
		entry.OverrideID, entry.Action, entry.ChangedBy, entry.ChangedAt, beforeArg, afterJSON,
	).Error
}

func (r *pgOverrideRepo) GetHistory(ctx context.Context, overrideID string) ([]domain.OverrideHistoryEntry, error) {
	type histRow struct {
		ID             int64           `gorm:"column:id"`
		OverrideID     string          `gorm:"column:override_id"`
		Action         string          `gorm:"column:action"`
		ChangedBy      string          `gorm:"column:changed_by"`
		ChangedAt      time.Time       `gorm:"column:changed_at"`
		SnapshotBefore json.RawMessage `gorm:"column:snapshot_before"`
		SnapshotAfter  json.RawMessage `gorm:"column:snapshot_after"`
	}
	var rows []histRow
	if err := r.db.WithContext(ctx).Raw(`
		SELECT id, override_id, action, changed_by, changed_at, snapshot_before, snapshot_after
		FROM override_history WHERE override_id = $1
		ORDER BY changed_at DESC`, overrideID,
	).Scan(&rows).Error; err != nil {
		return nil, err
	}
	entries := make([]domain.OverrideHistoryEntry, 0, len(rows))
	for _, row := range rows {
		e := domain.OverrideHistoryEntry{
			ID: row.ID, OverrideID: row.OverrideID,
			Action: row.Action, ChangedBy: row.ChangedBy, ChangedAt: row.ChangedAt,
		}
		if len(row.SnapshotBefore) > 0 && string(row.SnapshotBefore) != "null" {
			json.Unmarshal(row.SnapshotBefore, &e.SnapshotBefore) //nolint:errcheck
		}
		if len(row.SnapshotAfter) > 0 {
			json.Unmarshal(row.SnapshotAfter, &e.SnapshotAfter) //nolint:errcheck
		}
		entries = append(entries, e)
	}
	return entries, nil
}

func (r *pgOverrideRepo) FindConflicts(ctx context.Context) ([]domain.ConflictPair, error) {
	type conflictRow struct {
		IDA      string  `gorm:"column:id_a"`
		IDB      string  `gorm:"column:id_b"`
		StepKey  string  `gorm:"column:step_key"`
		TraitKey string  `gorm:"column:trait_key"`
		Spec     int     `gorm:"column:specificity"`
		AState   *string `gorm:"column:a_state"`
		AClient  *string `gorm:"column:a_client"`
		AInv     *string `gorm:"column:a_investor"`
		ACT      *string `gorm:"column:a_case_type"`
		BState   *string `gorm:"column:b_state"`
		BClient  *string `gorm:"column:b_client"`
		BInv     *string `gorm:"column:b_investor"`
		BCT      *string `gorm:"column:b_case_type"`
		AEff     string  `gorm:"column:a_eff"`
		BEff     string  `gorm:"column:b_eff"`
	}
	var rows []conflictRow
	if err := r.db.WithContext(ctx).Raw(`
		SELECT
		    a.id AS id_a, b.id AS id_b,
		    a.step_key, a.trait_key, a.specificity,
		    a.state AS a_state, a.client AS a_client, a.investor AS a_investor, a.case_type AS a_case_type,
		    b.state AS b_state, b.client AS b_client, b.investor AS b_investor, b.case_type AS b_case_type,
		    a.effective_date::text AS a_eff, b.effective_date::text AS b_eff
		FROM overrides a
		JOIN overrides b
		    ON a.id < b.id
		    AND a.step_key    = b.step_key
		    AND a.trait_key   = b.trait_key
		    AND a.specificity = b.specificity
		WHERE a.status = 'active'
		  AND b.status = 'active'
		  AND a.effective_date < COALESCE(b.expires_date, 'infinity'::date)
		  AND b.effective_date < COALESCE(a.expires_date, 'infinity'::date)
		  AND NOT (a.state     IS NOT NULL AND b.state     IS NOT NULL AND a.state     != b.state)
		  AND NOT (a.client    IS NOT NULL AND b.client    IS NOT NULL AND a.client    != b.client)
		  AND NOT (a.investor  IS NOT NULL AND b.investor  IS NOT NULL AND a.investor  != b.investor)
		  AND NOT (a.case_type IS NOT NULL AND b.case_type IS NOT NULL AND a.case_type != b.case_type)`,
	).Scan(&rows).Error; err != nil {
		return nil, err
	}
	pairs := make([]domain.ConflictPair, 0, len(rows))
	for _, row := range rows {
		selA := selectorDesc(row.AState, row.AClient, row.AInv, row.ACT)
		selB := selectorDesc(row.BState, row.BClient, row.BInv, row.BCT)
		reason := fmt.Sprintf(
			"Same step/trait (%s/%s), same specificity (%d), overlapping effective dates (%s, %s), compatible selectors %s and %s",
			row.StepKey, row.TraitKey, row.Spec, row.AEff, row.BEff, selA, selB,
		)
		pairs = append(pairs, domain.ConflictPair{
			OverrideA: row.IDA, OverrideB: row.IDB,
			StepKey: row.StepKey, TraitKey: row.TraitKey,
			Reason: reason,
		})
	}
	return pairs, nil
}

func selectorDesc(state, client, investor, caseType *string) string {
	result := "{"
	sep := ""
	if state != nil {
		result += sep + "state: " + *state
		sep = ", "
	}
	if client != nil {
		result += sep + "client: " + *client
		sep = ", "
	}
	if investor != nil {
		result += sep + "investor: " + *investor
		sep = ", "
	}
	if caseType != nil {
		result += sep + "caseType: " + *caseType
	}
	return result + "}"
}

func rowsToOverrides(rows []overrideRow) ([]domain.Override, error) {
	result := make([]domain.Override, 0, len(rows))
	for _, row := range rows {
		o, err := rowToOverride(row)
		if err != nil {
			return nil, err
		}
		result = append(result, o)
	}
	return result, nil
}

func rowToOverride(row overrideRow) (domain.Override, error) {
	var rawVal any
	if err := json.Unmarshal(row.Value, &rawVal); err != nil {
		return domain.Override{}, fmt.Errorf("unmarshal value for override %s: %w", row.ID, err)
	}
	normalized, err := domain.NormalizeTraitValue(row.TraitKey, rawVal)
	if err != nil {
		// Return as-is if normalization fails (e.g., unknown trait)
		normalized = rawVal
	}
	return domain.Override{
		ID:            row.ID,
		StepKey:       row.StepKey,
		TraitKey:      row.TraitKey,
		Selector:      domain.Selector{State: row.State, Client: row.Client, Investor: row.Investor, CaseType: row.CaseType},
		Specificity:   row.Specificity,
		Value:         normalized,
		EffectiveDate: row.EffectiveDate,
		ExpiresDate:   row.ExpiresDate,
		Status:        row.Status,
		Description:   row.Description,
		CreatedAt:     row.CreatedAt,
		CreatedBy:     row.CreatedBy,
		UpdatedAt:     row.UpdatedAt,
		UpdatedBy:     row.UpdatedBy,
	}, nil
}
