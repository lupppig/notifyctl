package cmd

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"

	"github.com/lupppig/notifyctl/internal/config"
	internalgrpc "github.com/lupppig/notifyctl/internal/grpc"
	notifyv1 "github.com/lupppig/notifyctl/pkg/grpc/notify/v1"
)

var (
	timeout    time.Duration
	jsonOut    bool
	quiet      bool
	authToken  string
	configPath string
	cfg        *config.Config
	grpcConn   *grpc.ClientConn

	// clientFactory is used for unit testing
	clientFactory func(grpc.ClientConnInterface) notifyv1.NotifyServiceClient
)

func init() {
	clientFactory = notifyv1.NewNotifyServiceClient
}

const defaultServerAddr = "localhost:50051"

var rootCmd = &cobra.Command{
	Use:   "notifyctl",
	Short: "CLI for notifyctl notification platform",
	Long: `notifyctl is a CLI-driven notification and webhook delivery platform.

Send events, manage destinations, and monitor delivery status in real-time.`,
	SilenceUsage:  true,
	SilenceErrors: true,
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

		// Commands that don't require pre-configured auth/context
		if cmd.Name() == "version" || cmd.Name() == "create" {
			return initGRPCClient()
		}

		// Service management commands shouldn't be blocked by a missing ServiceID
		// as they either create them, list them, or take an ID as a flag.
		if cmd.Parent() != nil && cmd.Parent().Name() == "service" {
			if cfg.APIKey == "" {
				return fmt.Errorf("missing API key (set NOTIFYCTL_API_KEY or use config file)")
			}
			return initGRPCClient()
		}

		if cfg.APIKey == "" {
			return fmt.Errorf("missing API key (set NOTIFYCTL_API_KEY or use config file)")
		}
		if cfg.ServiceID == "" {
			return fmt.Errorf("missing Service ID (set NOTIFYCTL_SERVICE_ID or use config file)")
		}

		return initGRPCClient()
	},
	PersistentPostRun: func(cmd *cobra.Command, args []string) {
		closeGRPCClient()
	},
}

func Execute() error {
	err := rootCmd.Execute()
	if err != nil {
		exitOnError(err)
	}
	return err
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

func GetNotifyServiceClient() notifyv1.NotifyServiceClient {
	return clientFactory(grpcConn)
}

func NewCommandContext(parent context.Context) (context.Context, context.CancelFunc) {
	var ctx context.Context
	var cancel context.CancelFunc

	if timeout > 0 {
		ctx, cancel = context.WithTimeout(parent, timeout)
	} else {
		ctx, cancel = context.WithCancel(parent)
	}

	if authToken != "" {
		md := metadata.Pairs("x-auth-token", authToken)
		ctx = metadata.NewOutgoingContext(ctx, md)
	}

	return ctx, cancel
}

func exitOnError(err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
