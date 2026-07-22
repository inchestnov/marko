package diff

import (
	"testing"

	"github.com/inchestnov/marko/cli/internal/bookmarktree"
)

func emptyTree() *bookmarktree.BookmarkTree {
	t := &bookmarktree.BookmarkTree{
		Roots: []*bookmarktree.Node{
			{Kind: bookmarktree.KindFolder, Name: "bar", BrowserID: "1"},
			{Kind: bookmarktree.KindFolder, Name: "other", BrowserID: "2"},
		},
	}
	return t
}

func findOp(ops []Operation, typ OpType, name string) (Operation, bool) {
	for _, o := range ops {
		if o.Type == typ && o.Name == name {
			return o, true
		}
	}
	return Operation{}, false
}

func countType(ops []Operation, typ OpType) int {
	n := 0
	for _, o := range ops {
		if o.Type == typ {
			n++
		}
	}
	return n
}

func TestDiff_ExactMatch_NoOps(t *testing.T) {
	desired := emptyTree()
	desired.Root("bar").Children = []*bookmarktree.Node{
		{Kind: bookmarktree.KindBookmark, Name: "Gmail", URL: "https://mail.google.com"},
	}
	actual := emptyTree()
	actual.Root("bar").Children = []*bookmarktree.Node{
		{Kind: bookmarktree.KindBookmark, Name: "Gmail", URL: "https://mail.google.com", BrowserID: "10"},
	}

	plan := Diff(desired, actual)
	if len(plan.Operations) != 0 {
		t.Fatalf("expected no ops for exact match, got %+v", plan.Operations)
	}
}

func TestDiff_RenameMatch_FolderOverlap(t *testing.T) {
	desired := emptyTree()
	desired.Root("bar").Children = []*bookmarktree.Node{
		{Kind: bookmarktree.KindFolder, Name: "NewName", Children: []*bookmarktree.Node{
			{Kind: bookmarktree.KindBookmark, Name: "A", URL: "https://example.com/a"},
			{Kind: bookmarktree.KindBookmark, Name: "B", URL: "https://example.com/b"},
		}},
	}
	actual := emptyTree()
	actual.Root("bar").Children = []*bookmarktree.Node{
		{Kind: bookmarktree.KindFolder, Name: "OldName", BrowserID: "20", Children: []*bookmarktree.Node{
			{Kind: bookmarktree.KindBookmark, Name: "A", URL: "https://example.com/a", BrowserID: "21"},
			{Kind: bookmarktree.KindBookmark, Name: "B", URL: "https://example.com/b", BrowserID: "22"},
		}},
	}

	plan := Diff(desired, actual)
	op, ok := findOp(plan.Operations, OpUpdate, "NewName")
	if !ok {
		t.Fatalf("expected UPDATE (rename) for folder, got %+v", plan.Operations)
	}
	if op.BrowserID != "20" {
		t.Fatalf("expected rename-matched folder to keep BrowserID 20, got %q", op.BrowserID)
	}
	if len(op.Changes) != 1 || op.Changes[0] != "name" {
		t.Fatalf("expected Changes=[name], got %v", op.Changes)
	}
	if countType(plan.Operations, OpCreate) != 0 || countType(plan.Operations, OpDelete) != 0 {
		t.Fatalf("expected no CREATE/DELETE for rename-matched folder, got %+v", plan.Operations)
	}
}

func TestDiff_URLMatch_BookmarkRename(t *testing.T) {
	desired := emptyTree()
	desired.Root("bar").Children = []*bookmarktree.Node{
		{Kind: bookmarktree.KindBookmark, Name: "New Title", URL: "https://example.com/x"},
	}
	actual := emptyTree()
	actual.Root("bar").Children = []*bookmarktree.Node{
		{Kind: bookmarktree.KindBookmark, Name: "Old Title", URL: "https://example.com/x", BrowserID: "30"},
	}

	plan := Diff(desired, actual)
	op, ok := findOp(plan.Operations, OpUpdate, "New Title")
	if !ok {
		t.Fatalf("expected UPDATE via URL-match, got %+v", plan.Operations)
	}
	if op.BrowserID != "30" {
		t.Fatalf("expected matched BrowserID 30, got %q", op.BrowserID)
	}
	if len(op.Changes) != 1 || op.Changes[0] != "name" {
		t.Fatalf("expected Changes=[name], got %v", op.Changes)
	}
}

