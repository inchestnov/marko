import { beforeEach, describe, expect, it, vi } from "vitest";
import { applyOperations, chromeTreeToBookmarkTree } from "./bookmarksApply";
import type { Operation } from "./types";

// Minimal mock of the chrome.bookmarks API surface used by
// applyOperations. Each mutation method returns a resolved promise, mimicking
// the real chrome.bookmarks.* async API shape.
function installChromeMock() {
  const create = vi.fn(
    async (details: chrome.bookmarks.BookmarkCreateArg): Promise<chrome.bookmarks.BookmarkTreeNode> => {
      return {
        id: `new-${create.mock.calls.length}`,
        title: details.title ?? "",
        url: details.url,
        index: details.index,
        parentId: details.parentId,
      } as chrome.bookmarks.BookmarkTreeNode;
    }
  );
  const update = vi.fn(async () => ({}) as chrome.bookmarks.BookmarkTreeNode);
  const remove = vi.fn(async () => undefined);
  const removeTree = vi.fn(async () => undefined);
  const move = vi.fn(async () => ({}) as chrome.bookmarks.BookmarkTreeNode);

  (globalThis as any).chrome = {
    bookmarks: { create, update, remove, removeTree, move },
  };

  return { create, update, remove, removeTree, move };
}

describe("applyOperations", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it("threads a real created parent id into a CREATE whose parent is itself a just-created placeholder", async () => {
    const mocks = installChromeMock();

    const operations: Operation[] = [
      {
        type: "CREATE",
        targetPath: ["bar", "Work"],
        kind: "folder",
        name: "Work",
        position: 0,
        parentBrowserId: "", // placeholder: parent is native root "bar"
      },
      {
        type: "CREATE",
        targetPath: ["bar", "Work", "Kubernetes"],
        kind: "folder",
        name: "Kubernetes",
        position: 0,
        parentBrowserId: "PLACEHOLDER_NOT_YET_CREATED",
      },
    ];

    const results = await applyOperations(operations);

    expect(results).toHaveLength(2);
    expect(results[0].status).toBe("ok");
    expect(results[1].status).toBe("ok");

    // First CREATE: parent is the "bar" root itself -> real Chrome id "1".
    expect(mocks.create).toHaveBeenNthCalledWith(
      1,
      expect.objectContaining({ parentId: "1", title: "Work", index: 0 })
    );

    // Second CREATE: parent is the just-created "Work" folder -> its real
    // Chrome-assigned id ("new-1") must be threaded forward, not the
    // placeholder id the CLI supplied.
    expect(mocks.create).toHaveBeenNthCalledWith(
      2,
      expect.objectContaining({ parentId: "new-1", title: "Kubernetes", index: 0 })
    );
  });

  it("produces a full result array with mixed ok/error statuses on partial failure, without aborting the batch", async () => {
    const mocks = installChromeMock();
    mocks.update.mockRejectedValueOnce(new Error("chrome.bookmarks.update failed: boom"));

    const operations: Operation[] = [
      {
        type: "CREATE",
        targetPath: ["bar", "Work"],
        kind: "folder",
        name: "Work",
        position: 0,
      },
      {
        type: "UPDATE",
        targetPath: ["bar", "Company Wiki"],
        kind: "bookmark",
        name: "Company Wiki",
        url: "https://wiki.company.com",
        browserId: "existing-1",
        changes: ["url"],
        position: 0,
      },
      {
        type: "CREATE",
        targetPath: ["bar", "Other"],
        kind: "folder",
        name: "Other",
        position: 1,
      },
    ];

    const results = await applyOperations(operations);

    expect(results).toHaveLength(3);
    expect(results[0].status).toBe("ok");
    expect(results[1].status).toBe("error");
    expect(results[1].error).toContain("boom");
    // The batch must continue past the failed operation.
    expect(results[2].status).toBe("ok");
    expect(mocks.create).toHaveBeenCalledTimes(2);
  });

  it("calls removeTree for a DELETE of a folder and remove for a DELETE of a bookmark", async () => {
    const mocks = installChromeMock();

    const operations: Operation[] = [
      {
        type: "DELETE",
        targetPath: ["other", "Old Project"],
        kind: "folder",
        name: "Old Project",
        browserId: "folder-99",
        position: 0,
      },
      {
        type: "DELETE",
        targetPath: ["other", "Stale Link"],
        kind: "bookmark",
        name: "Stale Link",
        url: "https://example.com/old",
        browserId: "bookmark-42",
        position: 0,
      },
    ];

    const results = await applyOperations(operations);

    expect(results).toHaveLength(2);
    expect(results.every((r) => r.status === "ok")).toBe(true);

    expect(mocks.removeTree).toHaveBeenCalledWith("folder-99");
    expect(mocks.remove).toHaveBeenCalledWith("bookmark-42");
    expect(mocks.removeTree).not.toHaveBeenCalledWith("bookmark-42");
    expect(mocks.remove).not.toHaveBeenCalledWith("folder-99");
  });

  it("resolves parentBrowserId under the 'other' root to Chrome's native id '2'", async () => {
    const mocks = installChromeMock();

    const operations: Operation[] = [
      {
        type: "CREATE",
        targetPath: ["other", "Personal"],
        kind: "folder",
        name: "Personal",
        position: 0,
      },
    ];

    await applyOperations(operations);

    expect(mocks.create).toHaveBeenCalledWith(
      expect.objectContaining({ parentId: "2", title: "Personal" })
    );
  });

  it("moves a bookmark using its existing browserId and resolved parent", async () => {
    const mocks = installChromeMock();

    const operations: Operation[] = [
      {
        type: "MOVE",
        targetPath: ["bar", "Work", "Company Wiki"],
        kind: "bookmark",
        name: "Company Wiki",
        url: "https://wiki.company.com",
        browserId: "bookmark-7",
        parentBrowserId: "folder-existing",
        position: 2,
      },
    ];

    const results = await applyOperations(operations);

    expect(results[0].status).toBe("ok");
    expect(mocks.move).toHaveBeenCalledWith("bookmark-7", {
      parentId: "folder-existing",
      index: 2,
    });
  });

  it("applies an UPDATE with both name and url changes in a single chrome.bookmarks.update call", async () => {
    const mocks = installChromeMock();

    const operations: Operation[] = [
      {
        type: "UPDATE",
        targetPath: ["bar", "Work", "Company Wiki"],
        kind: "bookmark",
        name: "Company Wiki (New)",
        url: "https://wiki.new.company.com",
        browserId: "bookmark-7",
        changes: ["name", "url"],
        position: 0,
      },
    ];

    const results = await applyOperations(operations);

    expect(results[0].status).toBe("ok");
    expect(mocks.update).toHaveBeenCalledWith("bookmark-7", {
      title: "Company Wiki (New)",
      url: "https://wiki.new.company.com",
    });
  });

  it("applies an UPDATE renaming a folder without touching url (folders never carry a url change)", async () => {
    const mocks = installChromeMock();

    const operations: Operation[] = [
      {
        type: "UPDATE",
        targetPath: ["bar", "Work", "Kubernetes Renamed"],
        kind: "folder",
        name: "Kubernetes Renamed",
        browserId: "folder-55",
        changes: ["name"],
        position: 0,
      },
    ];

    const results = await applyOperations(operations);

    expect(results[0].status).toBe("ok");
    expect(mocks.update).toHaveBeenCalledWith("folder-55", {
      title: "Kubernetes Renamed",
    });
  });
});

