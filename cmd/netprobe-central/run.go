package main

import (
	"context"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Arylite/netprobe/internal/central"
	"github.com/Arylite/netprobe/internal/cli"
	"github.com/Arylite/netprobe/internal/version"
)

func run(listen, databaseURL, level string) error {
	log, err := cli.NewLogger(level)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	st, err := openStore(ctx, databaseURL)
	if err != nil {
		return err
	}
	defer st.Close()

	edges, err := st.ListEdges(ctx)
	if err != nil {
		return err
	}
	if !anyActive(edges) {
		log.Warn("no edge is registered, every request will be refused: run 'netprobe-central edge add --name NAME'")
	}

	httpSrv := &http.Server{
		Addr:              listen,
		Handler:           central.New(log, st).Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", listen)
	if err != nil {
		return err
	}

	log.Info("edge API listening", "addr", ln.Addr().String(), "version", version.Version)
	if !isLoopback(listen) {
		log.Warn("edge API is open to the network over plain HTTP: put a TLS proxy in front, tokens travel in clear otherwise", "addr", listen)
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
