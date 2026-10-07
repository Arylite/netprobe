package webapi

import (
	"crypto/rand"
	"crypto/subtle"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/Arylite/netprobe/internal/central/auth"
	"github.com/Arylite/netprobe/internal/central/store"
)

const (
	// A code of six digits is easy to read out and to type, and it is safe only
	// because guesses are scarce: ten wrong ones, from anyone, close the setup
	// for a while.
	maxSetupFailures = 10
	setupWindow      = 15 * time.Minute
)

// newSetupCode returns six random digits.
func newSetupCode() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}

func normalizeCode(code string) string {
	return strings.NewReplacer("-", "", " ", "").Replace(strings.TrimSpace(code))
}

// SetupCode is what the first administrator must give to be created from the
// UI. Whoever reaches a new central first could otherwise take it over, so the
// code is only in the log of the central, and changes at each start.
func (s *Server) SetupCode() string { return s.setupCode }

type setupStatus struct {
	Required bool `json:"required"`
}

type setupRequest struct {
	Code     string `json:"code"`
	Username string `json:"username"`
	Password string `json:"password"`
}

func (s *Server) setupRoutes() []route {
	return []route{
		{http.MethodGet, "/api/v1/setup", "", s.setupStatus},
		{http.MethodPost, "/api/v1/setup", "", s.setup},
	}
}

// setupStatus says whether the central still waits for its first account.
func (s *Server) setupStatus(w http.ResponseWriter, r *http.Request, _ caller) {
	has, err := s.store.HasUsers(r.Context())
	if err != nil {
		s.unavailable(w, "setup status", err)
		return
	}
	writeJSON(w, http.StatusOK, setupStatus{Required: !has})
}

// setup creates the first administrator, and signs them in. It works once.
func (s *Server) setup(w http.ResponseWriter, r *http.Request, _ caller) {
	var req setupRequest
	if !decode(w, r, &req) {
		return
	}
	ip := s.proxies.ClientIP(r)
	if s.byIP.Blocked(ip) || s.bySetup.Blocked("setup") {
		w.Header().Set("Retry-After", "900")
		writeError(w, http.StatusTooManyRequests, "too many failed attempts, try again later")
		return
	}
	has, err := s.store.HasUsers(r.Context())
	if err != nil {
		s.unavailable(w, "setup", err)
		return
	}
	if has {
		writeError(w, http.StatusConflict, "the setup is already done")
		return
	}
	if subtle.ConstantTimeCompare([]byte(normalizeCode(req.Code)), []byte(normalizeCode(s.setupCode))) != 1 {
		s.byIP.Fail(ip)
		s.bySetup.Fail("setup")
		s.log.Warn("setup refused: wrong setup code", "client_ip", ip)
		writeError(w, http.StatusForbidden, "the setup code is wrong")
		return
	}
	if err := store.ValidateUsername(req.Username); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := auth.ValidatePassword(req.Username, req.Password); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		s.unavailable(w, "hash password", err)
		return
	}
	switch err := s.store.AddFirstAdmin(r.Context(), req.Username, hash); {
	case errors.Is(err, store.ErrSetupDone):
		writeError(w, http.StatusConflict, "the setup is already done")
		return
	case err != nil:
		s.unavailable(w, "setup", err)
		return
	}
	token, expires, err := s.store.CreateSession(r.Context(), req.Username, s.ttl)
	if err != nil {
		s.unavailable(w, "create session", err)
		return
	}
	s.log.Info("first administrator created", "user", req.Username, "client_ip", ip)
	s.audit(r, req.Username, "setup", "")
	writeJSON(w, http.StatusCreated, loginResponse{Token: token, ExpiresAt: expires.Truncate(time.Second), User: userJSON{Username: req.Username, Role: store.RoleAdmin}})
}
