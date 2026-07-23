# Marko Architecture

Version: 1.0
Status: Binding technical contract for implementation agents

## 1. Overview &amp; Goals

Marko is a "bookmark infrastructure as code" tool. The source of truth is a
YAML file, `marko.yaml` (plus supporting files under `templates/`), that
declares the desired structure of a user's browser bookmarks: collections,
folders, bookmarks, reusable templates, and variables.

The Marko CLI (Go):

1. Parses and validates `marko.yaml` and `templates/*.yaml`.
2. Resolves templates (nesting, composition, variable substitution) into a
   fully-expanded configuration.
3. Renders the expanded configuration into an in-memory `BookmarkTree`
   (the **Desired State**).
4. Compares the Desired State against the actual browser bookmark tree (the
   **Browser State**, read directly from the target browser's own
   `Bookmarks` file) and produces an ordered `Plan` of `Operation`s
   (`CREATE`, `UPDATE`, `DELETE`, `MOVE`).
5. Applies that plan by writing the result back to the browser's
   `Bookmarks` file directly, or via a static exported JSON file for
   offline inspection.

The browser is only a rendering target. `marko.yaml` is authoritative.
Nothing is ever read back into `marko.yaml` automatically; sync is one-way
(YAML -> Browser), diff is read-only and safe to run at any time.

Design principles:

- No browser extension, no native messaging host, no local server. `marko
  sync` reads and writes the target browser's native `Bookmarks` file
  directly (see `cli/browserfile` and `docs/sync-protocol.md`) — an
  earlier iteration drove a Chrome extension over a local HTTP bridge
  instead, but that approach was dropped after real-world testing
  surfaced problems inherent to going through a browser extension at all
  (see `docs/sync-protocol.md` for what those were).
- The template engine is a pure data-substitution engine: variables,
  nesting, composition, and inheritance are allowed; code execution, shell,
  JS, Python, and loops are explicitly forbidden.
- All operations are idempotent: running `marko diff` / `marko sync`
  repeatedly with no changes to `marko.yaml` or the browser produces an
  empty plan.

## 2. Directory / Module Layout

This is the concrete layout the Project Bootstrap Agent must create.

```
marko/
├── cli/
│   ├── cmd/                     # Cobra command definitions (one file per command)
│   │   ├── root.go
│   │   ├── init.go
│   │   ├── validate.go
│   │   ├── render.go
│   │   ├── diff.go
│   │   └── sync.go
│   ├── internal/
│   │   ├── model/               # Shared data model structs (Config, Collection, Template, ...)
│   │   │   └── model.go
│   │   ├── bookmarktree/        # BookmarkTree struct + helpers (path resolution, walking)
│   │   │   └── tree.go
│   │   ├── fsutil/              # File discovery helpers (find marko.yaml, templates/*.yaml)
│   │   │   └── fsutil.go
│   │   └── version/
│   │       └── version.go
│   ├── parser/                  # YAML loading -> raw model.Config
│   │   ├── parser.go
│   │   └── parser_test.go
│   ├── validator/                # Static validation of resolved config
│   │   ├── validator.go
│   │   └── validator_test.go
│   ├── template/                 # Template resolution engine (nesting, vars, composition)
│   │   ├── engine.go
│   │   ├── resolve.go
│   │   └── engine_test.go
│   ├── renderer/                 # Resolved config -> BookmarkTree
│   │   ├── renderer.go
│   │   └── renderer_test.go
│   ├── diff/                     # BookmarkTree vs BookmarkTree -> []Operation
│   │   ├── diff.go
│   │   ├── match.go
│   │   └── diff_test.go
│   ├── browserfile/               # Reads/writes a Chromium-family browser's Bookmarks file directly
│   │   ├── paths.go               # Locates the file per browser/profile/OS
│   │   ├── lock.go                # Detects whether the browser is running (SingletonLock)
│   │   ├── file.go                # Parse / ToBookmarkTree / Apply(diff.Plan) / Write
│   │   └── file_test.go
│   ├── go.mod
│   ├── go.sum
│   └── main.go
├── templates/                     # Shared/example reusable template library (see §3)
│   ├── profile.yaml
│   ├── github.yaml
│   └── repository.yaml
├── examples/
│   ├── minimal/marko.yaml
│   └── full/
│       ├── marko.yaml
│       └── templates/
├── docs/
│   ├── architecture.md            # this file
│   ├── yaml-reference.md
│   ├── templates.md
│   └── sync-protocol.md
└── README.md
```

Module boundary rules (must be enforced by import direction, no cycles):

- `internal/model` has zero dependencies on other `cli/*` packages.
- `parser` depends only on `internal/model`.
- `template` depends only on `internal/model`.
- `validator` depends only on `internal/model` and `template` (to validate
  post-resolution).
- `renderer` depends only on `internal/model`, `template`, and
  `internal/bookmarktree`.
- `diff` depends only on `internal/bookmarktree`.
- `browserfile` depends only on `internal/bookmarktree` and `diff`.
- `cmd/*` is the only package allowed to wire everything together and talk
  to the filesystem/stdout directly.

## 3. Data Model

All Go types below live in `cli/internal/model/model.go` unless noted.

### 3.1 Go Structs

