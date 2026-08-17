{
  description = "Telemetry Telemetry - Production Deployment";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    let
      # NixOS configurations for different targets
      nixosConfigurations = {
        # EC2 deployment target
        telemetry-ec2 = nixpkgs.lib.nixosSystem {
          system = "x86_64-linux";
          modules = [
            "${nixpkgs}/nixos/modules/virtualisation/amazon-image.nix"
            ./nix/modules/telemetry-telemetry.nix
            ({ config, pkgs, ... }: {
              ec2.hvm = true;
              system.stateVersion = "24.05";

              services.telemetry-telemetry.enable = true;

              environment.systemPackages = with pkgs; [
                vim htop curl jq openssl certbot
              ];

              services.openssh.enable = true;
              nix.settings.experimental-features = [ "nix-command" "flakes" ];
              security.sudo.wheelNeedsPassword = false;
            })
          ];
        };
      };
    in
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = nixpkgs.legacyPackages.${system};

        # Common dependencies for all scripts
        commonDeps = with pkgs; [
          coreutils
          bash
          openssl
          gnused
          gnugrep
          curl
          jq
        ];

        # AWS/EC2 deployment dependencies
        awsDeps = with pkgs; [
          awscli2
          terraform
          openssh
        ];

        # Docker deployment dependencies
        dockerDeps = with pkgs; [
          docker
          docker-compose
        ];

        # Kubernetes/EKS deployment dependencies
        k8sDeps = with pkgs; [
          kubectl
          kubernetes-helm
        ];

        # All dependencies combined
        allDeps = commonDeps ++ awsDeps ++ dockerDeps ++ k8sDeps;

        # Deploy script
        deployScript = pkgs.writeShellScriptBin "telemetry-deploy" ''
          set -e

          # Load .env if exists
          if [ -f ".env" ]; then
            set -a; source .env; set +a
          fi

          SSH_KEY="''${SSH_KEY_PATH:-~/.ssh/id_ed25519}"
          DOMAIN="''${DOMAIN:-telemetry.example.com}"
          OTEL_DOMAIN="''${OTEL_DOMAIN:-otel.example.com}"
          GRAFANA_USER="''${GRAFANA_ADMIN_USER:-admin}"
          GRAFANA_PASS="''${GRAFANA_ADMIN_PASSWORD:-changeme}"
          ACME_EMAIL="''${ACME_EMAIL:-}"

          if [ -z "''${OTLP_AUTH_TOKEN:-}" ]; then
            OTLP_AUTH_TOKEN=$(${pkgs.openssl}/bin/openssl rand -hex 32)
            echo "Generated OTLP token: $OTLP_AUTH_TOKEN"
          fi

          usage() {
            echo "Telemetry Telemetry NixOS Deployment"
            echo ""
            echo "Usage: telemetry-deploy <command> <ip-address>"
            echo ""
            echo "Commands:"
            echo "  provision <ip>  - Install NixOS and deploy (fresh instance)"
            echo "  deploy <ip>     - Deploy/update to existing NixOS instance"
            echo "  status <ip>     - Check service status"
            echo "  logs <ip>       - View container logs"
            echo "  ssh <ip>        - SSH into instance"
            echo "  certs <ip>      - Setup Let's Encrypt certificates"
            echo ""
            echo "Set configuration in .env file or environment variables"
          }

          wait_ssh() {
            echo "Waiting for SSH..."
            for i in {1..30}; do
              if ${pkgs.openssh}/bin/ssh -i "$SSH_KEY" -o ConnectTimeout=5 -o StrictHostKeyChecking=no "root@$1" "echo ok" 2>/dev/null; then
                return 0
              fi
              sleep 10
            done
            echo "SSH timeout"; exit 1
          }

          provision() {
            local ip=$1
            echo "=== Provisioning NixOS on $ip ==="
            wait_ssh "$ip"

            if ${pkgs.openssh}/bin/ssh -i "$SSH_KEY" "root@$ip" "test -f /etc/NIXOS" 2>/dev/null; then
              echo "Already NixOS, deploying..."
            else
              echo "Installing NixOS via nixos-infect..."
              ${pkgs.openssh}/bin/ssh -i "$SSH_KEY" "root@$ip" \
                "curl -sL https://raw.githubusercontent.com/elitak/nixos-infect/master/nixos-infect | NIX_CHANNEL=nixos-24.05 bash -x"
              sleep 60
              wait_ssh "$ip"
            fi

            deploy "$ip"
          }

          deploy() {
            local ip=$1
            echo "=== Deploying to $ip ==="

            # Copy module
            ${pkgs.openssh}/bin/ssh -i "$SSH_KEY" "root@$ip" "mkdir -p /etc/nixos/modules /var/lib/telemetry/config"
            ${pkgs.openssh}/bin/scp -i "$SSH_KEY" -r ./nix/modules/* "root@$ip:/etc/nixos/modules/"
            ${pkgs.openssh}/bin/scp -i "$SSH_KEY" -r ./config/* "root@$ip:/var/lib/telemetry/config/"

            # Generate configuration
            ${pkgs.openssh}/bin/ssh -i "$SSH_KEY" "root@$ip" "cat > /etc/nixos/configuration.nix" << EOF
          { config, pkgs, modulesPath, ... }:
          {
            imports = [
              "\''${modulesPath}/virtualisation/amazon-image.nix"
              ./modules/telemetry-telemetry.nix
            ];

            ec2.hvm = true;
            system.stateVersion = "24.05";

            services.telemetry-telemetry = {
              enable = true;
              domain = "$DOMAIN";
              otelDomain = "$OTEL_DOMAIN";
              grafanaAdminUser = "$GRAFANA_USER";
              $([ -n "$ACME_EMAIL" ] && echo "acmeEmail = \"$ACME_EMAIL\";")
            };

            environment.systemPackages = with pkgs; [ vim htop curl jq openssl certbot ];
            services.openssh.enable = true;
            nix.settings.experimental-features = [ "nix-command" "flakes" ];
            security.sudo.wheelNeedsPassword = false;

            users.users.root.openssh.authorizedKeys.keys = [
              "$(cat ''${SSH_KEY}.pub 2>/dev/null || echo "")"
            ];
          }
          EOF

            # Create secrets
            echo "$GRAFANA_PASS" | ${pkgs.openssh}/bin/ssh -i "$SSH_KEY" "root@$ip" "cat > /var/lib/telemetry/grafana-password"
            echo "$OTLP_AUTH_TOKEN" | ${pkgs.openssh}/bin/ssh -i "$SSH_KEY" "root@$ip" "cat > /var/lib/telemetry/otlp-token"

            # Rebuild
            echo "Rebuilding NixOS..."
            ${pkgs.openssh}/bin/ssh -i "$SSH_KEY" "root@$ip" "nixos-rebuild switch"

            echo ""
            echo "=== Deployment Complete ==="
            echo "Dashboard: https://$DOMAIN"
            echo "OTLP: https://$OTEL_DOMAIN/v1/traces"
            echo "Token: $OTLP_AUTH_TOKEN"
            echo ""
            echo "DNS: $DOMAIN -> $ip"
            echo "DNS: $OTEL_DOMAIN -> $ip"
          }

          status() {
            ${pkgs.openssh}/bin/ssh -i "$SSH_KEY" "root@$1" "podman ps; echo; systemctl status 'podman-*' --no-pager || true"
          }

          logs() {
            ${pkgs.openssh}/bin/ssh -i "$SSH_KEY" "root@$1" "journalctl -f -u 'podman-*'"
          }

          certs() {
            [ -z "$ACME_EMAIL" ] && { echo "Set ACME_EMAIL"; exit 1; }
            ${pkgs.openssh}/bin/ssh -i "$SSH_KEY" "root@$1" "
              systemctl stop podman-envoy || true
              certbot certonly --standalone -n --agree-tos -m $ACME_EMAIL -d $DOMAIN -d $OTEL_DOMAIN
              cp /etc/letsencrypt/live/$DOMAIN/*.pem /var/lib/telemetry/certs/
              systemctl start podman-envoy
            "
          }

          case "''${1:-}" in
            provision) [ -z "''${2:-}" ] && { usage; exit 1; }; provision "$2" ;;
            deploy)    [ -z "''${2:-}" ] && { usage; exit 1; }; deploy "$2" ;;
            status)    [ -z "''${2:-}" ] && { usage; exit 1; }; status "$2" ;;
            logs)      [ -z "''${2:-}" ] && { usage; exit 1; }; logs "$2" ;;
            ssh)       [ -z "''${2:-}" ] && { usage; exit 1; }; ${pkgs.openssh}/bin/ssh -i "$SSH_KEY" "root@$2" ;;
            certs)     [ -z "''${2:-}" ] && { usage; exit 1; }; certs "$2" ;;
            *) usage ;;
          esac
        '';

      in {
        # Development shell with all tools
        devShells.default = pkgs.mkShell {
          name = "telemetry-telemetry";
          buildInputs = allDeps ++ [ deployScript ];

          shellHook = ''
            echo "🚀 Telemetry Telemetry Development Environment"
            echo ""
            echo "Unified Deploy (recommended):"
            echo "  ./scripts/deploy.sh provision <ip>  - Full EC2 deployment"
            echo "  ./scripts/deploy.sh deploy <ip>     - Update deployment"
            echo "  ./scripts/deploy.sh status <ip>     - Check status"
            echo "  ./scripts/deploy.sh logs <ip>       - View logs"
            echo "  ./scripts/deploy.sh ssh <ip>        - SSH into instance"
            echo ""
            echo "Other Scripts:"
            echo "  ./scripts/deploy-docker.sh   - Local Docker deployment"
            echo "  ./scripts/setup-otel-endpoint.sh - Generate OTLP token"
            echo ""
            echo "Terraform:"
            echo "  cd terraform && terraform apply"
            echo ""
            echo "Configuration: cp .env.example .env && nano .env"
            echo ""

            export PATH="$PWD/scripts:$PATH"

            # Aliases
            alias deploy="./scripts/deploy.sh"
            alias tf="terraform"
          '';
        };

        # Minimal shell for Docker-only deployment
        devShells.docker = pkgs.mkShell {
          name = "telemetry-docker";
          buildInputs = commonDeps ++ dockerDeps;
          shellHook = ''
            echo "🐳 Telemetry Telemetry (Docker only)"
            export PATH="$PWD/scripts:$PATH"
          '';
        };

        # Shell for AWS deployments
        devShells.aws = pkgs.mkShell {
          name = "telemetry-aws";
          buildInputs = commonDeps ++ awsDeps ++ dockerDeps ++ [ deployScript ];
          shellHook = ''
            echo "☁️  Telemetry Telemetry (AWS)"
            export PATH="$PWD/scripts:$PATH"
          '';
        };

        # Packaged deploy command
        packages.default = deployScript;
        packages.deploy = deployScript;

        # Apps
        apps.default = {
          type = "app";
          program = "${deployScript}/bin/telemetry-deploy";
        };
        apps.deploy = self.apps.${system}.default;
      }
    ) // {
      # NixOS configurations (outside eachDefaultSystem)
      inherit nixosConfigurations;
    };
}
