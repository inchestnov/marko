package sync

import "fmt"

// ExtensionID is the Chrome extension id for extension/, pinned via the
// "key" field in extension/chrome/manifest.json so that it stays the same
// every time the unpacked extension is loaded, regardless of machine or
// install path. This lets `marko sync --auto-open` construct a
// chrome-extension:// URL for the auto-sync page without any prior
// configuration. See docs/sync-protocol.md for how this id was derived.
const ExtensionID = "ekeafblhmofnlglmkbpojmpojphcnidm"

// AutoOpenURL builds the URL for the extension's auto-sync page
// (extension/src/sync/Sync.tsx), which on load automatically connects to
// the local sync server on port, computes the diff, and either applies it
// or (if preview) only reports what it would do.
func AutoOpenURL(port int, preview bool) string {
	previewParam := "0"
	if preview {
		previewParam = "1"
	}
	return fmt.Sprintf("chrome-extension://%s/sync/index.html?port=%d&preview=%s", ExtensionID, port, previewParam)
}
