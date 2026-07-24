// Black-box CLI integration test exercising the actual Cobra command
// layer end-to-end (in-process, no shelling out to `go build`/`go run`),
// per docs/architecture.md §9's worked examples. This complements
// cli/internal/integration/integration_test.go, which calls the
// parser/template/renderer/diff packages directly and never goes through
// cmd.Execute()/the Cobra RunE handlers, global flag wiring, or stdout
// formatting (tree view / JSON / plan text) that real users actually see.
//
// Technique: rootCmd is a package-level singleton (see root.go), so each
// invocation here resets rootCmd's persistent/local flags to their
// defaults, sets fresh args via SetArgs, and captures stdout/stderr via
// SetOut/SetErr before calling rootCmd.Execute(). This avoids the
// slowness/fragility of shelling out to a built binary while still
// driving the real Cobra RunE functions (not just the underlying Go
// packages).
package cmd

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runCmd resets global flag state, executes rootCmd with the given args
// in dir, and returns captured stdout, stderr, and the process exit code
// that main() would have used (via exitCodeFor).
func runCmd(t *testing.T, dir string, args ...string) (stdout, stderr string, exitCode int) {
	t.Helper()

	prevWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if dir != "" {
		if err := os.Chdir(dir); err != nil {
			t.Fatalf("Chdir(%q): %v", dir, err)
		}
		defer func() {
			if err := os.Chdir(prevWD); err != nil {
				t.Fatalf("restoring cwd: %v", err)
			}
		}()
	}

	resetGlobalFlags()

	var outBuf, errBuf bytes.Buffer
	rootCmd.SetOut(&outBuf)
	rootCmd.SetErr(&errBuf)
	rootCmd.SetArgs(args)

	execErr := rootCmd.Execute()

	// rootCmd.Execute() itself never calls os.Exit and (since
	// SilenceErrors is set on rootCmd) never prints the returned RunE
	// error either -- only the package-level Execute() function (called
	// from main()) does that, via fmt.Fprintln(os.Stderr, "Error:", err).
	// Replicate both behaviors here without touching the real process
	// stdout/stderr/exit code.
	code := 0
	if execErr != nil {
		code = exitCodeFor(execErr)
		fmt.Fprintln(&errBuf, "Error:", execErr)
	}

	return outBuf.String(), errBuf.String(), code
}

// resetGlobalFlags restores every package-level flag variable (and each
// subcommand's own Flags()) to its zero/default value between test
// invocations, since rootCmd and its subcommands are process-wide
// singletons (init() registers them once) and SetArgs/Execute alone does
// not reset flags that are omitted on a subsequent call.
func resetGlobalFlags() {
	configPath = ""
	templatesDir = ""
	verbose = false

	initDir = "."
	initForce = false

	renderOut = ""

	syncBrowser = ""
	syncBookmarksFile = ""
	syncForce = false
	syncPreview = false
}

