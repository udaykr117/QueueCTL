package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
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
			if parsed, err := parseFloat(configVal); err == nil {
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

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage configuration",
	Long:  `Manage system configuration such as retry count, backoff base, etc.`,
}

var configSetCmd = &cobra.Command{
	Use:   "set",
	Short: "Set a configuration value",
	Long:  `Set a configuration key-value pair. Common keys: max-retries, backoff-base`,
	Args:  cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		key := args[0]
		value := args[1]
		switch key {
		case "max-retries":
			if _, err := strconv.Atoi(value); err != nil {
				log.Fatalf("Invalid value for max-retries: %s (must be an integer)", value)
			}
		case "backoff-base":
			if _, err := parseFloat(value); err != nil {
				log.Fatalf("Invalid value for backoff-base: %s (must be a number)", value)
			}
		}

		if err := SetConfig(key, value); err != nil {
			log.Fatalf("Failed to set config: %v", err)
		}

		fmt.Printf("Configuration '%s' set to '%s'\n", key, value)
	},
}

var configGetCmd = &cobra.Command{
	Use:   "get",
	Short: "Get a configuration value",
	Long:  `Get the value of a configuration key.`,
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		key := args[0]

		value, err := GetConfig(key)
		if err != nil {
			log.Fatalf("Failed to get config: %v", err)
		}

		fmt.Println(value)
	},
}

var configListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all configuration",
	Long:  `Display all configuration key-value pairs.`,
	Run: func(cmd *cobra.Command, args []string) {
		config, err := GetAllConfig()
		if err != nil {
			log.Fatalf("Failed to get config: %v", err)
		}

		if len(config) == 0 {
			fmt.Println("No configuration set")
			return
		}

		fmt.Println("Configuration:")
		fmt.Println(strings.Repeat("=", 50))
		fmt.Printf("%-20s %s\n", "KEY", "VALUE")
		fmt.Println(strings.Repeat("-", 50))
		for key, value := range config {
			fmt.Printf("%-20s %s\n", key, value)
		}
	},
}

var ShowCmd = &cobra.Command{
	Use:   "show job-id",
	Short: "Show details and output of a job",
	Long:  `detailed information about a job including its output.`,
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		jobID := args[0]
		job, err := GetJobByID(jobID)
		if err != nil {
			log.Fatalf("failed to get job: %v", err)
		}

		var lastError sql.NullString
		err = DB().QueryRow("SELECT last_error FROM jobs WHERE id = ?", jobID).Scan(&lastError)
		if err != nil {
			log.Printf("Warning: failed to get last_error: %v", err)
		}

		fmt.Println("Job Details")
		fmt.Println(strings.Repeat("=", 80))
		fmt.Printf("%-20s %s\n", "ID:", job.ID)
		fmt.Printf("%-20s %s\n", "Command:", job.Command)
		fmt.Printf("%-20s %s\n", "State:", string(job.State))
		fmt.Printf("%-20s %d\n", "Attempts:", job.Attempts)
		fmt.Printf("%-20s %d\n", "Max Retries:", job.MaxRetries)
		if job.Timeout > 0 {
			fmt.Printf("%-20s %d seconds\n", "Timeout:", job.Timeout)
		} else {
			fmt.Printf("%-20s %s\n", "Timeout:", "default (5 minutes)")
		}
		fmt.Printf("%-20s %s\n", "Created At:", job.CreatedAt.Format(time.RFC3339))
		fmt.Printf("%-20s %s\n", "Updated At:", job.UpdatedAt.Format(time.RFC3339))
		if lastError.Valid && lastError.String != "" {
			fmt.Printf("%-20s %s\n", "Last Error:", lastError.String)
		}

		fmt.Println("\nOutput")
		fmt.Println(strings.Repeat("-", 80))
		if job.Output != "" {
			fmt.Println(job.Output)
		} else {
			fmt.Println("(No output available)")
		}
	},
}

var DashboardCmd = &cobra.Command{
	Use:   "dashboard",
	Short: "Start web dashboard server",
	Long:  `Start a minimal web dashboard server for monitoring queuectl metrics.`,
	Run: func(cmd *cobra.Command, args []string) {
		port, err := cmd.Flags().GetInt("port")
		if err != nil {
			log.Fatalf("failed to get port flag: %v", err)
		}
		if port < 1 || port > 65535 {
			log.Fatal("Invalid port")
		}
		cmd.PostRun = func(cmd *cobra.Command, args []string) {}
		server := NewServer(port)
		if err := server.Start(); err != nil {
			log.Fatalf("failed to start dashboard server: %v", err)
		}
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

	configCmd.AddCommand(configSetCmd)
	configCmd.AddCommand(configGetCmd)
	configCmd.AddCommand(configListCmd)
	rootCmd.AddCommand(configCmd)

	rootCmd.AddCommand(ShowCmd)

	DashboardCmd.Flags().IntP("port", "p", 8080, "Port to run the dashboard server on")
	rootCmd.AddCommand(DashboardCmd)
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
