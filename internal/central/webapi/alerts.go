package webapi

import (
	"net/http"
	"strconv"
	"time"

	"github.com/Arylite/netprobe/internal/central/alert"
	"github.com/Arylite/netprobe/internal/central/store"
	"github.com/Arylite/netprobe/internal/logsafe"
)

// statusWindow is the period the dashboard summarises.
const statusWindow = 24 * time.Hour

type statusEdgeJSON struct {
	EdgeID       string    `json:"edge_id"`
	Edge         string    `json:"edge"`
	LastAt       time.Time `json:"last_at"`
	OK           bool      `json:"ok"`
	RTTMillis    float64   `json:"rtt_millis"`
	Error        string    `json:"error,omitempty"`
	Samples      int       `json:"samples"`
	SuccessRatio float64   `json:"success_ratio"`
	P95RTTMillis *float64  `json:"p95_rtt_millis,omitempty"`
}

type statusCheckJSON struct {
	ID              string           `json:"id"`
	Kind            string           `json:"kind"`
	Target          string           `json:"target"`
	Expect          string           `json:"expect,omitempty"`
	IntervalSeconds int              `json:"interval_seconds"`
	Edges           []statusEdgeJSON `json:"edges"`
}

type channelJSON struct {
	Name      string    `json:"name"`
	URL       string    `json:"url"`
	HasSecret bool      `json:"has_secret"`
	CreatedAt time.Time `json:"created_at"`
}

// A secret is write-only: nothing the API answers gives it back.
func toChannelJSON(c store.Channel) channelJSON {
	return channelJSON{Name: c.Name, URL: c.URL, HasSecret: c.Secret != "", CreatedAt: c.CreatedAt}
}

type createChannelRequest struct {
	Name   string `json:"name"`
	URL    string `json:"url"`
	Secret string `json:"secret"`
}

func (s *Server) alertRoutes() []route {
	return []route{
		{http.MethodGet, "/api/v1/status", store.RoleViewer, s.status},
		{http.MethodGet, "/api/v1/incidents", store.RoleViewer, s.listIncidents},
		// The address of a channel may hold a secret of its own: administrators only.
		{http.MethodGet, "/api/v1/channels", store.RoleAdmin, s.listChannels},
		{http.MethodPost, "/api/v1/channels", store.RoleAdmin, s.createChannel},
		{http.MethodDelete, "/api/v1/channels/{name}", store.RoleAdmin, s.removeChannel},
		{http.MethodPost, "/api/v1/channels/{name}/test", store.RoleAdmin, s.testChannel},
	}
}

// status answers, for every check, how it did on each edge over the last day
// and its latest result: what a dashboard shows.
func (s *Server) status(w http.ResponseWriter, r *http.Request, _ caller) {
	ctx := r.Context()
	checks, err := s.store.ListChecks(ctx)
	if err != nil {
		s.unavailable(w, "list checks", err)
		return
	}
	edges, err := s.store.ListEdges(ctx)
	if err != nil {
		s.unavailable(w, "list edges", err)
		return
	}
	summaries, err := s.store.Summaries(ctx, time.Now().Add(-statusWindow))
	if err != nil {
		s.unavailable(w, "summaries", err)
		return
	}
	names := map[string]string{}
	for _, e := range edges {
		if e.Active() {
			names[e.ID] = e.Name
		}
	}
	byCheck := map[string][]statusEdgeJSON{}
	for _, m := range summaries {
		name, ok := names[m.EdgeID]
		if !ok {
			continue
		}
		byCheck[m.CheckID] = append(byCheck[m.CheckID], statusEdgeJSON{
			EdgeID: m.EdgeID, Edge: name, LastAt: m.LastAt, OK: m.LastOK, RTTMillis: m.LastRTT, Error: m.LastError,
			Samples: m.Samples, SuccessRatio: float64(m.Succeeded) / float64(m.Samples), P95RTTMillis: m.P95RTT,
		})
	}
	out := make([]statusCheckJSON, 0, len(checks))
	for _, c := range checks {
		rows := byCheck[c.ID]
		if rows == nil {
			rows = []statusEdgeJSON{}
		}
		out = append(out, statusCheckJSON{ID: c.ID, Kind: c.Kind, Target: c.Target, Expect: c.Expect, IntervalSeconds: c.IntervalSeconds, Edges: rows})
	}
	writeJSON(w, http.StatusOK, map[string]any{"window_seconds": int(statusWindow.Seconds()), "checks": out})
}

