package cmd

import (
	"fmt"

	"github.com/inchestnov/marko/cli/internal/bookmarktree"
	"github.com/inchestnov/marko/cli/internal/fsutil"
	"github.com/inchestnov/marko/cli/internal/model"
	"github.com/inchestnov/marko/cli/parser"
	"github.com/inchestnov/marko/cli/renderer"
	"github.com/inchestnov/marko/cli/template"
	"github.com/inchestnov/marko/cli/validator"
)

// resolveConfigPath applies the --config global flag, defaulting to
// searching upward from cwd for marko.yaml (docs/architecture.md §9).
func resolveConfigPath() (string, error) {
	if configPath != "" {
		return configPath, nil
	}
	found, err := fsutil.FindConfig("")
	if err != nil {
		return "", newExitError(3, err)
	}
	return found, nil
}

// resolveTemplatesDir applies the --templates-dir global flag, defaulting
// to "<dir of marko.yaml>/templates".
func resolveTemplatesDir(cfgPath string) string {
	if templatesDir != "" {
		return templatesDir
	}
	return fsutil.DefaultTemplatesDir(cfgPath)
}

// loadConfig runs the parser against the resolved --config/--templates-dir
// paths.
func loadConfig() (*parser.Result, error) {
	cfgPath, err := resolveConfigPath()
	if err != nil {
		return nil, err
	}
	tplDir := resolveTemplatesDir(cfgPath)

	res, err := parser.Load(cfgPath, tplDir)
	if err != nil {
		return nil, newExitError(3, err)
	}
	return res, nil
}

// validateConfig runs Phase A + Phase B validation and returns the
// findings. It does not itself decide the exit code; callers inspect
// validator.HasErrors(findings).
func validateConfig(cfg *model.Config, dups []model.DuplicateTemplate) ([]validator.Finding, error) {
	findings, err := validator.Validate(cfg, dups)
	if err != nil {
		return nil, newExitError(1, err)
	}
	return findings, nil
}

// pipelineResult bundles every stage's output for commands that need
// more than just the final tree (e.g. render's tree-view output).
type pipelineResult struct {
	ParseResult *parser.Result
	Findings    []validator.Finding
	Resolved    *template.Result
	Tree        *bookmarktree.BookmarkTree
}

// runPipeline executes parse -> validate -> resolve -> render in full,
// per the "render always validates internally" rule (§9.3) shared by
// render/diff/sync/export. If validation produced any E_* finding, the
// returned error is a *exitError with code 1 and Tree/Resolved are nil;
// callers should still print pr.Findings to the user.
func runPipeline() (*pipelineResult, error) {
	pr, err := loadConfig()
	if err != nil {
		return nil, err
	}

	findings, err := validateConfig(pr.Config, pr.DuplicateTemplates)
	if err != nil {
		return nil, err
	}

	result := &pipelineResult{ParseResult: pr, Findings: findings}

	if validator.HasErrors(findings) {
		return result, newExitError(1, fmt.Errorf("validation failed with %d error(s)", countErrors(findings)))
	}

	resolved, err := template.Resolve(pr.Config)
	if err != nil {
		// Should not normally happen since validateConfig already ran
		// resolution once, but guard defensively.
		return result, newExitError(1, err)
	}
	result.Resolved = resolved

	tree, err := renderer.Render(resolved)
	if err != nil {
		return result, newExitError(1, err)
	}
	result.Tree = tree

	return result, nil
}

func countErrors(findings []validator.Finding) int {
	n := 0
	for _, f := range findings {
		if f.Severity == validator.SeverityError {
			n++
		}
	}
	return n
}

func countWarnings(findings []validator.Finding) int {
	n := 0
	for _, f := range findings {
		if f.Severity == validator.SeverityWarning {
			n++
		}
	}
	return n
}

// pluralize renders "N word" or "N words" depending on n.
func pluralize(n int, word string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, word)
	}
	return fmt.Sprintf("%d %ss", n, word)
}
