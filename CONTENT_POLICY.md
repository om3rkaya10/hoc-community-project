# Content and IP Policy

## Goal

Keep this repository useful as a preservation handbook without redistributing material owned by Gameloft or other third parties.

## Allowed by default

- original prose written for this repository;
- original Go server source, tests, packet builders, and operational abstractions authored by the project;
- generic diagrams created by contributors;
- high-level descriptions of observed client behavior;
- interoperability concepts and protocol state-machine descriptions without copied payload dumps;
- links to official/public sources;
- contributor-owned screenshots with no copyrighted game artwork, personal data, tokens, or account details;
- reproducibility notes that do not reveal secrets or ship proprietary binaries.

## Not allowed

- APKs, OBBs, SO/DLL files, extracted assets, game tables, Lua/script bundles, textures, sounds, maps, or copied decompiled code;
- original Gameloft logos/artwork presented as project branding;
- credentials, access tokens, private keys, account databases, IP allowlists, or provider control details;
- raw packet captures containing user data or authentication material;
- exact binary patch instructions, memory addresses, or Frida scripts for modifying a proprietary client;
- claims that the project is official or authorized;
- instructions to bypass payment, access controls, anti-cheat, or another party's service.

## Review rule

Technical message identifiers and independently implemented field/state descriptions are allowed when they are necessary to explain interoperability. Gameloft binary code, assets, decompiled functions, patch bytes, or extracted tables are not. A private GitHub repository is not automatically safe to publish later.

## Licensing rule

Project-authored material is governed by `LICENSE` and `NOTICE`. A fork may not remove attribution,
convert the project code to a commercial/proprietary license, or sell/monetize the server without
separate written permission. Contributions must be owned by the contributor and submitted under
the repository license unless a separate written contributor agreement applies.

## Takedown/contact

Remove disputed material from distribution while it is reviewed, preserve only a neutral factual
summary when appropriate, and restore the material only after the project has established a documented
basis for its inclusion and any applicable rights or permissions.
