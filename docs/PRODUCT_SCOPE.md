# Njiwa-Mail: product scope and source of truth

This is the owner's scope statement, kept verbatim. Where another document or the code disagrees with it, this document wins and the
other one is wrong. The current state against it, with corrective tasks, is in [`SCOPE_AUDIT_CURRENT.md`](SCOPE_AUDIT_CURRENT.md).

## Product vision

Njiwa-Mail is an independent, self-hostable, API-first transactional email delivery and developer infrastructure platform.

Its purpose is to allow developers and organizations to operate and manage application email delivery through one unified platform
without being dependent on a specific commercial email gateway, mail server implementation or hosting provider.

Njiwa-Mail must provide a single dashboard, unified API, common domain-management system, message queue, template engine, monitoring
system, and developer experience.

The product must support exactly two primary outbound delivery modes:

### Mode 1: Njiwa-managed delivery

Njiwa-Mail provisions and manages the required outbound mail infrastructure, including Postfix and DKIM signing, on a compatible machine.

This mode must include installation preflight, conflict detection, DNS guidance, safe provisioning, queue monitoring, security controls,
rollback and clear operational diagnostics.

The installer must never overwrite or disrupt existing production mail services.

### Mode 2: Existing mail server

Njiwa-Mail connects to an already available SMTP submission service, including Mailcow, Postfix, Exim or another compatible SMTP server.

This mode must not install a competing mail transfer agent.

Mailcow API-based provisioning may be offered as an optional integration but must never be a prerequisite for generic SMTP connectivity.

A fresh VPS is not required for this mode.

## Core product requirements

- **Unified experience.** Both modes must share the same dashboard, REST API, project management, environment controls, templates, domain
  registration, sender identities, API keys, queues, delivery tracking, webhooks and analytics.
- **Intelligent installation.** The installer must inspect the host, detect existing services, determine compatible installation options
  and guide the operator through safe setup. It must be possible to deploy Njiwa-Mail alongside unrelated applications without taking
  ownership of their system configuration.
- **Domain management.** Users must be able to register and verify sending domains, generate transport-specific DNS records, validate
  SPF/DKIM/DMARC, manage sender identities and troubleshoot DNS issues.
- **Automated DNS.** Support authorized Cloudflare configuration through supported integration methods. Preview DNS changes, require user
  approval, prevent destructive conflicts, verify results and provide manual DNS fallback.
- **Developer API.** Provide secure, project-scoped API authentication and reliable email submission for OTPs, account verification,
  password resets and application notifications. Preserve documented contracts and backward compatibility unless an explicit versioned
  migration is approved.
- **Reliable sending.** Provide durable queues, worker processing, idempotency, retry policies, uncertain-outcome handling, suppression
  lists, bounce processing and accurate message states.
- **Security.** Encrypt stored secrets, enforce authorization boundaries, prevent unauthorized sender/domain usage, protect production
  infrastructure, avoid open SMTP relays and keep comprehensive audit records.
- **Public documentation.** Publish accurate human-readable guides, versioned OpenAPI contracts, SDK examples, agent-readable Markdown,
  integration instructions and developer troubleshooting resources.
- **Operations.** Support installation, upgrades, monitoring, health checks, backups, restore, disaster recovery and safe rollback.

## Important scope boundaries

Njiwa-Mail must NOT be designed as:

- A mandatory Mailcow installation or management system.
- A Postfix installer with a dashboard attached.
- A tool that only works on empty VPS hosts.
- A Ditronics-specific application.
- A system where a custom domain is treated as a third delivery mode.
- A replacement for existing production mail services by default.
- A product that requires full mailbox hosting before transactional email can be sent.

Preserve existing functional modules where possible, but do not expand unrelated functionality at the expense of the main transactional
delivery platform.

## Installation versus release testing

A fresh, isolated test machine may be required to verify managed provisioning. That requirement must not be confused with a customer
installation requirement.

Use QEMU/KVM and other isolated environments for repeatable installer testing. Existing-mail-server integrations must be tested without
modifying the existing server's global configuration.

Tests requiring genuine public IP routing, PTR configuration or external SMTP delivery must be clearly identified and verified separately.

Do not falsely declare production readiness based on mocked or simulated checks.

## Required agent actions

Before continuing development:

1. Audit the current implementation against this product scope.
2. Identify features that are already implemented, partially implemented, missing or incorrectly designed.
3. Identify any decisions that unnecessarily tie Njiwa-Mail to Mailcow, clean VPS installation or a single SMTP implementation.
4. Prepare corrective engineering tasks with affected files, migrations, dependencies and acceptance tests.
5. Preserve working functionality and existing data.
6. Do not alter production mail infrastructure, firewall rules, DNS or external resources without explicit approval.
7. Implement and test both delivery modes independently.
8. Update documentation and report remaining limitations accurately.

## Product definition of done

A developer must be able to install Njiwa-Mail on a compatible server, connect an existing SMTP server or safely provision managed
delivery, register a domain, configure DNS, generate an API key, send application email, inspect its status and manage the platform
through the same interface.

A developer using the API must not need to know which outbound transport is configured.

A production email service must not be disrupted by Njiwa-Mail installation or operation.

The goal is a seamless, self-hostable transactional email infrastructure platform, not a mandatory fresh-server mail installation tool.
