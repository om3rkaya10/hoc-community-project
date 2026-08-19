# Real-Client Validation Record

## Purpose

This document records the project's strongest validation evidence: observed behavior with the real Heroes of Order & Chaos Android client and the independently written Go server.

The record is intentionally separate from unit-test results. A unit test proves a local code property; a real-client run proves that a specific client/server lifecycle was observed under a particular environment.

## Historical validation scope

During development, the project owner reports approximately **2,500 real client/server interaction cycles** across debugging, protocol discovery, regression checks, and feature validation. This is a project-history statement, not an independently audited counter.

The most important validated lifecycle is:

```text
account/edge bootstrap
  → lobby connection
  → custom room create/join
  → game-server advertisement
  → game-server login
  → seat roster
  → hero selection
  → ready
  → shared LoadMap
  → StartPlay
  → shared match frames
  → movement/skills
  → leave/reconnect behavior
```

## Validation levels

| Level | Meaning | Evidence expected |
|---|---|---|
| V0 | Static/documentary | source inspection, historical note, or archived observation |
| V1 | Unit/regression | deterministic test in `server/hoc-server` |
| V2 | Local integration | multiple server layers exercised without the original client |
| V3 | Real client, local/LAN | real Android client reaches the documented state |
| V4 | Real client, WAN | real Android client reaches the state over a public route |
| V5 | Cross-geography public beta | independent player/ASN completes the lifecycle |

A claim must name its level. Do not describe V0/V1 evidence as real-client proof.

## Recorded real-client milestones

### Local and Nox validation — V3

Historical project records describe repeated Nox and dual-Nox validation of:

- custom room create/join;
- seat roster and hero selection;
- ready/load/start lifecycle;
- two-player shared match state;
- survivor behavior after a peer leaves;
- reconnect replay without freezing the survivor clock;
- 30 Hz shared frame clock;
- movement and skill relay.

### WAN phone validation — V4

The public edge was validated with a physical Android phone without PCAPdroid, VPN, or proxy. The observed path reached:

```text
authentication
  → lobby
  → custom room
  → game-server login
  → ready
  → LoadMap
  → StartPlay
  → op11/op7 gameplay
```

Measured server-side frame-clock evidence included approximately 30 Hz operation with low scheduler lateness. A separate TCP capture showed regular server-to-client frame pacing during a test window. These measurements describe the tested environment; they are not a latency guarantee for every network or device.

### Cross-geography public beta — V5

A real player connecting from Saudi Arabia through a Mobily/Etihad Etisalat network completed the following with a normal temporary GL Live-style username/password account:

```text
authenticate
  → lobby join
  → custom room membership=2
  → hero selection
  → READY=1
  → host ready
  → shared LoadMap
  → both players StartPlay
  → shared op11 with members=2
```

The observed game-server TCP RTT for that route was approximately 115 ms. The project goal is playable interoperability, not a universal perfect-latency guarantee.

## Supported login path found during validation

A normal beta username/password path is the canonical test path. Guest/device identities may reach the lobby but have not been treated as the canonical custom-room ready path. Reports involving guest/device identity must be classified separately from normal-account reports.

## What this record does not claim

- It does not claim that every Android device is compatible.
- It does not claim that every country, ISP, ASN, or mobile carrier is validated.
- It does not claim an official Gameloft specification.
- It does not claim that the historical 2,500-cycle count is independently audited.
- It does not replace a reproducible test or a redacted log for a new claim.

## Repeating a validation run

A contributor with an authorized test environment should record:

1. client/build identity;
2. server source commit/tag;
3. device and Android version;
4. country, ISP/carrier, and Wi-Fi/cellular path;
5. start/end time and timezone;
6. last confirmed lifecycle state;
7. redacted server evidence;
8. user-visible result;
9. whether the run was clean, partial, or failed.

Never commit passwords, access tokens, private keys, raw PCAPs, player IPs, or unredacted logs.

## Current validation status

The repository contains the documented history and deterministic Go tests. No new live Nox or phone run is required for this documentation change. Future behavior changes still require the appropriate real-client level before being called LIVE.
