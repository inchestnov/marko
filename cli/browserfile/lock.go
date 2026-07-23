package browserfile

import (
	"os"
	"path/filepath"
)

// singletonLockPath returns the path to Chromium's "SingletonLock" file,
// which lives in the browser's top-level user-data directory (the parent
// of the "Default"/"Profile N" subdirectories), not inside the profile
// itself. Chromium creates this (a symlink, on POSIX) while running and
// removes it on clean shutdown.
func singletonLockPath(bookmarksFilePath string) string {
	// bookmarksFilePath is .../<UserDataDir>/<Profile>/Bookmarks
	profileDir := filepath.Dir(bookmarksFilePath)
	userDataDir := filepath.Dir(profileDir)
	return filepath.Join(userDataDir, "SingletonLock")
}

// IsBrowserRunning best-effort detects whether the browser that owns
// bookmarksFilePath currently has this profile open, via Chromium's
// SingletonLock file/symlink. A false negative is possible (e.g. after an
// unclean shutdown that left a stale lock, or on platforms/versions that
// use a different mechanism) but a false positive should not happen: this
// only reports "running" when Chromium's own lock is actually present.
func IsBrowserRunning(bookmarksFilePath string) bool {
	_, err := os.Lstat(singletonLockPath(bookmarksFilePath))
	return err == nil
}
