package db

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gorm.io/gorm"
)

// Migrate runs the complete migration process for the database.
// It runs DDL migrations (schema) then DML migrations (data).
func Migrate(db *gorm.DB) error {
	if err := runSQLMigrations(db, "migrations/ddl"); err != nil {
		fmt.Printf("ERROR: DDL migrations failed: %v\n", err)
		return fmt.Errorf("failed to run DDL migrations: %w", err)
	}
	fmt.Println("Successfully completed DDL migrations")

	if err := runSQLMigrations(db, "migrations/dml"); err != nil {
		fmt.Printf("ERROR: DML migrations failed: %v\n", err)
		return fmt.Errorf("failed to run DML migrations: %w", err)
	}
	fmt.Println("Successfully completed DML migrations")

	return nil
}

func runSQLMigrations(db *gorm.DB, migrationDir string) error {
	if _, err := os.Stat(migrationDir); os.IsNotExist(err) {
		return nil
	}

	var sqlFiles []string
	err := filepath.WalkDir(migrationDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(strings.ToLower(path), ".sql") {
			sqlFiles = append(sqlFiles, path)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to read migration directory %s: %w", migrationDir, err)
	}

	sort.Strings(sqlFiles)

	for _, sqlFile := range sqlFiles {
		if err := executeSQLFile(db, sqlFile); err != nil {
			return fmt.Errorf("failed to execute migration file %s: %w", sqlFile, err)
		}
	}

	return nil
}

func executeSQLFile(db *gorm.DB, filePath string) error {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read SQL file %s: %w", filePath, err)
	}

	sqlContent := string(content)
	if strings.TrimSpace(sqlContent) == "" {
		return nil
	}

	if err := db.Exec(sqlContent).Error; err != nil {
		return fmt.Errorf("failed to execute SQL from file %s: %w", filePath, err)
	}

	fmt.Printf("Successfully executed migration: %s\n", filePath)
	return nil
}
