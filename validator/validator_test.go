package validator

import (
	"strings"
	"testing"

	"github.com/inchestnov/marko/internal/model"
)

func newCfg() *model.Config {
	return model.NewConfig()
}

func hasCode(findings []Finding, code string) (Finding, bool) {
	for _, f := range findings {
		if f.Code == code {
			return f, true
		}
	}
	return Finding{}, false
}

// ---------------------------------------------------------------------
// Phase A - Structural
// ---------------------------------------------------------------------

func TestPhaseA_MissingVersionAndCollections(t *testing.T) {
	cfg := newCfg()
	findings, err := Validate(cfg, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := hasCode(findings, CodeMissingField); !ok {
		t.Fatalf("expected E_MISSING_FIELD, got %+v", findings)
	}
}

func TestPhaseA_EmptyCollection(t *testing.T) {
	cfg := newCfg()
	cfg.Version = "1"
	cfg.Collections.Set("work", model.Collection{Root: "bar"})
	findings, _ := Validate(cfg, nil)
	f, ok := hasCode(findings, CodeEmptyCollection)
	if !ok {
		t.Fatalf("expected E_EMPTY_COLLECTION, got %+v", findings)
	}
	if !strings.Contains(f.Location, "collections.work") {
		t.Fatalf("expected location to reference collections.work, got %q", f.Location)
	}
}

func TestPhaseA_InvalidRootEnum(t *testing.T) {
	cfg := newCfg()
	cfg.Version = "1"
	cfg.Collections.Set("work", model.Collection{
		Root:      "invalid",
		Bookmarks: []model.Bookmark{{Name: "X", URL: "https://example.com"}},
	})
	findings, _ := Validate(cfg, nil)
	if _, ok := hasCode(findings, CodeInvalidEnum); !ok {
		t.Fatalf("expected E_INVALID_ENUM, got %+v", findings)
	}
}

func TestPhaseA_EmptyTemplate(t *testing.T) {
	cfg := newCfg()
	cfg.Version = "1"
	cfg.Templates = map[string]model.Template{"empty": {}}
	cfg.Collections.Set("work", model.Collection{
		Bookmarks: []model.Bookmark{{Name: "X", URL: "https://example.com"}},
	})
	findings, _ := Validate(cfg, nil)
	if _, ok := hasCode(findings, CodeEmptyTemplate); !ok {
		t.Fatalf("expected E_EMPTY_TEMPLATE, got %+v", findings)
	}
}

func TestPhaseA_MissingFolderName(t *testing.T) {
	cfg := newCfg()
	cfg.Version = "1"
	cfg.Collections.Set("work", model.Collection{
		Folder:    &model.Folder{Name: ""},
		Bookmarks: []model.Bookmark{{Name: "X", URL: "https://example.com"}},
	})
	findings, _ := Validate(cfg, nil)
	if _, ok := hasCode(findings, CodeMissingField); !ok {
		t.Fatalf("expected E_MISSING_FIELD for empty folder name, got %+v", findings)
	}
}

func TestPhaseA_MissingBookmarkName(t *testing.T) {
	cfg := newCfg()
	cfg.Version = "1"
	cfg.Collections.Set("work", model.Collection{
		Bookmarks: []model.Bookmark{{Name: "", URL: "https://example.com"}},
	})
	findings, _ := Validate(cfg, nil)
	if _, ok := hasCode(findings, CodeMissingField); !ok {
		t.Fatalf("expected E_MISSING_FIELD for empty bookmark name, got %+v", findings)
	}
}

func TestPhaseA_InvalidURL(t *testing.T) {
	cases := []string{"", "not-a-url", "ftp://example.com", "example.com"}
	for _, u := range cases {
		t.Run(u, func(t *testing.T) {
			cfg := newCfg()
			cfg.Version = "1"
			cfg.Collections.Set("work", model.Collection{
				Bookmarks: []model.Bookmark{{Name: "X", URL: u}},
			})
			findings, _ := Validate(cfg, nil)
			if _, ok := hasCode(findings, CodeInvalidURL); !ok {
				t.Fatalf("expected E_INVALID_URL for %q, got %+v", u, findings)
			}
		})
	}
}

func TestPhaseA_ValidURLSchemes(t *testing.T) {
	cases := []string{"http://example.com", "https://example.com", "chrome://bookmarks", "file:///Users/me/notes.pdf"}
	for _, u := range cases {
		t.Run(u, func(t *testing.T) {
			cfg := newCfg()
			cfg.Version = "1"
			cfg.Collections.Set("work", model.Collection{
				Bookmarks: []model.Bookmark{{Name: "X", URL: u}},
			})
			findings, _ := Validate(cfg, nil)
			if _, ok := hasCode(findings, CodeInvalidURL); ok {
				t.Fatalf("did not expect E_INVALID_URL for %q, got %+v", u, findings)
			}
		})
	}
}

func TestPhaseA_MissingTemplateRefField(t *testing.T) {
	cfg := newCfg()
	cfg.Version = "1"
	cfg.Collections.Set("work", model.Collection{
		Templates: []model.TemplateRef{{Template: ""}},
	})
	findings, _ := Validate(cfg, nil)
	if _, ok := hasCode(findings, CodeMissingField); !ok {
		t.Fatalf("expected E_MISSING_FIELD for empty TemplateRef.Template, got %+v", findings)
	}
}

func TestPhaseA_InvalidVariableDecl(t *testing.T) {
	cfg := newCfg()
	cfg.Version = "1"
	cfg.Variables = map[string]model.Variable{"x": {Required: true, Default: "val"}}
	cfg.Collections.Set("work", model.Collection{
		Bookmarks: []model.Bookmark{{Name: "X", URL: "https://example.com"}},
	})
	findings, _ := Validate(cfg, nil)
	if _, ok := hasCode(findings, CodeInvalidVariableDecl); !ok {
		t.Fatalf("expected E_INVALID_VARIABLE_DECL, got %+v", findings)
	}
}

func TestPhaseA_DuplicateTemplateAcrossFiles(t *testing.T) {
	cfg := newCfg()
	cfg.Version = "1"
	cfg.Templates = map[string]model.Template{
		"github": {Bookmarks: []model.Bookmark{{Name: "X", URL: "https://example.com"}}},
	}
	cfg.Collections.Set("work", model.Collection{
		Bookmarks: []model.Bookmark{{Name: "X", URL: "https://example.com"}},
	})
	dups := []model.DuplicateTemplate{{Name: "github", Files: []string{"a.yaml", "b.yaml"}}}
	findings, _ := Validate(cfg, dups)
	if _, ok := hasCode(findings, CodeDuplicateTemplate); !ok {
		t.Fatalf("expected E_DUPLICATE_TEMPLATE, got %+v", findings)
	}
}

func TestPhaseA_DuplicateCollectionCaseInsensitive(t *testing.T) {
	cfg := newCfg()
	cfg.Version = "1"
	cfg.Collections.Set("Work", model.Collection{Bookmarks: []model.Bookmark{{Name: "X", URL: "https://example.com"}}})
	cfg.Collections.Set("work", model.Collection{Bookmarks: []model.Bookmark{{Name: "Y", URL: "https://example.com"}}})
	findings, _ := Validate(cfg, nil)
	if _, ok := hasCode(findings, CodeDuplicateCollection); !ok {
		t.Fatalf("expected E_DUPLICATE_COLLECTION, got %+v", findings)
	}
}

// ---------------------------------------------------------------------
// Phase B - Semantic
// ---------------------------------------------------------------------

func TestPhaseB_UnknownTemplate(t *testing.T) {
	cfg := newCfg()
	cfg.Version = "1"
	cfg.Collections.Set("work", model.Collection{
		Templates: []model.TemplateRef{{Template: "missing"}},
	})
	findings, _ := Validate(cfg, nil)
	f, ok := hasCode(findings, "E_UNKNOWN_TEMPLATE")
	if !ok {
		t.Fatalf("expected E_UNKNOWN_TEMPLATE, got %+v", findings)
	}
	if !strings.Contains(f.Location, "collections.work.templates[0]") {
		t.Fatalf("expected location breadcrumb, got %q", f.Location)
	}
}

func TestPhaseB_UndefinedVariable(t *testing.T) {
	cfg := newCfg()
	cfg.Version = "1"
	cfg.Templates = map[string]model.Template{
		"t": {Bookmarks: []model.Bookmark{{Name: "X", URL: "https://example.com/{{ .missing }}"}}},
	}
	cfg.Collections.Set("work", model.Collection{
		Templates: []model.TemplateRef{{Template: "t"}},
	})
	findings, _ := Validate(cfg, nil)
	if _, ok := hasCode(findings, "E_UNDEFINED_VARIABLE"); !ok {
		t.Fatalf("expected E_UNDEFINED_VARIABLE, got %+v", findings)
	}
}

func TestPhaseB_MissingVariable(t *testing.T) {
	cfg := newCfg()
	cfg.Version = "1"
	cfg.Templates = map[string]model.Template{
		"t": {
			Vars:      map[string]model.Variable{"x": {Required: true}},
			Bookmarks: []model.Bookmark{{Name: "{{ .x }}", URL: "https://example.com"}},
		},
	}
	cfg.Collections.Set("work", model.Collection{
		Templates: []model.TemplateRef{{Template: "t"}},
	})
	findings, _ := Validate(cfg, nil)
	if _, ok := hasCode(findings, "E_MISSING_VARIABLE"); !ok {
		t.Fatalf("expected E_MISSING_VARIABLE, got %+v", findings)
	}
}

func TestPhaseB_CycleExtends(t *testing.T) {
	cfg := newCfg()
	cfg.Version = "1"
	cfg.Templates = map[string]model.Template{
		"a": {Extends: "b", Bookmarks: []model.Bookmark{{Name: "A", URL: "https://example.com"}}},
		"b": {Extends: "a", Bookmarks: []model.Bookmark{{Name: "B", URL: "https://example.com"}}},
	}
	cfg.Collections.Set("work", model.Collection{
		Templates: []model.TemplateRef{{Template: "a"}},
	})
	findings, _ := Validate(cfg, nil)
	f, ok := hasCode(findings, "E_CYCLE_EXTENDS")
	if !ok {
		t.Fatalf("expected E_CYCLE_EXTENDS, got %+v", findings)
	}
	if !strings.Contains(f.Message, "->") {
		t.Fatalf("expected cycle path, got %q", f.Message)
	}
}

func TestPhaseB_CycleTemplate(t *testing.T) {
	cfg := newCfg()
	cfg.Version = "1"
	cfg.Templates = map[string]model.Template{
		"repository": {Folder: &model.Folder{Name: "Repo"}, Templates: []model.TemplateRef{{Template: "github"}}},
		"github":     {Folder: &model.Folder{Name: "GitHub"}, Templates: []model.TemplateRef{{Template: "repository"}}},
	}
	cfg.Collections.Set("work", model.Collection{
		Templates: []model.TemplateRef{{Template: "repository"}},
	})
	findings, _ := Validate(cfg, nil)
	f, ok := hasCode(findings, "E_CYCLE_TEMPLATE")
	if !ok {
		t.Fatalf("expected E_CYCLE_TEMPLATE, got %+v", findings)
	}
	if !strings.Contains(f.Message, "repository -> github -> repository") {
		t.Fatalf("expected exact cycle path, got %q", f.Message)
	}
}

func TestPhaseB_TemplateSyntax(t *testing.T) {
	cfg := newCfg()
	cfg.Version = "1"
	cfg.Templates = map[string]model.Template{
		"t": {Bookmarks: []model.Bookmark{{Name: "{{ range .x }}", URL: "https://example.com"}}},
	}
	cfg.Collections.Set("work", model.Collection{
		Templates: []model.TemplateRef{{Template: "t"}},
	})
	findings, _ := Validate(cfg, nil)
	if _, ok := hasCode(findings, "E_TEMPLATE_SYNTAX"); !ok {
		t.Fatalf("expected E_TEMPLATE_SYNTAX, got %+v", findings)
	}
}

func TestPhaseB_WarnUnknownVar(t *testing.T) {
	cfg := newCfg()
	cfg.Version = "1"
	cfg.Templates = map[string]model.Template{
		"t": {Bookmarks: []model.Bookmark{{Name: "Static", URL: "https://example.com"}}},
	}
	cfg.Collections.Set("work", model.Collection{
		Templates: []model.TemplateRef{{Template: "t", Vars: map[string]string{"unused": "x"}}},
	})
	findings, _ := Validate(cfg, nil)
	f, ok := hasCode(findings, "W_UNKNOWN_VAR")
	if !ok {
		t.Fatalf("expected W_UNKNOWN_VAR, got %+v", findings)
	}
	if f.Severity != SeverityWarning {
		t.Fatalf("expected warning severity, got %v", f.Severity)
	}
	if HasErrors(findings) {
		t.Fatalf("warnings alone should not count as errors: %+v", findings)
	}
}

func TestPhaseB_WarnAsIgnored(t *testing.T) {
	cfg := newCfg()
	cfg.Version = "1"
	cfg.Templates = map[string]model.Template{
		"t": {Bookmarks: []model.Bookmark{{Name: "Static", URL: "https://example.com"}}},
	}
	cfg.Collections.Set("work", model.Collection{
		Templates: []model.TemplateRef{{Template: "t", As: "Renamed"}},
	})
	findings, _ := Validate(cfg, nil)
	f, ok := hasCode(findings, "W_AS_IGNORED")
	if !ok {
		t.Fatalf("expected W_AS_IGNORED, got %+v", findings)
	}
	if f.Severity != SeverityWarning {
		t.Fatalf("expected warning severity, got %v", f.Severity)
	}
}

func TestPhaseB_DuplicateSiblingDifferentURL(t *testing.T) {
	cfg := newCfg()
	cfg.Version = "1"
	cfg.Collections.Set("work", model.Collection{
		Bookmarks: []model.Bookmark{
			{Name: "Dup", URL: "https://example.com/a"},
			{Name: "Dup", URL: "https://example.com/b"},
		},
	})
	findings, _ := Validate(cfg, nil)
	if _, ok := hasCode(findings, "E_DUPLICATE_SIBLING"); !ok {
		t.Fatalf("expected E_DUPLICATE_SIBLING, got %+v", findings)
	}
}

func TestPhaseB_DuplicateSiblingSameURLNotAnError(t *testing.T) {
	cfg := newCfg()
	cfg.Version = "1"
	cfg.Collections.Set("work", model.Collection{
		Bookmarks: []model.Bookmark{
			{Name: "Dup", URL: "https://example.com/a"},
			{Name: "Dup", URL: "https://example.com/a"},
		},
	})
	findings, _ := Validate(cfg, nil)
	if _, ok := hasCode(findings, "E_DUPLICATE_SIBLING"); ok {
		t.Fatalf("identical (name, url) should be deduplicated, not an error: %+v", findings)
	}
}

func TestExitBehavior_WarningsDoNotFailValidation(t *testing.T) {
	cfg := newCfg()
	cfg.Version = "1"
	cfg.Templates = map[string]model.Template{
		"t": {Bookmarks: []model.Bookmark{{Name: "Static", URL: "https://example.com"}}},
	}
	cfg.Collections.Set("work", model.Collection{
		Templates: []model.TemplateRef{{Template: "t", As: "Renamed"}},
	})
	findings, _ := Validate(cfg, nil)
	if HasErrors(findings) {
		t.Fatalf("expected zero E_* findings (only a warning), got %+v", findings)
	}
}
