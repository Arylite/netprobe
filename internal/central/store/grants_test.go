package store_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Arylite/netprobe/internal/api"
	"github.com/Arylite/netprobe/internal/central/store/storetest"
)

func TestADashboardRoleReadsTheResultsAndNothingThatIsSecret(t *testing.T) {
	s, dbURL := storetest.OpenURL(t)
	ctx := context.Background()

	suffix := make([]byte, 4)
	_, _ = rand.Read(suffix)
	role := "dash_" + hex.EncodeToString(suffix)

	admin, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close(ctx)
	if _, err := admin.Exec(ctx, "CREATE ROLE "+role+" LOGIN PASSWORD 'dash'"); err != nil {
		t.Skipf("the database user may not create roles: %v", err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(ctx, "DROP OWNED BY "+role)
		_, _ = admin.Exec(ctx, "DROP ROLE "+role)
	})
	if _, err := admin.Exec(ctx, "GRANT USAGE ON SCHEMA public TO "+role); err != nil {
		t.Fatal(err)
	}

	edge, _, _ := s.AddEdge(ctx, "paris")
	_ = s.AddCheck(ctx, api.Check{ID: "web", Kind: api.KindTCP, Target: "a:1", IntervalSeconds: 5})
	_ = s.InsertResults(ctx, edge.ID, []api.Result{{CheckID: "web", At: time.Now(), OK: true, RTTMillis: 3}})
	_ = s.AddUser(ctx, "alice", "admin", "$argon2id$x")
	_, _ = s.AddChannel(ctx, "ops", "https://example.com/hook", "s3cret")

	if err := s.GrantReadOnly(ctx, "Not A Role"); err == nil {
		t.Fatal("an invalid role was accepted")
	}
	for range 2 { // it can be repeated
		if err := s.GrantReadOnly(ctx, role); err != nil {
			t.Fatal(err)
		}
	}

	u, _ := url.Parse(dbURL)
	u.User = url.UserPassword(role, "dash")
	reader, err := pgx.Connect(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close(ctx)

	for _, q := range []string{
		"SELECT count(*) FROM results",
		"SELECT count(*) FROM checks",
		"SELECT count(*) FROM incidents",
		"SELECT id, name, created_at, revoked_at FROM edges",
	} {
		if _, err := reader.Exec(ctx, q); err != nil {
			t.Errorf("%s: %v", q, err)
		}
	}
	for _, q := range []string{
		"SELECT token_hash FROM edges",
		"SELECT * FROM edges",
		"SELECT count(*) FROM users",
		"SELECT count(*) FROM sessions",
		"SELECT count(*) FROM channels",
		"SELECT count(*) FROM audit_events",
		"INSERT INTO results (edge_id, check_id, at, ok, rtt_millis) VALUES ('x', 'y', now(), true, 1)",
		"DELETE FROM checks",
	} {
		if _, err := reader.Exec(ctx, q); err == nil {
			t.Errorf("%s was allowed", q)
		}
	}
}
