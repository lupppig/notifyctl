package cmd

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	notifyv1 "github.com/lupppig/notifyctl/pkg/grpc/notify/v1"
)

var (
	svcName       string
	svcWebhookURL string
	svcSecret     string
)

var serviceCmd = &cobra.Command{
	Use:   "service",
	Short: "Manage services",
}

var createServiceCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new service",
	RunE: func(cmd *cobra.Command, args []string) error {
		conn := GetGRPCConn()
		client := notifyv1.NewNotifyServiceClient(conn)

		ctx, cancel := context.WithTimeout(context.Background(), GetTimeout())
		defer cancel()

		req := &notifyv1.RegisterServiceRequest{
			Name:       svcName,
			WebhookUrl: svcWebhookURL,
			Secret:     svcSecret,
		}

		resp, err := client.RegisterService(ctx, req)
		if err != nil {
			return fmt.Errorf("register service: %w", err)
		}

		if IsQuiet() {
			fmt.Println(resp.ServiceId)
			return nil
		}

		if IsJSONOutput() {
			data, _ := json.MarshalIndent(resp, "", "  ")
			fmt.Println(string(data))
			return nil
		}

		fmt.Printf("Service created successfully!\n")
		fmt.Printf("ID:      %s\n", resp.ServiceId)
		fmt.Printf("API Key: %s\n", resp.ApiKey)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(serviceCmd)
	serviceCmd.AddCommand(createServiceCmd)

	createServiceCmd.Flags().StringVar(&svcName, "name", "", "Service name (required)")
	createServiceCmd.Flags().StringVar(&svcWebhookURL, "webhook-url", "", "Webhook URL (required)")
	createServiceCmd.Flags().StringVar(&svcSecret, "secret", "", "Webhook secret")

	_ = createServiceCmd.MarkFlagRequired("name")
	_ = createServiceCmd.MarkFlagRequired("webhook-url")
}