const defaultIncidents = 100

// listIncidents answers the latest incidents, newest first; ?state=open keeps
// the ones that are still going on.
func (s *Server) listIncidents(w http.ResponseWriter, r *http.Request, _ caller) {
	q := r.URL.Query()
	openOnly := false
	switch q.Get("state") {
	case "", "all":
	case "open":
		openOnly = true
	default:
		writeError(w, http.StatusBadRequest, "state must be open or all")
		return
	}
	limit := defaultIncidents
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			writeError(w, http.StatusBadRequest, "limit must be a positive integer")
			return
		}
		limit = n
	}
	incidents, err := s.store.ListIncidents(r.Context(), openOnly, limit)
	if err != nil {
		s.unavailable(w, "list incidents", err)
		return
	}
	out := make([]alert.IncidentJSON, 0, len(incidents))
	for _, i := range incidents {
		out = append(out, alert.ToJSON(i))
	}
	writeJSON(w, http.StatusOK, map[string]any{"incidents": out})
}

func (s *Server) listChannels(w http.ResponseWriter, r *http.Request, _ caller) {
	channels, err := s.store.ListChannels(r.Context())
	if err != nil {
		s.unavailable(w, "list channels", err)
		return
	}
	out := make([]channelJSON, 0, len(channels))
	for _, c := range channels {
		out = append(out, toChannelJSON(c))
	}
	writeJSON(w, http.StatusOK, map[string]any{"channels": out})
}

func (s *Server) createChannel(w http.ResponseWriter, r *http.Request, c caller) {
	var req createChannelRequest
	if !decode(w, r, &req) {
		return
	}
	for _, err := range []error{store.ValidateChannelName(req.Name), store.ValidateChannelURL(req.URL), store.ValidateChannelSecret(req.Secret)} {
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	ch, err := s.store.AddChannel(r.Context(), req.Name, req.URL, req.Secret)
	if err != nil {
		s.storeError(w, "add channel", err)
		return
	}
	// The address is not logged: it may hold a secret.
	s.log.Info("channel added", "actor", logsafe.Line(c.user.Username), "channel", logsafe.Line(ch.Name))
	s.audit(r, c.user.Username, "channel.create", ch.Name)
	writeJSON(w, http.StatusCreated, toChannelJSON(ch))
}

func (s *Server) removeChannel(w http.ResponseWriter, r *http.Request, c caller) {
	name := r.PathValue("name")
	if err := s.store.RemoveChannel(r.Context(), name); err != nil {
		s.storeError(w, "remove channel", err)
		return
	}
	s.log.Info("channel removed", "actor", logsafe.Line(c.user.Username), "channel", logsafe.Line(name))
	s.audit(r, c.user.Username, "channel.remove", name)
	w.WriteHeader(http.StatusNoContent)
}

// testChannel sends a test notification, so that an administrator learns that
// a channel does not work now rather than during an outage. It answers 502 when
// the channel does not accept it.
func (s *Server) testChannel(w http.ResponseWriter, r *http.Request, c caller) {
	name := r.PathValue("name")
	ch, err := s.store.GetChannel(r.Context(), name)
	if err != nil {
		s.storeError(w, "read channel", err)
		return
	}
	if err := alert.SendTest(r.Context(), s.sender, ch); err != nil {
		s.log.Info("channel test failed", "actor", logsafe.Line(c.user.Username), "channel", logsafe.Line(name), "err", logsafe.Line(err.Error()))
		s.audit(r, c.user.Username, "channel.test", name)
		writeError(w, http.StatusBadGateway, "the channel did not accept the test: "+err.Error())
		return
	}
	s.log.Info("channel tested", "actor", logsafe.Line(c.user.Username), "channel", logsafe.Line(name))
	s.audit(r, c.user.Username, "channel.test", name)
	w.WriteHeader(http.StatusNoContent)
}
