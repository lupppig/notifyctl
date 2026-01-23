package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var sendCmd = &cobra.Command{
	Use:   "send",
	Short: "Send a notification",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("send command skeleton")
	},
}

func init() {
	rootCmd.AddCommand(sendCmd)
}
