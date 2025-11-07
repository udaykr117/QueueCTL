package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
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
		CloseDB()
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

		backoffBase := 1.0
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

func init() {
	rootCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")

	rootCmd.AddCommand(enqueueCmd)

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
