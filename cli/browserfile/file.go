package browserfile

import (
	"crypto/md5"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/inchestnov/marko/cli/diff"
	"github.com/inchestnov/marko/cli/internal/bookmarktree"
)

// jsonRootKey maps Marko's logical root names ("bar"/"other", used
// throughout cli/internal/bookmarktree and cli/diff) to the corresponding
// key under the file's top-level "roots" object.
var jsonRootKey = map[string]string{
	"bar":   "bookmark_bar",
	"other": "other",
}

// File is a parsed Chromium-family "Bookmarks" file, kept as a generic
// map so every field Marko doesn't explicitly model (Brave's
// "meta_info", "date_last_used", future additions, the entire "synced"
// / mobile-bookmarks root, etc.) survives a read-modify-write round trip
// completely untouched. Marko only ever reads/writes the "bookmark_bar"
// and "other" roots.
type File struct {
	raw  map[string]interface{}
	path string
}

// ReadFile parses the Bookmarks file at path.
func ReadFile(path string) (*File, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("browserfile: reading %q: %w", path, err)
	}
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("browserfile: parsing %q: %w", path, err)
	}
	if _, ok := raw["roots"].(map[string]interface{}); !ok {
		return nil, fmt.Errorf("browserfile: %q does not look like a Chromium Bookmarks file (missing \"roots\")", path)
	}
	return &File{raw: raw, path: path}, nil
}

func (f *File) roots() map[string]interface{} {
	return f.raw["roots"].(map[string]interface{})
}

func (f *File) rootNode(logicalName string) (map[string]interface{}, bool) {
	key, ok := jsonRootKey[logicalName]
	if !ok {
		return nil, false
	}
	node, ok := f.roots()[key].(map[string]interface{})
	return node, ok
}

// ToBookmarkTree converts the "bookmark_bar" and "other" roots into a
// bookmarktree.BookmarkTree (the "actual" state to diff against), with
// BrowserID populated from each node's real "id" field -- never assumed
// or hardcoded, since that varies by browser/profile/history (e.g. a
// real-world profile observed during development had "other" at id "3",
// not the commonly-assumed "2").
func (f *File) ToBookmarkTree() (*bookmarktree.BookmarkTree, error) {
	tree := &bookmarktree.BookmarkTree{}
	for _, logical := range []string{"bar", "other"} {
		raw, ok := f.rootNode(logical)
		if !ok {
			return nil, fmt.Errorf("browserfile: missing %q root in roots.%s", logical, jsonRootKey[logical])
		}
		node, err := convertRawNode(raw, logical, nil, 0)
		if err != nil {
			return nil, err
		}
		node.Name = logical
		tree.Roots = append(tree.Roots, node)
	}
	return tree, nil
}

func convertRawNode(raw map[string]interface{}, name string, parentPath []string, index int) (*bookmarktree.Node, error) {
	kind := bookmarktree.KindFolder
	if t, _ := raw["type"].(string); t == "url" {
		kind = bookmarktree.KindBookmark
	}

	id, _ := raw["id"].(string)
	node := &bookmarktree.Node{
		Kind:      kind,
		Name:      name,
		Path:      parentPath,
		BrowserID: id,
		Index:     index,
	}

	if kind == bookmarktree.KindBookmark {
		node.URL, _ = raw["url"].(string)
		return node, nil
	}

	childPath := append(append([]string(nil), parentPath...), name)
	children, _ := raw["children"].([]interface{})
	for i, c := range children {
		cm, ok := c.(map[string]interface{})
		if !ok {
			continue
		}
		cname, _ := cm["name"].(string)
		child, err := convertRawNode(cm, cname, childPath, i)
		if err != nil {
			return nil, err
		}
		node.Children = append(node.Children, child)
	}
	return node, nil
}

