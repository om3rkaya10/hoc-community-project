# Validation and Preservation Method

## Purpose

This document defines how the project observes client behavior, classifies evidence, and records real-client validation. It deliberately separates deterministic code tests from historical or real-device claims.

## Clean-room posture

The project records externally observable behavior and writes independent server implementations. Contributors must not copy proprietary code or redistribute original game data.

## Evidence workflow

1. State the question precisely.
2. Identify the account, lobby, game-server, or match-runtime layer under test.
3. Capture the smallest useful observation.
4. Remove credentials and personal data.
5. Reproduce locally where possible.
6. Assign an evidence level.
7. Record what would falsify the conclusion.

Useful measurements include endpoint response shapes without tokens, room-state transitions, frame-clock distributions, TCP RTT ranges, device/ABI results, and the exact user-visible failure state.

## Evidence levels

| Level | Meaning | Evidence expected |
|---|---|---|
| V0 | Static/documentary | source inspection, historical note, or archived observation |
| V1 | Unit/regression | deterministic test in `server/hoc-server` |
| V2 | Local integration | multiple server layers exercised without the original client |
| V3 | Real client, local/LAN | real Android client reaches the documented state |
| V4 | Real client, WAN | real Android client reaches the state over a public route |
| V5 | Cross-geography public beta | independent player/ASN completes the lifecycle |

A claim must identify its level. V0/V1 evidence must not be presented as real-client proof. Use `hypothesis` or `parked` when the evidence is insufficient.

## Historical validation scope

The project owner reports approximately **2,500 real client/server interaction cycles** during debugging, protocol discovery, regression checks, and feature validation. This is a project-history statement, not an independently audited counter.

The principal validated lifecycle is:

```text
account/edge bootstrap
  → lobby connection
  → custom room create/join
  → game-server advertisement and login
  → seat roster and hero selection
  → ready
  → shared LoadMap
  → StartPlay
  → shared match frames
  → movement/skills
  → leave/reconnect behavior
```

## Recorded milestones

### Local and Nox — V3

Historical records describe repeated Nox and dual-Nox validation of custom rooms, seat/hero state, ready/load/start, two-player shared matches, survivor behavior, reconnect replay, the shared 30 Hz frame clock, and movement/skill relay.

### Physical phone over WAN — V4

A physical Android phone completed authentication, lobby, custom-room, game-server login, ready, LoadMap, StartPlay, and gameplay without PCAPdroid, VPN, or proxy. Server-side timing and a separate TCP capture showed regular frame pacing in the tested environment. This is not a universal device or latency guarantee.

### Cross-geography public beta — V5

A player connecting from Saudi Arabia through a Mobily/Etihad Etisalat route completed:

```text
authenticate
  → lobby join
  → custom room membership=2
  → hero selection and READY=1
  → host ready
  → shared LoadMap
  → both players StartPlay
  → shared op11 with members=2
```

The observed game-server RTT was approximately 115 ms. The project goal is playable interoperability, not perfect latency for every geography.

## Supported login path

A normal beta username/password path is the canonical validation path. Guest/device identities may reach the lobby but are not the canonical custom-room ready path and should be reported separately.

## Repeating a real-client run

Record:

1. client/build identity;
2. server commit/tag;
3. device and Android version;
4. country, ISP/carrier, and Wi-Fi/cellular path;
5. time and timezone;
6. last confirmed lifecycle state;
7. redacted server evidence;
8. user-visible result;
9. clean, partial, or failed outcome.

## Redaction and publication boundary

Do not publish raw PCAPs, unredacted journals, passwords, tokens, private keys, player IPs, account data, provider credentials, Frida patch scripts, absolute memory addresses, original assets, or extracted tables.

The private research archive may retain evidence locally under access control, but it is not automatically suitable for publication.

## Limits of this record

This record does not claim universal device compatibility, universal ISP/ASN validation, an official Gameloft specification, or an independently audited interaction count. Every new behavior claim still requires appropriate evidence.

Documentation-only changes do not require a fresh live run. Changes to wire behavior, runtime lifecycle, persistence timing, or client-visible state require the appropriate V3/V4/V5 regression before being called LIVE.
