package repository

import (
	"context"

	"github.com/abir/rules-resolution-service/internal/domain"
	"github.com/jackc/pgx/v5/pgxpool"
)

type pgStepRepo struct{ db *pgxpool.Pool }

func NewStepRepository(db *pgxpool.Pool) StepRepository {
	return &pgStepRepo{db}
}

func (r *pgStepRepo) GetAll(ctx context.Context) ([]domain.Step, error) {
	rows, err := r.db.Query(ctx,
		`SELECT key, name, description, position FROM steps ORDER BY position`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var steps []domain.Step
	for rows.Next() {
		var s domain.Step
		if err := rows.Scan(&s.Key, &s.Name, &s.Description, &s.Position); err != nil {
			return nil, err
		}
		steps = append(steps, s)
	}
	return steps, rows.Err()
}
