# Sync Protocol Reference

`marko sync` reads and writes the target browser's native `Bookmarks`
file directly — no extension, no local server, no CORS. This document is
the reference for that mechanism, implemented in
[`cli/browserfile`](../cli/browserfile). For the diff/matching algorithm
itself, see [`docs/architecture.md`](./architecture.md) §7.

An earlier version of Marko drove a Chrome extension over a local HTTP
bridge instead. That approach was dropped after real-world testing
surfaced two problems inherent to going through a browser extension at
all: a missing CORS-preflight (`OPTIONS`) handler that made
cross-origin `POST` requests silently hang from the browser's
perspective, and a hardcoded assumption that the "Other Bookmarks" root
always has id `"2"`, which turned out to be false on a real profile (its
actual id was `"3"`). Reading/writing the Bookmarks file sidesteps both
categories of problem by not going through any browser extension API.

## How it works

1. Locates the browser's `Bookmarks` file: `--bookmarks-file <path>` if
   given, else `--browser <name>` (required if `--bookmarks-file` isn't
   given — there is no default browser; see `browserfile.KnownBrowsers`
   for the full list: `brave`, `chrome`, `chromium`, `edge`), always
   under that browser's `Default` profile directory (there is no flag to
   select a different profile), resolved to the OS-appropriate path (e.g.
   on macOS,
   `~/Library/Application Support/BraveSoftware/Brave-Browser/Default/Bookmarks`
   for Brave).
2. Refuses to proceed if the browser looks like it's currently running
   for that profile (Chromium's `SingletonLock` file/symlink is present
   next to the profile directory) — unless `--force` is passed, in which
   case it prints a warning and proceeds. **The browser should be
   closed**: Chromium periodically flushes its in-memory bookmark model
   back to this file, which would silently overwrite Marko's changes if
   the browser were left open.
3. Parses the file (kept as a fully generic `map[string]interface{}` tree
   internally, so any field Marko doesn't explicitly model — Brave's
   `meta_info`, `date_last_used`, the entire `synced`/mobile-bookmarks
   root, future additions — survives a read-modify-write round trip
   completely untouched) and converts the `bookmark_bar`/`other` roots
   into a `bookmarktree.BookmarkTree`, reading each root's real `id`
   directly from the file rather than assuming `"1"`/`"2"` (see above).
4. Runs it through the exact same `diff.Diff` engine as every other
   Marko command (§7) — the diff engine's contract is just "two
   `BookmarkTree` values in, a `Plan` out," so it doesn't care where
   "actual" came from.
5. Unless `--preview`, applies the plan directly to the in-memory
   parsed structure (assigning fresh sequential ids, random v4 GUIDs, and
   WebKit-epoch timestamps to new nodes, threading not-yet-existing
   parent ids forward within the same pass), backs up the original
   file's current content to `<path>.marko-backup-<unix-timestamp>`, and
   writes the result back atomically (temp file + rename).
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

## On the `checksum` field

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

## See also

- [`docs/architecture.md`](./architecture.md) §7 (diff/matching
  algorithm) and §9 (CLI command specs).
- [`docs/yaml-reference.md`](./yaml-reference.md), [`docs/templates.md`](./templates.md)
  for the `marko.yaml` side of the pipeline that produces the desired
  `BookmarkTree`.