func TestCLI_InitValidateRender_RealisticUserFlow(t *testing.T) {
	dir := t.TempDir()

	// 1. marko init
	stdout, stderr, code := runCmd(t, dir, "init")
	if code != 0 {
		t.Fatalf("marko init: expected exit 0, got %d (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stdout, "Created") || !strings.Contains(stdout, "marko.yaml") {
		t.Fatalf("marko init: expected 'Created ... marko.yaml' in stdout, got %q", stdout)
	}
	if _, err := os.Stat(filepath.Join(dir, "marko.yaml")); err != nil {
		t.Fatalf("expected marko.yaml to be created: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "templates")); err != nil {
		t.Fatalf("expected templates/ to be created: %v", err)
	}

	// init again without --force should fail with exit code 2 (usage error
	// per §9's exit code convention and §9.1's documented behavior).
	_, _, code = runCmd(t, dir, "init")
	if code != 2 {
		t.Fatalf("marko init (no --force, already exists): expected exit 2, got %d", code)
	}

	// 2. marko validate -- the raw scaffold is intentionally not yet
	// valid: its example bookmark and example template usage are both
	// commented out (so 'marko sync' can never surprise-create anything
	// from a fresh scaffold), leaving the "personal" collection empty.
	stdout, stderr, code = runCmd(t, dir, "validate")
	if code != 1 {
		t.Fatalf("marko validate (fresh scaffold): expected exit 1, got %d (stdout: %s, stderr: %s)", code, stdout, stderr)
	}
	if !strings.Contains(stderr, "E_EMPTY_COLLECTION") {
		t.Fatalf("marko validate (fresh scaffold): expected E_EMPTY_COLLECTION on stderr, got %q", stderr)
	}

	// 3. Fill in a real bookmark (simulating a user completing the
	// scaffold), then marko validate/render must succeed against it.
	markoPath := filepath.Join(dir, "marko.yaml")
	filled := strings.Replace(readFile(t, markoPath),
		"root: other\n",
		"root: other\n    bookmarks:\n      - name: Example\n        url: \"https://example.com\"\n",
		1)
	if err := os.WriteFile(markoPath, []byte(filled), 0o644); err != nil {
		t.Fatalf("writing filled-in marko.yaml: %v", err)
	}

	stdout, stderr, code = runCmd(t, dir, "validate")
	if code != 0 {
		t.Fatalf("marko validate (filled in): expected exit 0, got %d (stdout: %s, stderr: %s)", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "is valid") {
		t.Fatalf("marko validate (filled in): expected 'is valid' in stdout, got %q", stdout)
	}

	stdout, stderr, code = runCmd(t, dir, "render")
	if code != 0 {
		t.Fatalf("marko render: expected exit 0, got %d (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stdout, "Other Bookmarks") {
		t.Fatalf("marko render: expected root 'Other Bookmarks' in tree view, got %q", stdout)
	}
	if !strings.Contains(stdout, "Example") {
		t.Fatalf("marko render: expected scaffolded 'Example' bookmark in tree view, got %q", stdout)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %q: %v", path, err)
	}
	return string(data)
}

func TestCLI_ValidateFailsWithNonZeroExitOnInvalidYAML(t *testing.T) {
	dir := t.TempDir()

	// A marko.yaml missing "collections" (E_MISSING_FIELD) must fail
	// validate with exit code 1 and print the error code to stderr.
	invalid := "version: \"1\"\n"
	if err := os.WriteFile(filepath.Join(dir, "marko.yaml"), []byte(invalid), 0o644); err != nil {
		t.Fatalf("writing invalid marko.yaml: %v", err)
	}

	stdout, stderr, code := runCmd(t, dir, "validate")
	if code != 1 {
		t.Fatalf("expected exit 1 for invalid marko.yaml, got %d (stdout: %s, stderr: %s)", code, stdout, stderr)
	}
	if !strings.Contains(stderr, "E_MISSING_FIELD") {
		t.Fatalf("expected E_MISSING_FIELD on stderr, got %q", stderr)
	}
}

func TestCLI_RenderFailsWhenValidationFails(t *testing.T) {
	dir := t.TempDir()
	invalid := "version: \"1\"\ncollections:\n  work:\n    bookmarks:\n      - name: X\n        url: \"not-a-url\"\n"
	if err := os.WriteFile(filepath.Join(dir, "marko.yaml"), []byte(invalid), 0o644); err != nil {
		t.Fatalf("writing invalid marko.yaml: %v", err)
	}

	_, stderr, code := runCmd(t, dir, "render")
	if code != 1 {
		t.Fatalf("marko render on invalid config: expected exit 1, got %d", code)
	}
	if !strings.Contains(stderr, "E_INVALID_URL") {
		t.Fatalf("expected E_INVALID_URL on stderr, got %q", stderr)
	}
}

const fakeBookmarksFileFixture = `{
  "checksum": "x",
  "roots": {
    "bookmark_bar": {"children": [], "id": "1", "name": "Bookmarks bar", "type": "folder"},
    "other": {"children": [], "id": "2", "name": "Other bookmarks", "type": "folder"},
    "synced": {"children": [], "id": "3", "name": "Mobile bookmarks", "type": "folder"}
  },
  "version": 1
}`

// setUpFakeProfile creates <dir>/UserData/Default/Bookmarks and, if
// lockRunning is true, a SingletonLock file next to it (the marker
// browserfile.IsBrowserRunning looks for), returning the Bookmarks path.
func setUpFakeProfile(t *testing.T, dir string, lockRunning bool) string {
	t.Helper()
	profileDir := filepath.Join(dir, "UserData", "Default")
	if err := os.MkdirAll(profileDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	path := filepath.Join(profileDir, "Bookmarks")
	if err := os.WriteFile(path, []byte(fakeBookmarksFileFixture), 0o644); err != nil {
		t.Fatalf("writing fake Bookmarks: %v", err)
	}
	if lockRunning {
		lockPath := filepath.Join(dir, "UserData", "SingletonLock")
		if err := os.Symlink("somehost-1234", lockPath); err != nil {
			t.Fatalf("creating fake SingletonLock: %v", err)
		}
	}
	return path
}

func TestCLI_SyncFileBridge_RefusesWhenBrowserRunning(t *testing.T) {
	dir := t.TempDir()
	if _, _, code := runCmd(t, dir, "init"); code != 0 {
		t.Fatalf("marko init setup failed")
	}
	bookmarksPath := setUpFakeProfile(t, dir, true)

	_, stderr, code := runCmd(t, dir, "sync", "--bookmarks-file", bookmarksPath)
	if code != 1 {
		t.Fatalf("expected exit 1 when the browser appears to be running, got %d (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stderr, "--force") {
		t.Fatalf("expected guidance to pass --force on stderr, got %q", stderr)
	}
}

func TestCLI_SyncFileBridge_PreviewIgnoresBrowserRunning(t *testing.T) {
	dir := t.TempDir()
	valid := "version: \"1\"\ncollections:\n  personal:\n    root: other\n    bookmarks:\n      - name: Example\n        url: \"https://example.com\"\n"
	if err := os.WriteFile(filepath.Join(dir, "marko.yaml"), []byte(valid), 0o644); err != nil {
		t.Fatalf("writing marko.yaml: %v", err)
	}
	bookmarksPath := setUpFakeProfile(t, dir, true)

	stdout, stderr, code := runCmd(t, dir, "sync", "--bookmarks-file", bookmarksPath, "--preview")
	if code != 0 {
		t.Fatalf("expected --preview to succeed without --force even when the browser appears to be running, got %d (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stdout, "Preview complete") {
		t.Fatalf("expected a preview to run to completion, got stdout %q", stdout)
	}
}

func TestCLI_SyncFileBridge_ForceWarnsAndProceeds(t *testing.T) {
	dir := t.TempDir()
	valid := "version: \"1\"\ncollections:\n  personal:\n    root: other\n    bookmarks:\n      - name: Example\n        url: \"https://example.com\"\n"
	if err := os.WriteFile(filepath.Join(dir, "marko.yaml"), []byte(valid), 0o644); err != nil {
		t.Fatalf("writing marko.yaml: %v", err)
	}
	bookmarksPath := setUpFakeProfile(t, dir, true)

	stdout, stderr, code := runCmd(t, dir, "sync", "--bookmarks-file", bookmarksPath, "--force")
	if code != 0 {
		t.Fatalf("expected exit 0 with --force, got %d (stdout: %s, stderr: %s)", code, stdout, stderr)
	}
	if !strings.Contains(stderr, "warning:") || !strings.Contains(stderr, "--force") {
		t.Fatalf("expected a warning on stderr mentioning --force, got %q", stderr)
	}
	if !strings.Contains(stdout, "Wrote") {
		t.Fatalf("expected the sync to actually proceed and write, got stdout %q", stdout)
	}
}
