CREATE EXTENSION IF NOT EXISTS timescaledb;

CREATE TABLE edges (
    id          text PRIMARY KEY,
    name        text NOT NULL,
    token_hash  text NOT NULL UNIQUE,
    created_at  timestamptz NOT NULL,
    revoked_at  timestamptz
);

-- A name is free again once its edge is revoked.
CREATE UNIQUE INDEX edges_active_name ON edges (name) WHERE revoked_at IS NULL;

CREATE TABLE checks (
    id               text PRIMARY KEY,
    kind             text NOT NULL,
    target           text NOT NULL,
    interval_seconds integer NOT NULL CHECK (interval_seconds >= 1)
);

-- No foreign key on check_id: results outlive the checks they came from.
CREATE TABLE results (
    edge_id     text NOT NULL REFERENCES edges (id),
    check_id    text NOT NULL,
    at          timestamptz NOT NULL,
    ok          boolean NOT NULL,
    rtt_millis  double precision NOT NULL,
    error       text NOT NULL DEFAULT ''
);

SELECT create_hypertable('results', 'at');

CREATE INDEX results_check_at ON results (check_id, at DESC);
CREATE INDEX results_edge_at ON results (edge_id, at DESC);

ALTER TABLE results SET (
    timescaledb.compress,
    timescaledb.compress_segmentby = 'edge_id, check_id',
    timescaledb.compress_orderby = 'at DESC'
);
SELECT add_compression_policy('results', INTERVAL '7 days');
