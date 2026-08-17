#!/bin/bash
# Telemetry Telemetry - Deployment Script
# Architecture: Route53 → CloudFront (ACM TLS) → EC2 (HTTP)
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROD_DIR="$(dirname "$SCRIPT_DIR")"
OTEL_DIR="$(dirname "$(dirname "$PROD_DIR")")"

RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; BLUE='\033[0;34m'; NC='\033[0m'

[ -f "$PROD_DIR/.env" ] && { set -a; source "$PROD_DIR/.env"; set +a; }

# SSH key setup
SSH_AUTH_DIR="$PROD_DIR/.auth/ssh"
if [ ! -f "$SSH_AUTH_DIR/id_ed25519" ]; then
    mkdir -p "$SSH_AUTH_DIR"
    ssh-keygen -t ed25519 -f "$SSH_AUTH_DIR/id_ed25519" -N "" -C "telemetry-deploy"
    chmod 600 "$SSH_AUTH_DIR/id_ed25519"
    echo -e "${GREEN}SSH key generated:${NC}"; cat "$SSH_AUTH_DIR/id_ed25519.pub"
fi
SSH_KEY="$SSH_AUTH_DIR/id_ed25519"

# Config
GRAFANA_USER="${GRAFANA_ADMIN_USER:-admin}"
GRAFANA_PASS="${GRAFANA_ADMIN_PASSWORD:-changeme}"
GRAFANA_DOMAIN="${GRAFANA_DASHBOARD_DOMAIN:-telemetry.the-protobuf-project.dev}"
OTEL_DOMAIN="${OTEL_API_DOMAIN:-otel.the-protobuf-project.dev}"
ROUTE53_ZONE_ID="${ROUTE53_ZONE_ID:-Z024449328NM4DCSLOMSC}"
[ -z "${OTLP_API_AUTH_TOKEN:-}" ] && OTLP_API_AUTH_TOKEN=$(openssl rand -hex 32)

# usage prints the available deployment commands and usage examples.
usage() {
    cat << EOF
${BLUE}Telemetry Telemetry Deployment${NC}

Usage: $0 <command> [options]

Commands:
  up              Full deploy (terraform + provision)
  down            Destroy infrastructure
  provision <ip>  Deploy to EC2
  deploy <ip>     Update services
  status <ip>     Check status
  logs <ip>       View logs
  ssh <ip>        SSH into instance

Examples:
  $0 up
  $0 provision 13.205.196.25
EOF
}

wait_for_ssh() {
    local ip=$1
    echo "Waiting for SSH..."
    for i in {1..30}; do
        ssh -i "$SSH_KEY" -o ConnectTimeout=5 -o StrictHostKeyChecking=no "ec2-user@$ip" "echo ready" 2>/dev/null && return 0
        sleep 10
    done
    echo -e "${RED}SSH timeout${NC}"; return 1
}

# install_docker installs and enables Docker and Docker Compose on the remote EC2 instance if they are unavailable.
install_docker() {
    local ip=$1
    echo "Installing Docker..."
    ssh -i "$SSH_KEY" "ec2-user@$ip" '
        command -v docker || { sudo dnf install -y docker && sudo systemctl enable --now docker && sudo usermod -aG docker ec2-user; }
        command -v docker-compose || { sudo curl -sL "https://github.com/docker/compose/releases/latest/download/docker-compose-linux-x86_64" -o /usr/local/bin/docker-compose && sudo chmod +x /usr/local/bin/docker-compose; }
    '
}

# copy_files prepares the remote telemetry directories and copies production configuration, Envoy, Compose, and available dashboard files to the EC2 host.
copy_files() {
    local ip=$1
    echo "Copying files..."
    ssh -i "$SSH_KEY" "ec2-user@$ip" "sudo mkdir -p /opt/telemetry/{config,envoy,dashboards} && sudo chown -R ec2-user:ec2-user /opt/telemetry"
    scp -i "$SSH_KEY" -r "$PROD_DIR/config/"* "ec2-user@$ip:/opt/telemetry/config/"
    scp -i "$SSH_KEY" -r "$PROD_DIR/envoy/"* "ec2-user@$ip:/opt/telemetry/envoy/"
    scp -i "$SSH_KEY" "$PROD_DIR/docker-compose.prod.yaml" "ec2-user@$ip:/opt/telemetry/"
    [ -d "$OTEL_DIR/dashboards" ] && scp -i "$SSH_KEY" -r "$OTEL_DIR/dashboards/"* "ec2-user@$ip:/opt/telemetry/dashboards/" 2>/dev/null || true
}

# create_env_file writes deployment credentials, service domains, and the OTLP authentication token to the remote telemetry environment file.
create_env_file() {
    local ip=$1
    ssh -i "$SSH_KEY" "ec2-user@$ip" "cat > /opt/telemetry/.env << EOF
GRAFANA_ADMIN_USER=$GRAFANA_USER
GRAFANA_ADMIN_PASSWORD=$GRAFANA_PASS
GRAFANA_DASHBOARD_DOMAIN=$GRAFANA_DOMAIN
OTEL_API_DOMAIN=$OTEL_DOMAIN
OTLP_API_AUTH_TOKEN=$OTLP_API_AUTH_TOKEN
EOF"
}