func TestDiff_Create_NoActualMatch(t *testing.T) {
	desired := emptyTree()
	desired.Root("bar").Children = []*bookmarktree.Node{
		{Kind: bookmarktree.KindBookmark, Name: "New", URL: "https://example.com/new"},
	}
	actual := emptyTree()

	plan := Diff(desired, actual)
	op, ok := findOp(plan.Operations, OpCreate, "New")
	if !ok {
		t.Fatalf("expected CREATE, got %+v", plan.Operations)
	}
	if op.BrowserID != "" {
		t.Fatalf("expected empty BrowserID for CREATE, got %q", op.BrowserID)
	}
	if len(op.TargetPath) != 2 || op.TargetPath[0] != "bar" || op.TargetPath[1] != "New" {
		t.Fatalf("expected TargetPath [bar New], got %v", op.TargetPath)
	}
}

func TestDiff_Delete_NoDesiredMatch(t *testing.T) {
	desired := emptyTree()
	actual := emptyTree()
	actual.Root("bar").Children = []*bookmarktree.Node{
		{Kind: bookmarktree.KindBookmark, Name: "Old", URL: "https://example.com/old", BrowserID: "40"},
	}

	plan := Diff(desired, actual)
	op, ok := findOp(plan.Operations, OpDelete, "Old")
	if !ok {
		t.Fatalf("expected DELETE, got %+v", plan.Operations)
	}
	if op.BrowserID != "40" {
		t.Fatalf("expected BrowserID 40, got %q", op.BrowserID)
	}
}

// TestDiff_Move_ParentChange covers the case where a folder is renamed
// (rename-matched, so its identity/BrowserID carries forward) and its
// children set is compared in place -- a "parent change" MOVE in
// practice manifests as the child being re-homed under the *same
// matched folder pair* whose actual side now has a different
// BrowserID/position than before. Matching is strictly parent-pair
// recursive (§7.1): a leaf whose new desired parent is a wholly
// unrelated actual folder (no rename-match relationship) cannot be
// identified as "moved" by this local algorithm and instead decomposes
// into DELETE (old location) + CREATE (new location), which is the
// scenario this test documents.
func TestDiff_Move_ParentChange_UnrelatedFolders_DecomposesToDeleteCreate(t *testing.T) {
	desired := emptyTree()
	desired.Root("bar").Children = []*bookmarktree.Node{
		{Kind: bookmarktree.KindFolder, Name: "FolderA", Children: []*bookmarktree.Node{
			{Kind: bookmarktree.KindBookmark, Name: "Leaf", URL: "https://example.com/leaf"},
		}},
		{Kind: bookmarktree.KindFolder, Name: "FolderB"},
	}
	actual := emptyTree()
	actual.Root("bar").Children = []*bookmarktree.Node{
		{Kind: bookmarktree.KindFolder, Name: "FolderA", BrowserID: "50"},
		{Kind: bookmarktree.KindFolder, Name: "FolderB", BrowserID: "51", Children: []*bookmarktree.Node{
			{Kind: bookmarktree.KindBookmark, Name: "Leaf", URL: "https://example.com/leaf", BrowserID: "52"},
		}},
	}

	plan := Diff(desired, actual)
	if _, ok := findOp(plan.Operations, OpCreate, "Leaf"); !ok {
		t.Fatalf("expected CREATE for Leaf under its new parent, got %+v", plan.Operations)
	}
	if _, ok := findOp(plan.Operations, OpDelete, "Leaf"); !ok {
		t.Fatalf("expected DELETE for Leaf under its old parent, got %+v", plan.Operations)
	}
}

