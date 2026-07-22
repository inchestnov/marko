package template

import (
	"fmt"
	"strings"

	"github.com/inchestnov/marko/cli/internal/model"
)

// ResolvedBookmark is a bookmark with all placeholders substituted to
// literal strings.
type ResolvedBookmark struct {
	Name string
	URL  string
}

// ResolvedGroup is a folder-like node in the resolved tree: either a
// named folder (from a Folder-bearing Template, a Collection.Folder, or
// an inline NamedGroup), containing bookmarks and nested groups in
// canonical order (see docs/architecture.md §6.2).
type ResolvedGroup struct {
	Name      string
	Bookmarks []ResolvedBookmark
	Groups    []*ResolvedGroup
}

// ResolvedCollection is the fully-resolved output for one top-level
// model.Collection: an ordered tree ready for the renderer.
type ResolvedCollection struct {
	Name       string
	Root       string // "bar" | "other"
	FolderName string // empty if the collection has no wrapping folder
	HasFolder  bool
	Bookmarks  []ResolvedBookmark
	Groups     []*ResolvedGroup
}

// Result bundles the resolved collections plus any non-fatal warnings
// accumulated during resolution (W_UNKNOWN_VAR, W_AS_IGNORED).
type Result struct {
	Collections map[string]*ResolvedCollection
	Order       []string // collection names, in declaration order
	Warnings    []*Warning
}

// resolver carries the shared, read-only inputs plus mutable memoization
// state for a single Resolve call.
type resolver struct {
	cfg *model.Config

	// mergedTemplates memoizes the result of applying `extends` (§4.2
	// step 2) per template name.
	mergedTemplates map[string]model.Template
	extendsVisiting map[string]bool
	extendsStack    []string

	warnings []*Warning
}

// Resolve implements the algorithm from docs/architecture.md §4.2: it
// applies template inheritance, then instantiates every TemplateRef
// reachable from every collection, substituting all variable
// placeholders, and returns one ResolvedCollection per collection.
func Resolve(cfg *model.Config) (*Result, error) {
	r := &resolver{
		cfg:             cfg,
		mergedTemplates: make(map[string]model.Template),
		extendsVisiting: make(map[string]bool),
	}

	// Step 2: resolve `extends` for every declared template up front
	// (memoized), so instantiation always sees fully-merged templates.
	for name := range cfg.Templates {
		if _, err := r.mergedTemplate(name); err != nil {
			return nil, err
		}
	}

	result := &Result{Collections: make(map[string]*ResolvedCollection)}

	names := cfg.Collections.Order()
	for _, name := range names {
		col, _ := cfg.Collections.Get(name)
		rc, err := r.resolveCollection(name, col)
		if err != nil {
			return nil, err
		}
		result.Collections[name] = rc
		result.Order = append(result.Order, name)
	}

	result.Warnings = r.warnings
	return result, nil
}

// mergedTemplate returns the template named `name` after applying its
// `extends` chain (§4.2 step 2), memoized in r.mergedTemplates.
func (r *resolver) mergedTemplate(name string) (model.Template, error) {
	if merged, ok := r.mergedTemplates[name]; ok {
		return merged, nil
	}
	tpl, ok := r.cfg.Templates[name]
	if !ok {
		return model.Template{}, &ResolveError{
			Code:     CodeUnknownTemplate,
			Message:  fmt.Sprintf("unknown template %q referenced", name),
			Location: fmt.Sprintf("templates.%s", name),
		}
	}

	if r.extendsVisiting[name] {
		cycle := append(append([]string(nil), r.extendsStack...), name)
		return model.Template{}, &ResolveError{
			Code:     CodeCycleExtends,
			Message:  fmt.Sprintf("cyclic extends: %s", strings.Join(cycle, " -> ")),
			Location: fmt.Sprintf("templates.%s.extends", name),
		}
	}

	if tpl.Extends == "" {
		r.mergedTemplates[name] = tpl
		return tpl, nil
	}

	r.extendsVisiting[name] = true
	r.extendsStack = append(r.extendsStack, name)
	base, err := r.mergedTemplate(tpl.Extends)
	r.extendsStack = r.extendsStack[:len(r.extendsStack)-1]
	delete(r.extendsVisiting, name)
	if err != nil {
		return model.Template{}, err
	}

	merged := mergeTemplate(base, tpl)
	r.mergedTemplates[name] = merged
	return merged, nil
}

