package config

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/zet-plane/live-auction-backend/config"
)

var (
	outputPath string
	force      bool

	StartCmd = &cobra.Command{
		Use:     "config",
		Short:   "Generate a default config file",
		Example: "live-auction config -p config.yaml",
		Run: func(cmd *cobra.Command, args []string) {
			if err := config.GenConfig(outputPath, force); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
		},
	}
)

func init() {
	StartCmd.PersistentFlags().StringVarP(&outputPath, "path", "p", "config.yaml", "output path for the generated config file")
	StartCmd.PersistentFlags().BoolVarP(&force, "force", "f", false, "overwrite existing config file")
}
