package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
)

var dataDir string

var rootCmd = &cobra.Command{
	Use:   "queuectl",
	Short: "A CLI-based background job queue system",
	Long:  `queuectl`,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		var err error
		dataDir, err = GetDataDir()
		if err != nil {
			log.Fatalf("Failed to get data directory: %v", err)
		}
		if err := initDB(dataDir); err != nil {
			log.Fatalf("Failed to initialize DB: %v", err)
		}
	},
	PersistentPostRun: func(cmd *cobra.Command, args []string) {
		if cmd.Name() != "start" {
			CloseDB()
		}
	},
}

var enqueueCmd = &cobra.Command{
	Use:   "enqueue job-json",
	Short: "Add a new job to queue",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		job, err := ParseJobJSON(args[0])
		if err != nil {
			log.Fatalf("Failed to parse job JSON: %v", err)
		}
		if err := CreateJob(job); err != nil {
			log.Fatalf("Failed to enqueue job: %v", err)
		}
		fmt.Printf("Job enqueued successfully: %s\n", job.ID)
	},
}
var workerCmd = &cobra.Command{
	Use:   "worker",
	Short: "Manage worker processes",
}

var workerStartCmd = &cobra.Command{
	Use:   "start",
	Short: "start worker processes",
	Run: func(cmd *cobra.Command, args []string) {
		count, err := cmd.Flags().GetInt("count")
		if err != nil {
			log.Fatalf("failed to get count flag: %v", err)
		}
		if count < 1 {
			log.Fatalln("Worker count must be atleast 1")
		}

		backoffBase := 2.0
		if configVal, err := GetConfig("backoff-base"); err == nil {
			if parsed, err := parseFloat(configVal); err != nil {
				backoffBase = parsed
			}
		}

		pool := NewWorkerPool(count, backoffBase)
		if err := pool.StartWorkers(); err != nil {
			log.Fatalf("Failed to start workers: %v", err)
		}

		select {}
	},
	PersistentPostRun: func(cmd *cobra.Command, args []string) {},
}

var workerStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop worker processes",
	Long:  `Gracefully stop all running worker processes.`,
	Run: func(cmd *cobra.Command, args []string) {
		pool := GetWorkerPool()
		if pool != nil {
			if err := pool.StopWorkers(); err != nil {
				log.Fatalf("Failed to stop workers: %v", err)
			}
			fmt.Println("Workers stopped successfully")
			return
		}

		dataDir, err := GetDataDir()
		if err != nil {
			log.Fatalf("Failed to get data directory: %v", err)
		}
		pidFile := filepath.Join(dataDir, "worker.pid")

		pidBytes, err := os.ReadFile(pidFile)
		if os.IsNotExist(err) {
			fmt.Println("No workers are running")
			return
		}
		if err != nil {
			log.Fatalf("Failed to read PID file: %v", err)
		}

		var pid int
		if _, err := fmt.Sscanf(string(pidBytes), "%d", &pid); err != nil {
			log.Fatalf("Invalid PID file format: %v", err)
		}

		process, err := os.FindProcess(pid)
		if err != nil {
			fmt.Println("No workers are running (process not found)")
			os.Remove(pidFile)
			return
		}

		if err := process.Signal(os.Interrupt); err != nil {

			if os.IsNotExist(err) || err.Error() == "os: process already finished" {
				fmt.Println("No workers are running (process already exited)")
				os.Remove(pidFile)
				return
			}
			log.Fatalf("Failed to send signal to worker process: %v", err)
		}

		fmt.Printf("Sent stop signal to worker process (PID: %d). Waiting for graceful shutdown...\n", pid)

		time.Sleep(2 * time.Second)

		if err := process.Signal(syscall.Signal(0)); err != nil {
			fmt.Println("Workers stopped successfully")
			os.Remove(pidFile)
			return
		}

		fmt.Printf("Workers are shutting down (PID: %d). If they don't stop, you may need to send SIGTERM manually.\n", pid)
	},
}

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show summary of all job states & active workers",
	Long:  `Display a summary of job counts by state and the number of active workers.`,
	Run: func(cmd *cobra.Command, args []string) {
		counts, err := GetJobCountsByState()
		if err != nil {
			log.Fatalf("Failed to get job counts: %v", err)
		}
		activeWorkers := 0
		if IsWorkerRunning() {
			pool := GetWorkerPool()
			if pool != nil {
				activeWorkers = pool.workerCount
			} else {
				dataDir, err := GetDataDir()
				if err == nil {
					pidFile := filepath.Join(dataDir, "worker.pid")
					if pidBytes, err := os.ReadFile(pidFile); err == nil {
						var pid, workerCount int
						lines := strings.Split(strings.TrimSpace(string(pidBytes)), "\n")
						if len(lines) >= 1 {
							if _, err := fmt.Sscanf(lines[0], "%d", &pid); err == nil {
								if len(lines) >= 2 {
									if _, err := fmt.Sscanf(lines[1], "%d", &workerCount); err == nil {
										activeWorkers = workerCount
									} else {
										activeWorkers = 1 // Default assumption
									}
								} else {
									activeWorkers = 1 // Default assumption
								}

								process, err := os.FindProcess(pid)
								if err == nil {
									if err := process.Signal(syscall.Signal(0)); err != nil {
										activeWorkers = 0
									}
								} else {
									activeWorkers = 0
								}
							}
						}
					}
				}
			}
		}

		// Display summary
		fmt.Println("Job Queue Status")
		fmt.Println("===============")
		fmt.Printf("Pending:    %d\n", counts[StatePending])
		fmt.Printf("Processing: %d\n", counts[StateProcessing])
		fmt.Printf("Completed:  %d\n", counts[StateCompleted])
		fmt.Printf("Failed:     %d\n", counts[StateFailed])
		fmt.Printf("Dead:       %d\n", counts[StateDead])
		fmt.Println()
		fmt.Printf("Active Workers: %d\n", activeWorkers)
	},
}

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List jobs by state",
	Long:  `List all jobs, optionally filtered by state.`,
	Run: func(cmd *cobra.Command, args []string) {
		stateFlag, err := cmd.Flags().GetString("state")
		if err != nil {
			log.Fatalf("Failed to get state flag: %v", err)
		}

		var jobs []*Job
		if stateFlag != "" {
			jobState := JobState(stateFlag)
			validStates := []JobState{StatePending, StateProcessing, StateCompleted, StateFailed, StateDead}
			valid := false
			for _, vs := range validStates {
				if jobState == vs {
					valid = true
					break
				}
			}
			if !valid {
				log.Fatalf("Invalid state: %s. Valid states are: pending, processing, completed, failed, dead", stateFlag)
			}

			jobs, err = GetJobsByState(jobState)
			if err != nil {
				log.Fatalf("Failed to get jobs: %v", err)
			}
		} else {
			jobs, err = GetAllJobs()
			if err != nil {
				log.Fatalf("Failed to get jobs: %v", err)
			}
		}

		if len(jobs) == 0 {
			if stateFlag != "" {
				fmt.Printf("No jobs found with state: %s\n", stateFlag)
			} else {
				fmt.Println("No jobs found")
			}
			return
		}

		fmt.Printf("%-20s %-15s %-10s %-10s %-25s\n", "ID", "STATE", "ATTEMPTS", "MAX_RETRIES", "CREATED_AT")
		fmt.Println(strings.Repeat("-", 80))
		for _, job := range jobs {
			fmt.Printf("%-20s %-15s %-10d %-10d %-25s\n",
				job.ID,
				string(job.State),
				job.Attempts,
				job.MaxRetries,
				job.CreatedAt.Format(time.RFC3339),
			)
		}
	},
}

