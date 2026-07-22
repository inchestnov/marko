package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

var (
	initDir   string
	initForce bool
)

const starterMarkoYAML = `version: "1"

# See docs/yaml-reference.md and docs/templates.md for the full schema.
collections:
  personal:
    root: other
    bookmarks:
      - name: Example
        url: "https://example.com"
`

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Scaffold a starter marko.yaml (and templates/ dir) in the target directory",
	RunE: func(cmd *cobra.Command, args []string) error {
		targetDir := initDir
		if targetDir == "" {
			targetDir = "."
		}

		if err := os.MkdirAll(targetDir, 0o755); err != nil {
			return newExitError(3, fmt.Errorf("creating directory %q: %w", targetDir, err))
		}

		markoPath := filepath.Join(targetDir, "marko.yaml")
		if _, err := os.Stat(markoPath); err == nil && !initForce {
			return newExitError(2, fmt.Errorf("%s already exists (use --force to overwrite)", markoPath))
		}

		if err := os.WriteFile(markoPath, []byte(starterMarkoYAML), 0o644); err != nil {
			return newExitError(3, fmt.Errorf("writing %q: %w", markoPath, err))
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Created %s\n", markoPath)

		templatesPath := filepath.Join(targetDir, "templates")
		if err := os.MkdirAll(templatesPath, 0o755); err != nil {
			return newExitError(3, fmt.Errorf("creating %q: %w", templatesPath, err))
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Created %s/\n", templatesPath)

		return nil
	},
}

func init() {
	initCmd.Flags().StringVar(&initDir, "dir", ".", "target directory")
	initCmd.Flags().BoolVar(&initForce, "force", false, "overwrite existing marko.yaml")
	rootCmd.AddCommand(initCmd)
}
