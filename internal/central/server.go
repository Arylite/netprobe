package central

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"

	"github.com/Arylite/netprobe/internal/api"
)

const (
	maxBodyBytes     = 1 << 20
	maxStoredResults = 10_000
)

// Server is the edge API with its state held in memory.
type Server struct {
	log         *slog.Logger
	assignments []byte
	etag        string

	mu      sync.Mutex
	results []api.Result
}

// New builds a server that assigns the same checks to every edge.
func New(log *slog.Logger, checks []api.Check) (*Server, error) {
	seen := make(map[string]bool, len(checks))
	for _, c := range checks {
		if err := c.Validate(); err != nil {
			return nil, err
		}
		if seen[c.ID] {
			return nil, fmt.Errorf("check %s is listed twice", c.ID)
		}
		seen[c.ID] = true
	}
	if checks == nil {
		checks = []api.Check{}
	}
	body, err := json.Marshal(api.Assignments{Checks: checks})
	if err != nil {
		return nil, fmt.Errorf("encode assignments: %w", err)
	}
	sum := sha256.Sum256(body)
	return &Server{log: log, assignments: body, etag: `"` + hex.EncodeToString(sum[:8]) + `"`}, nil
}

// Handler serves the edge API.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+api.PathHealth, s.health)
	mux.HandleFunc("GET "+api.PathAssignments, s.getAssignments)
	mux.HandleFunc("POST "+api.PathResults, s.postResults)
	return mux
}

// Results returns a copy of the stored results, oldest first.
func (s *Server) Results() []api.Result {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]api.Result(nil), s.results...)
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) getAssignments(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("ETag", s.etag)
	if r.Header.Get("If-None-Match") == s.etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(s.assignments)
}

func (s *Server) postResults(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	var req api.ResultsRequest
	if err := dec.Decode(&req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	if err := req.Validate(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	s.results = append(s.results, req.Results...)
	if over := len(s.results) - maxStoredResults; over > 0 {
		s.results = s.results[over:]
	}
	s.mu.Unlock()

	s.log.Debug("results received", "count", len(req.Results))
	w.WriteHeader(http.StatusNoContent)
}
