# Release, Distribution, and Beta Operations

## Release posture

The community server is a public beta: the goal is playable interoperability and evidence gathering, not perfect latency or universal device support.

See `VALIDATION.md` for evidence levels and `DEVELOPMENT.md` for build/test gates.

## Versioning

Use server tags in this form:

```text
server-vMAJOR.MINOR.PATCH
```

The current releases are `server-v0.1.0` and `server-v0.1.1`. A release should record its date, source commit, compatibility expectations, known issues, rollback notes, test result, and checksums.

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
- Latest server release: `server-v0.1.1`
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