```go
package model

// Config is the fully-parsed, not-yet-resolved representation of
// marko.yaml plus every file discovered under templates/.
type Config struct {
	Version     string                `yaml:"version"`
	Variables   map[string]Variable   `yaml:"variables,omitempty"`
	Templates   map[string]Template   `yaml:"templates,omitempty"`
	Collections map[string]Collection `yaml:"collections"`

	// SourcePath is the absolute path to the loaded marko.yaml.
	// Not serialized; populated by the parser.
	SourcePath string `yaml:"-"`
}

// Variable declares a named, typed placeholder with an optional default.
// Values are always treated as strings after substitution; Type is
// advisory metadata used only by the validator (e.g. "url" triggers a
// URL-shape check).
type Variable struct {
	Default     string `yaml:"default,omitempty"`
	Description string `yaml:"description,omitempty"`
	Type        string `yaml:"type,omitempty"` // "string" (default) | "url"
	Required    bool   `yaml:"required,omitempty"`
}

// Template is a reusable, named subtree of folders/bookmarks/nested
// templates. A Template is never rendered on its own; it must be
// instantiated via a TemplateRef inside a Collection or another Template.
type Template struct {
	// Extends allows single inheritance: the base template's Bookmarks,
	// Folders and Templates are merged first, then this template's own
	// entries are appended / override by Name (see §4.5).
	Extends string `yaml:"extends,omitempty"`

	// Vars declares the variables this template accepts, with optional
	// per-template default overrides layered on top of global Variables.
	Vars map[string]Variable `yaml:"vars,omitempty"`

	// Folder, if set, wraps this template's contents in a named folder.
	// If omitted, the template's contents are emitted directly into the
	// parent (flattening / "mixin" behavior), which is how composition
	// of e.g. Profile + GitHub into Repository works.
	Folder *Folder `yaml:"folder,omitempty"`

	Bookmarks []Bookmark    `yaml:"bookmarks,omitempty"`
	Templates []TemplateRef `yaml:"templates,omitempty"`
}

// Folder is an explicit folder node. Name supports variable
// interpolation, e.g. "{{ .repo_name }}".
type Folder struct {
	Name string `yaml:"name"`
}

// Bookmark is a leaf node: a single browser bookmark.
// Name and URL both support variable interpolation.
type Bookmark struct {
	Name string `yaml:"name"`
	URL  string `yaml:"url"`
}

// TemplateRef instantiates a named Template at the current position in a
// Collection or inside another Template's Templates list ("nested
// template" / composition). It is also referred to as an "Instance".
type TemplateRef struct {
	// Template is the name key into Config.Templates (or, when nested
	// inside another template body, may also resolve relative to that
	// template's own local template set if declared - see §4).
	Template string `yaml:"template"`

	// As overrides the mount-point folder name for this instantiation.
	// If omitted, the referenced template's own Folder.Name (after
	// interpolation) is used. If the referenced template has no Folder,
	// As has no effect and the template's contents are flattened in place.
	As string `yaml:"as,omitempty"`

	// Vars supplies concrete values (or overrides) for the variables the
	// referenced template declares (and transitively, nested templates).
	Vars map[string]string `yaml:"vars,omitempty"`
}

// Collection is a top-level named bookmark tree, typically mapped 1:1 to
// a top-level Chrome bookmark folder (e.g. "Bookmarks Bar", "Other
// Bookmarks", or a user-named folder under one of those roots).
type Collection struct {
	// Root selects which native Chrome root this collection is mounted
	// under: "bar" (Bookmarks Bar) or "other" (Other Bookmarks).
	// Defaults to "other".
	Root string `yaml:"root,omitempty"`

	// Folder is optional; if set, the collection's contents are nested
	// inside a named folder under Root. If omitted, contents are placed
	// directly under Root.
	Folder *Folder `yaml:"folder,omitempty"`

	Bookmarks []Bookmark    `yaml:"bookmarks,omitempty"`
	Templates []TemplateRef `yaml:"templates,omitempty"`
	Folders   []NamedGroup  `yaml:"folders,omitempty"`
}

// NamedGroup is an inline (non-template) sub-folder allowed directly in a
// Collection or Template body, for one-off structure that doesn't warrant
// a reusable Template.
type NamedGroup struct {
	Folder    Folder        `yaml:"folder"`
	Bookmarks []Bookmark    `yaml:"bookmarks,omitempty"`
	Templates []TemplateRef `yaml:"templates,omitempty"`
	Folders   []NamedGroup  `yaml:"folders,omitempty"`
}
```

### 3.2 YAML Schema — Concrete Example

`marko.yaml`:

```yaml
version: "1"

variables:
  company_domain:
    default: company.com
    type: string

templates:
  profile:
    vars:
      username:
        required: true
        type: string
    bookmarks:
      - name: "{{ .username }} - Profile"
        url: "https://github.com/{{ .username }}"

  github:
    folder:
      name: GitHub
    bookmarks:
      - name: Documentation
        url: "https://docs.github.com"
      - name: GitHub
        url: "https://github.com"

  # "Repository" composes Profile + GitHub (nested templates), and adds
  # its own parameterized bookmark. This is the example from spec §15.
  repository:
    vars:
      username:
        required: true
      repo_name:
        required: true
      repo_org:
        default: "{{ .username }}"
    folder:
      name: "{{ .repo_org }}/{{ .repo_name }}"
    templates:
      - template: profile
        vars:
          username: "{{ .username }}"
      - template: github
        as: "GitHub Links"
    bookmarks:
      - name: Repository
        url: "https://github.com/{{ .repo_org }}/{{ .repo_name }}"

  kubernetes:
    folder:
      name: Kubernetes
    bookmarks:
      - name: Documentation
        url: "https://kubernetes.io/docs/"
      - name: GitHub
        url: "https://github.com/kubernetes/kubernetes"
      - name: Dashboard
        url: "https://dashboard.{{ .company_domain }}"
      - name: Grafana
        url: "https://grafana.{{ .company_domain }}"

collections:
  work:
    root: bar
    folder:
      name: Work
    templates:
      - template: kubernetes
      - template: repository
        as: "marko"
        vars:
          username: octocat
          repo_name: marko
      - template: repository
        as: "some-other-project"
        vars:
          username: octocat
          repo_name: some-other-project
          repo_org: some-org
    bookmarks:
      - name: Company Wiki
        url: "https://wiki.{{ .company_domain }}"

  personal:
    root: other
    bookmarks:
      - name: Gmail
        url: "https://mail.google.com"
```

Rendered structure (abbreviated) for the `work` collection:

```
Bookmarks Bar
└── Work
    ├── Kubernetes
    │   ├── Documentation
    │   ├── GitHub
    │   ├── Dashboard
    │   └── Grafana
    ├── marko
    │   ├── octocat - Profile
    │   ├── GitHub Links
    │   │   ├── Documentation
    │   │   └── GitHub
    │   └── Repository
    ├── some-other-project
    │   ├── octocat - Profile
    │   ├── GitHub Links
    │   │   ├── Documentation
    │   │   └── GitHub
    │   └── Repository
    └── Company Wiki
```