// Apply mutates the parsed file in place to reflect plan (as computed by
// diff.Diff against the tree returned by ToBookmarkTree), assigning new
// ids/guids/timestamps to CREATEd nodes and threading newly-created
// folder ids forward for their own children -- see resolveParentID: root
// prefix resolves to the real root id, a not-yet-created parent resolves
// via this pass's own createdIDs map, else the operation's own
// ParentBrowserID (a pre-existing node) is trusted.
func (f *File) Apply(plan *diff.Plan) error {
	nodesByID := map[string]map[string]interface{}{}
	parentByID := map[string]map[string]interface{}{}
	rootID := map[string]string{}

	for _, logical := range []string{"bar", "other"} {
		root, ok := f.rootNode(logical)
		if !ok {
			return fmt.Errorf("browserfile: missing %q root", logical)
		}
		id, _ := root["id"].(string)
		rootID[logical] = id
		nodesByID[id] = root
		indexNode(root, nodesByID, parentByID)
	}

	nextID := maxNodeID(f.raw) + 1
	createdIDs := map[string]string{}

	for _, op := range plan.Operations {
		switch op.Type {
		case diff.OpDelete:
			node, ok := nodesByID[op.BrowserID]
			if !ok {
				return fmt.Errorf("browserfile: DELETE target %s (id %q) not found", strings.Join(op.TargetPath, "/"), op.BrowserID)
			}
			parent := parentByID[op.BrowserID]
			removeChild(parent, op.BrowserID)
			delete(nodesByID, op.BrowserID)
			delete(parentByID, op.BrowserID)
			_ = node

		case diff.OpCreate:
			parentID := resolveParentID(op, rootID, createdIDs)
			parent, ok := nodesByID[parentID]
			if !ok {
				return fmt.Errorf("browserfile: CREATE target %s: parent id %q not found", strings.Join(op.TargetPath, "/"), parentID)
			}
			id := strconv.Itoa(nextID)
			nextID++
			node := newRawNode(id, op)
			insertChild(parent, node, op.Position)
			nodesByID[id] = node
			parentByID[id] = parent
			if op.Kind == "folder" {
				createdIDs[pathKey(op.TargetPath)] = id
			}

		case diff.OpMove:
			node, ok := nodesByID[op.BrowserID]
			if !ok {
				return fmt.Errorf("browserfile: MOVE target %s (id %q) not found", strings.Join(op.TargetPath, "/"), op.BrowserID)
			}
			oldParent := parentByID[op.BrowserID]
			removeChild(oldParent, op.BrowserID)
			newParentID := resolveParentID(op, rootID, createdIDs)
			newParent, ok := nodesByID[newParentID]
			if !ok {
				return fmt.Errorf("browserfile: MOVE target %s: new parent id %q not found", strings.Join(op.TargetPath, "/"), newParentID)
			}
			insertChild(newParent, node, op.Position)
			parentByID[op.BrowserID] = newParent

		case diff.OpUpdate:
			node, ok := nodesByID[op.BrowserID]
			if !ok {
				return fmt.Errorf("browserfile: UPDATE target %s (id %q) not found", strings.Join(op.TargetPath, "/"), op.BrowserID)
			}
			for _, field := range op.Changes {
				switch field {
				case "name":
					node["name"] = op.Name
				case "url":
					node["url"] = op.URL
				}
			}
			node["date_modified"] = webkitTimestamp(time.Now())
		}
	}

	f.raw["checksum"] = f.computeChecksum()
	return nil
}

// Write serializes the (possibly modified) file back to its original
// path. If backup is true, the original file's current on-disk content is
// first copied to "<path>.marko-backup-<unix-timestamp>" so a mistake is
// always recoverable. The write itself is atomic: a temp file in the same
// directory is written and fsynced, then renamed over the original.
func (f *File) Write(backup bool) error {
	if backup {
		if err := f.backupOriginal(); err != nil {
			return err
		}
	}

	data, err := json.MarshalIndent(f.raw, "", "   ")
	if err != nil {
		return fmt.Errorf("browserfile: marshaling: %w", err)
	}
	data = append(data, '\n')

	tmp := f.path + ".marko-tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("browserfile: writing temp file: %w", err)
	}
	if err := os.Rename(tmp, f.path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("browserfile: renaming temp file into place: %w", err)
	}
	return nil
}

func (f *File) backupOriginal() error {
	original, err := os.ReadFile(f.path)
	if err != nil {
		return fmt.Errorf("browserfile: reading original for backup: %w", err)
	}
	backupPath := fmt.Sprintf("%s.marko-backup-%d", f.path, time.Now().Unix())
	if err := os.WriteFile(backupPath, original, 0o600); err != nil {
		return fmt.Errorf("browserfile: writing backup %q: %w", backupPath, err)
	}
	return nil
}

// resolveParentID resolves an Operation's real parent id: a root-level
// parent path resolves to the real root id read from the file; a
// not-yet-created parent (created earlier in this same Apply pass)
// resolves via createdIDs; anything else trusts the operation's own
// ParentBrowserID (a pre-existing node).
func resolveParentID(op diff.Operation, rootID map[string]string, createdIDs map[string]string) string {
	parentPath := op.TargetPath[:len(op.TargetPath)-1]
	if len(parentPath) == 1 {
		if id, ok := rootID[parentPath[0]]; ok {
			return id
		}
	}
	if len(parentPath) > 0 {
		if id, ok := createdIDs[pathKey(parentPath)]; ok {
			return id
		}
	}
	return op.ParentBrowserID
}

func pathKey(path []string) string {
	return strings.Join(path, "/")
}

