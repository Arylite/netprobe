package webapi

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/Arylite/netprobe/internal/central/alert"
	"github.com/Arylite/netprobe/internal/central/auth"
	"github.com/Arylite/netprobe/internal/central/store"
	"github.com/Arylite/netprobe/internal/probe"
)

const (
	maxBodyBytes  = 1 << 20
	defaultTTL    = 12 * time.Hour
	failureWindow = 10 * time.Minute
	maxByIP       = 30
	maxByUserIP   = 5
)

// Config is what the UI API needs besides the store.
type Config struct {
	// AllowedOrigins are the web origins that may call the API from a browser,
	// as scheme://host[:port]. Empty: no browser origin is allowed.
	AllowedOrigins []string
	// SessionTTL is how long a login lasts; zero means 12 hours.
	SessionTTL time.Duration
	// Sender delivers the test notifications of a channel; nil posts to its
	// webhook.
	Sender alert.Sender
	// HSTS tells browsers to use HTTPS only; set it when the API serves TLS.
	HSTS bool
	// Policy is where a test notification may not connect; nil means the
	// default ranges, a pointer to the zero value nowhere.
	Policy *probe.Policy
	// TrustedProxies are the reverse proxies trusted to say who the client is.
	// Without them a failed login is counted against the address of the proxy,
	// and every user would be locked out together.
	TrustedProxies auth.Proxies
}

// Server is the JSON API of the web UI. It authenticates with bearer tokens:
// there are no cookies, so there is nothing for another site to forge.
type Server struct {
	log     *slog.Logger
	store   *store.Store
	origins map[string]bool
	ttl     time.Duration
	sender  alert.Sender
	proxies auth.Proxies
	hsts    bool

	setupCode string

	byIP     *auth.Limiter
	byUserIP *auth.Limiter
	bySetup  *auth.Limiter
}

// New builds the API on top of a store, or says why the settings are wrong.
func New(log *slog.Logger, st *store.Store, cfg Config) (*Server, error) {
	origins, err := ParseOrigins(cfg.AllowedOrigins)
	if err != nil {
		return nil, err
	}
	ttl := cfg.SessionTTL
	if ttl == 0 {
		ttl = defaultTTL
	}
	sender := cfg.Sender
	if sender == nil {
		policy := probe.DefaultPolicy()
		if cfg.Policy != nil {
			policy = *cfg.Policy
		}
		sender = alert.NewWebhook(policy)
	}
	code, err := newSetupCode()
	if err != nil {
		return nil, err
	}
	set := make(map[string]bool, len(origins))
	for _, o := range origins {
		set[o] = true
	}
	return &Server{
		log:       log,
		store:     st,
		origins:   set,
		ttl:       ttl,
		sender:    sender,
		proxies:   cfg.TrustedProxies,
		hsts:      cfg.HSTS,
		setupCode: code,
		byIP:      auth.NewLimiter(maxByIP, failureWindow),
		byUserIP:  auth.NewLimiter(maxByUserIP, failureWindow),
		bySetup:   auth.NewLimiter(maxSetupFailures, setupWindow),
	}, nil
}

// route is one endpoint and the role it needs; an empty role means anyone.
type route struct {
	method  string
	pattern string
	role    string
	handler authed
}

type authed func(w http.ResponseWriter, r *http.Request, c caller)

// caller is who is making an authenticated request.
type caller struct {
	user  store.User
	token string
}

func (s *Server) routes() []route {
	var all []route
	all = append(all, s.setupRoutes()...)
	all = append(all, s.sessionRoutes()...)
	all = append(all, s.readRoutes()...)
	all = append(all, s.adminRoutes()...)
	all = append(all, s.alertRoutes()...)
	all = append(all, s.auditRoutes()...)
	all = append(all, s.specRoutes()...)
	return all
}

// Handler serves the API.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	for _, rt := range s.routes() {
		mux.HandleFunc(rt.method+" "+rt.pattern, s.require(rt.role, rt.handler))
	}
	return s.secure(s.cors(mux))
}

// secure marks every answer as data: not to be sniffed, cached, framed or
// rendered.
func (s *Server) secure(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Cache-Control", "no-store")
		h.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		h.Set("Referrer-Policy", "no-referrer")
		if s.hsts {
			h.Set("Strict-Transport-Security", "max-age=31536000")
		}
		next.ServeHTTP(w, r)
	})
}

// require runs h for a caller whose role is enough, and answers 401 or 403
// otherwise. Administrators may do everything a viewer may.
func (s *Server) require(role string, h authed) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if role == "" {
			h(w, r, caller{})
			return
		}
		token, ok := auth.BearerToken(r)
		var user store.User
		if ok {
			var err error
			if user, ok, err = s.store.AuthenticateSession(r.Context(), token); err != nil {
				s.unavailable(w, "authenticate", err)
				return
			}
		}
		if !ok {
			w.Header().Set("WWW-Authenticate", "Bearer")
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		if role == store.RoleAdmin && user.Role != store.RoleAdmin {
			writeError(w, http.StatusForbidden, "this needs an administrator")
			return
		}
		h(w, r, caller{user: user, token: token})
	}
}

// unavailable answers 503 for a failure of the database, which is the
// central's problem and must not look like a refused credential.
func (s *Server) unavailable(w http.ResponseWriter, op string, err error) {
	s.log.Error("request failed", "op", op, "err", err)
	writeError(w, http.StatusServiceUnavailable, "service unavailable")
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

// decode reads a JSON body that must match v exactly; it answers 400 itself.
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return false
	}
	return true
}
