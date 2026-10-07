package main

import (
	"crypto/tls"
	"crypto/x509"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/Arylite/netprobe/internal/pkitest"
)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

func TestServerTLSRefusesIncompleteSettings(t *testing.T) {
	pki := pkitest.New(t)
	server := pki.Server(t, "server")
	if cfg, err := serverTLS("", "", "", quiet); cfg != nil || err != nil {
		t.Fatalf("no TLS asked: %v, %v", cfg, err)
	}
	for name, args := range map[string][3]string{
		"a certificate without a key": {server.CertFile, "", ""},
		"a key without a certificate": {"", server.KeyFile, ""},
		"a client CA without TLS":     {"", "", pki.CAFile},
		"a missing file":              {"/nonexistent.pem", server.KeyFile, ""},
		"a missing client CA":         {server.CertFile, server.KeyFile, "/nonexistent.pem"},
		"a client CA that is a key":   {server.CertFile, server.KeyFile, server.KeyFile},
	} {
		if cfg, err := serverTLS(args[0], args[1], args[2], quiet); err == nil {
			t.Errorf("%s: accepted (%v)", name, cfg)
		}
	}
}

// serveTLSOn answers 204 over the TLS settings and returns its address.
func serveTLSOn(t *testing.T, cfg *tls.Config) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	srv.Listener = tls.NewListener(ln, cfg)
	srv.Start()
	t.Cleanup(srv.Close)
	return "https://" + ln.Addr().String()
}

func clientFor(t *testing.T, caFile string, mutate func(*tls.Config)) *http.Client {
	t.Helper()
	pem, err := os.ReadFile(caFile)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(pem)
	cfg := &tls.Config{RootCAs: pool}
	if mutate != nil {
		mutate(cfg)
	}
	return &http.Client{Transport: &http.Transport{TLSClientConfig: cfg}, Timeout: 5 * time.Second}
}

func TestServerTLSIsTLS13AndAsksForAClientCertificateWhenToldTo(t *testing.T) {
	pki := pkitest.New(t)
	server := pki.Server(t, "server")
	client := pki.Client(t, "edge")
	cfg, err := serverTLS(server.CertFile, server.KeyFile, pki.CAFile, quiet)
	if err != nil {
		t.Fatal(err)
	}
	url := serveTLSOn(t, cfg)

	withCert := func(c *tls.Config) {
		pair, err := tls.LoadX509KeyPair(client.CertFile, client.KeyFile)
		if err != nil {
			t.Fatal(err)
		}
		c.Certificates = []tls.Certificate{pair}
	}
	res, err := clientFor(t, pki.CAFile, withCert).Get(url)
	if err != nil {
		t.Fatalf("an edge with its certificate: %v", err)
	}
	res.Body.Close()
	if res.TLS.Version != tls.VersionTLS13 {
		t.Fatalf("TLS version %x", res.TLS.Version)
	}

	if _, err := clientFor(t, pki.CAFile, nil).Get(url); err == nil {
		t.Fatal("a client without a certificate was served")
	}
	other := pkitest.New(t).Client(t, "stranger")
	if _, err := clientFor(t, pki.CAFile, func(c *tls.Config) {
		pair, _ := tls.LoadX509KeyPair(other.CertFile, other.KeyFile)
		c.Certificates = []tls.Certificate{pair}
	}).Get(url); err == nil {
		t.Fatal("a certificate of another CA was accepted")
	}
	if _, err := clientFor(t, pki.CAFile, func(c *tls.Config) {
		withCert(c)
		c.MaxVersion = tls.VersionTLS12
	}).Get(url); err == nil {
		t.Fatal("TLS 1.2 was accepted")
	}
}

func TestServerTLSWithoutClientCAServesAnyone(t *testing.T) {
	pki := pkitest.New(t)
	server := pki.Server(t, "server")
	cfg, err := serverTLS(server.CertFile, server.KeyFile, "", quiet)
	if err != nil {
		t.Fatal(err)
	}
	res, err := clientFor(t, pki.CAFile, nil).Get(serveTLSOn(t, cfg))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
}

func TestRenewedCertificateIsServedWithoutARestart(t *testing.T) {
	pki := pkitest.New(t)
	first := pki.Server(t, "first")
	second := pki.Server(t, "second")
	k, err := newKeypair(first.CertFile, first.KeyFile, quiet)
	if err != nil {
		t.Fatal(err)
	}
	was := k.cert.Certificate[0]

	// Renew: the files hold another certificate, newer than the one loaded.
	for src, dst := range map[string]string{second.CertFile: first.CertFile, second.KeyFile: first.KeyFile} {
		raw, _ := os.ReadFile(src)
		if err := os.WriteFile(dst, raw, 0o600); err != nil {
			t.Fatal(err)
		}
		later := time.Now().Add(time.Minute)
		_ = os.Chtimes(dst, later, later)
	}

	// Not looked at yet: the loaded one is served.
	k.checked = time.Now()
	got, _ := k.get(nil)
	if string(got.Certificate[0]) != string(was) {
		t.Fatal("the files were read before it was time to look")
	}
	k.checked = time.Now().Add(-2 * reloadEvery)
	got, _ = k.get(nil)
	if string(got.Certificate[0]) == string(was) {
		t.Fatal("the renewed certificate was not picked up")
	}

	// A renewed file that does not load keeps the certificate that works.
	if err := os.WriteFile(first.CertFile, []byte("not a certificate"), 0o600); err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(2 * time.Minute)
	_ = os.Chtimes(first.CertFile, later, later)
	k.checked = time.Now().Add(-2 * reloadEvery)
	kept, err := k.get(nil)
	if err != nil || string(kept.Certificate[0]) != string(got.Certificate[0]) {
		t.Fatalf("a broken renewal replaced the certificate: %v", err)
	}
}
