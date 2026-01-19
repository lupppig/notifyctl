package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "notifyctl",
	Short: "CLI for notifyctl notification platform",
	Long: `notifyctl is a CLI-driven notification and webhook delivery platform.

Send events, manage destinations, and monitor delivery status in real-time.`,
}

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.PersistentFlags().StringP("server", "s", "localhost:50051", "gRPC server address")
}

func exitOnError(err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
