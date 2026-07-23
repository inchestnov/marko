# `marko.yaml` Schema Reference

This is the complete field-by-field reference for `marko.yaml` and files
under `templates/`. It mirrors the Go types in
[`cli/internal/model/model.go`](../cli/internal/model/model.go), which is
the single source of truth for the schema. For the *semantics* of
templates (variable precedence, nesting, composition, inheritance), see
[`docs/templates.md`](./templates.md). For the full binding contract
(validation rules, render/diff algorithms, sync protocol), see
[`docs/architecture.md`](./architecture.md).

All values are plain YAML scalars/maps/lists — there is no custom tag or
anchor magic. The only "dynamic" syntax is the `{{ .name }}` variable
placeholder, which may appear in `Bookmark.name`, `Bookmark.url`,
`Folder.name`, and `TemplateRef.vars` values (see
[templates.md](./templates.md#placeholder-syntax)).

## Config (top level of `marko.yaml`)

| Field | Type | Required | Notes |
|---|---|---|---|
| `version` | string | yes | Schema version. Currently always `"1"`. |
| `variables` | map of name -> [Variable](#variable) | no | Global variables with defaults, visible to collections directly and to templates under the rules in [templates.md](./templates.md#global-variables). |
| `templates` | map of name -> [Template](#template) | no | The reusable template library. May also be split across files under `templates/` (or the directory passed via `--templates-dir`); all discovered files are merged with `marko.yaml`'s own `templates:` block. A template name declared twice (across files or within the same file) is a validation error (`E_DUPLICATE_TEMPLATE`). |
| `collections` | map of name -> [Collection](#collection) | yes (non-empty) | Rendered in **YAML declaration order** — this is deterministic and is exactly the order collections appear in the merged document (`cli/internal/model/model.go`'s `CollectionMap` preserves key order via a custom `UnmarshalYAML`). |

Example:

```yaml
version: "1"

variables:
  company_domain:
    default: company.com

templates:
  kubernetes:
    folder:
      name: Kubernetes
    bookmarks:
      - name: Documentation
        url: "https://kubernetes.io/docs/"

collections:
  work:
    root: bar
    templates:
      - template: kubernetes
```

## Variable

Declares a named, typed placeholder with an optional default. Used both
at the top level (`Config.variables`) and inside a template
(`Template.vars`).

| Field | Type | Required | Notes |
|---|---|---|---|
| `default` | string | no | A literal value, or itself a `{{ .name }}` expression referencing another variable already in scope (see [templates.md](./templates.md#sibling-defaults)). Mutually exclusive with `required: true` (`E_INVALID_VARIABLE_DECL` if both are set). |
| `description` | string | no | Free-text documentation; not used by any tooling logic. |
| `type` | string | no | `"string"` (default) or `"url"`. Advisory metadata only — currently used by the validator as documentation; it does not change how the value is substituted (all values are strings after substitution). |
| `required` | bool | no | If `true` and no value is ever supplied (via `TemplateRef.vars`, a template default, or an eligible global default), resolution fails with `E_MISSING_VARIABLE`. |

Example (template-scoped, required, no default):

```yaml
templates:
  profile:
    vars:
      username:
        required: true
        type: string
```

Example (template-scoped, with a default that references a sibling variable):

```yaml
templates:
  repository:
    vars:
      username:
        required: true
      repo_org:
        default: "{{ .username }}"
```

Example (global, with a plain default):

```yaml
variables:
  company_domain:
    default: company.com
    type: string
```

## Template

A reusable, named subtree of folders/bookmarks/nested templates. A
template is never rendered on its own — it must be instantiated via a
[TemplateRef](#templateref) inside a `Collection` or another `Template`.

| Field | Type | Required | Notes |
|---|---|---|---|
| `extends` | string | no | Single inheritance: names another template whose `bookmarks`, `templates`, and `vars` are merged in first (base first), then this template's own entries are appended (`vars` overlaid, self wins on key collision). A cycle (`a extends b extends a`) is a validation error (`E_CYCLE_EXTENDS`). |
| `vars` | map of name -> [Variable](#variable) | no | The variables this template accepts. A template can only ever read a global `variables:` default for a name it also declares here (see [templates.md](./templates.md#global-variables)). |
| `folder` | [Folder](#folder) | no | If set, this template's contents are wrapped in a named folder when instantiated. If omitted, contents are flattened directly into the parent at the `TemplateRef`'s position — this is how composition mixins (e.g. `profile` inside `repository`) work. |
| `bookmarks` | list of [Bookmark](#bookmark) | no | This template's own direct bookmarks. |
| `templates` | list of [TemplateRef](#templateref) | no | Nested template instantiations (composition). Self-reference, directly or transitively, is always an error (`E_CYCLE_TEMPLATE`) — there is no recursion-limit escape hatch, since loops are forbidden by design. |

A template must declare at least one of `bookmarks`/`templates`/`folder`
(`E_EMPTY_TEMPLATE` otherwise).

Example — folder-less (flattens into its caller):

```yaml
templates:
  profile:
    vars:
      username:
        required: true
    bookmarks:
      - name: "{{ .username }} - Profile"
        url: "https://github.com/{{ .username }}"
```

Example — with its own folder, nested templates (composition), and `extends`:

```yaml
templates:
  base-links:
    bookmarks:
      - name: Company Wiki
        url: "https://wiki.company.com"

  repository:
    extends: base-links
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
```

## Folder

An explicit named folder node. Used by `Collection.folder`,
`Template.folder`, and `NamedGroup.folder`.

| Field | Type | Required | Notes |
|---|---|---|---|
| `name` | string | yes (non-empty) | Supports `{{ .name }}` variable interpolation, e.g. `"{{ .repo_name }}"`. |

Example:

```yaml
folder:
  name: "{{ .repo_org }}/{{ .repo_name }}"
```

## Bookmark

A leaf node: a single browser bookmark.

| Field | Type | Required | Notes |
|---|---|---|---|
| `name` | string | yes (non-empty) | Supports variable interpolation. |
| `url` | string | yes (non-empty) | Supports variable interpolation. Once fully resolved (no remaining `{{ }}`), must parse as an absolute URL with scheme `http://`, `https://`, or `chrome://` (`E_INVALID_URL` otherwise). URL-shape checking on a string that still contains a placeholder is deferred until after substitution, since Phase A structural validation runs before resolution. |

Example:

```yaml
bookmarks:
  - name: GitHub
    url: "https://github.com"
```

Two sibling bookmarks with the same `name` but a *different* `url`
under the same parent are a validation error (`E_DUPLICATE_SIBLING`).
Identical `(name, url)` pairs are silently deduplicated into a single
node — this keeps repeated instantiation idempotent.

## TemplateRef

Instantiates a named `Template` at the current position inside a
`Collection`, `Template`, or `NamedGroup`'s `templates:` list. Also
referred to as an "Instance."

| Field | Type | Required | Notes |
|---|---|---|---|
| `template` | string | yes (non-empty) | The name key into `templates:` (merged across `marko.yaml` and every file under `templates/`). Unknown names fail with `E_UNKNOWN_TEMPLATE`. |
| `as` | string | no | Overrides the mount-point folder name for this instantiation. Only meaningful if the referenced template has its own `folder`; on a folder-less template, `as:` is ignored with a non-fatal warning (`W_AS_IGNORED`). |
| `vars` | map of name -> string | no | Concrete values (or overrides) for the variables the referenced template declares. Each value is itself interpolated against the **calling** scope before being bound into the template's own scope (so `vars: { username: "{{ .username }}" }` forwards a variable from the caller down into the callee under a possibly-different name). Keys not declared in the target template's own `vars:` map are ignored with a warning (`W_UNKNOWN_VAR`), not an error. |

Example (renaming the mount folder and forwarding a variable):

```yaml
templates:
  - template: repository
    as: "some-other-project"
    vars:
      username: octocat
      repo_name: some-other-project
      repo_org: some-org
```

## Collection

A top-level named bookmark tree, typically mapped 1:1 to a top-level
Chrome bookmark folder.

| Field | Type | Required | Notes |
|---|---|---|---|
| `root` | string | no | `"bar"` (Bookmarks Bar) or `"other"` (Other Bookmarks). Defaults to `"other"`. Any other value is `E_INVALID_ENUM`. |
| `folder` | [Folder](#folder) | no | If set, the collection's contents nest inside a named folder under `root`. If omitted, contents attach directly under `root`. |
| `bookmarks` | list of [Bookmark](#bookmark) | no | Direct bookmarks, always rendered before this collection's `folders` and `templates` (see [render ordering](#render-ordering-note) below). |
| `templates` | list of [TemplateRef](#templateref) | no | Template instantiations, in declaration order. |
| `folders` | list of [NamedGroup](#namedgroup) | no | Inline sub-folders, in declaration order. |

A collection must declare at least one of `bookmarks`/`templates`/`folders`
(`E_EMPTY_COLLECTION` otherwise). Two collections with case-insensitively
colliding names are `E_DUPLICATE_COLLECTION`.

Example:

```yaml
collections:
  work:
    root: bar
    folder:
      name: Work
    templates:
      - template: kubernetes
    bookmarks:
      - name: Company Wiki
        url: "https://wiki.company.com"

  personal:
    root: other
    bookmarks:
      - name: Gmail
        url: "https://mail.google.com"
```

### Render ordering note

Within any single folder's (or collection's) children, Marko always
emits, in this fixed category order, **regardless of the YAML key order
of `bookmarks:`/`folders:`/`templates:` in the source file**:

1. Directly declared `bookmarks` (in declaration order).
2. Directly declared inline `folders` (`NamedGroup`s, in declaration order).
3. `templates` (`TemplateRef`s, in declaration order); a folder-less
   template's flattened output is spliced in at this position.

This means a collection's own `bookmarks:` list always renders before
its `templates:` list's output in the tree, even if `templates:` is
written first in the YAML file — `bookmarks:` and `templates:` are
sibling keys of the same node, not nested inside each other, so raw
document text order between them is not the ordering signal. See
`docs/architecture.md` §6.2 for the full rule and its interaction with
the (visually similar but non-authoritative) §3.2 flow diagram.

## NamedGroup

An inline (non-template) sub-folder, usable directly inside a
`Collection`, `Template`, or another `NamedGroup`'s `folders:` list, for
one-off structure that doesn't warrant a reusable `Template`.

| Field | Type | Required | Notes |
|---|---|---|---|
| `folder` | [Folder](#folder) | yes | The folder this group renders as. |
| `bookmarks` | list of [Bookmark](#bookmark) | no | Direct bookmarks of this group. |
| `templates` | list of [TemplateRef](#templateref) | no | Template instantiations nested inside this group. |
| `folders` | list of NamedGroup | no | Further nested inline groups. |

Example:

```yaml
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
          - name: Lobsters
            url: "https://lobste.rs"
```

Two folders (whether from `NamedGroup`s, `Template.folder`s, or
`Collection.folder`) that share the same name under the same parent are
always **merged** (children concatenated, never an error) — this is what
lets two separate `TemplateRef`s legitimately produce sibling folders
with the same default name before `as:` disambiguates them.

## See also

- [`docs/templates.md`](./templates.md) — template authoring guide:
  variables, nesting, composition, inheritance, the restricted
  placeholder syntax.
- [`docs/sync-protocol.md`](./sync-protocol.md) — how `marko sync` reads
  and writes the browser's Bookmarks file directly.
- [`docs/architecture.md`](./architecture.md) — the full binding
  technical contract (validation rules, render/diff algorithms).
- [`examples/minimal/marko.yaml`](../examples/minimal/marko.yaml),
  [`examples/full/marko.yaml`](../examples/full/marko.yaml) — worked
  examples validated against the real CLI.
