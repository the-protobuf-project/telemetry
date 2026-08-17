# EC2-only deployment for Telemetry Telemetry
# NO EKS = saves $73/month!
# Cost: t3.medium spot ~$10-15/month + EIP ~$3.60/month = ~$15-20/month

terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.92"
    }
  }
  required_version = ">= 1.2"
}

provider "aws" {
  region = var.aws_region
}

# CloudFront requires ACM certificates in us-east-1
provider "aws" {
  alias  = "us_east_1"
  region = "us-east-1"
}

variable "aws_region" {
  description = "AWS region"
  default     = "us-east-1"
}

variable "instance_type" {
  description = "EC2 instance type (t3.medium = 2 vCPU, 4GB RAM)"
  default     = "t3.medium"
}

variable "domain_name" {
  description = "Domain for telemetry dashboard"
  default     = "telemetry.example.com"
}

variable "otel_domain_name" {
  description = "Domain for OTLP ingestion endpoint"
  default     = "otel.example.com"
}

variable "route53_zone_id" {
  description = "Route53 hosted zone ID for DNS validation (leave empty to skip ACM)"
  default     = ""
}

variable "grafana_password" {
  description = "Grafana admin password"
  default     = "changeme"
  sensitive   = true
}

variable "vpc_id" {
  description = "VPC ID to deploy into (leave empty to use default VPC)"
  default     = ""
}

variable "subnet_id" {
  description = "Subnet ID to deploy into (leave empty to auto-select)"
  default     = ""
}

# Use specified VPC or default
data "aws_vpc" "selected" {
  id = var.vpc_id != "" ? var.vpc_id : null
  default = var.vpc_id == "" ? true : null
}

data "aws_subnets" "default" {
  filter {
    name   = "vpc-id"
    values = [data.aws_vpc.selected.id]
  }
}

locals {
  subnet_id = var.subnet_id != "" ? var.subnet_id : data.aws_subnets.default.ids[0]
}