Templates may also be split into separate files under `templates/` (one
top-level `templates:` map key per file, or multiple); the parser merges
all discovered files with `marko.yaml`'s own `templates:` block. Duplicate
template names across files/`marko.yaml` are a validation error (see §5).

## 4. Template Engine Semantics

Implemented in `cli/template/engine.go` and `resolve.go`. Input: `model.Config`
(raw, as parsed). Output: a `ResolvedCollection` tree per collection, where
every `TemplateRef` has been fully expanded and every variable placeholder
has been substituted with a literal string. No `TemplateRef` or `{{ }}`
placeholder may remain in the output; the validator and renderer only ever
see resolved data.

### 4.1 Variable Placeholder Syntax

Placeholders use Go's `text/template`-*compatible lexical syntax*
restricted to a single construct: `{{ .name }}` (dot-prefixed field
access into the current variable scope). Implementation MAY use
`text/template` as the tokenizer/substitution mechanism, but MUST
construct it with:

- No custom functions registered (`Funcs` map empty).
- No pipelines (`|`), no `if`/`range`/`with`/`block` actions permitted —
  the engine MUST reject (validation error `E_TEMPLATE_SYNTAX`) any
  placeholder containing a token other than `.<identifier>` inside
  `{{ }}`. This is enforced with a pre-parse regex/scan
  (`^\s*\.[A-Za-z_][A-Za-z0-9_]*\s*$` on the trimmed contents between
  `{{` and `}}`) before ever handing the string to `text/template`,
  guaranteeing loops/exec/shell/JS/Python cannot be smuggled in even
  though the underlying engine is technically capable of more.
- Only `Bookmark.Name`, `Bookmark.URL`, `Folder.Name`, and
  `TemplateRef.Vars` *values* are interpolated. Template names, variable
  names, and YAML keys are never interpolated.

### 4.2 Resolution Algorithm

`Resolve(cfg *model.Config) (map[string]*ResolvedCollection, error)`:

1. **Build template index**: map template name -> `model.Template`.
   Error `E_UNKNOWN_TEMPLATE` deferred to validation pass (§5), but the
   engine itself must also return an error if resolution encounters a
   `TemplateRef.Template` not present in the index (defensive double
   check, same error code).
2. **Apply inheritance** (`extends`) per template, memoized, before any
   instantiation:
   - Resolve `Extends` transitively (a chain `c extends b extends a` is
     allowed as long as it terminates without a cycle).
   - Cycle detection: while resolving `extends` for template `T`,
     maintain a `visiting` set (ordered slice used as a stack) of
     template names currently being expanded. If `T` is already in
     `visiting`, return `E_CYCLE_EXTENDS` with the full cycle path
     (e.g. `a -> b -> a`).
   - Merge order (base first): `Bookmarks` = `base.Bookmarks ++
     self.Bookmarks` (append, base entries first). `Templates` =
     `base.Templates ++ self.Templates` (append). `Vars` = base vars
     map, overlaid by self vars map (self wins on key collision).
     `Folder` = self.Folder if set, else base.Folder.
3. **Instantiate each `TemplateRef`** encountered while walking
   Collections top-down, recursively, via
   `instantiate(ref TemplateRef, scope VarScope, pathStack []string) (*ResolvedGroup, error)`:
   1. Look up template `T := templates[ref.Template]` (post-inheritance
      merge from step 2).
   2. Cycle detection for nesting: `pathStack` holds the chain of
      template names currently being instantiated (distinct from the
      `extends` visiting set). If `ref.Template` is already in
      `pathStack`, return `E_CYCLE_TEMPLATE` with the cycle path, e.g.
      `repository -> github -> repository`. Self-nesting (a template
      referencing itself, directly or transitively) is always an error;
      there is no recursion-limit-based allowance since loops are
      explicitly forbidden by spec.
   3. Build the **child variable scope** (see §4.3 for precedence):
      start from `T.Vars` defaults, overlay global `Variables` defaults
      only for names also declared in `T.Vars` (global variables are
      only implicitly visible to a template if it declares a `Vars`
      entry of the same name with no default — see 4.3), then overlay
      `ref.Vars` (each value first interpolated against the **parent**
      scope, then assigned).
   4. Substitute all placeholders in `T.Folder.Name` (if present) and in
      each of `T.Bookmarks[i].Name` / `.URL` using the child scope.
   5. Recurse into `T.Templates` (nested `TemplateRef`s), each with
      `pathStack + [ref.Template]` and the child scope as *their*
      parent scope.
   6. Assemble a `ResolvedGroup`:
      - If `T.Folder` is set: a folder node named
        `ref.As` (if set) else the interpolated `T.Folder.Name`,
        containing the bookmarks from step 4, then the nested groups
        from step 5, in the order: **own bookmarks first, then own
        inline `Folders`/nested groups in the order the nested
        `Templates`/`Folders` lists were declared** (see §6 for the
        canonical ordering rule used by the renderer — the template
        engine preserves declaration order and does not reorder).
      - If `T.Folder` is unset: no wrapping folder is created; the
        bookmarks and nested groups are returned to be spliced directly
        into the parent's children list at the position of this
        `TemplateRef` (flattening, used for composition mixins like
        `profile` inside `repository`). `ref.As` is ignored with a
        `W_AS_IGNORED` warning (non-fatal; validator emits a warning,
        not an error) if set on a folder-less template.
4. **Inline `NamedGroup` / `Folders` entries** in Collections/Templates
   are processed the same way as step 3.4-3.6 but require no template
   lookup; their own `Bookmarks`/`Templates`/`Folders` are recursed
   directly.
5. Result per collection: an ordered `ResolvedGroup` tree with only
   literal strings, ready for the renderer.

### 4.3 Variable Precedence Rules (highest wins)

1. `TemplateRef.Vars[name]` at the instantiation site (explicit override),
   itself interpolated against the *calling* scope before being bound.
2. The referenced `Template.Vars[name].Default`, if the template declares
   a default for `name`.
3. The root `Config.Variables[name].Default` (global default), **only**
   if the template declares `name` in its own `Vars` map (a template
   cannot implicitly read a global variable it did not declare — this
   keeps templates self-documenting and independently reusable/portable).
4. If none of the above provide a value and the variable is marked
   `Required: true` (on the template's `Vars` entry, or implicitly
   required when no default exists anywhere), resolution fails with
   `E_MISSING_VARIABLE` (see §5).

