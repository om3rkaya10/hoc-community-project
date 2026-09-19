# Changelog

## Client — Global Public Beta 0.4 (armeabi-v7a and arm64-v8a) — 2026-09-19

- **60 fps.** The original client caps its render loop at 30 fps and steps the lockstep logic in fixed 33 ms increments: `NGDataPtl::HandleGamePlayFrame` pushes a hard-coded 33 ms dt per received op11 frame, `GS_GamePlay::EstimateExeFrames` paces execution by wall clock in 33 ms units, `NGDataPtl::_btpf` / `g_tartget_fps` and `Game::DoFrame` bound the in-game frame time from below, and on top of all that the Java `GLSurfaceView` thread sleeps to a 30 ms frame budget. The 0.4 build changes every one of those constants to 16 ms (the ×33 / ÷33 arithmetic in the pacer becomes shifts) and the Java budget to 16 ms, and pairs with a server room ticking at 62.5 Hz. Game logic is dt-driven, so the game runs at exactly the same speed (match clock measured 1:1 against wall time); movement, animation and camera render at 60 fps, and the input-latency floor (one frame lead) halves. Verified on Nox: 60 fps median frame time, two-player lockstep with no sequence errors or reconnects, and a 60 Hz arm64 build under a simulated 160 ms RTT with 1 % loss. Frame-counting cosmetics (some particle emitters) look denser.
- **Build marker.** `GetSimpleGameBuildVersion()` returns the literal `3.5.2b` instead of `[App] Version` from the downloaded `game_Android.conf` (`3.5.2a`). The client already sends that value in the game-server `LoginReq` and as the `custom_<build>` attribute of every custom-room create and search, so `server-v0.1.11` keeps 0.4 and 0.3.x players in separate rooms (a mixed match would run at half or double speed for everyone) and ticks 0.4 rooms at 16 ms. 0.3.x clients are unaffected and keep working; the two versions simply do not see each other's rooms.
- Native libraries differ from 0.3.3 at 17 words per ABI (the frame constants and the four-word marker stub); `classes.dex` differs only in the `GLSurfaceView` frame-budget field.

## server-v0.1.11 — 2026-09-19

### Lockstep rate per room (60 Hz client support), profiling instrumentation

