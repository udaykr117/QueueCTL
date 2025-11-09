package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

var (
	ErrInvalidJSON    = errors.New("invalid JSON")
	ErrMissingID      = errors.New("missing job ID")
	ErrMissingCommand = errors.New("missing job command")
)

func GetDataDir() (string, error) {
	if envDir := os.Getenv("QUEUECTL_DATA_DIR"); envDir != "" {
		return envDir, nil
	}
	execPath, err := os.Executable()
	if err != nil {
		wd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("failed to get working directory: %w", err)
		}
		return filepath.Join(wd, "data"), nil
	}
	execDir := filepath.Dir(execPath)
	return filepath.Join(execDir, "data"), nil
}

func ParseJobJSON(jsonStr string) (*Job, error) {
	var job Job
	if err := json.Unmarshal([]byte(jsonStr), &job); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidJSON, err)
	}

	if job.ID == "" {
		return nil, ErrMissingID
	}
	if job.Command == "" {
		return nil, ErrMissingCommand
	}

	if job.State == "" {
		job.State = StatePending
	}
	if job.MaxRetries <= 0 {
		job.MaxRetries = GetConfigInt("max-retries", 3)
	}
	return &job, nil
}