Scopes do not leak sideways or upward: a nested template's `Vars` are
never visible to its parent, and a sibling `TemplateRef`'s vars are never
visible to another sibling. Only explicit `Vars:` passed at each
`TemplateRef` site propagate downward.

### 4.4 Composition

"Composition" = a template's `Templates:` list containing references to
other templates, each expanded and (if folder-less) flattened inline, or
(if it has its own `Folder`) nested as a sub-folder — exactly the
mechanism in §4.2 step 3.6. This is how `Repository = Profile + GitHub` is
expressed: `Profile` is folder-less (flattens its bookmark(s) directly
into `Repository`'s folder), `GitHub` has its own `Folder` (nests as a
sub-folder named "GitHub" or renamed via `as:`).

### 4.5 Inheritance

`extends:` (§4.2 step 2) is single inheritance only (one parent, no
multiple inheritance/mixin-by-extends — use composition via `Templates:`
for combining multiple templates). A template that both `extends` another
template and is itself referenced via composition behaves identically to
any other template from the caller's perspective (inheritance is fully
resolved before any instantiation).

### 4.6 Forbidden Constructs (Enforced)

- No `{{ if }}`, `{{ range }}`, `{{ with }}`, `{{ block }}`, `{{ define }}`,
  pipelines, or function calls in placeholders — rejected at parse time
  with `E_TEMPLATE_SYNTAX`.
- No shell-out, `exec`, file I/O, or network calls are ever performed by
  the template engine.
- No looping construct exists anywhere in the schema. Repetition is only
  achieved by explicitly writing multiple `TemplateRef` entries (as in the
  `work` collection example instantiating `repository` twice) — this is
  intentional and matches spec §16.

## 5. Validation Rules

Implemented in `cli/validator/validator.go`. Validation runs in two
phases: **Phase A - Structural** (on raw `model.Config`, before
resolution) and **Phase B - Semantic** (on the resolved tree, after the
template engine runs but before rendering). `marko validate` runs both
phases and reports all errors found (not just the first), grouped by
phase.

All error messages follow the format:

```
<ERROR_CODE>: <human message> (at <location>)
```

Where `<location>` is a YAML-path-like breadcrumb, e.g.
`collections.work.templates[1]` or `templates.repository.vars.repo_name`.

### 5.1 Phase A - Structural (per entity required fields)

| Entity | Required fields | Error code |
|---|---|---|
| `Config` | `version`, `collections` (non-empty map) | `E_MISSING_FIELD` |
| `Collection` | at least one of `bookmarks`/`templates`/`folders` non-empty | `E_EMPTY_COLLECTION` |
| `Collection.root` | if set, must be `"bar"` or `"other"` | `E_INVALID_ENUM` |
| `Template` (any referenced or declared) | must have at least one of `bookmarks`/`templates`/`folder` (a template with none of these is meaningless) | `E_EMPTY_TEMPLATE` |
| `Folder.name` | non-empty string | `E_MISSING_FIELD` |
| `Bookmark.name` | non-empty string | `E_MISSING_FIELD` |
| `Bookmark.url` | non-empty string, must parse as absolute URL with `http://`, `https://`, or `chrome://` scheme | `E_INVALID_URL` |
| `TemplateRef.template` | non-empty string | `E_MISSING_FIELD` |
| `Variable` (global or template-scoped) | if `required: true`, must not also set `default` (contradictory) | `E_INVALID_VARIABLE_DECL` |
| Duplicate template name across `marko.yaml` + `templates/*.yaml` | none allowed | `E_DUPLICATE_TEMPLATE` |
| Duplicate collection name | none allowed (YAML map keys already enforce this at parse time; this covers case-insensitive collisions) | `E_DUPLICATE_COLLECTION` |

### 5.2 Phase B - Semantic (post-resolution / during resolution)

| Condition | Error code | Message format |
|---|---|---|
| `TemplateRef.template` not found in template index | `E_UNKNOWN_TEMPLATE` | `unknown template "X" referenced (at collections.C.templates[i])` |
| Placeholder `{{ .name }}` where `name` is not resolvable in the current scope (not in `ref.Vars`, no template default, no eligible global default) | `E_UNDEFINED_VARIABLE` | `undefined variable "name" (at templates.T.bookmarks[i].url)` |
| `Variable.Required == true` and no value supplied through any precedence level | `E_MISSING_VARIABLE` | `required variable "name" not provided for template "T" (at collections.C.templates[i])` |
| Cycle in `extends` chain | `E_CYCLE_EXTENDS` | `cyclic extends: a -> b -> a` |
| Cycle in nested `TemplateRef` chain | `E_CYCLE_TEMPLATE` | `cyclic template reference: repository -> github -> repository` |
| Placeholder fails the restricted-syntax check (§4.6) | `E_TEMPLATE_SYNTAX` | `invalid placeholder "{{ range .x }}": only "{{ .name }}" is allowed (at ...)` |
| `TemplateRef.vars` supplies a key not declared in the target template's `Vars` map | `W_UNKNOWN_VAR` (warning, non-fatal) | `variable "X" is not declared by template "T" and will be ignored` |
| Folder-less template referenced with `as:` set | `W_AS_IGNORED` (warning) | `"as" has no effect on folder-less template "T"` |
| Two sibling nodes (same parent) resolve to the same `(kind, name)` pair — two bookmarks or two folders with an identical title directly under the same parent | `E_DUPLICATE_SIBLING` | `duplicate bookmark "X" under collections.C (also produced by templates[0])` — this is an error only for Bookmarks with identical `(name, url)` are silently deduplicated (idempotent), but identical `name` with a **different** `url` under the same parent is `E_DUPLICATE_SIBLING`. Two folders with the same name under the same parent are always **merged** (their children concatenated), not an error. |

Exit behavior: any `E_*` finding causes `marko validate` (and any command
that internally validates, i.e. `render`/`diff`/`sync`/`export`) to fail
with a non-zero exit code (see §9). `W_*` findings are printed to stderr
but do not affect the exit code.

## 6. Render Engine

