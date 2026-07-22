// Package cmd contains the Cobra command definitions for the marko CLI.
// Per docs/architecture.md §2, cmd/* is the only package allowed to wire
// everything together and talk to the filesystem/stdout directly.
package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// Global flags shared by all commands (architecture.md §9).
var (
	configPath   string
	templatesDir string
	verbose      bool
	jsonOutput   bool
)

var rootCmd = &cobra.Command{
	Use:   "marko",
	Short: "Bookmark infrastructure as code",
}

func init() {
	rootCmd.PersistentFlags().StringVar(&configPath, "config", "", "path to marko.yaml (default: search upward from cwd)")
	rootCmd.PersistentFlags().StringVar(&templatesDir, "templates-dir", "", "path to templates directory (default: <dir of marko.yaml>/templates)")
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "verbose output")
	rootCmd.PersistentFlags().BoolVar(&jsonOutput, "json", false, "machine readable JSON output where applicable")
}

// Execute runs the root command. Called from main().
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
