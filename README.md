# Marko

**Bookmark infrastructure as code.** Describe your browser bookmarks
(folders, links, reusable templates, variables) in a `marko.yaml` file;
Marko renders it, diffs it against your browser's actual bookmarks, and
writes the result directly to its `Bookmarks` file — no browser
extension involved.

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

That's all you need.

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
Compares the desired state against a previously-captured browser state and prints an operation plan (`CREATE`/`UPDATE`/`DELETE`/`MOVE`). Read-only; never touches the browser.

| Flag | Default | Description |
|---|---|---|
| `--actual` | — | Path to a captured `actualTree` JSON file (required) |

### `marko sync`
The main command: diffs the desired state against your browser and imports it, by reading and writing the target browser's native `Bookmarks` file directly.

```bash
marko sync --config marko.yaml --preview   # show the plan, change nothing
marko sync --config marko.yaml             # apply it
```

| Flag | Default | Description |
|---|---|---|
| `--preview` | off | Compute and print the plan without changing anything (dry run) |
| `--browser` | `brave` | `brave`, `chrome`, `chromium`, or `edge` |
| `--profile` | `Default` | Browser profile directory name |
| `--bookmarks-file` | — | Explicit path to a `Bookmarks` file, overrides `--browser`/`--profile` |
| `--force` | off | Write even if the browser looks like it's currently running for that profile (prints a warning instead of refusing) |

The target browser must be closed — it periodically flushes its own
bookmark state back to disk and would otherwise overwrite Marko's
change. A timestamped backup of the previous file is always written
alongside it before anything is changed. Pass `--force` to write anyway;
a warning is printed instead of refusing.

## Global flags

Available on every command: `--config <path>` (default: search upward
from cwd for `marko.yaml`), `--templates-dir <path>` (default: `<dir of
marko.yaml>/templates`), `--json` (machine-readable output where
applicable), `-v/--verbose`.

## Development

```bash
cd cli && go build ./... && go vet ./... && gofmt -l . && go test ./...
```

## License

[MIT](LICENSE)
