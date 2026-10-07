# The image renderer of Grafana, with the security updates of Debian: the image
# ships a Chromium that is behind its fixes.
FROM grafana/grafana-image-renderer:v5.12.5@sha256:76542ccc4c045e5ff9f80f87b25de6fcc71b31222172de5031a8ceba33772ee1
LABEL org.opencontainers.image.title="netprobe-renderer" \
      org.opencontainers.image.source="https://github.com/Arylite/netprobe" \
      org.opencontainers.image.licenses="Apache-2.0"
USER root
RUN apt-get update \
    && apt-get upgrade -y --no-install-recommends \
    && rm -rf /var/lib/apt/lists/*
USER 65532
