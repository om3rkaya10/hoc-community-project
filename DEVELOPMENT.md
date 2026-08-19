# Development Guide

## Scope

The `server/hoc-server/` directory contains the independently written Go community server. It is separate from Gameloft's original client and assets. Compatibility is more important than architectural elegance; keep changes narrow and avoid large refactors.

## Main packages

| Package | Role |
|---|---|
| `internal/accounts` | temporary accounts, profile identity, token lifecycle |
| `internal/edge` | HTTP/TLS edge and authorization/profile/config responses |
| `internal/lobby` | room create/join/leave and server advertisement |
| `internal/gs` | game-server login, seats, ready/load/start, player actions |
| `internal/match` | shared 30 Hz clock, relay, reconnect replay |
| `internal/session` | rooms, seats, hero claims, survivor promotion |
| `internal/wire/glblock` | independently written block/field builders |
| `internal/wire/gs` | independently written game-server builders/parsers |
| `internal/wire/msgpack` | protocol utility |
| `internal/netx` | bounded in-match writes for WAN peers |

## Deterministic checks

Run from `server/hoc-server`:

```text
go build ./...
go vet ./...
go test ./... -count=1
```

The tests cover account normalization, configuration, packet builders/parsers, room/session state, match clock behavior, WAN write handling, reconnect logic, and edge/lobby/game-server handlers using deterministic test doubles.

These checks do not prove that the original Android client reaches the same state. Real-client evidence is recorded in `VALIDATION.md`.

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

## Pull requests and contributions

Before opening a change:

1. Read `README.md`, `LICENSE`, `NOTICE`, `RELEASE.md`, `VALIDATION.md`, and `GOVERNANCE.md`.
2. Confirm that the contribution is original or that You have the right to submit it.
3. Do not submit client code, extracted assets, APK/OBB/SO files, raw decompilation, raw Frida scripts, credentials, or unredacted captures.
4. Explain whether the change affects documentation, deterministic server behavior, or a real-client path.
5. Keep unrelated cleanup out of the change.

A protocol or behavior claim must include a deterministic test, redacted evidence, a V3/V4/V5 validation record, or an explicit `hypothesis`/`parked` label. Never present an inference as an official protocol specification.

A pull request should state what changed, why, possible compatibility impact, tests run, whether a live client run is required, and what sensitive/proprietary material was deliberately excluded.

For server changes, do not alter pinned wire behavior, frame-clock ownership, account identity semantics, or reconnect transitions without a focused regression test and compatibility note. Changes requiring a phone/Nox/VPS run must say so before merge. Documentation-only changes do not require live testing.

## Evidence and redaction

Before sharing evidence remove passwords, tokens, private keys, certificates, player IPs, account database contents, provider control details, raw PCAP payloads, and personal information. A timestamp, country/ISP, device model, lifecycle state, opcode label, and aggregate metric are usually enough.

## Release gate

A server source release requires:

```text
go build ./...
go vet ./...
go test ./... -count=1
git diff --check
```

A real-client claim additionally requires an appropriate V3/V4/V5 record. The current release does not require a connected phone or live VPS to run the deterministic suite.

## Intentionally deferred hardening

Graceful listener shutdown, atomic account persistence, and additional wire-layer fuzz/bounds work are not enabled by this guide. They may be valuable, but can affect runtime lifecycle, persistence timing, or packet acceptance. They require focused tests and, where behavior changes, a fresh real-client regression before adoption.

## Commit style

Use concise factual subjects:

```text
Document cross-geography validation levels
Add regression test for room survivor state
Clarify server release package boundary
```

## Contribution terms

Contributors represent that they have the right to submit their contribution and agree to the contribution terms in `LICENSE`. Unless a separate written agreement applies, contributions are made available under the HOC Community Server Community Source License.

## No live claim without evidence

Do not label a deterministic test, local mock, or static observation as LIVE. Record the evidence level explicitly.
