package sync

import (
	"bytes"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/inchestnov/marko/cli/internal/bookmarktree"
)

func sampleTree() *bookmarktree.BookmarkTree {
	return &bookmarktree.BookmarkTree{
		Roots: []*bookmarktree.Node{
			{Kind: bookmarktree.KindFolder, Name: "bar", Children: []*bookmarktree.Node{
				{Kind: bookmarktree.KindBookmark, Name: "Gmail", URL: "https://mail.google.com"},
			}},
			{Kind: bookmarktree.KindFolder, Name: "other"},
		},
	}
}

func newTestServer(render RenderFunc) *Server {
	return &Server{Render: render, done: make(chan reportOutcome, 1)}
}

func TestHandleHealth(t *testing.T) {
	s := newTestServer(func() (*bookmarktree.BookmarkTree, error) { return sampleTree(), nil })
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	s.handleHealth(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var resp HealthResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Status != "ok" {
		t.Fatalf("expected status 'ok', got %q", resp.Status)
	}
	if resp.MarkoVersion == "" {
		t.Fatal("expected non-empty markoVersion")
	}
}

func TestHandlePlan_Success(t *testing.T) {
	s := newTestServer(func() (*bookmarktree.BookmarkTree, error) { return sampleTree(), nil })
	req := httptest.NewRequest(http.MethodGet, "/plan", nil)
	rec := httptest.NewRecorder()
	s.handlePlan(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp PlanResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.DesiredTree == nil || len(resp.DesiredTree.Roots) != 2 {
		t.Fatalf("expected desiredTree with 2 roots, got %+v", resp.DesiredTree)
	}
	if resp.MarkoVersion == "" {
		t.Fatal("expected markoVersion set")
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("expected wildcard CORS header on /plan, got %q", got)
	}
}

func TestHandlePlan_ValidationFailureSurfacedAs500(t *testing.T) {
	s := newTestServer(func() (*bookmarktree.BookmarkTree, error) {
		return nil, &PipelineError{Code: "E_MISSING_VARIABLE", Message: "required variable \"x\" not provided"}
	})
	req := httptest.NewRequest(http.MethodGet, "/plan", nil)
	rec := httptest.NewRecorder()
	s.handlePlan(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
	var resp ErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Error.Code != "E_MISSING_VARIABLE" {
		t.Fatalf("expected E_MISSING_VARIABLE, got %q", resp.Error.Code)
	}
}

func TestHandleDiff_Success(t *testing.T) {
	s := newTestServer(func() (*bookmarktree.BookmarkTree, error) { return sampleTree(), nil })

	actual := &bookmarktree.BookmarkTree{
		Roots: []*bookmarktree.Node{
			{Kind: bookmarktree.KindFolder, Name: "bar", BrowserID: "1"},
			{Kind: bookmarktree.KindFolder, Name: "other", BrowserID: "2"},
		},
	}
	body, _ := json.Marshal(DiffRequest{ActualTree: actual})
	req := httptest.NewRequest(http.MethodPost, "/diff", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	s.handleDiff(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp DiffResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Operations) == 0 {
		t.Fatal("expected at least one CREATE operation for the missing Gmail bookmark")
	}
	found := false
	for _, op := range resp.Operations {
		if op.Type == "CREATE" && op.Name == "Gmail" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected CREATE Gmail operation, got %+v", resp.Operations)
	}
}

func TestHandleDiff_MalformedBody(t *testing.T) {
	s := newTestServer(func() (*bookmarktree.BookmarkTree, error) { return sampleTree(), nil })
	req := httptest.NewRequest(http.MethodPost, "/diff", bytes.NewReader([]byte("{not valid json")))
	rec := httptest.NewRecorder()
	s.handleDiff(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for malformed body, got %d", rec.Code)
	}
	var resp ErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Error.Code != "E_BAD_REQUEST" {
		t.Fatalf("expected E_BAD_REQUEST, got %q", resp.Error.Code)
	}
}

func TestHandleDiff_MissingActualTree(t *testing.T) {
	s := newTestServer(func() (*bookmarktree.BookmarkTree, error) { return sampleTree(), nil })
	req := httptest.NewRequest(http.MethodPost, "/diff", bytes.NewReader([]byte("{}")))
	rec := httptest.NewRecorder()
	s.handleDiff(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing actualTree, got %d", rec.Code)
	}
}

func TestHandleReport_Success(t *testing.T) {
	s := newTestServer(func() (*bookmarktree.BookmarkTree, error) { return sampleTree(), nil })

	reqBody := ReportRequest{
		Results: []ReportResult{
			{TargetPath: []string{"bar", "Work", "Kubernetes"}, Type: "CREATE", Status: "ok", BrowserID: "137"},
			{TargetPath: []string{"bar", "Work", "Company Wiki"}, Type: "UPDATE", Status: "error", Error: "chrome.bookmarks.update failed"},
		},
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/report", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	s.handleReport(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var resp ReportResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !resp.Accepted || resp.OKCount != 1 || resp.ErrorCount != 1 {
		t.Fatalf("expected accepted=true okCount=1 errorCount=1, got %+v", resp)
	}

	received, okCount, errorCount := s.WaitForReport(0)
	if !received || okCount != 1 || errorCount != 1 {
		t.Fatalf("expected WaitForReport to surface the same counts, got received=%v ok=%d err=%d", received, okCount, errorCount)
	}
}

func TestHandleReport_MalformedBody(t *testing.T) {
	s := newTestServer(func() (*bookmarktree.BookmarkTree, error) { return sampleTree(), nil })
	req := httptest.NewRequest(http.MethodPost, "/report", bytes.NewReader([]byte("not json")))
	rec := httptest.NewRecorder()
	s.handleReport(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for malformed body, got %d", rec.Code)
	}
}

func TestListen_BindsLoopbackOnly(t *testing.T) {
	s := NewServer(func() (*bookmarktree.BookmarkTree, error) { return sampleTree(), nil })
	if err := s.Listen(0); err != nil {
		t.Fatalf("unexpected error binding to an ephemeral port: %v", err)
	}
	defer s.Shutdown()

	addr := s.Addr()
	if addr == "" {
		t.Fatal("expected non-empty Addr()")
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("splitting host:port: %v", err)
	}
	if host != "127.0.0.1" {
		t.Fatalf("expected loopback bind, got host %q", host)
	}
}
