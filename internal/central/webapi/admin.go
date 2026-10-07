package webapi

import (
	"errors"
	"net/http"

	"github.com/Arylite/netprobe/internal/api"
	"github.com/Arylite/netprobe/internal/central/auth"
	"github.com/Arylite/netprobe/internal/central/store"
	"github.com/Arylite/netprobe/internal/logsafe"
)

type createEdgeRequest struct {
	Name string `json:"name"`
}

// createEdgeResponse is the only time the token of an edge is shown.
type createEdgeResponse struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Token string `json:"token"`
}

type createUserRequest struct {
	Username string `json:"username"`
	Role     string `json:"role"`
	Password string `json:"password"`
}

type passwordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

func (s *Server) adminRoutes() []route {
	return []route{
		{http.MethodPost, "/api/v1/edges", store.RoleAdmin, s.createEdge},
		{http.MethodDelete, "/api/v1/edges/{name}", store.RoleAdmin, s.revokeEdge},
		{http.MethodPost, "/api/v1/checks", store.RoleAdmin, s.createCheck},
		{http.MethodDelete, "/api/v1/checks/{id}", store.RoleAdmin, s.removeCheck},
		{http.MethodGet, "/api/v1/users", store.RoleAdmin, s.listUsers},
		{http.MethodPost, "/api/v1/users", store.RoleAdmin, s.createUser},
		{http.MethodDelete, "/api/v1/users/{username}", store.RoleAdmin, s.deleteUser},
		{http.MethodPost, "/api/v1/me/password", store.RoleViewer, s.changePassword},
	}
}

// storeError answers what a failed store call means: a missing or duplicate
// row is the caller's doing, anything else is the database's.
func (s *Server) storeError(w http.ResponseWriter, op string, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not found")
	case errors.Is(err, store.ErrExists):
		writeError(w, http.StatusConflict, "already exists")
	case errors.Is(err, store.ErrLastAdmin):
		writeError(w, http.StatusConflict, "this is the last administrator")
	default:
		s.unavailable(w, op, err)
	}
}

func (s *Server) createEdge(w http.ResponseWriter, r *http.Request, c caller) {
	var req createEdgeRequest
	if !decode(w, r, &req) {
		return
	}
	if err := store.ValidateEdgeName(req.Name); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	edge, token, err := s.store.AddEdge(r.Context(), req.Name)
	if err != nil {
		s.storeError(w, "add edge", err)
		return
	}
	s.log.Info("edge added", "actor", logsafe.Line(c.user.Username), "edge_id", edge.ID, "edge", logsafe.Line(edge.Name))
	s.audit(r, c.user.Username, "edge.create", edge.Name)
	writeJSON(w, http.StatusCreated, createEdgeResponse{ID: edge.ID, Name: edge.Name, Token: token})
}

func (s *Server) revokeEdge(w http.ResponseWriter, r *http.Request, c caller) {
	name := r.PathValue("name")
	if err := s.store.RevokeEdge(r.Context(), name); err != nil {
		s.storeError(w, "revoke edge", err)
		return
	}
	s.log.Info("edge revoked", "actor", logsafe.Line(c.user.Username), "edge", logsafe.Line(name))
	s.audit(r, c.user.Username, "edge.revoke", name)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) createCheck(w http.ResponseWriter, r *http.Request, c caller) {
	var check api.Check
	if !decode(w, r, &check) {
		return
	}
	if err := check.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.store.AddCheck(r.Context(), check); err != nil {
		s.storeError(w, "add check", err)
		return
	}
	s.log.Info("check added", "actor", logsafe.Line(c.user.Username), "check_id", logsafe.Line(check.ID))
	s.audit(r, c.user.Username, "check.create", check.ID)
	writeJSON(w, http.StatusCreated, check)
}

