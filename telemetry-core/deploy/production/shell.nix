# Nix shell for users without flakes enabled
# Usage: nix-shell
{ pkgs ? import <nixpkgs> {} }:

pkgs.mkShell {
  name = "telemetry-telemetry";

  buildInputs = with pkgs; [
    # Core utilities
    coreutils
    bash
    openssl
    gnused
    gnugrep
    curl
    jq

    # AWS/EC2 deployment
    awscli2
    terraform
    openssh

    # Docker deployment
    docker
    docker-compose

    # Kubernetes/EKS deployment
    kubectl
    kubernetes-helm
  ];

  shellHook = ''
    echo "🚀 Telemetry Telemetry Development Environment"
    echo ""
    echo "Available scripts:"
    echo "  ./scripts/deploy-docker.sh   - Deploy locally with Docker"
    echo "  ./scripts/deploy-ec2.sh      - Deploy to AWS EC2"
    echo "  ./scripts/deploy-eks.sh      - Deploy to AWS EKS"
    echo "  ./scripts/redeploy-ec2.sh    - Update EC2 deployment"
    echo "  ./scripts/destroy-ec2.sh     - Destroy EC2 infrastructure"
    echo "  ./scripts/setup-otel-endpoint.sh - Generate OTLP token"
    echo ""

    # Add scripts to PATH
    export PATH="$PWD/scripts:$PATH"
  '';
}
