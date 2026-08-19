# Release and Package Policy

## Purpose

This repository is the canonical source and documentation repository for the HOC Community Server project. It is not a repository for the original game client or original game data.

## What belongs in this repository

The following may be committed when they comply with `LICENSE`, `NOTICE`, and `CONTENT_POLICY.md`:

- original Go server source and tests;
- original documentation and diagrams;
- redacted protocol/state references;
- build instructions and reproducibility notes;
- source checksums and text manifests;
- changelogs and incident notes with secrets and personal data removed.

## What does not belong in this repository

Do not commit or attach to normal source commits:

- original or modified APK/IPA/OBB files;
- native libraries, extracted assets, game tables, scripts, maps, textures, audio, or video;
- decompiled client code or binary patch artifacts;
- raw Frida scripts, memory dumps, absolute patch addresses, or raw PCAPs;
- production certificates, private keys, account stores, passwords, tokens, provider details, or unredacted logs;
- user-submitted screenshots containing personal data or credentials.

The source repository and any client artifact distribution are separate release surfaces.

## Versioning

Use a human-readable release identifier:

```text
Global Public Beta 0.1
```

For future server source releases, use a repository tag such as:

```text
server-v0.2.0
```

A release should document:

- release date;
- source commit;
- server compatibility expectations;
- known issues;
- migration or rollback notes;
- checksums for any separately distributed project-authored artifact.

The first server source release is planned as `server-v0.1.0`. Its Linux amd64 binary, release notes,
and checksum manifest are generated locally for upload as release assets and are intentionally excluded
from ordinary source commits.

## Client artifact policy

The client artifact is not part of the source tree. If a client package is distributed by the project, its release page must state:

- exact file name and version;
- SHA-256 checksum;
- supported ABI/device profile;
- installation notes;
- known compatibility issues;
- whether the file contains or depends on third-party material;
- an unofficial-project disclaimer;
- a clear route for reporting a disputed or unavailable file.

Do not describe a client artifact as an official Gameloft release. Do not bundle original OBB/data files in this repository.

## Server package policy

A server source release may include the original Go source and tests under the HOC Community Server Community Source License. A runnable server package must never include:

- `accounts.json`;
- `.env` files;
- TLS private keys;
- production host/IP configuration;
- live access tokens;
- real player logs.

Operators must supply runtime configuration separately.

The repository also contains a Dockerfile and a GitHub Actions workflow that build the original Go
server from a tagged source commit and publish a Linux/amd64 container to GitHub Container Registry.
The workflow runs the Go build, vet, and test gates before publishing. It never receives client APK/OBB
files or production runtime secrets.

## Reproducibility

Before tagging a server release:

```text
go build ./...
go vet ./...
go test ./... -count=1
git diff --check
```

Record the source commit and verify that the working tree contains no runtime secrets or generated binaries.

## Rollback

Keep the previous known-good server commit and runtime configuration outside the repository's public tree. If a release regresses:

1. announce maintenance or rollback status;
2. stop or drain the affected service;
3. restore the known-good server artifact/configuration;
4. verify health endpoints and listeners;
5. record the incident without publishing secrets or personal data.

The hostname/DNS layer should be preferred for infrastructure migration so a new client build is not required merely because the server moves.

## Attribution and license

Every source or binary redistribution of the original project-authored server material must retain `LICENSE` and `NOTICE`, include the required attribution, identify substantial modifications, and remain within the non-commercial terms of the HOC Community Server Community Source License.

Commercial sale, paid access, paid hosting/SaaS, or monetized redistribution requires separate written permission from the project owner.

## Published-as-is responsibility

This project is published as-is. Each contributor, redistributor, operator, and user remains solely responsible for evaluating and complying with the laws, regulations, licenses, and third-party rights applicable in their own jurisdiction.

## Status

The repository may be public while client artifact distribution remains a separate operational decision. Publishing source does not grant rights to third-party client files, assets, trademarks, or original game data.
