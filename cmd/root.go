// Package cmd contains the Cobra command definitions for the marko CLI.
// Per docs/architecture.md §2, cmd/* is the only package allowed to wire
// everything together and talk to the filesystem/stdout directly.
package cmd

import (
	"fmt"
	"os"

	"github.com/inchestnov/marko/internal/version"
	"github.com/spf13/cobra"
)

// Global flags shared by all commands (architecture.md §9).
var (
	configPath   string
	templatesDir string
	verbose      bool
)

var rootCmd = &cobra.Command{
	Use:           "marko",
	Short:         "Bookmark infrastructure as code",
	Long:          "Marko - Bookmark infrastructure as code.\n\nMarko lets you declare your browser bookmarks (folders, links, reusable\ntemplates) in a YAML file and syncs that declared state into your\nbrowser by reading and writing its Bookmarks file directly (marko sync)\n-- no browser extension required.",
	SilenceUsage:  true,
	SilenceErrors: true,
}

func init() {
	rootCmd.PersistentFlags().StringVar(&configPath, "config", "", "path to marko.yaml (default: search upward from cwd)")
	rootCmd.PersistentFlags().StringVar(&templatesDir, "templates-dir", "", "path to templates directory (default: <dir of marko.yaml>/templates)")
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "verbose output")
	rootCmd.Version = version.Version
}

// Execute runs the root command. Called from main(). Exit codes follow
// docs/architecture.md §9's shared convention: 0 success, 1
// validation/runtime error, 2 usage error, 3 I/O error. Commands signal
// their intended exit code by returning a *exitError; anything else
// defaults to 1.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(exitCodeFor(err))
	}
}

// exitError wraps an error with an explicit process exit code.
type exitError struct {
	code int
	err  error
}

func (e *exitError) Error() string { return e.err.Error() }
func (e *exitError) Unwrap() error { return e.err }

func newExitError(code int, err error) error {
	return &exitError{code: code, err: err}
}

func exitCodeFor(err error) int {
	if ee, ok := err.(*exitError); ok {
		return ee.code
	}
	return 1
}