// mergeTemplate implements §4.2 step 2's merge order: base first.
func mergeTemplate(base, self model.Template) model.Template {
	merged := model.Template{}

	merged.Bookmarks = append(append([]model.Bookmark(nil), base.Bookmarks...), self.Bookmarks...)
	merged.Templates = append(append([]model.TemplateRef(nil), base.Templates...), self.Templates...)

	vars := make(map[string]model.Variable, len(base.Vars)+len(self.Vars))
	for k, v := range base.Vars {
		vars[k] = v
	}
	for k, v := range self.Vars {
		vars[k] = v
	}
	if len(vars) > 0 {
		merged.Vars = vars
	}

	if self.Folder != nil {
		merged.Folder = self.Folder
	} else {
		merged.Folder = base.Folder
	}

	// Extends itself is not carried forward on the merged result; the
	// chain has already been fully applied.
	return merged
}

// varScope is a resolved (name -> literal string value) mapping used for
// substitution at a given point in the tree.
type varScope map[string]string

// globalScope returns a fresh varScope seeded with every global
// Config.Variables default (values with no default are simply omitted,
// making a bare {{ .name }} reference to them resolve as undefined
// unless some more specific scope provides it). This is the top-level
// scope collections resolve against (see resolveCollection).
func (r *resolver) globalScope() varScope {
	scope := make(varScope, len(r.cfg.Variables))
	for name, v := range r.cfg.Variables {
		if v.Default != "" {
			scope[name] = v.Default
		}
	}
	return scope
}

// fragment is the ordered output of resolving a list of bookmarks/inline
// groups/template refs at one nesting level, following the canonical
// category order from §6.2: bookmarks, then inline folders, then
// template-produced content (each category in declaration order,
// template-produced content spliced in its own internal order when the
// template is folder-less).
type fragment struct {
	Bookmarks []ResolvedBookmark
	Groups    []*ResolvedGroup
}

// resolveCollection resolves one top-level Collection into a
// ResolvedCollection.
func (r *resolver) resolveCollection(name string, col model.Collection) (*ResolvedCollection, error) {
	root := col.Root
	if root == "" {
		root = "other"
	}
	rc := &ResolvedCollection{Name: name, Root: root}

	// Collections sit at the top of the scope chain and have no Vars
	// declaration mechanism of their own (unlike Templates), so they see
	// every global Config.Variables default directly, without needing to
	// "declare" it first -- the §4.3-point-3 restriction ("a template
	// cannot implicitly read a global variable it did not declare")
	// exists specifically to keep reusable Templates self-documenting/
	// portable; it does not apply to the top-level Collection scope,
	// matching the worked example in §3.2 (Company Wiki's URL references
	// {{ .company_domain }} directly at the collection level).
	baseScope := r.globalScope()

	if col.Folder != nil {
		rc.HasFolder = true
		fname, err := substitute(col.Folder.Name, baseScope, fmt.Sprintf("collections.%s.folder.name", name))
		if err != nil {
			return nil, err
		}
		rc.FolderName = fname
	}

	loc := fmt.Sprintf("collections.%s", name)
	frag, err := r.resolveBody(col.Bookmarks, col.Folders, col.Templates, baseScope, nil, loc)
	if err != nil {
		return nil, err
	}
	rc.Bookmarks = frag.Bookmarks
	rc.Groups = frag.Groups
	return rc, nil
}

// resolveBody resolves the three sibling lists (Bookmarks, inline
// Folders/NamedGroups, Templates) that hang off a Collection, Template,
// or NamedGroup body, applying the fixed category order from §6.2:
// 1. directly declared Bookmarks (declaration order)
// 2. directly declared inline Folders/NamedGroups (declaration order)
// 3. Templates (declaration order), folder-less ones flattened in place
func (r *resolver) resolveBody(
	bms []model.Bookmark,
	inline []model.NamedGroup,
	refs []model.TemplateRef,
	scope varScope,
	pathStack []string,
	loc string,
) (*fragment, error) {
	frag := &fragment{}

	resolvedBms, err := r.resolveBookmarks(bms, scope, loc)
	if err != nil {
		return nil, err
	}
	frag.Bookmarks = resolvedBms

	inlineGroups, err := r.resolveNamedGroups(inline, scope, pathStack, loc)
	if err != nil {
		return nil, err
	}
	frag.Groups = append(frag.Groups, inlineGroups...)

	for i, ref := range refs {
		refLoc := fmt.Sprintf("%s.templates[%d]", loc, i)
		wrapped, subFrag, err := r.instantiate(ref, scope, pathStack, refLoc)
		if err != nil {
			return nil, err
		}
		if wrapped != nil {
			frag.Groups = append(frag.Groups, wrapped)
			continue
		}
		// Folder-less: splice the sub-fragment's bookmarks and groups
		// directly into this level's trailing "template-produced
		// content" category, preserving the fragment's own internal
		// bookmarks-then-groups order.
		frag.Bookmarks = append(frag.Bookmarks, subFrag.Bookmarks...)
		frag.Groups = append(frag.Groups, subFrag.Groups...)
	}

	return frag, nil
}

