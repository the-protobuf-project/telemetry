#!/bin/bash
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROD_DIR="$(dirname "$SCRIPT_DIR")"
OTEL_DIR="$(dirname "$(dirname "$PROD_DIR")")"

# Load .env if exists
if [ -f "$PROD_DIR/.env" ]; then
    source "$PROD_DIR/.env"
fi

SSH_KEY="${SSH_KEY_PATH:-~/.ssh/id_ed25519}"

echo "=== Telemetry Telemetry EC2 Re-deployment ==="
echo "Use this script to update an existing EC2 deployment"

# Get public IP from terraform output or argument
if [ -n "$1" ]; then
    PUBLIC_IP="$1"
else
    cd "$PROD_DIR/terraform"
    PUBLIC_IP=$(terraform output -raw public_ip 2>/dev/null || echo "")
    cd "$PROD_DIR"
fi

if [ -z "$PUBLIC_IP" ]; then
    echo "Error: No public IP found. Pass IP as argument: ./redeploy-ec2.sh <PUBLIC_IP>"
    exit 1
fi

echo "Re-deploying to: $PUBLIC_IP"

# Copy updated files
echo "Copying updated files..."
scp -i "$SSH_KEY" -r "$PROD_DIR/config/"* ec2-user@"$PUBLIC_IP":/opt/telemetry/config/
scp -i "$SSH_KEY" -r "$PROD_DIR/envoy/"* ec2-user@"$PUBLIC_IP":/opt/telemetry/envoy/
scp -i "$SSH_KEY" "$PROD_DIR/docker-compose.prod.yaml" ec2-user@"$PUBLIC_IP":/opt/telemetry/
scp -i "$SSH_KEY" -r "$OTEL_DIR/dashboards/"* ec2-user@"$PUBLIC_IP":/opt/telemetry/dashboards/
scp -i "$SSH_KEY" -r "$OTEL_DIR/docker/"* ec2-user@"$PUBLIC_IP":/opt/telemetry/docker/

# Restart services
echo "Restarting services..."
ssh -i "$SSH_KEY" ec2-user@"$PUBLIC_IP" "cd /opt/telemetry && sudo docker-compose -f docker-compose.prod.yaml down && sudo docker-compose -f docker-compose.prod.yaml up -d --build"

# Wait and show status
sleep 10
echo ""
echo "Service status:"
ssh -i "$SSH_KEY" ec2-user@"$PUBLIC_IP" "sudo docker ps --format 'table {{.Names}}\t{{.Status}}'"

echo ""
echo "=== Re-deployment Complete ==="
