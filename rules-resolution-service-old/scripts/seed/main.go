package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/jackc/pgx/v5"
)

func main() {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://rrs:rrs@localhost:5432/rrs?sslmode=disable"
	}

	dataDir := os.Getenv("DATA_DIR")
	if dataDir == "" {
		dataDir = "../../sr_backend_assignment_data"
	}

	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		log.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)

	if err := seedSteps(ctx, conn, dataDir); err != nil {
		log.Fatalf("seed steps: %v", err)
	}
	if err := seedDefaults(ctx, conn, dataDir); err != nil {
		log.Fatalf("seed defaults: %v", err)
	}
	if err := seedOverrides(ctx, conn, dataDir); err != nil {
		log.Fatalf("seed overrides: %v", err)
	}

	fmt.Println("Seed complete.")
}

type stepRow struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Position    int    `json:"position"`
}

func seedSteps(ctx context.Context, conn *pgx.Conn, dataDir string) error {
	data, err := os.ReadFile(dataDir + "/steps.json")
	if err != nil {
		return err
	}
	var steps []stepRow
	if err := json.Unmarshal(data, &steps); err != nil {
		return err
	}
	for _, s := range steps {
		_, err := conn.Exec(ctx,
			`INSERT INTO steps (key, name, description, position)
             VALUES ($1, $2, $3, $4)
             ON CONFLICT (key) DO NOTHING`,
			s.Key, s.Name, s.Description, s.Position,
		)
		if err != nil {
			return fmt.Errorf("insert step %s: %w", s.Key, err)
		}
	}
	fmt.Printf("  steps: %d rows\n", len(steps))
	return nil
}

func seedDefaults(ctx context.Context, conn *pgx.Conn, dataDir string) error {
	data, err := os.ReadFile(dataDir + "/defaults.json")
	if err != nil {
		return err
	}
	// defaults.json is a map[stepKey]map[traitKey]value
	var raw map[string]map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	count := 0
	for stepKey, traits := range raw {
		for traitKey, val := range traits {
			_, err := conn.Exec(ctx,
				`INSERT INTO defaults (step_key, trait_key, value)
                 VALUES ($1, $2, $3)
                 ON CONFLICT (step_key, trait_key) DO NOTHING`,
				stepKey, traitKey, val,
			)
			if err != nil {
				return fmt.Errorf("insert default %s.%s: %w", stepKey, traitKey, err)
			}
			count++
		}
	}
	fmt.Printf("  defaults: %d rows\n", count)
	return nil
}

type overrideRow struct {
	ID            string          `json:"id"`
	StepKey       string          `json:"stepKey"`
	TraitKey      string          `json:"traitKey"`
	Selector      json.RawMessage `json:"selector"`
	Value         json.RawMessage `json:"value"`
	EffectiveDate string          `json:"effectiveDate"`
	ExpiresDate   *string         `json:"expiresDate"`
	Status        string          `json:"status"`
	Description   string          `json:"description"`
	CreatedBy     string          `json:"createdBy"`
}

type selector struct {
	State    *string `json:"state"`
	Client   *string `json:"client"`
	Investor *string `json:"investor"`
	CaseType *string `json:"caseType"`
}

func computeSpecificity(s selector) int {
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

func seedOverrides(ctx context.Context, conn *pgx.Conn, dataDir string) error {
	data, err := os.ReadFile(dataDir + "/overrides.json")
	if err != nil {
		return err
	}
	var rows []overrideRow
	if err := json.Unmarshal(data, &rows); err != nil {
		return err
	}

	now := time.Now().UTC()

	for _, r := range rows {
		var sel selector
		if err := json.Unmarshal(r.Selector, &sel); err != nil {
			return fmt.Errorf("parse selector for %s: %w", r.ID, err)
		}
		specificity := computeSpecificity(sel)

		effDate, err := time.Parse("2006-01-02", r.EffectiveDate)
		if err != nil {
			return fmt.Errorf("parse effectiveDate for %s: %w", r.ID, err)
		}

		var expiresDate *time.Time
		if r.ExpiresDate != nil {
			t, err := time.Parse("2006-01-02", *r.ExpiresDate)
			if err != nil {
				return fmt.Errorf("parse expiresDate for %s: %w", r.ID, err)
			}
			expiresDate = &t
		}

		_, err = conn.Exec(ctx,
			`INSERT INTO overrides
                (id, step_key, trait_key, state, client, investor, case_type,
                 specificity, value, effective_date, expires_date, status,
                 description, created_at, created_by, updated_at, updated_by)
             VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$14,$15)
             ON CONFLICT (id) DO NOTHING`,
			r.ID, r.StepKey, r.TraitKey,
			sel.State, sel.Client, sel.Investor, sel.CaseType,
			specificity, r.Value,
			effDate, expiresDate, r.Status,
			r.Description, now, r.CreatedBy,
		)
		if err != nil {
			return fmt.Errorf("insert override %s: %w", r.ID, err)
		}

		// Record creation in history
		snapshot, _ := json.Marshal(map[string]any{
			"id": r.ID, "stepKey": r.StepKey, "traitKey": r.TraitKey,
			"selector": sel, "specificity": specificity,
			"value": json.RawMessage(r.Value), "effectiveDate": r.EffectiveDate,
			"status": r.Status, "description": r.Description,
			"createdBy": r.CreatedBy,
		})
		_, err = conn.Exec(ctx,
			`INSERT INTO override_history
                (override_id, action, changed_by, changed_at, snapshot_before, snapshot_after)
             VALUES ($1, 'created', $2, $3, NULL, $4)
             ON CONFLICT DO NOTHING`,
			r.ID, r.CreatedBy, now, snapshot,
		)
		if err != nil {
			// History insert conflict clause doesn't work on BIGSERIAL PK — ignore duplicate seeds
			_ = err
		}
	}
	fmt.Printf("  overrides: %d rows\n", len(rows))
	return nil
}
