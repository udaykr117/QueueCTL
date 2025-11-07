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
			updated_at TEXT NOT NULL,
			last_error TEXT DEFAULT '',
			next_retry_at TEXT,
			locked_by TEXT,
			locked_at TEXT
		);
		CREATE INDEX IF NOT EXISTS idx_state ON jobs(state);
		CREATE INDEX IF NOT EXISTS idx_locked_by ON jobs(locked_by);
		`
	migrations := []string{
		"ALTER TABLE jobs ADD COLUMN last_error TEXT DEFAULT ''",
		"ALTER TABLE jobs ADD COLUMN next_retry_at TEXT",
		"ALTER TABLE jobs ADD COLUMN locked_by TEXT",
		"ALTER TABLE jobs ADD COLUMN locked_at TEXT",
	}
	for _, migration := range migrations {
		_, _ = db.Exec(migration)
	}

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

func GetNextPendingJob(workerID string) (*Job, error) {
	now := time.Now().UTC()
	nowStr := now.Format(time.RFC3339)

	var jobID string
	err := db.QueryRow(`
		SELECT id FROM jobs
		WHERE state = 'pending' 
		AND (locked_by IS NULL OR datetime(locked_at) < datetime('now', '-5 minutes'))
		AND (next_retry_at IS NULL OR datetime(next_retry_at) <= datetime('now'))
		ORDER BY created_at ASC
		LIMIT 1
	`).Scan(&jobID)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to find pending job: %w", err)
	}

	result, err := db.Exec(`
		UPDATE jobs 
		SET locked_by = ?, locked_at = ?, state = ?
		WHERE id = ? AND state = 'pending'
	`, workerID, nowStr, string(StateProcessing), jobID)

	if err != nil {
		return nil, fmt.Errorf("failed to claim job: %w", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return nil, nil // No job available (another worker claimed it)
	}

	var job Job
	var createdAtStr, updatedAtStr sql.NullString
	var lastError, nextRetryAt, lockedBy, lockedAt sql.NullString
	err = db.QueryRow(`
		SELECT id, command, state, attempts, max_retries, created_at, updated_at,
		       last_error, next_retry_at, locked_by, locked_at
		FROM jobs
		WHERE locked_by = ? AND state = ?
		ORDER BY locked_at DESC
		LIMIT 1
	`, workerID, string(StateProcessing)).Scan(
		&job.ID, &job.Command, &job.State, &job.Attempts, &job.MaxRetries,
		&createdAtStr, &updatedAtStr, &lastError, &nextRetryAt,
		&lockedBy, &lockedAt,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get claimed job: %w", err)
	}
	if createdAtStr.Valid {
		job.CreatedAt, _ = time.Parse(time.RFC3339, createdAtStr.String)
	}
	if updatedAtStr.Valid {
		job.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAtStr.String)
	}

	return &job, nil
}
func UpdateJobState(jobID string, state JobState, lastError string) error {
	now := time.Now().UTC()
	_, err := db.Exec(`
		UPDATE jobs
		SET state = ?, last_error = ?, updated_at = ?, locked_by = NULL, locked_at = NULL
		WHERE id = ?
	`, string(state), lastError, now.Format(time.RFC3339), jobID)

	if err != nil {
		return fmt.Errorf("failed to update job state: %w", err)
	}
	return nil
}

func IncrementJobAttempts(jobID string) error {
	now := time.Now().UTC()
	_, err := db.Exec(`
		UPDATE jobs
		SET attempts = attempts + 1, updated_at = ?
		WHERE id = ?
	`, now.Format(time.RFC3339), jobID)

	if err != nil {
		return fmt.Errorf("failed to increment attempts: %w", err)
	}
	return nil
}
func SetNextRetryAt(jobID string, nextRetry time.Time) error {
	now := time.Now().UTC()
	_, err := db.Exec(`
		UPDATE jobs
		SET next_retry_at = ?, updated_at = ?
		WHERE id = ?
	`, nextRetry.Format(time.RFC3339), now.Format(time.RFC3339), jobID)

	if err != nil {
		return fmt.Errorf("failed to set next retry: %w", err)
	}
	return nil
}

func GetJobCountsByState() (map[JobState]int, error) {
	counts := make(map[JobState]int)
	rows, err := db.Query(`
		SELECT state, COUNT(*) as count
		FROM jobs
		GROUP BY state
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to get job counts: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var state string
		var count int
		if err := rows.Scan(&state, &count); err != nil {
			return nil, fmt.Errorf("failed to scan job count: %w", err)
		}
		counts[JobState(state)] = count
	}

	return counts, nil
}

func GetJobsByState(state JobState) ([]*Job, error) {
	rows, err := db.Query(`
		SELECT id, command, state, attempts, max_retries, created_at, updated_at
		FROM jobs
		WHERE state = ?
		ORDER BY created_at ASC
	`, string(state))
	if err != nil {
		return nil, fmt.Errorf("failed to get jobs by state: %w", err)
	}
	defer rows.Close()

	var jobs []*Job
	for rows.Next() {
		var job Job
		var createdAtStr, updatedAtStr string

		if err := rows.Scan(
			&job.ID, &job.Command, &job.State, &job.Attempts, &job.MaxRetries,
			&createdAtStr, &updatedAtStr,
		); err != nil {
			return nil, fmt.Errorf("failed to scan job: %w", err)
		}

		if createdAt, err := time.Parse(time.RFC3339, createdAtStr); err == nil {
			job.CreatedAt = createdAt
		}
		if updatedAt, err := time.Parse(time.RFC3339, updatedAtStr); err == nil {
			job.UpdatedAt = updatedAt
		}

		jobs = append(jobs, &job)
	}

	return jobs, nil
}

func GetAllJobs() ([]*Job, error) {
	rows, err := db.Query(`
		SELECT id, command, state, attempts, max_retries, created_at, updated_at
		FROM jobs
		ORDER BY created_at ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to get all jobs: %w", err)
	}
	defer rows.Close()

	var jobs []*Job
	for rows.Next() {
		var job Job
		var createdAtStr, updatedAtStr string

		if err := rows.Scan(
			&job.ID, &job.Command, &job.State, &job.Attempts, &job.MaxRetries,
			&createdAtStr, &updatedAtStr,
		); err != nil {
			return nil, fmt.Errorf("failed to scan job: %w", err)
		}
		if createdAt, err := time.Parse(time.RFC3339, createdAtStr); err == nil {
			job.CreatedAt = createdAt
		}
		if updatedAt, err := time.Parse(time.RFC3339, updatedAtStr); err == nil {
			job.UpdatedAt = updatedAt
		}

		jobs = append(jobs, &job)
	}

	return jobs, nil
}