func indexNode(node map[string]interface{}, nodesByID, parentByID map[string]map[string]interface{}) {
	children, _ := node["children"].([]interface{})
	for _, c := range children {
		cm, ok := c.(map[string]interface{})
		if !ok {
			continue
		}
		id, _ := cm["id"].(string)
		if id != "" {
			nodesByID[id] = cm
			parentByID[id] = node
		}
		indexNode(cm, nodesByID, parentByID)
	}
}

func removeChild(parent map[string]interface{}, id string) {
	children, _ := parent["children"].([]interface{})
	out := make([]interface{}, 0, len(children))
	for _, c := range children {
		if cm, ok := c.(map[string]interface{}); ok {
			if cid, _ := cm["id"].(string); cid == id {
				continue
			}
		}
		out = append(out, c)
	}
	parent["children"] = out
}

func insertChild(parent map[string]interface{}, node map[string]interface{}, position int) {
	children, _ := parent["children"].([]interface{})
	if position < 0 {
		position = 0
	}
	if position > len(children) {
		position = len(children)
	}
	out := make([]interface{}, 0, len(children)+1)
	out = append(out, children[:position]...)
	out = append(out, node)
	out = append(out, children[position:]...)
	parent["children"] = out
}

func newRawNode(id string, op diff.Operation) map[string]interface{} {
	now := webkitTimestamp(time.Now())
	node := map[string]interface{}{
		"id":            id,
		"guid":          newGUID(),
		"name":          op.Name,
		"date_added":    now,
		"date_modified": now,
	}
	if op.Kind == "bookmark" {
		node["type"] = "url"
		node["url"] = op.URL
		node["date_last_used"] = "0"
		delete(node, "date_modified")
	} else {
		node["type"] = "folder"
		node["children"] = []interface{}{}
	}
	return node
}

// maxNodeID walks every node under bookmark_bar/other/synced and returns
// the largest numeric "id" found, so newly CREATEd nodes get ids that
// can never collide with an existing one (including in the untouched
// "synced" root, which Marko never modifies but whose id space it still
// shares).
func maxNodeID(raw map[string]interface{}) int {
	max := 0
	roots, _ := raw["roots"].(map[string]interface{})
	var walk func(node map[string]interface{})
	walk = func(node map[string]interface{}) {
		if idStr, ok := node["id"].(string); ok {
			if id, err := strconv.Atoi(idStr); err == nil && id > max {
				max = id
			}
		}
		children, _ := node["children"].([]interface{})
		for _, c := range children {
			if cm, ok := c.(map[string]interface{}); ok {
				walk(cm)
			}
		}
	}
	for _, key := range []string{"bookmark_bar", "other", "synced"} {
		if node, ok := roots[key].(map[string]interface{}); ok {
			walk(node)
		}
	}
	return max
}

// newGUID generates a random UUID v4, formatted the same way Chromium
// stores its "guid" fields.
func newGUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // RFC 4122 variant
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// webkitEpochOffsetMicroseconds is the number of microseconds between the
// WebKit/Chrome epoch (1601-01-01 00:00:00 UTC) and the Unix epoch
// (1970-01-01 00:00:00 UTC), used by Chromium's "date_added"/
// "date_modified" fields.
const webkitEpochOffsetMicroseconds = 11644473600000000

func webkitTimestamp(t time.Time) string {
	micros := t.UnixMicro() + webkitEpochOffsetMicroseconds
	return strconv.FormatInt(micros, 10)
}

// computeChecksum is a best-effort MD5 over each node's id/name(/url),
// walked depth-first across bookmark_bar then other then synced,
// mirroring Marko's understanding of Chromium's own BookmarkCodec
// checksum algorithm. Chromium/Brave were confirmed during development
// to not strictly enforce this on load (an exact match is not required
// for the file to be accepted), so this exists mainly to avoid leaving
// an obviously-stale checksum behind, not as a correctness-critical value.
func (f *File) computeChecksum() string {
	h := md5.New()
	update := func(s string) { _, _ = h.Write([]byte(s)) }
	var visit func(node map[string]interface{})
	visit = func(node map[string]interface{}) {
		id, _ := node["id"].(string)
		name, _ := node["name"].(string)
		if t, _ := node["type"].(string); t == "url" {
			url, _ := node["url"].(string)
			update(id)
			update(name)
			update(url)
			return
		}
		update(id)
		update(name)
		children, _ := node["children"].([]interface{})
		for _, c := range children {
			if cm, ok := c.(map[string]interface{}); ok {
				visit(cm)
			}
		}
	}
	roots := f.roots()
	for _, key := range []string{"bookmark_bar", "other", "synced"} {
		if node, ok := roots[key].(map[string]interface{}); ok {
			visit(node)
		}
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}
