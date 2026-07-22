import { beforeEach, describe, expect, it, vi } from "vitest";
import { applyOperations } from "./bookmarksApply";
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
});
