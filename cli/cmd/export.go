package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var exportOut string

var exportCmd = &cobra.Command{
	Use:   "export",
	Short: "Render the desired state and write it to a static JSON file",
	RunE: func(cmd *cobra.Command, args []string) error {
		// TODO(go-core-agent): implement parse -> resolve -> validate -> render -> write per architecture.md §8.4, §9.6
		return fmt.Errorf("not implemented")
	},
}

func init() {
	exportCmd.Flags().StringVar(&exportOut, "out", "marko-export.json", "output file path")
	rootCmd.AddCommand(exportCmd)
}
