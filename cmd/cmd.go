package cmd

import (
	"os"

	cmdconfig "github.com/zet-plane/live-auction-backend/cmd/config"
	"github.com/zet-plane/live-auction-backend/cmd/server"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:          "live-auction",
	Short:        "Live auction backend",
	SilenceUsage: true,
}

func init() {
	rootCmd.AddCommand(server.StartCmd)
	rootCmd.AddCommand(cmdconfig.StartCmd)
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
