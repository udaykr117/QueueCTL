package main

import (
	"fmt"
	"log"
	"os"

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

func init() {
	rootCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
	rootCmd.AddCommand(enqueueCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