describe("chromeTreeToBookmarkTree", () => {
  it("converts an empty browser (no children under bar/other) into a two-root tree with no children", () => {
    const chromeRoots: chrome.bookmarks.BookmarkTreeNode[] = [
      {
        id: "0",
        title: "",
        children: [
          { id: "1", title: "Bookmarks Bar", children: [] },
          { id: "2", title: "Other Bookmarks", children: [] },
        ],
      } as chrome.bookmarks.BookmarkTreeNode,
    ];

    const tree = chromeTreeToBookmarkTree(chromeRoots);

    expect(tree.roots).toHaveLength(2);
    const bar = tree.roots.find((r) => r.name === "bar");
    const other = tree.roots.find((r) => r.name === "other");
    expect(bar).toBeDefined();
    expect(other).toBeDefined();
    expect(bar?.children ?? []).toHaveLength(0);
    expect(other?.children ?? []).toHaveLength(0);
    expect(bar?.path).toEqual([]);
    expect(bar?.browserId).toBe("1");
    expect(other?.browserId).toBe("2");
  });

  it("converts deeply nested folders, computing path and browserId at every level and skipping unmanaged roots (e.g. Mobile Bookmarks id 3)", () => {
    const chromeRoots: chrome.bookmarks.BookmarkTreeNode[] = [
      {
        id: "0",
        title: "",
        children: [
          {
            id: "1",
            title: "Bookmarks Bar",
            children: [
              {
                id: "10",
                title: "Work",
                children: [
                  {
                    id: "11",
                    title: "Kubernetes",
                    children: [
                      {
                        id: "12",
                        title: "Documentation",
                        url: "https://kubernetes.io/docs/",
                      },
                    ],
                  },
                ],
              },
            ],
          },
          { id: "2", title: "Other Bookmarks", children: [] },
          {
            id: "3",
            title: "Mobile Bookmarks",
            children: [{ id: "99", title: "Should be ignored", url: "https://example.com" }],
          },
        ],
      } as unknown as chrome.bookmarks.BookmarkTreeNode,
    ];

    const tree = chromeTreeToBookmarkTree(chromeRoots);

    // Only bar/other are managed; Mobile Bookmarks (id "3") is skipped.
    expect(tree.roots).toHaveLength(2);

    const bar = tree.roots.find((r) => r.name === "bar")!;
    const work = bar.children![0];
    expect(work.kind).toBe("folder");
    expect(work.name).toBe("Work");
    expect(work.path).toEqual(["bar"]);
    expect(work.browserId).toBe("10");

    const kubernetes = work.children![0];
    expect(kubernetes.name).toBe("Kubernetes");
    expect(kubernetes.path).toEqual(["bar", "Work"]);
    expect(kubernetes.browserId).toBe("11");

    const doc = kubernetes.children![0];
    expect(doc.kind).toBe("bookmark");
    expect(doc.name).toBe("Documentation");
    expect(doc.url).toBe("https://kubernetes.io/docs/");
    expect(doc.path).toEqual(["bar", "Work", "Kubernetes"]);
    expect(doc.browserId).toBe("12");
    expect(doc.children).toBeUndefined();
  });

  it("always orders bar before other regardless of Chrome's internal top-level child order", () => {
    const chromeRoots: chrome.bookmarks.BookmarkTreeNode[] = [
      {
        id: "0",
        title: "",
        children: [
          { id: "2", title: "Other Bookmarks", children: [] },
          { id: "1", title: "Bookmarks Bar", children: [] },
        ],
      } as chrome.bookmarks.BookmarkTreeNode,
    ];

    const tree = chromeTreeToBookmarkTree(chromeRoots);

    expect(tree.roots.map((r) => r.name)).toEqual(["bar", "other"]);
  });
});
