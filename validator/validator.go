// Package validator performs static validation of the raw and resolved
// config (Phase A - Structural, Phase B - Semantic). See
// docs/architecture.md §5. validator depends only on internal/model and
// template.
package validator

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/inchestnov/marko/internal/model"
	"github.com/inchestnov/marko/template"
)

// Severity distinguishes fatal errors ("E_*") from non-fatal warnings
// ("W_*").
type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
)

// Error codes not already defined in package template.
const (
	CodeMissingField        = "E_MISSING_FIELD"
	CodeEmptyCollection     = "E_EMPTY_COLLECTION"
	CodeInvalidEnum         = "E_INVALID_ENUM"
	CodeEmptyTemplate       = "E_EMPTY_TEMPLATE"
	CodeInvalidURL          = "E_INVALID_URL"
	CodeInvalidVariableDecl = "E_INVALID_VARIABLE_DECL"
	CodeDuplicateTemplate   = "E_DUPLICATE_TEMPLATE"
	CodeDuplicateCollection = "E_DUPLICATE_COLLECTION"
)

// Finding is one validation result: either an error (E_*) or a warning
// (W_*), matching the "<CODE>: <message> (at <location>)" format from
// docs/architecture.md §5.
type Finding struct {
	Code     string   `json:"code"`
	Message  string   `json:"message"`
	Location string   `json:"location"`
	Severity Severity `json:"severity"`
	Phase    string   `json:"phase"` // "structural" | "semantic"
}

// String renders the finding in the canonical "<CODE>: <message> (at
// <location>)" format.
func (f Finding) String() string {
	if f.Location != "" {
		return fmt.Sprintf("%s: %s (at %s)", f.Code, f.Message, f.Location)
	}
	return fmt.Sprintf("%s: %s", f.Code, f.Message)
}

// HasErrors returns true if any finding in the slice is an E_* (error
// severity) finding.
func HasErrors(findings []Finding) bool {
	for _, f := range findings {
		if f.Severity == SeverityError {
			return true
		}
	}
	return false
}

// Validate runs Phase A (structural) then Phase B (semantic) validation
// and returns every finding collected (not just the first). The returned
// error is only non-nil for conditions that make validation itself
// impossible to run (e.g. nil cfg); ordinary validation failures are
// reported as E_* Finding values, not as the returned error.
func Validate(cfg *model.Config, dupTemplates []model.DuplicateTemplate) ([]Finding, error) {
	if cfg == nil {
		return nil, fmt.Errorf("validator: nil config")
	}

	var findings []Finding

	findings = append(findings, phaseA(cfg, dupTemplates)...)

	// Phase B requires a successful resolution to inspect the expanded
	// tree. Resolution failures themselves are surfaced as findings
	// (translated from the *template.ResolveError the engine returns).
	res, resolveErr := template.Resolve(cfg)
	if resolveErr != nil {
		findings = append(findings, findingFromResolveErr(resolveErr))
		return findings, nil
	}

	for _, w := range res.Warnings {
		findings = append(findings, Finding{
			Code:     w.Code,
			Message:  w.Message,
			Location: w.Location,
			Severity: SeverityWarning,
			Phase:    "semantic",
		})
	}

	findings = append(findings, phaseBDuplicateSiblings(res)...)

	return findings, nil
}

func findingFromResolveErr(err error) Finding {
	if rerr, ok := err.(*template.ResolveError); ok {
		return Finding{
			Code:     rerr.Code,
			Message:  rerr.Message,
			Location: rerr.Location,
			Severity: SeverityError,
			Phase:    "semantic",
		}
	}
	return Finding{
		Code:     "E_UNKNOWN",
		Message:  err.Error(),
		Severity: SeverityError,
		Phase:    "semantic",
	}
}

// ---------------------------------------------------------------------
// Phase A - Structural
// ---------------------------------------------------------------------

