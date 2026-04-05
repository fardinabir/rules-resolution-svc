package repository

import (
	"context"
	"encoding/json"

	"github.com/abir/rules-resolution-service/internal/domain"
	"github.com/jackc/pgx/v5/pgxpool"
)

type pgDefaultRepo struct{ db *pgxpool.Pool }

func NewDefaultRepository(db *pgxpool.Pool) DefaultRepository {
	return &pgDefaultRepo{db}
}

func (r *pgDefaultRepo) GetAll(ctx context.Context) (map[domain.StepTrait]any, error) {
	rows, err := r.db.Query(ctx, `SELECT step_key, trait_key, value FROM defaults`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := make(map[domain.StepTrait]any)
	for rows.Next() {
		var stepKey, traitKey string
		var raw []byte
		if err := rows.Scan(&stepKey, &traitKey, &raw); err != nil {
			return nil, err
		}
		var val any
		if err := json.Unmarshal(raw, &val); err != nil {
			return nil, err
		}
		m[domain.StepTrait{StepKey: stepKey, TraitKey: traitKey}] = val
	}
	return m, rows.Err()
}
