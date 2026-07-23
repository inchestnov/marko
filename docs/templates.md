# Template Authoring Guide

Templates are Marko's reuse mechanism: a named, parameterized subtree of
folders and bookmarks that can be instantiated — possibly more than
once, with different variables — from a `Collection` or from another
template. This guide builds up from a single bookmark to the full
`profile` + `github` -> `repository` composition example used throughout
the project's docs and examples.

For the field-by-field schema, see
[`docs/yaml-reference.md`](./yaml-reference.md). For the exact resolution
algorithm and variable-precedence rules, see
[`docs/architecture.md`](./architecture.md) §4 — this guide explains the
same rules in a more example-driven way and calls out where the real
implementation (`cli/template/resolve.go`) is slightly broader than the
architecture doc's literal prose.

## 1. A template with no variables

The simplest possible template: a named folder with a fixed set of
bookmarks.

```yaml
templates:
  kubernetes:
    folder:
      name: Kubernetes
    bookmarks:
      - name: Documentation
        url: "https://kubernetes.io/docs/"
      - name: GitHub
        url: "https://github.com/kubernetes/kubernetes"
```

Instantiate it from a collection with a `TemplateRef`:

```yaml
collections:
  work:
    root: bar
    templates:
      - template: kubernetes
```

Renders:

```
Bookmarks Bar
└── Kubernetes
    ├── Documentation
    └── GitHub
```

## 2. Placeholder syntax

Bookmark names/URLs and folder names support variable interpolation
using **exactly one** construct: `{{ .name }}` — a dot followed by a
bare identifier, nothing else. This is enforced by a pre-parse check
(`^\s*\.[A-Za-z_][A-Za-z0-9_]*\s*$` on the trimmed contents between `{{`
and `}}`) before the string is ever handed to a template engine, so:

- `{{ .username }}` — allowed.
- `{{ if .x }}...{{ end }}`, `{{ range .items }}`, `{{ .x | upper }}`,
  any pipeline or function call — **rejected** at validation time with
  `E_TEMPLATE_SYNTAX`.

This is a deliberate, permanent restriction (marko.txt §16): the
template engine is pure data substitution. There is no code execution,
no shell-out, no JS/Python, and — critically — **no looping construct
anywhere in the schema**. If you need the same structure five times,
you write five `TemplateRef` entries (as the `work` collection example
below does with `repository`, twice). This keeps `marko.yaml` fully
static and auditable: what you see in the file is structurally what gets
rendered, just with strings substituted in.

Only `Bookmark.name`, `Bookmark.url`, `Folder.name`, and
`TemplateRef.vars` *values* are ever interpolated. Template names,
variable names, and YAML keys themselves are never interpolated.

## 3. Variables and required values

A template declares the variables it accepts under its own `vars:` map.
Each variable can be `required: true` (must be supplied by every caller,
with no default) or carry a `default` (used when no caller overrides it).
`required` and `default` are mutually exclusive on the same variable.

```yaml
templates:
  profile:
    vars:
      username:
        required: true
        type: string
    bookmarks:
      - name: "{{ .username }} - Profile"
        url: "https://github.com/{{ .username }}"
```

A `TemplateRef` supplies concrete values via `vars:`:

```yaml
collections:
  work:
    templates:
      - template: profile
        vars:
          username: octocat
```

Renders `octocat - Profile` pointing at `https://github.com/octocat`.
Omitting `username` here is a validation error: `E_MISSING_VARIABLE`.

## 4. Nesting and composition

A template's own `templates:` list can reference other templates — this
is how you build bigger structures out of smaller, independently
reusable pieces. Whether a referenced template nests as a sub-folder or
flattens directly into its caller depends on whether *that* template
declares its own `folder:`:

