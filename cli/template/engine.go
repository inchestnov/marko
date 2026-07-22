// Package template implements the template resolution engine (nesting,
// variable substitution, composition, inheritance). See
// docs/architecture.md §4. template depends only on internal/model.
package template

import (
	"fmt"
	"regexp"
	"strings"
	"text/template"
)

// Error codes shared across the template engine and validator packages
// (docs/architecture.md §5).
const (
	CodeUnknownTemplate  = "E_UNKNOWN_TEMPLATE"
	CodeUndefinedVar     = "E_UNDEFINED_VARIABLE"
	CodeMissingVariable  = "E_MISSING_VARIABLE"
	CodeCycleExtends     = "E_CYCLE_EXTENDS"
	CodeCycleTemplate    = "E_CYCLE_TEMPLATE"
	CodeTemplateSyntax   = "E_TEMPLATE_SYNTAX"
	CodeWarnUnknownVar   = "W_UNKNOWN_VAR"
	CodeWarnAsIgnored    = "W_AS_IGNORED"
	CodeDuplicateSibling = "E_DUPLICATE_SIBLING"
)

// ResolveError is an error produced by the resolution engine, carrying a
// stable error Code and a human Location breadcrumb, matching the
// "<CODE>: <message> (at <location>)" format from §5.
type ResolveError struct {
	Code     string
	Message  string
	Location string
}

func (e *ResolveError) Error() string {
	if e.Location != "" {
		return fmt.Sprintf("%s: %s (at %s)", e.Code, e.Message, e.Location)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// Warning is a non-fatal finding produced during resolution (§5.2's
// W_* codes).
type Warning struct {
	Code     string
	Message  string
	Location string
}

func (w *Warning) String() string {
	if w.Location != "" {
		return fmt.Sprintf("%s: %s (at %s)", w.Code, w.Message, w.Location)
	}
	return fmt.Sprintf("%s: %s", w.Code, w.Message)
}

// placeholderPattern matches a full `{{ ... }}` placeholder occurrence
// within a larger string.
var placeholderPattern = regexp.MustCompile(`\{\{[^{}]*\}\}`)

// allowedInner is the restricted-syntax check from §4.1: the trimmed
// contents between "{{" and "}}" must be exactly ".<identifier>".
var allowedInner = regexp.MustCompile(`^\s*\.[A-Za-z_][A-Za-z0-9_]*\s*$`)

// varNamePattern extracts the identifier from a validated placeholder body.
var varNamePattern = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*`)

// checkPlaceholderSyntax scans s for every `{{ ... }}` occurrence and
// returns a *ResolveError with code E_TEMPLATE_SYNTAX for the first one
// that doesn't match the restricted `{{ .name }}` grammar. Returns nil if
// s contains no disallowed placeholders (including the case of no
// placeholders at all).
func checkPlaceholderSyntax(s string, location string) error {
	matches := placeholderPattern.FindAllString(s, -1)
	for _, m := range matches {
		inner := strings.TrimSuffix(strings.TrimPrefix(m, "{{"), "}}")
		if !allowedInner.MatchString(inner) {
			return &ResolveError{
				Code:     CodeTemplateSyntax,
				Message:  fmt.Sprintf("invalid placeholder %q: only \"{{ .name }}\" is allowed", m),
				Location: location,
			}
		}
	}
	return nil
}

// extractVarNames returns the set of variable names referenced via
// {{ .name }} placeholders in s. Assumes s has already passed
// checkPlaceholderSyntax.
func extractVarNames(s string) []string {
	matches := placeholderPattern.FindAllString(s, -1)
	var names []string
	for _, m := range matches {
		inner := strings.TrimSuffix(strings.TrimPrefix(m, "{{"), "}}")
		name := varNamePattern.FindString(inner)
		if name != "" {
			names = append(names, name)
		}
	}
	return names
}

// substitute replaces every {{ .name }} placeholder in s using scope,
// after validating restricted syntax. Returns an E_UNDEFINED_VARIABLE
// error if a referenced name has no entry in scope.
func substitute(s string, scope map[string]string, location string) (string, error) {
	if err := checkPlaceholderSyntax(s, location); err != nil {
		return "", err
	}
	for _, name := range extractVarNames(s) {
		if _, ok := scope[name]; !ok {
			return "", &ResolveError{
				Code:     CodeUndefinedVar,
				Message:  fmt.Sprintf("undefined variable %q", name),
				Location: location,
			}
		}
	}
	if !strings.Contains(s, "{{") {
		return s, nil
	}

	tmpl, err := template.New("marko").Option("missingkey=error").Parse(s)
	if err != nil {
		return "", &ResolveError{Code: CodeTemplateSyntax, Message: err.Error(), Location: location}
	}
	data := make(map[string]string, len(scope))
	for k, v := range scope {
		data[k] = v
	}
	var sb strings.Builder
	if err := tmpl.Execute(&sb, data); err != nil {
		return "", &ResolveError{Code: CodeUndefinedVar, Message: err.Error(), Location: location}
	}
	return sb.String(), nil
}
