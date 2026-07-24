// Package renderer converts a resolved config into a
// bookmarktree.BookmarkTree. See docs/architecture.md §6. renderer
// depends only on internal/model, template, and internal/bookmarktree.
package renderer

import (
	"github.com/inchestnov/marko/internal/bookmarktree"
	"github.com/inchestnov/marko/template"
)

// Render implements the algorithm from docs/architecture.md §6.1: it
// creates the two native roots ("bar", "other"), attaches every resolved
// collection's contents under the appropriate root (optionally nested in
// the collection's own folder), normalizes duplicate sibling folder
// names by merging their children, and finally assigns Index/Path to
// every node.
func Render(res *template.Result) (*bookmarktree.BookmarkTree, error) {
	tree := &bookmarktree.BookmarkTree{
		Roots: []*bookmarktree.Node{
			{Kind: bookmarktree.KindFolder, Name: "bar"},
			{Kind: bookmarktree.KindFolder, Name: "other"},
		},
	}

	for _, name := range res.Order {
		rc := res.Collections[name]
		root := tree.Root(rc.Root)
		if root == nil {
			// Defensive: Root defaults to "other" during resolution, so
			// this should never happen, but fall back rather than panic.
			root = tree.Root("other")
		}

		target := root
		if rc.HasFolder {
			target = attachFolder(root, rc.FolderName)
		}

		target.Children = append(target.Children, bookmarksToNodes(rc.Bookmarks)...)
		target.Children = append(target.Children, groupsToNodes(rc.Groups)...)
	}

	normalizeTree(tree)
	tree.Normalize()

	return tree, nil
}

// attachFolder finds (or creates) a direct child folder of parent named
// name, per §6.1 step 2.2 ("create/find a child folder node ... and
// descend into it").
func attachFolder(parent *bookmarktree.Node, name string) *bookmarktree.Node {
	if existing := parent.FindChild(bookmarktree.KindFolder, name); existing != nil {
		return existing
	}
	n := &bookmarktree.Node{Kind: bookmarktree.KindFolder, Name: name}
	parent.Children = append(parent.Children, n)
	return n
}

func bookmarksToNodes(bms []template.ResolvedBookmark) []*bookmarktree.Node {
	out := make([]*bookmarktree.Node, 0, len(bms))
	for _, b := range bms {
		out = append(out, &bookmarktree.Node{Kind: bookmarktree.KindBookmark, Name: b.Name, URL: b.URL})
	}
	return out
}

func groupsToNodes(groups []*template.ResolvedGroup) []*bookmarktree.Node {
	out := make([]*bookmarktree.Node, 0, len(groups))
	for _, g := range groups {
		n := &bookmarktree.Node{Kind: bookmarktree.KindFolder, Name: g.Name}
		n.Children = append(n.Children, bookmarksToNodes(g.Bookmarks)...)
		n.Children = append(n.Children, groupsToNodes(g.Groups)...)
		out = append(out, n)
	}
	return out
}

// normalizeTree implements §6.1 step 3: walk the whole tree and, for
// every folder, merge any children folders that share the same Name into
// a single folder node with concatenated, ordered children. Duplicate
// bookmark (name, url) pairs are deduplicated into one node; bookmarks
// with the same name but different url are left as-is (the validator is
// responsible for rejecting that case as E_DUPLICATE_SIBLING before the
// renderer ever runs in the normal CLI pipeline, but this function must
// not panic if it's called on unvalidated input).
func normalizeTree(t *bookmarktree.BookmarkTree) {
	for _, r := range t.Roots {
		normalizeChildren(r)
	}
}

func normalizeChildren(n *bookmarktree.Node) {
	if n == nil || len(n.Children) == 0 {
		return
	}

	var merged []*bookmarktree.Node
	folderIndex := make(map[string]*bookmarktree.Node)
	bookmarkIndex := make(map[string]*bookmarktree.Node) // key: name + "\x00" + url

	for _, c := range n.Children {
		switch c.Kind {
		case bookmarktree.KindFolder:
			if existing, ok := folderIndex[c.Name]; ok {
				existing.Children = append(existing.Children, c.Children...)
				continue
			}
			folderIndex[c.Name] = c
			merged = append(merged, c)
		case bookmarktree.KindBookmark:
			key := c.Name + "\x00" + c.URL
			if _, ok := bookmarkIndex[key]; ok {
				// Identical (name, url): silently deduplicated.
				continue
			}
			bookmarkIndex[key] = c
			merged = append(merged, c)
		default:
			merged = append(merged, c)
		}
	}

	n.Children = merged
	for _, c := range n.Children {
		if c.Kind == bookmarktree.KindFolder {
			normalizeChildren(c)
		}
	}
}
