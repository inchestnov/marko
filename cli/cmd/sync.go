package cmd

import (
	"fmt"
	"io"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/inchestnov/marko/cli/browserfile"
	"github.com/inchestnov/marko/cli/diff"
	"github.com/inchestnov/marko/cli/internal/bookmarktree"
	"github.com/inchestnov/marko/cli/sync"
	"github.com/inchestnov/marko/cli/validator"
	"github.com/spf13/cobra"
)

var (
	syncBridge        string
	syncBrowser       string
	syncProfile       string
	syncBookmarksFile string
	syncForce         bool
	syncPreview       bool

	// --bridge=http only.
	syncPort     int
	syncTimeout  string
	syncAutoOpen bool
)

var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Compute the diff and import it into the browser's bookmarks",
	RunE: func(cmd *cobra.Command, args []string) error {
		switch syncBridge {
		case "file":
			return runFileBridgeSync(cmd)
		case "http":
			return runHTTPBridgeSync(cmd)
		default:
			return newExitError(2, fmt.Errorf("invalid --bridge %q (must be \"file\" or \"http\")", syncBridge))
		}
	},
}

func init() {
	syncCmd.Flags().StringVar(&syncBridge, "bridge", "file", `how to reach the browser: "file" (read/write its Bookmarks file directly, default) or "http" (the legacy local HTTP server + Chrome extension bridge)`)
	syncCmd.Flags().StringVar(&syncBrowser, "browser", "", fmt.Sprintf("browser whose Bookmarks file to use with --bridge=file (one of %v, default %q)", browserfile.KnownBrowsers, browserfile.DefaultBrowser))
	syncCmd.Flags().StringVar(&syncProfile, "profile", "", fmt.Sprintf("browser profile directory name with --bridge=file (default %q)", browserfile.DefaultProfile))
	syncCmd.Flags().StringVar(&syncBookmarksFile, "bookmarks-file", "", "explicit path to a Bookmarks file with --bridge=file, overriding --browser/--profile")
	syncCmd.Flags().BoolVar(&syncForce, "force", false, "with --bridge=file, write even if the browser appears to be running for this profile")
	syncCmd.Flags().BoolVar(&syncPreview, "preview", false, "compute and log the plan without making any changes (dry run)")

	syncCmd.Flags().IntVar(&syncPort, "port", 8765, "(--bridge=http only) port to bind the local HTTP server to")
	syncCmd.Flags().StringVar(&syncTimeout, "timeout", "5m", "(--bridge=http only) shut down automatically after this duration (0 = no timeout)")
	syncCmd.Flags().BoolVar(&syncAutoOpen, "auto-open", true, "(--bridge=http only) automatically open the Marko extension's auto-sync page via the OS default browser handler")

	rootCmd.AddCommand(syncCmd)
}

// runFileBridgeSync is the default sync mechanism: it locates the target
// browser's native Bookmarks file, reads it directly into a
// bookmarktree.BookmarkTree, diffs it against the desired state exactly
// like every other Marko command does, and (unless --preview) applies
// the resulting plan and writes the file back -- no browser extension,
// HTTP server, or CORS/manifest concerns involved at all. See
// cli/browserfile and docs/sync-protocol.md.
func runFileBridgeSync(cmd *cobra.Command) error {
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
}

// runHTTPBridgeSync is the legacy bridge: it starts the local HTTP server
// and opens the extension's auto-sync page (or waits for the popup's
// manual "Connect"), exactly as before --bridge existed. Kept for cases
// where writing the Bookmarks file directly isn't desired or the
// extension is what's available.
func runHTTPBridgeSync(cmd *cobra.Command) error {
	timeout, err := time.ParseDuration(syncTimeout)
	if err != nil {
		return newExitError(2, fmt.Errorf("invalid --timeout %q: %w", syncTimeout, err))
	}

	render := func() (*bookmarktree.BookmarkTree, error) {
		pr, err := runPipeline()
		if err != nil {
			if pr != nil && len(pr.Findings) > 0 {
				for _, f := range pr.Findings {
					if f.Severity == validator.SeverityError {
						return nil, &sync.PipelineError{Code: f.Code, Message: f.Message}
					}
				}
			}
			return nil, &sync.PipelineError{Code: "E_RUNTIME", Message: err.Error()}
		}
		return pr.Tree, nil
	}

	out := cmd.OutOrStdout()

	srv := sync.NewServer(render)
	srv.OnDiff = func(plan *diff.Plan) {
		fmt.Fprintf(out, "\nComputed plan (%d operation(s)):\n", len(plan.Operations))
		printPlan(cmd, plan)
		fmt.Fprintln(out)
	}
	srv.OnReport = func(req sync.ReportRequest) {
		if req.Preview {
			fmt.Fprintln(out, "Preview report from extension (no changes were made):")
		} else {
			fmt.Fprintln(out, "Import report from extension:")
		}
		printReportResults(out, req.Results)
	}

	if err := srv.Listen(syncPort); err != nil {
		return newExitError(1, err)
	}

	go func() {
		_ = srv.Serve()
	}()

	fmt.Fprintf(out, "marko sync listening on http://127.0.0.1:%d\n", syncPort)
	if syncPreview {
		fmt.Fprintln(out, "Preview mode: the plan will be computed and logged, but nothing will be changed in Chrome.")
	}

	if syncAutoOpen {
		url := sync.AutoOpenURL(syncPort, syncPreview)
		fmt.Fprintln(out, "Opening the Marko extension in your browser...")
		openBrowser(url)
	} else {
		fmt.Fprintf(out, "Open %s in Chrome to review and %s the plan.\n", sync.AutoOpenURL(syncPort, syncPreview), map[bool]string{true: "preview", false: "apply"}[syncPreview])
	}

	if timeout > 0 {
		fmt.Fprintf(out, "Waiting... (timeout in %s)\n", timeout)
	} else {
		fmt.Fprintln(out, "Waiting... (no timeout, Ctrl+C to stop)")
	}

	received, okCount, errorCount, preview := srv.WaitForReport(timeout)
	_ = srv.Shutdown()

	if !received {
		fmt.Fprintln(out, "Timed out waiting for a report from the extension.")
		return newExitError(1, fmt.Errorf("sync timed out with no report received"))
	}

	if preview {
		fmt.Fprintln(out, "Preview complete. No changes were made to Chrome.")
		return nil
	}

	fmt.Fprintf(out, "Sync complete: %d ok, %d errors\n", okCount, errorCount)
	if errorCount > 0 {
		return newExitError(1, fmt.Errorf("%d operation(s) reported errors", errorCount))
	}
	return nil
}

// printReportResults prints one line per operation result reported by the
// extension, e.g.:
//
//	CREATE  folder    other/Marko/foo          ok       id=137
//	DELETE  bookmark  other/Marko/bar          error    chrome.bookmarks.remove failed: ...
//	CREATE  folder    other/Marko/baz          planned
func printReportResults(out io.Writer, results []sync.ReportResult) {
	for _, res := range results {
		path := strings.Join(res.TargetPath, "/")
		line := fmt.Sprintf("%-7s %-40s %s", res.Type, path, res.Status)
		switch res.Status {
		case "ok":
			if res.BrowserID != "" {
				line += fmt.Sprintf("  id=%s", res.BrowserID)
			}
		case "error":
			line += fmt.Sprintf("  %s", res.Error)
		}
		fmt.Fprintln(out, line)
	}
}

// openBrowser best-effort opens url via the OS's default handler.
func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}
