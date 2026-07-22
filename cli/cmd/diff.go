package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/inchestnov/marko/cli/diff"
	"github.com/inchestnov/marko/cli/internal/bookmarktree"
	"github.com/spf13/cobra"
)

var diffActual string

var diffCmd = &cobra.Command{
	Use:   "diff",
	Short: "Compare the desired state against a captured browser state and print an operation plan",
	RunE: func(cmd *cobra.Command, args []string) error {
		if diffActual == "" {
			return newExitError(1, fmt.Errorf("--actual <file> is required; marko cannot read the browser directly (no native messaging). Run \"marko sync\" instead, or capture browser state to a file via the extension's Options page and pass it with --actual"))
		}

		pr, err := runPipeline()
		if err != nil {
			printFindingsToStderr(pr)
			return err
		}

		actualData, err := os.ReadFile(diffActual)
		if err != nil {
			return newExitError(3, fmt.Errorf("reading --actual file %q: %w", diffActual, err))
		}

		var actual bookmarktree.BookmarkTree
		if err := json.Unmarshal(actualData, &actual); err != nil {
			return newExitError(1, fmt.Errorf("parsing --actual file %q: %w", diffActual, err))
		}

		plan := diff.Diff(pr.Tree, &actual)

		if jsonOutput {
			data, err := json.MarshalIndent(plan, "", "  ")
			if err != nil {
				return newExitError(1, err)
			}
			fmt.Fprintln(cmd.OutOrStdout(), string(data))
			return nil
		}

		printPlan(cmd, plan)
		return nil
	},
}

func init() {
	diffCmd.Flags().StringVar(&diffActual, "actual", "", "path to a previously-exported actualTree JSON file")
	rootCmd.AddCommand(diffCmd)
}

func printPlan(cmd *cobra.Command, plan *diff.Plan) {
	out := cmd.OutOrStdout()
	for _, op := range plan.Operations {
		path := strings.Join(op.TargetPath, "/")
		line := fmt.Sprintf("%-7s %-9s %s", op.Type, op.Kind, path)
		if op.Type == diff.OpUpdate && len(op.Changes) > 0 {
			line += fmt.Sprintf("  (%s changed)", strings.Join(op.Changes, ", "))
		}
		fmt.Fprintln(out, line)
	}
}
