package sync

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/inchestnov/marko/cli/internal/bookmarktree"
	"github.com/inchestnov/marko/cli/internal/version"
)

// BuildExport constructs the ExportFile payload for a given desired tree
// (docs/architecture.md §8.4).
func BuildExport(tree *bookmarktree.BookmarkTree) *ExportFile {
	return &ExportFile{
		FormatVersion: "1",
		GeneratedAt:   time.Now().UTC().Format(time.RFC3339),
		MarkoVersion:  version.Version,
		DesiredTree:   tree,
	}
}

// WriteExport renders tree and writes the marko export JSON file format
// to path. Returns the number of nodes written (folders + bookmarks,
// including the two roots) for CLI reporting purposes.
func WriteExport(path string, tree *bookmarktree.BookmarkTree) (int, error) {
	export := BuildExport(tree)

	data, err := json.MarshalIndent(export, "", "  ")
	if err != nil {
		return 0, fmt.Errorf("sync: marshaling export JSON: %w", err)
	}

	if err := os.WriteFile(path, data, 0o644); err != nil {
		return 0, fmt.Errorf("sync: writing export file %q: %w", path, err)
	}

	return CountNodes(tree), nil
}

// CountNodes returns the total number of nodes in tree, including both
// roots and all descendants.
func CountNodes(tree *bookmarktree.BookmarkTree) int {
	count := 0
	tree.Walk(func(n *bookmarktree.Node) bool {
		count++
		return true
	})
	return count
}
