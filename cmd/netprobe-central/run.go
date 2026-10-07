package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/Arylite/netprobe/internal/central"
	"github.com/Arylite/netprobe/internal/central/alert"
	"github.com/Arylite/netprobe/internal/central/auth"
	"github.com/Arylite/netprobe/internal/central/store"
	"github.com/Arylite/netprobe/internal/central/webapi"
	"github.com/Arylite/netprobe/internal/cli"
	"github.com/Arylite/netprobe/internal/probe"
	"github.com/Arylite/netprobe/internal/version"
)

const purgeEvery = time.Hour

type serveConfig struct {
	edgeListen  string
	apiListen   string
	databaseURL string
	corsOrigins []string
	sessionTTL  time.Duration
	level       string

	trustedProxies []string
	webhookDeny    string
	alertFailures  int
	edgeSilence    time.Duration
	alertInterval  time.Duration
	// retention is how long results are kept; zero keeps them for ever.
	retention time.Duration
}

// surface is one thing the central serves, on its own address.
type surface struct {
	name string
	addr string
	srv  *http.Server
}

func run(cfg serveConfig) error {
	if cfg.edgeListen == "" || cfg.apiListen == "" {
		return errors.New("--edge-listen and --api-listen must not be empty")
	}
	if cfg.edgeListen == cfg.apiListen {
		return fmt.Errorf("--edge-listen and --api-listen are both %s: they are separate surfaces and need separate addresses", cfg.edgeListen)
	}
	log, err := cli.NewLogger(cfg.level)
	if err != nil {
		return err
	}
	if cfg.retention < 0 {
		return errors.New("--retention must not be negative")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	st, err := openStore(ctx, cfg.databaseURL)
	if err != nil {
		return err
	}
	defer st.Close()
	if err := st.SetRetention(ctx, cfg.retention); err != nil {
		return err
	}
	policy, err := probe.ParseDeny(cfg.webhookDeny)
	if err != nil {
		return err
	}
	if cfg.webhookDeny == probe.DenyNone {
		log.Warn("no range is denied to the webhooks: an administrator can make this central connect to any address, cloud metadata services included")
	}
	engine, err := alert.New(log, st, alert.Config{Failures: cfg.alertFailures, Silence: cfg.edgeSilence, Interval: cfg.alertInterval, Policy: &policy})
	if err != nil {
		return err
	}
	warnAboutSetup(ctx, log, st, cfg)

	proxies, err := auth.ParseProxies(cfg.trustedProxies)
	if err != nil {
		return err
	}
	ui, err := webapi.New(log, st, webapi.Config{AllowedOrigins: cfg.corsOrigins, SessionTTL: cfg.sessionTTL, TrustedProxies: proxies, Policy: &policy})
	if err != nil {
		return err
	}
	surfaces := []surface{
		{name: "edge API", addr: cfg.edgeListen, srv: newServer(central.New(log, st, central.WithProxies(proxies)).Handler())},
		{name: "UI API", addr: cfg.apiListen, srv: newServer(ui.Handler())},
	}

	listeners := make([]net.Listener, 0, len(surfaces))
	defer func() {
		for _, ln := range listeners {
			_ = ln.Close()
		}
	}()
	for _, s := range surfaces {
		ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", s.addr)
		if err != nil {
			return fmt.Errorf("%s: %w", s.name, err)
		}
		listeners = append(listeners, ln)
		log.Info("listening", "surface", s.name, "addr", ln.Addr().String(), "version", version.Version)
	}

	errc := make(chan error, len(surfaces))
	for i, s := range surfaces {
		go func() { errc <- s.srv.Serve(listeners[i]) }()
	}
	go purgeSessions(ctx, log, st)
	engineDone := make(chan struct{})
	go func() {
		defer close(engineDone)
		engine.Run(ctx)
	}()
	// Runs before the store closes: the engine must be done with it.
	defer func() {
		stop()
		<-engineDone
	}()

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var errs []error
	for _, s := range surfaces {
		errs = append(errs, s.srv.Shutdown(shutdownCtx))
	}
	return errors.Join(errs...)
}

func newServer(h http.Handler) *http.Server {
	return &http.Server{
		Handler:           h,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
}

// warnAboutSetup says what an operator should know before the first request.
func warnAboutSetup(ctx context.Context, log *slog.Logger, st *store.Store, cfg serveConfig) {
	if edges, err := st.ListEdges(ctx); err == nil && !anyActive(edges) {
		log.Warn("no edge is registered, every edge request will be refused: run 'netprobe-central edge add --name NAME'")
	}
	if users, err := st.ListUsers(ctx); err == nil && len(users) == 0 {
		log.Warn("no user exists, nobody can sign in to the UI: run 'netprobe-central user add --username NAME --role admin'")
	}
	if channels, err := st.ListChannels(ctx); err == nil && len(channels) == 0 {
		log.Warn("no notification channel exists, incidents are recorded but nobody is told: add one from the UI or with 'netprobe-central channel add'")
	}
	if cfg.retention > 0 {
		log.Info("results are dropped after", "retention", cfg.retention.String())
	} else {
		log.Info("results are kept for ever: set --retention to drop the old ones")
	}
	for _, s := range []struct{ name, addr string }{{"edge API", cfg.edgeListen}, {"UI API", cfg.apiListen}} {
		if !isLoopback(s.addr) {
			log.Warn("open to the network over plain HTTP: put a TLS proxy in front, tokens and passwords travel in clear otherwise", "surface", s.name, "addr", s.addr)
		}
	}
	if len(cfg.corsOrigins) > 0 {
		log.Info("browser origins allowed", "origins", strings.Join(cfg.corsOrigins, ","))
	}
}

func purgeSessions(ctx context.Context, log *slog.Logger, st *store.Store) {
	ticker := time.NewTicker(purgeEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if n, err := st.PurgeSessions(ctx); err != nil {
				log.Warn("purging sessions failed", "err", err)
			} else if n > 0 {
				log.Debug("expired sessions purged", "count", n)
			}
		}
	}
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
