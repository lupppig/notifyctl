package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	notifyv1 "github.com/lupppig/notifyctl/pkg/grpc/notify/v1"
)

var (
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

		if IsQuiet() || IsJSONOutput() {
			client := GetNotifyServiceClient()
			ctx, cancel := NewCommandContext(context.Background())
			defer cancel()

			resp, err := client.SendNotification(ctx, &notifyv1.SendNotificationRequest{
				ServiceId:    cfg.ServiceID,
				Topic:        p.Topic,
				Payload:      p.Payload,
				Destinations: p.Destinations,
			})
			if err != nil {
				return err
			}

			if IsQuiet() {
				fmt.Println(resp.NotificationId)
			} else {
				data, _ := json.MarshalIndent(resp, "", "  ")
				fmt.Println(string(data))
			}
			return nil
		}

		m := NewSendModel(cfg.ServiceID, p.Topic, p.Payload, p.Destinations)
		return NewUI(m).Run()
	},
}

func init() {
	rootCmd.AddCommand(sendCmd)

	sendCmd.Flags().StringVar(&sendPayloadPath, "payload", "", "Path to JSON payload file")
	sendCmd.Flags().StringVar(&sendRawPayload, "raw", "", "Raw JSON payload string")
}
