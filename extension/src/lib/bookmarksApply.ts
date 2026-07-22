// The only module allowed to call chrome.bookmarks.* mutation methods.
// Executes operations sequentially via chrome.bookmarks.create/update/
// remove/removeTree/move, maintaining a TargetPath -> browserId map for
// newly created nodes. See docs/architecture.md §8.3, §10.4.
//
// TODO(extension-agent): implement operation application semantics.

import type { Operation } from "./types";

export async function applyOperations(_operations: Operation[]): Promise<void> {
  throw new Error("not implemented");
}
