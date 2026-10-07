package webapi

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/Arylite/netprobe/internal/central/store"
)

const defaultAudit = 100

type auditJSON struct {
	ID       int64     `json:"id"`
	At       time.Time `json:"at"`
	Actor    string    `json:"actor,omitempty"`
	Action   string    `json:"action"`
	Target   string    `json:"target,omitempty"`
	ClientIP string    `json:"client_ip,omitempty"`
}

func (s *Server) auditRoutes() []route {
	return []route{{http.MethodGet, "/api/v1/audit", store.RoleAdmin, s.listAudit}}
}

// audit adds an event to the trail. A failure is logged and does not fail the
// request: what was done is done.
func (s *Server) audit(r *http.Request, actor, action, target string) {
	e := store.AuditEvent{At: time.Now().UTC(), Actor: actor, Action: action, Target: target, ClientIP: s.proxies.ClientIP(r)}
	if err := s.store.RecordAudit(context.WithoutCancel(r.Context()), e); err != nil {
		s.log.Error("audit event not recorded", "action", action, "err", err)
	}
}

func (s *Server) listAudit(w http.ResponseWriter, r *http.Request, _ caller) {
	limit := defaultAudit
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			writeError(w, http.StatusBadRequest, "limit must be a positive integer")
			return
		}
		limit = n
	}
	events, err := s.store.ListAudit(r.Context(), limit)
	if err != nil {
		s.unavailable(w, "list audit", err)
		return
	}
	out := make([]auditJSON, 0, len(events))
	for _, e := range events {
		out = append(out, auditJSON{ID: e.ID, At: e.At, Actor: e.Actor, Action: e.Action, Target: e.Target, ClientIP: e.ClientIP})
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": out})
}
