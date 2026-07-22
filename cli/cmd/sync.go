package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var (
	syncPort     int
	syncTimeout  string
	syncAutoOpen bool
)

var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Start the local HTTP server for the Chrome extension to review and apply changes",
	RunE: func(cmd *cobra.Command, args []string) error {
		// TODO(go-core-agent): implement local HTTP server per architecture.md §8, §9.5
		return fmt.Errorf("not implemented")
	},
}

func init() {
	syncCmd.Flags().IntVar(&syncPort, "port", 8765, "port to bind the local HTTP server to")
	syncCmd.Flags().StringVar(&syncTimeout, "timeout", "5m", "shut down automatically after this duration (0 = no timeout)")
	syncCmd.Flags().BoolVar(&syncAutoOpen, "auto-open", false, "best-effort open the extension's popup URL via the OS default browser handler")
	rootCmd.AddCommand(syncCmd)
}
