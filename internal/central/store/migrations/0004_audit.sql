-- Who did what, and from where. An actor may be gone and a target removed: no
-- foreign keys, the trail must outlive what it is about.
CREATE TABLE audit_events (
    id         bigserial PRIMARY KEY,
    at         timestamptz NOT NULL,
    actor      text NOT NULL,
    action     text NOT NULL,
    target     text NOT NULL DEFAULT '',
    client_ip  text NOT NULL DEFAULT ''
);

CREATE INDEX audit_events_at ON audit_events (at DESC);
