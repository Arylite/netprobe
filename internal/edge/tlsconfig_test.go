package edge

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/Arylite/netprobe/internal/pkitest"
)

func TestTLSConfigRefusesWhatCannotWork(t *testing.T) {
	pki := pkitest.New(t)
	client := pki.Client(t, "edge")
	for name, args := range map[string][3]string{
		"a missing CA file":          {"/nonexistent.pem", "", ""},
		"a CA file that is a key":    {client.KeyFile, "", ""},
		"a certificate without key":  {"", client.CertFile, ""},
		"a key without certificate":  {"", "", client.KeyFile},
		"a certificate that is a CA": {"", pki.CAFile, client.KeyFile},
	} {
		if _, err := TLSConfig(args[0], args[1], args[2]); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if cfg, err := TLSConfig("", "", ""); err != nil || cfg.RootCAs != nil || cfg.GetClientCertificate != nil {
		t.Fatalf("nothing asked: %+v, %v", cfg, err)
	}
}

func TestClientTrustsThePrivateCAAndPresentsItsCertificate(t *testing.T) {
	pki := pkitest.New(t)
	server := pki.Server(t, "server")
	client := pki.Client(t, "edge")

	var subject string
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		subject = r.TLS.PeerCertificates[0].Subject.CommonName
		_, _ = w.Write([]byte(`{"checks":[]}`))
	}))
	pair, err := tls.LoadX509KeyPair(server.CertFile, server.KeyFile)
	if err != nil {
		t.Fatal(err)
	}
	caPEM, _ := os.ReadFile(pki.CAFile)
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(caPEM)
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{pair}, ClientCAs: pool, ClientAuth: tls.RequireAndVerifyClientCert}
	srv.StartTLS()
	defer srv.Close()

	// The system roots do not know this CA.
	plain, err := NewClient(srv.URL, "np_token")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := plain.Assignments(context.Background()); err == nil {
		t.Fatal("a certificate of an unknown CA was trusted")
	}

	cfg, err := TLSConfig(pki.CAFile, client.CertFile, client.KeyFile)
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewClient(srv.URL, "np_token", WithTLS(cfg))
	if err != nil {
		t.Fatal(err)
	}
	checks, _, err := c.Assignments(context.Background())
	if err != nil || len(checks) != 0 || subject != "edge" {
		t.Fatalf("with the CA and the certificate: %v, %v, %q", checks, err, subject)
	}

	// Without its certificate the edge is refused by the server.
	noCert, _ := TLSConfig(pki.CAFile, "", "")
	c, _ = NewClient(srv.URL, "np_token", WithTLS(noCert))
	if _, _, err := c.Assignments(context.Background()); err == nil {
		t.Fatal("the central served an edge that showed no certificate")
	}
}

func TestClientCertificateIsReadAtEachHandshake(t *testing.T) {
	pki := pkitest.New(t)
	first := pki.Client(t, "first")
	second := pki.Client(t, "second")
	cfg, err := TLSConfig("", first.CertFile, first.KeyFile)
	if err != nil {
		t.Fatal(err)
	}
	was, _ := cfg.GetClientCertificate(nil)
	for src, dst := range map[string]string{second.CertFile: first.CertFile, second.KeyFile: first.KeyFile} {
		raw, _ := os.ReadFile(src)
		if err := os.WriteFile(dst, raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	now, err := cfg.GetClientCertificate(nil)
	if err != nil || string(now.Certificate[0]) == string(was.Certificate[0]) {
		t.Fatalf("the renewed certificate was not used: %v", err)
	}
}
