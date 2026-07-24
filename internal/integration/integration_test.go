// Package integration contains the end-to-end pipeline test described in
// docs/architecture.md §11.2: parser -> template engine -> renderer ->
// diff engine -> simulated apply -> re-diff for idempotency. It is a
// plain Go test (no build tag, no shelling out to the built binary),
// calling package functions directly per §11.2's stated preference.
package integration

import (
	"testing"

	"github.com/inchestnov/marko/diff"
	"github.com/inchestnov/marko/internal/bookmarktree"
	"github.com/inchestnov/marko/parser"
	"github.com/inchestnov/marko/renderer"
	"github.com/inchestnov/marko/template"
	"github.com/inchestnov/marko/validator"
)

func TestFullPipeline_ParseResolveRenderDiffApplyIdempotent(t *testing.T) {
	// 1. Load the fixture YAML (parser).
	pr, err := parser.Load("testdata/marko.yaml", "testdata/templates")
	if err != nil {
		t.Fatalf("parser.Load: %v", err)
	}

	// Assert zero validation errors (structural + semantic).
	findings, err := validator.Validate(pr.Config, pr.DuplicateTemplates)
	if err != nil {
		t.Fatalf("validator.Validate: %v", err)
	}
	if validator.HasErrors(findings) {
		t.Fatalf("expected zero validation errors, got %+v", findings)
	}

	// 2. Resolve templates (template engine).
	resolved, err := template.Resolve(pr.Config)
	if err != nil {
		t.Fatalf("template.Resolve: %v", err)
	}

	// 3. Render to BookmarkTree (renderer) -- assert exact expected
	// structure via a handful of targeted structural checks (a full
	// golden-JSON comparison is brittle across minor formatting changes,
	// so this asserts the load-bearing shape instead).
	desired, err := renderer.Render(resolved)
	if err != nil {
		t.Fatalf("renderer.Render: %v", err)
	}

	bar := desired.Root("bar")
	if bar == nil || len(bar.Children) != 1 || bar.Children[0].Name != "Work" {
		t.Fatalf("expected bar -> Work, got %+v", bar)
	}
	work := bar.Children[0]
	if len(work.Children) != 1 || work.Children[0].Name != "marko" {
		t.Fatalf("expected Work -> marko, got %+v", childNames(work))
	}
	repo := work.Children[0]
	wantRepoChildren := map[string]bool{"Repository": false, "octocat - Profile": false, "GitHub Links": false}
	for _, c := range repo.Children {
		if _, ok := wantRepoChildren[c.Name]; ok {
			wantRepoChildren[c.Name] = true
		}
	}
	for name, found := range wantRepoChildren {
		if !found {
			t.Fatalf("expected repo folder to contain %q, got %+v", name, childNames(repo))
		}
	}

	other := desired.Root("other")
	if other == nil || len(other.Children) != 1 || other.Children[0].Name != "Gmail" {
		t.Fatalf("expected other -> Gmail, got %+v", other)
	}

	// 4. Simulate "Chrome Import": an empty actual tree (just the two
	// native roots), diff against it, and assert the resulting Plan
	// contains exactly one CREATE per desired node, in valid
	// breadth-first dependency order (a parent's CREATE never appears
	// after its child's).
	actual := emptyActualTree()
	plan := diff.Diff(desired, actual)

	desiredNodeCount := countNodes(desired) - 2 // exclude the two roots themselves
	createCount := 0
	for _, op := range plan.Operations {
		if op.Type == diff.OpCreate {
			createCount++
		}
	}
	if createCount != desiredNodeCount {
		t.Fatalf("expected %d CREATE ops (one per desired node), got %d", desiredNodeCount, createCount)
	}
	assertParentBeforeChild(t, plan)

	// 5. Simulate "apply": a small in-memory fake of the mutation target
	// (the same shape cli/browserfile mutates for real) applies the Plan
	// op-by-op; assert the resulting tree structurally equals the desired
	// tree (ignoring BrowserID/Index bookkeeping).
	synced := applyPlan(t, actual, plan)
	assertStructurallyEqual(t, desired, synced)

	// 6. Re-run diff (desired vs the now-"synced" tree) -- assert the
	// resulting Plan.Operations is empty, proving idempotency.
	plan2 := diff.Diff(desired, synced)
	if len(plan2.Operations) != 0 {
		t.Fatalf("expected empty plan on second diff (idempotency), got %+v", plan2.Operations)
	}
}

