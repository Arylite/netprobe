package webapi

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// ParseOrigins checks the allowed web origins: each is scheme://host[:port],
// with no path and no wildcard, so that what a browser may do is always named.
func ParseOrigins(origins []string) ([]string, error) {
	out := make([]string, 0, len(origins))
	for _, o := range origins {
		o = strings.TrimSpace(o)
		u, err := url.Parse(o)
		if o == "*" || err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.Path != "" || u.RawQuery != "" || u.User != nil {
			return nil, fmt.Errorf("origin %q: want scheme://host[:port], without a path and without a wildcard", o)
		}
		out = append(out, u.Scheme+"://"+u.Host)
	}
	return out, nil
}

// cors lets the allowed origins call the API from a browser. Requests carry a
// bearer token, not a cookie, so no credentials mode is needed.
func (s *Server) cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Vary", "Origin")
		origin := r.Header.Get("Origin")
		if origin != "" && s.origins[origin] {
			w.Header().Set("Access-Control-Allow-Origin", origin)
		}
		if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
			if s.origins[origin] {
				h := w.Header()
				h.Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
				h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
				h.Set("Access-Control-Max-Age", "600")
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
