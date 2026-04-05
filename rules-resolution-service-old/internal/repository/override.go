package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/abir/rules-resolution-service/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type pgOverrideRepo struct{ db *pgxpool.Pool }

func NewOverrideRepository(db *pgxpool.Pool) OverrideRepository {
	return &pgOverrideRepo{db}
}

const overrideSelectCols = `
	id, step_key, trait_key,
	state, client, investor, case_type,
	specificity, value,
	effective_date, expires_date, status,
	description, created_at, created_by, updated_at, updated_by`

// FindMatchingOverrides returns all active overrides matching the case context,
// ordered by (step_key, trait_key, specificity DESC, effective_date DESC, created_at DESC).
// The order guarantees that group[0] is always the winner per (step, trait) in the resolver.
func (r *pgOverrideRepo) FindMatchingOverrides(ctx context.Context, caseCtx domain.CaseContext) ([]domain.Override, error) {
	asOf := caseCtx.AsOfDate
	if asOf.IsZero() {
		asOf = time.Now().UTC()
	}
	rows, err := r.db.Query(ctx, `
		SELECT `+overrideSelectCols+`
		FROM overrides
		WHERE status = 'active'
		  AND effective_date <= $1
		  AND (expires_date IS NULL OR expires_date > $1)
		  AND (state     IS NULL OR state     = $2)
		  AND (client    IS NULL OR client    = $3)
		  AND (investor  IS NULL OR investor  = $4)
		  AND (case_type IS NULL OR case_type = $5)
		ORDER BY step_key, trait_key, specificity DESC, effective_date DESC, created_at DESC`,
		asOf, caseCtx.State, caseCtx.Client, caseCtx.Investor, caseCtx.CaseType,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanOverrides(rows)
}

func (r *pgOverrideRepo) GetByID(ctx context.Context, id string) (*domain.Override, error) {
	rows, err := r.db.Query(ctx,
		`SELECT `+overrideSelectCols+` FROM overrides WHERE id = $1`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	overrides, err := scanOverrides(rows)
	if err != nil {
		return nil, err
	}
	if len(overrides) == 0 {
		return nil, domain.ErrNotFound
	}
	return &overrides[0], nil
}

func (r *pgOverrideRepo) List(ctx context.Context, f OverrideFilter) ([]domain.Override, error) {
	where := []string{"1=1"}
	args := []any{}
	n := 1

	add := func(col string, val *string) {
		if val != nil {
			where = append(where, fmt.Sprintf("%s = $%d", col, n))
			args = append(args, *val)
			n++
		}
	}
	add("step_key", f.StepKey)
	add("trait_key", f.TraitKey)
	add("state", f.State)
	add("client", f.Client)
	add("investor", f.Investor)
	add("case_type", f.CaseType)
	add("status", f.Status)

	q := `SELECT ` + overrideSelectCols + ` FROM overrides WHERE ` +
		strings.Join(where, " AND ") +
		` ORDER BY step_key, trait_key, specificity DESC, effective_date DESC`

	rows, err := r.db.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanOverrides(rows)
}

func (r *pgOverrideRepo) Create(ctx context.Context, o domain.Override) error {
	valJSON, err := json.Marshal(o.Value)
	if err != nil {
		return err
	}
	_, err = r.db.Exec(ctx, `
		INSERT INTO overrides
			(id, step_key, trait_key, state, client, investor, case_type,
			 specificity, value, effective_date, expires_date, status,
			 description, created_at, created_by, updated_at, updated_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$14,$15)`,
		o.ID, o.StepKey, o.TraitKey,
		o.Selector.State, o.Selector.Client, o.Selector.Investor, o.Selector.CaseType,
		o.Specificity, valJSON,
		o.EffectiveDate, o.ExpiresDate, o.Status,
		o.Description, o.CreatedAt, o.CreatedBy,
	)
	return err
}

func (r *pgOverrideRepo) Update(ctx context.Context, o domain.Override) error {
	valJSON, err := json.Marshal(o.Value)
	if err != nil {
		return err
	}
	tag, err := r.db.Exec(ctx, `
		UPDATE overrides SET
			step_key = $2, trait_key = $3,
			state = $4, client = $5, investor = $6, case_type = $7,
			specificity = $8, value = $9,
			effective_date = $10, expires_date = $11, status = $12,
			description = $13, updated_at = $14, updated_by = $15
		WHERE id = $1`,
		o.ID, o.StepKey, o.TraitKey,
		o.Selector.State, o.Selector.Client, o.Selector.Investor, o.Selector.CaseType,
		o.Specificity, valJSON,
		o.EffectiveDate, o.ExpiresDate, o.Status,
		o.Description, o.UpdatedAt, o.UpdatedBy,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *pgOverrideRepo) UpdateStatus(ctx context.Context, id, status, updatedBy string) error {
	tag, err := r.db.Exec(ctx,
		`UPDATE overrides SET status = $2, updated_at = $3, updated_by = $4 WHERE id = $1`,
		id, status, time.Now().UTC(), updatedBy,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *pgOverrideRepo) RecordHistory(ctx context.Context, e domain.OverrideHistoryEntry) error {
	var beforeJSON []byte
	if e.SnapshotBefore != nil {
		var err error
		beforeJSON, err = json.Marshal(e.SnapshotBefore)
		if err != nil {
			return err
		}
	}
	afterJSON, err := json.Marshal(e.SnapshotAfter)
	if err != nil {
		return err
	}
	_, err = r.db.Exec(ctx, `
		INSERT INTO override_history (override_id, action, changed_by, changed_at, snapshot_before, snapshot_after)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		e.OverrideID, e.Action, e.ChangedBy, e.ChangedAt, beforeJSON, afterJSON,
	)
	return err
}

func (r *pgOverrideRepo) GetHistory(ctx context.Context, overrideID string) ([]domain.OverrideHistoryEntry, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, override_id, action, changed_by, changed_at, snapshot_before, snapshot_after
		FROM override_history
		WHERE override_id = $1
		ORDER BY changed_at DESC`,
		overrideID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var entries []domain.OverrideHistoryEntry
	for rows.Next() {
		var e domain.OverrideHistoryEntry
		var beforeRaw, afterRaw []byte
		if err := rows.Scan(
			&e.ID, &e.OverrideID, &e.Action, &e.ChangedBy, &e.ChangedAt,
			&beforeRaw, &afterRaw,
		); err != nil {
			return nil, err
		}
		if beforeRaw != nil {
			var v any
			_ = json.Unmarshal(beforeRaw, &v)
			e.SnapshotBefore = v
		}
		var v any
		_ = json.Unmarshal(afterRaw, &v)
		e.SnapshotAfter = v
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

func (r *pgOverrideRepo) FindConflictCandidates(ctx context.Context) ([]domain.Override, error) {
	rows, err := r.db.Query(ctx, `
		SELECT `+overrideSelectCols+`
		FROM overrides
		WHERE status != 'archived'
		ORDER BY step_key, trait_key, specificity`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanOverrides(rows)
}

// scanOverrides scans all rows into Override structs.
func scanOverrides(rows pgx.Rows) ([]domain.Override, error) {
	var result []domain.Override
	for rows.Next() {
		o, err := scanOneOverride(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, o)
	}
	return result, rows.Err()
}

func scanOneOverride(rows pgx.Rows) (domain.Override, error) {
	var o domain.Override
	var valRaw []byte
	var expiresDate *time.Time
	var updatedBy *string

	err := rows.Scan(
		&o.ID, &o.StepKey, &o.TraitKey,
		&o.Selector.State, &o.Selector.Client, &o.Selector.Investor, &o.Selector.CaseType,
		&o.Specificity, &valRaw,
		&o.EffectiveDate, &expiresDate, &o.Status,
		&o.Description, &o.CreatedAt, &o.CreatedBy, &o.UpdatedAt, &updatedBy,
	)
	if err != nil {
		return o, err
	}
	if expiresDate != nil {
		o.ExpiresDate = expiresDate
	}
	if updatedBy != nil {
		o.UpdatedBy = *updatedBy
	}
	if valRaw != nil {
		var v any
		if err := json.Unmarshal(valRaw, &v); err != nil {
			return o, fmt.Errorf("unmarshal value for override %s: %w", o.ID, err)
		}
		o.Value = v
	}
	return o, nil
}

// EnsureNotFound wraps pgx.ErrNoRows → domain.ErrNotFound.
func EnsureNotFound(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrNotFound
	}
	return err
}