func phaseA(cfg *model.Config, dupTemplates []model.DuplicateTemplate) []Finding {
	var findings []Finding

	if cfg.Version == "" {
		findings = append(findings, errF(CodeMissingField, `missing required field "version"`, "version"))
	}
	if cfg.Collections == nil || cfg.Collections.Len() == 0 {
		findings = append(findings, errF(CodeMissingField, `missing required field "collections" (must be a non-empty map)`, "collections"))
	}

	// Duplicate collection names (case-insensitive collisions; exact-key
	// duplicates can't occur post-YAML-decode since map keys are unique,
	// but two differently-cased keys can both exist).
	seen := map[string]string{}
	if cfg.Collections != nil {
		for _, name := range cfg.Collections.Order() {
			lower := strings.ToLower(name)
			if orig, exists := seen[lower]; exists {
				findings = append(findings, errF(CodeDuplicateCollection,
					fmt.Sprintf("duplicate collection name %q (collides with %q)", name, orig),
					fmt.Sprintf("collections.%s", name)))
			} else {
				seen[lower] = name
			}
		}
	}

	if cfg.Collections != nil {
		for _, name := range cfg.Collections.Order() {
			col, _ := cfg.Collections.Get(name)
			findings = append(findings, validateCollection(name, col)...)
		}
	}

	for name, v := range cfg.Variables {
		findings = append(findings, validateVariable(v, fmt.Sprintf("variables.%s", name))...)
	}

	for name, t := range cfg.Templates {
		findings = append(findings, validateTemplate(name, t)...)
	}

	for _, dup := range dupTemplates {
		findings = append(findings, errF(CodeDuplicateTemplate,
			fmt.Sprintf("duplicate template name %q declared in multiple files", dup.Name),
			fmt.Sprintf("templates.%s", dup.Name)))
	}

	return findings
}

func validateCollection(name string, col model.Collection) []Finding {
	var findings []Finding
	loc := fmt.Sprintf("collections.%s", name)

	if len(col.Bookmarks) == 0 && len(col.Templates) == 0 && len(col.Folders) == 0 {
		findings = append(findings, errF(CodeEmptyCollection,
			fmt.Sprintf("collection %q must declare at least one of bookmarks/templates/folders", name), loc))
	}

	if col.Root != "" && col.Root != "bar" && col.Root != "other" {
		findings = append(findings, errF(CodeInvalidEnum,
			fmt.Sprintf(`invalid root %q: must be "bar" or "other"`, col.Root), loc+".root"))
	}

	if col.Folder != nil && col.Folder.Name == "" {
		findings = append(findings, errF(CodeMissingField, "folder.name must be non-empty", loc+".folder.name"))
	}

	for i, b := range col.Bookmarks {
		findings = append(findings, validateBookmark(b, fmt.Sprintf("%s.bookmarks[%d]", loc, i))...)
	}
	for i, ref := range col.Templates {
		findings = append(findings, validateTemplateRef(ref, fmt.Sprintf("%s.templates[%d]", loc, i))...)
	}
	for i, g := range col.Folders {
		findings = append(findings, validateNamedGroup(g, fmt.Sprintf("%s.folders[%d]", loc, i))...)
	}

	return findings
}

func validateNamedGroup(g model.NamedGroup, loc string) []Finding {
	var findings []Finding
	if g.Folder.Name == "" {
		findings = append(findings, errF(CodeMissingField, "folder.name must be non-empty", loc+".folder.name"))
	}
	for i, b := range g.Bookmarks {
		findings = append(findings, validateBookmark(b, fmt.Sprintf("%s.bookmarks[%d]", loc, i))...)
	}
	for i, ref := range g.Templates {
		findings = append(findings, validateTemplateRef(ref, fmt.Sprintf("%s.templates[%d]", loc, i))...)
	}
	for i, sub := range g.Folders {
		findings = append(findings, validateNamedGroup(sub, fmt.Sprintf("%s.folders[%d]", loc, i))...)
	}
	return findings
}

func validateTemplate(name string, t model.Template) []Finding {
	var findings []Finding
	loc := fmt.Sprintf("templates.%s", name)

	if len(t.Bookmarks) == 0 && len(t.Templates) == 0 && t.Folder == nil {
		findings = append(findings, errF(CodeEmptyTemplate,
			fmt.Sprintf("template %q must declare at least one of bookmarks/templates/folder", name), loc))
	}

	if t.Folder != nil && t.Folder.Name == "" {
		findings = append(findings, errF(CodeMissingField, "folder.name must be non-empty", loc+".folder.name"))
	}

	for i, b := range t.Bookmarks {
		findings = append(findings, validateBookmark(b, fmt.Sprintf("%s.bookmarks[%d]", loc, i))...)
	}
	for i, ref := range t.Templates {
		findings = append(findings, validateTemplateRef(ref, fmt.Sprintf("%s.templates[%d]", loc, i))...)
	}
	for varName, v := range t.Vars {
		findings = append(findings, validateVariable(v, fmt.Sprintf("%s.vars.%s", loc, varName))...)
	}

	return findings
}

