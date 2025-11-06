package main

import "time"

type JobState string

const (
	StatePending    JobState = "pending"
	StateProcessing JobState = "processing"
	StateCompleted  JobState = "Completed"
	StateFailed     JobState = "Failed"
	StateDead       JobState = "Dead"
)

type Job struct {
	ID         string    `json:"id"`
	Command    string    `json:"command"`
	Attempts   int       `json:"attempts"`
	State      JobState  `json:"state"`
	MaxRetries int       `json:"max_retries"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}
