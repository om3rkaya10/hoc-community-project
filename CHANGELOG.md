# Changelog

## Unreleased — handbook seed

### Server profile and Kitabe fixes

- Exposed all four talent classes while preserving one shared 40-point budget.
- Preserved an exhausted talent balance of zero across save/reload instead of treating zero as a missing default.
- Added deterministic normalization for over-budget talent presets.
- Added an idempotent starter quantity of Revival Runes for new and legacy accounts; login does not add the starter amount repeatedly.
- Implemented normal inscription tier exchange as an atomic four-source-to-one-target transaction.
- Rejected mixed-source, unknown-recipe, malformed, and insufficient-inventory exchange requests without mutation.
- Added regression tests for profile normalization, inventory replay, talent limits, and inscription exchange.

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
