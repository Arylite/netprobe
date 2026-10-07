package alert

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/Arylite/netprobe/internal/central/store"
	"github.com/Arylite/netprobe/internal/probe"
	"github.com/Arylite/netprobe/internal/version"
)

// Events a webhook is called for, besides the two of an incident.
const EventTest = "test"

const webhookTimeout = 10 * time.Second

// Payload is the JSON body posted to a channel. Text reads as a sentence, which
// is all that chat tools with incoming webhooks ask for.
type Payload struct {
	Event    string        `json:"event"`
	Text     string        `json:"text"`
	Incident *IncidentJSON `json:"incident,omitempty"`
}

// IncidentJSON is an incident as a webhook sees it.
type IncidentJSON struct {
	ID         int64      `json:"id"`
	Kind       string     `json:"kind"`
	CheckID    string     `json:"check_id,omitempty"`
	EdgeID     string     `json:"edge_id"`
	Edge       string     `json:"edge"`
	StartedAt  time.Time  `json:"started_at"`
	ResolvedAt *time.Time `json:"resolved_at,omitempty"`
	Detail     string     `json:"detail,omitempty"`
	Resolution string     `json:"resolution,omitempty"`
}

// ToJSON describes an incident for the UI API and for webhooks alike.
func ToJSON(i store.Incident) IncidentJSON {
	return IncidentJSON{
		ID: i.ID, Kind: i.Kind, CheckID: i.CheckID, EdgeID: i.EdgeID, Edge: i.EdgeName,
		StartedAt: i.StartedAt, ResolvedAt: i.ResolvedAt, Detail: i.Detail, Resolution: i.Resolution,
	}
}

// NewPayload says what happened in a notification.
func NewPayload(n store.Notification) Payload {
	i := n.Incident
	j := ToJSON(i)
	return Payload{Event: n.Event, Text: text(n.Event, i), Incident: &j}
}

func text(event string, i store.Incident) string {
	subject := i.EdgeName
	if i.Kind == store.IncidentCheck {
		subject = i.CheckID + " on " + i.EdgeName
	}
	switch {
	case event == store.EventOpened && i.Kind == store.IncidentCheck:
		return fmt.Sprintf("[netprobe] %s is failing: %s", subject, i.Detail)
	case event == store.EventOpened:
		return fmt.Sprintf("[netprobe] %s stopped reporting: %s", subject, i.Detail)
	case i.Resolution == store.ResolutionRecovered && i.Kind == store.IncidentCheck:
		return fmt.Sprintf("[netprobe] %s is back to normal", subject)
	case i.Resolution == store.ResolutionRecovered:
		return fmt.Sprintf("[netprobe] %s reports again", subject)
	}
	return fmt.Sprintf("[netprobe] %s: incident closed (%s)", subject, i.Resolution)
}

// send delivers a payload within the time a channel is given.
func send(ctx context.Context, s Sender, ch store.Channel, p Payload) error {
	ctx, cancel := context.WithTimeout(ctx, webhookTimeout)
	defer cancel()
	return s.Send(ctx, ch, p)
}

// SendTest sends a test notification, to check that a channel works.
func SendTest(ctx context.Context, s Sender, ch store.Channel) error {
	return send(ctx, s, ch, Payload{Event: EventTest, Text: "[netprobe] this is a test notification"})
}

// Sender delivers a payload to a channel.
type Sender interface {
	Send(ctx context.Context, ch store.Channel, p Payload) error
}

// Webhook posts payloads as JSON. Redirects are not followed: a channel is
// where the administrator said, nowhere else.
type Webhook struct {
	Client *http.Client
}

// NewWebhook returns a sender with a timeout and no redirects. It never
// connects to an address the policy denies, whatever name led to it.
func NewWebhook(policy probe.Policy) *Webhook {
	dialer := &net.Dialer{Timeout: webhookTimeout, Control: policy.Control}
	return &Webhook{Client: &http.Client{
		Timeout:       webhookTimeout,
		Transport:     &http.Transport{DialContext: dialer.DialContext, TLSHandshakeTimeout: webhookTimeout, ResponseHeaderTimeout: webhookTimeout},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

// Send posts the payload, signed when the channel has a secret (see the README).
// An error never holds the address, which may hold a secret of its own.
func (w *Webhook) Send(ctx context.Context, ch store.Channel, p Payload) error {
	body, err := json.Marshal(p)
	if err != nil {
		return fmt.Errorf("encode payload: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ch.URL, bytes.NewReader(body))
	if err != nil {
		return errors.New("the address is not usable")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "netprobe/"+version.Version)
	req.Header.Set("X-Netprobe-Event", p.Event)
	if ch.Secret != "" {
		ts := strconv.FormatInt(time.Now().Unix(), 10)
		mac := hmac.New(sha256.New, []byte(ch.Secret))
		mac.Write([]byte(ts + "."))
		mac.Write(body)
		req.Header.Set("X-Netprobe-Timestamp", ts)
		req.Header.Set("X-Netprobe-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	}
	res, err := w.Client.Do(req)
	if err != nil {
		if ue, ok := errors.AsType[*url.Error](err); ok {
			err = ue.Err
		}
		return err
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 4096))
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return fmt.Errorf("the channel answered %s", res.Status)
	}
	return nil
}
