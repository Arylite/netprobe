package main

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"
)

// reloadEvery is how often the certificate files are looked at for a change.
const reloadEvery = 30 * time.Second

// keypair serves a certificate and picks up a renewed one without a restart.
type keypair struct {
	certFile, keyFile string
	log               *slog.Logger

	mu      sync.Mutex
	cert    *tls.Certificate
	stamp   [2]time.Time
	checked time.Time
}

func newKeypair(certFile, keyFile string, log *slog.Logger) (*keypair, error) {
	k := &keypair{certFile: certFile, keyFile: keyFile, log: log}
	if err := k.load(); err != nil {
		return nil, err
	}
	return k, nil
}

func (k *keypair) load() error {
	pair, err := tls.LoadX509KeyPair(k.certFile, k.keyFile)
	if err != nil {
		return fmt.Errorf("load the certificate: %w", err)
	}
	k.cert = &pair
	k.stamp = k.stat()
	return nil
}

func (k *keypair) stat() (stamp [2]time.Time) {
	for i, f := range []string{k.certFile, k.keyFile} {
		if info, err := os.Stat(f); err == nil {
			stamp[i] = info.ModTime()
		}
	}
	return stamp
}

// get is tls.Config.GetCertificate. A renewed file that does not load keeps the
// certificate that works, and says so.
func (k *keypair) get(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if now := time.Now(); now.Sub(k.checked) >= reloadEvery {
		k.checked = now
		if stamp := k.stat(); stamp != k.stamp {
			if err := k.load(); err != nil {
				k.stamp = stamp
				k.log.Error("the renewed certificate was not loaded, the previous one stays", "err", err)
			} else {
				k.log.Info("certificate reloaded", "file", k.certFile)
			}
		}
	}
	return k.cert, nil
}

// serverTLS builds the TLS settings of a surface: TLS 1.3, and when a client CA
// is given, a client certificate signed by it.
func serverTLS(certFile, keyFile, clientCA string, log *slog.Logger) (*tls.Config, error) {
	if certFile == "" && keyFile == "" {
		if clientCA != "" {
			return nil, errors.New("a client CA needs a certificate for the server too")
		}
		return nil, nil
	}
	if certFile == "" || keyFile == "" {
		return nil, errors.New("give both a certificate and its key")
	}
	k, err := newKeypair(certFile, keyFile, log)
	if err != nil {
		return nil, err
	}
	cfg := &tls.Config{
		MinVersion:     tls.VersionTLS13,
		GetCertificate: k.get,
		NextProtos:     []string{"h2", "http/1.1"},
	}
	if clientCA != "" {
		pem, err := os.ReadFile(clientCA)
		if err != nil {
			return nil, fmt.Errorf("read the client CA: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, errors.New("the client CA file holds no certificate")
		}
		cfg.ClientCAs = pool
		cfg.ClientAuth = tls.RequireAndVerifyClientCert
	}
	return cfg, nil
}
