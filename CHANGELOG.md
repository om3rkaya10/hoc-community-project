# Changelog

## Client — Global Public Beta 0.3.1 arm64-v8a — 2026-09-16

- arm64-v8a client: private messages never rendered. `ChatSession::CreateRunThread` requests `SCHED_RR` with priority 0 for the game-side chat thread; bionic rejects that `sched_setscheduler` call and, in 64-bit processes only, fails the `pthread_create` ("for backwards compatibility reasons, we only report failures on 64-bit devices"), so the thread that drains the chat library's queue never started. The 64-bit build now requests `SCHED_OTHER`; PM, the new-message light and invitations behave as on 32-bit. The bug exists in the original 64-bit client too. 32-bit APK unchanged.

## server-v0.1.7 — 2026-09-16

### Hotfix: private chat over the public server

- Private chat: every message after the first one in a conversation was silently dropped by the client when played over the public server. The client keeps a per-conversation set of message ids but compares only their first four characters, so ids that shared a prefix were treated as duplicates. Message ids are now short fixed-width base-36 counters that always differ in the compared bytes and are not replayed after a server restart.
- Private chat: the listen stream is sent with explicit identity framing (the client's chat reader has no chunked-transfer decoder), a 5 s heartbeat inside the client's 10 s per-line timeout, and stays open instead of being rotated; messages posted while a client is between connections are queued and delivered on reconnect.
- Notifications: the alert-stream keepalive is a real `keepalive` event instead of an SSE comment line, which the client's parser was not proven to tolerate over WAN.
- Regression coverage for the chat flow over real HTTP connections: room_info first, reconnect key preserved, distinct id prefixes and counter rollover, POST echo matching the streamed document, fan-out to two subscribers.
- Known issue at release time: private messages did not render in the **arm64-v8a** client build — resolved by client build Global Public Beta 0.3.1 arm64 (see above).

## server-v0.1.6 — 2026-09-15

### Hotfix: friends list, presence and private chat

- Friends: the client's Gaia Osiris calls are now served instead of stubbed. Players can send friend requests (by in-game nickname or login name), see incoming requests in the message box, accept or ignore them, list friends and remove them. Crossing requests resolve straight into a friendship. Friend data is persisted per account.
- Friends: the batch profile lookup returns nickname, icon and signature in the shape the client parses, so friend entries and request messages show real names instead of a blank or a phantom entry.
- Notifications: `/alerts/me` is a real Kairos event stream, so a friend request or a chat invitation reaches the other player immediately (no re-login needed).
- Presence: the lobby QueryUser request (`0xe00e`) is answered with each friend's live state — offline, online, or in a match — so the friends list no longer shows everyone as offline and PM is no longer refused with "user is offline".
- Chat: the Arion group-chat service (`/chat/rooms/...`) is implemented — room subscribe, streamed listen with heartbeats, message send, and P2P invitations — which makes friend private messages work end to end, including the new-message light in the main menu.
- Regression coverage for friend request flows (create, accept, reject, cancel, crossing requests, persistence) and nickname-first player lookup.
## Client — Global Public Beta 0.3 arm64-v8a — 2026-09-14

- Added an `arm64-v8a` build of the Public Beta 0.3 client for devices without 32-bit support. Same server, same fixes; distributed as a separate APK from the 32-bit one.

## Hotfix — 2026-09-14 (on top of server-v0.1.5)

### Lobby chat, in-match chat and minimap ping

- The game server now relays custom-room lobby chat (`0x1003`), in-match chat and minimap pings (`op8`); they were previously dropped, so no player saw them.
- In-match chat and pings are stamped with the room frame clock before relay; relaying the sender's own frame made the client log out (`Dev|3004`).
- Team-scoped messages (aim `0x800`) reach only the sender's team half; all-chat (`0x4000`) reaches the whole room.
- Added regression coverage for scope routing, frame stamping and body layout.

## server-v0.1.5 — 2026-09-14

### Inventory v1: tablets, shop purchases, flags, in-match passives

- Kitabe: the server now addresses tablets in the client's own index space (position in the owned tablet vector, carried in `TabletSlot[4][0]`), so reopen (unlock) and delete act on the tablet the player tapped; the 0x53 delete request is read from its real field.
- Kitabe: the reopen dialog accepts the Rune option (20) as well as 750 Emblem, and the prices are sent explicitly in GetUserInfo.
- Kitabe: delete removes the tablet from the per-account owned list (inscriptions return to the inventory); a deleted tablet can be bought again. Equipped tablets no longer appear as locked duplicates in the backpack.
- Shop: purchases are routed by prototype type from a generated item catalog. Bundles hand out their real contents; Emblem/Rune rows credit the wallet; consumables land in the ItemInfo inventory; poles and banners are owned in the Flags screen. The debit is the request's line total (multi-count banner packs are charged once).
- Shop: accounts that bought any of these before this release are migrated once (`inventory_version=1`); everything previously stored in the inscription map is converted to what was actually purchased.
- Flags: every BuyItem/BuyItemCRM reply carries the flag ownership block the client assigns unconditionally, GetUserInfo carries it at login, SelectFlag persists the choice and replies with a success result, and in-match UseFlag consumes one banner charge.
- Flags: the Flags tab no longer hangs on a spinner — the post-login lobby push carries every child the client's guild-login handler requires.
- Match: LoadMap PlayerInfo now carries the awake tablets with their socketed inscriptions, the most-invested talent page, and the selected pole/banner (with charge count), so tablet passives and the battle banner exist in the match.
- Regression coverage for owned-index round trips, rune/emblem reopen, delete field order, typed purchases and pack expansion, migration idempotence, flag blob layout, guild-login-complete children, and PlayerInfo flat indexes.

## server-v0.1.4 — 2026-09-13

### Wallet display and post-login bootstrap fixes

- Corrected the BuyItem/BuyItemCRM wallet field order to the client's actual mapping (rune at `[4]`, emblem at `[5]`); the previous layout placed gems at `[4]`, so every purchase or return-to-lobby reply switched the rune HUD to 99999.
- Kept the client's post-login bootstrap (alerts, device registration, CRM catalog) running with a true rune value: the GetUserInfo that precedes the login BuyItem now reports rune minus one so the client observes a non-zero wallet delta; the BuyItem that follows sets the true value. A zero delta at login left shop and hero-select item requests timing out.
- Added regression coverage for the wallet index order and the login-inject rune delta.

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
