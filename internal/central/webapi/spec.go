package webapi

import (
	_ "embed"
	"net/http"
)

//go:embed openapi.yaml
var spec []byte

func (s *Server) specRoutes() []route {
	return []route{{http.MethodGet, "/api/v1/openapi.yaml", "", s.serveSpec}}
}

// serveSpec publishes the contract, so a UI can generate its client from the
// central it talks to.
func (s *Server) serveSpec(w http.ResponseWriter, _ *http.Request, _ caller) {
	w.Header().Set("Content-Type", "application/yaml")
	_, _ = w.Write(spec)
}
