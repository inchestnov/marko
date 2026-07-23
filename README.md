# Marko

**Bookmark infrastructure as code.**

Marko turns your browser bookmarks into a declarative, versionable,
templated YAML source of truth — and syncs that source of truth into
Chrome.

## Features

- 📄 **Declarative YAML schema** — collections, folders, bookmarks,
  variables, all in plain YAML. See
  [`docs/yaml-reference.md`](docs/yaml-reference.md).
- 🧩 **Template engine** — reusable, named subtrees with variables,
  required/default values, and single inheritance (`extends`), with a
  deliberately restricted `{{ .name }}` placeholder syntax (no loops, no
  code execution — see [`docs/templates.md`](docs/templates.md)).
- 🪆 **Nested templates & composition** — templates can reference other
  templates, either flattening into the caller (mixins) or nesting as
  sub-folders, letting you build e.g. a `repository` template out of
  `profile` + `github`.
- 🔍 **Diff engine** — compares your declared desired state against the
  browser's actual state and produces a minimal, ordered plan of
  `CREATE` / `UPDATE` / `DELETE` / `MOVE` operations, safe to compute at
  any time (diffing is read-only).
- 🔌 **Chrome extension sync** — a local-only HTTP bridge
  (`marko sync`) plus a Manifest V3 extension that reviews and applies
  the plan via `chrome.bookmarks`.
- 📦 **Static export fallback** — `marko export` writes the desired tree to
  a JSON file that can be imported via the extension's Options page even
  without a running CLI session (a conservative, CREATE-only fallback).

## Quick Example

A minimal `marko.yaml` — a couple of bookmarks and one folder, no
templates required (see
[`examples/minimal/marko.yaml`](examples/minimal/marko.yaml)):

```yaml
version: "1"

collections:
  personal:
    root: other
    bookmarks:
      - name: Gmail
        url: "https://mail.google.com"
    folders:
      - folder:
          name: Reading List
        bookmarks:
          - name: Hacker News
            url: "https://news.ycombinator.com"
```

```
$ marko render
Bookmarks Bar
Other Bookmarks
├── Gmail
└── Reading List
    └── Hacker News
```

