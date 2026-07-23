# Sync Protocol Reference

`marko sync` has two ways of reaching the browser, selected with
`--bridge`:

- **`--bridge=file` (default).** Marko reads and writes the target
  browser's native `Bookmarks` file directly — no extension, no HTTP
  server, no CORS. See [File bridge](#file-bridge-default) below.
- **`--bridge=http` (legacy).** A short-lived, loopback-only HTTP server
  that a Chrome extension polls/auto-opens. See
  [HTTP + extension bridge](#http--extension-bridge-legacy---bridgehttp)
  below.

The file bridge is the recommended, default path: it was added after
real-world testing of the HTTP+extension bridge surfaced two problems —
a missing CORS-preflight (`OPTIONS`) handler that made `POST /diff` and
`POST /report` silently hang from the browser's perspective (fixed, but
illustrative of how much surface area a browser-extension bridge has),
and a hardcoded assumption that the "Other Bookmarks" root always has id
`"2"`, which turned out to be false on a real profile (its actual id was
`"3"`). Reading/writing the Bookmarks file sidesteps both categories of
problem entirely by not going through the browser's extension APIs at
all.

## File bridge (default)

Implemented in [`cli/browserfile`](../cli/browserfile). Marko:

1. Locates the browser's `Bookmarks` file: `--bookmarks-file <path>` if
   given, else `--browser <name>` (`brave` by default — see
   `browserfile.KnownBrowsers` for the full list: `brave`, `chrome`,
   `chromium`, `edge`) + `--profile <name>` (`Default` by default),
   resolved to the OS-appropriate path (e.g. on macOS,
   `~/Library/Application Support/BraveSoftware/Brave-Browser/Default/Bookmarks`
   for Brave).
2. Refuses to proceed if the browser looks like it's currently running
   for that profile (Chromium's `SingletonLock` file/symlink is present
   next to the profile directory) — unless `--force` is passed. **The
   browser must be closed** while `--bridge=file` runs: Chromium
   periodically flushes its in-memory bookmark model back to this file,
   which would silently overwrite Marko's changes if the browser were
   left open.
3. Parses the file (kept as a fully generic `map[string]interface{}` tree
   internally, so any field Marko doesn't explicitly model — Brave's
   `meta_info`, `date_last_used`, the entire `synced`/mobile-bookmarks
   root, future additions — survives a read-modify-write round trip
   completely untouched) and converts the `bookmark_bar`/`other` roots
   into a `bookmarktree.BookmarkTree`, reading each root's real `id`
   directly from the file rather than assuming `"1"`/`"2"` (see above).
4. Runs it through the exact same `diff.Diff` engine as every other
   Marko command (§7) — the diff engine's contract is just "two
   `BookmarkTree` values in, a `Plan` out," so it doesn't care whether
   "actual" came from a browser extension or a parsed file.
5. Unless `--preview`, applies the plan directly to the in-memory
   parsed structure (assigning fresh sequential ids, random v4 GUIDs, and
   WebKit-epoch timestamps to new nodes, threading not-yet-existing
   parent ids forward within the same pass exactly like
   `extension/src/lib/bookmarksApply.ts`'s `resolveParentBrowserId` does
   for the HTTP bridge), backs up the original file's current content to
   `<path>.marko-backup-<unix-timestamp>`, and writes the result back
   atomically (temp file + rename).
6. The full plan is always printed before anything is written — this is
   "what was imported, what was deleted," not just a summary count, and
   it's printed whether or not `--preview` is set.

```
$ marko sync --config marko.yaml
Bookmarks file: /Users/you/Library/Application Support/BraveSoftware/Brave-Browser/Default/Bookmarks

Computed plan (3 operation(s)):
CREATE  folder    other/Work/Kubernetes
CREATE  bookmark  other/Work/Kubernetes/Documentation
UPDATE  bookmark  other/Work/Company Wiki  (url changed)

Wrote 3 operation(s) to .../Bookmarks (a backup of the previous content was saved alongside it).
Restart the browser (if it was already closed, just open it) to see the change.
```

`marko sync --preview --config marko.yaml` runs the exact same steps 1-4
and prints the plan, but stops before step 5 — nothing is written.

### On the `checksum` field

Chromium's `Bookmarks` file carries a `"checksum"` field that the browser
recomputes from an MD5 over each node's id/name(/url). Marko recomputes
its own best-effort approximation of that algorithm on every write, but
this was confirmed during development to not be strictly
enforced/validated by Chromium-family browsers on load (an exact
algorithmic match is not required for the file to be accepted) — it
exists mainly so the file doesn't carry an obviously-stale value, not
because getting it byte-for-byte identical to Chromium's own computation
is load-bearing. The automatic backup (step 5 above) is the actual
safety net if anything about a given browser version's handling of this
file ever turns out to be pickier than observed.

## HTTP + extension bridge (legacy, `--bridge=http`)

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
  `Access-Control-Allow-Origin: chrome-extension://<ExtensionID>` (no
  `Access-Control-Allow-Credentials`). This works because the extension's
  id is pinned to a fixed value via the `"key"` field in
  `extension/chrome/manifest.json` (see `ExtensionID` in
  [`cli/sync/autoopen.go`](../cli/sync/autoopen.go)) — unlike a typical
  unpacked/dev install, which gets a random id per machine, this
  extension always loads with the same id, so the CLI can name it
  exactly instead of falling back to a wildcard origin.

## Auto-sync: how `marko sync` reaches into the browser

Chrome only exposes `chrome.bookmarks` to code running inside an
extension — no external process can call it directly, and Native
Messaging hosts can only be *launched by* the browser, never the reverse,
so a CLI process cannot reach into an already-running Chrome on its own
initiative either way. To still make `marko sync` feel like a single
command that "just imports," it does the next best thing:

1. `marko sync` starts the local HTTP server described below.
2. By default (`--auto-open`, on unless passed `--auto-open=false`) it
   shells out to the OS's default URL handler (`open` / `xdg-open` /
   `rundll32 url.dll,FileProtocolHandler`) to open
   `chrome-extension://<ExtensionID>/sync/index.html?port=<port>&preview=<0|1>`
   — a dedicated extension page (`extension/src/sync/Sync.tsx`), distinct
   from the popup, whose whole purpose is to run the sync flow
   automatically the moment it loads, with no button clicks.