// TestDiff_Move_ParentChange_ViaRenameMatchedFolder covers the case
// where the parent folder itself is rename-matched (so the same
// BrowserID carries forward under a new Name), and its child set
// differs in a way that produces a MOVE for a child that survives the
// match within that same folder identity.
func TestDiff_Move_ParentChange_WithinSameMatchedFolder(t *testing.T) {
	desired := emptyTree()
	desired.Root("bar").Children = []*bookmarktree.Node{
		{Kind: bookmarktree.KindFolder, Name: "Work", Children: []*bookmarktree.Node{
			{Kind: bookmarktree.KindBookmark, Name: "Second", URL: "https://example.com/second"},
			{Kind: bookmarktree.KindBookmark, Name: "First", URL: "https://example.com/first"},
		}},
	}
	actual := emptyTree()
	actual.Root("bar").Children = []*bookmarktree.Node{
		{Kind: bookmarktree.KindFolder, Name: "Work", BrowserID: "50", Children: []*bookmarktree.Node{
			{Kind: bookmarktree.KindBookmark, Name: "First", URL: "https://example.com/first", BrowserID: "51"},
			{Kind: bookmarktree.KindBookmark, Name: "Second", URL: "https://example.com/second", BrowserID: "52"},
		}},
	}

	plan := Diff(desired, actual)
	if countType(plan.Operations, OpMove) == 0 {
		t.Fatalf("expected at least one MOVE for reordered siblings, got %+v", plan.Operations)
	}
}

func TestDiff_Move_PositionChange(t *testing.T) {
	desired := emptyTree()
	desired.Root("bar").Children = []*bookmarktree.Node{
		{Kind: bookmarktree.KindBookmark, Name: "Second", URL: "https://example.com/second"},
		{Kind: bookmarktree.KindBookmark, Name: "First", URL: "https://example.com/first"},
	}
	actual := emptyTree()
	actual.Root("bar").Children = []*bookmarktree.Node{
		{Kind: bookmarktree.KindBookmark, Name: "First", URL: "https://example.com/first", BrowserID: "60"},
		{Kind: bookmarktree.KindBookmark, Name: "Second", URL: "https://example.com/second", BrowserID: "61"},
	}

	plan := Diff(desired, actual)
	if countType(plan.Operations, OpMove) == 0 {
		t.Fatalf("expected at least one MOVE due to position change, got %+v", plan.Operations)
	}
}

func TestDiff_Update_NameAndURLChange(t *testing.T) {
	desired := emptyTree()
	desired.Root("bar").Children = []*bookmarktree.Node{
		{Kind: bookmarktree.KindBookmark, Name: "NewName", URL: "https://example.com/newurl"},
	}
	actual := emptyTree()
	actual.Root("bar").Children = []*bookmarktree.Node{
		{Kind: bookmarktree.KindBookmark, Name: "OldName", URL: "https://example.com/oldurl", BrowserID: "70"},
	}

	// Neither exact-match nor URL-match applies (both differ), so this
	// should surface as one CREATE + one DELETE, not an UPDATE — this
	// documents the matcher's behavior when neither identity signal
	// matches.
	plan := Diff(desired, actual)
	if countType(plan.Operations, OpCreate) != 1 || countType(plan.Operations, OpDelete) != 1 {
		t.Fatalf("expected CREATE+DELETE when neither exact nor URL match, got %+v", plan.Operations)
	}
}

