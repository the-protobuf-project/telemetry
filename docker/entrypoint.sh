#!/bin/bash
set -e

SUPERVISOR_CONF="/etc/supervisor/conf.d/supervisord.conf"
EMBEDDED_CONFIG_DIR="/etc/opentelemetry-stack"
STACK_CONFIG_DIR="${STACK_CONFIG_DIR:-/config}"

if [ ! -e "$STACK_CONFIG_DIR" ]; then
    ln -s "$EMBEDDED_CONFIG_DIR" "$STACK_CONFIG_DIR"
fi

export STACK_CONFIG_DIR
export ENVOY_CONFIG="${ENVOY_CONFIG:-${STACK_CONFIG_DIR}/envoy.yaml}"
export GRAFANA_DOMAIN="${GRAFANA_DOMAIN:-127.0.0.1}"

ENABLE_ENVOY="${ENABLE_ENVOY:-true}"
if [ "$ENABLE_ENVOY" = "false" ] || [ "$ENABLE_ENVOY" = "0" ]; then
    sed -i '/\[program:envoy\]/,/priority=/{s/autostart=true/autostart=false/}' "$SUPERVISOR_CONF"
    echo "Envoy disabled"
else
    if ! command -v envoy >/dev/null 2>&1; then
        echo "Error: Envoy binary not found in image. Build/pull an image that includes /usr/local/bin/envoy." >&2
        exit 1
    fi
    if [ -f "${STACK_CONFIG_DIR}/otel-collector.yaml" ] && grep -Eq 'endpoint:[[:space:]]*0\.0\.0\.0:6009' "${STACK_CONFIG_DIR}/otel-collector.yaml"; then
        echo "Error: ${STACK_CONFIG_DIR}/otel-collector.yaml still binds OTLP gRPC to :6009." >&2
        echo "Fix it to :4317 (with HTTP on :4318) when ENABLE_ENVOY=true, then restart the container." >&2
        exit 1
    fi
    sed -i '/\[program:envoy\]/,/priority=/{s/autostart=false/autostart=true/}' "$SUPERVISOR_CONF"
    echo "Envoy enabled on port 6009"
fi

mkdir -p /data/loki /data/tempo /data/prometheus /data/pyroscope \
    /data/grafana /data/alertmanager /tmp/loki /tmp/tempo

# ── OTel Collector sizing (docker/README.md, "Sizing for 1M records/s") ──
# The collector shares the container with Loki, Tempo, Prometheus and the
# rest, so it gets a fixed share of the container's memory and cores rather
# than all of them. OTELCOL_MEMORY_MIB / OTELCOL_CPUS set the share outright.
container_memory_mib() {
    local max
    max="$(cat /sys/fs/cgroup/memory.max 2>/dev/null || true)"
    if [[ "$max" =~ ^[0-9]+$ ]]; then
        echo $(( max / 1048576 ))
    else
        awk '/MemTotal/ { print int($2 / 1024) }' /proc/meminfo
    fi
}
container_cpus() {
    local quota period
    read -r quota period < /sys/fs/cgroup/cpu.max 2>/dev/null || true
    if [[ "$quota" =~ ^[0-9]+$ && "$period" =~ ^[0-9]+$ && "$period" -gt 0 ]]; then
        echo $(( (quota + period - 1) / period ))
    else
        nproc
    fi
}
total_mib="$(container_memory_mib)"
total_cpus="$(container_cpus)"
OTELCOL_MEMORY_MIB="${OTELCOL_MEMORY_MIB:-$(( total_mib * ${OTELCOL_MEMORY_SHARE_PCT:-25} / 100 ))}"
OTELCOL_CPUS="${OTELCOL_CPUS:-$(( total_cpus > 1 ? total_cpus / 2 : 1 ))}"
# memory_limiter refuses data above 80% of the share and keeps 20% for a
# spike; GOMEMLIMIT makes the Go GC work to stay under the refusal point.
export OTELCOL_MEMORY_LIMIT_MIB="${OTELCOL_MEMORY_LIMIT_MIB:-$(( OTELCOL_MEMORY_MIB * 80 / 100 ))}"
export OTELCOL_MEMORY_SPIKE_MIB="${OTELCOL_MEMORY_SPIKE_MIB:-$(( OTELCOL_MEMORY_MIB * 20 / 100 ))}"
export OTELCOL_GOMEMLIMIT="${OTELCOL_GOMEMLIMIT:-$(( OTELCOL_MEMORY_LIMIT_MIB ))MiB}"
export OTELCOL_GOMAXPROCS="$OTELCOL_CPUS"
echo "OTel Collector: ${OTELCOL_CPUS} of ${total_cpus} CPUs, ${OTELCOL_MEMORY_MIB} of ${total_mib} MiB (refuses above ${OTELCOL_MEMORY_LIMIT_MIB} MiB)"

echo "Starting OpenTelemetry stack via supervisord..."
exec /usr/bin/supervisord -c "$SUPERVISOR_CONF"
