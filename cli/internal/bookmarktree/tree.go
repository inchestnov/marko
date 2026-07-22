// Package bookmarktree defines the BookmarkTree struct and helpers (path
// resolution, walking) representing the desired/actual bookmark state.
// See docs/architecture.md §6.
package bookmarktree

// NodeKind distinguishes folders from bookmarks in the tree.
type NodeKind string

const (
	KindFolder   NodeKind = "folder"
	KindBookmark NodeKind = "bookmark"
)

// Node is a single folder or bookmark in the tree. Bookmarks never have
// Children. Folders never have URL set.
type Node struct {
	Kind NodeKind `json:"kind"`
	Name string   `json:"name"`
	URL  string   `json:"url,omitempty"`

	// Path is the ordered list of ancestor folder names from the
	// applicable root down to (but excluding) this node itself, e.g.
	// ["bar", "Work", "Kubernetes"]. Path[0] is always "bar" or "other".
	// Path is derivable from tree position but is denormalized onto the
	// Node for O(1) lookup during diff/matching.
	Path []string `json:"path"`

	Children []*Node `json:"children,omitempty"`

	// BrowserID is populated only on Browser State trees (never on
	// Desired State trees, since desired nodes have no browser identity
	// yet). Empty string means "not yet created".
	BrowserID string `json:"browserId,omitempty"`

	// Index is this node's 0-based position among its siblings, assigned
	// during render/normalize and used by the diff engine to detect
	// ordering changes that warrant a MOVE.
	Index int `json:"index"`
}

// BookmarkTree is the root container. Roots holds exactly two entries
// with Name "bar" and "other" (Chrome's two native top-level folders that
// Marko manages; "Bookmarks Bar" id "1" and "Other Bookmarks" id "2").
type BookmarkTree struct {
	Roots []*Node `json:"roots"`
}

// Root returns the root node with the given name ("bar" or "other"), or
// nil if not present.
func (t *BookmarkTree) Root(name string) *Node {
	if t == nil {
		return nil
	}
	for _, r := range t.Roots {
		if r.Name == name {
			return r
		}
	}
	return nil
}

// FindChild returns the direct child of n with the given kind and name,
// or nil if none exists.
func (n *Node) FindChild(kind NodeKind, name string) *Node {
	if n == nil {
		return nil
	}
	for _, c := range n.Children {
		if c.Kind == kind && c.Name == name {
			return c
		}
	}
	return nil
}

// Walk performs a pre-order traversal over the node and all of its
// descendants, invoking fn for each node visited (including n itself).
// If fn returns false, the traversal does not descend into that node's
// children (but continues with siblings/ancestors' remaining traversal).
func (n *Node) Walk(fn func(*Node) bool) {
	if n == nil {
		return
	}
	if !fn(n) {
		return
	}
	for _, c := range n.Children {
		c.Walk(fn)
	}
}

// Walk performs a pre-order traversal starting at every root of the tree.
func (t *BookmarkTree) Walk(fn func(*Node) bool) {
	if t == nil {
		return
	}
	for _, r := range t.Roots {
		r.Walk(fn)
	}
}

// Clone returns a deep copy of the node.
func (n *Node) Clone() *Node {
	if n == nil {
		return nil
	}
	cp := *n
	cp.Path = append([]string(nil), n.Path...)
	if n.Children != nil {
		cp.Children = make([]*Node, len(n.Children))
		for i, c := range n.Children {
			cp.Children[i] = c.Clone()
		}
	}
	return &cp
}

// Clone returns a deep copy of the tree.
func (t *BookmarkTree) Clone() *BookmarkTree {
	if t == nil {
		return nil
	}
	cp := &BookmarkTree{}
	for _, r := range t.Roots {
		cp.Roots = append(cp.Roots, r.Clone())
	}
	return cp
}

// AssignIndexAndPath walks the tree (or subtree rooted at n) and assigns
// Index (position among siblings) and Path (ancestor folder names,
// root-first, excluding the node itself) to every node. parentPath is the
// Path to use as the base for n's direct children; pass nil when calling
// on a top-level root.
func AssignIndexAndPath(n *Node, parentPath []string) {
	if n == nil {
		return
	}
	n.Path = append([]string(nil), parentPath...)
	childPath := append(append([]string(nil), parentPath...), n.Name)
	for i, c := range n.Children {
		c.Index = i
		AssignIndexAndPath(c, childPath)
	}
}

// Normalize assigns Index/Path across the whole tree, treating each root
// itself as having an empty Path (per §6's Node.Path convention that
// Path[0] is "bar"/"other", i.e. the root's own name is the first Path
// element of its children, not of itself).
func (t *BookmarkTree) Normalize() {
	if t == nil {
		return
	}
	for i, r := range t.Roots {
		r.Index = i
		r.Path = nil
		childPath := []string{r.Name}
		for j, c := range r.Children {
			c.Index = j
			AssignIndexAndPath(c, childPath)
		}
	}
}
