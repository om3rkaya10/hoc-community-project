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

**Public source-available community repository.** The canonical repository is
[`om3rkaya10/hoc-community-project`](https://github.com/om3rkaya10/hoc-community-project).
All published material must comply with [`GOVERNANCE.md`](GOVERNANCE.md).

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

- [`HANDBOOK.md`](HANDBOOK.md) — project history, principles, architecture, and server overview;
- [`PROTOCOL_REFERENCE.md`](PROTOCOL_REFERENCE.md) — sanitized observed message/state reference;
- [`COMPATIBILITY.md`](COMPATIBILITY.md) — Android ABI and device compatibility;
- [`VALIDATION.md`](VALIDATION.md) — preservation method and real-client evidence;
- [`DEVELOPMENT.md`](DEVELOPMENT.md) — Go server architecture, testing, and contributing;
- [`RELEASE.md`](RELEASE.md) — releases, packages, distribution, beta operations, and rollback;
- [`GOVERNANCE.md`](GOVERNANCE.md) — content, security, licensing, and third-party boundaries;
- [`CHANGELOG.md`](CHANGELOG.md) — repository history;
- [`LICENSE`](LICENSE) / [`NOTICE`](NOTICE) — complete terms and attribution;
- [`server/hoc-server/`](server/hoc-server/) — independently written Go backend and tests.

## Responsibility and legal status

The Go server and original project documentation are project-authored materials; the original client,
assets, and Gameloft names and marks are not. This project is published as-is. Each contributor,
redistributor, operator, and user remains solely responsible for evaluating and complying with the
laws, regulations, licenses, and third-party rights applicable in their own jurisdiction.