func TestDiff_MovePlusUpdateCombined(t *testing.T) {
	desired := emptyTree()
	desired.Root("bar").Children = []*bookmarktree.Node{
		{Kind: bookmarktree.KindFolder, Name: "FolderA"},
		{Kind: bookmarktree.KindFolder, Name: "FolderB", Children: []*bookmarktree.Node{
			{Kind: bookmarktree.KindBookmark, Name: "Renamed", URL: "https://example.com/x"},
		}},
	}
	actual := emptyTree()
	actual.Root("bar").Children = []*bookmarktree.Node{
		{Kind: bookmarktree.KindFolder, Name: "FolderA", BrowserID: "80"},
		{Kind: bookmarktree.KindFolder, Name: "FolderB", BrowserID: "81", Children: []*bookmarktree.Node{
			{Kind: bookmarktree.KindBookmark, Name: "Old", URL: "https://example.com/x", BrowserID: "82"},
		}},
	}

	plan := Diff(desired, actual)
	_, hasUpdate := findOp(plan.Operations, OpUpdate, "Renamed")
	if !hasUpdate {
		t.Fatalf("expected UPDATE for renamed bookmark, got %+v", plan.Operations)
	}
	// Same-parent, position unchanged (only child), so no MOVE expected
	// here; this test focuses on ensuring URL-match rename works
	// correctly inside a matched folder. See TestDiff_Move_ParentChange
	// for MOVE and combine below for MOVE+UPDATE on the same node.
}

// TestDiff_MoveAndUpdateSameNode covers a node that needs both a
// position change (MOVE, within its matched parent's sibling set) and a
// field change (UPDATE) simultaneously -- MOVE and UPDATE are not
// mutually exclusive (§7.1 step 4) and MOVE must be ordered before
// UPDATE in the final plan.
func TestDiff_MoveAndUpdateSameNode(t *testing.T) {
	desired := emptyTree()
	desired.Root("bar").Children = []*bookmarktree.Node{
		{Kind: bookmarktree.KindFolder, Name: "Work", Children: []*bookmarktree.Node{
			{Kind: bookmarktree.KindBookmark, Name: "RenamedSecond", URL: "https://example.com/second"},
			{Kind: bookmarktree.KindBookmark, Name: "First", URL: "https://example.com/first"},
		}},
	}
	actual := emptyTree()
	actual.Root("bar").Children = []*bookmarktree.Node{
		{Kind: bookmarktree.KindFolder, Name: "Work", BrowserID: "90", Children: []*bookmarktree.Node{
			{Kind: bookmarktree.KindBookmark, Name: "First", URL: "https://example.com/first", BrowserID: "91"},
			{Kind: bookmarktree.KindBookmark, Name: "OldSecond", URL: "https://example.com/second", BrowserID: "92"},
		}},
	}

	plan := Diff(desired, actual)
	moveOp, hasMove := findOp(plan.Operations, OpMove, "RenamedSecond")
	updateOp, hasUpdate := findOp(plan.Operations, OpUpdate, "RenamedSecond")
	if !hasMove {
		t.Fatalf("expected MOVE for reordered leaf, got %+v", plan.Operations)
	}
	if !hasUpdate {
		t.Fatalf("expected UPDATE for leaf rename, got %+v", plan.Operations)
	}
	if moveOp.ParentBrowserID != "90" {
		t.Fatalf("expected MOVE to target Work's BrowserID 90, got %q", moveOp.ParentBrowserID)
	}
	if len(updateOp.Changes) != 1 || updateOp.Changes[0] != "name" {
		t.Fatalf("expected Changes=[name], got %v", updateOp.Changes)
	}

	// MOVE must appear before UPDATE in the final plan ordering (§7.3).
	moveIdx, updateIdx := -1, -1
	for i, o := range plan.Operations {
		if o.Type == OpMove && o.Name == "RenamedSecond" {
			moveIdx = i
		}
		if o.Type == OpUpdate && o.Name == "RenamedSecond" {
			updateIdx = i
		}
	}
	if moveIdx == -1 || updateIdx == -1 || moveIdx > updateIdx {
		t.Fatalf("expected MOVE before UPDATE, got moveIdx=%d updateIdx=%d", moveIdx, updateIdx)
	}
}

