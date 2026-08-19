# Conceptual Architecture

This document intentionally uses conceptual names rather than copying proprietary implementation details.

| Layer | Responsibility | Typical failure |
|---|---|---|
| Account/federation | identity, token, profile, device bootstrap | login loop, invalid profile |
| Lobby/session | room create/join, room membership, server advertisement | room not listed, join timeout |
| Game server | seat state, hero choice, ready/load/start | stuck at seat or loading |
| Match runtime | shared frame clock, actions, reconnect | movement feel, desync, disconnect |

## Boundary rules

1. Account success does not prove that the match server is reachable.
2. A lobby room can exist while the game-server socket is dead.
3. A healthy frame clock does not prove that a phone rendered every frame.
4. A high RTT does not automatically mean the server is broken.
5. A provider block can happen before the operating-system firewall sees a packet.

## Neutral state machine

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
