package repository

import (
	"context"

	"gorm.io/gorm"

	"github.com/fardinabir/go-svc-boilerplate/internal/domain"
)

// StepRepository loads the steps reference table.
type StepRepository interface {
	GetAll(ctx context.Context) ([]domain.Step, error)
}

type pgStepRepo struct{ db *gorm.DB }

func NewStepRepository(db *gorm.DB) StepRepository {
	return &pgStepRepo{db: db}
}

func (r *pgStepRepo) GetAll(ctx context.Context) ([]domain.Step, error) {
	type stepRow struct {
		Key         string `gorm:"column:key"`
		Name        string `gorm:"column:name"`
		Description string `gorm:"column:description"`
		Position    int    `gorm:"column:position"`
	}
	var rows []stepRow
	if err := r.db.WithContext(ctx).Raw(`SELECT key, name, description, position FROM steps ORDER BY position`).Scan(&rows).Error; err != nil {
		return nil, err
	}
	steps := make([]domain.Step, len(rows))
	for i, r := range rows {
		steps[i] = domain.Step{Key: r.Key, Name: r.Name, Description: r.Description, Position: r.Position}
	}
	return steps, nil
}
