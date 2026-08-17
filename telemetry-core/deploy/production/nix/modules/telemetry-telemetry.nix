{ config, lib, pkgs, ... }:

with lib;

let
  cfg = config.services.telemetry-telemetry;

  # OCI container images
  images = {
    grafana = "grafana/grafana:11.0.0";
    loki = "grafana/loki:3.0.0";
    tempo = "grafana/tempo:2.4.1";
    prometheus = "prom/prometheus:v2.51.0";
    alertmanager = "prom/alertmanager:v0.27.0";
    otelcol = "otel/opentelemetry-collector-contrib:0.96.0";
    pyroscope = "grafana/pyroscope:1.5.0";
    envoy = "envoyproxy/envoy:v1.29.2";
  };

in {
  options.services.telemetry-telemetry = {
    enable = mkEnableOption "Telemetry Telemetry observability stack";

    domain = mkOption {
      type = types.str;
      default = "telemetry.example.com";
      description = "Domain for Grafana dashboard";
    };

    otelDomain = mkOption {
      type = types.str;
      default = "otel.example.com";
      description = "Domain for OTLP ingestion endpoint";
    };

    grafanaAdminUser = mkOption {
      type = types.str;
      default = "admin";
      description = "Grafana admin username";
    };

    grafanaAdminPasswordFile = mkOption {
      type = types.nullOr types.path;
      default = null;
      description = "Path to file containing Grafana admin password";
    };

    otlpAuthTokenFile = mkOption {
      type = types.nullOr types.path;
      default = null;
      description = "Path to file containing OTLP authentication token";
    };

    tlsCertFile = mkOption {
      type = types.nullOr types.path;
      default = null;
      description = "Path to TLS certificate file";
    };

    tlsKeyFile = mkOption {
      type = types.nullOr types.path;
      default = null;
      description = "Path to TLS private key file";
    };

    acmeEmail = mkOption {
      type = types.nullOr types.str;
      default = null;
      description = "Email for Let's Encrypt ACME registration";
    };

    dataDir = mkOption {
      type = types.path;
      default = "/var/lib/telemetry";
      description = "Directory for persistent data";
    };

    openFirewall = mkOption {
      type = types.bool;
      default = true;
      description = "Open firewall ports for HTTP/HTTPS and OTLP";
    };
  };

  config = mkIf cfg.enable {
    # Create data directories
    systemd.tmpfiles.rules = [
      "d ${cfg.dataDir} 0755 root root -"
      "d ${cfg.dataDir}/grafana 0755 472 472 -"
      "d ${cfg.dataDir}/loki 0755 10001 10001 -"
      "d ${cfg.dataDir}/tempo 0755 10001 10001 -"
      "d ${cfg.dataDir}/prometheus 0755 65534 65534 -"
      "d ${cfg.dataDir}/pyroscope 0755 10001 10001 -"
      "d ${cfg.dataDir}/certs 0755 root root -"
      "d ${cfg.dataDir}/config 0755 root root -"
    ];

    # ACME/Let's Encrypt certificates
    security.acme = mkIf (cfg.acmeEmail != null) {
      acceptTerms = true;
      defaults.email = cfg.acmeEmail;
      certs.${cfg.domain} = {
        extraDomainNames = [ cfg.otelDomain ];
        group = "nginx";
      };
    };

    # Firewall
    networking.firewall = mkIf cfg.openFirewall {
      allowedTCPPorts = [ 80 443 4317 4318 ];
    };

    # Docker/Podman
    virtualisation.podman = {
      enable = true;
      dockerCompat = true;
      defaultNetwork.settings.dns_enabled = true;
    };

    # Create network
    systemd.services.telemetry-network = {
      description = "Create Telemetry Telemetry network";
      wantedBy = [ "multi-user.target" ];
      before = [
        "podman-grafana.service"
        "podman-loki.service"
        "podman-tempo.service"
        "podman-prometheus.service"
        "podman-otelcol.service"
        "podman-envoy.service"
      ];
      serviceConfig = {
        Type = "oneshot";
        RemainAfterExit = true;
        ExecStart = "${pkgs.podman}/bin/podman network create telemetry-net || true";
        ExecStop = "${pkgs.podman}/bin/podman network rm telemetry-net || true";
      };
    };

    # Loki
    virtualisation.oci-containers.containers.loki = {
      image = images.loki;
      autoStart = true;
      extraOptions = [ "--network=telemetry-net" ];
      volumes = [
        "${cfg.dataDir}/loki:/loki"
        "${cfg.dataDir}/config/loki.yaml:/etc/loki/local-config.yaml:ro"
      ];
      cmd = [ "-config.file=/etc/loki/local-config.yaml" ];
    };

    # Tempo
    virtualisation.oci-containers.containers.tempo = {
      image = images.tempo;
      autoStart = true;
      extraOptions = [ "--network=telemetry-net" ];
      volumes = [
        "${cfg.dataDir}/tempo:/var/tempo"
        "${cfg.dataDir}/config/tempo.yaml:/etc/tempo/tempo.yaml:ro"
      ];
      cmd = [ "-config.file=/etc/tempo/tempo.yaml" ];
    };

    # Prometheus
    virtualisation.oci-containers.containers.prometheus = {
      image = images.prometheus;
      autoStart = true;
      extraOptions = [ "--network=telemetry-net" ];
      volumes = [
        "${cfg.dataDir}/prometheus:/prometheus"
        "${cfg.dataDir}/config/prometheus.yaml:/etc/prometheus/prometheus.yml:ro"
      ];
      cmd = [
        "--config.file=/etc/prometheus/prometheus.yml"
        "--storage.tsdb.path=/prometheus"
        "--web.enable-remote-write-receiver"
      ];
    };

    # Pyroscope
    virtualisation.oci-containers.containers.pyroscope = {
      image = images.pyroscope;
      autoStart = true;
      extraOptions = [ "--network=telemetry-net" ];
      volumes = [
        "${cfg.dataDir}/pyroscope:/var/lib/pyroscope"
      ];
    };

    # Alertmanager
    virtualisation.oci-containers.containers.alertmanager = {
      image = images.alertmanager;
      autoStart = true;
      extraOptions = [ "--network=telemetry-net" ];
      volumes = [
        "${cfg.dataDir}/config/alertmanager.yaml:/etc/alertmanager/alertmanager.yml:ro"
      ];
    };

    # OpenTelemetry Collector
    virtualisation.oci-containers.containers.otelcol = {
      image = images.otelcol;
      autoStart = true;
      extraOptions = [ "--network=telemetry-net" ];
      ports = [
        "4317:4317"
        "4318:4318"
      ];
      volumes = [
        "${cfg.dataDir}/config/otel-collector.yaml:/etc/otelcol/config.yaml:ro"
      ];
      environment = mkIf (cfg.otlpAuthTokenFile != null) {
        OTLP_AUTH_TOKEN = "$(cat ${cfg.otlpAuthTokenFile})";
      };
      cmd = [ "--config=/etc/otelcol/config.yaml" ];
    };

    # Grafana
    virtualisation.oci-containers.containers.grafana = {
      image = images.grafana;
      autoStart = true;
      extraOptions = [ "--network=telemetry-net" ];
      volumes = [
        "${cfg.dataDir}/grafana:/var/lib/grafana"
        "${cfg.dataDir}/config/grafana-datasources.yaml:/etc/grafana/provisioning/datasources/datasources.yaml:ro"
        "${cfg.dataDir}/config/grafana-dashboards.yaml:/etc/grafana/provisioning/dashboards/dashboards.yaml:ro"
      ];
      environment = {
        GF_SECURITY_ADMIN_USER = cfg.grafanaAdminUser;
        GF_SERVER_ROOT_URL = "https://${cfg.domain}";
        GF_SERVER_DOMAIN = cfg.domain;
      };
    };

    # Envoy Proxy
    virtualisation.oci-containers.containers.envoy = {
      image = images.envoy;
      autoStart = true;
      extraOptions = [ "--network=telemetry-net" ];
      ports = [
        "80:80"
        "443:443"
      ];
      volumes = [
        "${cfg.dataDir}/config/envoy.yaml:/etc/envoy/envoy.yaml:ro"
        "${cfg.dataDir}/certs:/etc/envoy/certs:ro"
      ];
    };

    # Nginx reverse proxy (alternative to Envoy, with ACME support)
    services.nginx = mkIf (cfg.acmeEmail != null) {
      enable = true;
      recommendedProxySettings = true;
      recommendedTlsSettings = true;

      virtualHosts.${cfg.domain} = {
        forceSSL = true;
        enableACME = true;
        locations."/" = {
          proxyPass = "http://127.0.0.1:3000";
          proxyWebsockets = true;
        };
      };

      virtualHosts.${cfg.otelDomain} = {
        forceSSL = true;
        useACMEHost = cfg.domain;
        locations."/v1/" = {
          proxyPass = "http://127.0.0.1:4318";
        };
        locations."/" = {
          return = "301 https://example.com";
        };
      };
    };
  };
}
