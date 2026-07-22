// Applies a static export JSON file (marko export output) using the
// conservative CREATE-only, skip-existing fallback described in
// docs/architecture.md §8.4.
//
// TODO(extension-agent): implement file-import fallback logic.

import type { BookmarkTree } from "./types";

export async function importFromExportFile(_desiredTree: BookmarkTree): Promise<void> {
  throw new Error("not implemented");
}
