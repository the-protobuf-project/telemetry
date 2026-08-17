{ config, lib, pkgs, ... }:

# Deploy script as a Nix derivation
# Usage: nix run .#deploy -- <ip-address>

let
  deployScript = pkgs.writeShellScriptBin "telemetry-deploy" ''
    set -e

    # Colors
    RED='\033[0;31m'
    GREEN='\033[0;32m'
    YELLOW='\033[1;33m'
    NC='\033[0m'

    SCRIPT_DIR="$(cd "$(dirname "''${BASH_SOURCE[0]}")" && pwd)"

    # Load .env if exists
    if [ -f ".env" ]; then
      source .env
    fi

    # Configuration from environment
    SSH_KEY="''${SSH_KEY_PATH:-~/.ssh/id_ed25519}"
    DOMAIN="''${DOMAIN:-telemetry.example.com}"
    OTEL_DOMAIN="''${OTEL_DOMAIN:-otel.example.com}"
    GRAFANA_USER="''${GRAFANA_ADMIN_USER:-admin}"
    GRAFANA_PASS="''${GRAFANA_ADMIN_PASSWORD:-changeme}"
    ACME_EMAIL="''${ACME_EMAIL:-}"

    # Generate OTLP token if not set
    if [ -z "''${OTLP_AUTH_TOKEN:-}" ]; then
      OTLP_AUTH_TOKEN=$(${pkgs.openssl}/bin/openssl rand -hex 32)
      echo -e "''${YELLOW}Generated OTLP auth token: $OTLP_AUTH_TOKEN''${NC}"
    fi

    usage() {
      echo "Usage: telemetry-deploy <command> [options]"
      echo ""
      echo "Commands:"
      echo "  provision <ip>   - Initial setup of a fresh EC2 instance"
      echo "  deploy <ip>      - Deploy/update configuration to instance"
      echo "  status <ip>      - Check service status"
      echo "  logs <ip> [svc]  - View logs (optional: service name)"
      echo "  ssh <ip>         - SSH into instance"
      echo "  certs <ip>       - Setup Let's Encrypt certificates"
      echo ""
      echo "Environment variables (or .env file):"
      echo "  SSH_KEY_PATH     - Path to SSH private key"
      echo "  DOMAIN           - Grafana domain"
      echo "  OTEL_DOMAIN      - OTLP endpoint domain"
      echo "  GRAFANA_ADMIN_USER"
      echo "  GRAFANA_ADMIN_PASSWORD"
      echo "  OTLP_AUTH_TOKEN"
      echo "  ACME_EMAIL       - Email for Let's Encrypt"
    }

    wait_for_ssh() {
      local ip=$1
      echo "Waiting for SSH to be available..."
      for i in {1..30}; do
        if ${pkgs.openssh}/bin/ssh -i "$SSH_KEY" -o ConnectTimeout=5 -o StrictHostKeyChecking=no "root@$ip" "echo ready" 2>/dev/null; then
          echo -e "''${GREEN}SSH is ready!''${NC}"
          return 0
        fi
        echo "  Waiting... ($i/30)"
        sleep 10
      done
      echo -e "''${RED}Timeout waiting for SSH''${NC}"
      return 1
    }

    provision() {
      local ip=$1
      echo -e "''${GREEN}=== Provisioning NixOS on $ip ===${NC}"

      wait_for_ssh "$ip"

      # Check if already NixOS
      if ${pkgs.openssh}/bin/ssh -i "$SSH_KEY" "root@$ip" "test -f /etc/NIXOS" 2>/dev/null; then
        echo "Instance is already running NixOS"
      else
        echo -e "''${YELLOW}Installing NixOS...''${NC}"
        # Use nixos-infect for Amazon Linux -> NixOS conversion
        ${pkgs.openssh}/bin/ssh -i "$SSH_KEY" "root@$ip" "curl https://raw.githubusercontent.com/elitak/nixos-infect/master/nixos-infect | NIX_CHANNEL=nixos-24.05 bash -x"

        echo "Waiting for reboot..."
        sleep 30
        wait_for_ssh "$ip"
      fi

      deploy "$ip"
    }

    deploy() {
      local ip=$1
      echo -e "''${GREEN}=== Deploying Telemetry Telemetry to $ip ===${NC}"

      # Create configuration
      local config_dir=$(mktemp -d)
      trap "rm -rf $config_dir" EXIT

      # Generate NixOS configuration
      cat > "$config_dir/configuration.nix" << EOF
    { config, pkgs, ... }:
    {
      imports = [ ./hardware-configuration.nix ];

      services.telemetry-telemetry = {
        enable = true;
        domain = "$DOMAIN";
        otelDomain = "$OTEL_DOMAIN";
        grafanaAdminUser = "$GRAFANA_USER";
        $([ -n "$ACME_EMAIL" ] && echo "acmeEmail = \"$ACME_EMAIL\";")
      };

      # SSH keys
      users.users.root.openssh.authorizedKeys.keys = [
        "$(cat ''${SSH_KEY}.pub 2>/dev/null || echo "")"
      ];

      system.stateVersion = "24.05";
    }
    EOF

      # Copy module
      ${pkgs.openssh}/bin/scp -i "$SSH_KEY" -r ./nix/modules "root@$ip:/etc/nixos/"
      ${pkgs.openssh}/bin/scp -i "$SSH_KEY" "$config_dir/configuration.nix" "root@$ip:/etc/nixos/"

      # Copy config files
      ${pkgs.openssh}/bin/ssh -i "$SSH_KEY" "root@$ip" "mkdir -p /var/lib/telemetry/config"
      ${pkgs.openssh}/bin/scp -i "$SSH_KEY" -r ./config/* "root@$ip:/var/lib/telemetry/config/"

      # Create secrets
      echo "$GRAFANA_PASS" | ${pkgs.openssh}/bin/ssh -i "$SSH_KEY" "root@$ip" "cat > /var/lib/telemetry/grafana-password"
      echo "$OTLP_AUTH_TOKEN" | ${pkgs.openssh}/bin/ssh -i "$SSH_KEY" "root@$ip" "cat > /var/lib/telemetry/otlp-token"

      # Rebuild NixOS
      echo "Rebuilding NixOS configuration..."
      ${pkgs.openssh}/bin/ssh -i "$SSH_KEY" "root@$ip" "nixos-rebuild switch"

      echo ""
      echo -e "''${GREEN}=== Deployment Complete ===${NC}"
      echo "Public IP: $ip"
      echo ""
      echo "Access:"
      echo "  Dashboard: https://$DOMAIN"
      echo "  Credentials: $GRAFANA_USER / $GRAFANA_PASS"
      echo ""
      echo "OTLP Endpoints:"
      echo "  HTTPS: https://$OTEL_DOMAIN/v1/traces"
      echo "  gRPC: $ip:4317"
      echo "  HTTP: $ip:4318"
      echo ""
      echo "OTLP Authentication:"
      echo "  Token: $OTLP_AUTH_TOKEN"
      echo "  Header: Authorization: Bearer $OTLP_AUTH_TOKEN"
      echo ""
      echo "DNS: Create A records:"
      echo "  $DOMAIN -> $ip"
      echo "  $OTEL_DOMAIN -> $ip"
    }

    status() {
      local ip=$1
      echo -e "''${GREEN}=== Service Status on $ip ===${NC}"
      ${pkgs.openssh}/bin/ssh -i "$SSH_KEY" "root@$ip" "systemctl status 'podman-*' --no-pager || true"
      echo ""
      ${pkgs.openssh}/bin/ssh -i "$SSH_KEY" "root@$ip" "podman ps"
    }

    logs() {
      local ip=$1
      local svc=''${2:-}
      if [ -n "$svc" ]; then
        ${pkgs.openssh}/bin/ssh -i "$SSH_KEY" "root@$ip" "podman logs -f $svc"
      else
        ${pkgs.openssh}/bin/ssh -i "$SSH_KEY" "root@$ip" "journalctl -f -u 'podman-*'"
      fi
    }

    setup_certs() {
      local ip=$1
      echo -e "''${GREEN}=== Setting up Let's Encrypt certificates ===${NC}"

      if [ -z "$ACME_EMAIL" ]; then
        echo -e "''${RED}Error: ACME_EMAIL not set''${NC}"
        exit 1
      fi

      ${pkgs.openssh}/bin/ssh -i "$SSH_KEY" "root@$ip" "
        systemctl stop podman-envoy || true
        certbot certonly --standalone --non-interactive --agree-tos \
          --email $ACME_EMAIL \
          -d $DOMAIN \
          -d $OTEL_DOMAIN
        cp /etc/letsencrypt/live/$DOMAIN/fullchain.pem /var/lib/telemetry/certs/
        cp /etc/letsencrypt/live/$DOMAIN/privkey.pem /var/lib/telemetry/certs/
        systemctl start podman-envoy
      "

      echo -e "''${GREEN}Certificates installed!''${NC}"
    }

    # Main
    case "''${1:-}" in
      provision)
        [ -z "''${2:-}" ] && { echo "Error: IP address required"; usage; exit 1; }
        provision "$2"
        ;;
      deploy)
        [ -z "''${2:-}" ] && { echo "Error: IP address required"; usage; exit 1; }
        deploy "$2"
        ;;
      status)
        [ -z "''${2:-}" ] && { echo "Error: IP address required"; usage; exit 1; }
        status "$2"
        ;;
      logs)
        [ -z "''${2:-}" ] && { echo "Error: IP address required"; usage; exit 1; }
        logs "$2" "''${3:-}"
        ;;
      ssh)
        [ -z "''${2:-}" ] && { echo "Error: IP address required"; usage; exit 1; }
        ${pkgs.openssh}/bin/ssh -i "$SSH_KEY" "root@$2"
        ;;
      certs)
        [ -z "''${2:-}" ] && { echo "Error: IP address required"; usage; exit 1; }
        setup_certs "$2"
        ;;
      *)
        usage
        exit 1
        ;;
    esac
  '';

in deployScript
