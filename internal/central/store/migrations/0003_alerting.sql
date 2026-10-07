-- Where notifications go. The secret signs what is sent to the url.
CREATE TABLE channels (
    name        text PRIMARY KEY,
    url         text NOT NULL,
    secret      text NOT NULL DEFAULT '',
    created_at  timestamptz NOT NULL
);

-- A check that keeps failing on an edge ('check'), or an edge that stopped
-- reporting ('edge', with an empty check_id). Open until resolved_at is set.
CREATE TABLE incidents (
    id          bigserial PRIMARY KEY,
    kind        text NOT NULL CHECK (kind IN ('check', 'edge')),
    check_id    text NOT NULL DEFAULT '',
    edge_id     text NOT NULL REFERENCES edges (id),
    started_at  timestamptz NOT NULL,
    resolved_at timestamptz,
    detail      text NOT NULL DEFAULT '',
    resolution  text NOT NULL DEFAULT ''
);

-- One open incident per subject, even when several centrals evaluate at once.
CREATE UNIQUE INDEX incidents_open ON incidents (kind, check_id, edge_id) WHERE resolved_at IS NULL;
CREATE INDEX incidents_started_at ON incidents (started_at DESC);

-- What was told to which channel. No foreign key on channel: removing a
-- channel must not need the history to go with it.
CREATE TABLE deliveries (
    incident_id  bigint NOT NULL REFERENCES incidents (id) ON DELETE CASCADE,
    event        text NOT NULL CHECK (event IN ('opened', 'resolved')),
    channel      text NOT NULL,
    delivered_at timestamptz NOT NULL,
    PRIMARY KEY (incident_id, event, channel)
);
