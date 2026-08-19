# Observed Interoperability Reference

This is an original, redacted reference to observed client/server state transitions. It is not copied source code and is not an official Gameloft specification.

## Account and edge

```text
service configuration
  → data-center selection
  → service URL discovery
  → authorization/authentication
  → profile and client-data bootstrap
```

The community edge uses a stable hostname so the backend can move between providers without requiring a new client artifact.

## Lobby state machine

```text
lobby handshake
  → identity/profile registration
  → room create or room search
  → room join
  → game-server advertisement
```

A room advertisement contains a game-server endpoint and a short-lived join token. Tokens in examples are intentionally omitted.

## Custom match state machine

```text
custom room create
  → room advertisement
  → game-server login
  → seat roster
  → hero selection
  → ready state
  → map load
  → start play
  → shared match clock
```

Observed message labels used by the client include `e038`, `e039`, `e067`, `e02d`, `0x100B`, `0x100C`, `0x1008`, `0x1009`, `0x1010`, `0x1011`, `0x2001`, `0x2002`, `0x2003`, and `0x2004`. These labels are identifiers in an interoperability observation, not copied implementation code.

## Critical behavioral pins

- The room login identity uses a room-scoped marker; a server must keep room identity consistent across the lobby advertisement and game-server login.
- Seat values are local seat positions, not hero/item IDs.
- The selected hero is preserved in player-info state; the server must not replace a missing selection with an invented hero.
- The ready flow requires a selected hero.
- The match clock is one shared 30 Hz source per room.
- Player actions and frame packets share a monotonic synchronization stream.
- A disconnected player can be held and replayed without freezing the survivor’s clock.
- Writes to a slow WAN peer are bounded so one peer cannot block the whole room.

## Evidence discipline

Every new field should be backed by a redacted log, a test, or a user-confirmed observation. Raw captures and exact proprietary binary locations stay outside this repository.
