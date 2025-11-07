package main

import (
	"database/sql"
	"fmt"
	"strconv"
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

func parseFloat(s string) (float64, error) {
	return strconv.ParseFloat(s, 64)
}
