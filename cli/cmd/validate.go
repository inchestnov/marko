package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var validateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Run structural and semantic validation on marko.yaml and templates/",
	RunE: func(cmd *cobra.Command, args []string) error {
		// TODO(go-core-agent): implement Phase A + Phase B validation per architecture.md §5, §9.2
		return fmt.Errorf("not implemented")
	},
}

func init() {
	rootCmd.AddCommand(validateCmd)
}
