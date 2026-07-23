// Package diff compares two bookmarktree.BookmarkTree values and produces
// an ordered Plan of Operations. See docs/architecture.md §7. diff
// depends only on internal/bookmarktree.
package diff

import (
	"sort"
	"time"

	"github.com/inchestnov/marko/cli/internal/bookmarktree"
)

// OpType identifies the kind of atomic action an Operation represents.
type OpType string

const (
	OpCreate OpType = "CREATE"
	OpUpdate OpType = "UPDATE"
	OpDelete OpType = "DELETE"
	OpMove   OpType = "MOVE"
)

// Operation is one atomic, idempotent action for browserfile to apply to
// the browser's Bookmarks file. Fields are populated according to Type;
// see docs/architecture.md §7 for the full field table.
type Operation struct {
	Type OpType `json:"type"`

	// TargetPath is the desired ancestor path (folder names only, root
	// first) of the node being acted on — used for CREATE where no
	// BrowserID exists yet, and for logging/human display on every op.
	TargetPath []string `json:"targetPath"`

	// Kind: "folder" or "bookmark" (mirrors bookmarktree.NodeKind).
	Kind string `json:"kind"`

	// Name/URL: desired final values. For DELETE, these reflect the
	// actual (about-to-be-removed) node's current values (informational).
	Name string `json:"name"`
	URL  string `json:"url,omitempty"`

	// BrowserID: the actual bookmark node id (as found in the browser's
	// Bookmarks file) this op targets. Empty for CREATE (does not exist yet).
	BrowserID string `json:"browserId,omitempty"`

	// ParentBrowserID: id of the parent folder this node should end up
	// under. For CREATE/MOVE, required. Empty means "one of the two
	// native roots", disambiguated by TargetPath[0] ("bar"/"other").
	ParentBrowserID string `json:"parentBrowserId,omitempty"`

	// Position: desired 0-based index among new siblings, set for
	// CREATE and MOVE so browserfile can insert the node at the right
	// index among its new siblings.
	Position int `json:"position"`

	// Changes lists which fields differ, for UPDATE only: subset of
	// "name", "url".
	Changes []string `json:"changes,omitempty"`
}

// Plan is the full ordered set of operations needed to bring the actual
// tree in line with the desired tree.
type Plan struct {
	GeneratedAt string      `json:"generatedAt"` // RFC3339
	Operations  []Operation `json:"operations"`
}

// Diff computes the Plan required to transform actual into desired,
// following the matching strategy (§7.1), move detection (§7.2), and
// operation ordering (§7.3) rules exactly.
func Diff(desired, actual *bookmarktree.BookmarkTree) *Plan {
	var creates, deletes, moves, updates []Operation

	for _, rootName := range []string{"bar", "other"} {
		d := desired.Root(rootName)
		a := actual.Root(rootName)
		if d == nil {
			d = &bookmarktree.Node{Kind: bookmarktree.KindFolder, Name: rootName}
		}
		if a == nil {
			a = &bookmarktree.Node{Kind: bookmarktree.KindFolder, Name: rootName, BrowserID: nativeRootID(rootName)}
		}

		pairs := matchChildren(d, a)
		c, del, mv, upd := walkPairs(pairs, []string{rootName}, a.BrowserID)
		creates = append(creates, c...)
		deletes = append(deletes, del...)
		moves = append(moves, mv...)
		updates = append(updates, upd...)
	}

	sort.SliceStable(deletes, func(i, j int) bool {
		return len(deletes[i].TargetPath) < len(deletes[j].TargetPath)
	})
	sort.SliceStable(creates, func(i, j int) bool {
		return len(creates[i].TargetPath) < len(creates[j].TargetPath)
	})

	ops := make([]Operation, 0, len(deletes)+len(creates)+len(moves)+len(updates))
	ops = append(ops, deletes...)
	ops = append(ops, creates...)
	ops = append(ops, moves...)
	ops = append(ops, updates...)

	return &Plan{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Operations:  ops,
	}
}

// nativeRootID returns Chrome's well-known id for the given native root
// name, used when the caller's actual tree omits BrowserID on the root
// itself.
func nativeRootID(rootName string) string {
	switch rootName {
	case "bar":
		return "1"
	case "other":
		return "2"
	default:
		return ""
	}
}

