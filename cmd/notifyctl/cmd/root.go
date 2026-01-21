package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/lupppig/notifyctl/internal/config"
	internalgrpc "github.com/lupppig/notifyctl/internal/grpc"
)

var (
	timeout   time.Duration
	jsonOut   bool
	quiet      bool
	authToken  string
	configPath string
	cfg        *config.Config
	grpcConn   *grpc.ClientConn
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

		var err error
		cfg, err = config.Load(configPath)
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}

		if cmd.Name() != "version" && cfg.APIKey == "" {
			return fmt.Errorf("missing API key (set NOTIFYCTL_API_KEY or use config file)")
		}
		if cmd.Name() != "version" && cfg.ServiceID == "" {
			return fmt.Errorf("missing Service ID (set NOTIFYCTL_SERVICE_ID or use config file)")
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
	rootCmd.PersistentFlags().StringVar(&configPath, "config", "", "Path to config file (default is $HOME/.notifyctl.yaml)")
}

func initGRPCClient() error {
	var err error
	addr := cfg.ServerAddr
	if addr == "" {
		addr = defaultServerAddr
	}

	grpcConn, err = grpc.NewClient(addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithUnaryInterceptor(internalgrpc.UnaryAuthInterceptor(cfg.APIKey, cfg.ServiceID)),
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
