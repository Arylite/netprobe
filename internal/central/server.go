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

	"github.com/Arylite/netprobe/internal/api"
	"github.com/Arylite/netprobe/internal/central/store"
)

const maxBodyBytes = 1 << 20

// Server is the edge API. Edges authenticate with the token the store issued
// them; checks and results live in the store.
type Server struct {
	log   *slog.Logger
	store *store.Store
}

// New builds the edge API on top of a store.
func New(log *slog.Logger, st *store.Store) *Server {
	return &Server{log: log, store: st}
}

// Handler serves the edge API.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+api.PathHealth, s.health)
	mux.HandleFunc("GET "+api.PathAssignments, s.authenticated(s.getAssignments))
	mux.HandleFunc("POST "+api.PathResults, s.authenticated(s.postResults))
	return mux
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	if err := s.store.Ping(r.Context()); err != nil {
		s.unavailable(w, "health check", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// authenticated runs next for requests that carry the token of an active edge.
func (s *Server) authenticated(next func(http.ResponseWriter, *http.Request, store.Edge)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token, ok := bearerToken(r)
		var edge store.Edge
		if ok {
			var err error
			edge, ok, err = s.store.AuthenticateEdge(r.Context(), token)
			if err != nil {
				s.unavailable(w, "authenticate", err)
				return
			}
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

// unavailable answers 503 for a failure of the database, which is the central's
// problem and must not look like a refused token.
func (s *Server) unavailable(w http.ResponseWriter, op string, err error) {
	s.log.Error("request failed", "op", op, "err", err)
	http.Error(w, "service unavailable", http.StatusServiceUnavailable)
}

func (s *Server) getAssignments(w http.ResponseWriter, r *http.Request, _ store.Edge) {
	checks, err := s.store.ListChecks(r.Context())
	if err != nil {
		s.unavailable(w, "list checks", err)
		return
	}
	body, err := json.Marshal(api.Assignments{Checks: checks})
	if err != nil {
		s.unavailable(w, "encode assignments", fmt.Errorf("encode: %w", err))
		return
	}
	sum := sha256.Sum256(body)
	etag := `"` + hex.EncodeToString(sum[:8]) + `"`
	w.Header().Set("ETag", etag)
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(body)
}

func (s *Server) postResults(w http.ResponseWriter, r *http.Request, edge store.Edge) {
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
	if err := s.store.InsertResults(r.Context(), edge.ID, req.Results); err != nil {
		s.unavailable(w, "insert results", err)
		return
	}
	s.log.Debug("results received", "edge_id", edge.ID, "edge", edge.Name, "count", len(req.Results))
	w.WriteHeader(http.StatusNoContent)
}
