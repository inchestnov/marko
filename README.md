# Marko

**Bookmark infrastructure as code.** Describe your browser bookmarks
(folders, links, reusable templates, variables) in a `marko.yaml` file;
Marko renders it, diffs it against your browser's actual bookmarks, and
writes the result — no browser extension required by default.

- Full reference: [`docs/architecture.md`](docs/architecture.md)
- YAML schema: [`docs/yaml-reference.md`](docs/yaml-reference.md)
- Templates: [`docs/templates.md`](docs/templates.md)
- Sync mechanism: [`docs/sync-protocol.md`](docs/sync-protocol.md)
- Examples: [`examples/minimal`](examples/minimal), [`examples/full`](examples/full)

## Install

```bash
cd cli
go build -o marko .        # or: go install .
```

Extension (only needed for `marko sync --bridge=http`):

```bash
cd extension && npm install && npm run build
```
Then `chrome://extensions` → Developer mode → Load unpacked → `extension/dist`.

## Commands

### `marko init`
Scaffolds a starter `marko.yaml` (+ empty `templates/`).

| Flag | Default | Description |
|---|---|---|
| `--dir` | `.` | Target directory |
| `--force` | off | Overwrite an existing `marko.yaml` |

### `marko validate`
Runs structural and semantic validation on `marko.yaml` + `templates/`. No command-specific flags.

### `marko render`
Runs the full pipeline (parse → resolve templates → validate → render) and prints the resulting bookmark tree.

| Flag | Default | Description |
|---|---|---|
| `--out` | stdout | Write output to a file instead of stdout |

### `marko diff`
Compares the desired state against a previously-captured browser state and prints an operation plan (`CREATE`/`UPDATE`/`DELETE`/`MOVE`). Read-only.

| Flag | Default | Description |
|---|---|---|
| `--actual` | — | Path to a captured `actualTree` JSON file (required) |

### `marko export`
Renders the desired state to a static JSON file, for offline/no-CLI-session import via the extension's Options page.

| Flag | Default | Description |
|---|---|---|
| `--out` | `marko-export.json` | Output file path |

### `marko sync`
The main command: diffs the desired state against your browser and imports it. Two mechanisms, selected with `--bridge`:

- **`--bridge=file` (default)** — reads and writes the target browser's native `Bookmarks` file directly. No extension, no server.
- **`--bridge=http` (legacy)** — starts a local HTTP server and opens a Chrome extension page that applies the plan via `chrome.bookmarks`.

```bash
marko sync --config marko.yaml --preview   # show the plan, change nothing
marko sync --config marko.yaml             # apply it
```

| Flag | Default | Applies to | Description |
|---|---|---|---|
| `--bridge` | `file` | both | `file` or `http` |
| `--preview` | off | both | Compute and print the plan without changing anything (dry run) |
| `--browser` | `brave` | `file` | `brave`, `chrome`, `chromium`, or `edge` |
| `--profile` | `Default` | `file` | Browser profile directory name |
| `--bookmarks-file` | — | `file` | Explicit path to a `Bookmarks` file, overrides `--browser`/`--profile` |
| `--force` | off | `file` | Write even if the browser looks like it's currently running for that profile (prints a warning instead of refusing) |
| `--port` | `8765` | `http` | Port for the local HTTP server |
| `--timeout` | `5m` | `http` | Give up waiting for the extension after this long (`0` = never) |
| `--auto-open` | on | `http` | Automatically open the extension's sync page |

By default (`--bridge=file`), the target browser must be closed — it
periodically flushes its own bookmark state back to disk and would
otherwise overwrite Marko's change. A timestamped backup of the previous
file is always written alongside it before anything is changed. Pass
`--force` to write anyway; a warning is printed instead of refusing.

## Global flags

Available on every command: `--config <path>` (default: search upward
from cwd for `marko.yaml`), `--templates-dir <path>` (default: `<dir of
marko.yaml>/templates`), `--json` (machine-readable output where
applicable), `-v/--verbose`.

## Development

```bash
(cd cli && go build ./... && go vet ./... && gofmt -l . && go test ./...)
(cd extension && npx tsc --noEmit && npx vitest run)
```

## License

[MIT](LICENSE)