// resolveBookmarks substitutes placeholders in a list of bookmarks using
// scope.
func (r *resolver) resolveBookmarks(bms []model.Bookmark, scope varScope, loc string) ([]ResolvedBookmark, error) {
	out := make([]ResolvedBookmark, 0, len(bms))
	for i, b := range bms {
		name, err := substitute(b.Name, scope, fmt.Sprintf("%s.bookmarks[%d].name", loc, i))
		if err != nil {
			return nil, err
		}
		url, err := substitute(b.URL, scope, fmt.Sprintf("%s.bookmarks[%d].url", loc, i))
		if err != nil {
			return nil, err
		}
		out = append(out, ResolvedBookmark{Name: name, URL: url})
	}
	return out, nil
}

// resolveNamedGroups resolves inline NamedGroup entries (§4.2 step 4).
func (r *resolver) resolveNamedGroups(groups []model.NamedGroup, scope varScope, pathStack []string, loc string) ([]*ResolvedGroup, error) {
	out := make([]*ResolvedGroup, 0, len(groups))
	for i, g := range groups {
		gLoc := fmt.Sprintf("%s.folders[%d]", loc, i)
		name, err := substitute(g.Folder.Name, scope, gLoc+".folder.name")
		if err != nil {
			return nil, err
		}

		subFrag, err := r.resolveBody(g.Bookmarks, g.Folders, g.Templates, scope, pathStack, gLoc)
		if err != nil {
			return nil, err
		}

		out = append(out, &ResolvedGroup{Name: name, Bookmarks: subFrag.Bookmarks, Groups: subFrag.Groups})
	}
	return out, nil
}

// instantiate implements §4.2 step 3: instantiate a single TemplateRef.
// Returns either a non-nil *ResolvedGroup (wrapped: the template has a
// Folder) or, for folder-less templates, the flattened fragment to
// splice directly into the caller (group == nil in that case).
func (r *resolver) instantiate(ref model.TemplateRef, parentScope varScope, pathStack []string, loc string) (*ResolvedGroup, *fragment, error) {
	if ref.Template == "" {
		return nil, nil, &ResolveError{Code: "E_MISSING_FIELD", Message: "template ref missing \"template\" field", Location: loc}
	}

	tpl, ok := r.mergedTemplates[ref.Template]
	if !ok {
		return nil, nil, &ResolveError{
			Code:     CodeUnknownTemplate,
			Message:  fmt.Sprintf("unknown template %q referenced", ref.Template),
			Location: loc,
		}
	}

	for _, seen := range pathStack {
		if seen == ref.Template {
			cycle := append(append([]string(nil), pathStack...), ref.Template)
			return nil, nil, &ResolveError{
				Code:     CodeCycleTemplate,
				Message:  fmt.Sprintf("cyclic template reference: %s", strings.Join(cycle, " -> ")),
				Location: loc,
			}
		}
	}

	childScope, err := r.buildChildScope(ref, tpl, parentScope, loc)
	if err != nil {
		return nil, nil, err
	}

	if tpl.Folder == nil && ref.As != "" {
		r.warnings = append(r.warnings, &Warning{
			Code:     CodeWarnAsIgnored,
			Message:  fmt.Sprintf("\"as\" has no effect on folder-less template %q", ref.Template),
			Location: loc,
		})
	}

	tplLoc := fmt.Sprintf("templates.%s", ref.Template)
	childPathStack := append(append([]string(nil), pathStack...), ref.Template)

	subFrag, err := r.resolveBody(tpl.Bookmarks, nil, tpl.Templates, childScope, childPathStack, tplLoc)
	if err != nil {
		return nil, nil, err
	}

	if tpl.Folder != nil {
		name := ref.As
		if name == "" {
			n, err := substitute(tpl.Folder.Name, childScope, tplLoc+".folder.name")
			if err != nil {
				return nil, nil, err
			}
			name = n
		}
		return &ResolvedGroup{Name: name, Bookmarks: subFrag.Bookmarks, Groups: subFrag.Groups}, nil, nil
	}

	return nil, subFrag, nil
}

