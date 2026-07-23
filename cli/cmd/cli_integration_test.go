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
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/inchestnov/marko/cli/internal/bookmarktree"
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
	jsonOutput = false

	initDir = "."
	initForce = false

	renderOut = ""

	diffActual = ""

	syncBrowser = ""
	syncProfile = ""
	syncBookmarksFile = ""
	syncForce = false
	syncPreview = false
}

func TestCLI_InitValidateRenderDiff_RealisticUserFlow(t *testing.T) {
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

	// 2. marko validate -- the scaffolded marko.yaml must be valid.
	stdout, stderr, code = runCmd(t, dir, "validate")
	if code != 0 {
		t.Fatalf("marko validate: expected exit 0, got %d (stdout: %s, stderr: %s)", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "is valid") {
		t.Fatalf("marko validate: expected 'is valid' in stdout, got %q", stdout)
	}

	// 3. marko render -- expect a tree view mentioning the scaffolded
	// "personal" collection's bookmark.
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

	// render --json must produce a valid bookmarktree.BookmarkTree JSON.
	stdout, _, code = runCmd(t, dir, "render", "--json")
	if code != 0 {
		t.Fatalf("marko render --json: expected exit 0, got %d", code)
	}
	var tree bookmarktree.BookmarkTree
	if err := json.Unmarshal([]byte(stdout), &tree); err != nil {
		t.Fatalf("marko render --json: output is not valid BookmarkTree JSON: %v\noutput: %s", err, stdout)
	}
	if len(tree.Roots) != 2 {
		t.Fatalf("expected 2 roots (bar, other), got %d", len(tree.Roots))
	}

	// 4. marko diff --actual <empty tree file> -- expect CREATE operations
	// (an empty browser has nothing matching the scaffolded "Example"
	// bookmark, so it must be CREATEd).
	emptyActual := bookmarktree.BookmarkTree{
		Roots: []*bookmarktree.Node{
			{Kind: bookmarktree.KindFolder, Name: "bar", BrowserID: "1"},
			{Kind: bookmarktree.KindFolder, Name: "other", BrowserID: "2"},
		},
	}
	actualPath := filepath.Join(dir, "actual.json")
	actualData, err := json.Marshal(emptyActual)
	if err != nil {
		t.Fatalf("marshal empty actual tree: %v", err)
	}
	if err := os.WriteFile(actualPath, actualData, 0o644); err != nil {
		t.Fatalf("write actual tree file: %v", err)
	}

	stdout, stderr, code = runCmd(t, dir, "diff", "--actual", actualPath)
	if code != 0 {
		t.Fatalf("marko diff: expected exit 0 (non-empty diff is not an error), got %d (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stdout, "CREATE") {
		t.Fatalf("marko diff: expected at least one CREATE op in output, got %q", stdout)
	}
	if !strings.Contains(stdout, "Example") {
		t.Fatalf("marko diff: expected CREATE for scaffolded 'Example' bookmark, got %q", stdout)
	}

	// diff --json must parse as a diff.Plan with only CREATE ops (browser
	// starts empty).
	stdout, _, code = runCmd(t, dir, "diff", "--actual", actualPath, "--json")
	if code != 0 {
		t.Fatalf("marko diff --json: expected exit 0, got %d", code)
	}
	var plan struct {
		GeneratedAt string `json:"generatedAt"`
		Operations  []struct {
			Type string `json:"type"`
			Name string `json:"name"`
		} `json:"operations"`
	}
	if err := json.Unmarshal([]byte(stdout), &plan); err != nil {
		t.Fatalf("marko diff --json: output is not valid Plan JSON: %v\noutput: %s", err, stdout)
	}
	if len(plan.Operations) == 0 {
		t.Fatal("expected at least one operation in the JSON plan")
	}
	for _, op := range plan.Operations {
		if op.Type != "CREATE" {
			t.Fatalf("expected only CREATE ops against an empty browser tree, found %s", op.Type)
		}
	}
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

func TestCLI_DiffWithoutActualFlagDirectsToSync(t *testing.T) {
	dir := t.TempDir()
	if _, _, code := runCmd(t, dir, "init"); code != 0 {
		t.Fatalf("marko init setup failed")
	}

	_, stderr, code := runCmd(t, dir, "diff")
	if code != 1 {
		t.Fatalf("marko diff without --actual: expected exit 1, got %d", code)
	}
	if !strings.Contains(stderr, "marko sync") {
		t.Fatalf("expected guidance to run 'marko sync' on stderr, got %q", stderr)
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

	_, stderr, code := runCmd(t, dir, "sync", "--bookmarks-file", bookmarksPath, "--preview")
	if code != 1 {
		t.Fatalf("expected exit 1 when the browser appears to be running, got %d (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stderr, "--force") {
		t.Fatalf("expected guidance to pass --force on stderr, got %q", stderr)
	}
}

func TestCLI_SyncFileBridge_ForceWarnsAndProceeds(t *testing.T) {
	dir := t.TempDir()
	if _, _, code := runCmd(t, dir, "init"); code != 0 {
		t.Fatalf("marko init setup failed")
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
