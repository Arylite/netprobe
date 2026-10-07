// Package pkitest makes a throwaway certificate authority for tests.
package pkitest

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Pair is a certificate and its key, as files.
type Pair struct{ CertFile, KeyFile string }

// PKI is a CA that signs the certificates of a test.
type PKI struct {
	CAFile string
	dir    string
	caCert *x509.Certificate
	caKey  *ecdsa.PrivateKey
}

// New creates a CA in the temporary directory of the test.
func New(t *testing.T) *PKI {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "netprobe test CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	p := &PKI{dir: t.TempDir(), caCert: cert, caKey: key}
	p.CAFile = filepath.Join(p.dir, "ca.pem")
	write(t, p.CAFile, "CERTIFICATE", der)
	return p
}

// Server issues a certificate for 127.0.0.1 and localhost, valid for a day.
func (p *PKI) Server(t *testing.T, name string) Pair {
	t.Helper()
	return p.ServerFor(t, name, 24*time.Hour)
}

// ServerFor is Server with the time the certificate stays valid.
func (p *PKI) ServerFor(t *testing.T, name string, valid time.Duration) Pair {
	t.Helper()
	return p.issue(t, name, x509.ExtKeyUsageServerAuth, []string{"localhost"}, []net.IP{net.ParseIP("127.0.0.1"), net.IPv6loopback}, valid)
}

// Client issues a client certificate.
func (p *PKI) Client(t *testing.T, name string) Pair {
	t.Helper()
	return p.issue(t, name, x509.ExtKeyUsageClientAuth, nil, nil, 24*time.Hour)
}

func (p *PKI) issue(t *testing.T, name string, usage x509.ExtKeyUsage, dns []string, ips []net.IP, valid time.Duration) Pair {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	serial, _ := rand.Int(rand.Reader, big.NewInt(1<<62))
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: name},
		DNSNames:     dns,
		IPAddresses:  ips,
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(valid),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{usage},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, p.caCert, &key.PublicKey, p.caKey)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	pair := Pair{CertFile: filepath.Join(p.dir, name+".pem"), KeyFile: filepath.Join(p.dir, name+".key")}
	write(t, pair.CertFile, "CERTIFICATE", der)
	write(t, pair.KeyFile, "EC PRIVATE KEY", keyDER)
	return pair
}

func write(t *testing.T, path, kind string, der []byte) {
	t.Helper()
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: kind, Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
}
