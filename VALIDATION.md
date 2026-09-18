# Validation and Preservation Method

## Purpose

This document defines how the project observes client behavior, classifies evidence, and records real-client validation. It deliberately separates deterministic code tests from historical or real-device claims.

## Clean-room posture

The project records externally observable behavior and writes independent server implementations. Contributors must not copy proprietary code or redistribute original game data.

## Evidence workflow

1. State the question precisely.
2. Identify the account, lobby, game-server, or match-runtime layer under test.
3. Capture the smallest useful observation.
4. Remove credentials and personal data.
5. Reproduce locally where possible.
6. Assign an evidence level.
7. Record what would falsify the conclusion.

Useful measurements include endpoint response shapes without tokens, room-state transitions, frame-clock distributions, TCP RTT ranges, device/ABI results, and the exact user-visible failure state.

## Evidence levels

| Level | Meaning | Evidence expected |
|---|---|---|
| V0 | Static/documentary | source inspection, historical note, or archived observation |
| V1 | Unit/regression | deterministic test in `server/hoc-server` |
| V2 | Local integration | multiple server layers exercised without the original client |
| V3 | Real client, local/LAN | real Android client reaches the documented state |
| V4 | Real client, WAN | real Android client reaches the state over a public route |
| V5 | Cross-geography public beta | independent player/ASN completes the lifecycle |

A claim must identify its level. V0/V1 evidence must not be presented as real-client proof. Use `hypothesis` or `parked` when the evidence is insufficient.

## Historical validation scope

The project owner reports approximately **2,500 real client/server interaction cycles** during debugging, protocol discovery, regression checks, and feature validation. This is a project-history statement, not an independently audited counter.

The principal validated lifecycle is:

```text
account/edge bootstrap
  → lobby connection
  → custom room create/join
  → game-server advertisement and login
  → seat roster and hero selection
  → ready
  → shared LoadMap
  → StartPlay
  → shared match frames
  → movement/skills
  → leave/reconnect behavior
```

## Recorded milestones

### Local and Nox — V3

Historical records describe repeated Nox and dual-Nox validation of custom rooms, seat/hero state, ready/load/start, two-player shared matches, survivor behavior, reconnect replay, the shared 30 Hz frame clock, and movement/skill relay.

Additional V3 profile validation covered:

- all four talent classes visible with one shared 40-point budget;
- spending the full budget, saving, and reopening with zero remaining points and no duplicate grant;
- a starter Revival Rune quantity visible after a cold login;
- normal inscription exchange consuming four identical lower-tier inscriptions and producing one matching next-tier inscription;
- reopening a filled tablet for 750 Emblem on a clean account, with the exact debit persisted and the tablet returning to an editable state;
- stable card-to-tablet targeting across page and equipped views, without opening a different tablet;
- inventory persistence after leaving and reopening the relevant UI;
- (server-v0.1.4, Nox against the public server) the rune counter staying at its true value after Emblem purchases in the shop and after returning from a match, while the shop and hero-select catalogs still load after a cold start and after an in-game logout/re-login.
- (server-v0.1.5, Nox against the public server) deleting a backpack tablet removes exactly the tapped tablet; reopening a locked tablet with the Rune option and with the Emblem option; an Emblem Pack bought with Runes crediting Emblems; a potion appearing in the Items tab; a purchased pole and banner selectable in the Flags screen, Save succeeding, the selection surviving re-login and a shop purchase; the Flags tab opening without a spinner; and, in a custom-room match, the equipped tablet's passive and the selected battle banner being present.
- (server-v0.1.7, public server, a 32-bit phone client and a Nox client on two different accounts) opening a private chat from the friends list on both sides and exchanging several messages in each direction, every message rendering on both clients including the sender's own; a message posted while one client was reconnecting delivered on reconnect. The same phone on the arm64-v8a client build rendered none of the messages (recorded as a known client-side issue).
- (server-v0.1.8, Nox against the public server) changing the two summoner spells twice in the match-setup screen and pressing START: every SkillAck the server logged carried real spell ids (941/603, then 942/597) for both body sizes the client sends (584 B with an empty PlayerInfo guid, 613 B with it filled), the last pair going into the LoadMap PlayerInfo; before the fix the same account's SkillAcks logged 3584/3840-style garbage on most attempts.
- (client Global Public Beta 0.3.2, Samsung Remote Test Lab Galaxy A36 5G, Android 16) a solo 3v3 match loading past the point (72.5 %) where builds 0.3 / 0.3.1 crashed with `SIGSEGV SEGV_ACCERR` in `TerrainTiled::GetHeight`, with the 64-bit and then the 32-bit APK installed on the same device.
- (server-v0.1.9, Nox against a local build, one account) equipping tablets on several pages without any of them locking; a tablet with both energy bars full ascending from EDIT TABLET → SAVE → OK (blue icon, "This tablet has been ascended and locked", SAVE turned into UNLOCK), unlocking it for 750 Emblems and again for 20 Runes with the exact debits persisted, and re-ascending it afterwards; a tablet with a partially filled bar saving without the ascend prompt; the loft showing no socket-marker flicker on empty tablets; four different silver inscriptions exchanging into a gold one with the CONGRATULATIONS card, four bronze into a silver, and four consecutive exchanges producing four different results; the gold 1↔1 swap consuming the source gold, granting the target and debiting 10 Runes; buying a tablet already owned adding a second copy at the end of the loft, both copies sitting on the same page, an inscription mounted on the second copy leaving the first (ascended) copy untouched, and deleting the second copy returning its inscription and leaving the first in its slot; a legacy account store migrating to the copy-based inventory with ascension, sockets and slots preserved.
- (client Global Public Beta 0.3.3, Redmi Note 13 / Android 15, arm64, over the public server) tapping every tablet card on every page, repeatedly scrolling the loft drawer and re-entering the tablet screen without the `Hero::GetCampTypeColor` crash that the same phone produced on 0.3.2 within a minute of browsing.
- (server-v0.1.6, two Nox clients against a local build) sending a friend request by nickname, the request appearing on the other client immediately with the sender's name, accepting it and both friends lists updating; the friend shown as online in the friends list; opening a private chat from the friends list and exchanging messages in both directions; a private message arriving while the recipient is in the main menu lighting the friends button.

