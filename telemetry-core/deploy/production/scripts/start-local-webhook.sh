#!/bin/bash
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WEBHOOK_DIR="$(dirname "$SCRIPT_DIR")/local-webhook"

echo "=== Starting Local Alert Webhook Receiver ==="

cd "$WEBHOOK_DIR"

# Check if Go is installed
if ! command -v go &> /dev/null; then
    echo "Go is not installed. Please install Go 1.21+ first."
    exit 1
fi

# Build and run
echo "Building webhook receiver..."
go build -o alert-webhook .

echo "Starting webhook receiver on port 9095..."
echo "This will receive alerts from Alertmanager and show desktop notifications"
echo ""
echo "Press Ctrl+C to stop"
echo ""

./alert-webhook
