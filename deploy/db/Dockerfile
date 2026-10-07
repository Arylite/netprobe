# TimescaleDB without the Go tools of its image: the central uses none of them,
# and their Go runtime carries known vulnerabilities. It runs as postgres from
# the start, which is what gosu was for, and skips the tuning of the first start.
FROM timescale/timescaledb:2.30.2-pg18@sha256:e72689191e1c977892c53d6f2c344dbc4a9657a867dc8cc1899229f9d3672b2e
LABEL org.opencontainers.image.title="netprobe-db" \
      org.opencontainers.image.source="https://github.com/Arylite/netprobe" \
      org.opencontainers.image.licenses="Apache-2.0"
RUN rm -f /usr/local/bin/gosu /usr/local/bin/timescaledb-parallel-copy /usr/local/bin/timescaledb-tune
ENV NO_TS_TUNE=1
USER postgres
