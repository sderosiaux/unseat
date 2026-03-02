package cli

import (
	"github.com/spf13/cobra"
)

var (
	jsonOutput bool
	configFile string
)

var rootCmd = &cobra.Command{
	Use:   "unseat",
	Short: "Identity Lifecycle Management across SaaS providers",
	Long:  "Cross-reference Google Workspace with SaaS providers to automate provisioning, deprovisioning, and seat optimization.",
}

func init() {
	rootCmd.PersistentFlags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	rootCmd.PersistentFlags().StringVar(&configFile, "config", "unseat.yaml", "Config file path")
}

func Execute() error {
	return rootCmd.Execute()
}