The matching V1 suite verifies valid-zero handling, deterministic talent clamping, idempotent account migration, authoritative inventory replay, atomic exchange, rejection of mixed or insufficient source inventories, idempotent tablet reopen, owned-index round trips, delete request field order, typed purchases and pack expansion, one-shot inventory migration, flag ownership blob layout, guild-login-complete children, PlayerInfo flat indexes, duplicate equipped-tablet normalization, ascend-flag handling and reply echo, four-socket TabletInfo, tier-based random exchange with mixed-tier and insufficient rejections, gold swap request layout and rejections, tablet copies addressed by owned index across wear/fill/delete, second-copy purchase, legacy-to-copy inventory migration, friend request lifecycle (create, accept, reject, cancel, crossing requests, persistence), and nickname-first player lookup.

### Physical phone over WAN — V4

A physical Android phone completed authentication, lobby, custom-room, game-server login, ready, LoadMap, StartPlay, and gameplay without PCAPdroid, VPN, or proxy. Server-side timing and a separate TCP capture showed regular frame pacing in the tested environment. This is not a universal device or latency guarantee.

### Cross-geography public beta — V5

A player connecting from Saudi Arabia through a Mobily/Etihad Etisalat route completed:

```text
authenticate
  → lobby join
  → custom room membership=2
  → hero selection and READY=1
  → host ready
  → shared LoadMap
  → both players StartPlay
  → shared op11 with members=2
```

The observed game-server RTT was approximately 115 ms. The project goal is playable interoperability, not perfect latency for every geography.

## Supported login path

A normal beta username/password path is the canonical validation path. Guest/device identities may reach the lobby but are not the canonical custom-room ready path and should be reported separately.

## Repeating a real-client run

Record:

1. client/build identity;
2. server commit/tag;
3. device and Android version;
4. country, ISP/carrier, and Wi-Fi/cellular path;
5. time and timezone;
6. last confirmed lifecycle state;
7. redacted server evidence;
8. user-visible result;
9. clean, partial, or failed outcome.

## Redaction and publication boundary

Do not publish raw PCAPs, unredacted journals, passwords, tokens, private keys, player IPs, account data, provider credentials, Frida patch scripts, absolute memory addresses, original assets, or extracted tables.

The private research archive may retain evidence locally under access control, but it is not automatically suitable for publication.

## Limits of this record

This record does not claim universal device compatibility, universal ISP/ASN validation, an official Gameloft specification, or an independently audited interaction count. Every new behavior claim still requires appropriate evidence.

Documentation-only changes do not require a fresh live run. Changes to wire behavior, runtime lifecycle, persistence timing, or client-visible state require the appropriate V3/V4/V5 regression before being called LIVE.