func (s *Server) removeCheck(w http.ResponseWriter, r *http.Request, c caller) {
	id := r.PathValue("id")
	if err := s.store.RemoveCheck(r.Context(), id); err != nil {
		s.storeError(w, "remove check", err)
		return
	}
	s.log.Info("check removed", "actor", logsafe.Line(c.user.Username), "check_id", logsafe.Line(id))
	s.audit(r, c.user.Username, "check.remove", id)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listUsers(w http.ResponseWriter, r *http.Request, _ caller) {
	users, err := s.store.ListUsers(r.Context())
	if err != nil {
		s.unavailable(w, "list users", err)
		return
	}
	out := make([]userJSON, 0, len(users))
	for _, u := range users {
		out = append(out, toUserJSON(u))
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": out})
}

func (s *Server) createUser(w http.ResponseWriter, r *http.Request, c caller) {
	var req createUserRequest
	if !decode(w, r, &req) {
		return
	}
	if err := store.ValidateUsername(req.Username); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !store.ValidRole(req.Role) {
		writeError(w, http.StatusBadRequest, "role must be admin or viewer")
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
	if err := s.store.AddUser(r.Context(), req.Username, req.Role, hash); err != nil {
		s.storeError(w, "add user", err)
		return
	}
	s.log.Info("user added", "actor", logsafe.Line(c.user.Username), "user", logsafe.Line(req.Username), "role", logsafe.Line(req.Role))
	s.audit(r, c.user.Username, "user.create", req.Username)
	writeJSON(w, http.StatusCreated, userJSON{Username: req.Username, Role: req.Role})
}

func (s *Server) deleteUser(w http.ResponseWriter, r *http.Request, c caller) {
	username := r.PathValue("username")
	if username == c.user.Username {
		writeError(w, http.StatusConflict, "you cannot delete your own account")
		return
	}
	if err := s.store.DeleteUser(r.Context(), username); err != nil {
		s.storeError(w, "delete user", err)
		return
	}
	s.log.Info("user deleted", "actor", logsafe.Line(c.user.Username), "user", logsafe.Line(username))
	s.audit(r, c.user.Username, "user.delete", username)
	w.WriteHeader(http.StatusNoContent)
}

// changePassword needs the current password, so that a stolen session alone
// cannot lock the owner out. It ends every session of the account.
func (s *Server) changePassword(w http.ResponseWriter, r *http.Request, c caller) {
	var req passwordRequest
	if !decode(w, r, &req) {
		return
	}
	if len(req.CurrentPassword) > auth.MaxPasswordLength {
		writeError(w, http.StatusBadRequest, "the current password is too long")
		return
	}
	ip := s.proxies.ClientIP(r)
	pair := c.user.Username + "|" + ip
	if s.byUserIP.Blocked(pair) {
		w.Header().Set("Retry-After", "600")
		writeError(w, http.StatusTooManyRequests, "too many failed attempts, try again later")
		return
	}
	_, hash, err := s.store.PasswordHash(r.Context(), c.user.Username)
	if err != nil {
		s.storeError(w, "read password", err)
		return
	}
	ok, err := auth.VerifyPassword(req.CurrentPassword, hash)
	if errors.Is(err, auth.ErrBusy) {
		s.unavailable(w, "change password", err)
		return
	}
	if err != nil {
		s.log.Error("stored password hash is unusable", "op", "change password", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !ok {
		s.byUserIP.Fail(pair)
		writeError(w, http.StatusForbidden, "the current password is wrong")
		return
	}
	if err := auth.ValidatePassword(c.user.Username, req.NewPassword); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	newHash, err := auth.HashPassword(req.NewPassword)
	if err != nil {
		s.unavailable(w, "hash password", err)
		return
	}
	if err := s.store.SetPasswordHash(r.Context(), c.user.Username, newHash); err != nil {
		s.storeError(w, "set password", err)
		return
	}
	s.byUserIP.Reset(pair)
	s.log.Info("password changed", "actor", logsafe.Line(c.user.Username))
	s.audit(r, c.user.Username, "password.change", "")
	w.WriteHeader(http.StatusNoContent)
}