Implemented in `cli/renderer/renderer.go`. Input: the resolved
per-collection tree from the template engine (§4) after Phase B
validation passes. Output: a single in-memory `BookmarkTree`
(`cli/internal/bookmarktree/tree.go`) representing the full desired
state across all collections.

```go
package bookmarktree

// NodeKind distinguishes folders from bookmarks in the tree.
type NodeKind string

const (
	KindFolder   NodeKind = "folder"
	KindBookmark NodeKind = "bookmark"
)

// Node is a single folder or bookmark in the tree. Bookmarks never have
// Children. Folders never have URL set.
type Node struct {
	Kind NodeKind `json:"kind"`
	Name string   `json:"name"`
	URL  string   `json:"url,omitempty"`

	// Path is the ordered list of ancestor folder names from the
	// applicable root down to (but excluding) this node itself, e.g.
	// ["bar", "Work", "Kubernetes"]. Path[0] is always "bar" or "other".
	// Path is derivable from tree position but is denormalized onto the
	// Node for O(1) lookup during diff/matching.
	Path []string `json:"path"`

	Children []*Node `json:"children,omitempty"`

	// BrowserID is populated only on Browser State trees (never on
	// Desired State trees, since desired nodes have no browser identity
	// yet). Empty string means "not yet created".
	BrowserID string `json:"browserId,omitempty"`

	// Index is this node's 0-based position among its siblings, assigned
	// during render/normalize and used by the diff engine to detect
	// ordering changes that warrant a MOVE.
	Index int `json:"index"`
}

// BookmarkTree is the root container. Roots holds exactly two entries
// with Name "bar" and "other" (Chrome's two native top-level folders that
// Marko manages; "Bookmarks Bar" id "1" and "Other Bookmarks" id "2").
type BookmarkTree struct {
	Roots []*Node `json:"roots"`
}
```

### 6.1 Render Algorithm

`Render(resolved map[string]*ResolvedCollection) (*bookmarktree.BookmarkTree, error)`:

1. Create two root `Node`s: `{Kind: KindFolder, Name: "bar", Path: []}` and
   `{Kind: KindFolder, Name: "other", Path: []}`.
2. For each collection `C` (iterate collections in the order they appear
   in the merged YAML document — Go's `yaml.v3` preserves map key order
   when decoding into an explicit ordered structure; the parser MUST
   decode `collections` into an order-preserving structure, e.g. a
   `yaml.Node`-walked slice of `(name, Collection)` pairs rather than a
   plain Go map, specifically to make this ordering deterministic):
   1. Pick the target root node by `C.Root` (default `"other"`).
   2. If `C.Folder` is set, create/find a child folder node named
      `C.Folder.Name` under that root and descend into it; else attach
      directly to the root.
   3. Recursively append `C.Bookmarks`, then `C.Folders`
      (inline groups), then `C.Templates` (resolved groups) — see §6.2
      for the canonical ordering rule.
3. **Normalize**: walk the whole tree and, for every folder, if two or
   more children folders share the same `Name` (this can legitimately
   happen when two `TemplateRef`s emit sibling folders with identical
   names, e.g. two instantiations of a template that both default to the
   same folder name before `as:` is applied), merge them into a single
   folder node preserving the concatenated, ordered children (per the
   `E_DUPLICATE_SIBLING` rule in §5.2, this only applies to folders;
   duplicate bookmark name+url pairs are deduplicated into one node,
   differing url is already a validation error so never reaches here).
4. Assign `Index` to every node = its position in its parent's `Children`
   slice after normalization.
