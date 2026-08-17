#!/bin/bash
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROD_DIR="$(dirname "$SCRIPT_DIR")"
OTEL_DIR="$(dirname "$(dirname "$PROD_DIR")")"

echo "=== Telemetry Telemetry Local/VM Deployment ==="

# Check for .env file
if [ ! -f "$PROD_DIR/.env" ]; then
    if [ -f "$PROD_DIR/.env.example" ]; then
        echo "Creating .env from .env.example..."
        cp "$PROD_DIR/.env.example" "$PROD_DIR/.env"
    else
        echo "Creating default .env..."
        cat > "$PROD_DIR/.env" << 'EOF'
GRAFANA_ADMIN_USER=admin
GRAFANA_ADMIN_PASSWORD=changeme
GRAFANA_DASHBOARD_DOMAIN=localhost
OTEL_API_DOMAIN=localhost
OTLP_API_AUTH_TOKEN=
EOF
    fi
    echo "⚠️  Please edit $PROD_DIR/.env with your credentials"
fi

# Load environment variables
source "$PROD_DIR/.env"

GRAFANA_DOMAIN="${GRAFANA_DASHBOARD_DOMAIN:-localhost}"
OTEL_DOMAIN="${OTEL_API_DOMAIN:-localhost}"
GRAFANA_ADMIN_USER="${GRAFANA_ADMIN_USER:-admin}"
GRAFANA_ADMIN_PASSWORD="${GRAFANA_ADMIN_PASSWORD:-changeme}"
OTLP_API_AUTH_TOKEN="${OTLP_API_AUTH_TOKEN:-}"

# Generate OTLP token if not set
if [ -z "$OTLP_API_AUTH_TOKEN" ]; then
    OTLP_API_AUTH_TOKEN=$(openssl rand -hex 32)
    echo "Generated OTLP auth token: $OTLP_API_AUTH_TOKEN"
fi

# Create required directories
mkdir -p "$PROD_DIR/certs"
mkdir -p "$PROD_DIR/dashboards"

# Copy dashboards if not present
if [ ! -f "$PROD_DIR/dashboards/metrics.dashboard.json" ]; then
    echo "Copying dashboards..."
    cp -r "$OTEL_DIR/dashboards/"* "$PROD_DIR/dashboards/" 2>/dev/null || true
fi

# Check for TLS certificates in .auth folder
AUTH_DIR="$PROD_DIR/.auth/certs"
mkdir -p "$AUTH_DIR"

if [ ! -f "$AUTH_DIR/fullchain.pem" ] || [ ! -f "$AUTH_DIR/privkey.pem" ]; then
    echo "⚠️  TLS certificates not found in $AUTH_DIR"
    echo "Generating self-signed certificates..."

    openssl req -x509 -nodes -days 365 -newkey rsa:2048 \
        -keyout "$AUTH_DIR/privkey.pem" \
        -out "$AUTH_DIR/fullchain.pem" \
        -subj "/CN=$GRAFANA_DOMAIN" \
        -addext "subjectAltName=DNS:$GRAFANA_DOMAIN,DNS:$OTEL_DOMAIN,DNS:localhost"

    chmod 644 "$AUTH_DIR/privkey.pem"
    echo "✓ Self-signed certificates generated in .auth/certs"
fi

# Copy to certs folder for docker-compose
mkdir -p "$PROD_DIR/certs"
cp "$AUTH_DIR/fullchain.pem" "$PROD_DIR/certs/"
cp "$AUTH_DIR/privkey.pem" "$PROD_DIR/certs/"

# Check Docker is installed
if ! command -v docker &> /dev/null; then
    echo "Error: Docker is not installed. Please install Docker first."
    exit 1
fi

# Check Docker Compose
if ! command -v docker-compose &> /dev/null && ! docker compose version &> /dev/null; then
    echo "Error: Docker Compose is not installed. Please install Docker Compose first."
    exit 1
fi

# Use docker compose or docker-compose
if docker compose version &> /dev/null; then
    COMPOSE_CMD="docker compose"
else
    COMPOSE_CMD="docker-compose"
fi

# Start the stack
echo "Starting Telemetry Telemetry stack..."
cd "$PROD_DIR"
$COMPOSE_CMD -f docker-compose.prod.yaml up -d --build

# Wait and show status
echo "Waiting for services to start..."
sleep 10

echo ""
echo "Service status:"
docker ps --format 'table {{.Names}}\t{{.Status}}' | grep telemetry

echo ""
echo "=== Deployment Complete ==="
echo ""
echo "Access:"
echo "  Dashboard: https://localhost (or https://$GRAFANA_DOMAIN after DNS)"
echo "  Credentials: $GRAFANA_ADMIN_USER / $GRAFANA_ADMIN_PASSWORD"
echo ""
echo "OTLP Endpoints:"
echo "  gRPC: localhost:4317"
echo "  HTTP: localhost:4318"
echo "  After DNS: https://$OTEL_DOMAIN (port 443)"
echo ""
echo "OTLP Authentication:"
echo "  Token: $OTLP_API_AUTH_TOKEN"
echo "  Header: Authorization: Bearer $OTLP_API_AUTH_TOKEN"
echo ""
echo "⚠️  Change the default password immediately!"
echo ""
echo "Save your OTLP token to .env:"
echo "  OTLP_API_AUTH_TOKEN=$OTLP_API_AUTH_TOKEN"
