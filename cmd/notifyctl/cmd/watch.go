package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var watchCmd = &cobra.Command{
	Use:   "watch",
	Short: "Watch notification status",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("watch command skeleton")
	},
}

func init() {
	rootCmd.AddCommand(watchCmd)
}