// buildChildScope implements the 4-level variable precedence from §4.3.
//
// Level 1 (TemplateRef.Vars) is computed first, even though it is the
// *highest*-precedence source, because Level 2 (a template's own Vars
// defaults) may itself reference a sibling variable declared on the same
// template (e.g. the worked example in §3.2: `repository`'s `repo_org`
// default is "{{ .username }}", and `username` has no template-level
// default of its own -- it is only ever supplied via ref.Vars at the
// instantiation site). Computing explicit overrides up front lets
// default expressions see them, while the final `scope` assignment order
// still ensures ref.Vars wins over template defaults on any actual key
// collision.
func (r *resolver) buildChildScope(ref model.TemplateRef, tpl model.Template, parentScope varScope, loc string) (varScope, error) {
	scope := make(varScope)

	// Level 3 (base layer): every global Config.Variables name already
	// resolved in the calling (parent) scope is inherited as-is. This
	// lets a template reference an unresolved global (e.g. the worked
	// example in §3.2, where the "kubernetes" template uses
	// {{ .company_domain }} without declaring it in its own Vars map)
	// while still keeping unrelated ancestor-template-local variables
	// out of scope (only names that are genuinely global
	// Config.Variables entries propagate this way).
	for name := range r.cfg.Variables {
		if val, ok := parentScope[name]; ok {
			scope[name] = val
		}
	}

	// Level 3b: global Config.Variables default, but ONLY for names the
	// template itself declares in its own Vars map, and only if the
	// template doesn't already provide its own default (level 2 takes
	// precedence and is applied next, overwriting this).
	for name, decl := range tpl.Vars {
		if decl.Default == "" {
			if gv, ok := r.cfg.Variables[name]; ok && gv.Default != "" {
				val, err := substitute(gv.Default, parentScope, loc)
				if err != nil {
					return nil, err
				}
				scope[name] = val
			}
		}
	}

	// Pre-compute Level 1 (TemplateRef.Vars) overrides, each interpolated
	// against the PARENT scope, so Level 2 default expressions below can
	// already see them (see doc comment above).
	explicitOverrides := make(varScope, len(ref.Vars))
	for name, raw := range ref.Vars {
		if _, declared := tpl.Vars[name]; !declared {
			r.warnings = append(r.warnings, &Warning{
				Code:     CodeWarnUnknownVar,
				Message:  fmt.Sprintf("variable %q is not declared by template %q and will be ignored", name, ref.Template),
				Location: loc,
			})
			continue
		}
		val, err := substitute(raw, parentScope, fmt.Sprintf("%s.vars.%s", loc, name))
		if err != nil {
			return nil, err
		}
		explicitOverrides[name] = val
	}

	// Level 2: the referenced Template.Vars[name].Default, evaluated
	// against scope-so-far plus any already-known explicit overrides
	// (but not yet committing overrides for names that have their own
	// default, since Level 1 still wins below on true collisions).
	defaultEvalScope := make(varScope, len(scope)+len(explicitOverrides))
	for k, v := range scope {
		defaultEvalScope[k] = v
	}
	for k, v := range explicitOverrides {
		defaultEvalScope[k] = v
	}
	for name, decl := range tpl.Vars {
		if decl.Default != "" {
			val, err := substitute(decl.Default, defaultEvalScope, fmt.Sprintf("templates.%s.vars.%s", ref.Template, name))
			if err != nil {
				return nil, err
			}
			scope[name] = val
		}
	}

	// Level 1 (highest): commit the explicit overrides, winning over
	// whatever Level 2/3 computed above.
	for name, val := range explicitOverrides {
		scope[name] = val
	}

	// Level 4: required-but-missing check.
	for name, decl := range tpl.Vars {
		if _, ok := scope[name]; !ok && decl.Required {
			return nil, &ResolveError{
				Code:     CodeMissingVariable,
				Message:  fmt.Sprintf("required variable %q not provided for template %q", name, ref.Template),
				Location: loc,
			}
		}
	}

	return scope, nil
}
