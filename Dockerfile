# syntax=docker/dockerfile:1
#
# Three images from one file: --target central, --target edge or --target web.

FROM node:lts-alpine@sha256:ebfe2f90462722a7a4de65e91990e97fe0d401c70e0e762c5b53302f905ec1c1 AS webbuild
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.27-alpine@sha256:8a5910f31396cd4d89662f56c68b3ae31d374308270a1c3bd96672ee5ed43414 AS gobuild
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
ARG VERSION=dev
ARG COMMIT=none
ARG DATE=unknown
ARG PKG=github.com/Arylite/netprobe/internal/version
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath \
    -ldflags "-s -w -X ${PKG}.Version=${VERSION} -X ${PKG}.Commit=${COMMIT} -X ${PKG}.Date=${DATE}" \
    -o /out/ ./cmd/...

FROM gcr.io/distroless/static-debian13:nonroot@sha256:e2e927ec666bae08560abb3c55d0659eceabb657f56b6782ab500a9fc7f555e3 AS central
LABEL org.opencontainers.image.title="netprobe-central" \
      org.opencontainers.image.source="https://github.com/Arylite/netprobe" \
      org.opencontainers.image.licenses="Apache-2.0"
COPY --from=gobuild /out/netprobe-central /netprobe-central
ENV NETPROBE_EDGE_LISTEN=0.0.0.0:8080 NETPROBE_API_LISTEN=0.0.0.0:8081
EXPOSE 8080 8081
USER 65532:65532
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s CMD ["/netprobe-central", "healthcheck"]
ENTRYPOINT ["/netprobe-central"]
CMD ["serve"]

FROM gcr.io/distroless/static-debian13:nonroot@sha256:e2e927ec666bae08560abb3c55d0659eceabb657f56b6782ab500a9fc7f555e3 AS edge
LABEL org.opencontainers.image.title="netprobe-edge" \
      org.opencontainers.image.source="https://github.com/Arylite/netprobe" \
      org.opencontainers.image.licenses="Apache-2.0"
COPY --from=gobuild /out/netprobe-edge /netprobe-edge
USER 65532:65532
ENTRYPOINT ["/netprobe-edge"]

FROM nginxinc/nginx-unprivileged:stable-alpine@sha256:15c994d10d6d78658721c3bcafff14cb281fba2a4bdf9d5ba92c416a472516e3 AS web
LABEL org.opencontainers.image.title="netprobe-web" \
      org.opencontainers.image.source="https://github.com/Arylite/netprobe" \
      org.opencontainers.image.licenses="Apache-2.0"
COPY --from=webbuild /src/web/dist /usr/share/nginx/html
COPY deploy/web/default.conf.template /etc/nginx/templates/default.conf.template
COPY deploy/web/security-headers.conf /etc/nginx/security-headers.conf
# Where the UI finds the UI API: served as config.json and allowed by the CSP.
ENV NETPROBE_API_URL=http://127.0.0.1:8081
EXPOSE 8080
