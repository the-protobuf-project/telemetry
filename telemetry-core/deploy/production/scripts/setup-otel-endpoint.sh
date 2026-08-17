#!/bin/bash
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROD_DIR="$(dirname "$SCRIPT_DIR")"

# Load .env if exists
if [ -f "$PROD_DIR/.env" ]; then
    source "$PROD_DIR/.env"
fi

# Configuration
OTEL_DOMAIN="${OTEL_DOMAIN:-otel.example.com}"
TELEMETRY_DOMAIN="${DOMAIN:-telemetry.example.com}"
OTLP_TOKEN="${OTLP_AUTH_TOKEN:-}"

echo "=== Telemetry OTEL Endpoint Setup ==="
echo ""

# Generate token if not provided
if [ -z "$OTLP_TOKEN" ]; then
    echo "Generating new OTLP authentication token..."
    OTLP_TOKEN=$(openssl rand -hex 32)
    echo "Generated token: $OTLP_TOKEN"
    echo ""
    echo "IMPORTANT: Save this token! Add it to your .env file:"
    echo "  OTLP_AUTH_TOKEN=$OTLP_TOKEN"
    echo ""
fi

echo "OTEL Endpoint Configuration:"
echo "  Domain: $OTEL_DOMAIN"
echo "  Telemetry Domain: $TELEMETRY_DOMAIN"
echo ""

# Update secrets.yaml with the token
SECRETS_FILE="$PROD_DIR/k8s/secrets.yaml"
if [ -f "$SECRETS_FILE" ]; then
    echo "Updating secrets.yaml with OTLP token..."
    # Use sed to replace the placeholder token
    if [[ "$OSTYPE" == "darwin"* ]]; then
        sed -i '' "s/otlp-auth-token: .*/otlp-auth-token: $OTLP_TOKEN/" "$SECRETS_FILE"
    else
        sed -i "s/otlp-auth-token: .*/otlp-auth-token: $OTLP_TOKEN/" "$SECRETS_FILE"
    fi
    echo "Updated $SECRETS_FILE"
fi

echo ""
echo "=== DNS Configuration Required ==="
echo "Create the following DNS A records pointing to your load balancer/server IP:"
echo "  1. $TELEMETRY_DOMAIN -> <YOUR_IP>"
echo "  2. $OTEL_DOMAIN -> <YOUR_IP>"
echo ""

echo "=== Client Configuration ==="
echo ""
echo "Go (telemetry-go) configuration:"
cat << EOF
telemetryOpts := options.TelemetryOptions{
    Telemetry: options.DefaultTelemetry(),
}
telemetryOpts.Telemetry.OTLP.Enabled = true
telemetryOpts.Telemetry.OTLP.Host = "$OTEL_DOMAIN"
telemetryOpts.Telemetry.OTLP.Port = 443
telemetryOpts.Telemetry.OTLP.Secure = true
telemetryOpts.Telemetry.OTLP.UseHTTP = true
EOF
echo ""
echo "Set the authentication token as environment variable:"
echo "  export OTEL_EXPORTER_OTLP_HEADERS=\"Authorization=Bearer $OTLP_TOKEN\""
echo ""

echo "=== Python configuration ==="
cat << EOF
from opentelemetry.exporter.otlp.proto.http.trace_exporter import OTLPSpanExporter

exporter = OTLPSpanExporter(
    endpoint="https://$OTEL_DOMAIN/v1/traces",
    headers={"Authorization": "Bearer $OTLP_TOKEN"}
)
EOF
echo ""

echo "=== Node.js configuration ==="
cat << EOF
const { OTLPTraceExporter } = require('@opentelemetry/exporter-trace-otlp-http');

const exporter = new OTLPTraceExporter({
  url: 'https://$OTEL_DOMAIN/v1/traces',
  headers: {
    'Authorization': 'Bearer $OTLP_TOKEN'
  }
});
EOF
echo ""

echo "=== cURL test command ==="
echo "curl -v https://$OTEL_DOMAIN/v1/traces \\"
echo "  -H 'Content-Type: application/json' \\"
echo "  -H 'Authorization: Bearer $OTLP_TOKEN' \\"
echo "  -d '{}'"
echo ""

echo "=== Environment Variables for .env ==="
echo "Add these to your $PROD_DIR/.env file:"
echo ""
echo "OTEL_DOMAIN=$OTEL_DOMAIN"
echo "OTLP_AUTH_TOKEN=$OTLP_TOKEN"
echo ""

# Optionally append to .env
read -p "Append to .env file? (y/N): " -n 1 -r
echo
if [[ $REPLY =~ ^[Yy]$ ]]; then
    echo "" >> "$PROD_DIR/.env"
    echo "# OTEL Endpoint Configuration" >> "$PROD_DIR/.env"
    echo "OTEL_DOMAIN=$OTEL_DOMAIN" >> "$PROD_DIR/.env"
    echo "OTLP_AUTH_TOKEN=$OTLP_TOKEN" >> "$PROD_DIR/.env"
    echo "Updated $PROD_DIR/.env"
fi

echo ""
echo "=== Setup Complete ==="
echo "Next steps:"
echo "  1. Configure DNS records"
echo "  2. Deploy with: ./deploy-ec2.sh or ./deploy-eks.sh"
echo "  3. Use the token in your client applications"
