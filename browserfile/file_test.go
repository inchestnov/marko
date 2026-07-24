package browserfile

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/inchestnov/marko/diff"
	"github.com/inchestnov/marko/internal/bookmarktree"
)

// fixtureJSON mirrors the real shape observed from a live Brave profile
// during development (including "other" at id "3", not the commonly
// assumed "2" -- exactly the kind of assumption this package must never
// make), plus a non-empty "synced" root that Marko must never touch, and
// an unknown per-node field ("meta_info", as Brave adds) that must
// survive a read-modify-write round trip untouched.
const fixtureJSON = `{
  "checksum": "stale-checksum-should-be-recomputed",
  "roots": {
    "bookmark_bar": {
      "children": [],
      "date_added": "13400404536767500",
      "guid": "0bc5d13f-2cba-5d74-951f-3f233fe6c908",
      "id": "1",
      "name": "Bookmarks bar",
      "type": "folder"
    },
    "other": {
      "children": [
        {
          "date_added": "13429278277818058",
          "guid": "0a19083b-431e-45d0-87e9-49a19b575d71",
          "id": "189",
          "meta_info": { "power_bookmark_meta": "" },
          "name": "Existing Bookmark",
          "type": "url",
          "url": "https://example.com/existing"
        },
        {
          "children": [
            {
              "date_added": "13429278277818058",
              "guid": "11111111-1111-4111-8111-111111111111",
              "id": "190",
              "name": "Nested",
              "type": "url",
              "url": "https://example.com/nested"
            }
          ],
          "date_added": "13429278277818058",
          "guid": "22222222-2222-4222-8222-222222222222",
          "id": "191",
          "name": "Existing Folder",
          "type": "folder"
        }
      ],
      "date_added": "13400404536767500",
      "guid": "9d417dda-08b5-5c58-8a88-3fbc98e7ec3d",
      "id": "3",
      "name": "Other bookmarks",
      "type": "folder"
    },
    "synced": {
      "children": [
        {
          "date_added": "13400404536767500",
          "guid": "33333333-3333-4333-8333-333333333333",
          "id": "4",
          "name": "Mobile Bookmark, do not touch",
          "type": "url",
          "url": "https://example.com/mobile"
        }
      ],
      "date_added": "13400404536767500",
      "guid": "44444444-4444-4444-8444-444444444444",
      "id": "5",
      "name": "Mobile bookmarks",
      "type": "folder"
    }
  },
  "version": 1
}
`

func writeFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "Bookmarks")
	if err := os.WriteFile(path, []byte(fixtureJSON), 0o600); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}
	return path
}

func TestToBookmarkTree_ReadsRealIDsNotHardcoded(t *testing.T) {
	f, err := ReadFile(writeFixture(t))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	tree, err := f.ToBookmarkTree()
	if err != nil {
		t.Fatalf("ToBookmarkTree: %v", err)
	}

	bar := tree.Root("bar")
	other := tree.Root("other")
	if bar == nil || other == nil {
		t.Fatalf("expected both bar and other roots, got bar=%v other=%v", bar, other)
	}
	if bar.BrowserID != "1" {
		t.Errorf("expected bar id 1, got %q", bar.BrowserID)
	}
	// The whole point of this test: "other"'s real id is 3 in the
	// fixture (mirroring real-world data), not a hardcoded "2".
	if other.BrowserID != "3" {
		t.Errorf("expected other id 3 (not hardcoded), got %q", other.BrowserID)
	}
	if len(other.Children) != 2 {
		t.Fatalf("expected 2 children under other, got %d", len(other.Children))
	}
	if other.Children[1].Name != "Existing Folder" || len(other.Children[1].Children) != 1 {
		t.Fatalf("expected Existing Folder with 1 child, got %+v", other.Children[1])
	}
}

