package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var renderOut string

var renderCmd = &cobra.Command{
	Use:   "render",
	Short: "Run the full pipeline and print the resulting BookmarkTree",
	RunE: func(cmd *cobra.Command, args []string) error {
		// TODO(go-core-agent): implement parse -> resolve -> validate -> render per architecture.md §9.3
		return fmt.Errorf("not implemented")
	},
}

func init() {
	renderCmd.Flags().StringVar(&renderOut, "out", "", "write output to a file instead of stdout")
	rootCmd.AddCommand(renderCmd)
}
