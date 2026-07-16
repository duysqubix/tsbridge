# tsbridge Threat Model

## Overview

tsbridge exposes multiple backend services through isolated tsnet nodes. It relies on Tailscale for network identity and access control.

## Intended Use Case

Use tsbridge in relatively trusted environments:

- Home labs
- Personal development environments
- Small team internal networks
- Testing and staging environments

Do not use tsbridge for:

- Security-critical production workloads
- Public internet services, except services intentionally exposed through Funnel
- High-security enterprise deployments
- Workloads subject to strict compliance requirements such as PCI DSS or HIPAA

## Trust Boundaries

### Trusted Components

- Trust the Tailnet identities and ACL policy enforced by Tailscale.
- Treat the TOML configuration file and Docker labels as trusted input.
- Trust every configured backend service.
- Trust the host running tsbridge.

### Untrusted Components

- Treat networks outside the Tailnet as untrusted.
- Treat requests without a Tailscale identity as untrusted.

## Security Model

### Authentication & Authorization

- Tailscale authenticates nodes and enforces network access through ACLs.
- tsbridge adds no application-level authentication or authorization.

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

- Tailscale ACLs determine which Tailnet members can reach each service.
- tsbridge adds no per-service authentication.
- Funnel publishes a service to the internet.

### 3. Configuration Security

- Configuration files may contain OAuth credentials and other secrets.
- Restrict files containing secrets to mode `0600`.
- Anyone with Docker API access can read container labels.

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

- Compromise of a configured backend service
- Root access to the tsbridge host
- Compromise of Tailscale infrastructure
- Side-channel attacks such as timing or cache attacks
- Compromised build or runtime dependencies

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
