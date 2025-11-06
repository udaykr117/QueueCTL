package main

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

var db *sql.DB

func initDB(dataDir string) error {
	dbPath := filepath.Join(dataDir, "jobs.db")

	err := os.MkdirAll(dataDir, 0755)
	if err != nil {
		return fmt.Errorf("failed to create data directory: %w", err)
	}

	db, err = sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_foreign_keys=1")
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}

	schema := `
		CREATE TABLE IF NOT EXISTS jobs (
			id TEXT PRIMARY KEY,
			command TEXT NOT NULL,
			state TEXT NOT NULL,
			attempts INTEGER NOT NULL DEFAULT 0,
			max_retries INTEGER NOT NULL DEFAULT 3,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_state ON jobs(state);
		`
	if _, err = db.Exec(schema); err != nil {
		db.Close()
		return fmt.Errorf("failed to initialize schema: %w", err)
	}

	return err
}
func CloseDB() error {
	if db != nil {
		return db.Close()
	}
	return nil
}

func CreateJob(job *Job) error {
	now := time.Now().UTC()
	job.CreatedAt = now
	job.UpdatedAt = now
	_, err := db.Exec(`
		INSERT INTO jobs (id, command, state, attempts, max_retries, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		job.ID,
		job.Command,
		string(job.State),
		job.Attempts,
		job.MaxRetries,
		now.Format(time.RFC3339),
		now.Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("failed to create job: %w", err)
	}

	return nil
}
