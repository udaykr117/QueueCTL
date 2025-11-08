package main

import (
	"database/sql"
	"fmt"
	"time"
)

func IncrementMetric(key string) error {
	now := time.Now().UTC()
	_, err := db.Exec(`
	INSERT INTO metrics (key , value , updated_at)
	VALUES (?,1,?)
	ON CONFLICT(key) DO UPDATE SET value = value +	1 , updated_at =?
	`, key, now.Format(time.RFC3339), now.Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("failed to increment metric: %w", err)
	}
	return nil
}

func GetMetric(key string) (int64, error) {
	var value int64
	err := db.QueryRow("SELECT value FROM metrics WHERE key =?", key).Scan(&value)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("failed to get metric: %w", err)
	}
	return value, nil
}

func GetAllMetrics() (map[string]int64, error) {
	metrics := make(map[string]int64)
	rows, err := db.Query("SELECT key, value FROM metrics ORDER BY key")
	if err != nil {
		return nil, fmt.Errorf("failed to get metrics: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var key string
		var value int64
		if err := rows.Scan(&key, &value); err != nil {
			return nil, fmt.Errorf("failed to scan metric: %W", err)
		}
		metrics[key] = value
	}
	return metrics, nil
}

func RecordJobExecution(jobID string, startedAt time.Time, completedAt time.Time, success bool, timeout bool, errMsg string) error {
	durationMs := int64(0)
	if !completedAt.IsZero() {
		durationMs = completedAt.Sub(startedAt).Milliseconds()
	}
	successInt := 0
	if success {
		successInt = 1
	}
	timeoutInt := 0
	if timeout {
		timeoutInt = 1
	}

	_, err := db.Exec(`
	INSERT INTO job_executions (job_id, started_at, completed_at, duration_ms, success, timeout, error)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		`, jobID, startedAt.Format(time.RFC3339), completedAt.Format(time.RFC3339), durationMs, successInt, timeoutInt, errMsg)
	if err != nil {
		return fmt.Errorf("failed to record job execution: %w", err)
	}
	return nil
}

func GetExecutionStats() (map[string]interface{}, error) {
	stats := make(map[string]interface{})

	totalProcessed, _ := GetMetric("jobs_processed")
	totalSucceeded, _ := GetMetric("jobs_succeeded")
	totalFailed, _ := GetMetric("jobs_failed")
	totalTimeout, _ := GetMetric("jobs_timeout")

	stats["total_processed"] = totalProcessed
	stats["total_succeeded"] = totalSucceeded
	stats["total_failed"] = totalFailed
	stats["total_timeout"] = totalTimeout

	var successRate float64
	if totalProcessed > 0 {
		successRate = float64(totalSucceeded) / float64(totalProcessed) * 100
	}
	stats["success_rate"] = successRate

	var avgDuration sql.NullFloat64
	err := db.QueryRow(`
		SELECT AVG(duration_ms) FROM job_executions
		WHERE completed_at IS NOT NULL
		AND started_at > datetime('now', '-24 hours')
	`).Scan(&avgDuration)

	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("failed to get avg duration: %w", err)

	}
	if avgDuration.Valid {
		stats["avg_duration_ms"] = avgDuration.Float64
	} else {
		stats["avg_duration_ms"] = 0.0
	}

	var recentCount int64
	err = db.QueryRow(`
		SELECT COUNT(*) FROM job_executions
		WHERE started_at > datetime('now', '-24 hours')
	`).Scan(&recentCount)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("failed to get recent count: %w", err)
	}
	stats["recent_24h_count"] = recentCount

	return stats, nil
}

func GetRecentExecutions(limit int) ([]map[string]interface{}, error) {
	rows, err := db.Query(`
		SELECT e.job_id,j.command,j.state, e.started_at, e.completed_at, e.duration_ms, e.success, e.timeout, e.error
		FROM job_executions e
		JOIN jobs j ON e.job_id = j.id
		ORDER BY started_at DESC
		LIMIT ? `, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to get recent executions: %w", err)
	}
	defer rows.Close()
	var executions []map[string]interface{}
	for rows.Next() {
		var jobID, command, state, startedAtStr, completedAtStr sql.NullString
		var durationMs sql.NullInt64
		var success, timeout int
		var errorMsg sql.NullString

		if err := rows.Scan(&jobID, &command, &state, &startedAtStr, &completedAtStr, &durationMs, &success, &timeout, &errorMsg); err != nil {
			return nil, fmt.Errorf("failed to scan execution: %w", err)
		}

		exec := make(map[string]interface{})
		exec["job_id"] = jobID.String
		exec["command"] = command.String
		exec["state"] = state.String

		if startedAtStr.Valid {
			exec["started_at"] = startedAtStr.String
		}
		if completedAtStr.Valid {
			exec["completed_at"] = completedAtStr.String
		}
		if durationMs.Valid {
			exec["duration_ms"] = durationMs.Int64
		}
		exec["success"] = success == 1
		exec["timeout"] = timeout == 1
		if errorMsg.Valid {
			exec["error"] = errorMsg.String
		}

		executions = append(executions, exec)
	}

	return executions, nil
}
