// Package model contains the shared data model structs (Config,
// Collection, Template, ...) parsed from marko.yaml. See
// docs/architecture.md §3. internal/model has zero dependencies on other
// cli/* packages.
package model

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// Config is the fully-parsed, not-yet-resolved representation of
// marko.yaml plus every file discovered under templates/.
type Config struct {
	Version   string              `yaml:"version"`
	Variables map[string]Variable `yaml:"variables,omitempty"`
	Templates map[string]Template `yaml:"templates,omitempty"`

	// Collections holds the collections in YAML declaration order, per
	// §6.1's hard requirement that render order be deterministic and
	// match the merged YAML document's declaration order. It is decoded
	// via a custom UnmarshalYAML that walks the underlying yaml.Node so
	// that Go's usual (unordered) map decoding never loses order.
	Collections *CollectionMap `yaml:"collections"`

	// SourcePath is the absolute path to the loaded marko.yaml.
	// Not serialized; populated by the parser.
	SourcePath string `yaml:"-"`
}

// NewConfig returns a Config with an initialized, empty CollectionMap.
func NewConfig() *Config {
	return &Config{Collections: NewCollectionMap()}
}

// UnmarshalYAML implements custom decoding for Config so that the
// Collections field preserves YAML declaration order (see CollectionMap).
func (c *Config) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("config: expected a mapping node, got kind %d", node.Kind)
	}

	type rawConfig struct {
		Version     string              `yaml:"version"`
		Variables   map[string]Variable `yaml:"variables,omitempty"`
		Templates   map[string]Template `yaml:"templates,omitempty"`
		Collections yaml.Node           `yaml:"collections"`
	}
	var raw rawConfig
	if err := node.Decode(&raw); err != nil {
		return err
	}

	c.Version = raw.Version
	c.Variables = raw.Variables
	c.Templates = raw.Templates

	cm := NewCollectionMap()
	// If "collections" key was absent, raw.Collections.Kind will be zero.
	if raw.Collections.Kind == yaml.MappingNode {
		if err := cm.UnmarshalYAML(&raw.Collections); err != nil {
			return err
		}
	}
	c.Collections = cm
	return nil
}

// CollectionMap is an order-preserving map of collection name -> Collection.
// yaml.v3 decodes mappings into plain Go maps without preserving key
// order; since §6.1 requires collections to render in declaration order,
// this type implements yaml.Unmarshaler and walks the raw yaml.Node's
// Content slice (which alternates key/value nodes in document order).
type CollectionMap struct {
	order []string
	items map[string]Collection
}

// NewCollectionMap returns an empty, initialized CollectionMap.
func NewCollectionMap() *CollectionMap {
	return &CollectionMap{items: make(map[string]Collection)}
}

// UnmarshalYAML implements yaml.Unmarshaler, preserving key order.
func (m *CollectionMap) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("collections: expected a mapping node, got kind %d", node.Kind)
	}
	if m.items == nil {
		m.items = make(map[string]Collection)
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		keyNode := node.Content[i]
		valNode := node.Content[i+1]

		var key string
		if err := keyNode.Decode(&key); err != nil {
			return fmt.Errorf("collections: invalid key at line %d: %w", keyNode.Line, err)
		}

		var col Collection
		if err := valNode.Decode(&col); err != nil {
			return fmt.Errorf("collections.%s: %w", key, err)
		}

		if _, exists := m.items[key]; !exists {
			m.order = append(m.order, key)
		}
		m.items[key] = col
	}
	return nil
}

// MarshalYAML implements yaml.Marshaler, preserving key order.
func (m *CollectionMap) MarshalYAML() (interface{}, error) {
	node := &yaml.Node{Kind: yaml.MappingNode}
	for _, name := range m.order {
		var keyNode, valNode yaml.Node
		if err := keyNode.Encode(name); err != nil {
			return nil, err
		}
		if err := valNode.Encode(m.items[name]); err != nil {
			return nil, err
		}
		node.Content = append(node.Content, &keyNode, &valNode)
	}
	return node, nil
}

// Order returns collection names in declaration order.
func (m *CollectionMap) Order() []string {
	if m == nil {
		return nil
	}
	out := make([]string, len(m.order))
	copy(out, m.order)
	return out
}

// Get returns the Collection for name and whether it was found.
func (m *CollectionMap) Get(name string) (Collection, bool) {
	if m == nil {
		return Collection{}, false
	}
	c, ok := m.items[name]
	return c, ok
}

// Set inserts or replaces the Collection for name, appending to the
// declaration order if the key is new.
func (m *CollectionMap) Set(name string, col Collection) {
	if m.items == nil {
		m.items = make(map[string]Collection)
	}
	if _, exists := m.items[name]; !exists {
		m.order = append(m.order, name)
	}
	m.items[name] = col
}

