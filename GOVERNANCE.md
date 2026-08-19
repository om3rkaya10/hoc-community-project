# Governance, Content, Security, and Third-Party Boundaries

## Project boundary

This is an unofficial preservation/interoperability project. Heroes of Order & Chaos, Gameloft, and related names and marks belong to their respective owners. The repository does not claim ownership of or affiliation with Gameloft.

The independently written Go server and original project documentation are project-authored materials governed by `LICENSE` and `NOTICE`. Their presence does not imply ownership of the original game, client, assets, or trademarks.

## Allowed content

- original project prose, diagrams, Go source, tests, packet builders, and operational abstractions;
- high-level descriptions of observed client behavior;
- interoperability concepts and protocol state-machine descriptions;
- technical identifiers and independently implemented field/state descriptions needed for compatibility;
- links to official/public sources;
- redacted reproducibility notes and contributor-owned material that does not expose third-party assets or personal data.

## Prohibited content

- APK/IPA/OBB/SO/DLL files, extracted assets, game tables, scripts, textures, sounds, maps, or copied/decompiled client code;
- Gameloft logos or artwork used as project branding;
- credentials, tokens, private keys, certificates, account databases, IP allowlists, provider controls, or production environment files;
- raw PCAPs, unredacted logs, memory dumps, exact patch bytes/addresses, or raw Frida patch scripts;
- instructions to bypass payment, access controls, anti-cheat, or third-party services;
- claims that the project is official or authorized.

When uncertain, keep the material out and publish only a neutral factual summary.

## Security

Never commit passwords, tokens, SSH/private-key material, account databases, production configuration, raw logs with credentials, client binaries, or unredacted captures.

If a secret is exposed:

1. revoke or rotate it immediately;
2. remove it from working files and history where appropriate;
3. inspect logs/access records;
4. do not paste the secret into a public issue.

Use public issues only for redacted reproducible facts. Use a private channel for infrastructure/security details.

## Licensing and contributions

Forks and redistributions must preserve `LICENSE`, `NOTICE`, attribution, modification notices, and the same source-available terms. Commercial sale, paid access, paid hosting/SaaS, or monetized use requires separate written permission.

Contributors must own or have the right to submit their work and must not submit third-party proprietary code, leaked material, client assets, credentials, or personal data. Unless a separate written agreement applies, contributions are made under the repository license.

## Trademarks and external links

Names may be used only for truthful identification, compatibility, attribution, and historical reference. Do not imply endorsement or official status. External links should point to official or publicly accessible sources and must not be used to redistribute copyrighted game files.

## Disputed material and takedown

Remove disputed material from distribution while it is reviewed. Preserve a neutral factual summary when appropriate. Restore the material only after the project has a documented basis for inclusion and any applicable rights or permissions.

## Responsibility

This project is published as-is. Each contributor, redistributor, operator, and user remains solely responsible for evaluating and complying with the laws, regulations, licenses, and third-party rights applicable in their own jurisdiction.

## Related documents

- `LICENSE` — complete source-available license
- `NOTICE` — ownership, attribution, repository identity
- `DEVELOPMENT.md` — contributor and testing workflow
- `RELEASE.md` — release/distribution policy
- `VALIDATION.md` — evidence and preservation method

## Maintenance rule

Update this document instead of creating additional overlapping content/security/legal-policy files.
