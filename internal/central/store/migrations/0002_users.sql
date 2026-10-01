CREATE TABLE users (
    username       text PRIMARY KEY,
    password_hash  text NOT NULL,
    role           text NOT NULL CHECK (role IN ('admin', 'viewer')),
    created_at     timestamptz NOT NULL
);

-- Only the hash of a session token is stored. Deleting a user ends its sessions.
CREATE TABLE sessions (
    token_hash  text PRIMARY KEY,
    username    text NOT NULL REFERENCES users (username) ON DELETE CASCADE,
    created_at  timestamptz NOT NULL,
    expires_at  timestamptz NOT NULL
);

CREATE INDEX sessions_expires_at ON sessions (expires_at);
