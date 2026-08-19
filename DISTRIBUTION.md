# Distribution Guide

## Distribution channels

Use separate channels for separate material:

| Material | Recommended channel |
|---|---|
| Handbook and Go server source | This GitHub repository |
| Source release/tag | GitHub Releases or repository tags |
| Server binaries | Optional release asset, built from a recorded commit |
| Client APK | Separate project-controlled distribution location, subject to the client artifact policy |
| OBB/original game data | Not distributed by this repository |
| Runtime configuration/secrets | Never publicly distributed |

## GitHub source releases

A source release should include:

- release title and tag;
- commit SHA;
- summary of changes;
- known issues;
- build/test results;
- upgrade and rollback notes;
- `LICENSE` and `NOTICE`.

Recommended server tag format:

```text
server-vMAJOR.MINOR.PATCH
```

Example:

```text
server-v0.2.0
```

## Server binary packages

If project-authored server binaries are published, build from a clean tagged source commit. Name packages clearly, for example:

```text
hoc-community-server_server-v0.2.0_linux_amd64.zip
```

A binary package should contain only:

- compiled server binary;
- `LICENSE`;
- `NOTICE`;
- a minimal configuration example containing no secrets;
- SHA-256 manifest;
- startup and health-check instructions.

Do not include private keys, certificates, player/account data, production environment files, provider credentials, or live logs.

## Client package announcements

A client package announcement should include:

- beta/release status;
- exact file name;
- SHA-256 checksum;
- device/ABI compatibility;
- installation instructions;
- normal account-login requirement and guest limitations;
- known issues;
- bug-report template;
- password/privacy warnings where applicable;
- unofficial-project statement.

The announcement must not imply that the package or community server is an official Gameloft service.

## Integrity verification

Provide SHA-256 for every downloadable package. Example commands:

```text
Windows PowerShell:
Get-FileHash -Algorithm SHA256 .\package.zip

Linux:
sha256sum package.zip

macOS:
shasum -a 256 package.zip
```

Users should verify the published checksum before installation or execution.

## Free community use

The project license permits free non-commercial community operation with required attribution. A fork or operator may not charge for server access, sell builds, or offer paid hosting/SaaS without separate written permission.

Voluntary donations or transparent infrastructure cost sharing are permitted only within the exact conditions stated in `LICENSE`; payment may not buy access, priority, features, virtual items, ranks, support entitlements, or gameplay advantages.

## Takedown and disputed material

If a distributed artifact is disputed, remove it from active distribution while it is reviewed. Keep source documentation and neutral factual summaries separate from the disputed binary. Restore a removed artifact only after a documented basis for inclusion and any required rights or permissions have been established.

## Operational separation

The public source repository must remain reproducible and free of secrets even when the live service changes providers. DNS, certificates, account stores, and deployment environment are operational concerns and must not be embedded into public packages.
