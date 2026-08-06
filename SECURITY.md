# Security Policy

Security reports are welcome and should be submitted privately whenever possible.

This document describes which versions are supported, how to report a vulnerability, what information to include, and the boundaries for good-faith security research.

## Supported Versions

| Version | Security support |
|---|---|
| `v0.6.x` | Supported |
| `main` | Best effort; development code may be unstable |
| `v0.5.x` and earlier | Not supported |

Users should upgrade to the latest stable release before reporting an issue that may already have been fixed.

## Reporting a Vulnerability

Do not disclose suspected vulnerabilities in public GitHub issues, discussions, pull requests, social media, or other public channels.

The preferred reporting method is GitHub Private Vulnerability Reporting:

1. Open the repository [Security page](https://github.com/santaklouse/go-p2p-netcat/security).
2. Select **Report a vulnerability**.
3. Submit the report with the information requested below.

If the **Report a vulnerability** button is unavailable, open a public GitHub issue containing only this sentence:

> I need a private security contact for go-p2p-netcat.

Do not include vulnerability details, logs, proof-of-concept code, credentials, pairing tokens, identity keys, IP addresses, or other sensitive information in that issue.

## Information to Include

A useful report should contain:

- The affected release, tag, or commit.
- The operating system, architecture, and installation method.
- The affected component or command.
- The relevant CLI flags and transport configuration.
- Whether pairing was enabled.
- The expected and actual behavior.
- The assumed attacker position and required prerequisites.
- Exact reproduction steps.
- A minimal proof of concept, when safe to provide.
- The practical confidentiality, integrity, or availability impact.
- Suggested mitigations, if known.
- Whether you want to be credited in the advisory.

Sanitize logs, packet captures, screenshots, and configuration files before attaching them. Never submit real identity private keys, pairing tokens, token passwords, session credentials, or unrelated user data.

## Project Security Scope

Security reports may cover:

- The `p2p-nc` and `pnc` command-line applications.
- Peer identity verification and route validation.
- Pairing, admission, token encryption, and token-file handling.
- TCP, UDP, QUIC, WebSocket, WebRTC, Circuit Relay, DHT, mDNS, PubSub, Nostr, and WebTorrent integration.
- Native WebRTC signaling, authentication, channel binding, and reconnection.
- Raw sessions, PTY, remote execution, SOCKS, and TCP or UDP forwarding.
- WireGuard full-tunnel routing and privilege isolation.
- The browser core package and web PWA.
- Docker images, installation scripts, release archives, checksums, and GitHub Actions workflows.

Examples of relevant vulnerabilities include:

- Authentication or pairing bypass.
- Peer impersonation or acceptance of an unexpected PeerId.
- Exposure of identity keys, pairing tokens, passwords, or session secrets.
- Weaknesses in pairing-token encryption or encrypted token-file handling.
- Replay, downgrade, man-in-the-middle, or cross-session attacks.
- Unintended exposure of PTY, command execution, SOCKS, or forwarding services.
- Privilege-boundary or WireGuard routing-policy bypass.
- Memory corruption, command injection, path traversal, or unsafe file creation.
- Practical remote denial of service with a clear security impact.
- Release, dependency, build, or supply-chain compromise.

## Reports That Are Usually Not Security Vulnerabilities

The following are generally treated as ordinary bug reports unless they create a concrete security impact:

- Failure to connect through a particular NAT or firewall.
- mDNS being unavailable inside a container or restricted network namespace.
- QUIC receive-buffer warnings.
- Unsupported combinations of command-line options.
- Expected exposure caused by intentionally publishing or forwarding a port.
- Scanner output without evidence that the dependency or code path is reachable and exploitable.
- Performance problems that require unrealistic traffic volumes or complete control of the local machine.
- Attacks requiring prior possession of the victim's identity private key or unencrypted pairing token.

When uncertain, report the issue privately.

## Good-Faith Research Guidelines

Good-faith security research is welcome. Researchers must:

- Test only systems, accounts, devices, peers, and data they own or are explicitly authorized to test.
- Use the minimum access and traffic necessary to demonstrate the issue.
- Stop testing if unrelated private data is encountered.
- Avoid modifying, deleting, corrupting, or retaining user data.
- Avoid persistence, destructive payloads, malware, social engineering, and physical attacks.
- Avoid denial-of-service testing against public or shared infrastructure.
- Avoid using third-party relays, Nostr relays, WebTorrent trackers, DHT participants, or other public services without authorization.
- Give maintainers a reasonable opportunity to investigate and release a fix before public disclosure.

This policy does not authorize testing systems operated by third parties.

## Response and Disclosure Process

After receiving a report, maintainers will make a best-effort attempt to:

1. Confirm receipt through the private reporting channel.
2. Reproduce the issue and determine its severity and affected versions.
3. Request additional information when necessary.
4. Develop and test a mitigation or fix.
5. Prepare a patched release and security advisory when appropriate.
6. Coordinate public disclosure with the reporter.
7. Credit the reporter if requested and agreed upon.

Response and remediation times depend on severity, reproducibility, maintainer availability, and release complexity. This project does not guarantee a fixed response deadline.

Please keep the report private until a fix or advisory is published, or until a disclosure date is mutually agreed upon. A CVE may be requested for qualifying vulnerabilities, but CVE assignment is not guaranteed.

## Bug Bounty

This project does not currently operate a paid bug-bounty program. Submission of a report does not create an obligation to provide compensation.

## Security Recommendations for Users

- Use the latest stable release.
- Treat pairing tokens as bearer credentials.
- Store identity keys and tokens with permissions restricted to the current user.
- Prefer encrypted token files when tokens must be stored or transferred.
- Enable pairing for any listener exposed beyond a trusted local network.
- Never expose PTY, command execution, SOCKS, or forwarding modes without appropriate authentication and network controls.
- Verify the expected PeerId through a trusted channel.
- Do not publish identity private keys, pairing tokens, or token passwords in shell history, logs, bug reports, or container images.
- Limit externally reachable transport ports with host firewall rules.

## Additional Security Documentation

- [Security Audit](SECURITY_AUDIT.md)
- [Security Remediation Plan](SECURITY_REMEDIATION_PLAN.md)
- [Pairing Protocol](docs/PAIRING_PROTOCOL.md)
- [Use Cases and Deployment Guidance](docs/USE_CASES.md)
