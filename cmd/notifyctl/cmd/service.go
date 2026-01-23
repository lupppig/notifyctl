package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var serviceCmd = &cobra.Command{
	Use:   "service",
	Short: "Manage services",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("service command skeleton")
	},
}

var createServiceCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new service",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("service create command skeleton")
	},
}

func init() {
	rootCmd.AddCommand(serviceCmd)
	serviceCmd.AddCommand(createServiceCmd)
}
