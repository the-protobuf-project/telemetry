#!/bin/bash
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROD_DIR="$(dirname "$SCRIPT_DIR")"

echo "=== Telemetry Telemetry EC2 Destroy ==="
echo "This will destroy the EC2 instance and all associated resources"
echo ""

read -p "Are you sure you want to destroy? (yes/no): " confirm
if [ "$confirm" != "yes" ]; then
    echo "Aborted."
    exit 0
fi

cd "$PROD_DIR/terraform"
terraform destroy

echo ""
echo "=== Destruction Complete ==="