- **Has a `folder:`** -> nests as a sub-folder (named after
  `Folder.name`, or renamed via the `TemplateRef`'s `as:`).
- **No `folder:`** -> flattens: its bookmarks and any nested groups are
  spliced directly into the parent at the position of the `TemplateRef`,
  with `as:` having no effect (and producing a non-fatal `W_AS_IGNORED`
  warning if set anyway).

This is exactly how `repository = profile + github` works (the worked
example from `docs/architecture.md` §3.2 / marko.txt §15, and the
contents of [`templates/profile.yaml`](../templates/profile.yaml),
[`templates/github.yaml`](../templates/github.yaml), and
[`templates/repository.yaml`](../templates/repository.yaml)):

```yaml
templates:
  profile:
    vars:
      username:
        required: true
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
      - template: profile          # folder-less -> flattens into repository's folder
        vars:
          username: "{{ .username }}"
      - template: github           # has a folder -> nests, renamed via "as"
        as: "GitHub Links"
    bookmarks:
      - name: Repository
        url: "https://github.com/{{ .repo_org }}/{{ .repo_name }}"
```

Instantiated twice with different variables in a collection (this is the
`work` collection from `docs/architecture.md` §3.2 and
[`examples/full/marko.yaml`](../examples/full/marko.yaml)):

```yaml
collections:
  work:
    root: bar
    folder:
      name: Work
    templates:
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
```

Renders:

```
Bookmarks Bar
└── Work
    ├── marko
    │   ├── octocat - Profile
    │   ├── GitHub Links
    │   │   ├── Documentation
    │   │   └── GitHub
    │   └── Repository
    └── some-other-project
        ├── octocat - Profile
        ├── GitHub Links
        │   ├── Documentation
        │   └── GitHub
        └── Repository
```

Note `octocat - Profile` renders *before* `Repository` in each folder,
even though `repository`'s own `bookmarks:` key (`Repository`) is written
after its `templates:` key in the YAML above — nested `TemplateRef`
content is category (3) and always spliced after category (1) direct
bookmarks and category (2) inline folders, at the template's own
internal render position. See `docs/yaml-reference.md`'s
[render ordering note](./yaml-reference.md#render-ordering-note).

Self-nesting (a template referencing itself, directly or transitively —
e.g. `repository -> github -> repository`) is always a validation error,
`E_CYCLE_TEMPLATE`, with the full cycle path in the message. There is no
depth limit that would otherwise allow it; loops of any kind are
forbidden by design.

## 5. Sibling variable defaults

A variable's `default` may itself be a `{{ .name }}` expression
referencing another variable on the *same* template — as `repository`'s
`repo_org` does above, defaulting to `{{ .username }}`.

This default expression is evaluated against a scope that **already
includes any explicit override passed at the call site for the sibling
variable**, not just that sibling's own template-level default. In other
words: if a caller does

```yaml
- template: repository
  vars:
    username: octocat
    repo_name: marko
    # repo_org not supplied -> uses default "{{ .username }}" -> "octocat"
```

`repo_org` resolves to `octocat`, following the override. If the caller
instead supplies `repo_org` explicitly (as `some-other-project` above,
with `repo_org: some-org`), that explicit value wins outright — the
default expression is simply never used for that variable. This
"overrides visible to sibling defaults" behavior (implemented in
`cli/template/resolve.go`'s `buildChildScope`) is slightly more specific
than a first read of `docs/architecture.md` §4.3 might suggest; it is
the behavior the real CLI implements and the one to rely on.

## Global variables

Variables declared at the top level under `Config.variables` (not inside
any template) are:

- Directly visible to every `Collection`'s own `bookmarks:`/`folders:`
  bodies (a collection has no `vars:` declaration mechanism of its own —
  it simply inherits every global default), as in
  `docs/architecture.md` §3.2's `Company Wiki` bookmark:

  ```yaml
  variables:
    company_domain:
      default: company.com

  collections:
    work:
      bookmarks:
        - name: Company Wiki
          url: "https://wiki.{{ .company_domain }}"
  ```

- Visible to a **template** in two ways, both implemented in
  `cli/template/resolve.go`:
  1. If the template declares its own `vars:` entry for that name with no
     default, the global default is used to seed it (this is the
     narrower rule `docs/architecture.md` §4.3 describes literally).
  2. **More broadly than the architecture doc's literal §4.3 prose
     states**: any global variable already resolved in an *ancestor*
     scope (ultimately, the top-level `Collection` scope, which always
     carries every global default) remains visible to a nested template
     even if that template does **not** declare a same-named entry in
     its own `vars:` map — as long as the name matches a real global
     `Config.Variables` entry. This is exactly why the `kubernetes`
     template (see [`templates/kubernetes.yaml`](../examples/full/templates/kubernetes.yaml)
     in the full example) can reference `{{ .company_domain }}` directly
     in its bookmark URLs without ever declaring `company_domain` under
     its own `vars:`.

  A template-local variable (declared under some *other* template's
  `vars:`, not `Config.Variables`) never leaks this way — only genuine
  global variables propagate down through ancestor scopes like this.
  Scopes never leak sideways (between sibling `TemplateRef`s) or upward
  (from a nested template back to its caller); only explicit `vars:`
  passed at each `TemplateRef` site, plus this global-variable
  inheritance, cross scope boundaries.

## Inheritance (`extends`)

`extends:` provides single inheritance — one parent template only (use
composition via `templates:` to combine more than one). The base
template's `bookmarks`, `templates`, and `vars` are merged in first (base
entries first, self's appended after; `vars` maps are overlaid with self
winning key collisions; `folder` is self's if set, else the base's),
*before* any instantiation happens.

```yaml
templates:
  base-links:
    bookmarks:
      - name: Company Wiki
        url: "https://wiki.company.com"

  team-links:
    extends: base-links
    bookmarks:
      - name: Team Runbook
        url: "https://runbook.company.com"
```

Instantiating `team-links` yields both `Company Wiki` (inherited, first)
and `Team Runbook` (own, appended). A cycle in the `extends` chain
(`a extends b extends a`) is `E_CYCLE_EXTENDS` with the full cycle path.
A template can both `extends` a base and be referenced via composition
elsewhere — inheritance is fully resolved before any `TemplateRef` is
ever instantiated, so callers never need to think about the difference.

## Splitting templates across files

Templates don't all need to live in `marko.yaml` itself. Any `*.yaml`
file under the templates directory (default `<dir of marko.yaml>/templates`,
overridable with `--templates-dir`) with its own top-level `templates:`
map is merged in. This is how [`templates/`](../templates) and
[`examples/full/templates/`](../examples/full/templates) are organized —
one file per template (`profile.yaml`, `github.yaml`,
`repository.yaml`, `kubernetes.yaml`). Declaring the same template name
in two files (or twice in the same file) is a validation error,
`E_DUPLICATE_TEMPLATE`.

## Validating and rendering

```
$ marko validate --config marko.yaml --templates-dir templates
marko.yaml is valid (0 errors, 0 warnings)

$ marko render --config marko.yaml --templates-dir templates
Bookmarks Bar
└── Work
    ...
```

See [`docs/yaml-reference.md`](./yaml-reference.md) for the full field
reference and [`examples/full/marko.yaml`](../examples/full/marko.yaml)
for a complete, validated worked example combining everything above.
