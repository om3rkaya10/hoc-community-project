# Release, Distribution, and Beta Operations

## Release posture

The community server is a public beta: the goal is playable interoperability and evidence gathering, not perfect latency or universal device support.

See `VALIDATION.md` for evidence levels and `DEVELOPMENT.md` for build/test gates.

## Versioning

Use server tags in this form:

```text
server-vMAJOR.MINOR.PATCH
```

The current releases are `server-v0.1.0`, `server-v0.1.1`, `server-v0.1.2`, `server-v0.1.3`, `server-v0.1.4`, `server-v0.1.5`, `server-v0.1.6`, `server-v0.1.7`, and `server-v0.1.8`. A release should record its date, source commit, compatibility expectations, known issues, rollback notes, test result, and checksums.

### server-v0.1.8

`server-v0.1.8` is a summoner-spell hotfix on top of `server-v0.1.7`. The server located the two summoner spells in the client's `0x100C` SkillAck body heuristically (READY+14 plus a byte scan), and on the public server that scan mostly latched garbage, so the spells a player picked sometimes did not reach the LoadMap PlayerInfo and the match. The body layout was recovered from the client (cid, three length-prefixed strings, then a fixed run of little-endian ints with the spells at READY+12 / READY+16) and the parser now reads it directly. It is backed by the deterministic Go suite (new layout-based parser tests) and a recorded validation on the public server with a Nox client in `VALIDATION.md`. No client update is required; it pairs with Global Public Beta 0.3.2, the client build that fixes the Android 15/16 match-loading crash (see `CHANGELOG.md` and `COMPATIBILITY.md`).

### server-v0.1.7

`server-v0.1.7` is a private-chat hotfix on top of `server-v0.1.6`. The client compares only the first four characters of a chat message id when filtering duplicates, so with the ids `server-v0.1.6` generated every message after the first in a conversation was dropped before rendering when played over the public server (the local two-client validation had not exposed it). Message ids are now short fixed-width base-36 counters, the listen stream uses explicit identity framing with a 5 s heartbeat and is no longer rotated, and the alert-stream keepalive is a real event. It is backed by the deterministic Go suite (including new chat-flow tests over real HTTP connections) and a recorded validation on the public server with a 32-bit phone client and a Nox client in `VALIDATION.md`. No client update is required; it pairs with Global Public Beta 0.3. The arm64-v8a client build did not render private messages at release time; client build Global Public Beta 0.3.1 arm64 (2026-09-16) fixes that on the client side.

### server-v0.1.6

`server-v0.1.6` is a social hotfix release. It implements the Gaia Osiris friend endpoints (requests, accept/ignore, list, remove), the Seshat batch profile lookup the friends list depends on, the Kairos alert stream for instant friend-request and chat-invitation delivery, lobby presence (`0xe00e`/`0xe00f`) so friends show as online or in a match, and the Arion group-chat service that carries friend private messages. Before this release the friends list could not add anyone, everyone appeared offline and private messages went nowhere. It is backed by the deterministic Go suite and a recorded two-client Nox validation against a local build in `VALIDATION.md`. No client update is required; it pairs with Global Public Beta 0.3.

### server-v0.1.5

