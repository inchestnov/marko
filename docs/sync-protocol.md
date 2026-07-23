# Sync Protocol Reference

Marko's CLI never talks to the browser directly — there is no native
messaging host. Instead, `marko sync` starts a short-lived, loopback-only
HTTP server that the Chrome extension polls. This document is the
user/integrator-facing reference for that protocol (implemented in
[`cli/sync/server.go`](../cli/sync/server.go) and
[`cli/sync/protocol.go`](../cli/sync/protocol.go), consumed by
[`extension/src/lib/api.ts`](../extension/src/lib/api.ts) and
[`extension/src/lib/types.ts`](../extension/src/lib/types.ts)), plus the
`marko export` static file format and its reduced-functionality import
fallback. For the diff/matching algorithm itself, see
[`docs/architecture.md`](./architecture.md) §7.

## Transport

- `marko sync` binds a plain HTTP (not HTTPS) server strictly to
  `127.0.0.1` — the listener is created with
  `net.Listen("tcp", "127.0.0.1:<port>")`, never a wildcard bind, so the
  server is unreachable from other machines on the network.
- Default port: `8765`. Override with `marko sync --port <int>`.
- No authentication token. This is a deliberate, documented limitation:
  loopback-only binding is considered sufficient given the data in
  transit (the user's own already-locally-readable bookmark titles and
  URLs) — see [`docs/architecture.md`](./architecture.md) §12.
- CORS: every response from `/plan`, `/diff`, and `/report` sets
  `Access-Control-Allow-Origin: *` (no `Access-Control-Allow-Credentials`).
  This is wider than a single fixed extension origin because unpacked/dev
  extension installs don't have a stable ID known to the CLI ahead of
  time; since the server binds only to loopback and holds no
  cookies/session state, the wildcard does not expose anything beyond
  what's already locally readable.

## Endpoints

### `GET /health`

Response `200 OK`:

```json
{ "status": "ok", "markoVersion": "1.0.0" }
```

Used by the extension popup's `ConnectionStatus` component to show
connected/disconnected before attempting to fetch a plan.

### `GET /plan`

Re-runs the full pipeline (parse -> resolve -> validate -> render) and
returns the **desired tree only** — the CLI has no way to read the
browser's actual state on its own, so this is never a diff.

Response `200 OK`:

```json
{
  "generatedAt": "2026-07-22T10:00:00Z",
  "markoVersion": "1.0.0",
  "desiredTree": {
    "roots": [
      { "kind": "folder", "name": "bar", "path": [], "children": [], "index": 0 },
      { "kind": "folder", "name": "other", "path": [], "children": [], "index": 0 }
    ]
  }
}
```

(`children` is abbreviated to `[]` above for readability; in practice it
holds the full nested folder/bookmark tree — see the worked example in
`marko-export.json` produced by `marko export`.)

Response `500 Internal Server Error` on validation/parse failure:

```json
{ "error": { "code": "E_MISSING_VARIABLE", "message": "required variable \"username\" not provided for template \"profile\"" } }
```

### `POST /diff`

Request body — the actual browser tree, as captured by the extension via
`chrome.bookmarks.getTree()` and converted to the same tree shape:

```json
{ "actualTree": { "roots": [ { "kind": "folder", "name": "bar", "path": [], "children": [], "index": 0 }, { "kind": "folder", "name": "other", "path": [], "children": [], "index": 0 } ] } }
```

The CLI re-renders the desired tree fresh on every call (so edits to
`marko.yaml` are picked up live without restarting `marko sync`), runs
the diff engine against it, and returns the resulting plan:

Response `200 OK`:

```json
{
  "generatedAt": "2026-07-22T10:00:05Z",
  "operations": [
    {
      "type": "CREATE",
      "targetPath": ["bar", "Work", "Kubernetes"],
      "kind": "folder",
      "name": "Kubernetes",
      "position": 0,
      "parentBrowserId": "42"
    }
  ]
}
```

`400 Bad Request` on a malformed or missing `actualTree` body; `500` on
a pipeline/validation error (same `{ "error": { "code", "message" } }`
shape as `/plan`).

Each `Operation` (mirrored 1:1 in
[`extension/src/lib/types.ts`](../extension/src/lib/types.ts)):

| Field | Type | Notes |
|---|---|---|
| `type` | `"CREATE"` \| `"UPDATE"` \| `"DELETE"` \| `"MOVE"` | |
| `targetPath` | string[] | Desired ancestor folder-name path, root first (e.g. `["bar", "Work", "Kubernetes"]`); used for `CREATE` (no `browserId` yet) and for display on every op. |
| `kind` | `"folder"` \| `"bookmark"` | |
| `name` | string | Desired final name. For `DELETE`, the about-to-be-removed node's current name. |
| `url` | string (bookmarks only) | Desired final URL. |
| `browserId` | string | The actual `chrome.bookmarks` node id this op targets. Empty for `CREATE`. |
| `parentBrowserId` | string | Id of the parent folder. Required for `CREATE`/`MOVE`. Empty means "one of the two native roots", disambiguated by `targetPath[0]` (`"bar"`/`"other"`). |
| `position` | number | Desired 0-based index among final siblings, set for `CREATE`/`MOVE`. |
| `changes` | string[] | `UPDATE` only: subset of `"name"`, `"url"`. |

