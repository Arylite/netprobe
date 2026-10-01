package webapi

import (
	"net/http"
	"strconv"
	"time"

	"github.com/Arylite/netprobe/internal/api"
	"github.com/Arylite/netprobe/internal/central/store"
)

const defaultResults = 100

type edgeJSON struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	CreatedAt time.Time  `json:"created_at"`
	RevokedAt *time.Time `json:"revoked_at,omitempty"`
	LastSeen  *time.Time `json:"last_seen,omitempty"`
}

type resultJSON struct {
	EdgeID    string    `json:"edge_id"`
	At        time.Time `json:"at"`
	OK        bool      `json:"ok"`
	RTTMillis float64   `json:"rtt_millis"`
	Error     string    `json:"error,omitempty"`
}

func (s *Server) readRoutes() []route {
	return []route{
		{http.MethodGet, "/api/v1/edges", store.RoleViewer, s.listEdges},
		{http.MethodGet, "/api/v1/checks", store.RoleViewer, s.listChecks},
		{http.MethodGet, "/api/v1/checks/{id}/results", store.RoleViewer, s.listResults},
	}
}

// listEdges answers every edge with when it last reported, which is what tells
// an edge that works from one that went quiet.
func (s *Server) listEdges(w http.ResponseWriter, r *http.Request, _ caller) {
	edges, err := s.store.ListEdges(r.Context())
	if err != nil {
		s.unavailable(w, "list edges", err)
		return
	}
	last, err := s.store.LastResultPerEdge(r.Context())
	if err != nil {
		s.unavailable(w, "last results", err)
		return
	}
	out := make([]edgeJSON, 0, len(edges))
	for _, e := range edges {
		j := edgeJSON{ID: e.ID, Name: e.Name, CreatedAt: e.CreatedAt, RevokedAt: e.RevokedAt}
		if at, ok := last[e.ID]; ok {
			j.LastSeen = &at
		}
		out = append(out, j)
	}
	writeJSON(w, http.StatusOK, map[string]any{"edges": out})
}

func (s *Server) listChecks(w http.ResponseWriter, r *http.Request, _ caller) {
	checks, err := s.store.ListChecks(r.Context())
	if err != nil {
		s.unavailable(w, "list checks", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string][]api.Check{"checks": checks})
}

// listResults answers the latest results of a check, newest first. The results
// of a check that was removed are still there.
func (s *Server) listResults(w http.ResponseWriter, r *http.Request, _ caller) {
	limit := defaultResults
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			writeError(w, http.StatusBadRequest, "limit must be a positive integer")
			return
		}
		limit = n
	}
	results, err := s.store.RecentResults(r.Context(), r.PathValue("id"), limit)
	if err != nil {
		s.unavailable(w, "read results", err)
		return
	}
	out := make([]resultJSON, 0, len(results))
	for _, res := range results {
		out = append(out, resultJSON{EdgeID: res.EdgeID, At: res.At, OK: res.OK, RTTMillis: res.RTTMillis, Error: res.Error})
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": out})
}