- The match clock is now a property of the room, chosen from the client build that created it, so a future 60 fps client (16 ms lockstep frames, 62.5 Hz) can be rolled out without touching players on the current client. The original client already reports its build twice — as a UTF field in the game-server `LoginReq` (after the token) and as the room attribute `custom_<build>` in every custom-room create and search (Gameloft's own version matchmaking, which this server used to ignore) — and the value comes from `[App] Version` in the downloaded `game_Android.conf` (`3.5.2a`). The server now records that build on the room, lists in a room search only the rooms of the searcher's lockstep class, refuses a join or a game-server login that would mix the classes, and drives the room's frame ticker at the class's period: `3.5.2a` (and any unknown build) keeps the 30 Hz clock exactly as before; the build named by `HOC_BUILD_60HZ` (default `3.5.2b`, the value the 60 fps client reports) gets `HOC_FRAME_MS_60HZ` (16 ms). Two rooms of different classes run side by side on one server. Nothing changes for the current client: its rooms, searches and matches behave as in `server-v0.1.10`. Why the classes must never mix: the client executes one fixed-dt logic step per received frame, so a 33 ms client in a 16 ms room runs the match at half speed and a 16 ms client in a 33 ms room at double speed, for everyone.
- `HOC_FRAME_MS` / `HOC_FRAME_HZ` set the default period for local testing (unset = the previous `time.Second/30`).
- Match-lag profiling (`HOC_PROFILE=true`, off by default): every 10 s the log carries the per-tick socket write time (p50/p95/max, the slowest socket and whose it was, the wait for the room wire lock), op7 relay latency and the cost of its log line, in-match write timeouts, `accounts.json` rewrites, GC/heap/goroutines and process CPU. `HOC_PPROF_ADDR=127.0.0.1:6060` serves `net/http/pprof` (loopback addresses only). Profiling builds also dump the raw `LoginReq` and the lobby packet fields once per connection. Measured with it on the public server on 2026-09-19 with a 3v3 (five emulators and a phone) and `tc netem` on the phone's downlink (250 ms ± 80 ms, 10 % loss, four 5 s blackouts): the write loop for six sockets stays under 1.5 ms, no write ever blocked (the kernel buffer absorbs 30 Hz frames for minutes), tick spacing max 33.354 ms; the only effect of a blackout was the client closing its own socket after ~5 s of silence and rejoining through the `server-v0.1.10` reconnect path (four of four resumes clean, unnoticed by the player). The suspected head-of-line blocking on the room wire lock does not occur.
- Regression coverage: build parsed from a captured `LoginReq` (stock and 60 Hz values), the `custom_<build>` attribute extracted from a nested lobby attribute block, class matching and the period per class; the profiler naming the stalled peer of a tick and counting its write deadline.

## Client — Global Public Beta 0.3.3 (armeabi-v7a and arm64-v8a) — 2026-09-18

- The game closed while browsing the tablet page (tapping certain tablet cards; reported as an intermittent crash on Nox and reproduced with a symbolized tombstone on a Redmi Note 13 / Android 15, arm64). Tapping a card builds the tablet's description by learning its passive on the lobby player (`DlgTabletPage::RefreshDescription` → `Player::EquipTablet` → `SpellStorage::LearnSpell`); some passives fire an immediate trigger whose spell script spawns a visual effect, and `SpellEffect::Init` asks the caster's `Hero` for its camp colour. `Hero::GetCampTypeColor(Unit*)` guarded only the `Unit` argument, and on the community server the lobby has no `Hero` object (the main menu's 3D hero panel that creates one is not populated), so `this` was NULL (`SIGSEGV`, fault address 8 on arm64 / 4 on arm32, in `GLThread`). The function now returns the "no camp" colour when called without a `Hero`. Both APKs rebuilt; the native libraries differ from 0.3.2 only inside that function. Verified on the Redmi (every card on every page, repeated drawer scrolling and re-entry). The original client never hit this because Gameloft's lobby always had the hero model loaded.

## server-v0.1.10 — 2026-09-19

### Hotfix: mid-match reconnect, summoner spells, tablet pages

- Reconnecting into a running match failed whenever the player's network dropped without closing the connection (Wi-Fi → mobile hand-off, signal loss — the common case on phones). The server keeps a reserved-seat *hold* only once it sees the old game socket close; a peer that vanishes without a TCP reset leaves that socket ESTABLISHED for minutes (the 30-byte frames never fill the send buffer, so the 2 s match write deadline never fires and Linux retransmits for ~15 min), so no hold existed and every `ReLoginReq` from the new connection was rejected with `no-hold` until the client gave up (public-server journal, 2026-09-18 20:50: three rejections over 30 s, the room still emitting frames into the dead socket). Two changes: a `ReLoginReq` whose owner is still attached in a running match now takes the seat over — the session enters the hold, the stale transports are closed out of band without running the disconnect path, and the request is served on the new connection; and game sockets get TCP keepalive plus, on Linux, `TCP_USER_TIMEOUT` (10 s, `HOC_GS_USER_TIMEOUT_SEC`) so a silently vanished peer surfaces as a disconnect in seconds even without a retry from the client.
- The reconnect flap guard no longer deletes the seat. After two Ack→immediate-EOF cycles the server refuses further `ReLoginReq`s without an Ack to stop the client's spam, but it used to leave the room on that refusal — 4 s into a 90 s hold — while the client went on retrying calmly for another 50 s, every attempt dying with `no-hold` (journal, 2026-09-18 20:33). The refusal now keeps the hold; once the last failure is older than the cooldown (5 s, `HOC_MATCH_RELOGIN_FAIL_COOLDOWN_SEC`) the counter resets and the next attempt gets a real Ack again. The hold TTL alone decides when the seat is given up.
- Solo matches could never be rejoined at all, for two independent reasons found while reproducing the above on Nox (airplane mode mid-match): the solo LoadMap path never marked the room as in a match (`State` stayed `open`, and the hold claim requires `match`), and the room clock disarmed on the first tick after the only player entered the hold (no playing member), which the hold claim also requires. A solo room is now marked in-match on its LoadMap, and a member in reconnect hold keeps the clock armed (frames keep filling the replay ring the rejoiner is caught up from) until the hold expires. The rejection log now names the failing condition instead of a bare `no-hold`.
- The resume no longer rewinds the room clock when the replay ring can deliver the missed range. With every peer frozen (a solo match, or all players reconnecting) the server rewound the clock to the client's cursor *and* replayed the ring, so the client received the old packets and then a second, lower live sequence right behind them, rejected it, and re-requested from the same cursor until the flap guard fired — the exact loop seen on the public server on 2026-09-18 20:33 (`ReLoginAck` → op3 → replay → EOF, three times in a second). The rewind remains only as the fallback for a gap older than the ring (~4.5 min).
- Regression coverage: takeover of a still-attached owner (hold created, stale handler exits without clearing it, Ack on the new socket, no soft-fail counted, clock still armed), takeover refused for a foreign GUID, refusal keeping the seat and Acking again after the cooldown, a solo LoadMap marking the room in-match, a held solo member keeping the clock armed and the ring filling, no rewind while the ring covers the request and the rewind fallback for an older gap; low summoner-spell ids parsed, remembered pair restored and carried by LoadMap; the same copy on two pages kept, same-page duplicate moved, migration binding a second page to the same copy.
- Validated on Nox against a local build (see `VALIDATION.md`): a solo match surviving a 15 s silent drop, a second drop in the same match, and a 60 s drop (2,492-packet replay), each rejoined on the first request.
- Summoner spells were still random for many players after the `server-v0.1.8` parser fix. The parser rejected any spell id below 100, and the client reports the level-1 Heal (id 34) together with the level-1 Mana Regen (593) whenever the player has not touched the picker — the most common case — so the pair was dropped, the seat stayed at 0/0 and LoadMap sent 0/0, which the match fills randomly (public journal: 195 of ~400 SkillAcks logged as 0/0, 8 matches started that way). Ids from 1 are accepted now; the raw pair is logged on every SkillAck. In addition the server remembers the last real pair per account (`summoner_spells`) and uses it, or the 34/593 default, when a SkillAck genuinely carries none. Confirmed on Nox: an untouched picker now starts the match with Heal + Mana Regen, a picked pair (941/600) is applied and persisted.
- The same tablet copy can be equipped on several pages again. Pages are alternative loadouts (one is active in a match), but `EquipTablet` had evicted the copy from every other slot since `server-v0.1.3`, so equipping a tablet on page 2 silently emptied its slot on page 3 (player video, 2026-09-18). A copy is now unique only within a page; the inventory migration and the duplicate normaliser follow the same rule and bind the same copy to a second page instead of dropping it. `cmd/restore-tablet-pages` re-creates the slots the `server-v0.1.9` migration dropped from the pre-v0.1.9 backup (one slot on the public server). Trashing a tablet still discards it from the loft, as in the original client (its remove-from-slot request is dead code).

## server-v0.1.9 — 2026-09-18

### Kitabe: ascension, inscription exchange, empty-socket flicker, tablet copies

- Ascension is now what the client means by it. A tablet becomes *ascended* (the blue, locked state whose passive works in a match) only when the player fills both energy bars, opens the tablet in EDIT TABLET and confirms "Do you want to ascend this tablet?" — the client's `0x4f` request carries an ascend flag and the reply's field `[7]` now echoes it, so the client plays the ascension and turns SAVE into UNLOCK. Before this release the server marked every tablet ascended the moment it was equipped, which locked tablets nobody had ascended, made the 750-Emblem / 20-Rune unlock pointless (re-equipping the tablet on another page locked it again for free) and let a plain SAVE never re-lock anything. Equipping (`0x4b`) no longer touches the flag; the flag belongs to the tablet copy and survives unequipping (the loft shows it as "(Ascended)"), and only the paid unlock clears it. A record without the flag list means "nothing ascended" instead of "everything equipped is". `cmd/clear-ascension` is a one-shot tool that clears the flags the old server set, so players re-ascend the tablets they actually completed.
- Empty tablets in the loft no longer flicker their socket markers between Order and Chaos. The client only hides a socket's energy marker when the socket key is present with `filled=false`; the server sent only the filled sockets, so the marker's movie clip on every empty socket kept playing. Every `TabletInfo` now carries all four sockets.
- Inscription exchange accepts any four inscriptions of one tier (the screen lets the player mix stats) and returns one random inscription of the next tier, which is what the "?" result slot shows; before, four different inscriptions were rejected without feedback and even the identical-four case never showed the result card because the reply did not carry the produced item (field `[6]`). The gold 1↔1 panel (`0x59`) is implemented: swap one gold inscription for a copy of another gold you own, for 300 Emblems or 10 Runes (prices published in `GetUserInfo`; original prices unknown, configurable).
- Tablets are individual copies. A player can buy and own several copies of the same tablet (the client counts copies per id and addresses every tablet by its position in the owned vector); each copy keeps its own inscriptions, ascension and page slot, and deleting one copy leaves the others alone. Before, a tablet id could be owned once, so buying a tablet you already had was silently ignored ("purchase successful", nothing charged, nothing delivered — the report that started this release). Existing accounts migrate on the first load (inventory v2: `tablet_instances` with stable per-copy ids; the previous fields stay mirrored for older readers); the loft capacity defaults to 75 so the 50-tablet grant leaves room for copies.
- Regression coverage: ascend flag and reply echo, wear never ascending, four-socket `TabletInfo`, mixed-stat exchange, mixed-tier and insufficient rejections, gold swap (live wire layout, price/tier/same-item rejections), two copies addressed by index across wear/fill/delete with the surviving copy re-indexed, second-copy purchase, legacy record migration and mirror consistency.

## Client — Global Public Beta 0.3.2 (armeabi-v7a and arm64-v8a) — 2026-09-16

- Match loading crashed on Android 15/16 devices (loading bar stuck at 72.5 %, then "Heroes O&C has stopped"; reported from a Galaxy A36 5G and a Honor 200, reproduced on a Galaxy A36 5G through Samsung Remote Test Lab). `TerrainTiled::GetHeight(float, float, vector3d*, WATER_INFO*)` rejects a terrain coordinate only when it is *greater than* the map size, so a water-material model sitting exactly on the far edge (`iz == height`) passed the check and the bilinear fetch read one row past the `(w+1)*(h+1)` heightmap. Older allocators land that read in readable heap; Scudo on Android 15/16 ends the block just before a guard page, so the read faults (`SIGSEGV SEGV_ACCERR` in `GLThread`). Both compares now reject the edge (`bhi` → `bhs`) and fall through to the existing default-height return. Both APKs rebuilt; the native libraries differ from 0.3 / 0.3.1 arm64 only at those two instructions. Verified on the A36 with both ABIs. The bug exists in the original client.

## server-v0.1.8 — 2026-09-16

### Hotfix: summoner spells parsed from the real SkillAck layout

- Summoner spells sometimes did not carry into the match, or different values appeared, and a retry would "fix" it. The server located the spell pair in the client's `0x100C` SkillAck body with a READY+14 read plus a byte scan for two ints in 100..65536; the public-server journal shows the scan mostly latched garbage (3584/3840, 6912/7168 — two consecutive small ints read one byte early) for the very same body sizes that other times parsed fine. The body is deterministic — cid, three length-prefixed UTF strings (session guid, PlayerInfo guid, nickname), then a fixed run of 139 little-endian ints starting with READY, two PlayerInfo ints, spell 1 and spell 2 — so the old code, which walked only two strings, depended on how the third length prefix happened to look. The parser now walks the three strings and reads READY+12 / READY+16; the heuristics remain only as a fallback for bodies whose prefixes do not parse. No client update is required.
- Tests build SkillAck bodies per that layout for the nickname/guid lengths seen on the public server; the previous parser reproduces the live 3584/3840 garbage on them.

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
