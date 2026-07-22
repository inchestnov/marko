// Package sync implements the local-only HTTP bridge server, wire
// protocol types, and the static export writer. See docs/architecture.md
// §8. sync depends on internal/bookmarktree, diff, and renderer outputs,
// but never the reverse.
package sync
