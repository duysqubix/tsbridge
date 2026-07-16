# tsbridge Threat Model

## Overview

tsbridge is a Go-based proxy manager built on Tailscale's tsnet library, designed to expose multiple services on a Tailnet through a single configuration file. The threat model defines tsbridge's security assumptions, intended uses, and known limitations.

## Intended Use Case

tsbridge is designed for relatively trusted environments such as:

- Home labs
- Personal development environments
- Small team internal networks
- Testing and staging environments

tsbridge is NOT designed for:

- Security-critical production environments
- Public-facing internet services
- High-security enterprise deployments
- Environments requiring strict compliance (PCI-DSS, HIPAA, etc.)

## Trust Boundaries

### Trusted Components

1. Tailscale Network: The Tailnet is considered trusted; all nodes authenticated via Tailscale are trusted
2. Configuration Source: The TOML configuration file or Docker labels are trusted inputs
3. Backend Services: All configured backend services are trusted
4. Host System: The system running tsbridge is fully trusted

### Untrusted Components

1. External Networks: Any network outside the Tailnet
2. Unauthenticated Requests: Requests not authenticated by Tailscale

## Security Model

### Authentication & Authorization

- Primary Security: Relies entirely on Tailscale's authentication and network security
- No Additional Auth: tsbridge does not implement its own authentication layer
- Network-Level Security: Security is enforced at the network level via Tailscale ACLs

### Data Protection

- Tailscale encrypts Tailnet traffic with WireGuard.
- Each service stores tsnet node state under its state directory. tsbridge does not store proxied request or response bodies.
- OAuth credentials can come from files or environment variables. tsbridge redacts secrets in logs but holds them unencrypted in memory.

## Known Security Considerations

### 1. Proxy Trust Model

- tsbridge can read and modify all proxied request and response data.
- Incoming `X-Tailscale-*` headers are removed before optional Whois middleware adds trusted identity headers.
- Operators can add or remove configured upstream and downstream headers. Go's reverse proxy also manages `Host` and forwarding headers.
- Response bodies pass through without content inspection.

### 2. Service Exposure

- Any service configured in tsbridge is accessible to all Tailnet members (subject to Tailscale ACLs)
- No per-service authentication is implemented
- Funnel mode exposes services to the public internet (use with extreme caution)

### 3. Configuration Security

- Configuration files may contain sensitive data (OAuth tokens)
- File permissions should be restricted (recommended: 600)
- Docker labels are visible to anyone with Docker API access

### 4. Resource Limits

- tsbridge has no built-in rate or connection limits.
- Request bodies are limited to 50 MiB by default. Operators can change or disable the limit globally or per service.
- Trusted Tailnet members can still exhaust connections, memory, CPU, or backend capacity.

### 5. Logging & Monitoring

- Access logs may contain sensitive information.
- Whois data includes user identities.
- The metrics endpoint has no authentication. Metrics are disabled by default; the configured listen address determines network exposure.

## Threat Scenarios

### Out of Scope Threats

1. Malicious Backend Services: Compromised backend services
2. Host System Compromise: Root access to tsbridge host
3. Tailscale Infrastructure Compromise: Issues with Tailscale's security
4. Side-Channel Attacks: Timing attacks, cache attacks, etc.
5. Supply Chain Attacks: Compromised dependencies

## Security Best Practices

### Deployment

1. Run tsbridge with minimal privileges
2. Use a dedicated service account
3. Restrict configuration file permissions
4. Enable access logging for audit trails
5. Monitor resource usage

### Configuration

1. Use file-based secrets or environment variables instead of inline configuration
2. Limit funnel usage to truly public services only
3. Configure appropriate timeouts for all connections
4. Use TLS for backend connections where possible

### Network

1. Configure Tailscale ACLs appropriately
2. Monitor Tailnet access patterns

### Monitoring

1. Bind Prometheus metrics to `127.0.0.1` or another protected interface
2. Monitor unusual access patterns
3. Alert on request failures and service lifecycle failures
4. Monitor backend health outside tsbridge

## Security Updates

- tsbridge follows Go's security update cycle.
- Dependencies receive regular updates.
- Report vulnerabilities through the private process in [SECURITY.md](SECURITY.md).
- Security fixes are released as patch versions after verification.

## Disclaimer

tsbridge is provided as-is for use in trusted environments. Users deploying tsbridge accept responsibility for:

- Evaluating its suitability for their use case
- Implementing additional security controls as needed
- Monitoring and responding to security events
- Keeping the software and its dependencies updated
