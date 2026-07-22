// Applies a static export JSON file (marko export output) using the
// conservative CREATE-only, skip-existing fallback described in
// docs/architecture.md §8.4: it walks desiredTree and, for each node
// without an exact (kind, name[, url]) match found via a local recursive
// scan of actualTree, creates it. It never deletes or moves in this mode.

import type { BookmarkTree, Node } from "./types";

const ROOT_NAME_BAR = "bar";
const ROOT_NAME_OTHER = "other";
const CHROME_ROOT_BAR_ID = "1";
const CHROME_ROOT_OTHER_ID = "2";

/** True if `candidate` is an exact (kind, name[, url]) match for `desired`. */
function isExactMatch(desired: Node, candidate: Node): boolean {
  if (desired.kind !== candidate.kind) return false;
  if (desired.name !== candidate.name) return false;
  if (desired.kind === "bookmark" && desired.url !== candidate.url) return false;
  return true;
}

/** Recursively scans `subtree` (and its descendants) for an exact match. */
function findMatch(desired: Node, subtree: Node): Node | undefined {
  if (isExactMatch(desired, subtree)) return subtree;
  for (const child of subtree.children ?? []) {
    const found = findMatch(desired, child);
    if (found) return found;
  }
  return undefined;
}

function findMatchInTree(desired: Node, actualTree: BookmarkTree): Node | undefined {
  for (const root of actualTree.roots) {
    const found = findMatch(desired, root);
    if (found) return found;
  }
  return undefined;
}

/** Resolves the real Chrome browserId for a desired root's actual parent. */
function rootBrowserId(rootName: string, actualTree: BookmarkTree): string | undefined {
  const root = actualTree.roots.find((r) => r.name === rootName);
  if (root?.browserId) return root.browserId;
  if (rootName === ROOT_NAME_BAR) return CHROME_ROOT_BAR_ID;
  if (rootName === ROOT_NAME_OTHER) return CHROME_ROOT_OTHER_ID;
  return undefined;
}

/**
 * Walks `desiredTree` (from a parsed ExportFile) and creates, via
 * chrome.bookmarks.create, any node that has no exact (kind, name[, url])
 * match anywhere in `actualTree`. Never deletes or moves anything. Returns
 * the number of nodes created.
 *
 * `actualTree` should be produced by the same conversion helper used
 * elsewhere (chromeTreeToBookmarkTree in bookmarksApply.ts), so browserIds
 * and root names ("bar"/"other") line up.
 */
export async function importFromExportFile(
  desiredTree: BookmarkTree,
  actualTree: BookmarkTree
): Promise<number> {
  let createdCount = 0;

  for (const desiredRoot of desiredTree.roots) {
    const parentBrowserId = rootBrowserId(desiredRoot.name, actualTree);
    createdCount += await walkAndCreate(
      desiredRoot.children ?? [],
      parentBrowserId,
      actualTree
    );
  }

  return createdCount;
}

/**
 * Recursively walks `desiredChildren`, creating any node (and, for
 * newly-created folders, all of its children unconditionally, since a
 * freshly created folder can contain no pre-existing matches) that isn't
 * already present in `actualTree`. Returns the count of created nodes.
 */
async function walkAndCreate(
  desiredChildren: Node[],
  parentBrowserId: string | undefined,
  actualTree: BookmarkTree
): Promise<number> {
  let createdCount = 0;

  for (const desired of desiredChildren) {
    const match = findMatchInTree(desired, actualTree);

    if (match) {
      // Already exists — recurse into children to fill in anything missing
      // inside this existing folder (skip-existing, not skip-existing-subtree).
      if (desired.kind === "folder") {
        createdCount += await walkAndCreate(
          desired.children ?? [],
          match.browserId,
          actualTree
        );
      }
      continue;
    }

    const created = await chrome.bookmarks.create({
      parentId: parentBrowserId,
      title: desired.name,
      url: desired.kind === "bookmark" ? desired.url : undefined,
    });
    createdCount += 1;

    if (desired.kind === "folder" && desired.children?.length) {
      createdCount += await walkAndCreate(desired.children, created.id, actualTree);
    }
  }

  return createdCount;
}
