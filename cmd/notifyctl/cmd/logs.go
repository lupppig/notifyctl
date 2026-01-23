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

var (
	logsServiceID string
)

var logsCmd = &cobra.Command{
	Use:   "logs",
	Short: "View notification logs",
	RunE: func(cmd *cobra.Command, args []string) error {
		client := GetNotifyServiceClient()

		ctx, cancel := NewCommandContext(context.Background())
		defer cancel()

		req := &notifyv1.ListNotificationJobsRequest{
			ServiceId: logsServiceID,
		}

		resp, err := client.ListNotificationJobs(ctx, req)
		if err != nil {
			return err
		}

		if IsJSONOutput() {
			data, _ := json.MarshalIndent(resp.Jobs, "", "  ")
			fmt.Println(string(data))
			return nil
		}

		if len(resp.Jobs) == 0 {
			fmt.Println("No logs found.")
			return nil
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "REQUEST ID\tSERVICE ID\tEVENT\tSTATUS\tCREATED AT")
		for _, job := range resp.Jobs {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
				job.RequestId,
				job.ServiceId,
				job.Event,
				job.Status,
				job.CreatedAt,
			)
		}
		w.Flush()

		return nil
	},
}

func init() {
	rootCmd.AddCommand(logsCmd)
	logsCmd.Flags().StringVar(&logsServiceID, "service-id", "", "Filter by service ID")
}
