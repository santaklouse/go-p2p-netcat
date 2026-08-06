# Security remediation plan

[Русская версия](SECURITY_REMEDIATION_PLAN.RU.md)

Plan date: August 6, 2026.

Baseline: `v0.6.0` plus the reports in `SECURITY_AUDIT.md` and
`SECURITY_AUDIT.RU.md`. The audit remains a historical assessment of commit
`8676929`; this file tracks remediation on `codex/security-audit-fixes`.

## Release policy

The secure-default changes intentionally reject configurations that previously
started unauthenticated privileged services. They should ship as `v0.7.0`, with
an explicit migration note. The data-channel frame protocol remains version 2.
Production authentication uses response version 2 and the new
`p2p-netcat/native-webrtc-auth/v2` domain. The historical
`p2p-netcat/trystero-auth/v1` value remains available only through explicit
legacy helpers and is never accepted by the production endpoint.

## P0: prevent credential-less exposure

### S-01 — Privileged listeners require pairing

Status: implemented; local verification passed.

Listener `-i`, `-e`, `-S`, TCP forwarding, and UDP forwarding reject startup
unless a pairing token source is configured. Deliberately public operation
requires the long flag `--allow-unauthenticated-listener` and emits a warning.
Raw stream listeners retain netcat-style public behavior.

Acceptance criteria:

- every privileged listener mode is rejected without a token;
- `--pairing-token`, `--pairing-token-file`, and `P2P_NETCAT_TOKEN` satisfy the
  startup guard and are still fully decoded and scoped before networking;
- the unsafe override is valid only on a privileged listener;
- listener-lock and mode-matrix tests cover the new behavior.

### S-02 — Disable unauthenticated Native WebRTC by default

Status: implemented; local verification passed.

The Go CLI starts custom Nostr/WebTorrent Native WebRTC only with a valid
pairing token. The explicit `--allow-unauthenticated-native-webrtc` escape hatch
emits a warning. Standard libp2p WebRTC Direct remains available because Noise
binds that connection to the expected PeerId. The browser PWA has no unsafe UI
escape hatch: without a pairing token it attempts only the Noise-authenticated
libp2p Worker route.

Acceptance criteria:

- Go listener and client do not create Native WebRTC signaling sessions without
  a token or the explicit unsafe flag;
- the PWA does not instantiate `BrowserNativeWebRtcClient` without a token;
- paired Go and browser connections continue to race libp2p and Native WebRTC;
- Tor and `--no-webrtc` continue to disable all WebRTC routes.

## P1: bind sessions and bound attacker-controlled work

### S-03 — Native WebRTC channel-binding migration

Status: implemented; local cross-language and real-Pion verification passed.

Authentication response version 2 uses a new domain. The signed transcript
contains length-delimited values for:

- the new authentication domain and response version;
- client and server roles;
- expected server PeerId and logical port;
- signaling session ID;
- client challenge;
- SHA-256 of the exact offer SDP;
- SHA-256 of the exact answer SDP.

The SDP hashes bind both DTLS certificate fingerprints and ICE/session context
to the PeerId signature. Clients reject legacy responses without downgrade
fallback. Go and `packages/core` share a fixed payload vector, and a regression
test with two real Pion connections proves that a proof from one connection
cannot be used on another.

Acceptance criteria:

- a relayed signature from a second PeerConnection fails verification;
- changing the offer, answer, role, session ID, PeerId, port, or challenge
  invalidates the proof;
- Go-to-browser and browser-to-Go paired compatibility tests pass;
- a targeted two-PeerConnection MitM regression test is included.

### S-04 — Resource and parsing limits

Status: implemented; local verification passed.

Implemented limits include 32 concurrent Native WebRTC handshakes globally,
two per signaling peer ID, a 1 MiB stream receive queue, fail-closed Pion frame
queue behavior, 256 KiB SDP, 64 KiB ICE candidate JSON, 512 KiB encrypted and
WebSocket signaling messages, a SOCKS negotiation deadline, and 255-byte
SOCKS4 user/domain fields.

Acceptance criteria:

- limit tests prove rejection without unbounded allocation or goroutine growth;
- the race detector passes the listener close/handshake paths;
- the real-Pion smoke profile and browser tests pass.

### S-05 — Sensitive-file policy

Status: implemented on Unix and Windows; Windows runtime execution remains a CI gate.

Existing identity and pairing-token files must be regular files and reads are
size-limited. Unix rejects group/other permissions. Windows inspects the DACL
and permits secret access only to the owner, current user, LocalSystem, and
built-in Administrators; missing and unsupported DACLs fail closed. New files
remain mode `0600` on Unix and receive a protected DACL for the current user,
LocalSystem, and built-in Administrators on Windows.

## P2: supply chain and availability

### S-06 — Dependency advisories

Status: partially implemented.

The web override is updated from `brace-expansion@5.0.8` to `5.0.9`, and
`npm audit --package-lock-only --audit-level=high` reports zero vulnerabilities.
`govulncheck` still reports GO-2024-3218 in
`go-libp2p-kad-dht@v0.42.1`; the advisory lists no fixed version. Keep DHT
optional, document `--no-dht` plus a trusted direct address or Circuit Relay as
the availability-sensitive mitigation, and recheck each dependency update.

### S-07 — Signed releases

Status: implemented; first published bundles await the `v0.7.0` release.

The release workflow creates a keyless Sigstore bundle for every archive,
installer script, and `SHA256SUMS`, while retaining GitHub artifact
attestations. The deploy script verifies the `SHA256SUMS` bundle against the
exact repository workflow identity and GitHub Actions OIDC issuer before using
the checksums. The cosign installer action is pinned to a commit SHA. Legacy
checksum-only installation requires explicit `P2PNC_ALLOW_UNSIGNED=1` opt-in.
Optional UPX-packed archives are produced only for Linux `amd64` and `arm64`,
alongside the original archives. They retain standard UPX metadata, pass UPX
integrity and round-trip comparison checks, and are covered by the same
checksums, signatures, and attestations as every other release artifact.

## Verification gate

Before release, all of the following must pass:

```bash
GOTOOLCHAIN=auto go test ./...
GOTOOLCHAIN=auto go test -race -count=1 -timeout=25m ./...
GOTOOLCHAIN=auto go vet ./...
GOTOOLCHAIN=auto go run ./cmd/webrtc-soak --profile smoke
bash deploy/deploy_test.sh
bash deploy/wireguard-full-tunnel_test.sh
bash scripts/sync-wiki_test.sh
bash scripts/docker_test.sh
(cd packages/core && npm ci && npm test && npm run lint && npm pack --dry-run)
(cd web && npm ci && npm test && npm run lint && npm pack --dry-run)
```

The privileged Linux network-namespace test remains a separate required gate
on a compatible Linux host.