func childNames(n *bookmarktree.Node) []string {
	var out []string
	for _, c := range n.Children {
		out = append(out, c.Name)
	}
	return out
}

func emptyActualTree() *bookmarktree.BookmarkTree {
	return &bookmarktree.BookmarkTree{
		Roots: []*bookmarktree.Node{
			{Kind: bookmarktree.KindFolder, Name: "bar", BrowserID: "1"},
			{Kind: bookmarktree.KindFolder, Name: "other", BrowserID: "2"},
		},
	}
}

func countNodes(tree *bookmarktree.BookmarkTree) int {
	n := 0
	tree.Walk(func(*bookmarktree.Node) bool {
		n++
		return true
	})
	return n
}

// assertParentBeforeChild verifies that for every CREATE op, no CREATE
// targeting one of its descendant paths appears earlier in the plan.
func assertParentBeforeChild(t *testing.T, plan *diff.Plan) {
	t.Helper()
	seenPaths := map[string]int{}
	for i, op := range plan.Operations {
		if op.Type != diff.OpCreate {
			continue
		}
		key := pathKey(op.TargetPath)
		seenPaths[key] = i
		// Every proper prefix of this path (i.e. every ancestor) must
		// have already been seen at an earlier index, except the
		// native root itself (len 1: "bar"/"other" always exist and are
		// never CREATEd).
		for l := 2; l < len(op.TargetPath); l++ {
			ancestorKey := pathKey(op.TargetPath[:l])
			if idx, ok := seenPaths[ancestorKey]; !ok || idx > i {
				t.Fatalf("CREATE for %v appears before its ancestor %v was created", op.TargetPath, op.TargetPath[:l])
			}
		}
	}
}

func pathKey(path []string) string {
	key := ""
	for _, p := range path {
		key += "/" + p
	}
	return key
}

// applyPlan is a minimal in-memory fake of the real mutation target,
// applying Operations sequentially against a mutable copy of actual, in
// the same forward-pass style cli/browserfile uses for real (tracking
// newly created folder ids by TargetPath so subsequent MOVE/CREATE ops
// can reference them).
func applyPlan(t *testing.T, actual *bookmarktree.BookmarkTree, plan *diff.Plan) *bookmarktree.BookmarkTree {
	t.Helper()
	tree := actual.Clone()
	idByPath := map[string]string{
		"/bar":   tree.Root("bar").BrowserID,
		"/other": tree.Root("other").BrowserID,
	}
	nextID := 1000

	findByID := func(id string) *bookmarktree.Node {
		var found *bookmarktree.Node
		tree.Walk(func(n *bookmarktree.Node) bool {
			if n.BrowserID == id {
				found = n
				return false
			}
			return true
		})
		return found
	}

	for _, op := range plan.Operations {
		switch op.Type {
		case diff.OpDelete:
			parent := findParent(tree, op.BrowserID)
			if parent == nil {
				t.Fatalf("apply DELETE: could not find parent of %s", op.BrowserID)
			}
			removeChild(parent, op.BrowserID)

		case diff.OpCreate:
			parentID := op.ParentBrowserID
			if parentID == "" {
				parentID = idByPath[pathKey(op.TargetPath[:len(op.TargetPath)-1])]
			}
			var parent *bookmarktree.Node
			if len(op.TargetPath) == 1 {
				parent = tree.Root(op.TargetPath[0])
			} else {
				parent = findByID(parentID)
			}
			if parent == nil {
				t.Fatalf("apply CREATE: could not resolve parent for %v (parentBrowserId=%q)", op.TargetPath, op.ParentBrowserID)
			}
			nextID++
			newID := itoa(nextID)
			n := &bookmarktree.Node{
				Kind:      bookmarktree.NodeKind(op.Kind),
				Name:      op.Name,
				URL:       op.URL,
				BrowserID: newID,
			}
			parent.Children = insertAt(parent.Children, n, op.Position)
			idByPath[pathKey(op.TargetPath)] = newID

		case diff.OpMove:
			n := findByID(op.BrowserID)
			if n == nil {
				t.Fatalf("apply MOVE: could not find node %s", op.BrowserID)
			}
			oldParent := findParent(tree, op.BrowserID)
			removeChild(oldParent, op.BrowserID)
			newParent := findByID(op.ParentBrowserID)
			if newParent == nil {
				newParent = tree.Root(op.TargetPath[0])
			}
			newParent.Children = insertAt(newParent.Children, n, op.Position)

		case diff.OpUpdate:
			n := findByID(op.BrowserID)
			if n == nil {
				t.Fatalf("apply UPDATE: could not find node %s", op.BrowserID)
			}
			for _, c := range op.Changes {
				switch c {
				case "name":
					n.Name = op.Name
				case "url":
					n.URL = op.URL
				}
			}
		}
	}

	tree.Normalize()
	return tree
}

