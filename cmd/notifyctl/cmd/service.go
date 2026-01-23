package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	notifyv1 "github.com/lupppig/notifyctl/pkg/grpc/notify/v1"
)

var (
	svcName       string
	svcWebhookURL string
	svcSecret     string
	svcIDToDelete string
)

var serviceCmd = &cobra.Command{
	Use:   "service",
	Short: "Manage services",
}

var createServiceCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new service",
	RunE: func(cmd *cobra.Command, args []string) error {
		client := GetNotifyServiceClient()

		ctx, cancel := NewCommandContext(context.Background())
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

		out := bufio.NewWriter(os.Stdout)
		defer out.Flush()

		if IsQuiet() {
			fmt.Fprintln(out, resp.ServiceId)
			return nil
		}

		if IsJSONOutput() {
			data, _ := json.MarshalIndent(resp, "", "  ")
			fmt.Fprintln(out, string(data))
			return nil
		}

		fmt.Fprintln(out, "Service created successfully!")
		fmt.Fprintf(out, "ID:      %s\n", resp.ServiceId)
		fmt.Fprintf(out, "API Key: %s\n", resp.ApiKey)
		return nil
	},
}

var deleteServiceCmd = &cobra.Command{
	Use:   "delete",
	Short: "Delete a service",
	RunE: func(cmd *cobra.Command, args []string) error {
		if !IsQuiet() {
			fmt.Printf("Are you sure you want to delete service %s? [y/N]: ", svcIDToDelete)
			var response string
			fmt.Scanln(&response)
			if strings.ToLower(response) != "y" {
				fmt.Println("Deletion cancelled.")
				return nil
			}
		}

		client := GetNotifyServiceClient()

		ctx, cancel := NewCommandContext(context.Background())
		defer cancel()

		_, err := client.DeleteService(ctx, &notifyv1.DeleteServiceRequest{
			Id: svcIDToDelete,
		})
		if err != nil {
			return fmt.Errorf("delete service: %w", err)
		}

		out := bufio.NewWriter(os.Stdout)
		defer out.Flush()

		if !IsQuiet() {
			fmt.Fprintln(out, "Service deleted successfully.")
		}

		return nil
	},
}

var listServicesCmd = &cobra.Command{
	Use:   "list",
	Short: "List all services",
	RunE: func(cmd *cobra.Command, args []string) error {
		client := GetNotifyServiceClient()

		ctx, cancel := NewCommandContext(context.Background())
		defer cancel()

		resp, err := client.ListServices(ctx, &notifyv1.ListServicesRequest{})
		if err != nil {
			return fmt.Errorf("list services: %w", err)
		}

		out := bufio.NewWriter(os.Stdout)
		defer out.Flush()

		if IsJSONOutput() {
			data, _ := json.MarshalIndent(resp.Services, "", "  ")
			fmt.Fprintln(out, string(data))
			return nil
		}

		if len(resp.Services) == 0 {
			if !IsQuiet() {
				fmt.Fprintln(out, "No services found.")
			}
			return nil
		}

		// Table rendering
		fmt.Fprintf(out, "%-36s  %-20s  %s\n", "ID", "NAME", "WEBHOOK URL")
		fmt.Fprintf(out, "%-36s  %-20s  %s\n", "---", "----", "-----------")
		for _, svc := range resp.Services {
			fmt.Fprintf(out, "%-36s  %-20s  %s\n", svc.Id, svc.Name, svc.WebhookUrl)
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(serviceCmd)
	serviceCmd.AddCommand(createServiceCmd)
	serviceCmd.AddCommand(listServicesCmd)
	serviceCmd.AddCommand(deleteServiceCmd)

	createServiceCmd.Flags().StringVar(&svcName, "name", "", "Service name (required)")
	createServiceCmd.Flags().StringVar(&svcWebhookURL, "webhook-url", "", "Webhook URL (required)")
	createServiceCmd.Flags().StringVar(&svcSecret, "secret", "", "Webhook secret")

	_ = createServiceCmd.MarkFlagRequired("name")
	_ = createServiceCmd.MarkFlagRequired("webhook-url")

	deleteServiceCmd.Flags().StringVar(&svcIDToDelete, "id", "", "Service ID (required)")
	_ = deleteServiceCmd.MarkFlagRequired("id")
}