// Len returns the number of collections.
func (m *CollectionMap) Len() int {
	if m == nil {
		return 0
	}
	return len(m.items)
}

// DuplicateTemplate describes a template name that was declared more
// than once across marko.yaml and templates/*.yaml. Detection happens in
// the parser (merging the maps naively would silently lose one
// definition), but reporting it as a validation finding
// (E_DUPLICATE_TEMPLATE) is the validator's job (see
// docs/architecture.md §5.1); this type lives in model, which both
// parser and validator may import without violating the module boundary
// rules in §2 (validator may depend on model + template, but not parser).
type DuplicateTemplate struct {
	Name  string
	Files []string // source file paths where this template name was declared, in encounter order
}

// Variable declares a named, typed placeholder with an optional default.
// Values are always treated as strings after substitution; Type is
// advisory metadata used only by the validator (e.g. "url" triggers a
// URL-shape check).
type Variable struct {
	Default     string `yaml:"default,omitempty"`
	Description string `yaml:"description,omitempty"`
	Type        string `yaml:"type,omitempty"` // "string" (default) | "url"
	Required    bool   `yaml:"required,omitempty"`
}

// Template is a reusable, named subtree of folders/bookmarks/nested
// templates. A Template is never rendered on its own; it must be
// instantiated via a TemplateRef inside a Collection or another Template.
type Template struct {
	// Extends allows single inheritance: the base template's Bookmarks,
	// Folders and Templates are merged first, then this template's own
	// entries are appended / override by Name (see §4.5).
	Extends string `yaml:"extends,omitempty"`

	// Vars declares the variables this template accepts, with optional
	// per-template default overrides layered on top of global Variables.
	Vars map[string]Variable `yaml:"vars,omitempty"`

	// Folder, if set, wraps this template's contents in a named folder.
	// If omitted, the template's contents are emitted directly into the
	// parent (flattening / "mixin" behavior), which is how composition
	// of e.g. Profile + GitHub into Repository works.
	Folder *Folder `yaml:"folder,omitempty"`

	Bookmarks []Bookmark    `yaml:"bookmarks,omitempty"`
	Templates []TemplateRef `yaml:"templates,omitempty"`
}

// Folder is an explicit folder node. Name supports variable
// interpolation, e.g. "{{ .repo_name }}".
type Folder struct {
	Name string `yaml:"name"`
}

// Bookmark is a leaf node: a single browser bookmark.
// Name and URL both support variable interpolation.
type Bookmark struct {
	Name string `yaml:"name"`
	URL  string `yaml:"url"`
}

// TemplateRef instantiates a named Template at the current position in a
// Collection or inside another Template's Templates list ("nested
// template" / composition). It is also referred to as an "Instance".
type TemplateRef struct {
	// Template is the name key into Config.Templates (or, when nested
	// inside another template body, may also resolve relative to that
	// template's own local template set if declared - see §4).
	Template string `yaml:"template"`

	// As overrides the mount-point folder name for this instantiation.
	// If omitted, the referenced template's own Folder.Name (after
	// interpolation) is used. If the referenced template has no Folder,
	// As has no effect and the template's contents are flattened in place.
	As string `yaml:"as,omitempty"`

	// Vars supplies concrete values (or overrides) for the variables the
	// referenced template declares (and transitively, nested templates).
	Vars map[string]string `yaml:"vars,omitempty"`
}

// Collection is a top-level named bookmark tree, typically mapped 1:1 to
// a top-level Chrome bookmark folder (e.g. "Bookmarks Bar", "Other
// Bookmarks", or a user-named folder under one of those roots).
type Collection struct {
	// Root selects which native Chrome root this collection is mounted
	// under: "bar" (Bookmarks Bar) or "other" (Other Bookmarks).
	// Defaults to "other".
	Root string `yaml:"root,omitempty"`

	// Folder is optional; if set, the collection's contents are nested
	// inside a named folder under Root. If omitted, contents are placed
	// directly under Root.
	Folder *Folder `yaml:"folder,omitempty"`

	Bookmarks []Bookmark    `yaml:"bookmarks,omitempty"`
	Templates []TemplateRef `yaml:"templates,omitempty"`
	Folders   []NamedGroup  `yaml:"folders,omitempty"`
}

// NamedGroup is an inline (non-template) sub-folder allowed directly in a
// Collection or Template body, for one-off structure that doesn't warrant
// a reusable Template.
type NamedGroup struct {
	Folder    Folder        `yaml:"folder"`
	Bookmarks []Bookmark    `yaml:"bookmarks,omitempty"`
	Templates []TemplateRef `yaml:"templates,omitempty"`
	Folders   []NamedGroup  `yaml:"folders,omitempty"`
}