func TestDiff_RecursiveDelete_NoChildDeleteOps(t *testing.T) {
	desired := emptyTree()
	actual := emptyTree()
	actual.Root("other").Children = []*bookmarktree.Node{
		{Kind: bookmarktree.KindFolder, Name: "OldProject", BrowserID: "100", Children: []*bookmarktree.Node{
			{Kind: bookmarktree.KindBookmark, Name: "Child1", URL: "https://example.com/1", BrowserID: "101"},
			{Kind: bookmarktree.KindBookmark, Name: "Child2", URL: "https://example.com/2", BrowserID: "102"},
		}},
	}

	plan := Diff(desired, actual)
	if countType(plan.Operations, OpDelete) != 1 {
		t.Fatalf("expected exactly 1 DELETE (top-most folder only), got %+v", plan.Operations)
	}
	op, _ := findOp(plan.Operations, OpDelete, "OldProject")
	if op.BrowserID != "100" {
		t.Fatalf("expected DELETE targeting folder id 100, got %q", op.BrowserID)
	}
}

func TestDiff_OperationOrdering_AllFourTypes(t *testing.T) {
	desired := emptyTree()
	desired.Root("bar").Children = []*bookmarktree.Node{
		{Kind: bookmarktree.KindFolder, Name: "Kept", Children: []*bookmarktree.Node{
			{Kind: bookmarktree.KindBookmark, Name: "RenamedBookmark", URL: "https://example.com/kept"},
		}},
		{Kind: bookmarktree.KindFolder, Name: "NewFolder", Children: []*bookmarktree.Node{
			{Kind: bookmarktree.KindBookmark, Name: "NewBookmark", URL: "https://example.com/new"},
		}},
	}
	actual := emptyTree()
	actual.Root("bar").Children = []*bookmarktree.Node{
		{Kind: bookmarktree.KindFolder, Name: "Kept", BrowserID: "200", Children: []*bookmarktree.Node{
			{Kind: bookmarktree.KindBookmark, Name: "OldBookmark", URL: "https://example.com/kept", BrowserID: "201"},
		}},
		{Kind: bookmarktree.KindFolder, Name: "ToDelete", BrowserID: "202"},
	}

	plan := Diff(desired, actual)

	if countType(plan.Operations, OpDelete) == 0 {
		t.Fatal("expected at least one DELETE")
	}
	if countType(plan.Operations, OpCreate) == 0 {
		t.Fatal("expected at least one CREATE")
	}
	if countType(plan.Operations, OpUpdate) == 0 {
		t.Fatal("expected at least one UPDATE")
	}

	// Verify ordering: all DELETEs first, then CREATEs, then MOVEs, then UPDATEs.
	seenCreate, seenMove, seenUpdate := false, false, false
	for _, op := range plan.Operations {
		switch op.Type {
		case OpDelete:
			if seenCreate || seenMove || seenUpdate {
				t.Fatalf("DELETE found after other op types: %+v", plan.Operations)
			}
		case OpCreate:
			seenCreate = true
			if seenMove || seenUpdate {
				t.Fatalf("CREATE found after MOVE/UPDATE: %+v", plan.Operations)
			}
		case OpMove:
			seenMove = true
			if seenUpdate {
				t.Fatalf("MOVE found after UPDATE: %+v", plan.Operations)
			}
		case OpUpdate:
			seenUpdate = true
		}
	}
}

func TestDiff_Idempotent_EmptyPlanWhenNoChanges(t *testing.T) {
	desired := emptyTree()
	desired.Root("bar").Children = []*bookmarktree.Node{
		{Kind: bookmarktree.KindFolder, Name: "Work", Children: []*bookmarktree.Node{
			{Kind: bookmarktree.KindBookmark, Name: "A", URL: "https://example.com/a"},
		}},
	}
	actual := emptyTree()
	actual.Root("bar").Children = []*bookmarktree.Node{
		{Kind: bookmarktree.KindFolder, Name: "Work", BrowserID: "300", Children: []*bookmarktree.Node{
			{Kind: bookmarktree.KindBookmark, Name: "A", URL: "https://example.com/a", BrowserID: "301"},
		}},
	}

	plan := Diff(desired, actual)
	if len(plan.Operations) != 0 {
		t.Fatalf("expected idempotent empty plan, got %+v", plan.Operations)
	}
}
