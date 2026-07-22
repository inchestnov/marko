// Hand-maintained TypeScript mirrors of the Go JSON structs in
// docs/architecture.md §6-§8 (BookmarkTree, Node, Operation, Plan,
// request/response shapes). Keep in lockstep with cli/internal/bookmarktree
// and cli/sync/protocol.go — no codegen in v1 (see architecture.md §12).
//
// TODO(extension-agent): fill in full field sets as the Go structs solidify.

export type NodeKind = "folder" | "bookmark";

export interface BookmarkNode {
  kind: NodeKind;
  name: string;
  url?: string;
  path: string[];
  children?: BookmarkNode[];
  browserId?: string;
  index: number;
}

export interface BookmarkTree {
  roots: BookmarkNode[];
}

export type OpType = "CREATE" | "UPDATE" | "DELETE" | "MOVE";

export interface Operation {
  type: OpType;
  targetPath: string[];
  kind: NodeKind;
  name: string;
  url?: string;
  browserId?: string;
  parentBrowserId?: string;
  position: number;
  changes?: string[];
}

export interface Plan {
  generatedAt: string;
  operations: Operation[];
}