# start_services starts the telemetry services on the remote host and displays their container status.
start_services() {
    local ip=$1
    echo "Starting services..."
    ssh -i "$SSH_KEY" "ec2-user@$ip" "cd /opt/telemetry && sudo docker-compose -f docker-compose.prod.yaml up -d --build"
    sleep 10
    ssh -i "$SSH_KEY" "ec2-user@$ip" "sudo docker ps --format 'table {{.Names}}\t{{.Status}}'"
}

print_summary() {
    local ip=$1
    echo -e "
${GREEN}=== Deployment Complete ===${NC}
EC2 IP: $ip

Dashboard: https://$GRAFANA_DOMAIN ($GRAFANA_USER / $GRAFANA_PASS)
OTLP:      https://$OTEL_DOMAIN/v1/traces
Token:     $OTLP_API_AUTH_TOKEN

SSH: ssh -i $SSH_KEY ec2-user@$ip
"
}

# cmd_provision provisions the telemetry services on the specified host and displays the deployment summary.
cmd_provision() {
    local ip=$1
    [ -z "$ip" ] && { usage; exit 1; }
    echo -e "${GREEN}Provisioning $ip${NC}"
    wait_for_ssh "$ip"
    install_docker "$ip"
    copy_files "$ip"
    create_env_file "$ip"
    start_services "$ip"
    print_summary "$ip"
}

# cmd_deploy updates files and restarts the telemetry services on the specified host.
cmd_deploy() {
    local ip=$1
    [ -z "$ip" ] && { usage; exit 1; }
    echo -e "${GREEN}Deploying to $ip${NC}"
    copy_files "$ip"
    create_env_file "$ip"
    ssh -i "$SSH_KEY" "ec2-user@$ip" "cd /opt/telemetry && sudo docker-compose -f docker-compose.prod.yaml up -d --build"
    sleep 5
    ssh -i "$SSH_KEY" "ec2-user@$ip" "sudo docker ps --format 'table {{.Names}}\t{{.Status}}'"
}

# cmd_status displays the status of running telemetry containers on the specified host.
cmd_status() {
    local ip=$1
    [ -z "$ip" ] && { usage; exit 1; }
    ssh -i "$SSH_KEY" "ec2-user@$ip" "sudo docker ps --format 'table {{.Names}}\t{{.Status}}'"
}

# cmd_logs follows logs for a specified service on the remote telemetry host, or for all Compose services when no service is provided.
cmd_logs() {
    local ip=$1; local svc=${2:-}
    [ -z "$ip" ] && { usage; exit 1; }
    [ -n "$svc" ] && ssh -i "$SSH_KEY" "ec2-user@$ip" "sudo docker logs -f $svc" || ssh -i "$SSH_KEY" "ec2-user@$ip" "cd /opt/telemetry && sudo docker-compose -f docker-compose.prod.yaml logs -f"
}

# cmd_ssh opens an SSH session to the specified EC2 instance.
cmd_ssh() {
    local ip=$1
    [ -z "$ip" ] && { usage; exit 1; }
    ssh -i "$SSH_KEY" "ec2-user@$ip"
}

cmd_up() {
    echo -e "${GREEN}=== Full Deployment (EC2 + CloudFront + ACM) ===${NC}"
    cd "$PROD_DIR/terraform"
    terraform init -input=false

    echo -e "${BLUE}Creating infrastructure...${NC}"
    terraform apply -auto-approve \
        -var="route53_zone_id=$ROUTE53_ZONE_ID" \
        -var="domain_name=$GRAFANA_DOMAIN" \
        -var="otel_domain_name=$OTEL_DOMAIN"

    local ip=$(terraform output -raw public_ip)
    [ -z "$ip" ] && { echo -e "${RED}Failed to get IP${NC}"; exit 1; }
    echo -e "${GREEN}EC2: $ip${NC}"

    cd "$PROD_DIR"
    wait_for_ssh "$ip"
    install_docker "$ip"
    copy_files "$ip"
    create_env_file "$ip"
    start_services "$ip"
    print_summary "$ip"

    echo -e "${GREEN}CloudFront URLs:${NC}"
    cd "$PROD_DIR/terraform"
    echo "  Dashboard: https://$GRAFANA_DOMAIN"
    echo "  OTLP:      https://$OTEL_DOMAIN/v1/traces"
}

cmd_down() {
    echo -e "${YELLOW}Destroying infrastructure...${NC}"
    cd "$PROD_DIR/terraform"
    terraform destroy -auto-approve \
        -var="route53_zone_id=$ROUTE53_ZONE_ID" \
        -var="domain_name=$GRAFANA_DOMAIN" \
        -var="otel_domain_name=$OTEL_DOMAIN"
    echo -e "${GREEN}Done${NC}"
}

case "${1:-}" in
    up)        cmd_up ;;
    down)      cmd_down ;;
    provision) cmd_provision "$2" ;;
    deploy)    cmd_deploy "$2" ;;
    status)    cmd_status "$2" ;;
    logs)      cmd_logs "$2" "$3" ;;
    ssh)       cmd_ssh "$2" ;;
    *)         usage ;;
esac
