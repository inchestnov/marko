// The only module allowed to call chrome.bookmarks.* mutation methods.
// Converts chrome.bookmarks.getTree() output into the BookmarkTree shape
// from docs/architecture.md §6, and executes Operations sequentially via
// chrome.bookmarks.create/update/remove/removeTree/move, maintaining a
// targetPath -> browserId map for newly created nodes (§8.3).

import type { BookmarkTree, Node, Operation, OperationResult } from "./types";

// Chrome's native top-level bookmark folder ids. Marko only manages these
// two roots (see §6, §7.1).
const CHROME_ROOT_BAR_ID = "1";
const CHROME_ROOT_OTHER_ID = "2";

const ROOT_NAME_BAR = "bar";
const ROOT_NAME_OTHER = "other";

/**
 * Converts the result of chrome.bookmarks.getTree() into the BookmarkTree
 * JSON shape described in architecture.md §6: Chrome's native root ids
 * ("1"/"2") are remapped to the logical root names "bar"/"other", `path` is
 * computed for every descendant, and nodes with a `url` are treated as
 * bookmarks while all others are folders.
 */
export function chromeTreeToBookmarkTree(
  chromeRoots: chrome.bookmarks.BookmarkTreeNode[]
): BookmarkTree {
  const roots: Node[] = [];

  // chrome.bookmarks.getTree() returns a single synthetic root node whose
  // children are "Bookmarks Bar" (id "1"), "Other Bookmarks" (id "2"), and
  // (on some platforms) "Mobile Bookmarks". We only manage bar/other.
  const topLevel =
    chromeRoots.length === 1 && chromeRoots[0].children
      ? chromeRoots[0].children
      : chromeRoots;

  for (const child of topLevel) {
    let rootName: string | undefined;
    if (child.id === CHROME_ROOT_BAR_ID) {
      rootName = ROOT_NAME_BAR;
    } else if (child.id === CHROME_ROOT_OTHER_ID) {
      rootName = ROOT_NAME_OTHER;
    } else {
      // Skip roots Marko doesn't manage (e.g. Mobile Bookmarks, id "3").
      continue;
    }

    const rootNode: Node = {
      kind: "folder",
      name: rootName,
      path: [],
      browserId: child.id,
      index: rootName === ROOT_NAME_BAR ? 0 : 1,
      children: (child.children ?? []).map((grandchild, idx) =>
        convertChromeNode(grandchild, [rootName!], idx)
      ),
    };
    roots.push(rootNode);
  }

  // Ensure deterministic bar-then-other ordering regardless of Chrome's
  // internal ordering of the synthetic top-level children.
  roots.sort((a, b) => (a.name === ROOT_NAME_BAR ? -1 : 1) - (b.name === ROOT_NAME_BAR ? -1 : 1));

  return { roots };
}

function convertChromeNode(
  node: chrome.bookmarks.BookmarkTreeNode,
  parentPath: string[],
  index: number
): Node {
  const isBookmark = typeof node.url === "string";
  const result: Node = {
    kind: isBookmark ? "bookmark" : "folder",
    name: node.title,
    path: parentPath,
    browserId: node.id,
    index,
  };
  if (isBookmark) {
    result.url = node.url;
  } else {
    const childPath = [...parentPath, node.title];
    result.children = (node.children ?? []).map((child, idx) =>
      convertChromeNode(child, childPath, idx)
    );
  }
  return result;
}

/** Joins a targetPath into a stable map key, e.g. ["bar","Work"] -> "bar/Work". */
function pathKey(path: string[]): string {
  return path.join("/");
}

/**
 * Resolves a parentBrowserId for an operation, substituting the real
 * Chrome-assigned id for any placeholder that refers to a node created
 * earlier in this same apply pass (threaded forward via `createdIds`,
 * keyed by joined targetPath — see §8.3). Root path prefixes ("bar"/
 * "other") are mapped back to Chrome's real root ids ("1"/"2").
 */
