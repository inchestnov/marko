package sync

import (
	"github.com/inchestnov/marko/cli/diff"
	"github.com/inchestnov/marko/cli/internal/bookmarktree"
)

// HealthResponse is the body of GET /health (docs/architecture.md §8.2).
type HealthResponse struct {
	Status       string `json:"status"`
	MarkoVersion string `json:"markoVersion"`
}

// PlanResponse is the body of a successful GET /plan.
type PlanResponse struct {
	GeneratedAt  string                     `json:"generatedAt"`
	MarkoVersion string                     `json:"markoVersion"`
	DesiredTree  *bookmarktree.BookmarkTree `json:"desiredTree"`
}

// ErrorBody is the nested "error" object used by error responses.
type ErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// ErrorResponse is the body of a non-2xx JSON error response
// (e.g. 500 on GET /plan validation failure).
type ErrorResponse struct {
	Error ErrorBody `json:"error"`
}

// DiffRequest is the body of POST /diff.
type DiffRequest struct {
	ActualTree *bookmarktree.BookmarkTree `json:"actualTree"`
}

// DiffResponse is the body of a successful POST /diff.
type DiffResponse struct {
	GeneratedAt string           `json:"generatedAt"`
	Operations  []diff.Operation `json:"operations"`
}

// ReportResult is one entry in POST /report's "results" array. Status is
// "ok" or "error" for a real apply, or "planned" when Preview is true on
// the enclosing ReportRequest (the operation was computed but
// deliberately not executed against chrome.bookmarks).
type ReportResult struct {
	TargetPath []string    `json:"targetPath"`
	Type       diff.OpType `json:"type"`
	Status     string      `json:"status"` // "ok" | "error" | "planned"
	BrowserID  string      `json:"browserId,omitempty"`
	Error      string      `json:"error,omitempty"`
}

// ReportRequest is the body of POST /report. Preview is true when this
// report describes a dry run: every Results entry has Status "planned"
// and no chrome.bookmarks mutation was actually performed.
type ReportRequest struct {
	Results []ReportResult `json:"results"`
	Preview bool           `json:"preview,omitempty"`
}

// ReportResponse is the body of a successful POST /report.
type ReportResponse struct {
	Accepted   bool `json:"accepted"`
	OKCount    int  `json:"okCount"`
	ErrorCount int  `json:"errorCount"`
}

// ExportFile is the JSON format written by `marko export` (§8.4).
type ExportFile struct {
	FormatVersion string                     `json:"formatVersion"`
	GeneratedAt   string                     `json:"generatedAt"`
	MarkoVersion  string                     `json:"markoVersion"`
	DesiredTree   *bookmarktree.BookmarkTree `json:"desiredTree"`
}
