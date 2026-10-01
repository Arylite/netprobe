package central

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"

	"github.com/Arylite/netprobe/internal/api"
	"github.com/Arylite/netprobe/internal/central/registry"
)

const (
	maxBodyBytes     = 1 << 20
	maxStoredResults = 10_000
)

// StoredResult is a result with the edge that reported it.
type StoredResult struct {
	EdgeID string
	api.Result
}

// Server is the edge API. Edges authenticate with the token the registry
// issued them; results are held in memory.
type Server struct {
	log         *slog.Logger
	reg         *registry.Registry
	assignments []byte
	etag        string

	mu      sync.Mutex
	results []StoredResult
}

// New builds a server that assigns the same checks to every edge.
func New(log *slog.Logger, checks []api.Check, reg *registry.Registry) (*Server, error) {
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
	return &Server{log: log, reg: reg, assignments: body, etag: `"` + hex.EncodeToString(sum[:8]) + `"`}, nil
}

// Handler serves the edge API.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+api.PathHealth, s.health)
	mux.HandleFunc("GET "+api.PathAssignments, s.authenticated(s.getAssignments))
	mux.HandleFunc("POST "+api.PathResults, s.authenticated(s.postResults))
	return mux
}

// authenticated runs next for requests that carry the token of an active edge.
func (s *Server) authenticated(next func(http.ResponseWriter, *http.Request, registry.Edge)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token, ok := bearerToken(r)
		var edge registry.Edge
		if ok {
			edge, ok = s.reg.Authenticate(token)
		}
		if !ok {
			s.log.Debug("unauthorized request", "path", r.URL.Path, "client_ip", connectionHost(r))
			w.Header().Set("WWW-Authenticate", "Bearer")
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r, edge)
	}
}

func bearerToken(r *http.Request) (string, bool) {
	scheme, token, found := strings.Cut(r.Header.Get("Authorization"), " ")
	if !found || !strings.EqualFold(scheme, "Bearer") || token == "" {
		return "", false
	}
	return token, true
}

// connectionHost is the address of the peer, not of a client behind a proxy.
func connectionHost(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// Results returns a copy of the stored results, oldest first.
func (s *Server) Results() []StoredResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]StoredResult(nil), s.results...)
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) getAssignments(w http.ResponseWriter, r *http.Request, _ registry.Edge) {
	w.Header().Set("ETag", s.etag)
	if r.Header.Get("If-None-Match") == s.etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(s.assignments)
}

func (s *Server) postResults(w http.ResponseWriter, r *http.Request, edge registry.Edge) {
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
	for _, res := range req.Results {
		s.results = append(s.results, StoredResult{EdgeID: edge.ID, Result: res})
	}
	if over := len(s.results) - maxStoredResults; over > 0 {
		s.results = s.results[over:]
	}
	s.mu.Unlock()

	s.log.Debug("results received", "edge_id", edge.ID, "edge", edge.Name, "count", len(req.Results))
	w.WriteHeader(http.StatusNoContent)
}
