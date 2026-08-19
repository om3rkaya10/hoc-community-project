# Heroes of Order & Chaos — Community Handbook

An unofficial preservation, compatibility, and community-maintenance handbook for **Heroes of Order & Chaos** Android.

> This repository contains original documentation and the project's independently written Go community server. It does not distribute the original game APK, OBB/data files, native libraries, extracted assets, Gameloft source code, credentials, server access tokens, or raw reverse-engineering dumps.

## Purpose

This handbook records reproducible, high-level knowledge needed to understand and preserve a discontinued online game:

- historical context and project scope;
- client/server architecture at a conceptual level;
- compatibility and installation guidance;
- public-beta operations and incident reporting;
- clean-room observation methodology;
- terminology and troubleshooting.

It is not an official Gameloft repository and is not affiliated with, endorsed by, sponsored by, or supported by Gameloft.

## Repository status

**Private archive candidate — not published yet.** Before any public release, every document must pass the content policy in [`CONTENT_POLICY.md`](CONTENT_POLICY.md).

## License

The original project-authored materials, including the Go server, are available under the
[`HOC Community Server Community Source License 1.0`](LICENSE): free non-commercial use,
forking, modification, redistribution, and free community-server operation are allowed;
attribution and same-license sharing are required; sale, paid access, paid hosting/SaaS,
and other commercial use require separate written permission. This is source-available,
not OSI open source. See [`NOTICE`](NOTICE).

## What is deliberately excluded

- Original APK/OBB/assets/native libraries;
- extracted game tables, scripts, textures, sounds, or maps;
- decompiled proprietary source or copied code;
- private server IPs, SSH details, passwords, tokens, account data, or logs containing credentials;
- exact memory addresses/patch recipes intended to modify a proprietary binary;
- raw packet captures or third-party personal data;
- Gameloft logos, artwork, or official branding beyond nominative reference.

## Contents

- [`HANDBOOK.md`](HANDBOOK.md) — main technical and historical guide;
- [`SERVER.md`](SERVER.md) — original Go server ownership, architecture, and build guide;
- [`PROTOCOL_REFERENCE.md`](PROTOCOL_REFERENCE.md) — sanitized observed state/message reference;
- [`ARCHITECTURE.md`](ARCHITECTURE.md) — conceptual service layers;
- [`COMPATIBILITY.md`](COMPATIBILITY.md) — Android/ABI/device notes;
- [`PRESERVATION_METHOD.md`](PRESERVATION_METHOD.md) — evidence and clean-room workflow;
- [`BETA_OPERATIONS.md`](BETA_OPERATIONS.md) — community testing and incident handling;
- [`CONTENT_POLICY.md`](CONTENT_POLICY.md) — IP/safety boundary for contributions;
- [`SECURITY.md`](SECURITY.md) — secrets and responsible reporting;
- [`THIRD_PARTY_NOTICES.md`](THIRD_PARTY_NOTICES.md) — attribution/disclaimer;
- [`CHANGELOG.md`](CHANGELOG.md) — handbook history.
- [`LICENSE`](LICENSE) — detailed community source license;
- [`NOTICE`](NOTICE) — ownership, attribution, and repository identity;
- [`server/hoc-server/`](server/hoc-server/) — independently written Go backend and tests.

## Legal note

This is a preservation/interoperability project, not legal advice. The Go server is original project property; the original client, assets, and Gameloft marks are not. Copyright, trademark, reverse-engineering, interoperability, and distribution rules vary by jurisdiction. Obtain professional legal advice before public publication.
