ARG OTELCOL_VERSION=0.147.0
ARG LOKI_VERSION=3.6.7
ARG TEMPO_VERSION=2.10.2
ARG PROMETHEUS_VERSION=v3.10.0
ARG GRAFANA_VERSION=12.4.1
ARG ALERTMANAGER_VERSION=v0.31.1
ARG ENVOY_VERSION=v1.37-latest

# ── pull binaries from official images ───────────────────────────────
FROM otel/opentelemetry-collector-contrib:${OTELCOL_VERSION} AS otelcol
FROM grafana/loki:${LOKI_VERSION} AS loki
FROM grafana/tempo:${TEMPO_VERSION} AS tempo
FROM prom/prometheus:${PROMETHEUS_VERSION} AS prometheus
FROM grafana/grafana:${GRAFANA_VERSION} AS grafana
FROM prom/alertmanager:${ALERTMANAGER_VERSION} AS alertmanager
FROM prom/pushgateway:latest AS pushgateway
FROM envoyproxy/envoy:${ENVOY_VERSION} AS envoy

# ── build stage: collect everything ──────────────────────────────────
FROM alpine:3.21 AS builder

RUN apk add --no-cache supervisor curl bash ca-certificates fontconfig

COPY --from=otelcol /otelcol-contrib /usr/local/bin/otelcol-contrib
COPY --from=loki /usr/bin/loki /usr/local/bin/loki
COPY --from=tempo /tempo /usr/local/bin/tempo
COPY --from=prometheus /bin/prometheus /usr/local/bin/prometheus
COPY --from=grafana /usr/share/grafana /opt/grafana
COPY --from=grafana /run.sh /opt/grafana/run.sh
COPY --from=alertmanager /bin/alertmanager /usr/local/bin/alertmanager
COPY --from=pushgateway /bin/pushgateway /usr/local/bin/pushgateway
COPY --from=envoy /usr/local/bin/envoy /usr/local/bin/envoy

RUN chmod +x /opt/grafana/run.sh && \
  mkdir -p /data/loki /data/tempo /data/prometheus \
  /data/grafana /data/alertmanager /tmp/loki /tmp/tempo

# ── final runtime image ───────────────────────────────────────────────
# Use glibc runtime so Envoy starts reliably on arm64/amd64.
FROM debian:bookworm-slim

# OCI labels link the published package to the repo (enables the GHCR
# package to inherit repo visibility and show source/README).
LABEL org.opencontainers.image.source="https://github.com/the-protobuf-project/telemetry" \
  org.opencontainers.image.title="telemetry-stack" \
  org.opencontainers.image.description="Grafana, Prometheus, Pushgateway, Tempo, Loki, Alertmanager, OTel Collector and Envoy in a single image, fronted by one ingress port (6009)." \
  org.opencontainers.image.licenses="Apache-2.0"

RUN apt-get update && \
  apt-get install -y --no-install-recommends \
  supervisor \
  curl \
  bash \
  ca-certificates \
  fontconfig \
  libstdc++6 && \
  rm -rf /var/lib/apt/lists/*

# binaries
COPY --from=builder /usr/local/bin/otelcol-contrib /usr/local/bin/otelcol-contrib
COPY --from=builder /usr/local/bin/loki /usr/local/bin/loki
COPY --from=builder /usr/local/bin/tempo /usr/local/bin/tempo
COPY --from=builder /usr/local/bin/prometheus /usr/local/bin/prometheus
COPY --from=builder /usr/local/bin/alertmanager /usr/local/bin/alertmanager
COPY --from=builder /usr/local/bin/pushgateway /usr/local/bin/pushgateway
COPY --from=builder /usr/local/bin/envoy /usr/local/bin/envoy
COPY --from=builder /opt/grafana /opt/grafana

# data dirs
COPY --from=builder /data /data
RUN mkdir -p /tmp/loki /tmp/tempo

# scripts + supervisord config
COPY supervisord.conf /etc/supervisor/conf.d/supervisord.conf
COPY config /etc/opentelemetry-stack
COPY envoy/envoy.yaml /etc/opentelemetry-stack/envoy.yaml
RUN mkdir -p /etc/envoy && cp /etc/opentelemetry-stack/envoy.yaml /etc/envoy/envoy.yaml
COPY entrypoint.sh /entrypoint.sh
COPY health_check.sh /health_check.sh
RUN chmod +x /entrypoint.sh /health_check.sh

VOLUME ["/data"]

# Single ingress port for OTLP traffic via Envoy
EXPOSE 6009

ENTRYPOINT ["/entrypoint.sh"]
