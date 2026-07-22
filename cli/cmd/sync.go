package cmd

import (
	"fmt"
	"os/exec"
	"runtime"
	"time"

	"github.com/inchestnov/marko/cli/internal/bookmarktree"
	"github.com/inchestnov/marko/cli/sync"
	"github.com/inchestnov/marko/cli/validator"
	"github.com/spf13/cobra"
)

var (
	syncPort     int
	syncTimeout  string
	syncAutoOpen bool
)

var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Start the local HTTP server for the Chrome extension to review and apply changes",
	RunE: func(cmd *cobra.Command, args []string) error {
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

		srv := sync.NewServer(render)
		if err := srv.Listen(syncPort); err != nil {
			return newExitError(1, err)
		}

		go func() {
			_ = srv.Serve()
		}()

		out := cmd.OutOrStdout()
		fmt.Fprintf(out, "marko sync listening on http://127.0.0.1:%d\n", syncPort)
		fmt.Fprintln(out, "Open the Marko extension and click \"Connect\" to review and apply changes.")
		if timeout > 0 {
			fmt.Fprintf(out, "Waiting... (timeout in %s)\n", timeout)
		} else {
			fmt.Fprintln(out, "Waiting... (no timeout, Ctrl+C to stop)")
		}

		if syncAutoOpen {
			openBrowser(fmt.Sprintf("http://127.0.0.1:%d/health", syncPort))
		}

		received, okCount, errorCount := srv.WaitForReport(timeout)
		_ = srv.Shutdown()

		if !received {
			fmt.Fprintln(out, "Timed out waiting for a report from the extension.")
			return newExitError(1, fmt.Errorf("sync timed out with no report received"))
		}

		fmt.Fprintf(out, "Sync complete: %d ok, %d errors\n", okCount, errorCount)
		if errorCount > 0 {
			return newExitError(1, fmt.Errorf("%d operation(s) reported errors", errorCount))
		}
		return nil
	},
}

func init() {
	syncCmd.Flags().IntVar(&syncPort, "port", 8765, "port to bind the local HTTP server to")
	syncCmd.Flags().StringVar(&syncTimeout, "timeout", "5m", "shut down automatically after this duration (0 = no timeout)")
	syncCmd.Flags().BoolVar(&syncAutoOpen, "auto-open", false, "best-effort open the extension's popup URL via the OS default browser handler")
	rootCmd.AddCommand(syncCmd)
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