That's the whole loop: describe the structure you want, then `marko
render` / `marko diff` / `marko sync` to preview and apply it in Chrome.
For templates, variables, and everything else Marko can do, keep
reading below.

## Motivation

Plain browser bookmarks don't scale. Once you have more than a handful,
the built-in bookmark manager runs into all the same problems any
hand-maintained, unstructured pile of state runs into:

- No way to reuse structure — every new project/team/service gets its
  bookmark folder built by hand, link by link.
- No way to template anything — "give every new repo a folder with
  Docs/Issues/CI links" is not expressible.
- No portability — moving your structure to a new machine or browser
  profile means starting over.
- No history — there's no diff, no review, no rollback.

Marko applies the **Infrastructure as Code** approach to this problem.
You describe the bookmark structure you want in a `marko.yaml` file
(plus, optionally, a reusable `templates/` library) and Marko computes
and applies the changes needed to make your browser match it. The
browser is only ever a rendering target; `marko.yaml` is authoritative,
and nothing is ever read back into it automatically.

## Architecture

```
marko.yaml + templates/*.yaml
        |
        v
   Marko CLI (Go)
        |
  parse -> resolve templates -> validate -> render
        |
        v
    BookmarkTree (Desired State)
        |
        v
  Diff Engine  <---  actual BookmarkTree (from chrome.bookmarks)
        |
        v
   Plan (CREATE/UPDATE/DELETE/MOVE)
        |
        v
  Local HTTP bridge (marko sync, 127.0.0.1 only)
        |
        v
  Chrome Extension  --chrome.bookmarks-->  Chrome
```

The CLI never talks to the browser directly (no native messaging host);
`marko sync` starts a loopback-only HTTP server that the extension polls
for a plan and reports results back to. See
[`docs/architecture.md`](docs/architecture.md) for the full binding
technical contract — data model, template resolution algorithm,
validation rules, render/diff algorithms, and the sync protocol.

## Installation

**Go CLI:**

```bash
cd cli
go build -o marko .
# or, to install into $GOPATH/bin:
go install .
```

**Chrome extension:**

```bash
cd extension
npm install
npm run build
```

This produces `extension/dist` (via `vite build` for the popup/options
UI and a second `vite build --config vite.config.background.ts` pass for
the service worker). Then, in Chrome:

1. Go to `chrome://extensions`.
2. Enable "Developer mode" (top right).
3. Click "Load unpacked" and select `extension/dist`.

## Configuration

The full schema is documented in
[`docs/yaml-reference.md`](docs/yaml-reference.md). A minimal
`marko.yaml`:

```yaml
version: "1"

collections:
  personal:
    root: other
    bookmarks:
      - name: Gmail
        url: "https://mail.google.com"
```

By default, every command searches upward from the current directory for
`marko.yaml` (override with `--config <path>`) and looks for a
`templates/` directory next to it (override with `--templates-dir
<path>`). Scaffold a starter file with:

```bash
marko init
```

## Templates

The template engine supports variables, nesting, and composition — see
[`docs/templates.md`](docs/templates.md) for the full authoring guide.
The canonical example: a `profile` template (folder-less, so it flattens
into its caller) composed with a `github` template (has its own folder,
so it nests) into a `repository` template:

```yaml
templates:
  profile:
    vars:
      username: { required: true }
    bookmarks:
      - name: "{{ .username }} - Profile"
        url: "https://github.com/{{ .username }}"

  github:
    folder: { name: GitHub }
    bookmarks:
      - name: Documentation
        url: "https://docs.github.com"

  repository:
    vars:
      username: { required: true }
      repo_name: { required: true }
      repo_org: { default: "{{ .username }}" }
    folder:
      name: "{{ .repo_org }}/{{ .repo_name }}"
    templates:
      - template: profile
        vars: { username: "{{ .username }}" }
      - template: github
        as: "GitHub Links"
    bookmarks:
      - name: Repository
        url: "https://github.com/{{ .repo_org }}/{{ .repo_name }}"
```

Instantiating `repository` twice with different variables (see
[`examples/full/marko.yaml`](examples/full/marko.yaml)) produces two
independent, fully-populated project folders from one reusable
definition.

## Examples

- [`examples/minimal/marko.yaml`](examples/minimal/marko.yaml) — a tiny,
  template-free single-collection example for a first-time user.
- [`examples/full/marko.yaml`](examples/full/marko.yaml) (+
  [`examples/full/templates/`](examples/full/templates)) — collections on
  both `bar` and `other` roots, a global `variables:` default, the
  `kubernetes` template, `repository` instantiated twice with different
  vars, and an inline `folders:` group.

Both are validated against the real CLI with zero errors:

```
$ marko validate --config examples/full/marko.yaml --templates-dir examples/full/templates
examples/full/marko.yaml is valid (0 errors, 0 warnings)

$ marko render --config examples/full/marko.yaml --templates-dir examples/full/templates
Bookmarks Bar
└── Work
    ├── Company Wiki
    ├── Kubernetes
    │   ├── Documentation
    │   ├── GitHub
    │   ├── Dashboard
    │   └── Grafana
    ├── marko
    │   ├── Repository
    │   ├── octocat - Profile
    │   └── GitHub Links
    │       ├── Documentation
    │       └── GitHub
    └── some-other-project
        ├── Repository
        ├── octocat - Profile
        └── GitHub Links
            ├── Documentation
            └── GitHub
Other Bookmarks
├── Gmail
└── Reading List
    ├── Hacker News
    └── Lobsters
```

## Browser Support

**Chrome only, today.** The extension targets Manifest V3 and
`chrome.bookmarks`. The CLI <-> extension protocol (`GET /plan`,
`POST /diff`, `POST /report`) is deliberately browser-agnostic, so a
future Firefox/Safari bridge could reuse the same contract — but no such
adapter exists yet (see [`docs/architecture.md`](docs/architecture.md)
§12 and [Roadmap](#roadmap) below).

## Development

Repository layout:

```
cli/            Go CLI: cmd/, parser/, template/, validator/, renderer/, diff/, sync/
extension/      Chrome extension (TypeScript + React + Vite, Manifest V3)
templates/      Shared/example reusable template library
examples/       Worked, validated example configs (minimal/, full/)
docs/           Reference documentation
```

Run the CLI test suite:

```bash
cd cli
go test ./...
```

Run the extension test suite:

```bash
cd extension
npx vitest run
```

For manual end-to-end testing: build and run the CLI's sync server, then
load the unpacked extension and connect to it.

```bash
cd cli && go build -o marko . && ./marko sync --config ../examples/full/marko.yaml --templates-dir ../examples/full/templates
```

Then, with the extension loaded (see [Installation](#installation)),
open its popup and click "Connect" to fetch the plan, review the diff,
and apply it.

## Testing

- **Go (`cli/`)**: table-driven unit tests per package —
  `parser`, `template`, `validator`, `renderer`, `diff`, `sync` — plus an
  integration test (`cli/internal/integration`) that runs a
  `marko.yaml`-shaped fixture through the full
  parse -> resolve -> render -> diff -> simulated-apply -> re-diff
  pipeline and asserts the final re-diff is empty (idempotency), and a
  black-box `cli/cmd` integration test that drives the actual Cobra
  commands end-to-end (`init` -> `validate` -> `render` -> `export` ->
  `diff`). `go test ./...` reports 74 passing test cases, 0 failing.
- **TypeScript (`extension/`)**: Vitest coverage (10 tests) for
  `lib/bookmarksApply.ts` (tree conversion, operation application,
  placeholder-id threading, partial-failure handling) against a mocked
  `chrome.bookmarks` global.

Run everything:

```bash
(cd cli && go build ./... && go vet ./... && gofmt -l . && go test ./...)
(cd extension && npx tsc --noEmit && npx vitest run)
```

## Roadmap

Deliberately out of scope for v1 (see
[`docs/architecture.md`](docs/architecture.md) §12), and candidates for
future work:

- Bridges for other browsers (Firefox, Safari, Edge) reusing the same
  `GET /plan` / `POST /diff` / `POST /report` protocol.
- Authenticated / HTTPS sync transport (today: unauthenticated,
  loopback-only HTTP, considered sufficient for locally-readable data).
- Cloud sync, multi-device sync, or remote storage of `marko.yaml` /
  bookmark state.
- Two-way sync — reading the browser's current state back into
  `marko.yaml` automatically (today, `marko diff --actual` / the
  extension's "export current browser state" are one-off snapshots, not
  a reconciliation feature).
- Conflict resolution UI for concurrent edits between `marko diff` and
  `marko sync` apply.
- Codegen to keep `extension/src/lib/types.ts` in lockstep with the Go
  protocol structs (currently hand-maintained).
- Bookmark favicons, tags, and Chrome's "Managed bookmarks" enterprise
  policy tree.
- Packaging/distribution automation (Homebrew formula, Chrome Web Store
  publishing) — local build/install only, for now.
- A template macro/partial system beyond what `vars` + nesting already
  provide, or any conditional logic — this one is a permanent design
  constraint, not a gap (see [`docs/templates.md`](docs/templates.md)),
  but is listed here for completeness since it's explicitly named as
  deferred/out-of-scope in the architecture doc.

## License

[MIT](LICENSE)
