// Package browserfile reads and writes a Chromium-family browser's native
// "Bookmarks" JSON file directly, as an alternative to the HTTP+extension
// bridge (see cli/sync and docs/sync-protocol.md): instead of a browser
// extension calling chrome.bookmarks.*, marko sync locates the browser's
// profile directory, parses its Bookmarks file into a bookmarktree.Tree,
// runs it through the same diff engine used everywhere else in Marko, and
// writes the result back to disk. This requires the browser to not be
// actively running against that profile (see lock.go), since Chromium
// periodically flushes its in-memory bookmark model back to this file and
// would otherwise silently overwrite Marko's changes.
package browserfile

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// DefaultBrowser and DefaultProfile are the defaults `marko sync` uses
// when --browser/--profile are not passed.
const (
	DefaultBrowser = "brave"
	DefaultProfile = "Default"
)

// KnownBrowsers lists the --browser values LocateBookmarksFile understands.
var KnownBrowsers = []string{"brave", "chrome", "chromium", "edge"}

// userDataDir returns the browser's top-level "User Data"-equivalent
// directory (the parent of per-profile subdirectories like "Default"),
// per OS and browser. This is also where Chromium's SingletonLock file
// lives (see lock.go).
func userDataDir(browser string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("browserfile: resolving home directory: %w", err)
	}

	switch runtime.GOOS {
	case "darwin":
		base := filepath.Join(home, "Library", "Application Support")
		switch browser {
		case "brave":
			return filepath.Join(base, "BraveSoftware", "Brave-Browser"), nil
		case "chrome":
			return filepath.Join(base, "Google", "Chrome"), nil
		case "chromium":
			return filepath.Join(base, "Chromium"), nil
		case "edge":
			return filepath.Join(base, "Microsoft Edge"), nil
		}
	case "linux":
		base := filepath.Join(home, ".config")
		switch browser {
		case "brave":
			return filepath.Join(base, "BraveSoftware", "Brave-Browser"), nil
		case "chrome":
			return filepath.Join(base, "google-chrome"), nil
		case "chromium":
			return filepath.Join(base, "chromium"), nil
		case "edge":
			return filepath.Join(base, "microsoft-edge"), nil
		}
	case "windows":
		base := os.Getenv("LOCALAPPDATA")
		if base == "" {
			return "", fmt.Errorf("browserfile: %%LOCALAPPDATA%% is not set")
		}
		switch browser {
		case "brave":
			return filepath.Join(base, "BraveSoftware", "Brave-Browser", "User Data"), nil
		case "chrome":
			return filepath.Join(base, "Google", "Chrome", "User Data"), nil
		case "chromium":
			return filepath.Join(base, "Chromium", "User Data"), nil
		case "edge":
			return filepath.Join(base, "Microsoft", "Edge", "User Data"), nil
		}
	}

	return "", fmt.Errorf("browserfile: unknown browser %q (known: %v) or unsupported OS %q", browser, KnownBrowsers, runtime.GOOS)
}

// LocateBookmarksFile resolves the path to a Chromium-family browser's
// Bookmarks file for the given browser name (see KnownBrowsers; "" means
// DefaultBrowser) and profile directory name (e.g. "Default", "Profile 1";
// "" means DefaultProfile). It does not check whether the file exists.
func LocateBookmarksFile(browser, profile string) (string, error) {
	if browser == "" {
		browser = DefaultBrowser
	}
	if profile == "" {
		profile = DefaultProfile
	}
	dir, err := userDataDir(browser)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, profile, "Bookmarks"), nil
}
