package central

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"sort"
	"strings"
	"time"

	"github.com/Arylite/netprobe/internal/central/store"
	"github.com/Arylite/netprobe/internal/doctor"
)

// silentAfter is how long an active edge may go without reporting before the
// diagnostics call it out.
const silentAfter = 10 * time.Minute

// DoctorConfig is what the central diagnostics need.
type DoctorConfig struct {
	DatabaseURL string
	EdgeListen  string
	APIListen   string
	// Now is the clock; nil means time.Now.
	Now func() time.Time
}

// checkListen judges how a surface is exposed: loopback is private, anything
// else is open to a network over plain HTTP.
func checkListen(addr, what, why string) doctor.Outcome {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return doctor.Failure(fmt.Sprintf("listen address %q: %v", addr, err), "use host:port, for example 127.0.0.1:8080")
	}
	if ip, err := netip.ParseAddr(host); (err == nil && ip.IsLoopback()) || host == "localhost" {
		return doctor.Pass(addr + ": reachable from this machine only, anything elsewhere needs a reverse proxy or another address")
	}
	return doctor.Warning(addr+": "+what+" is open to the network over plain HTTP", "put a TLS reverse proxy in front: "+why)
}

// DoctorSteps checks, in order, what the central needs: the database, the
// TimescaleDB extension, the schema, the edges and checks, whether the edges
// report, and how the edge API is exposed. The returned function releases what
// the steps opened.
func DoctorSteps(cfg DoctorConfig) ([]doctor.Step, func()) {
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	var (
		info   store.Info
		st     *store.Store
		edges  []store.Edge
		active []store.Edge
	)
	current := func() bool { return info.SchemaVersion == info.LatestVersion && info.SchemaVersion > 0 }

	steps := []doctor.Step{
		{Name: "database", Run: func(ctx context.Context) doctor.Outcome {
			if cfg.DatabaseURL == "" {
				return doctor.Failure("no database URL", "set NETPROBE_DATABASE_URL or --database-url")
			}
			var err error
			if info, err = store.Inspect(ctx, cfg.DatabaseURL); err != nil {
				return doctor.Failure(err.Error(), "check the URL, that PostgreSQL listens there, and that this user may connect to this database")
			}
			return doctor.Pass("PostgreSQL " + info.ServerVersion + " reachable")
		}},
		{Name: "timescaledb", Run: func(context.Context) doctor.Outcome {
			switch {
			case info.TimescaleVersion != "":
				return doctor.Pass("TimescaleDB " + info.TimescaleVersion)
			case !info.TimescaleAvailable:
				return doctor.Failure("this PostgreSQL server does not have the TimescaleDB extension", "use a TimescaleDB image, or install the TimescaleDB package for this server")
			case info.SchemaVersion == 0:
				return doctor.Warning("TimescaleDB is available but not created in this database yet", "the first start of 'serve' creates it: the database user must be allowed to create extensions")
			}
			return doctor.Failure("the schema exists but TimescaleDB is not created in this database", "run CREATE EXTENSION timescaledb as a superuser, in this database")
		}},
		{Name: "schema", Run: func(context.Context) doctor.Outcome {
			switch {
			case info.SchemaVersion == 0:
				return doctor.Warning("the schema is empty", "the first start of 'serve' creates it")
			case info.SchemaVersion < info.LatestVersion:
				return doctor.Warning(fmt.Sprintf("the schema is at version %d, this binary expects %d", info.SchemaVersion, info.LatestVersion), "the next start of 'serve' migrates it")
			case info.SchemaVersion > info.LatestVersion:
				return doctor.Warning(fmt.Sprintf("the schema is at version %d, newer than the %d this binary knows", info.SchemaVersion, info.LatestVersion), "this binary is older than the one that migrated the database: upgrade it")
			}
			return doctor.Pass(fmt.Sprintf("version %d, up to date", info.SchemaVersion))
		}},
		{Name: "edges", Run: func(ctx context.Context) doctor.Outcome {
			if !current() {
				return doctor.Skipped("the schema is not up to date")
			}
			var err error
			if st, err = store.Open(ctx, cfg.DatabaseURL); err != nil {
				return doctor.Failure(err.Error(), "")
			}
			if edges, err = st.ListEdges(ctx); err != nil {
				return doctor.Failure(err.Error(), "")
			}
			for _, e := range edges {
				if e.Active() {
					active = append(active, e)
				}
			}
			if len(active) == 0 {
				return doctor.Warning("no active edge", "register one with 'netprobe-central edge add --name NAME'")
			}
			return doctor.Pass(fmt.Sprintf("%d active, %d revoked", len(active), len(edges)-len(active)))
		}},
		{Name: "checks", Run: func(ctx context.Context) doctor.Outcome {
			if st == nil {
				return doctor.Skipped("the schema is not up to date")
			}
			checks, err := st.ListChecks(ctx)
			if err != nil {
				return doctor.Failure(err.Error(), "")
			}
			if len(checks) == 0 {
				return doctor.Warning("no check is assigned: edges have nothing to run", "add one with 'netprobe-central check add'")
			}
			return doctor.Pass(fmt.Sprintf("%d checks assigned", len(checks)))
		}},
		{Name: "activity", Run: func(ctx context.Context) doctor.Outcome {
			if st == nil || len(active) == 0 {
				return doctor.Skipped("no active edge to look at")
			}
			last, err := st.LastResultPerEdge(ctx)
			if err != nil {
				return doctor.Failure(err.Error(), "")
			}
			var silent []string
			for _, e := range active {
				at, ok := last[e.ID]
				switch {
				case !ok:
					silent = append(silent, e.Name+" (never reported)")
				case now().Sub(at) > silentAfter:
					silent = append(silent, fmt.Sprintf("%s (last report %s ago)", e.Name, now().Sub(at).Round(time.Minute)))
				}
			}
			if len(silent) > 0 {
				sort.Strings(silent)
				return doctor.Warning("silent: "+strings.Join(silent, ", "), "run 'netprobe-edge doctor' on those machines")
			}
			return doctor.Pass(fmt.Sprintf("all %d active edges reported in the last %s", len(active), silentAfter))
		}},
		{Name: "alerting", Run: func(ctx context.Context) doctor.Outcome {
			if st == nil {
				return doctor.Skipped("the schema is not up to date")
			}
			channels, err := st.ListChannels(ctx)
			if err != nil {
				return doctor.Failure(err.Error(), "")
			}
			open, err := st.OpenIncidents(ctx)
			if err != nil {
				return doctor.Failure(err.Error(), "")
			}
			var going []string
			for _, i := range open {
				if i.Kind == store.IncidentCheck {
					going = append(going, i.CheckID+" on "+i.EdgeName)
				} else {
					going = append(going, i.EdgeName+" stopped reporting")
				}
			}
			switch {
			case len(going) > 0:
				return doctor.Warning(fmt.Sprintf("%d incidents open: %s", len(going), strings.Join(going, ", ")), "'netprobe-central incident list --open' says what each one is")
			case len(channels) == 0:
				return doctor.Warning("no notification channel: incidents are recorded but nobody is told", "add a webhook with 'netprobe-central channel add --name NAME --url URL'")
			}
			return doctor.Pass(fmt.Sprintf("%d channels, no incident open", len(channels)))
		}},
		{Name: "retention", Run: func(ctx context.Context) doctor.Outcome {
			if st == nil {
				return doctor.Skipped("the schema is not up to date")
			}
			kept, err := st.Retention(ctx)
			if err != nil {
				return doctor.Failure(err.Error(), "")
			}
			if kept == 0 {
				return doctor.Warning("results are kept for ever, the database grows without bound", "start 'serve' with --retention, for example --retention 2160h for 90 days")
			}
			return doctor.Pass("results are dropped after " + kept.String())
		}},
		{Name: "edge-listen", Run: func(context.Context) doctor.Outcome {
			return checkListen(cfg.EdgeListen, "the edge API", "edges refuse to send their token over plain HTTP to a remote host")
		}},
		{Name: "api-listen", Run: func(context.Context) doctor.Outcome {
			return checkListen(cfg.APIListen, "the UI API", "browsers and the UI send passwords and tokens to it")
		}},
	}
	return steps, func() {
		if st != nil {
			st.Close()
		}
	}
}