// walkPairs converts a matched pairing list into ops, recursing into
// matched folder pairs. parentPath is the TargetPath of the parent these
// pairs are children of; parentBrowserID is the actual browser id new
// children should be created/moved under (may be empty for CREATE
// parents, which is fine since Position/TargetPath fully disambiguate
// and browserfile resolves it via its own TargetPath map at apply time).
func walkPairs(pairs []*pairing, parentPath []string, parentBrowserID string) (creates, deletes, moves, updates []Operation) {
	// Determine desired final child order (only pairs with a Desired
	// side, in the order matchChildren already emitted them — which
	// mirrors desired's declaration/Index order since matchChildren
	// iterates desiredChildren in order first).
	var desiredOrder []*pairing
	for _, p := range pairs {
		if p.Desired != nil {
			desiredOrder = append(desiredOrder, p)
		}
	}

	for _, p := range pairs {
		path := append(append([]string(nil), parentPath...), nameOf(p))

		switch {
		case p.Desired == nil:
			// DELETE: unmatched actual node. Only top-level unmatched
			// folders/bookmarks get an op; children are never
			// separately walked (removeTree is recursive), which is
			// already guaranteed since matchChildren doesn't recurse
			// into unmatched actual nodes.
			deletes = append(deletes, Operation{
				Type:       OpDelete,
				TargetPath: path,
				Kind:       string(p.Actual.Kind),
				Name:       p.Actual.Name,
				URL:        p.Actual.URL,
				BrowserID:  p.Actual.BrowserID,
			})

		case p.Actual == nil:
			// CREATE: unmatched desired node (and, recursively, its
			// whole subtree, whose children are also CREATE-only).
			pos := desiredIndexAmong(desiredOrder, p)
			creates = append(creates, Operation{
				Type:            OpCreate,
				TargetPath:      path,
				Kind:            string(p.Desired.Kind),
				Name:            p.Desired.Name,
				URL:             p.Desired.URL,
				ParentBrowserID: parentBrowserID,
				Position:        pos,
			})
			if p.Desired.Kind == bookmarktree.KindFolder {
				c, d, m, u := walkPairs(p.Children, path, "")
				creates = append(creates, c...)
				deletes = append(deletes, d...)
				moves = append(moves, m...)
				updates = append(updates, u...)
			}

		default:
			// Matched pair: compare fields, detect MOVE, then recurse.
			var changes []string
			if p.Desired.Name != p.Actual.Name {
				changes = append(changes, "name")
			}
			if p.Desired.Kind == bookmarktree.KindBookmark && p.Desired.URL != p.Actual.URL {
				changes = append(changes, "url")
			}

			desiredPos := desiredIndexAmong(desiredOrder, p)
			actualPos := actualSurvivorIndex(pairs, p)

			needsMove := desiredPos != actualPos

			if needsMove {
				moves = append(moves, Operation{
					Type:            OpMove,
					TargetPath:      path,
					Kind:            string(p.Desired.Kind),
					Name:            p.Desired.Name,
					URL:             p.Desired.URL,
					BrowserID:       p.Actual.BrowserID,
					ParentBrowserID: parentBrowserID,
					Position:        desiredPos,
				})
			}

			if len(changes) > 0 {
				updates = append(updates, Operation{
					Type:       OpUpdate,
					TargetPath: path,
					Kind:       string(p.Desired.Kind),
					Name:       p.Desired.Name,
					URL:        p.Desired.URL,
					BrowserID:  p.Actual.BrowserID,
					Changes:    changes,
				})
			}

			if p.Desired.Kind == bookmarktree.KindFolder {
				c, d, m, u := walkPairs(p.Children, path, p.Actual.BrowserID)
				creates = append(creates, c...)
				deletes = append(deletes, d...)
				moves = append(moves, m...)
				updates = append(updates, u...)
			}
		}
	}

	return creates, deletes, moves, updates
}

func nameOf(p *pairing) string {
	if p.Desired != nil {
		return p.Desired.Name
	}
	return p.Actual.Name
}

// desiredIndexAmong returns p's 0-based position within the desired
// final sibling order (i.e. its index among all pairs that have a
// Desired side).
func desiredIndexAmong(desiredOrder []*pairing, target *pairing) int {
	for i, p := range desiredOrder {
		if p == target {
			return i
		}
	}
	return -1
}

// actualSurvivorIndex computes the "current" position of a matched pair
// among actual survivors (matched actual nodes only, i.e. excluding
// nodes that will be DELETEd), laid out in the same relative order they
// appeared in the actual tree (by actualOrigIndex, not by desired
// declaration order), so it can be compared against the final desired
// position per §7.2.
func actualSurvivorIndex(pairs []*pairing, target *pairing) int {
	type survivor struct {
		p       *pairing
		origIdx int
	}
	var survivors []survivor
	for _, p := range pairs {
		if p.Actual == nil {
			continue // unmatched desired (CREATE): not a survivor slot
		}
		survivors = append(survivors, survivor{p, p.actualOrigIndex})
	}
	sort.SliceStable(survivors, func(i, j int) bool {
		return survivors[i].origIdx < survivors[j].origIdx
	})
	for idx, s := range survivors {
		if s.p == target {
			return idx
		}
	}
	return -1
}