var dlqCmd = &cobra.Command{
	Use:   "dlq",
	Short: "Manage Dead Letter Queue",
	Long:  `View and manage jobs in the Dead Letter Queue (permanently failed jobs).`,
}

var dlqListCmd = &cobra.Command{
	Use:   "list",
	Short: "List jobs in Dead Letter Queue",
	Long:  `Display all jobs that have been moved to the Dead Letter Queue.`,
	Run: func(cmd *cobra.Command, args []string) {
		jobs, err := GetDLQJobs()
		if err != nil {
			log.Fatalf("Failed to get DLQ jobs: %v", err)
		}

		if len(jobs) == 0 {
			fmt.Println("No jobs in Dead Letter Queue")
			return
		}
		fmt.Printf("Dead Letter Queue Jobs (%d)\n", len(jobs))
		fmt.Println(strings.Repeat("=", 80))
		fmt.Printf("%-20s %-15s %-10s %-10s %-25s\n", "ID", "STATE", "ATTEMPTS", "MAX_RETRIES", "CREATED_AT")
		fmt.Println(strings.Repeat("-", 80))
		for _, job := range jobs {
			fmt.Printf("%-20s %-15s %-10d %-10d %-25s\n",
				job.ID,
				string(job.State),
				job.Attempts,
				job.MaxRetries,
				job.CreatedAt.Format(time.RFC3339),
			)
		}
	},
}

var dlqRetryCmd = &cobra.Command{
	Use:   "retry",
	Short: "Retry a job from Dead Letter Queue",
	Long:  `Reset a job from DLQ back to pending state so it can be retried.`,
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		jobID := args[0]

		if err := RetryDLQJob(jobID); err != nil {
			log.Fatalf("Failed to retry DLQ job: %v", err)
		}

		fmt.Printf("Job %s has been reset to pending state and will be retried\n", jobID)
	},
}

func init() {
	rootCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")

	rootCmd.AddCommand(enqueueCmd)

	rootCmd.AddCommand(statusCmd)

	listCmd.Flags().StringP("state", "s", "", "Filter jobs by state (pending, processing, completed, failed, dead)")
	rootCmd.AddCommand(listCmd)

	dlqCmd.AddCommand(dlqListCmd)
	dlqCmd.AddCommand(dlqRetryCmd)
	rootCmd.AddCommand(dlqCmd)

	workerStartCmd.Flags().IntP("count", "c", 1, "Number of workers to start")
	workerCmd.AddCommand(workerStartCmd)
	workerCmd.AddCommand(workerStopCmd)
	rootCmd.AddCommand(workerCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
