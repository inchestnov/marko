package diff

import "github.com/inchestnov/marko/cli/internal/bookmarktree"

// pairing is a matched (desired, actual) node pair, or one side nil to
// represent an unmatched node (CREATE when Actual == nil, DELETE when
// Desired == nil). Children holds the recursively matched pairing for
// this pair's children, already ordered to match Desired's declared
// child order (with trailing unmatched-actual DELETE pairings appended).
type pairing struct {
	Desired  *bookmarktree.Node
	Actual   *bookmarktree.Node
	Children []*pairing

	// actualOrigIndex is Actual's 0-based position among its original
	// siblings in the actual tree (before this diff's ops are applied).
	// -1 when Actual is nil (unmatched desired / CREATE). Used to
	// compute each survivor's current position for MOVE detection
	// (§7.2), independent of the desired-order iteration used to build
	// `pairs`.
	actualOrigIndex int
}

// matchChildren implements docs/architecture.md §7.1 step 2: match the
// children of a matched (desiredParent, actualParent) pair using the
// precedence exact-match -> rename-match (folders) -> URL-match
// (bookmarks) -> CREATE/DELETE for anything left over. Recurses into
// every matched folder pair.
func matchChildren(desiredParent, actualParent *bookmarktree.Node) []*pairing {
	desiredChildren := childrenOrEmpty(desiredParent)
	actualChildren := childrenOrEmpty(actualParent)

	consumed := make([]bool, len(actualChildren))
	matchOf := make([]int, len(desiredChildren)) // index into actualChildren, or -1
	for i := range matchOf {
		matchOf[i] = -1
	}

	// Pass 1: exact match (same Kind, Name, and URL for bookmarks).
	for di, d := range desiredChildren {
		for ai, a := range actualChildren {
			if consumed[ai] {
				continue
			}
			if exactMatch(d, a) {
				matchOf[di] = ai
				consumed[ai] = true
				break
			}
		}
	}

	// Pass 2: rename-match for folders (>50% grandchild overlap, single
	// best unambiguous candidate).
	for di, d := range desiredChildren {
		if matchOf[di] != -1 || d.Kind != bookmarktree.KindFolder {
			continue
		}
		best := -1
		bestCount := -1
		ambiguous := false
		for ai, a := range actualChildren {
			if consumed[ai] || a.Kind != bookmarktree.KindFolder {
				continue
			}
			if a.Name == d.Name {
				continue // would have exact-matched already if identical
			}
			overlap := grandchildOverlapCount(d, a)
			threshold := (len(d.Children) + 1) / 2 // strictly more than 50%
			if overlap == 0 || overlap*2 <= len(d.Children) {
				continue
			}
			_ = threshold
			if overlap > bestCount {
				bestCount = overlap
				best = ai
				ambiguous = false
			} else if overlap == bestCount {
				ambiguous = true
			}
		}
		if best != -1 && !ambiguous {
			matchOf[di] = best
			consumed[best] = true
		}
	}

	// Pass 3: URL match fallback for bookmarks (same URL, different Name).
	for di, d := range desiredChildren {
		if matchOf[di] != -1 || d.Kind != bookmarktree.KindBookmark {
			continue
		}
		for ai, a := range actualChildren {
			if consumed[ai] || a.Kind != bookmarktree.KindBookmark {
				continue
			}
			if a.URL == d.URL {
				matchOf[di] = ai
				consumed[ai] = true
				break
			}
		}
	}

	var pairs []*pairing
	for di, d := range desiredChildren {
		if matchOf[di] == -1 {
			// CREATE: unmatched desired node. Children (if a folder) are
			// all CREATE too, since the parent doesn't exist yet.
			pairs = append(pairs, &pairing{Desired: d, Actual: nil, Children: createSubtree(d), actualOrigIndex: -1})
			continue
		}
		ai := matchOf[di]
		a := actualChildren[ai]
		var children []*pairing
		if d.Kind == bookmarktree.KindFolder {
			children = matchChildren(d, a)
		}
		pairs = append(pairs, &pairing{Desired: d, Actual: a, Children: children, actualOrigIndex: ai})
	}

	// Anything left in actual unconsumed -> DELETE (top-most unmatched
	// folder only; children are implicitly removed by removeTree and
	// must not be separately walked/emitted).
	for ai, a := range actualChildren {
		if !consumed[ai] {
			pairs = append(pairs, &pairing{Desired: nil, Actual: a, Children: nil, actualOrigIndex: ai})
		}
	}

	return pairs
}

func childrenOrEmpty(n *bookmarktree.Node) []*bookmarktree.Node {
	if n == nil {
		return nil
	}
	return n.Children
}

func exactMatch(d, a *bookmarktree.Node) bool {
	if d.Kind != a.Kind || d.Name != a.Name {
		return false
	}
	if d.Kind == bookmarktree.KindBookmark {
		return d.URL == a.URL
	}
	return true
}

// grandchildOverlapCount counts how many of desired folder d's direct
// children exact-match (by Kind+Name(+URL)) some direct child of actual
// folder a.
func grandchildOverlapCount(d, a *bookmarktree.Node) int {
	count := 0
	usedA := make([]bool, len(a.Children))
	for _, dc := range d.Children {
		for ai, ac := range a.Children {
			if usedA[ai] {
				continue
			}
			if exactMatch(dc, ac) {
				usedA[ai] = true
				count++
				break
			}
		}
	}
	return count
}

// createSubtree builds pairing nodes for a desired subtree that has no
// actual counterpart at all (its parent is itself being created).
func createSubtree(d *bookmarktree.Node) []*pairing {
	var out []*pairing
	for _, c := range d.Children {
		out = append(out, &pairing{Desired: c, Actual: nil, Children: createSubtree(c)})
	}
	return out
}
