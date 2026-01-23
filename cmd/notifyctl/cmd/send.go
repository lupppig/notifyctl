package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	notifyv1 "github.com/lupppig/notifyctl/pkg/grpc/notify/v1"
)

var (
	sendServiceID   string
	sendPayloadPath string
	sendRawPayload  string
)

type sendPayload struct {
	Topic        string                  `json:"topic"`
	Payload      json.RawMessage         `json:"payload"`
	Destinations []*notifyv1.Destination `json:"destinations"`
}

var sendCmd = &cobra.Command{
	Use:   "send",
	Short: "Send a notification",
	RunE: func(cmd *cobra.Command, args []string) error {
		if sendPayloadPath == "" && sendRawPayload == "" {
			return fmt.Errorf("must provide either --payload or --raw")
		}
		if sendPayloadPath != "" && sendRawPayload != "" {
			return fmt.Errorf("cannot provide both --payload and --raw")
		}

		var data []byte
		var err error
		if sendPayloadPath != "" {
			data, err = os.ReadFile(sendPayloadPath)
			if err != nil {
				return fmt.Errorf("read payload file: %w", err)
			}
		} else {
			data = []byte(sendRawPayload)
		}

		var p sendPayload
		if err := json.Unmarshal(data, &p); err != nil {
			return fmt.Errorf("parse JSON payload: %w", err)
		}

		client := GetNotifyServiceClient()

		ctx, cancel := NewCommandContext(context.Background())
		defer cancel()

		req := &notifyv1.SendNotificationRequest{
			ServiceId:    sendServiceID,
			Topic:        p.Topic,
			Payload:      p.Payload,
			Destinations: p.Destinations,
		}

		resp, err := client.SendNotification(ctx, req)
		if err != nil {
			return fmt.Errorf("send notification: %w", err)
		}

		out := bufio.NewWriter(os.Stdout)
		defer out.Flush()

		if IsQuiet() {
			fmt.Fprintln(out, resp.NotificationId)
		} else {
			fmt.Fprintf(out, "Notification sent successfully! Request ID: %s\n", resp.NotificationId)
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(sendCmd)

	sendCmd.Flags().StringVar(&sendServiceID, "service-id", "", "Service ID (required)")
	sendCmd.Flags().StringVar(&sendPayloadPath, "payload", "", "Path to JSON payload file")
	sendCmd.Flags().StringVar(&sendRawPayload, "raw", "", "Raw JSON payload string")

	_ = sendCmd.MarkFlagRequired("service-id")
}