# Security group
resource "aws_security_group" "telemetry" {
  name        = "telemetry-telemetry-sg"
  description = "Telemetry telemetry security group"
  vpc_id      = data.aws_vpc.selected.id

  # SSH
  ingress {
    description = "SSH"
    from_port   = 22
    to_port     = 22
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }

  # HTTP (redirect to HTTPS)
  ingress {
    description = "HTTP"
    from_port   = 80
    to_port     = 80
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }

  # HTTPS
  ingress {
    description = "HTTPS"
    from_port   = 443
    to_port     = 443
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }

  # OTLP gRPC
  ingress {
    description = "OTLP gRPC"
    from_port   = 4317
    to_port     = 4317
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }

  # OTLP HTTP
  ingress {
    description = "OTLP HTTP"
    from_port   = 4318
    to_port     = 4318
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }

  # Grafana direct access (backup)
  ingress {
    description = "Grafana"
    from_port   = 3000
    to_port     = 3000
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = {
    Name = "telemetry-telemetry"
  }
}

# Latest Amazon Linux 2023 AMI
data "aws_ami" "al2023" {
  most_recent = true
  owners      = ["amazon"]

  filter {
    name   = "name"
    values = ["al2023-ami-*-x86_64"]
  }

  filter {
    name   = "virtualization-type"
    values = ["hvm"]
  }
}

variable "ssh_public_key_content" {
  description = "SSH public key content for EC2 access (auto-read from .auth/ssh/id_ed25519.pub if empty)"
  type        = string
  default     = ""
}

locals {
  # Read SSH public key from .auth/ssh/ if not provided
  ssh_public_key = var.ssh_public_key_content != "" ? var.ssh_public_key_content : file("${path.module}/../.auth/ssh/id_ed25519.pub")
}

# SSH Key
resource "aws_key_pair" "telemetry" {
  key_name   = "telemetry-telemetry-key"
  public_key = local.ssh_public_key
}

# User data script - installs Docker (files copied via SSH after)
locals {
  user_data = <<-EOF
    #!/bin/bash
    set -ex

    # Update system
    dnf update -y

    # Install Docker
    dnf install -y docker git
    systemctl enable docker
    systemctl start docker
    usermod -aG docker ec2-user

    # Install Docker Compose
    curl -L "https://github.com/docker/compose/releases/latest/download/docker-compose-linux-x86_64" -o /usr/local/bin/docker-compose
    chmod +x /usr/local/bin/docker-compose
    ln -sf /usr/local/bin/docker-compose /usr/bin/docker-compose

    # Create telemetry directory structure
    mkdir -p /opt/telemetry/{config,certs,envoy,dashboards}

    # Generate self-signed certs (replace with Let's Encrypt later)
    openssl req -x509 -nodes -days 365 -newkey rsa:2048 \
      -keyout /opt/telemetry/certs/privkey.pem \
      -out /opt/telemetry/certs/fullchain.pem \
      -subj "/CN=${var.domain_name}"

    # Set permissions
    chown -R ec2-user:ec2-user /opt/telemetry

    echo "Setup complete! Copy files via SCP then run docker-compose"
  EOF
}

# Spot instance request for lowest cost
resource "aws_spot_instance_request" "telemetry" {
  ami                    = data.aws_ami.al2023.id
  instance_type          = var.instance_type
  key_name               = aws_key_pair.telemetry.key_name
  vpc_security_group_ids = [aws_security_group.telemetry.id]
  subnet_id              = local.subnet_id

  spot_type                      = "persistent"
  instance_interruption_behavior = "stop"
  wait_for_fulfillment           = true

  user_data = base64encode(local.user_data)

  root_block_device {
    volume_size           = 30
    volume_type           = "gp3"
    delete_on_termination = true
  }

  tags = {
    Name = "telemetry-telemetry"
  }
}

# Name tag for the spot instance
resource "aws_ec2_tag" "telemetry_name" {
  resource_id = aws_spot_instance_request.telemetry.spot_instance_id
  key         = "Name"
  value       = "telemetry-telemetry"
}

# Elastic IP for stable DNS pointing
resource "aws_eip" "telemetry" {
  domain = "vpc"

  tags = {
    Name = "telemetry-telemetry"
  }
}

# Associate EIP with spot instance
resource "aws_eip_association" "telemetry" {
  instance_id   = aws_spot_instance_request.telemetry.spot_instance_id
  allocation_id = aws_eip.telemetry.id
}

# Get instance details for public DNS
data "aws_instance" "telemetry" {
  instance_id = aws_spot_instance_request.telemetry.spot_instance_id
  depends_on  = [aws_eip_association.telemetry]
}

locals {
  # CloudFront needs a domain, not IP. Use EC2 public DNS or create a subdomain
  origin_domain = "origin.${var.domain_name}"
}

# Outputs
output "public_ip" {
  description = "Public IP address - point DNS here"
  value       = aws_eip.telemetry.public_ip
}

output "ssh_command" {
  description = "SSH into the instance"
  value       = "ssh ec2-user@${aws_eip.telemetry.public_ip}"
}

output "deploy_command" {
  description = "Run this after terraform apply to deploy services"
  value       = "cd .. && ./scripts/deploy-ec2.sh ${aws_eip.telemetry.public_ip}"
}

output "dashboard_url" {
  description = "Dashboard URL (after DNS is configured)"
  value       = "https://${var.domain_name}"
}

output "dns_instructions" {
  description = "DNS configuration"
  value       = "Create an A record: ${var.domain_name} -> ${aws_eip.telemetry.public_ip}"
}

output "monthly_cost_estimate" {
  description = "Estimated monthly cost"
  value       = "~$15-20/month (t3.medium spot ~$10-15 + EIP $3.60)"
}

# =============================================================================
# AWS Certificate Manager (ACM) - Optional, requires Route53 zone
# =============================================================================

# ACM Certificate for both domains (must be in us-east-1 for CloudFront)
resource "aws_acm_certificate" "telemetry" {
  count    = var.route53_zone_id != "" ? 1 : 0
  provider = aws.us_east_1

  domain_name               = var.domain_name
  subject_alternative_names = [var.otel_domain_name]
  validation_method         = "DNS"

  lifecycle {
    create_before_destroy = true
  }

  tags = {
    Name = "telemetry-telemetry"
  }
}

# Route53 DNS validation records
resource "aws_route53_record" "acm_validation" {
  for_each = var.route53_zone_id != "" ? {
    for dvo in aws_acm_certificate.telemetry[0].domain_validation_options : dvo.domain_name => {
      name   = dvo.resource_record_name
      record = dvo.resource_record_value
      type   = dvo.resource_record_type
    }
  } : {}

  allow_overwrite = true
  name            = each.value.name
  records         = [each.value.record]
  ttl             = 60
  type            = each.value.type
  zone_id         = var.route53_zone_id
}

# Wait for certificate validation
resource "aws_acm_certificate_validation" "telemetry" {
  count    = var.route53_zone_id != "" ? 1 : 0
  provider = aws.us_east_1

  certificate_arn         = aws_acm_certificate.telemetry[0].arn
  validation_record_fqdns = [for record in aws_route53_record.acm_validation : record.fqdn]
}

# =============================================================================
# CloudFront Distributions - TLS termination with ACM
# =============================================================================

# CloudFront distribution for Grafana dashboard (telemetry.the-protobuf-project.dev)
resource "aws_cloudfront_distribution" "telemetry" {
  count = var.route53_zone_id != "" ? 1 : 0

  enabled             = true
  is_ipv6_enabled     = true
  comment             = "Telemetry Telemetry Dashboard"
  default_root_object = ""
  aliases             = [var.domain_name]

  origin {
    domain_name = local.origin_domain
    origin_id   = "ec2-origin"

    custom_origin_config {
      http_port              = 80
      https_port             = 443
      origin_protocol_policy = "http-only"
      origin_ssl_protocols   = ["TLSv1.2"]
    }
  }

  default_cache_behavior {
    allowed_methods  = ["DELETE", "GET", "HEAD", "OPTIONS", "PATCH", "POST", "PUT"]
    cached_methods   = ["GET", "HEAD"]
    target_origin_id = "ec2-origin"

    forwarded_values {
      query_string = true
      headers      = ["Host", "Origin", "Authorization", "Accept", "Accept-Language"]

      cookies {
        forward = "all"
      }
    }

    viewer_protocol_policy = "redirect-to-https"
    min_ttl                = 0
    default_ttl            = 0
    max_ttl                = 0
    compress               = true
  }

  restrictions {
    geo_restriction {
      restriction_type = "none"
    }
  }

  viewer_certificate {
    acm_certificate_arn      = aws_acm_certificate_validation.telemetry[0].certificate_arn
    ssl_support_method       = "sni-only"
    minimum_protocol_version = "TLSv1.2_2021"
  }

  tags = {
    Name = "telemetry-telemetry"
  }

  depends_on = [aws_acm_certificate_validation.telemetry]
}

# CloudFront distribution for OTLP endpoint (otel.the-protobuf-project.dev)
resource "aws_cloudfront_distribution" "otel" {
  count = var.route53_zone_id != "" ? 1 : 0

  enabled         = true
  is_ipv6_enabled = true
  comment         = "Telemetry OTLP Ingestion Endpoint"
  aliases         = [var.otel_domain_name]

  origin {
    domain_name = local.origin_domain
    origin_id   = "ec2-otel-origin"

    custom_origin_config {
      http_port              = 80
      https_port             = 443
      origin_protocol_policy = "http-only"
      origin_ssl_protocols   = ["TLSv1.2"]
    }
  }

  default_cache_behavior {
    allowed_methods  = ["DELETE", "GET", "HEAD", "OPTIONS", "PATCH", "POST", "PUT"]
    cached_methods   = ["GET", "HEAD"]
    target_origin_id = "ec2-otel-origin"

    forwarded_values {
      query_string = true
      headers      = ["*"]

      cookies {
        forward = "all"
      }
    }

    viewer_protocol_policy = "https-only"
    min_ttl                = 0
    default_ttl            = 0
    max_ttl                = 0
    compress               = true
  }

  restrictions {
    geo_restriction {
      restriction_type = "none"
    }
  }

  viewer_certificate {
    acm_certificate_arn      = aws_acm_certificate_validation.telemetry[0].certificate_arn
    ssl_support_method       = "sni-only"
    minimum_protocol_version = "TLSv1.2_2021"
  }

  tags = {
    Name = "telemetry-otel"
  }

  depends_on = [aws_acm_certificate_validation.telemetry]
}

# =============================================================================
# Route53 Records
# =============================================================================

# Origin subdomain pointing to EC2 (for CloudFront to use)
resource "aws_route53_record" "origin" {
  count = var.route53_zone_id != "" ? 1 : 0

  zone_id = var.route53_zone_id
  name    = local.origin_domain
  type    = "A"
  ttl     = 300
  records = [aws_eip.telemetry.public_ip]
}

# Route53 A record for telemetry dashboard -> CloudFront
resource "aws_route53_record" "telemetry" {
  count = var.route53_zone_id != "" ? 1 : 0

  zone_id = var.route53_zone_id
  name    = var.domain_name
  type    = "A"

  alias {
    name                   = aws_cloudfront_distribution.telemetry[0].domain_name
    zone_id                = aws_cloudfront_distribution.telemetry[0].hosted_zone_id
    evaluate_target_health = false
  }
}

# Route53 A record for OTLP endpoint -> CloudFront
resource "aws_route53_record" "otel" {
  count = var.route53_zone_id != "" ? 1 : 0

  zone_id = var.route53_zone_id
  name    = var.otel_domain_name
  type    = "A"

  alias {
    name                   = aws_cloudfront_distribution.otel[0].domain_name
    zone_id                = aws_cloudfront_distribution.otel[0].hosted_zone_id
    evaluate_target_health = false
  }
}

# =============================================================================
# Outputs
# =============================================================================

output "acm_certificate_arn" {
  description = "ACM certificate ARN"
  value       = var.route53_zone_id != "" ? aws_acm_certificate.telemetry[0].arn : "N/A - set route53_zone_id to enable"
}

output "acm_certificate_status" {
  description = "ACM certificate validation status"
  value       = var.route53_zone_id != "" ? aws_acm_certificate_validation.telemetry[0].id : "N/A"
}

output "cloudfront_telemetry_domain" {
  description = "CloudFront distribution domain for telemetry dashboard"
  value       = var.route53_zone_id != "" ? aws_cloudfront_distribution.telemetry[0].domain_name : "N/A"
}

output "cloudfront_otel_domain" {
  description = "CloudFront distribution domain for OTLP endpoint"
  value       = var.route53_zone_id != "" ? aws_cloudfront_distribution.otel[0].domain_name : "N/A"
}

output "telemetry_url" {
  description = "Telemetry dashboard URL"
  value       = var.route53_zone_id != "" ? "https://${var.domain_name}" : "https://${aws_eip.telemetry.public_ip} (self-signed cert)"
}

output "otel_endpoint" {
  description = "OTLP HTTP endpoint for sending telemetry"
  value       = var.route53_zone_id != "" ? "https://${var.otel_domain_name}/v1/traces" : "http://${aws_eip.telemetry.public_ip}:4318/v1/traces"
}
