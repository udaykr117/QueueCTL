package main

import (
	"database/sql"
	"fmt"
	"strconv"
	"time"
)

func GetConfig(key string) (string, error) {
	var value string
	err := db.QueryRow("SELECT value FROM config WHERE key = ?", key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", fmt.Errorf("config key not found: %s", key)
	}
	if err != nil {
		return "", fmt.Errorf("failed to get config: %w", err)
	}
	return value, nil
}

func SetConfig(key, value string) error {
	now := time.Now().UTC()
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS config (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)
	`)
	if err != nil {
		return fmt.Errorf("failed to create config table: %w", err)
	}

	_, err = db.Exec(`
		INSERT INTO config (key, value, updated_at)
		VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET value = ?, updated_at = ?
	`, key, value, now.Format(time.RFC3339), value, now.Format(time.RFC3339))

	if err != nil {
		return fmt.Errorf("failed to set config: %w", err)
	}
	return nil
}

func parseFloat(s string) (float64, error) {
	return strconv.ParseFloat(s, 64)
}

func GetAllConfig() (map[string]string, error) {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS config (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to create config table: %w", err)
	}

	rows, err := db.Query("SELECT key, value FROM config ORDER BY key")
	if err != nil {
		return nil, fmt.Errorf("failed to get all config: %w", err)
	}
	defer rows.Close()

	config := make(map[string]string)
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return nil, fmt.Errorf("failed to scan config: %w", err)
		}
		config[key] = value
	}

	return config, nil
}
func GetConfigWithDefault(key, defaultValue string) string {
	value, err := GetConfig(key)
	if err != nil {
		return defaultValue
	}
	return value
}
func GetConfigInt(key string, defaultValue int) int {
	value, err := GetConfig(key)
	if err != nil {
		return defaultValue
	}

	parsed, err := strconv.Atoi(value)
	if err != nil {
		return defaultValue
	}
	return parsed
}

func GetConfigFloat(key string, defaultValue float64) float64 {
	value, err := GetConfig(key)
	if err != nil {
		return defaultValue
	}
	parsed, err := parseFloat(value)
	if err != nil {
		return defaultValue
	}

	return parsed
}

func GetConfigDuration(key string, defaultValue time.Duration) time.Duration {
	value, err := GetConfig(key)
	if err != nil {
		return defaultValue
	}
	seconds, err := strconv.Atoi(value)
	if err != nil {
		return defaultValue
	}

	return time.Duration(seconds) * time.Second
}
