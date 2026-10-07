package webapi

import (
	"errors"
	"net/http"
	"time"

	"github.com/Arylite/netprobe/internal/central/auth"
	"github.com/Arylite/netprobe/internal/central/store"
	"github.com/Arylite/netprobe/internal/logsafe"
)

const maxUsernameLength = 64

type userJSON struct {
	Username string `json:"username"`
	Role     string `json:"role"`
}

func toUserJSON(u store.User) userJSON { return userJSON{Username: u.Username, Role: u.Role} }

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type loginResponse struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
	User      userJSON  `json:"user"`
}

func (s *Server) sessionRoutes() []route {
	return []route{
		{http.MethodPost, "/api/v1/login", "", s.login},
		{http.MethodPost, "/api/v1/logout", store.RoleViewer, s.logout},
		{http.MethodGet, "/api/v1/me", store.RoleViewer, s.me},
	}
}

// login trades a username and a password for a session token. Every failure
// looks the same and takes the same time, so that the answer does not tell
// which usernames exist.
func (s *Server) login(w http.ResponseWriter, r *http.Request, _ caller) {
	var req loginRequest
	if !decode(w, r, &req) {
		return
	}
	if req.Username == "" || len(req.Username) > maxUsernameLength || len(req.Password) > auth.MaxPasswordLength {
		writeError(w, http.StatusBadRequest, "username or password is missing or too long")
		return
	}

	ip := s.proxies.ClientIP(r)
	pair := req.Username + "|" + ip
	if s.byIP.Blocked(ip) || s.byUserIP.Blocked(pair) {
		w.Header().Set("Retry-After", "600")
		writeError(w, http.StatusTooManyRequests, "too many failed attempts, try again later")
		return
	}

	user, hash, err := s.store.PasswordHash(r.Context(), req.Username)
	switch {
	case errors.Is(err, store.ErrNotFound):
		auth.SpendHashingTime(req.Password)
		s.loginFailed(w, r, ip, pair, "")
		return
	case err != nil:
		s.unavailable(w, "login", err)
		return
	}
	ok, err := auth.VerifyPassword(req.Password, hash)
	if errors.Is(err, auth.ErrBusy) {
		s.unavailable(w, "login", err)
		return
	}
	if err != nil {
		s.log.Error("stored password hash is unusable", "op", "login", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !ok {
		s.loginFailed(w, r, ip, pair, req.Username)
		return
	}

	token, expires, err := s.store.CreateSession(r.Context(), user.Username, s.ttl)
	if err != nil {
		s.unavailable(w, "create session", err)
		return
	}
	// The address keeps its failures: resetting them on a success would let
	// someone with an account of their own guess other passwords for free.
	s.byUserIP.Reset(pair)
	s.log.Info("login", "actor", logsafe.Line(user.Username), "client_ip", ip)
	s.audit(r, user.Username, "login", "")
	writeJSON(w, http.StatusOK, loginResponse{Token: token, ExpiresAt: expires, User: toUserJSON(user)})
}

// loginFailed records the attempt. The username is kept only when it is one that
// exists: a password typed in the wrong field must not end up in the trail.
func (s *Server) loginFailed(w http.ResponseWriter, r *http.Request, ip, pair, existing string) {
	s.byIP.Fail(ip)
	s.byUserIP.Fail(pair)
	s.log.Debug("login failed", "client_ip", ip)
	s.audit(r, "", "login.failed", existing)
	writeError(w, http.StatusUnauthorized, "invalid username or password")
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request, c caller) {
	if err := s.store.DeleteSession(r.Context(), c.token); err != nil {
		s.unavailable(w, "logout", err)
		return
	}
	s.audit(r, c.user.Username, "logout", "")
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) me(w http.ResponseWriter, _ *http.Request, c caller) {
	writeJSON(w, http.StatusOK, toUserJSON(c.user))
}
