# Marko

**Bookmark infrastructure as code.** Describe your browser bookmarks
(folders, links, reusable templates, variables) in a `marko.yaml` file;
Marko renders it, diffs it against your browser's actual bookmarks, and
writes the result directly to its `Bookmarks` file — no browser
extension involved.

## Contents

- [Installation](#installation)
- [Examples](#examples)
- [Commands](#commands)
- [Full documentation](docs/architecture.md)
- [YAML schema](docs/yaml-reference.md)
- [Templates](docs/templates.md)
- [Sync mechanism](docs/sync-protocol.md)

## Installation

```bash
go install github.com/inchestnov/marko@latest
```

This installs the `marko` binary directly into `$(go env GOPATH)/bin`.

### From source code

```bash
git clone https://github.com/inchestnov/marko.git
cd marko
go build -o marko .
```

## Examples

**Quick look** — preview what would change in Chrome without touching anything:

```bash
marko sync --browser chrome --preview
```

**A full loop** — scaffold a config, check it, see the rendered tree, then preview and apply:

```bash
marko init                                      # scaffold marko.yaml + templates/
marko validate                                  # check it
marko render                                    # print the resulting bookmark tree
marko sync --browser chrome --preview           # show the plan, change nothing
marko sync --browser chrome                     # apply it
```

**End to end**, starting from an actual `marko.yaml`:

```yaml
version: "1"

collections:
  personal:
    root: other
    bookmarks:
      - name: Gmail
        url: "https://mail.google.com"
      - name: Google Calendar
        url: "https://calendar.google.com"
    folders:
      - folder:
          name: Reading List
        bookmarks:
          - name: Hacker News
            url: "https://news.ycombinator.com"
```

```bash
marko validate --config marko.yaml
marko render --config marko.yaml
marko sync --config marko.yaml --browser chrome --preview
marko sync --config marko.yaml --browser chrome
```

This is [`examples/minimal`](examples/minimal) in full. For a richer
setup using templates and variables, see [`examples/full`](examples/full).

## Commands

### `marko init`
Scaffolds a starter `marko.yaml` and a `templates/` directory (with a
worked example template, `repository.yaml`). Both the example bookmark
and the example template usage in `marko.yaml` are commented out, so a
fresh scaffold doesn't validate until you uncomment or add real content
— nothing in it is meant to be applied as-is.

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

### `marko sync`
The main command: diffs the desired state against your browser and imports it, by reading and writing the target browser's native `Bookmarks` file directly.

```bash
marko sync --config marko.yaml --browser chrome --preview   # show the plan, change nothing
marko sync --config marko.yaml --browser chrome             # apply it
```

One of `--browser` or `--bookmarks-file` is required — there is no default browser.

| Flag | Default | Description |
|---|---|---|
| `--preview` | off | Compute and print the plan without changing anything (dry run) |
| `--browser` | — | `brave`, `chrome`, `chromium`, or `edge`; required unless `--bookmarks-file` is given |
| `--bookmarks-file` | — | Explicit path to a `Bookmarks` file, overrides `--browser` |
| `--force` | off | Write even if the browser looks like it's currently running for that profile (prints a warning instead of refusing); ignored with `--preview`, which never writes |

The target browser must be closed — it periodically flushes its own
bookmark state back to disk and would otherwise overwrite Marko's
change. A timestamped backup of the previous file is always written
alongside it before anything is changed. Pass `--force` to write anyway;
a warning is printed instead of refusing. `--preview` never writes, so
it runs regardless of whether the browser looks like it's running and
doesn't need `--force`.

## Global flags

Available on every command: `--config <path>` (default: search upward
from cwd for `marko.yaml`), `--templates-dir <path>` (default: `<dir of
marko.yaml>/templates`), `-v/--verbose`.

## Development

```bash
go build ./... && go vet ./... && gofmt -l . && go test ./...
```

## License

[MIT](LICENSE)
