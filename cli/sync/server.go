// Package sync implements the local-only HTTP bridge server, wire
// protocol types, and the static export writer. See docs/architecture.md
// §8. sync depends on internal/bookmarktree, diff, and renderer outputs,
// but never the reverse.
package sync

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/inchestnov/marko/cli/diff"
	"github.com/inchestnov/marko/cli/internal/bookmarktree"
	"github.com/inchestnov/marko/cli/internal/version"
)

// RenderFunc runs the full parse -> resolve -> validate -> render
// pipeline and returns the freshly-computed desired BookmarkTree, so
// that marko.yaml edits are picked up live on every call (§8.2's
// requirement for POST /diff). On validation/parse failure, it should
// return a *PipelineError so handlers can surface the right error code.
type RenderFunc func() (*bookmarktree.BookmarkTree, error)

// PipelineError carries a stable error Code (matching validator/template
// error codes) alongside a human message, for surfacing as the
// { "error": { "code", "message" } } shape from §8.2.
type PipelineError struct {
	Code    string
	Message string
}

func (e *PipelineError) Error() string {
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// Server is the local-only HTTP bridge described in docs/architecture.md
// §8. It binds strictly to 127.0.0.1.
type Server struct {
	Render RenderFunc

	httpServer *http.Server
	listener   net.Listener

	mu         sync.Mutex
	reportCh   chan ReportResult
	reportOnce sync.Once
	done       chan reportOutcome
}

type reportOutcome struct {
	okCount    int
	errorCount int
	received   bool
}

// NewServer constructs a Server that uses render to (re-)compute the
// desired tree on every /plan and /diff call.
func NewServer(render RenderFunc) *Server {
	return &Server{
		Render: render,
		done:   make(chan reportOutcome, 1),
	}
}

// Addr returns the address the server is listening on (valid only after
// Listen has succeeded).
func (s *Server) Addr() string {
	if s.listener == nil {
		return ""
	}
	return s.listener.Addr().String()
}

// Listen binds the HTTP listener to 127.0.0.1:port. Per §8.1 this MUST
// use an explicit loopback address, never ":<port>" / "0.0.0.0".
func (s *Server) Listen(port int) error {
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return fmt.Errorf("sync: binding to 127.0.0.1:%d: %w", port, err)
	}
	s.listener = ln

	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/plan", s.handlePlan)
	mux.HandleFunc("/diff", s.handleDiff)
	mux.HandleFunc("/report", s.handleReport)
	mux.HandleFunc("/shutdown", s.handleShutdown)

	s.httpServer = &http.Server{Handler: mux}
	return nil
}

// Serve blocks, accepting connections, until the listener is closed
// (via Shutdown) or an unrecoverable error occurs. Call after Listen.
func (s *Server) Serve() error {
	err := s.httpServer.Serve(s.listener)
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

// Shutdown gracefully stops the server.
func (s *Server) Shutdown() error {
	if s.httpServer == nil {
		return nil
	}
	return s.httpServer.Close()
}

// WaitForReport blocks until a /report call has been received or the
// timeout elapses (timeout <= 0 means wait forever). Returns whether a
// report was actually received, plus the ok/error counts from it.
func (s *Server) WaitForReport(timeout time.Duration) (received bool, okCount, errorCount int) {
	if timeout <= 0 {
		outcome := <-s.done
		return outcome.received, outcome.okCount, outcome.errorCount
	}
	select {
	case outcome := <-s.done:
		return outcome.received, outcome.okCount, outcome.errorCount
	case <-time.After(timeout):
		return false, 0, 0
	}
}

func setCORS(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: ErrorBody{Code: "E_METHOD_NOT_ALLOWED", Message: "method not allowed"}})
		return
	}
	writeJSON(w, http.StatusOK, HealthResponse{Status: "ok", MarkoVersion: version.Version})
}

func (s *Server) handlePlan(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: ErrorBody{Code: "E_METHOD_NOT_ALLOWED", Message: "method not allowed"}})
		return
	}

	tree, err := s.Render()
	if err != nil {
		writePipelineError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, PlanResponse{
		GeneratedAt:  time.Now().UTC().Format(time.RFC3339),
		MarkoVersion: version.Version,
		DesiredTree:  tree,
	})
}

func (s *Server) handleDiff(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: ErrorBody{Code: "E_METHOD_NOT_ALLOWED", Message: "method not allowed"}})
		return
	}

	var req DiffRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: ErrorBody{Code: "E_BAD_REQUEST", Message: "malformed JSON body: " + err.Error()}})
		return
	}
	if req.ActualTree == nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: ErrorBody{Code: "E_BAD_REQUEST", Message: "missing \"actualTree\" field"}})
		return
	}

	desired, err := s.Render()
	if err != nil {
		writePipelineError(w, err)
		return
	}

	plan := diff.Diff(desired, req.ActualTree)
	writeJSON(w, http.StatusOK, DiffResponse{
		GeneratedAt: plan.GeneratedAt,
		Operations:  plan.Operations,
	})
}

func (s *Server) handleReport(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: ErrorBody{Code: "E_METHOD_NOT_ALLOWED", Message: "method not allowed"}})
		return
	}

	var req ReportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: ErrorBody{Code: "E_BAD_REQUEST", Message: "malformed JSON body: " + err.Error()}})
		return
	}

	okCount, errorCount := 0, 0
	for _, res := range req.Results {
		if res.Status == "ok" {
			okCount++
		} else {
			errorCount++
		}
	}

	writeJSON(w, http.StatusOK, ReportResponse{Accepted: true, OKCount: okCount, ErrorCount: errorCount})

	s.reportOnce.Do(func() {
		s.done <- reportOutcome{okCount: okCount, errorCount: errorCount, received: true}
	})
}

func (s *Server) handleShutdown(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: ErrorBody{Code: "E_METHOD_NOT_ALLOWED", Message: "method not allowed"}})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"accepted": true})
	go func() {
		_ = s.Shutdown()
	}()
}

func writePipelineError(w http.ResponseWriter, err error) {
	if perr, ok := err.(*PipelineError); ok {
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: ErrorBody{Code: perr.Code, Message: perr.Message}})
		return
	}
	writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: ErrorBody{Code: "E_UNKNOWN", Message: err.Error()}})
}
