package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"

	notifyv1 "github.com/lupppig/notifyctl/pkg/grpc/notify/v1"
)

var dlqServiceID string

var dlqCmd = &cobra.Command{
	Use:   "dlq",
	Short: "Inspect the dead-letter queue",
}

var dlqListCmd = &cobra.Command{
	Use:   "list",
	Short: "List persisted dead letters (deliveries failed after max retries)",
	RunE: func(cmd *cobra.Command, args []string) error {
		client := GetNotifyServiceClient()

		ctx, cancel := NewCommandContext(context.Background())
		defer cancel()

		serviceID := dlqServiceID
		if serviceID == "" {
			serviceID = cfg.ServiceID
		}

		resp, err := client.ListDeadLetters(ctx, &notifyv1.ListDeadLettersRequest{
			ServiceId: serviceID,
		})
		if err != nil {
			return err
		}

		if IsQuiet() {
			for _, dl := range resp.DeadLetters {
				fmt.Println(dl.Id)
			}
			return nil
		}

		if IsJSONOutput() {
			data, _ := json.MarshalIndent(resp.DeadLetters, "", "  ")
			fmt.Println(string(data))
			return nil
		}

		if len(resp.DeadLetters) == 0 {
			fmt.Println("No dead letters found.")
			return nil
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tNOTIFICATION ID\tSERVICE ID\tATTEMPTS\tLAST ERROR\tFAILED AT")
		for _, dl := range resp.DeadLetters {
			fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%s\t%s\n",
				dl.Id,
				dl.NotificationId,
				dl.ServiceId,
				dl.AttemptCount,
				dl.LastError,
				dl.FailedAt,
			)
		}
		w.Flush()

		return nil
	},
}

var dlqReplayCmd = &cobra.Command{
	Use:   "replay <id>",
	Short: "Re-enqueue a dead letter back into the dispatcher",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client := GetNotifyServiceClient()

		ctx, cancel := NewCommandContext(context.Background())
		defer cancel()

		resp, err := client.ReplayDeadLetter(ctx, &notifyv1.ReplayDeadLetterRequest{
			Id: args[0],
		})
		if err != nil {
			return err
		}

		if IsQuiet() {
			fmt.Println(resp.NotificationId)
			return nil
		}

		if IsJSONOutput() {
			data, _ := json.MarshalIndent(resp, "", "  ")
			fmt.Println(string(data))
			return nil
		}

		fmt.Printf("Dead letter %s replayed; re-enqueued notification %s\n", args[0], resp.NotificationId)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(dlqCmd)
	dlqCmd.AddCommand(dlqListCmd)
	dlqCmd.AddCommand(dlqReplayCmd)
	dlqListCmd.Flags().StringVar(&dlqServiceID, "service-id", "", "Filter by service ID")
}