func findParent(tree *bookmarktree.BookmarkTree, childID string) *bookmarktree.Node {
	var found *bookmarktree.Node
	tree.Walk(func(n *bookmarktree.Node) bool {
		for _, c := range n.Children {
			if c.BrowserID == childID {
				found = n
				return false
			}
		}
		return true
	})
	return found
}

func removeChild(parent *bookmarktree.Node, childID string) {
	out := parent.Children[:0]
	for _, c := range parent.Children {
		if c.BrowserID != childID {
			out = append(out, c)
		}
	}
	parent.Children = out
}

func insertAt(children []*bookmarktree.Node, n *bookmarktree.Node, pos int) []*bookmarktree.Node {
	if pos < 0 || pos > len(children) {
		pos = len(children)
	}
	out := make([]*bookmarktree.Node, 0, len(children)+1)
	out = append(out, children[:pos]...)
	out = append(out, n)
	out = append(out, children[pos:]...)
	return out
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// assertStructurallyEqual compares two trees ignoring BrowserID/Index,
// per §11.2 step 5.
func assertStructurallyEqual(t *testing.T, desired, actual *bookmarktree.BookmarkTree) {
	t.Helper()
	if len(desired.Roots) != len(actual.Roots) {
		t.Fatalf("root count mismatch: desired=%d actual=%d", len(desired.Roots), len(actual.Roots))
	}
	for _, name := range []string{"bar", "other"} {
		d := desired.Root(name)
		a := actual.Root(name)
		assertNodesEqual(t, d, a, name)
	}
}

func assertNodesEqual(t *testing.T, d, a *bookmarktree.Node, path string) {
	t.Helper()
	if d.Kind != a.Kind {
		t.Fatalf("%s: kind mismatch: desired=%s actual=%s", path, d.Kind, a.Kind)
	}
	if d.Name != a.Name {
		t.Fatalf("%s: name mismatch: desired=%q actual=%q", path, d.Name, a.Name)
	}
	if d.URL != a.URL {
		t.Fatalf("%s: url mismatch: desired=%q actual=%q", path, d.URL, a.URL)
	}
	if len(d.Children) != len(a.Children) {
		t.Fatalf("%s: children count mismatch: desired=%d actual=%d (%v vs %v)", path, len(d.Children), len(a.Children), childNames(d), childNames(a))
	}
	for i := range d.Children {
		assertNodesEqual(t, d.Children[i], a.Children[i], path+"/"+d.Children[i].Name)
	}
}
