# Handbook

## 1. What the game was

Heroes of Order & Chaos was a mobile multiplayer action game developed and operated by Gameloft. The original online service is no longer treated as a dependable live service, which creates a preservation problem for players who want to understand or revisit the client experience.

This handbook documents the community effort to make the game understandable and interoperable without redistributing original game content.

## 2. Core preservation principle

The client is the behavioral reference. Community implementations should reproduce only the minimum observable behavior needed for interoperability, using original server-side code and neutral documentation.

Do not confuse:

- the game client with the account/federation layer;
- lobby/room management with match simulation;
- shop/owned-hero identifiers with match hero identifiers;
- a UI seat number with a creature or item identifier;
- a test observation with an official specification.

## 3. High-level player journey

```text
account/federation
    → service configuration
    → lobby/session
    → room create or join
    → game-server session
    → seat and hero selection
    → ready/load/start
    → synchronized match frames and player actions
```

A successful test must identify which layer failed rather than treating every error as a generic network problem.

## 4. Community-server goals

- no permanent binary patch as the product requirement;
- server-side interoperability where possible;
- a single hostname-based client configuration for portability;
- no bots or fake members as a substitute for real players;
- conservative behavior: do not invent protocol fields without evidence;
- clear beta status and honest known issues.

## 5. Known classes of issues

- cold-start versus warm-start client state;
- Android ABI/device compatibility;
- provider connection-rate filters;
- mobile/CGNAT routing;
- client rendering/frame pacing;
- room-ready state and guest/device identities;
- reconnect timing when changing networks during a match.

## 6. Evidence labels

Use one of:

- **Verified** — reproduced with a test, log, or user confirmation;
- **Strong evidence** — repeated measurement, not yet independently reproduced;
- **Hypothesis** — plausible but unconfirmed;
- **Parked** — intentionally not pursued.

Never publish a guess as a protocol fact.

## 7. Conceptual architecture

| Layer | Responsibility | Typical failure |
|---|---|---|
| Account/federation | identity, token, profile, device bootstrap | login loop, invalid profile |
| Lobby/session | room create/join, membership, server advertisement | room not listed, join timeout |
| Game server | seat state, hero choice, ready/load/start | stuck at seat or loading |
| Match runtime | shared frame clock, actions, reconnect | movement feel, desync, disconnect |

Boundary rules:

1. Account success does not prove that the match server is reachable.
2. A lobby room can exist while the game-server socket is dead.
3. A healthy frame clock does not prove that a phone rendered every frame.
4. A high RTT does not automatically mean the server is broken.
5. A provider block can happen before the operating-system firewall sees a packet.

```text
LOGIN
  └─> LOBBY_CONNECTED
        ├─> ROOM_CREATED
        ├─> ROOM_JOINED
        └─> GAME_SERVER_CONNECTED
                └─> SEAT_READY
                        └─> MAP_LOADING
                                └─> PLAYING
                                        ├─> RECONNECT_HOLD
                                        └─> COMPLETE
```

A report should include the last confirmed state and the first missing transition.

## 8. Original server implementation

The repository includes the independently written Go backend organized around account, edge, lobby, game-server, session, wire, and match-clock packages. It is accompanied by deterministic tests and contains no original client binary or copied Gameloft source.

The server is project property and is available under the non-commercial, attribution-required, same-license terms in `LICENSE` and `NOTICE`. Development and testing instructions are consolidated in `DEVELOPMENT.md`.
