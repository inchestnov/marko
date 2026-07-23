package cmd

import (
	"fmt"

	"github.com/inchestnov/marko/cli/browserfile"
	"github.com/inchestnov/marko/cli/diff"
	"github.com/spf13/cobra"
)

var (
	syncBrowser       string
	syncProfile       string
	syncBookmarksFile string
	syncForce         bool
	syncPreview       bool
)

var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Compute the diff and import it into the browser's bookmarks",
	RunE: func(cmd *cobra.Command, args []string) error {
		out := cmd.OutOrStdout()

		path := syncBookmarksFile
		if path == "" {
			var err error
			path, err = browserfile.LocateBookmarksFile(syncBrowser, syncProfile)
			if err != nil {
				return newExitError(2, err)
			}
		}
		fmt.Fprintf(out, "Bookmarks file: %s\n", path)

		if browserfile.IsBrowserRunning(path) {
			if !syncForce {
				return newExitError(1, fmt.Errorf(
					"the browser appears to be running for this profile (found its SingletonLock) -- "+
						"close it first, since it will otherwise overwrite these changes the next time it saves its own state, "+
						"or pass --force to write anyway"))
			}
			fmt.Fprintln(cmd.ErrOrStderr(), "warning: the browser appears to be running for this profile; writing anyway because --force was passed. It may overwrite this change the next time it saves its own state.")
		}

		pr, err := runPipeline()
		if err != nil {
			printFindingsToStderr(cmd.ErrOrStderr(), pr)
			return err
		}

		bf, err := browserfile.ReadFile(path)
		if err != nil {
			return newExitError(3, err)
		}
		actual, err := bf.ToBookmarkTree()
		if err != nil {
			return newExitError(1, err)
		}

		plan := diff.Diff(pr.Tree, actual)
		fmt.Fprintf(out, "\nComputed plan (%d operation(s)):\n", len(plan.Operations))
		printPlan(cmd, plan)

		if len(plan.Operations) == 0 {
			fmt.Fprintln(out, "\nNothing to do -- already matches the desired state.")
			return nil
		}

		if syncPreview {
			fmt.Fprintln(out, "\nPreview complete. No changes were made.")
			return nil
		}

		if err := bf.Apply(plan); err != nil {
			return newExitError(1, fmt.Errorf("applying plan: %w", err))
		}
		if err := bf.Write(true); err != nil {
			return newExitError(3, fmt.Errorf("writing %q: %w", path, err))
		}

		fmt.Fprintf(out, "\nWrote %d operation(s) to %s (a backup of the previous content was saved alongside it).\n", len(plan.Operations), path)
		fmt.Fprintln(out, "Restart the browser (if it was already closed, just open it) to see the change.")
		return nil
	},
}

func init() {
	syncCmd.Flags().StringVar(&syncBrowser, "browser", "", fmt.Sprintf("browser whose Bookmarks file to use (one of %v, default %q)", browserfile.KnownBrowsers, browserfile.DefaultBrowser))
	syncCmd.Flags().StringVar(&syncProfile, "profile", "", fmt.Sprintf("browser profile directory name (default %q)", browserfile.DefaultProfile))
	syncCmd.Flags().StringVar(&syncBookmarksFile, "bookmarks-file", "", "explicit path to a Bookmarks file, overriding --browser/--profile")
	syncCmd.Flags().BoolVar(&syncForce, "force", false, "write even if the browser appears to be running for this profile (prints a warning instead of refusing)")
	syncCmd.Flags().BoolVar(&syncPreview, "preview", false, "compute and log the plan without making any changes (dry run)")
	rootCmd.AddCommand(syncCmd)
}
