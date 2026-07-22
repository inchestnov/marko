package renderer

import (
	"testing"

	"github.com/inchestnov/marko/cli/internal/bookmarktree"
	"github.com/inchestnov/marko/cli/internal/model"
	"github.com/inchestnov/marko/cli/template"
)

func resolveOrFatal(t *testing.T, cfg *model.Config) *template.Result {
	t.Helper()
	res, err := template.Resolve(cfg)
	if err != nil {
		t.Fatalf("unexpected resolve error: %v", err)
	}
	return res
}

func childNames(n *bookmarktree.Node) []string {
	var out []string
	for _, c := range n.Children {
		out = append(out, c.Name)
	}
	return out
}

func TestRender_OrderingRule_BookmarksThenInlineFoldersThenTemplates(t *testing.T) {
	cfg := model.NewConfig()
	cfg.Version = "1"
	cfg.Templates = map[string]model.Template{
		"tpl": {
			Folder:    &model.Folder{Name: "FromTemplate"},
			Bookmarks: []model.Bookmark{{Name: "TplBookmark", URL: "https://example.com/tpl"}},
		},
	}
	cfg.Collections.Set("work", model.Collection{
		Root: "bar",
		Bookmarks: []model.Bookmark{
			{Name: "DirectBookmark", URL: "https://example.com/direct"},
		},
		Folders: []model.NamedGroup{
			{Folder: model.Folder{Name: "InlineFolder"}},
		},
		Templates: []model.TemplateRef{
			{Template: "tpl"},
		},
	})

	res := resolveOrFatal(t, cfg)
	tree, err := Render(res)
	if err != nil {
		t.Fatalf("unexpected render error: %v", err)
	}

	bar := tree.Root("bar")
	names := childNames(bar)
	want := []string{"DirectBookmark", "InlineFolder", "FromTemplate"}
	if len(names) != len(want) {
		t.Fatalf("expected %v, got %v", want, names)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("expected order %v, got %v", want, names)
		}
	}
}

func TestRender_DuplicateSiblingFolderNamesMerge(t *testing.T) {
	cfg := model.NewConfig()
	cfg.Version = "1"
	cfg.Templates = map[string]model.Template{
		"a": {Folder: &model.Folder{Name: "Shared"}, Bookmarks: []model.Bookmark{{Name: "A", URL: "https://example.com/a"}}},
		"b": {Folder: &model.Folder{Name: "Shared"}, Bookmarks: []model.Bookmark{{Name: "B", URL: "https://example.com/b"}}},
	}
	cfg.Collections.Set("work", model.Collection{
		Root: "bar",
		Templates: []model.TemplateRef{
			{Template: "a"},
			{Template: "b"},
		},
	})

	res := resolveOrFatal(t, cfg)
	tree, err := Render(res)
	if err != nil {
		t.Fatalf("unexpected render error: %v", err)
	}

	bar := tree.Root("bar")
	if len(bar.Children) != 1 {
		t.Fatalf("expected merged into 1 folder, got %d: %v", len(bar.Children), childNames(bar))
	}
	shared := bar.Children[0]
	if shared.Name != "Shared" {
		t.Fatalf("expected folder named 'Shared', got %q", shared.Name)
	}
	if len(shared.Children) != 2 {
		t.Fatalf("expected merged children count 2, got %d", len(shared.Children))
	}
	if shared.Children[0].Name != "A" || shared.Children[1].Name != "B" {
		t.Fatalf("expected concatenated ordered children [A, B], got %v", childNames(shared))
	}
}

func TestRender_DuplicateBookmarkDedup(t *testing.T) {
	cfg := model.NewConfig()
	cfg.Version = "1"
	cfg.Collections.Set("work", model.Collection{
		Root: "bar",
		Bookmarks: []model.Bookmark{
			{Name: "Same", URL: "https://example.com/x"},
			{Name: "Same", URL: "https://example.com/x"},
		},
	})
	res := resolveOrFatal(t, cfg)
	tree, err := Render(res)
	if err != nil {
		t.Fatalf("unexpected render error: %v", err)
	}
	bar := tree.Root("bar")
	if len(bar.Children) != 1 {
		t.Fatalf("expected deduplication to 1 bookmark, got %d", len(bar.Children))
	}
}

func TestRender_DuplicateBookmarkDifferentURLDoesNotPanic(t *testing.T) {
	// Renderer must not panic even though the validator should have
	// already rejected this input (E_DUPLICATE_SIBLING); it's a
	// defensive test only.
	cfg := model.NewConfig()
	cfg.Version = "1"
	cfg.Collections.Set("work", model.Collection{
		Root: "bar",
		Bookmarks: []model.Bookmark{
			{Name: "Same", URL: "https://example.com/x"},
			{Name: "Same", URL: "https://example.com/y"},
		},
	})
	res := resolveOrFatal(t, cfg)
	tree, err := Render(res)
	if err != nil {
		t.Fatalf("unexpected render error: %v", err)
	}
	bar := tree.Root("bar")
	if len(bar.Children) != 2 {
		t.Fatalf("expected both differing-url bookmarks kept, got %d", len(bar.Children))
	}
}

func TestRender_IndexAndPathAssignment(t *testing.T) {
	cfg := model.NewConfig()
	cfg.Version = "1"
	cfg.Collections.Set("work", model.Collection{
		Root:   "bar",
		Folder: &model.Folder{Name: "Work"},
		Folders: []model.NamedGroup{
			{Folder: model.Folder{Name: "Sub"}, Bookmarks: []model.Bookmark{{Name: "Leaf", URL: "https://example.com"}}},
		},
	})
	res := resolveOrFatal(t, cfg)
	tree, err := Render(res)
	if err != nil {
		t.Fatalf("unexpected render error: %v", err)
	}

	bar := tree.Root("bar")
	if bar.Path != nil {
		t.Fatalf("expected root Path to be empty, got %v", bar.Path)
	}
	work := bar.Children[0]
	if work.Index != 0 {
		t.Fatalf("expected Work.Index == 0, got %d", work.Index)
	}
	if len(work.Path) != 1 || work.Path[0] != "bar" {
		t.Fatalf("expected Work.Path == [bar], got %v", work.Path)
	}
	sub := work.Children[0]
	if len(sub.Path) != 2 || sub.Path[0] != "bar" || sub.Path[1] != "Work" {
		t.Fatalf("expected Sub.Path == [bar, Work], got %v", sub.Path)
	}
	leaf := sub.Children[0]
	if len(leaf.Path) != 3 || leaf.Path[2] != "Sub" {
		t.Fatalf("expected Leaf.Path == [bar, Work, Sub], got %v", leaf.Path)
	}
	if leaf.Index != 0 {
		t.Fatalf("expected Leaf.Index == 0, got %d", leaf.Index)
	}
}

func TestRender_CollectionWithoutFolderAttachesDirectlyToRoot(t *testing.T) {
	cfg := model.NewConfig()
	cfg.Version = "1"
	cfg.Collections.Set("personal", model.Collection{
		Root:      "other",
		Bookmarks: []model.Bookmark{{Name: "Gmail", URL: "https://mail.google.com"}},
	})
	res := resolveOrFatal(t, cfg)
	tree, err := Render(res)
	if err != nil {
		t.Fatalf("unexpected render error: %v", err)
	}
	other := tree.Root("other")
	if len(other.Children) != 1 || other.Children[0].Name != "Gmail" {
		t.Fatalf("expected Gmail directly under 'other', got %v", childNames(other))
	}
}
