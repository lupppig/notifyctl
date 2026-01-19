package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

var (
	timeout   time.Duration
	jsonOut   bool
	quiet     bool
	authToken string
	grpcConn  *grpc.ClientConn
)

const defaultServerAddr = "localhost:50051"

var rootCmd = &cobra.Command{
	Use:   "notifyctl",
	Short: "CLI for notifyctl notification platform",
	Long: `notifyctl is a CLI-driven notification and webhook delivery platform.

Send events, manage destinations, and monitor delivery status in real-time.`,
	SilenceUsage: true,
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Help()
	},
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		if cmd.Name() == "help" || cmd.Name() == "completion" || cmd.Name() == "notifyctl" {
			return nil
		}
		return initGRPCClient()
	},
	PersistentPostRun: func(cmd *cobra.Command, args []string) {
		closeGRPCClient()
	},
}

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.PersistentFlags().DurationVar(&timeout, "timeout", 30*time.Second, "Request timeout duration")
	rootCmd.PersistentFlags().BoolVar(&jsonOut, "json", false, "Output in JSON format")
	rootCmd.PersistentFlags().BoolVarP(&quiet, "quiet", "q", false, "Suppress non-essential output")
	rootCmd.PersistentFlags().StringVar(&authToken, "auth-token", "", "Authentication token (optional)")
}

func initGRPCClient() error {
	var err error
	grpcConn, err = grpc.NewClient(defaultServerAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return fmt.Errorf("failed to create client: %w", err)
	}
	return nil
}

func closeGRPCClient() {
	if grpcConn != nil {
		grpcConn.Close()
	}
}

func GetGRPCConn() *grpc.ClientConn {
	return grpcConn
}

func IsJSONOutput() bool {
	return jsonOut
}

func IsQuiet() bool {
	return quiet
}

func GetTimeout() time.Duration {
	return time.Duration(timeout)
}

func exitOnError(err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