func TestApply_CreateUpdateDeleteMove_RoundTrip(t *testing.T) {
	path := writeFixture(t)
	f, err := ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	actual, err := f.ToBookmarkTree()
	if err != nil {
		t.Fatalf("ToBookmarkTree: %v", err)
	}

	// Desired state: keep "Existing Folder" but rename its child, drop
	// "Existing Bookmark", and add a brand new folder with a bookmark
	// inside (exercising CREATE-of-CREATE parent id threading).
	desired := &bookmarktree.BookmarkTree{Roots: []*bookmarktree.Node{
		{Kind: bookmarktree.KindFolder, Name: "bar"},
		{Kind: bookmarktree.KindFolder, Name: "other", Children: []*bookmarktree.Node{
			{Kind: bookmarktree.KindFolder, Name: "Existing Folder", Children: []*bookmarktree.Node{
				{Kind: bookmarktree.KindBookmark, Name: "Nested (renamed)", URL: "https://example.com/nested"},
			}},
			{Kind: bookmarktree.KindFolder, Name: "New Folder", Children: []*bookmarktree.Node{
				{Kind: bookmarktree.KindBookmark, Name: "New Bookmark", URL: "https://example.com/new"},
			}},
		}},
	}}

	plan := diff.Diff(desired, actual)
	if err := f.Apply(plan); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if err := f.Write(false); err != nil {
		t.Fatalf("Write: %v", err)
	}

	// Re-read from disk and verify.
	f2, err := ReadFile(path)
	if err != nil {
		t.Fatalf("re-reading written file: %v", err)
	}
	actual2, err := f2.ToBookmarkTree()
	if err != nil {
		t.Fatalf("ToBookmarkTree after write: %v", err)
	}

	other := actual2.Root("other")
	if other.BrowserID != "3" {
		t.Fatalf("expected other's real id 3 to survive the round trip, got %q", other.BrowserID)
	}
	if len(other.Children) != 2 {
		t.Fatalf("expected 2 children under other (Existing Folder + New Folder), got %d: %+v", len(other.Children), other.Children)
	}

	existingFolder := other.Children[0]
	if existingFolder.Name != "Existing Folder" || existingFolder.BrowserID != "191" {
		t.Fatalf("expected Existing Folder to keep its id 191, got %+v", existingFolder)
	}
	if len(existingFolder.Children) != 1 || existingFolder.Children[0].Name != "Nested (renamed)" || existingFolder.Children[0].BrowserID != "190" {
		t.Fatalf("expected renamed Nested bookmark to keep its id 190, got %+v", existingFolder.Children)
	}

	newFolder := other.Children[1]
	if newFolder.Name != "New Folder" {
		t.Fatalf("expected New Folder, got %+v", newFolder)
	}
	newID, err := strconv.Atoi(newFolder.BrowserID)
	if err != nil || newID <= 191 {
		t.Fatalf("expected New Folder to get a fresh id > 191, got %q", newFolder.BrowserID)
	}
	if len(newFolder.Children) != 1 || newFolder.Children[0].Name != "New Bookmark" || newFolder.Children[0].URL != "https://example.com/new" {
		t.Fatalf("expected New Bookmark under New Folder, got %+v", newFolder.Children)
	}
	newBookmarkID, err := strconv.Atoi(newFolder.Children[0].BrowserID)
	if err != nil || newBookmarkID <= newID {
		t.Fatalf("expected New Bookmark's id (%q) to be greater than its parent's (%q)", newFolder.Children[0].BrowserID, newFolder.BrowserID)
	}

	// Idempotency: re-diffing desired against the now-written actual
	// state should be empty.
	plan2 := diff.Diff(desired, actual2)
	if len(plan2.Operations) != 0 {
		t.Fatalf("expected empty plan on re-diff (idempotency), got %+v", plan2.Operations)
	}

	// The untouched "synced"/mobile root and unknown per-node fields
	// (Brave's "meta_info") must survive completely unmodified.
	rawData, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading raw written file: %v", err)
	}
	var raw map[string]interface{}
	if err := json.Unmarshal(rawData, &raw); err != nil {
		t.Fatalf("unmarshaling raw written file: %v", err)
	}
	roots := raw["roots"].(map[string]interface{})
	synced := roots["synced"].(map[string]interface{})
	if synced["id"] != "5" {
		t.Fatalf("expected synced root id 5 preserved, got %v", synced["id"])
	}
	syncedChildren := synced["children"].([]interface{})
	if len(syncedChildren) != 1 || syncedChildren[0].(map[string]interface{})["name"] != "Mobile Bookmark, do not touch" {
		t.Fatalf("expected synced root's child untouched, got %+v", syncedChildren)
	}
}

func TestApply_Delete(t *testing.T) {
	path := writeFixture(t)
	f, err := ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	actual, err := f.ToBookmarkTree()
	if err != nil {
		t.Fatalf("ToBookmarkTree: %v", err)
	}

	// Desired: drop "Existing Bookmark" and the whole "Existing Folder"
	// subtree entirely.
	desired := &bookmarktree.BookmarkTree{Roots: []*bookmarktree.Node{
		{Kind: bookmarktree.KindFolder, Name: "bar"},
		{Kind: bookmarktree.KindFolder, Name: "other"},
	}}

	plan := diff.Diff(desired, actual)
	if err := f.Apply(plan); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	actual2, err := f.ToBookmarkTree()
	if err != nil {
		t.Fatalf("ToBookmarkTree after apply: %v", err)
	}
	other := actual2.Root("other")
	if len(other.Children) != 0 {
		t.Fatalf("expected other to be empty after deleting everything, got %+v", other.Children)
	}
}

func TestWrite_BackupCreatesRecoverableCopy(t *testing.T) {
	path := writeFixture(t)
	f, err := ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	f.raw["roots"].(map[string]interface{})["other"].(map[string]interface{})["name"] = "mutated"

	if err := f.Write(true); err != nil {
		t.Fatalf("Write: %v", err)
	}

	matches, err := filepath.Glob(path + ".marko-backup-*")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected exactly one backup file, got %v", matches)
	}
	backupData, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatalf("reading backup: %v", err)
	}
	if string(backupData) != fixtureJSON {
		t.Fatalf("expected backup to contain the original, unmodified content")
	}
}

func TestLocateBookmarksFile_RequiresBrowser(t *testing.T) {
	if _, err := LocateBookmarksFile(""); err == nil {
		t.Fatal("expected an error when no browser is specified")
	}
}

func TestLocateBookmarksFile_UsesDefaultProfile(t *testing.T) {
	path, err := LocateBookmarksFile("brave")
	if err != nil {
		t.Fatalf("LocateBookmarksFile: %v", err)
	}
	if filepath.Base(path) != "Bookmarks" {
		t.Fatalf("expected path to end in Bookmarks, got %q", path)
	}
	if filepath.Base(filepath.Dir(path)) != DefaultProfile {
		t.Fatalf("expected default profile %q as the parent dir, got %q", DefaultProfile, path)
	}
}

func TestIsBrowserRunning_FalseWhenNoLockFile(t *testing.T) {
	path := writeFixture(t)
	if IsBrowserRunning(path) {
		t.Fatal("expected no SingletonLock next to a fresh temp fixture")
	}
}