func validateBookmark(b model.Bookmark, loc string) []Finding {
	var findings []Finding
	if b.Name == "" {
		findings = append(findings, errF(CodeMissingField, "bookmark name must be non-empty", loc+".name"))
	}
	if b.URL == "" {
		findings = append(findings, errF(CodeInvalidURL, "bookmark url must be non-empty", loc+".url"))
	} else if !containsPlaceholder(b.URL) {
		if !isValidBookmarkURL(b.URL) {
			findings = append(findings, errF(CodeInvalidURL,
				fmt.Sprintf("invalid url %q: must be an absolute http://, https://, file://, or chrome:// URL", b.URL), loc+".url"))
		}
	}
	return findings
}

// containsPlaceholder reports whether s contains a {{ }} placeholder;
// URL-shape validation for such strings is deferred until after
// resolution substitutes the placeholder (Phase A can't know the final
// value yet).
func containsPlaceholder(s string) bool {
	return strings.Contains(s, "{{")
}

func isValidBookmarkURL(s string) bool {
	u, err := url.Parse(s)
	if err != nil {
		return false
	}
	switch u.Scheme {
	case "http", "https":
		return u.Host != ""
	case "chrome", "file":
		return true
	default:
		return false
	}
}

func validateTemplateRef(ref model.TemplateRef, loc string) []Finding {
	var findings []Finding
	if ref.Template == "" {
		findings = append(findings, errF(CodeMissingField, `template ref must set "template"`, loc+".template"))
	}
	return findings
}

func validateVariable(v model.Variable, loc string) []Finding {
	var findings []Finding
	if v.Required && v.Default != "" {
		findings = append(findings, errF(CodeInvalidVariableDecl,
			`variable cannot set both "required: true" and a "default" value`, loc))
	}
	return findings
}

func errF(code, msg, loc string) Finding {
	return Finding{Code: code, Message: msg, Location: loc, Severity: SeverityError, Phase: "structural"}
}

func errFPhase(code, msg, loc, phase string) Finding {
	return Finding{Code: code, Message: msg, Location: loc, Severity: SeverityError, Phase: phase}
}

// ---------------------------------------------------------------------
// Phase B - Semantic: E_DUPLICATE_SIBLING
// ---------------------------------------------------------------------

// phaseBDuplicateSiblings walks every resolved collection looking for
// sibling bookmarks that share a Name but differ in URL (an error per
// §5.2); same-name-and-URL bookmarks are silently deduplicated
// (idempotent) elsewhere (by the renderer) and are not flagged here.
// Folders with duplicate names are always merged (never an error), so
// only bookmark/bookmark collisions are checked.
func phaseBDuplicateSiblings(res *template.Result) []Finding {
	var findings []Finding
	for _, name := range res.Order {
		rc := res.Collections[name]
		loc := fmt.Sprintf("collections.%s", name)
		findings = append(findings, checkSiblingBookmarks(rc.Bookmarks, loc)...)
		for _, g := range rc.Groups {
			findings = append(findings, checkGroupSiblings(g, loc)...)
		}
	}
	return findings
}

func checkGroupSiblings(g *template.ResolvedGroup, parentLoc string) []Finding {
	var findings []Finding
	loc := fmt.Sprintf("%s.%s", parentLoc, g.Name)
	findings = append(findings, checkSiblingBookmarks(g.Bookmarks, loc)...)
	for _, child := range g.Groups {
		findings = append(findings, checkGroupSiblings(child, loc)...)
	}
	return findings
}

func checkSiblingBookmarks(bms []template.ResolvedBookmark, parentLoc string) []Finding {
	var findings []Finding
	byName := map[string]string{} // name -> first-seen URL
	for _, b := range bms {
		if prevURL, exists := byName[b.Name]; exists {
			if prevURL != b.URL {
				findings = append(findings, errFPhase(template.CodeDuplicateSibling,
					fmt.Sprintf("duplicate bookmark %q under %s", b.Name, parentLoc), parentLoc, "semantic"))
			}
			// identical (name, url): silently deduplicated, not an error.
			continue
		}
		byName[b.Name] = b.URL
	}
	return findings
}