function resolveParentBrowserId(
  op: Operation,
  createdIds: Map<string, string>
): string | undefined {
  // The parent's path is the operation's targetPath minus its own last
  // segment (the node's own name).
  const parentPath = op.targetPath.slice(0, -1);

  if (parentPath.length === 0) {
    // No parent segments: the node is itself a root ("bar"/"other"), which
    // Marko never creates/moves. Fall back to whatever the op specified.
    return op.parentBrowserId;
  }

  if (parentPath.length === 1) {
    // Direct child of a native root.
    if (parentPath[0] === ROOT_NAME_BAR) return CHROME_ROOT_BAR_ID;
    if (parentPath[0] === ROOT_NAME_OTHER) return CHROME_ROOT_OTHER_ID;
  }

  const known = createdIds.get(pathKey(parentPath));
  if (known) {
    return known;
  }

  // Not a newly created placeholder we've seen — trust the id the CLI
  // supplied (it refers to a pre-existing browser node).
  return op.parentBrowserId;
}

/**
 * Executes operations against chrome.bookmarks in array order (the CLI
 * already ordered them DELETE -> CREATE -> MOVE -> UPDATE per §7.3).
 * Maintains a local targetPath -> browserId map so that CREATEs of
 * not-yet-created parents have their real ids threaded forward into
 * subsequent operations' parentBrowserId. Per-operation failures are
 * caught and recorded rather than aborting the whole batch.
 */
export async function applyOperations(
  operations: Operation[]
): Promise<OperationResult[]> {
  const createdIds = new Map<string, string>();
  const results: OperationResult[] = [];

  for (const op of operations) {
    try {
      const browserId = await applyOne(op, createdIds);
      results.push({
        targetPath: op.targetPath,
        type: op.type,
        status: "ok",
        ...(browserId ? { browserId } : {}),
      });
    } catch (err) {
      results.push({
        targetPath: op.targetPath,
        type: op.type,
        status: "error",
        error: err instanceof Error ? err.message : String(err),
      });
    }
  }

  return results;
}

/**
 * Preview/dry-run counterpart to applyOperations: reports what each
 * operation *would* do, in the same OperationResult shape, without
 * calling any chrome.bookmarks mutation method. Used by the auto-sync
 * page's preview mode so the CLI's log shows a realistic plan without
 * touching the browser.
 */
export function planOperations(operations: Operation[]): OperationResult[] {
  return operations.map((op) => ({
    targetPath: op.targetPath,
    type: op.type,
    status: "planned",
    ...(op.type !== "CREATE" && op.browserId ? { browserId: op.browserId } : {}),
  }));
}

/** Applies a single operation, returning the relevant browserId (if any). */
async function applyOne(
  op: Operation,
  createdIds: Map<string, string>
): Promise<string | undefined> {
  switch (op.type) {
    case "DELETE": {
      if (!op.browserId) {
        throw new Error("DELETE operation missing browserId");
      }
      if (op.kind === "folder") {
        await chrome.bookmarks.removeTree(op.browserId);
      } else {
        await chrome.bookmarks.remove(op.browserId);
      }
      return op.browserId;
    }

    case "CREATE": {
      const parentId = resolveParentBrowserId(op, createdIds);
      const created = await chrome.bookmarks.create({
        parentId,
        title: op.name,
        url: op.kind === "bookmark" ? op.url : undefined,
        index: op.position,
      });
      createdIds.set(pathKey(op.targetPath), created.id);
      return created.id;
    }

    case "MOVE": {
      if (!op.browserId) {
        throw new Error("MOVE operation missing browserId");
      }
      const parentId = resolveParentBrowserId(op, createdIds);
      await chrome.bookmarks.move(op.browserId, {
        parentId,
        index: op.position,
      });
      return op.browserId;
    }

    case "UPDATE": {
      if (!op.browserId) {
        throw new Error("UPDATE operation missing browserId");
      }
      const changes: chrome.bookmarks.BookmarkChangesArg = {};
      if (!op.changes || op.changes.includes("name")) {
        changes.title = op.name;
      }
      if (op.kind === "bookmark" && (!op.changes || op.changes.includes("url"))) {
        changes.url = op.url;
      }
      await chrome.bookmarks.update(op.browserId, changes);
      return op.browserId;
    }

    default: {
      const exhaustive: never = op.type;
      throw new Error(`Unknown operation type: ${exhaustive}`);
    }
  }
}
