# Testing Guide

## Test layers

The project intentionally separates test layers:

### Unit and package regression — V1

Run from `server/hoc-server`:

```text
go test ./... -count=1
```

This exercises account normalization, configuration, packet builders/parsers, room/session behavior, match clock behavior, WAN write handling, reconnect logic, and edge/lobby/game-server handlers using deterministic test doubles.

### Build and static checks

```text
go build ./...
go vet ./...
```

These checks must pass before a source release or server tag.

### Local integration — V2

Exercise multiple server layers with local sockets/test doubles. Keep this separate from real-client claims: passing integration tests does not prove that the original Android client reaches the same state.

### Real-client local/WAN — V3/V4

A real phone or Nox run validates compatibility with a particular client build, device, route, and configuration. Record the evidence in the format from `REAL_CLIENT_VALIDATION.md`.

### Cross-geography beta — V5

A report from an independent country/ISP/ASN is valuable evidence, but it is still a test of that route and device. Do not generalize one player's RTT or success to all regions.

## Canonical lifecycle checklist

```text
[ ] Account/edge bootstrap
[ ] Lobby connection
[ ] Custom room create or join
[ ] Game-server advertisement
[ ] Game-server login
[ ] Seat roster
[ ] Hero selection
[ ] Ready
[ ] LoadMap
[ ] StartPlay
[ ] Shared 30 Hz frames
[ ] Movement/skill relay
[ ] Leave/reconnect behavior (when tested)
```

## Evidence labels

Use the following labels in issues and notes:

- `V0-static`
- `V1-unit`
- `V2-integration`
- `V3-real-client-lan`
- `V4-real-client-wan`
- `V5-cross-geography`
- `hypothesis`
- `parked`

## Redaction rules

Before sharing evidence, remove:

- passwords and tokens;
- private keys and certificates;
- player IP addresses;
- account database contents;
- provider control details;
- raw PCAP payloads;
- personal information.

A timestamp, country/ISP, device model, lifecycle state, opcode label, and aggregate metric are usually enough for a useful public report.

## Release gate

A server source release requires:

```text
go build ./...
go vet ./...
go test ./... -count=1
git diff --check
```

A real-client claim additionally requires a corresponding V3/V4/V5 record. A documentation-only change does not require a new live client run.

## What is intentionally not automated yet

Graceful listener shutdown, atomic account persistence, and additional wire-layer fuzz/bounds work are not enabled by this document. They may be valuable, but changing them can affect runtime lifecycle, persistence timing, or packet acceptance. They require focused tests and, where behavior changes, a fresh real-client regression before adoption.

## Failure reporting

Include:

- command and working directory;
- source commit/tag;
- complete error output;
- test layer;
- whether the failure is deterministic;
- whether any real client was involved.

Do not call a unit-test failure a network failure without evidence.

## Current baseline

The copied Go server under `server/hoc-server` is expected to pass build, vet, and all package tests. The public repository does not require a connected phone or live VPS to run its deterministic test suite.
