package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func main() {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://postgres:postgres@localhost:5432/user?sslmode=disable"
	}
	dataDir := os.Getenv("DATA_DIR")
	if dataDir == "" {
		dataDir = "../../sr_backend_assignment_data"
	}

	db, err := gorm.Open(postgres.Open(dbURL), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		log.Fatalf("connect db: %v", err)
	}

	if err := seedSteps(db, dataDir); err != nil {
		log.Fatalf("seed steps: %v", err)
	}
	if err := seedDefaults(db, dataDir); err != nil {
		log.Fatalf("seed defaults: %v", err)
	}
	if err := seedOverrides(db, dataDir); err != nil {
		log.Fatalf("seed overrides: %v", err)
	}
	fmt.Println("Seed complete.")
}

type stepJSON struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Position    int    `json:"position"`
}

func seedSteps(db *gorm.DB, dataDir string) error {
	data, err := os.ReadFile(filepath.Join(dataDir, "steps.json"))
	if err != nil {
		return err
	}
	var steps []stepJSON
	if err := json.Unmarshal(data, &steps); err != nil {
		return err
	}
	for _, s := range steps {
		if err := db.Exec(`INSERT INTO steps (key, name, description, position) VALUES ($1,$2,$3,$4) ON CONFLICT DO NOTHING`,
			s.Key, s.Name, s.Description, s.Position).Error; err != nil {
			return fmt.Errorf("insert step %s: %w", s.Key, err)
		}
	}
	fmt.Printf("Seeded %d steps\n", len(steps))
	return nil
}

func seedDefaults(db *gorm.DB, dataDir string) error {
	data, err := os.ReadFile(filepath.Join(dataDir, "defaults.json"))
	if err != nil {
		return err
	}
	var defaults map[string]map[string]json.RawMessage
	if err := json.Unmarshal(data, &defaults); err != nil {
		return err
	}
	count := 0
	for stepKey, traits := range defaults {
		for traitKey, val := range traits {
			if err := db.Exec(`INSERT INTO defaults (step_key, trait_key, value) VALUES ($1,$2,$3) ON CONFLICT DO NOTHING`,
				stepKey, traitKey, val).Error; err != nil {
				return fmt.Errorf("insert default %s/%s: %w", stepKey, traitKey, err)
			}
			count++
		}
	}
	fmt.Printf("Seeded %d defaults\n", count)
	return nil
}

type selectorJSON struct {
	State    *string `json:"state,omitempty"`
	Client   *string `json:"client,omitempty"`
	Investor *string `json:"investor,omitempty"`
	CaseType *string `json:"caseType,omitempty"`
}

func (s selectorJSON) specificity() int {
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

type overrideJSON struct {
	ID            string          `json:"id"`
	StepKey       string          `json:"stepKey"`
	TraitKey      string          `json:"traitKey"`
	Selector      selectorJSON    `json:"selector"`
	Value         json.RawMessage `json:"value"`
	EffectiveDate string          `json:"effectiveDate"`
	ExpiresDate   *string         `json:"expiresDate,omitempty"`
	Status        string          `json:"status"`
	Description   string          `json:"description"`
	CreatedBy     string          `json:"createdBy"`
}

func seedOverrides(db *gorm.DB, dataDir string) error {
	data, err := os.ReadFile(filepath.Join(dataDir, "overrides.json"))
	if err != nil {
		return err
	}
	var overrides []overrideJSON
	if err := json.Unmarshal(data, &overrides); err != nil {
		return err
	}
	now := time.Now().UTC()
	count := 0
	for _, o := range overrides {
		effDate, err := time.Parse("2006-01-02", o.EffectiveDate)
		if err != nil {
			return fmt.Errorf("parse effectiveDate for %s: %w", o.ID, err)
		}
		var expiresDate interface{}
		if o.ExpiresDate != nil {
			t, err := time.Parse("2006-01-02", *o.ExpiresDate)
			if err != nil {
				return fmt.Errorf("parse expiresDate for %s: %w", o.ID, err)
			}
			expiresDate = t
		}
		spec := o.Selector.specificity()
		createdBy := o.CreatedBy
		if createdBy == "" {
			createdBy = "seed"
		}
		err = db.Exec(`
			INSERT INTO overrides
			  (id, step_key, trait_key, state, client, investor, case_type,
			   specificity, value, effective_date, expires_date,
			   status, description, created_at, created_by, updated_at, updated_by)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)
			ON CONFLICT DO NOTHING`,
			o.ID, o.StepKey, o.TraitKey,
			o.Selector.State, o.Selector.Client, o.Selector.Investor, o.Selector.CaseType,
			spec, o.Value, effDate, expiresDate,
			o.Status, o.Description, now, createdBy, now, "seed",
		).Error
		if err != nil {
			return fmt.Errorf("insert override %s: %w", o.ID, err)
		}
		// Record creation in history
		afterJSON, _ := json.Marshal(map[string]any{
			"id": o.ID, "stepKey": o.StepKey, "traitKey": o.TraitKey,
			"status": o.Status, "createdBy": createdBy,
		})
		db.Exec(`
			INSERT INTO override_history (override_id, action, changed_by, changed_at, snapshot_after)
			VALUES ($1,'created',$2,$3,$4) ON CONFLICT DO NOTHING`,
			o.ID, createdBy, now, afterJSON,
		) //nolint:errcheck
		count++
	}
	fmt.Printf("Seeded %d overrides\n", count)
	return nil
}