`server-v0.1.5` is an inventory and in-match passives release. It fixes tablet reopen/delete targeting (the server now uses the client's owned-tablet index space and accepts the Rune reopen price), routes shop purchases by item type (emblem packs credit Emblems, bundles expand, consumables reach the Items tab, poles/banners are owned in the Flags screen) with a one-time account migration, keeps flag ownership and selection across purchases and re-login, unblocks the Flags tab, and carries awake tablets, talents and the battle banner in the LoadMap PlayerInfo. It is backed by the deterministic Go suite and a recorded V3 Nox validation against the public server in `VALIDATION.md`. No client update is required; it pairs with Global Public Beta 0.3.

### server-v0.1.4

`server-v0.1.4` is a wallet-display and post-login bootstrap maintenance release. It sends the BuyItem/BuyItemCRM wallet fields in the order the client actually reads (rune `[4]`, emblem `[5]`), which stops the rune counter from jumping to 99999 after a purchase or a match, and it keeps the client's post-login bootstrap (alerts, device registration, CRM catalog) running by giving the login inject a non-zero rune delta. The release is backed by the deterministic Go suite and a recorded V3 Nox validation against the public server in `VALIDATION.md`. It pairs with client build Global Public Beta 0.3 (two client-side fixes: lobby icon corruption after a match, hero-select timeout after in-game logout/re-login).

### server-v0.1.3

`server-v0.1.3` is a Kitabe tablet-state maintenance release. It implements the 750-Emblem reopen transaction, keeps the debit idempotent, preserves stable tablet packet identities across all rendered views, prevents duplicate equipped-tablet records, and normalizes legacy duplicate state. The release is backed by the deterministic Go suite and recorded V3 clean-account validation in `VALIDATION.md`.

### server-v0.1.2

`server-v0.1.2` is a client-visible profile and Kitabe maintenance release. It includes shared talent-budget fixes, valid exhausted-balance persistence, idempotent Revival Rune starter inventory, and atomic normal inscription tier exchange. The release is backed by the deterministic Go suite and recorded V3 real-client validation in `VALIDATION.md`.

## Distribution channels

| Material | Channel |
|---|---|
| Handbook and Go source | This GitHub repository |
| Source release | GitHub tag / GitHub Release |
| Server binary | Optional GitHub Release asset |
| Container | `ghcr.io/om3rkaya10/hoc-community-project` |
| Client APK | Separate project-controlled location |
| OBB/original game data | Not distributed here |
| Runtime secrets/config | Never public |

The source tree must not contain APK/IPA/OBB, native libraries, extracted assets, game tables, scripts, decompiled client code, raw Frida scripts, memory dumps, raw PCAPs, credentials, certificates, account stores, or unredacted logs.

## Server packages

A binary package may contain only the compiled Go server, `LICENSE`, `NOTICE`, a non-secret configuration example, SHA-256 manifest, and startup/health instructions. Runtime certificates, account storage, environment files, provider details, and live logs are supplied separately.

The tagged GitHub Actions workflow runs:

```text
go build ./...
go vet ./...
go test ./... -count=1
```

before publishing the Linux/amd64 GHCR image. The image contains only the original Go server binary, license/notice files, and an empty non-secret runtime directory.

## Client artifact policy

Client artifacts are not part of the source tree. Any client announcement must state exact file/version, SHA-256, ABI/device profile, installation notes, known issues, account-login requirements, password/privacy warnings, bug-report route, and unofficial-project status. It must not imply an official Gameloft release or bundle original OBB/data files here.

## Checksums

Provide SHA-256 for every downloadable package:

```text
Windows: Get-FileHash -Algorithm SHA256 .\package.zip
Linux:   sha256sum package.zip
macOS:   shasum -a 256 package.zip
```

## Public-beta operations

Collect bug reports for approximately 15 days, classify them, then ship small grouped fixes. Avoid emergency speculative changes to pinned wire behavior.

Ask for country/ISP, Wi-Fi or mobile data, device/Android version, local time/timezone, last visible lifecycle state, reproduction steps, and safe screenshots/video. Never request passwords, tokens, private logs, device identifiers, or personal data.

Normal beta username/password login is the canonical path. Guest/device identities may reach the lobby but are not the canonical custom-room ready path.

## Rollback and migration

Keep a known-good server commit, binary, and runtime configuration outside the public tree. If a release regresses:

1. announce maintenance or rollback;
2. stop or drain the affected service;
3. restore the known-good artifact/configuration;
4. verify health endpoints and listeners;
5. record the incident without secrets or personal data.

Prefer hostname/DNS migration so an infrastructure move does not require a new client build.

## Free community use

The license permits free non-commercial community operation with attribution. A fork or operator may not charge for access, sell builds, or offer paid hosting/SaaS without separate written permission. Donations and transparent infrastructure cost sharing are permitted only under the exact conditions in `LICENSE`; payment may not buy access, priority, features, ranks, or gameplay advantages.

## Disputed artifacts

Remove disputed material from active distribution while it is reviewed. Keep neutral documentation separate from the disputed binary and restore an artifact only after a documented basis for inclusion and any required rights or permissions have been established.

## Responsibility

This project is published as-is. Each contributor, redistributor, operator, and user remains solely responsible for evaluating and complying with the laws, regulations, licenses, and third-party rights applicable in their own jurisdiction.

## References

- `LICENSE` / `NOTICE` — ownership and source-available terms
- `GOVERNANCE.md` — content, security, and third-party boundaries
- `DEVELOPMENT.md` — server development, testing, and contribution
- `VALIDATION.md` — preservation and real-client evidence
- `COMPATIBILITY.md` — Android/device notes

## Current links

- Repository: https://github.com/om3rkaya10/hoc-community-project
- Latest server release: `server-v0.1.4`
- Container: `ghcr.io/om3rkaya10/hoc-community-project`

## Documentation-only note

This policy consolidation does not change Go code, wire behavior, persistence, shutdown behavior, or deployment configuration. No live client run is required for this change.

## Release checklist

```text
[ ] Record source commit/tag
[ ] Run build/vet/test
[ ] Verify checksum
[ ] Exclude secrets and third-party client data
[ ] Include LICENSE/NOTICE
[ ] Keep rollback artifact
[ ] Label real-client evidence correctly
```

## Package naming examples

```text
hoc-community-server_server-v0.2.0_linux_amd64.zip
ghcr.io/om3rkaya10/hoc-community-project:server-v0.2.0
```

## Current baseline

Live client testing is required only when a change affects client-visible behavior or when a new LIVE claim is made. Documentation-only changes do not require Nox, phone, or VPS testing.

## Status

This document consolidates the former release, distribution, and beta-operations guidance.
