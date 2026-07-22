package template

import (
	"strings"
	"testing"

	"github.com/inchestnov/marko/cli/internal/model"
)

func newCfg() *model.Config {
	return model.NewConfig()
}

func setCollections(cfg *model.Config, pairs ...struct {
	Name string
	Col  model.Collection
}) {
	for _, p := range pairs {
		cfg.Collections.Set(p.Name, p.Col)
	}
}

func collection(name string, col model.Collection) struct {
	Name string
	Col  model.Collection
} {
	return struct {
		Name string
		Col  model.Collection
	}{name, col}
}

func TestSubstitute_SimpleVariable(t *testing.T) {
	cfg := newCfg()
	cfg.Templates = map[string]model.Template{
		"greet": {
			Vars: map[string]model.Variable{"username": {Required: true}},
			Bookmarks: []model.Bookmark{
				{Name: "{{ .username }} - Profile", URL: "https://github.com/{{ .username }}"},
			},
		},
	}
	setCollections(cfg, collection("work", model.Collection{
		Templates: []model.TemplateRef{{Template: "greet", Vars: map[string]string{"username": "octocat"}}},
	}))

	res, err := Resolve(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	rc := res.Collections["work"]
	if len(rc.Groups) != 0 {
		t.Fatalf("expected 0 groups (folder-less template flattens), got %d groups", len(rc.Groups))
	}
	if len(rc.Bookmarks) != 1 {
		t.Fatalf("expected 1 bookmark spliced in, got %d", len(rc.Bookmarks))
	}
	bm := rc.Bookmarks[0]
	if bm.Name != "octocat - Profile" || bm.URL != "https://github.com/octocat" {
		t.Fatalf("unexpected bookmark: %+v", bm)
	}
}

func TestSubstitute_MissingVariable(t *testing.T) {
	cfg := newCfg()
	cfg.Templates = map[string]model.Template{
		"greet": {
			Vars: map[string]model.Variable{"username": {Required: true}},
			Bookmarks: []model.Bookmark{
				{Name: "{{ .username }}", URL: "https://example.com"},
			},
		},
	}
	setCollections(cfg, collection("work", model.Collection{
		Templates: []model.TemplateRef{{Template: "greet"}},
	}))

	_, err := Resolve(cfg)
	if err == nil {
		t.Fatal("expected an error for missing required variable")
	}
	rerr, ok := err.(*ResolveError)
	if !ok {
		t.Fatalf("expected *ResolveError, got %T: %v", err, err)
	}
	if rerr.Code != CodeMissingVariable {
		t.Fatalf("expected %s, got %s", CodeMissingVariable, rerr.Code)
	}
}

func TestSubstitute_DisallowedSyntaxRejected(t *testing.T) {
	cases := []string{
		"{{ range .x }}",
		"{{ .x | upper }}",
		"{{ if .x }}yes{{ end }}",
		"{{ 42 }}",
		"{{.x.y}}",
	}
	for _, c := range cases {
		t.Run(c, func(t *testing.T) {
			cfg := newCfg()
			cfg.Templates = map[string]model.Template{
				"t": {Bookmarks: []model.Bookmark{{Name: c, URL: "https://example.com"}}},
			}
			setCollections(cfg, collection("work", model.Collection{
				Templates: []model.TemplateRef{{Template: "t"}},
			}))
			_, err := Resolve(cfg)
			if err == nil {
				t.Fatalf("expected E_TEMPLATE_SYNTAX error for %q", c)
			}
			rerr, ok := err.(*ResolveError)
			if !ok || rerr.Code != CodeTemplateSyntax {
				t.Fatalf("expected E_TEMPLATE_SYNTAX, got %v", err)
			}
		})
	}
}

func TestNestedTemplate_FolderlessFlattening(t *testing.T) {
	cfg := newCfg()
	cfg.Templates = map[string]model.Template{
		"profile": {
			Vars:      map[string]model.Variable{"username": {Required: true}},
			Bookmarks: []model.Bookmark{{Name: "{{ .username }} - Profile", URL: "https://github.com/{{ .username }}"}},
		},
		"repository": {
			Vars:   map[string]model.Variable{"username": {Required: true}, "repo_name": {Required: true}},
			Folder: &model.Folder{Name: "{{ .repo_name }}"},
			Templates: []model.TemplateRef{
				{Template: "profile", Vars: map[string]string{"username": "{{ .username }}"}},
			},
		},
	}
	setCollections(cfg, collection("work", model.Collection{
		Templates: []model.TemplateRef{
			{Template: "repository", Vars: map[string]string{"username": "octocat", "repo_name": "marko"}},
		},
	}))

	res, err := Resolve(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	rc := res.Collections["work"]
	if len(rc.Groups) != 1 {
		t.Fatalf("expected 1 group (repository folder), got %d", len(rc.Groups))
	}
	repo := rc.Groups[0]
	if repo.Name != "marko" {
		t.Fatalf("expected folder name 'marko', got %q", repo.Name)
	}
	if len(repo.Bookmarks) != 1 || repo.Bookmarks[0].Name != "octocat - Profile" {
		t.Fatalf("expected profile bookmark flattened into repository, got %+v", repo.Bookmarks)
	}
}

func TestNestedTemplate_WithFolderNesting(t *testing.T) {
	cfg := newCfg()
	cfg.Templates = map[string]model.Template{
		"github": {
			Folder:    &model.Folder{Name: "GitHub"},
			Bookmarks: []model.Bookmark{{Name: "Documentation", URL: "https://docs.github.com"}},
		},
		"repository": {
			Vars:   map[string]model.Variable{"repo_name": {Required: true}},
			Folder: &model.Folder{Name: "{{ .repo_name }}"},
			Templates: []model.TemplateRef{
				{Template: "github"},
			},
		},
	}
	setCollections(cfg, collection("work", model.Collection{
		Templates: []model.TemplateRef{
			{Template: "repository", Vars: map[string]string{"repo_name": "marko"}},
		},
	}))

	res, err := Resolve(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	repo := res.Collections["work"].Groups[0]
	if len(repo.Groups) != 1 || repo.Groups[0].Name != "GitHub" {
		t.Fatalf("expected nested GitHub folder, got %+v", repo.Groups)
	}
}

func TestTemplateRef_AsRename(t *testing.T) {
	cfg := newCfg()
	cfg.Templates = map[string]model.Template{
		"github": {
			Folder:    &model.Folder{Name: "GitHub"},
			Bookmarks: []model.Bookmark{{Name: "Documentation", URL: "https://docs.github.com"}},
		},
	}
	setCollections(cfg, collection("work", model.Collection{
		Templates: []model.TemplateRef{{Template: "github", As: "GitHub Links"}},
	}))

	res, err := Resolve(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	rc := res.Collections["work"]
	if len(rc.Groups) != 1 || rc.Groups[0].Name != "GitHub Links" {
		t.Fatalf("expected renamed folder 'GitHub Links', got %+v", rc.Groups)
	}
}

func TestTemplateRef_AsIgnoredOnFolderless(t *testing.T) {
	cfg := newCfg()
	cfg.Templates = map[string]model.Template{
		"profile": {
			Vars:      map[string]model.Variable{"username": {Required: true}},
			Bookmarks: []model.Bookmark{{Name: "{{ .username }}", URL: "https://github.com/{{ .username }}"}},
		},
	}
	setCollections(cfg, collection("work", model.Collection{
		Templates: []model.TemplateRef{{Template: "profile", As: "Ignored", Vars: map[string]string{"username": "octocat"}}},
	}))

	res, err := Resolve(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	found := false
	for _, w := range res.Warnings {
		if w.Code == CodeWarnAsIgnored {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected W_AS_IGNORED warning, got %+v", res.Warnings)
	}
}

func TestInheritance_MergeOrder(t *testing.T) {
	cfg := newCfg()
	cfg.Templates = map[string]model.Template{
		"base": {
			Folder:    &model.Folder{Name: "Base"},
			Bookmarks: []model.Bookmark{{Name: "Base1", URL: "https://example.com/base1"}},
		},
		"child": {
			Extends:   "base",
			Bookmarks: []model.Bookmark{{Name: "Child1", URL: "https://example.com/child1"}},
		},
	}
	setCollections(cfg, collection("work", model.Collection{
		Templates: []model.TemplateRef{{Template: "child"}},
	}))

	res, err := Resolve(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	rc := res.Collections["work"]
	if len(rc.Groups) != 1 {
		t.Fatalf("expected 1 group inherited from base's Folder, got %d", len(rc.Groups))
	}
	g := rc.Groups[0]
	if g.Name != "Base" {
		t.Fatalf("expected inherited folder name 'Base', got %q", g.Name)
	}
	if len(g.Bookmarks) != 2 || g.Bookmarks[0].Name != "Base1" || g.Bookmarks[1].Name != "Child1" {
		t.Fatalf("expected base bookmarks first then child's, got %+v", g.Bookmarks)
	}
}

func TestExtendsCycleDetection(t *testing.T) {
	cfg := newCfg()
	cfg.Templates = map[string]model.Template{
		"a": {Extends: "b", Bookmarks: []model.Bookmark{{Name: "A", URL: "https://example.com/a"}}},
		"b": {Extends: "a", Bookmarks: []model.Bookmark{{Name: "B", URL: "https://example.com/b"}}},
	}
	setCollections(cfg, collection("work", model.Collection{
		Templates: []model.TemplateRef{{Template: "a"}},
	}))

	_, err := Resolve(cfg)
	if err == nil {
		t.Fatal("expected cycle error")
	}
	rerr, ok := err.(*ResolveError)
	if !ok || rerr.Code != CodeCycleExtends {
		t.Fatalf("expected E_CYCLE_EXTENDS, got %v", err)
	}
	if !strings.Contains(rerr.Message, "->") {
		t.Fatalf("expected cycle path in message, got %q", rerr.Message)
	}
}

func TestNestedTemplateCycleDetection(t *testing.T) {
	cfg := newCfg()
	cfg.Templates = map[string]model.Template{
		"repository": {
			Folder:    &model.Folder{Name: "Repo"},
			Templates: []model.TemplateRef{{Template: "github"}},
		},
		"github": {
			Folder:    &model.Folder{Name: "GitHub"},
			Templates: []model.TemplateRef{{Template: "repository"}},
		},
	}
	setCollections(cfg, collection("work", model.Collection{
		Templates: []model.TemplateRef{{Template: "repository"}},
	}))

	_, err := Resolve(cfg)
	if err == nil {
		t.Fatal("expected cycle error")
	}
	rerr, ok := err.(*ResolveError)
	if !ok || rerr.Code != CodeCycleTemplate {
		t.Fatalf("expected E_CYCLE_TEMPLATE, got %v", err)
	}
	if !strings.Contains(rerr.Message, "repository -> github -> repository") {
		t.Fatalf("expected exact cycle path, got %q", rerr.Message)
	}
}

func TestVariablePrecedence_AllFourLevels(t *testing.T) {
	// Level 4: required + missing -> error.
	t.Run("level4_missing_required", func(t *testing.T) {
		cfg := newCfg()
		cfg.Templates = map[string]model.Template{
			"t": {Vars: map[string]model.Variable{"x": {Required: true}}, Bookmarks: []model.Bookmark{{Name: "{{ .x }}", URL: "https://example.com"}}},
		}
		setCollections(cfg, collection("c", model.Collection{Templates: []model.TemplateRef{{Template: "t"}}}))
		_, err := Resolve(cfg)
		if err == nil {
			t.Fatal("expected E_MISSING_VARIABLE")
		}
	})

	// Level 3: global default used only because template declares Vars["x"].
	t.Run("level3_global_default", func(t *testing.T) {
		cfg := newCfg()
		cfg.Variables = map[string]model.Variable{"x": {Default: "global-val"}}
		cfg.Templates = map[string]model.Template{
			"t": {Vars: map[string]model.Variable{"x": {}}, Bookmarks: []model.Bookmark{{Name: "{{ .x }}", URL: "https://example.com"}}},
		}
		setCollections(cfg, collection("c", model.Collection{Templates: []model.TemplateRef{{Template: "t"}}}))
		res, err := Resolve(cfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got := res.Collections["c"].Bookmarks[0].Name; got != "global-val" {
			t.Fatalf("expected 'global-val', got %q", got)
		}
	})

	// Level 2: template default overrides global default.
	t.Run("level2_template_default_overrides_global", func(t *testing.T) {
		cfg := newCfg()
		cfg.Variables = map[string]model.Variable{"x": {Default: "global-val"}}
		cfg.Templates = map[string]model.Template{
			"t": {Vars: map[string]model.Variable{"x": {Default: "template-val"}}, Bookmarks: []model.Bookmark{{Name: "{{ .x }}", URL: "https://example.com"}}},
		}
		setCollections(cfg, collection("c", model.Collection{Templates: []model.TemplateRef{{Template: "t"}}}))
		res, err := Resolve(cfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got := res.Collections["c"].Bookmarks[0].Name; got != "template-val" {
			t.Fatalf("expected 'template-val', got %q", got)
		}
	})

	// Level 1: ref.Vars overrides everything.
	t.Run("level1_ref_vars_overrides_all", func(t *testing.T) {
		cfg := newCfg()
		cfg.Variables = map[string]model.Variable{"x": {Default: "global-val"}}
		cfg.Templates = map[string]model.Template{
			"t": {Vars: map[string]model.Variable{"x": {Default: "template-val"}}, Bookmarks: []model.Bookmark{{Name: "{{ .x }}", URL: "https://example.com"}}},
		}
		setCollections(cfg, collection("c", model.Collection{Templates: []model.TemplateRef{{Template: "t", Vars: map[string]string{"x": "ref-val"}}}}))
		res, err := Resolve(cfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got := res.Collections["c"].Bookmarks[0].Name; got != "ref-val" {
			t.Fatalf("expected 'ref-val', got %q", got)
		}
	})
}

func TestUnknownTemplateReference(t *testing.T) {
	cfg := newCfg()
	setCollections(cfg, collection("c", model.Collection{
		Templates: []model.TemplateRef{{Template: "does-not-exist"}},
	}))
	_, err := Resolve(cfg)
	if err == nil {
		t.Fatal("expected error")
	}
	rerr, ok := err.(*ResolveError)
	if !ok || rerr.Code != CodeUnknownTemplate {
		t.Fatalf("expected E_UNKNOWN_TEMPLATE, got %v", err)
	}
}

func TestUnknownVarWarning(t *testing.T) {
	cfg := newCfg()
	cfg.Templates = map[string]model.Template{
		"t": {Bookmarks: []model.Bookmark{{Name: "Static", URL: "https://example.com"}}},
	}
	setCollections(cfg, collection("c", model.Collection{
		Templates: []model.TemplateRef{{Template: "t", Vars: map[string]string{"unused": "value"}}},
	}))
	res, err := Resolve(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	found := false
	for _, w := range res.Warnings {
		if w.Code == CodeWarnUnknownVar {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected W_UNKNOWN_VAR warning, got %+v", res.Warnings)
	}
}
