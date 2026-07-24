// Package parser loads marko.yaml and templates/*.yaml into a raw
// model.Config. See docs/architecture.md §3. parser depends only on
// internal/model.
package parser

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/inchestnov/marko/internal/fsutil"
	"github.com/inchestnov/marko/internal/model"
	"gopkg.in/yaml.v3"
)

// Result is the outcome of a Load call: the merged Config plus bookkeeping
// the validator needs but that doesn't belong on model.Config itself.
type Result struct {
	Config *model.Config

	// DuplicateTemplates lists any template names declared more than once
	// across marko.yaml + templates/*.yaml. Empty when there are none.
	DuplicateTemplates []model.DuplicateTemplate

	// TemplatesDir is the templates directory that was searched (for
	// diagnostics/reporting).
	TemplatesDir string
}

// Error is a parse error enriched with the source file and, where
// available, YAML line/column location.
type Error struct {
	File    string
	Line    int
	Column  int
	Wrapped error
}

func (e *Error) Error() string {
	if e.Line > 0 {
		return fmt.Sprintf("%s:%d:%d: %s", e.File, e.Line, e.Column, e.Wrapped)
	}
	return fmt.Sprintf("%s: %s", e.File, e.Wrapped)
}

func (e *Error) Unwrap() error {
	return e.Wrapped
}

// Load reads configPath (a marko.yaml file) and every *.yaml/*.yml file
// directly under templatesDir, merging their `templates:` blocks with
// marko.yaml's own, and returns the combined model.Config.
//
// If templatesDir is empty, it defaults to "<dir of configPath>/templates".
func Load(configPath string, templatesDir string) (*Result, error) {
	absConfigPath, err := filepath.Abs(configPath)
	if err != nil {
		return nil, fmt.Errorf("parser: resolving config path %q: %w", configPath, err)
	}

	data, err := os.ReadFile(absConfigPath)
	if err != nil {
		return nil, &Error{File: absConfigPath, Wrapped: err}
	}

	cfg := model.NewConfig()
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, wrapYAMLErr(absConfigPath, err)
	}
	cfg.SourcePath = absConfigPath
	if cfg.Templates == nil {
		cfg.Templates = make(map[string]model.Template)
	}
	if cfg.Variables == nil {
		cfg.Variables = make(map[string]model.Variable)
	}
	if cfg.Collections == nil {
		cfg.Collections = model.NewCollectionMap()
	}

	if templatesDir == "" {
		templatesDir = fsutil.DefaultTemplatesDir(absConfigPath)
	}

	// Track which file each template name first (and subsequently) came
	// from, so we can report duplicates.
	declaredIn := map[string][]string{}
	for name := range cfg.Templates {
		declaredIn[name] = append(declaredIn[name], absConfigPath)
	}

	files, err := fsutil.DiscoverTemplateFiles(templatesDir)
	if err != nil {
		return nil, err
	}

	for _, f := range files {
		fdata, err := os.ReadFile(f)
		if err != nil {
			return nil, &Error{File: f, Wrapped: err}
		}

		var fileCfg struct {
			Templates map[string]model.Template `yaml:"templates,omitempty"`
		}
		if err := yaml.Unmarshal(fdata, &fileCfg); err != nil {
			return nil, wrapYAMLErr(f, err)
		}

		for name, tpl := range fileCfg.Templates {
			declaredIn[name] = append(declaredIn[name], f)
			if _, exists := cfg.Templates[name]; !exists {
				cfg.Templates[name] = tpl
			}
			// If it already exists, keep the first-seen definition;
			// the duplicate is still recorded in declaredIn for the
			// validator to flag as E_DUPLICATE_TEMPLATE.
		}
	}

	var dups []model.DuplicateTemplate
	for name, srcs := range declaredIn {
		if len(srcs) > 1 {
			dups = append(dups, model.DuplicateTemplate{Name: name, Files: srcs})
		}
	}

	return &Result{
		Config:             cfg,
		DuplicateTemplates: dups,
		TemplatesDir:       templatesDir,
	}, nil
}

func wrapYAMLErr(file string, err error) error {
	line, col := 0, 0
	if te, ok := err.(*yaml.TypeError); ok && len(te.Errors) > 0 {
		return &Error{File: file, Wrapped: fmt.Errorf("%s", te.Errors[0])}
	}
	return &Error{File: file, Line: line, Column: col, Wrapped: err}
}