3. That page's `useEffect` on mount: `GET /plan` -> `desiredTree`,
   `chrome.bookmarks.getTree()` (local) -> `actualTree`, `POST /diff` ->
   `operations[]`. If `preview=1` it calls `planOperations()` (no
   `chrome.bookmarks` calls at all); otherwise it calls
   `applyOperations()`, which does the real `chrome.bookmarks.*` calls.
   Either way it then `POST /report`s the results (see below) and shows a
   final summary on the page — no auto-close (Chrome blocks
   `window.close()` on tabs it opened itself, not a script), so the page
   just tells the user they can close the tab.
4. `marko sync` prints the full plan (via `Server.OnDiff`) and the full
   per-operation report (via `Server.OnReport`) to stdout as they arrive,
   then exits as soon as the report is received — it does not wait out
   the full `--timeout` once the browser has responded.

This means the CLI-visible lifecycle is bounded to one sync cycle: it
opens the browser, logs exactly what it found and what happened to it,
and exits — never hangs indefinitely (the `--timeout`, default `5m`, is
only a safety net for the case where the browser/extension never
responds at all, e.g. the extension isn't installed).

### `--preview` (dry run)

`marko sync --preview` runs the exact same flow, but the auto-sync page
never calls any `chrome.bookmarks` mutation method — it uses
`planOperations()` instead of `applyOperations()`, which reports every
operation with `status: "planned"` and `preview: true` on the `/report`
body. `marko sync` recognizes this and prints "Preview complete. No
changes were made to Chrome." with exit code `0` regardless of the plan
size, instead of the normal ok/error summary.

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

Request body — one entry per operation, plus a top-level `preview` flag:

```json
{
  "preview": false,
  "results": [
    { "targetPath": ["bar", "Work", "Kubernetes"], "type": "CREATE", "status": "ok", "browserId": "137" },
    { "targetPath": ["bar", "Work", "Company Wiki"], "type": "UPDATE", "status": "error", "error": "chrome.bookmarks.update failed: ..." }
  ]
}
```

`status` is `"ok"` / `"error"` for a real apply, or `"planned"` for every
entry when `preview: true` (see `--preview` above) — the operation was
computed but no `chrome.bookmarks` call was made for it.

Response `200 OK`:

```json
{ "accepted": true, "okCount": 1, "errorCount": 1 }
```

(`okCount`/`errorCount` only tally `"ok"`/`"error"` statuses; `"planned"`
results count toward neither.)

`marko sync` prints the full per-operation log (via `Server.OnReport`,
see above) followed by a summary. For a real sync: `Sync complete: N ok,
M errors`, exit `0` if `errorCount == 0` else `1`. For a preview:
`Preview complete. No changes were made to Chrome.`, always exit `0`.

### `POST /shutdown` (optional)

No body required. Lets the extension tell the CLI's one-shot server it
can stop listening immediately after a successful `/report`, instead of
waiting out `marko sync`'s `--timeout` (default `5m`, `0` = no timeout).

## Sequence

```
marko sync                         (binds 127.0.0.1:8765)
    |
    v
os/exec opens chrome-extension://<ExtensionID>/sync/index.html?port=8765&preview=0
(--auto-open, default on; the extension id is pinned, see above)
    |
    v
Sync.tsx runs automatically on page load, no clicks required:
GET  /plan                         -> desiredTree
chrome.bookmarks.getTree()         -> actualTree (local, in the extension)
POST /diff { actualTree }          -> operations[]
    |
    v
marko sync logs the full plan (Server.OnDiff)
    |
    v
preview=0: bookmarksApply.ts applies operations[] in array order via
chrome.bookmarks.create/update/remove/removeTree/move, threading
newly-created browserIds forward via a local targetPath -> browserId map
preview=1: planOperations() computes the same results without calling
chrome.bookmarks at all
    |
    v
POST /report { results, preview }  -> { accepted, okCount, errorCount }
    |
    v
marko sync logs the full report (Server.OnReport), prints a summary,
and exits immediately (0 if errorCount == 0 or preview, else 1) —
it does not wait out --timeout once the browser has responded
```

The popup (manual "Connect"/"Apply" buttons) still exists and uses the
identical `GET /plan` / `POST /diff` / `POST /report` calls — it's a
fallback for when you'd rather review the diff by hand before running
`marko sync` again, e.g. via `marko diff --actual <exported-state.json>`
without a running CLI session at all.

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
