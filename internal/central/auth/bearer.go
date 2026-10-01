package auth

import (
	"net"
	"net/http"
	"strings"
)

// BearerToken returns the token of an "Authorization: Bearer" header.
func BearerToken(r *http.Request) (string, bool) {
	scheme, token, found := strings.Cut(r.Header.Get("Authorization"), " ")
	if !found || !strings.EqualFold(scheme, "Bearer") || token == "" {
		return "", false
	}
	return token, true
}

// PeerHost is the address of the connection, not of a client behind a proxy.
func PeerHost(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
