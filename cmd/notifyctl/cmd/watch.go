package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	notifyv1 "github.com/lupppig/notifyctl/pkg/grpc/notify/v1"
)

var (
	watchRequestID string
	watchServiceID string
	watchUseUI     bool
)

var watchCmd = &cobra.Command{
	Use:   "watch",
	Short: "Watch notification status",
	Long:  "Stream real-time delivery status updates for a specific notification or service.",
	RunE: func(cmd *cobra.Command, args []string) error {
		if watchRequestID == "" && watchServiceID == "" {
			return fmt.Errorf("must provide either --request-id or --service-id")
		}

		// Handle Ctrl+C and global flags
		baseCtx, baseCancel := NewCommandContext(context.Background())
		defer baseCancel()
		ctx, stop := signal.NotifyContext(baseCtx, os.Interrupt, syscall.SIGTERM)
		defer stop()

		client := GetNotifyServiceClient()

		stream, err := client.StreamDeliveryStatus(ctx, &notifyv1.StreamDeliveryStatusRequest{
			NotificationId: watchRequestID,
			ServiceId:      watchServiceID,
		})
		if err != nil {
			return fmt.Errorf("open stream: %w", err)
		}

		if watchUseUI {
			return runWatchUI(ctx, stream)
		}

		out := bufio.NewWriter(os.Stdout)
		defer out.Flush()

		if !IsQuiet() && !IsJSONOutput() {
			fmt.Fprintf(out, "Watching status for Request ID: %s...\n", watchRequestID)
			fmt.Fprintf(out, "%-20s  %-15s  %s\n", "DESTINATION", "STATUS", "MESSAGE")
			fmt.Fprintf(out, "%-20s  %-15s  %s\n", "-----------", "------", "-------")
			out.Flush()
		}

		for {
			event, err := stream.Recv()
			if err == io.EOF {
				if !IsQuiet() && !IsJSONOutput() {
					fmt.Fprintln(out, "\nStream closed by server.")
				}
				break
			}
			if err != nil {
				// Check if it's context cancellation
				if ctx.Err() != nil {
					return nil
				}
				return fmt.Errorf("receive event: %w", err)
			}

			if IsJSONOutput() {
				data, _ := json.Marshal(event)
				fmt.Fprintln(out, string(data))
			} else {
				fmt.Fprintf(out, "%-20s  %-15s  %s\n",
					event.Destination,
					event.Status.String()[16:], // Strip 'DELIVERY_STATUS_'
					event.Message,
				)
			}
			out.Flush()
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(watchCmd)

	watchCmd.Flags().StringVar(&watchRequestID, "request-id", "", "Notification Request ID to watch")
	watchCmd.Flags().StringVar(&watchServiceID, "service-id", "", "Service ID to watch (streams all notifications for this service)")
	watchCmd.Flags().BoolVar(&watchUseUI, "ui", false, "Use TUI for watching status")
}
