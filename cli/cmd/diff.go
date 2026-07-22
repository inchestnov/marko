package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var diffActual string

var diffCmd = &cobra.Command{
	Use:   "diff",
	Short: "Compare the desired state against a captured browser state and print an operation plan",
	RunE: func(cmd *cobra.Command, args []string) error {
		// TODO(go-core-agent): implement diff engine invocation per architecture.md §7, §9.4
		return fmt.Errorf("not implemented")
	},
}

func init() {
	diffCmd.Flags().StringVar(&diffActual, "actual", "", "path to a previously-exported actualTree JSON file")
	rootCmd.AddCommand(diffCmd)
}
