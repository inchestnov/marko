package parser

import (
	"testing"
)

func TestLoad_ValidMinimalYAML(t *testing.T) {
	res, err := Load("testdata/minimal/marko.yaml", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	cfg := res.Config
	if cfg.Version != "1" {
		t.Fatalf("expected version '1', got %q", cfg.Version)
	}
	if cfg.Collections.Len() != 1 {
		t.Fatalf("expected 1 collection, got %d", cfg.Collections.Len())
	}
	col, ok := cfg.Collections.Get("personal")
	if !ok {
		t.Fatal("expected 'personal' collection")
	}
	if len(col.Bookmarks) != 1 || col.Bookmarks[0].Name != "Gmail" {
		t.Fatalf("unexpected bookmarks: %+v", col.Bookmarks)
	}
	if cfg.SourcePath == "" {
		t.Fatal("expected SourcePath to be populated")
	}
}

func TestLoad_MalformedYAML(t *testing.T) {
	_, err := Load("testdata/malformed/marko.yaml", "")
	if err == nil {
		t.Fatal("expected an error for malformed YAML")
	}
	perr, ok := err.(*Error)
	if !ok {
		t.Fatalf("expected *parser.Error, got %T: %v", err, err)
	}
	if perr.File == "" {
		t.Fatal("expected File to be populated on the wrapped error")
	}
}

func TestLoad_MultipleTemplateFilesMerge(t *testing.T) {
	res, err := Load("testdata/multifile/marko.yaml", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	cfg := res.Config
	if _, ok := cfg.Templates["profile"]; !ok {
		t.Fatal("expected 'profile' template from marko.yaml")
	}
	if _, ok := cfg.Templates["github"]; !ok {
		t.Fatal("expected 'github' template merged in from templates/github.yaml")
	}
	if len(res.DuplicateTemplates) != 0 {
		t.Fatalf("expected no duplicates, got %+v", res.DuplicateTemplates)
	}
}

func TestLoad_DuplicateTemplateNameAcrossFiles(t *testing.T) {
	res, err := Load("testdata/duplicate/marko.yaml", "")
	if err != nil {
		t.Fatalf("parser itself should not error on duplicates (surfaced for validator): %v", err)
	}
	if len(res.DuplicateTemplates) != 1 {
		t.Fatalf("expected exactly 1 duplicate template reported, got %+v", res.DuplicateTemplates)
	}
	dup := res.DuplicateTemplates[0]
	if dup.Name != "github" {
		t.Fatalf("expected duplicate name 'github', got %q", dup.Name)
	}
	if len(dup.Files) != 2 {
		t.Fatalf("expected 2 source files recorded, got %+v", dup.Files)
	}
}
