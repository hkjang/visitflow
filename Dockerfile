# syntax=docker/dockerfile:1.7
FROM node:24-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci --ignore-scripts && node node_modules/esbuild/install.js
COPY web/ ./
RUN npm run build

FROM golang:1.24-bookworm AS backend
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ cmd/
COPY internal/ internal/
RUN rm -rf cmd/visitflow/webdist && mkdir -p cmd/visitflow/webdist
COPY --from=web /src/web/dist/ cmd/visitflow/webdist/
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILT_AT=unknown
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath \
    -ldflags="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.builtAt=${BUILT_AT}" \
    -o /out/visitflow ./cmd/visitflow

FROM debian:bookworm-slim
LABEL org.opencontainers.image.title="VisitFlow" \
      org.opencontainers.image.description="Offline-ready enterprise visitor management" \
      org.opencontainers.image.source="https://github.com/hkjang/seaton"
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates tzdata \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd --system --gid 10001 visitflow \
    && useradd --system --uid 10001 --gid visitflow --home-dir /var/lib/visitflow visitflow \
    && install -d -o visitflow -g visitflow -m 0700 /var/lib/visitflow
COPY --from=backend --chown=visitflow:visitflow /out/visitflow /usr/local/bin/visitflow
USER 10001:10001
EXPOSE 8080
VOLUME ["/var/lib/visitflow"]
ENTRYPOINT ["/usr/local/bin/visitflow"]
