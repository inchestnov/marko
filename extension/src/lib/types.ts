// Hand-maintained TypeScript mirrors of the Go JSON structs in
// docs/architecture.md §6-§8 (BookmarkTree, Node, Operation, Plan,
// request/response shapes). Keep in lockstep with cli/internal/bookmarktree
// and cli/sync/protocol.go — no codegen in v1 (see architecture.md §12).

// ---------------------------------------------------------------------------
// §6 Render Engine — cli/internal/bookmarktree/tree.go
// ---------------------------------------------------------------------------

export type NodeKind = "folder" | "bookmark";

// Node mirrors bookmarktree.Node. Bookmarks never have `children`; folders
// never have `url` set.
export interface Node {
  kind: NodeKind;
  name: string;
  /** Only set for kind === "bookmark". */
  url?: string;
  /**
   * Ordered list of ancestor folder names from the applicable root down to
   * (but excluding) this node itself, e.g. ["bar", "Work", "Kubernetes"].
   * path[0] is always "bar" or "other".
   */
  path: string[];
  children?: Node[];
  /**
   * Populated only on Browser State trees (never on Desired State trees).
   * Empty string / undefined means "not yet created".
   */
  browserId?: string;
  /** 0-based position among siblings. */
  index: number;
}

// BookmarkTree mirrors bookmarktree.BookmarkTree. Roots holds exactly two
// entries with name "bar" and "other".
export interface BookmarkTree {
  roots: Node[];
}

// ---------------------------------------------------------------------------
// §7 Diff Engine — cli/diff/diff.go
// ---------------------------------------------------------------------------

export type OpType = "CREATE" | "UPDATE" | "DELETE" | "MOVE";

// Operation mirrors diff.Operation: one atomic, idempotent action for the
// extension to apply via chrome.bookmarks.
export interface Operation {
  type: OpType;

  /**
   * Desired ancestor path (folder names only, root first) of the node being
   * acted on — used for CREATE (no browserId exists yet) and for
   * logging/human display on every op.
   */
  targetPath: string[];

  /** "folder" or "bookmark" (mirrors NodeKind). */
  kind: NodeKind;

  /** Desired final values. For DELETE, reflects the actual (about-to-be-removed) node. */
  name: string;
  url?: string;

  /** The actual chrome.bookmarks node id this op targets. Empty for CREATE. */
  browserId?: string;

  /**
   * Id of the parent folder this node should end up under. For CREATE/MOVE,
   * required. Empty means "one of the two native roots", disambiguated by
   * targetPath[0] ("bar"/"other").
   */
  parentBrowserId?: string;

  /**
   * Desired 0-based index among new siblings, set for CREATE and MOVE so the
   * extension can call chrome.bookmarks.move / .create with an explicit
   * `index`.
   */
  position: number;

  /** For UPDATE only: subset of "name", "url". */
  changes?: string[];
}

// Plan mirrors diff.Plan.
export interface Plan {
  /** RFC3339 timestamp. */
  generatedAt: string;
  operations: Operation[];
}

// ---------------------------------------------------------------------------
// §8.2 Sync protocol — wire envelopes for each endpoint
// ---------------------------------------------------------------------------

/** GET /health response. */
export interface HealthResponse {
  status: string;
  markoVersion: string;
}

/** GET /plan response. */
export interface PlanResponse {
  generatedAt: string;
  markoVersion: string;
  desiredTree: BookmarkTree;
}

/** POST /diff request body. */
export interface DiffRequest {
  actualTree: BookmarkTree;
}

/** POST /diff response body (same shape as Plan). */
export interface DiffResponse {
  generatedAt: string;
  operations: Operation[];
}

/** A single per-operation apply result, as sent in POST /report's `results`. */
export interface OperationResult {
  targetPath: string[];
  type: OpType;
  status: "ok" | "error";
  /** Present when status === "ok" and the op created/targeted a node. */
  browserId?: string;
  /** Present when status === "error". */
  error?: string;
}

/** POST /report request body. */
export interface ReportRequest {
  results: OperationResult[];
}

/** POST /report response body. */
export interface ReportResponse {
  accepted: boolean;
  okCount: number;
  errorCount: number;
}

/** Shared error envelope returned by any endpoint on failure. */
export interface ErrorEnvelope {
  error: {
    code: string;
    message: string;
  };
}

// ---------------------------------------------------------------------------
// §8.4 `marko export` JSON file format
// ---------------------------------------------------------------------------

export interface ExportFile {
  formatVersion: string;
  generatedAt: string;
  markoVersion: string;
  desiredTree: BookmarkTree;
}
