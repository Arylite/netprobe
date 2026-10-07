package alert_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Arylite/netprobe/internal/central/alert"
	"github.com/Arylite/netprobe/internal/central/store"
	"github.com/Arylite/netprobe/internal/probe"
)

func notification(event, resolution string) store.Notification {
	resolved := time.Date(2026, 10, 7, 9, 5, 0, 0, time.UTC)
	i := store.Incident{
		ID: 7, Kind: store.IncidentCheck, CheckID: "web", EdgeID: "e1", EdgeName: "paris",
		StartedAt: time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC), Detail: "3 failures in a row, last: timeout",
	}
	if resolution != "" {
		i.ResolvedAt, i.Resolution = &resolved, resolution
	}
	return store.Notification{Incident: i, Event: event, Channel: store.Channel{Name: "ops"}}
}

func TestPayloadTexts(t *testing.T) {
	edge := notification(store.EventOpened, "")
	edge.Incident.Kind, edge.Incident.CheckID, edge.Incident.Detail = store.IncidentEdge, "", "no result for 6m0s"
	edgeBack := notification(store.EventResolved, store.ResolutionRecovered)
	edgeBack.Incident.Kind, edgeBack.Incident.CheckID = store.IncidentEdge, ""

	for _, c := range []struct {
		n    store.Notification
		want string
	}{
		{notification(store.EventOpened, ""), "[netprobe] web on paris is failing: 3 failures in a row, last: timeout"},
		{notification(store.EventResolved, store.ResolutionRecovered), "[netprobe] web on paris is back to normal"},
		{notification(store.EventResolved, store.ResolutionCheckRemoved), "[netprobe] web on paris: incident closed (check removed)"},
		{edge, "[netprobe] paris stopped reporting: no result for 6m0s"},
		{edgeBack, "[netprobe] paris reports again"},
	} {
		if got := alert.NewPayload(c.n).Text; got != c.want {
			t.Errorf("text = %q, want %q", got, c.want)
		}
	}

	p := alert.NewPayload(notification(store.EventResolved, store.ResolutionRecovered))
	raw, _ := json.Marshal(p)
	var back map[string]any
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	inc := back["incident"].(map[string]any)
	if back["event"] != "resolved" || inc["id"] != float64(7) || inc["edge"] != "paris" || inc["edge_id"] != "e1" || inc["check_id"] != "web" || inc["resolved_at"] == nil {
		t.Fatalf("json: %s", raw)
	}
}

func TestWebhookPostsJSONAndSignsIt(t *testing.T) {
	type got struct {
		header http.Header
		body   []byte
	}
	received := make(chan got, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		received <- got{r.Header.Clone(), body}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	p := alert.NewPayload(notification(store.EventOpened, ""))
	ch := store.Channel{Name: "ops", URL: srv.URL + "/hook", Secret: "s3cret"}
	if err := alert.NewWebhook(probe.Policy{}).Send(context.Background(), ch, p); err != nil {
		t.Fatal(err)
	}
	r := <-received
	if r.header.Get("Content-Type") != "application/json" || r.header.Get("X-Netprobe-Event") != "opened" || !strings.HasPrefix(r.header.Get("User-Agent"), "netprobe/") {
		t.Fatalf("headers: %v", r.header)
	}
	var decoded alert.Payload
	if err := json.Unmarshal(r.body, &decoded); err != nil || decoded.Event != "opened" || decoded.Incident == nil || decoded.Incident.CheckID != "web" {
		t.Fatalf("body: %s (%v)", r.body, err)
	}

	// What a receiver does: recompute the signature from the secret.
	ts := r.header.Get("X-Netprobe-Timestamp")
	if n, err := strconv.ParseInt(ts, 10, 64); err != nil || time.Since(time.Unix(n, 0)) > time.Minute {
		t.Fatalf("timestamp %q: %v", ts, err)
	}
	mac := hmac.New(sha256.New, []byte("s3cret"))
	mac.Write([]byte(ts + "."))
	mac.Write(r.body)
	if want := "sha256=" + hex.EncodeToString(mac.Sum(nil)); r.header.Get("X-Netprobe-Signature") != want {
		t.Fatalf("signature %q, want %q", r.header.Get("X-Netprobe-Signature"), want)
	}
}

func TestWebhookWithoutSecretIsNotSigned(t *testing.T) {
	var header http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { header = r.Header.Clone() }))
	defer srv.Close()
	if err := alert.NewWebhook(probe.Policy{}).Send(context.Background(), store.Channel{URL: srv.URL}, alert.Payload{Event: alert.EventTest}); err != nil {
		t.Fatal(err)
	}
	if header.Get("X-Netprobe-Signature") != "" || header.Get("X-Netprobe-Timestamp") != "" {
		t.Fatalf("headers: %v", header)
	}
}

func TestWebhookFailures(t *testing.T) {
	var redirected bool
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { redirected = true }))
	defer target.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/redirect":
			http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
		case "/slow":
			// The server only notices that the client left once the body is read.
			_, _ = io.Copy(io.Discard, r.Body)
			<-r.Context().Done()
		default:
			http.Error(w, "boom", http.StatusInternalServerError)
		}
	}))
	defer srv.Close()
	send := func(ctx context.Context, path string) error {
		return alert.NewWebhook(probe.Policy{}).Send(ctx, store.Channel{URL: srv.URL + path + "?token=SECRET"}, alert.Payload{Event: alert.EventTest})
	}

	if err := send(context.Background(), "/fail"); err == nil || !strings.Contains(err.Error(), "500") {
		t.Fatalf("a 500: %v", err)
	}
	if err := send(context.Background(), "/redirect"); err == nil || redirected {
		t.Fatalf("a redirect was followed (%v) or accepted (%v)", redirected, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err := send(ctx, "/slow")
	if err == nil {
		t.Fatal("a timeout was hidden")
	}
	srv.Close()
	for _, e := range []error{err, send(context.Background(), "/fail")} {
		if e == nil || strings.Contains(e.Error(), "SECRET") || strings.Contains(e.Error(), srv.URL) {
			t.Fatalf("the error leaks the address or is missing: %v", e)
		}
	}
	if err := alert.NewWebhook(probe.Policy{}).Send(context.Background(), store.Channel{URL: "http://bad host/"}, alert.Payload{}); err == nil || strings.Contains(err.Error(), "bad host") {
		t.Fatalf("an unusable address: %v", err)
	}
}

func TestWebhookDoesNotConnectToDeniedAddresses(t *testing.T) {
	var reached bool
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached = true }))
	defer srv.Close()

	// The server listens on loopback, which the default policy denies.
	err := alert.NewWebhook(probe.DefaultPolicy()).Send(context.Background(), store.Channel{URL: srv.URL}, alert.Payload{Event: alert.EventTest})
	if err == nil || !strings.Contains(err.Error(), "denied by the policy") || reached {
		t.Fatalf("a loopback webhook was reached (%v): %v", reached, err)
	}
	for _, addr := range []string{"http://169.254.169.254/latest/meta-data/", "http://[64:ff9b::a9fe:a9fe]/"} {
		err := alert.NewWebhook(probe.DefaultPolicy()).Send(context.Background(), store.Channel{URL: addr}, alert.Payload{Event: alert.EventTest})
		if err == nil || !strings.Contains(err.Error(), "denied by the policy") {
			t.Errorf("%s: %v", addr, err)
		}
	}
}
