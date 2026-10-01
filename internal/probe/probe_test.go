package probe

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const testTimeout = 2 * time.Second

func local() *Prober { return New(Policy{}, testTimeout) }

func TestTCP(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	o := local().TCP(context.Background(), ln.Addr().String())
	if !o.OK || o.RTT < 0 || o.Err != "" {
		t.Fatalf("open port: %+v", o)
	}

	addr := ln.Addr().String()
	ln.Close()
	if o := local().TCP(context.Background(), addr); o.OK || o.Err == "" {
		t.Fatalf("closed port: %+v", o)
	}
}

func TestTCPRejectsAMalformedTarget(t *testing.T) {
	if o := local().TCP(context.Background(), "example.com"); o.OK || !strings.Contains(o.Err, "host:port") {
		t.Fatalf("outcome %+v", o)
	}
}

func TestHTTPStatuses(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/fail":
			http.Error(w, "down", http.StatusServiceUnavailable)
		case "/redirect":
			http.Redirect(w, r, "/", http.StatusFound)
		case "/loop":
			http.Redirect(w, r, "/loop", http.StatusFound)
		}
	}))
	defer ts.Close()

	tests := []struct {
		path   string
		wantOK bool
		errHas string
	}{
		{"/", true, ""},
		{"/redirect", true, ""},
		{"/loop", true, ""},
		{"/fail", false, "503"},
	}
	for _, tt := range tests {
		o := local().HTTP(context.Background(), ts.URL+tt.path)
		if o.OK != tt.wantOK || !strings.Contains(o.Err, tt.errHas) || o.RTT < 0 {
			t.Errorf("%s: %+v", tt.path, o)
		}
	}
}

func TestHTTPRejectsBadTargets(t *testing.T) {
	for _, target := range []string{"", "example.com", "ftp://example.com", "http://"} {
		if o := local().HTTP(context.Background(), target); o.OK || o.Err == "" {
			t.Errorf("HTTP(%q) = %+v", target, o)
		}
	}
}

func TestHTTPTimesOut(t *testing.T) {
	release := make(chan struct{})
	ts := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { <-release }))
	defer ts.Close()
	defer close(release)

	p := New(Policy{}, 100*time.Millisecond)
	start := time.Now()
	o := p.HTTP(context.Background(), ts.URL)
	if o.OK || time.Since(start) > testTimeout {
		t.Fatalf("outcome %+v after %v", o, time.Since(start))
	}
}

func TestContextCancelStopsAProbe(t *testing.T) {
	release := make(chan struct{})
	ts := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { <-release }))
	defer ts.Close()
	defer close(release)

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)
	start := time.Now()
	if o := local().HTTP(ctx, ts.URL); o.OK || time.Since(start) > testTimeout {
		t.Fatalf("outcome %+v after %v", o, time.Since(start))
	}
}

func TestPolicyBlocksLoopbackProbes(t *testing.T) {
	policy, err := ParsePolicy(DefaultDeny)
	if err != nil {
		t.Fatal(err)
	}
	p := New(policy, testTimeout)

	ts := httptest.NewServer(http.NotFoundHandler())
	defer ts.Close()
	if o := p.HTTP(context.Background(), ts.URL); o.OK || !strings.Contains(o.Err, "denied") {
		t.Fatalf("HTTP to loopback: %+v", o)
	}
	if o := p.TCP(context.Background(), ts.Listener.Addr().String()); o.OK || !strings.Contains(o.Err, "denied") {
		t.Fatalf("TCP to loopback: %+v", o)
	}
}

func TestPolicyAppliesToRedirects(t *testing.T) {
	// The first hop is allowed, the redirect points at a denied range.
	policy, _ := ParsePolicy([]string{"203.0.113.0/24"})
	hop := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://203.0.113.9/", http.StatusFound)
	}))
	defer hop.Close()
	o := New(policy, testTimeout).HTTP(context.Background(), hop.URL)
	if o.OK || !strings.Contains(o.Err, "denied") {
		t.Fatalf("redirect into a denied range: %+v", o)
	}
}

func TestErrorsDoNotCarryTheURL(t *testing.T) {
	o := local().HTTP(context.Background(), "http://user:secret@127.0.0.1:1/path")
	if o.OK || strings.Contains(o.Err, "secret") {
		t.Fatalf("outcome %+v", o)
	}
}
