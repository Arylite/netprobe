package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Arylite/netprobe/internal/api"
	"github.com/Arylite/netprobe/internal/central"
	"github.com/Arylite/netprobe/internal/cli"
	"github.com/Arylite/netprobe/internal/version"
)

func run(listen, checksFile, level string) error {
	log, err := cli.NewLogger(level)
	if err != nil {
		return err
	}
	checks, err := loadChecks(checksFile)
	if err != nil {
		return err
	}
	srv, err := central.New(log, checks)
	if err != nil {
		return err
	}

	httpSrv := &http.Server{
		Addr:              listen,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", listen)
	if err != nil {
		return err
	}

	log.Info("edge API listening", "addr", ln.Addr().String(), "checks", len(checks), "version", version.Version)
	if !isLoopback(listen) {
		log.Warn("edge API is open to the network and has no authentication yet", "addr", listen)
	}

	errc := make(chan error, 1)
	go func() { errc <- httpSrv.Serve(ln) }()

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return httpSrv.Shutdown(shutdownCtx)
}

// loadChecks reads the checks to assign; no file means no checks.
func loadChecks(path string) ([]api.Check, error) {
	if path == "" {
		return nil, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read checks file: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var a api.Assignments
	if err := dec.Decode(&a); err != nil {
		return nil, fmt.Errorf("read checks file %s: %w", path, err)
	}
	return a.Checks, nil
}

func isLoopback(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip, err := netip.ParseAddr(host)
	return err == nil && ip.IsLoopback()
}
