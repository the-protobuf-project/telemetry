#!/bin/bash
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROD_DIR="$(dirname "$SCRIPT_DIR")"
K8S_DIR="$PROD_DIR/k8s"

echo "=== Telemetry Telemetry EKS Deployment ==="

# Check prerequisites
command -v kubectl >/dev/null 2>&1 || { echo "kubectl is required but not installed."; exit 1; }
command -v aws >/dev/null 2>&1 || { echo "AWS CLI is required but not installed."; exit 1; }

# Check for .env file
if [ ! -f "$PROD_DIR/.env" ]; then
    echo "Creating .env from .env.example..."
    cp "$PROD_DIR/.env.example" "$PROD_DIR/.env"
    echo "⚠️  Please edit $PROD_DIR/.env with your AWS configuration"
    exit 1
fi

source "$PROD_DIR/.env"

# Verify kubectl context
echo "Current kubectl context:"
kubectl config current-context
echo ""
read -p "Is this the correct EKS cluster? (y/n) " -n 1 -r
echo
if [[ ! $REPLY =~ ^[Yy]$ ]]; then
    echo "Please configure kubectl to use the correct EKS cluster"
    echo "Run: aws eks update-kubeconfig --region $AWS_REGION --name YOUR_CLUSTER_NAME"
    exit 1
fi

# Generate OTLP token if not set
OTLP_AUTH_TOKEN="${OTLP_AUTH_TOKEN:-}"
if [ -z "$OTLP_AUTH_TOKEN" ]; then
    OTLP_AUTH_TOKEN=$(openssl rand -hex 32)
    echo "Generated OTLP auth token: $OTLP_AUTH_TOKEN"
fi

# Update secrets with actual values
echo "Updating secrets..."
GRAFANA_PASS_B64=$(echo -n "$GRAFANA_ADMIN_PASSWORD" | base64)
GRAFANA_USER_B64=$(echo -n "$GRAFANA_ADMIN_USER" | base64)
OTLP_TOKEN_B64=$(echo -n "$OTLP_AUTH_TOKEN" | base64)

# Create temporary secrets file with actual values
cat > "$K8S_DIR/secrets-generated.yaml" << EOF
apiVersion: v1
kind: Secret
metadata:
  name: telemetry-secrets
  namespace: telemetry
type: Opaque
data:
  grafana-admin-user: $GRAFANA_USER_B64
  grafana-admin-password: $GRAFANA_PASS_B64
  otlp-auth-token: $OTLP_TOKEN_B64
EOF

# Update ingress with ACM certificate ARN
if [ -n "$ACM_CERTIFICATE_ARN" ]; then
    sed -i.bak "s|\${ACM_CERTIFICATE_ARN}|$ACM_CERTIFICATE_ARN|g" "$K8S_DIR/ingress.yaml"
fi

# Apply manifests
echo "Applying Kubernetes manifests..."
kubectl apply -f "$K8S_DIR/namespace.yaml"
kubectl apply -f "$K8S_DIR/secrets-generated.yaml"
kubectl apply -f "$K8S_DIR/configmaps.yaml"
kubectl apply -f "$K8S_DIR/deployments.yaml"
kubectl apply -f "$K8S_DIR/services.yaml"
kubectl apply -f "$K8S_DIR/ingress.yaml"

# Clean up generated secrets file
rm -f "$K8S_DIR/secrets-generated.yaml"

# Wait for deployments
echo "Waiting for deployments to be ready..."
kubectl -n telemetry rollout status deployment/grafana --timeout=300s
kubectl -n telemetry rollout status deployment/prometheus --timeout=300s
kubectl -n telemetry rollout status deployment/otel-collector --timeout=300s

# Get ingress address
echo ""
echo "=== Deployment Complete ==="
echo ""
echo "Ingress status:"
kubectl -n telemetry get ingress

echo ""
echo "Services:"
kubectl -n telemetry get svc

echo ""
OTEL_DOMAIN="${OTEL_DOMAIN:-otel.example.com}"
echo "After DNS propagation:"
echo "  Dashboard: https://$DOMAIN"
echo "  OTLP Endpoint: https://$OTEL_DOMAIN"
echo ""
echo "To get the ALB DNS name for DNS configuration:"
echo "kubectl -n telemetry get ingress telemetry-ingress -o jsonpath='{.status.loadBalancer.ingress[0].hostname}'"