**Applying the plan.** Operations are pre-ordered by the CLI so a single
forward pass is always safe: all `DELETE`s first, then `CREATE`s
(breadth-first, shallowest `targetPath` first, so a folder exists before
its children need it), then `MOVE`s, then `UPDATE`s. Because newly
created folders don't have a real Chrome id yet at plan-generation time,
the extension must thread real ids forward itself as it applies each op,
keyed by `targetPath`, substituting any placeholder `parentBrowserId` it
already resolved earlier in the same pass — this is exactly what
[`extension/src/lib/bookmarksApply.ts`](../extension/src/lib/bookmarksApply.ts)
does.

### `POST /report`

Request body — one entry per applied operation:

```json
{
  "results": [
    { "targetPath": ["bar", "Work", "Kubernetes"], "type": "CREATE", "status": "ok", "browserId": "137" },
    { "targetPath": ["bar", "Work", "Company Wiki"], "type": "UPDATE", "status": "error", "error": "chrome.bookmarks.update failed: ..." }
  ]
}
```

Response `200 OK`:

```json
{ "accepted": true, "okCount": 1, "errorCount": 1 }
```

`marko sync` prints a human-readable summary (`Sync complete: N ok, M
errors`) and exits: `0` if `errorCount == 0`, else `1`.

### `POST /shutdown` (optional)

No body required. Lets the extension tell the CLI's one-shot server it
can stop listening immediately after a successful `/report`, instead of
waiting out `marko sync`'s `--timeout` (default `5m`, `0` = no timeout).

## Sequence

```
marko sync                         (binds 127.0.0.1:8765, waits)
    |
    v
Extension popup -> "Connect"
    |
    v
GET  /health                       -> { status: "ok", ... }
GET  /plan                         -> desiredTree
chrome.bookmarks.getTree()         -> actualTree (local, in the extension)
POST /diff { actualTree }          -> operations[]
    |
    v
DiffView renders operations[] grouped by type; user clicks "Apply"
    |
    v
bookmarksApply.ts applies operations[] in array order via
chrome.bookmarks.create/update/remove/removeTree/move, threading
newly-created browserIds forward via a local targetPath -> browserId map
    |
    v
POST /report { results }           -> { accepted, okCount, errorCount }
    |
    v
marko sync prints summary, exits (0 if errorCount == 0, else 1)
```

## `marko export` file format

`marko export [--out <file>]` (default `marko-export.json`) runs the
same parse -> resolve -> validate -> render pipeline as `/plan`, but
writes the result to a static file instead of serving it — for
offline/no-running-CLI import:

```json
{
  "formatVersion": "1",
  "generatedAt": "2026-07-22T10:00:00Z",
  "markoVersion": "1.0.0",
  "desiredTree": { "roots": [] }
}
```

```
$ marko export --out snapshot.json
Wrote snapshot.json (25 nodes)
```

### Import-from-file: CREATE-only, skip-existing fallback

The extension's Options page ("Import from file") loads this JSON, runs
`chrome.bookmarks.getTree()` locally, and applies a **deliberately
reduced** fallback implemented in
[`extension/src/lib/importExport.ts`](../extension/src/lib/importExport.ts):
it walks `desiredTree` and, for each node without an exact `(kind, name
[, url])` match found via a recursive scan of the actual tree, creates
it (recursing into newly-created folders unconditionally, since they
can contain no pre-existing matches). **It never deletes or moves
anything** in this mode — there is no diff engine bundled into the
extension; that logic only exists in the Go CLI.

This means file-import is safe to run repeatedly (idempotent — matching
nodes are skipped, not recreated) but will not reconcile renames,
deletions, or reordering the way the live `marko sync` HTTP flow does.
Full `CREATE`/`UPDATE`/`DELETE`/`MOVE` diffing is only available via
`marko sync`, where the Go diff engine (`cli/diff/diff.go`) performs the
actual structural matching described in `docs/architecture.md` §7.

## `marko diff --actual <file>`

For CLI-only usage without a running extension session, `marko diff`
accepts a previously-captured actual-tree JSON (the same shape as
`desiredTree`/`actualTree` above — e.g. produced by the extension's
Options page "Export current browser state" button) via `--actual`:

```
$ marko diff --actual browser-state.json
CREATE  folder    bar/Work/Kubernetes
CREATE  bookmark  bar/Work/Kubernetes/Documentation
UPDATE  bookmark  bar/Work/Company Wiki  (url changed)
DELETE  folder    other/Old Project
```

Without `--actual`, `marko diff` fails with a message directing the user
to `marko sync` instead, since the CLI has no independent way to read
the browser. `--json` prints the raw `diff.Plan` JSON (same
`operations[]` shape as `POST /diff`'s response).

## See also

- [`docs/architecture.md`](./architecture.md) §7 (diff/matching
  algorithm), §8 (full protocol spec), §10 (extension architecture).
- [`docs/yaml-reference.md`](./yaml-reference.md), [`docs/templates.md`](./templates.md)
  for the `marko.yaml` side of the pipeline that produces `desiredTree`.