5. Compute and set `Path` on every node during the walk (parent's `Path +
   [parent.Name]`).

### 6.2 Ordering Rules (canonical, must be followed exactly)

Within any single folder's children, the render engine emits, in this
fixed order:

1. All directly declared `Bookmarks` (in YAML declaration order).
2. All directly declared inline `Folders` / `NamedGroup`s (in YAML
   declaration order), each recursively rendered.
3. All `Templates` (`TemplateRef`s) (in YAML declaration order). A
   folder-less template's flattened output (bookmarks + nested folders)
   is spliced in at this position, in the template's own internal order
   (which itself follows this same three-step rule, recursively).

This means bookmarks always sort before folders at the same nesting
level unless the folder came from a `TemplateRef` that appears (in
declaration order) after some inline bookmark — ordering is always
"declaration order within category, categories in the fixed sequence
bookmarks -> inline folders -> template-produced content." This matches
the worked example in §3.2 (Company Wiki, an inline bookmark, would sort
before templates only if declared textually before them in a real file;
in the example it is declared after the `templates:` list at the
`Collection` level, and per the category-based rule above, **all
directly-declared bookmarks of a node always render before that same
node's templates regardless of YAML key order**, since `bookmarks:` and
`templates:` are sibling keys of the same node, not children of each
other — implementers must apply the fixed category order 1-2-3 above,
not raw YAML text order, for children of the *same* parent node).

The diff engine (§7) treats `Index` purely as a MOVE signal, never as
part of node identity/matching.

## 7. Diff Engine

Implemented in `cli/diff/diff.go` and `match.go`. Input: two
`*bookmarktree.BookmarkTree` values — `desired` (from the renderer) and
`actual` (read from the browser's Bookmarks file by `cli/browserfile`,
with `BrowserID` populated on every node from the file's real `id`
fields). Output: `Plan` — an ordered `[]Operation`. The engine itself has
no notion of where `actual` came from; it just diffs two trees.

```go
package diff

type OpType string

const (
	OpCreate OpType = "CREATE"
	OpUpdate OpType = "UPDATE"
	OpDelete OpType = "DELETE"
	OpMove   OpType = "MOVE"
)

// Operation is one atomic, idempotent action for browserfile to apply to
// the browser's Bookmarks file. Fields are populated according to Type;
// see the table below.
type Operation struct {
	Type OpType `json:"type"`

	// TargetPath is the desired ancestor path (folder names only, root
	// first) of the node being acted on — used for CREATE where no
	// BrowserID exists yet, and for logging/human display on every op.
	TargetPath []string `json:"targetPath"`

	// Kind: "folder" or "bookmark" (mirrors bookmarktree.NodeKind).
	Kind string `json:"kind"`

	// Name/URL: desired final values. For DELETE, these reflect the
	// actual (about-to-be-removed) node's current values (informational).
	Name string `json:"name"`
	URL  string `json:"url,omitempty"`

	// BrowserID: the actual bookmark node id (as found in the browser's
	// Bookmarks file) this op targets. Empty for CREATE (does not exist yet).
	BrowserID string `json:"browserId,omitempty"`

	// ParentBrowserID: id of the parent folder this node should end up
	// under. For CREATE/MOVE, required. Empty means "one of the two
	// native roots", disambiguated by TargetPath[0] ("bar"/"other").
	ParentBrowserID string `json:"parentBrowserId,omitempty"`

	// Position: desired 0-based index among new siblings, set for
	// CREATE and MOVE so browserfile can insert the node at the right
	// index among its new siblings.
	Position int `json:"position"`

	// Changes lists which fields differ, for UPDATE only: subset of
	// "name", "url".
	Changes []string `json:"changes,omitempty"`
}

type Plan struct {
	GeneratedAt string      `json:"generatedAt"` // RFC3339
	Operations  []Operation `json:"operations"`
}
```

### 7.1 Matching Strategy

Chrome bookmark IDs don't exist for desired nodes (they aren't created
yet), so nodes are matched between `desired` and `actual` trees by
**structural identity**, computed top-down, parent-before-child (a
node's identity depends on its already-resolved parent match, not on
absolute path alone, so that folder renames don't cascade-break all
descendants):

1. Start at the two roots (`bar`, `bar`) and (`other`, `other`) — these
   are always matched to each other unconditionally (Chrome guarantees
   their IDs, `"1"` and `"2"`, and their `Name`/order are not managed by
   Marko).
2. For a matched pair of parent folders `(D, A)` (desired, actual), match
   their children using this precedence, consuming each `actual` child at
   most once:
   1. **Exact match**: same `Kind`, same `Name`, and (for bookmarks) same
      `URL`. This is the strongest signal and is preferred even if
      position differs.
   2. **Rename match** (folders only): same `Kind == folder`, and exactly
      one unmatched actual folder child remains whose Name differs but
      whose *entire immediate child set* (by exact-match on grandchildren
      `Kind`+`Name`(+`URL`)) overlaps with the desired folder's children
      by more than 50%. This heuristic allows a folder rename in
      `marko.yaml` to produce an `UPDATE` (rename) rather than a
      `DELETE`+`CREATE` pair, as long as its contents are recognizably
      the same. If no such single best candidate exists (ambiguous or no
      overlap), fall through to no match.
   3. **URL match** (bookmarks only, fallback): same `Kind == bookmark`,
      same `URL`, different `Name` -> treated as matched (an `UPDATE`
      with `Changes: ["name"]`), since URL is a stronger identity signal
      for a bookmark than its display title.
   4. Anything in `desired` left unmatched after 2.1-2.3 -> `CREATE`.
   5. Anything in `actual` left unmatched after 2.1-2.3 -> `DELETE`.
3. Recurse into each matched folder pair to match their children (step
   2), building the full pairing bottom-up... top-down in traversal,
   bottom-up in that a folder's DELETE is only emitted for the folder
   node itself — Chrome's `bookmarks.removeTree` recursively removes
   children, so child DELETE operations for descendants of a
   to-be-deleted folder are **not individually emitted**; only the
   top-most unmatched folder gets a single `DELETE`.
4. For every matched pair (folder or bookmark), compare fields:
   - `Name` differs -> `UPDATE` with `Changes` including `"name"`.
   - `URL` differs (bookmarks only) -> `UPDATE` with `Changes` including
     `"url"`.
   - If matched to a *different* parent than its previous actual parent,
     or its actual `Index` among siblings-after-this-plan-applies would
     differ from `desired.Index` -> `MOVE` (see 7.2; `MOVE` and `UPDATE`
     are not mutually exclusive — a node can need both; emit them as two
     separate `Operation`s in the plan, `MOVE` first, so the code applying
     the plan always resolves parentage before renaming).
5. Unmatched desired folders that are `CREATE`d are recursed into
   immediately (their children can only be `CREATE`, never matched to
   anything in `actual`, since the parent itself doesn't exist yet).

### 7.2 Move Detection

A `MOVE` is emitted when either:
- `ParentBrowserID` (mapped from the matched parent) differs from the
  node's current actual parent id, or
- The parent is unchanged but the desired 0-based position among final
  siblings differs from the current actual position **after** accounting
  for other CREATE/DELETE ops already changing that sibling set (i.e.
  position is computed against the *final* desired sibling order, not
  the stale actual order) — implementers compute this by, per parent,
  laying out the final desired children order and diffing index-by-index
  against a same-length view of actual survivors (post CREATE/DELETE)
  augmented with placeholders for not-yet-created nodes; any actual
  survivor node whose slot differs gets `MOVE`.

### 7.3 Operation Ordering in the Plan

The final `Plan.Operations` slice is ordered so it is safe to apply
sequentially, top-down:

1. All `DELETE`s first, deepest-path-last-among-deletes is irrelevant
   since `removeTree` is recursive — but `DELETE`s are still emitted
   parent-before-descendant order is moot (only top-most unmatched
   folder emits one DELETE per §7.1.3); multiple independent DELETEs are
   ordered by ascending `len(TargetPath)` (shallowest first) purely for
   readable output, not correctness.
2. Then all `CREATE`s for folders, ordered breadth-first (shallowest
   `TargetPath` first, so parent folders exist before their children need
   them) — bookmark CREATEs interleave after their parent folder's CREATE
   in the same breadth-first pass.
3. Then all `MOVE`s.
4. Then all `UPDATE`s (renames are safe last since they don't affect
   parentage).

This ordering guarantees the plan can be applied strictly in array order
with a single forward pass and never reference a `ParentBrowserID` that
doesn't exist yet (newly created folders' real ids are threaded forward
at apply-time — see §8.4, `browserfile` substitutes real ids as it goes
using a local map keyed by `TargetPath`, since the CLI cannot know real
ids for not-yet-created nodes in advance).

## 8. Browser Bridge: direct Bookmarks file access

`marko sync` (`cli/browserfile`) reads and writes the target browser's
native `Bookmarks` file directly. There is no browser extension, no
local server, and no wire protocol — the "bridge" is filesystem I/O.
(An earlier iteration drove a Chrome extension over a local HTTP bridge
instead; it was removed after real-world testing found problems
inherent to going through a browser extension at all — see
`docs/sync-protocol.md`'s introduction for what those were. Nothing in
the current codebase depends on it.)

### 8.1 Locating the file

`--bookmarks-file <path>` if given; otherwise `--browser <name>` (one of
`brave` (default), `chrome`, `chromium`, `edge`) + `--profile <name>`
(default `Default`), resolved to the OS-appropriate path (e.g. on macOS,
`~/Library/Application Support/BraveSoftware/Brave-Browser/<profile>/Bookmarks`
for Brave; see `cli/browserfile/paths.go` for the full per-OS table).

### 8.2 Safety: is the browser running?

Chromium creates a `SingletonLock` file/symlink in its top-level
user-data directory (the parent of the profile directory) while running,
and removes it on clean shutdown. `marko sync` checks for this
(`browserfile.IsBrowserRunning`) and refuses to proceed unless `--force`
is passed, since Chromium periodically flushes its in-memory bookmark
model back to this file and would otherwise silently overwrite Marko's
change. With `--force`, a warning is printed to stderr and the write
proceeds anyway.

### 8.3 Reading

The file is parsed into a generic `map[string]interface{}` tree (not a
fixed struct), so every field Marko doesn't explicitly model — Brave's
`meta_info`, `date_last_used`, the entire `synced`/mobile-bookmarks root,
future additions — survives a read-modify-write round trip completely
untouched. `File.ToBookmarkTree()` converts the `bookmark_bar`/`other`
roots into a `bookmarktree.BookmarkTree` (the "actual" state), reading
each root's real `id` field directly from the file — **never** assuming
a fixed value like `"1"`/`"2"`, since real-world profiles have been
observed with `"other"` at a different id (e.g. `"3"`).

### 8.4 Diffing and applying

The resulting `BookmarkTree` is diffed against the rendered desired
state using the exact same `diff.Diff` engine as every other Marko
command (§7) — its contract is just "two `BookmarkTree` values in, a
`Plan` out," so it has no idea whether "actual" came from a browser
extension or a parsed file. Unless `--preview`, `File.Apply(plan)`
mutates the parsed structure in place:

- **CREATE**: assigns a fresh sequential id (one past the largest id
  found anywhere in the file, including the untouched `synced` root,
  so ids can never collide), a random v4 GUID, and a WebKit-epoch
  timestamp (microseconds since 1601-01-01, Chromium's convention).
  Not-yet-created parent folders are resolved via a `targetPath ->
  id` map built up within the same `Apply` pass, exactly like the
  now-removed extension bridge's `resolveParentBrowserId` did.
- **DELETE**: removes the node (and, since JSON nesting already implies
  the whole subtree, everything under it) from its parent's `children`.
- **MOVE**: removes the node from its old parent's `children` and
  inserts it into the new parent's at the operation's `Position`.
- **UPDATE**: overwrites `name`/`url` per the operation's `Changes`.

### 8.5 Writing

`File.Write` recomputes the top-level `checksum` field (a best-effort
MD5 over each node's id/name(/url), depth-first across `bookmark_bar`,
`other`, `synced`) — confirmed during development to not be strictly
enforced by Chromium-family browsers on load, so an exact match to
Chromium's own algorithm is not load-bearing — then backs up the
original file's current on-disk content to
`<path>.marko-backup-<unix-timestamp>` and writes the new content to a
temp file in the same directory, `fsync`ed and renamed over the
original (atomic on POSIX).

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

## 9. CLI Command Specs

Global flags (all commands): `--config <path>` (default: search upward
from cwd for `marko.yaml`), `--templates-dir <path>` (default:
`<dir of marko.yaml>/templates`), `-v/--verbose`, `--json` (machine
readable output where applicable).

Exit codes (shared convention across all commands):
`0` success, `1` validation/runtime error, `2` usage error (bad flags/args), `3` I/O error (file not found/unreadable).

### 9.1 `marko init`
Scaffolds a starter `marko.yaml` (+ empty `templates/` dir) in the target
directory.

Flags: `--dir <path>` (default `.`), `--force` (overwrite existing file).

Stdout: path of the created file. Stdin: none.

```
$ marko init
Created marko.yaml
Created templates/
```
Exit `2` if `marko.yaml` already exists and `--force` not passed.

### 9.2 `marko validate`
Runs Phase A + Phase B (§5). Prints each finding one per line:
`<code>: <message> (at <location>)`, errors to stderr, warnings to
stderr prefixed `warning:`. With `--json`, prints a single JSON array of
finding objects instead.

```
$ marko validate
E_UNDEFINED_VARIABLE: undefined variable "repo_org" (at templates.repository.folder.name)
$ echo $?
1
```
```
$ marko validate
marko.yaml is valid (0 errors, 1 warning)
$ echo $?
0
```
Exit `0` only if zero `E_*` findings (warnings alone still exit `0`).

### 9.3 `marko render`
Runs full pipeline through §6, prints the resulting `BookmarkTree`.
Default output: an indented tree view to stdout. `--json` prints the raw
`bookmarktree.BookmarkTree` JSON (§6 struct). `--out <file>` writes to a
file instead of stdout.

```
$ marko render
Bookmarks Bar
└── Work
    ├── Kubernetes
    │   ├── Documentation
    │   ├── GitHub
    │   ├── Dashboard
    │   └── Grafana
    ...
```
Exit `1` if validation fails first (render always validates internally).

### 9.4 `marko diff`
A read-only preview: `--actual <file>` accepts a previously-captured
`actualTree` JSON (the same `bookmarktree.BookmarkTree` shape used
everywhere else) and diffs it against the desired state without writing
anything. `--actual` is required — `marko diff` never reads the browser
itself; `marko sync --preview` is the more convenient way to see this
same plan computed directly from the browser's real `Bookmarks` file.

```
$ marko diff --actual browser-state.json
CREATE  folder    bar/Work/Kubernetes
CREATE  bookmark  bar/Work/Kubernetes/Documentation
UPDATE  bookmark  bar/Work/Company Wiki  (url changed)
DELETE  folder    other/Old Project
```
`--json` prints the `diff.Plan` JSON. Exit `0` even if operations are
non-empty (a non-empty diff is not an error); exit `1` only on
validation/parse errors reading `--actual` or `marko.yaml`.

### 9.5 `marko sync`
Reads and writes the target browser's native `Bookmarks` file directly
(`cli/browserfile`, §8) — no extension, no server. Flags: `--browser
<name>` (default `brave`; also `chrome`, `chromium`, `edge`), `--profile
<name>` (default `Default`), `--bookmarks-file <path>` (explicit
override, skips `--browser`/`--profile` lookup), `--force` (write even
if the browser looks like it's running for that profile; prints a
warning instead of refusing), `--preview` (compute and log the plan
without writing). The browser must be closed (or `--force` passed) since
Chromium periodically flushes its own in-memory bookmark state back to
this file, which would silently overwrite Marko's changes otherwise. A
timestamped backup of the previous file content is always written
alongside it before any change, and the full computed plan is always
logged before anything is written — "what was imported, what was
deleted," not just a summary count.

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

## 10. Testing Strategy

### 10.1 Unit tests (Go, standard `testing` package, table-driven)

- `cli/parser`: valid minimal YAML parses to expected `model.Config`;
  malformed YAML returns a wrapped error with line/column; multiple
  `templates/*.yaml` files merge correctly; duplicate template name
  across files is caught (or surfaced for validator to catch — parser
  vs validator boundary must be tested explicitly for this case).
- `cli/template`: variable substitution (simple case, missing variable,
  disallowed syntax rejected); nested template flattening (folder-less)
  vs nesting (with folder); `as:` rename; inheritance merge order;
  `extends` cycle detection with exact cycle path in error; nested
  `TemplateRef` cycle detection; variable precedence order (all 4
  levels from §4.3, each overriding the next).
- `cli/validator`: one test per row in §5.1 and §5.2 tables, each
  asserting exact error code and that message contains the expected
  location string.
- `cli/renderer`: ordering rule (§6.2) with a fixture mixing bookmarks +
  inline folders + templates at the same level; duplicate-named sibling
  folder merge; duplicate bookmark dedup vs `E_DUPLICATE_SIBLING` on
  differing URL (renderer relies on validator having already rejected
  this, but renderer should defensively test it doesn't panic).
- `cli/diff`: each of exact-match, rename-match, URL-match, CREATE
  (no actual match), DELETE (no desired match), MOVE (parent change),
  MOVE (position change), UPDATE (name/url change), MOVE+UPDATE combined
  on the same node; recursive-delete (folder DELETE doesn't also emit
  child DELETEs); operation ordering (§7.3) asserted on a fixture
  requiring all four op types simultaneously.
- `cli/browserfile`: parses a fixture Chromium `Bookmarks` file (with a
  non-standard root id and unknown per-node fields, mirroring real-world
  data observed during development), converts it to a `BookmarkTree`,
  applies a plan covering CREATE/UPDATE/DELETE/MOVE, writes it back, and
  re-reads it to assert: real ids are preserved (never hardcoded),
  unknown fields and the untouched `synced` root survive unmodified, new
  nodes get fresh non-colliding ids, and a re-diff against the written
  result is empty (idempotency). Also covers path resolution
  (`LocateBookmarksFile`) and running-browser detection
  (`IsBrowserRunning`) via a fixture `SingletonLock`.

### 10.2 Integration test

Location: `cli/renderer` + `cli/diff` combined test, or a dedicated
`cli/internal/integration_test.go` (build-tagged `integration` if it
needs to shell out to the built binary; otherwise a plain Go test calling
package functions directly is preferred and sufficient — no actual Chrome
browser is driven in CI).

Scenario, asserting each stage's output feeds the next correctly:
1. Load the fixture YAML at `examples/full/marko.yaml` (parser).
2. Resolve templates (template engine) — assert zero validation errors.
3. Render to `BookmarkTree` (renderer) — assert exact expected tree
   structure (golden JSON fixture comparison).
4. Simulate an empty browser: construct a synthetic **empty** actual
   `BookmarkTree` (just the two empty `bar`/`other` roots) and run the
   diff engine — assert the resulting `Plan` contains exactly one
   `CREATE` per node in the desired tree, in valid breadth-first
   dependency order (a parent's CREATE never appears after its child's).
5. Simulate "apply": a small in-memory fake of the mutation target
   (a Go struct implementing create/update/remove/move against a mutable
   tree, the same shape `cli/browserfile` mutates for real) applies the
   `Plan` op-by-op; assert the resulting tree exactly equals the desired
   tree (structural equality, ignoring `BrowserID`/`Index` bookkeeping
   fields).
6. Re-run diff (desired vs the now-"synced" tree) — assert the resulting
   `Plan.Operations` is empty, proving idempotency.

## 11. Out of Scope / Deferred

- Only Chromium-family browsers (Brave, Chrome, Chromium, Edge — anything
  that uses the same `Bookmarks` JSON file format) are supported.
  Firefox uses a completely different (SQLite-based) storage format and
  would need its own `cli/browserfile` implementation; none exists yet.
- No cloud sync, no multi-device sync, no remote storage of
  `marko.yaml` or bookmark state — everything is local-file and
  local-browser only.
- No two-way sync: Marko never reads the browser's current state back
  into `marko.yaml`. `marko diff --actual` is a one-off convenience for
  previewing against a captured snapshot, not a reconciliation feature,
  and is explicitly not meant to be round-tripped back into authored
  YAML automatically.
- No conflict resolution UI for concurrent edits (if the user edits
  bookmarks directly between rendering the plan and `marko sync`
  writing it, last-write-wins; no optimistic-lock/versioning) — this is
  why the browser must be closed while `marko sync` runs.
- No support for bookmark favicons, tags, or Chrome's "Managed
  bookmarks" enterprise policy tree — only title + URL + folder
  structure under the Bookmarks Bar / Other Bookmarks roots.
- No package/distribution automation (Homebrew formula, etc.) — local
  build/install only.
- Template engine has no macro system, no partials-with-parameters
  beyond what `Vars` + nesting already provide, and no conditional
  logic of any kind (by design, per spec §16).
