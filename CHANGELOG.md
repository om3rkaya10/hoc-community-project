# Changelog

## server-v0.1.3 — 2026-08-23

### Tablet reopen and equipment-state fixes

- Implemented the 750-Emblem tablet reopen transaction with exact wallet persistence.
- Made repeated successful reopen requests idempotent so they do not charge twice.
- Preserved one stable packet identity for each equipped tablet across the equipped list and page/card views.
- Resolved reopen targets through the client-returned equipped packet index instead of treating it as a page-local slot.
- Prevented one tablet item from being equipped in multiple slots; equipping it again now moves it.
- Added deterministic legacy-account normalization for duplicate equipped-tablet records.
- Added regression coverage for insufficient balance, packet-index round trips, duplicate prevention, and legacy cleanup.

## server-v0.1.2 — 2026-08-23

### Server profile and Kitabe fixes

- Exposed all four talent classes while preserving one shared 40-point budget.
- Preserved an exhausted talent balance of zero across save/reload instead of treating zero as a missing default.
- Added deterministic normalization for over-budget talent presets.
- Added an idempotent starter quantity of Revival Runes for new and legacy accounts; login does not add the starter amount repeatedly.
- Implemented normal inscription tier exchange as an atomic four-source-to-one-target transaction.
- Rejected mixed-source, unknown-recipe, malformed, and insufficient-inventory exchange requests without mutation.
- Added regression tests for profile normalization, inventory replay, talent limits, and inscription exchange.

## Unreleased — handbook seed

- Created a documentation-first, private-ready handbook structure.
- Added IP/content boundary, compatibility notes, architecture, preservation method, beta operations, and security policy.
- Deliberately excluded original binaries, extracted assets, raw RE material, credentials, and provider operations.
- Added the independently written Go server source and tests.
- Added a sanitized observed protocol/state reference.
- Declared server ownership and an all-rights-reserved status pending a deliberate license decision.
- Replaced the temporary all-rights-reserved notice with HOC Community Server Community Source License 1.0.
- Allowed non-commercial use, forks, modifications, free redistribution, and free community servers with attribution and same-license obligations.
- Prohibited sale, paid access, paid hosting/SaaS, and other commercial use without written permission.
- Added release and distribution policy separating the original Go server source from client APK/OBB/artifact distribution.
- Added real-client validation records, evidence levels, testing guide, and contributor workflow.
