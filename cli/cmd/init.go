package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var (
	initDir   string
	initForce bool
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Scaffold a starter marko.yaml (and templates/ dir) in the target directory",
	RunE: func(cmd *cobra.Command, args []string) error {
		// TODO(go-core-agent): implement scaffolding per architecture.md §9.1
		return fmt.Errorf("not implemented")
	},
}

func init() {
	initCmd.Flags().StringVar(&initDir, "dir", ".", "target directory")
	initCmd.Flags().BoolVar(&initForce, "force", false, "overwrite existing marko.yaml")
	rootCmd.AddCommand(initCmd)
}
