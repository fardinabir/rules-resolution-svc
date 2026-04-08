package repository

import (
	"context"
	"encoding/json"

	"gorm.io/gorm"

	"github.com/fardinabir/go-svc-boilerplate/internal/domain"
)

// DefaultRepository loads the defaults reference table.
type DefaultRepository interface {
	GetAll(ctx context.Context) (map[domain.StepTrait]any, error)
}

type pgDefaultRepo struct{ db *gorm.DB }

func NewDefaultRepository(db *gorm.DB) DefaultRepository {
	return &pgDefaultRepo{db: db}
}

func (r *pgDefaultRepo) GetAll(ctx context.Context) (map[domain.StepTrait]any, error) {
	type defaultRow struct {
		StepKey  string          `gorm:"column:step_key"`
		TraitKey string          `gorm:"column:trait_key"`
		Value    json.RawMessage `gorm:"column:value"`
	}
	var rows []defaultRow
	if err := r.db.WithContext(ctx).Raw(`SELECT step_key, trait_key, value FROM defaults`).Scan(&rows).Error; err != nil {
		return nil, err
	}
	result := make(map[domain.StepTrait]any, len(rows))
	for _, row := range rows {
		var rawVal any
		if err := json.Unmarshal(row.Value, &rawVal); err != nil {
			continue
		}
		normalized, err := domain.NormalizeTraitValue(row.TraitKey, rawVal)
		if err != nil {
			normalized = rawVal
		}
		result[domain.StepTrait{StepKey: row.StepKey, TraitKey: row.TraitKey}] = normalized
	}
	return result, nil
}
