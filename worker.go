package main

import (
	"context"
	"fmt"
	"log"
	"math"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

type WorkerPool struct {
	ctx         context.Context
	cancel      context.CancelFunc
	wg          sync.WaitGroup
	workerCount int
	pidFile     string
	backoffBase float64
}

var (
	workerPoolMutex  sync.Mutex
	globalWorkerPool *WorkerPool
)

func NewWorkerPool(workerCount int, backeoffBase float64) *WorkerPool {
	ctx, cancel := context.WithCancel(context.Background())

	dataDir, _ := GetDataDir()
	pidFile := filepath.Join(dataDir, "worker.pid")
	return &WorkerPool{
		ctx:         ctx,
		cancel:      cancel,
		workerCount: workerCount,
		pidFile:     pidFile,
		backoffBase: backeoffBase,
	}
}

func (wp *WorkerPool) StartWorkers() error {
	workerPoolMutex.Lock()
	defer workerPoolMutex.Unlock()

	if globalWorkerPool != nil {
		return fmt.Errorf("workers are already running")
	}
	pid := os.Getpid()
	if err := os.WriteFile(wp.pidFile, []byte(fmt.Sprintf("%d\n", pid)), 0644); err != nil {
		return fmt.Errorf("failed to write PID file: %w", err)
	}

	globalWorkerPool = wp

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		log.Println("Received shutdown signal, stopping workers...")
		wp.StopWorkers()
		CloseDB()
		os.Exit(0)
	}()

	for i := 0; i < wp.workerCount; i++ {
		wp.wg.Add(1)
		workerID := fmt.Sprintf("worker-%d", i+1)
		go wp.workerLoop(workerID)
	}
	log.Printf("Started %d workers (PID: %d)", wp.workerCount, pid)
	return nil
}

func (wp *WorkerPool) StopWorkers() error {
	workerPoolMutex.Lock()
	defer workerPoolMutex.Unlock()

	if globalWorkerPool == nil {
		return fmt.Errorf("no workers are running")
	}
	log.Println("Stopping workers...")

	wp.cancel()
	wp.wg.Wait()

	if err := os.Remove(wp.pidFile); err != nil && !os.IsNotExist(err) {
		log.Printf("Warning: failed to remove PID file: %v", err)
	}

	globalWorkerPool = nil
	log.Println("All workers stopped")
	return nil

}

func (wp *WorkerPool) workerLoop(workerID string) {
	defer wp.wg.Done()
	log.Printf("[%s] Started", workerID)

	for {
		select {
		case <-wp.ctx.Done():
			log.Printf("[%s] Shutting down...", workerID)
			return
		default:
		}

		job, err := GetNextPendingJob(workerID)
		if err != nil {
			log.Printf("[%s] Error getting job: %v", workerID, err)
			time.Sleep(1 * time.Second)
			continue
		}
		if job == nil {
			time.Sleep(500 * time.Millisecond)
			continue
		}
		log.Printf("[%s] Processing job: %s (command: %s)", workerID, job.ID, job.Command)
		wp.processJob(workerID, job)
	}

}

func (wp *WorkerPool) processJob(workerID string, job *Job) {
	if err := IncrementJobAttempts(job.ID); err != nil {
		log.Printf("[%s] Error incrementing attempts for job %s: %v", workerID, job.ID, err)
	}
	output, err := executeJob(job)
	if err := SaveJobOutput(job.ID, output); err != nil {
		log.Printf("[%s] Error saving job output: %v", workerID, err)
	}

	if err == nil {
		log.Printf("[%s] Job %s completed successfully", workerID, job.ID)
		if err := UpdateJobState(job.ID, StateCompleted, ""); err != nil {
			log.Printf("[%s] Error updating job state: %v", workerID, err)
		}
		return
	}
	errorMsg := err.Error()
	log.Printf("[%s] Job %s failed: %s", workerID, job.ID, errorMsg)

	var currentAttempts int
	err = db.QueryRow("SELECT attempts FROM jobs WHERE id = ?", job.ID).Scan(&currentAttempts)
	if err != nil {
		log.Printf("[%s] Error getting attempt count: %v", workerID, err)
		currentAttempts = job.Attempts + 1
	}

	if currentAttempts >= job.MaxRetries {
		log.Printf("[%s] Job %s exceeded max retries (%d), moving to DLQ", workerID, job.ID, job.MaxRetries)
		if err := UpdateJobState(job.ID, StateDead, errorMsg); err != nil {
			log.Printf("[%s] Error moving job to DLQ: %v", workerID, err)
		}
	} else {
		delay := CalculateBackoffDelay(currentAttempts, wp.backoffBase)
		nextRetry := time.Now().UTC().Add(delay)
		log.Printf("[%s] Job %s will retry in %v (attempt %d/%d)", workerID, job.ID, delay, currentAttempts, job.MaxRetries)
		if err := SetNextRetryAt(job.ID, nextRetry); err != nil {
			log.Printf("[%s] Error setting next retry: %v", workerID, err)
		}
		if err := UpdateJobState(job.ID, StatePending, errorMsg); err != nil {
			log.Printf("[%s] Error updating job state for retry: %v", workerID, err)
		}
	}
}

func executeJob(job *Job) (string, error) {
	// Set timeout (default 5 minutes if not specified)
	timeout := 5 * time.Minute
	if job.Timeout > 0 {
		timeout = time.Duration(job.Timeout) * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", job.Command)
	output, err := cmd.CombinedOutput()
	outputStr := string(output)

	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return outputStr, fmt.Errorf("job timeout after %v: %s", timeout, outputStr)
		}
		exitErr, ok := err.(*exec.ExitError)
		if ok {
			return outputStr, fmt.Errorf("command exited with code %d: %s", exitErr.ExitCode(), string(output))
		}
		return outputStr, fmt.Errorf("command execution failed: %w: %s", err, string(output))
	}
	return outputStr, nil
}

func CalculateBackoffDelay(attempts int, baseDelay float64) time.Duration {
	if attempts <= 0 {
		attempts = 1
	}
	delaySeconds := math.Pow(baseDelay, float64(attempts))
	return time.Duration(delaySeconds) * time.Second
}

func IsWorkerRunning() bool {
	workerPoolMutex.Lock()
	defer workerPoolMutex.Unlock()
	return globalWorkerPool != nil
}

func GetWorkerPool() *WorkerPool {
	workerPoolMutex.Lock()
	defer workerPoolMutex.Unlock()
	return globalWorkerPool
}
