# Original Go Community Server

The `server/hoc-server/` directory contains the community server written from scratch for this project. It is separate from Gameloft’s original client software and does not contain copied Gameloft source code or game assets.

## Ownership and licensing

The server implementation, tests, packet builders, session model, match clock, account/edge logic, and documentation in this repository are original project work. They are **not licensed to third parties yet**. No permission to copy, redistribute, host, or sell this server code is granted by this archive until an explicit license is added.

This ownership statement does not claim ownership of Heroes of Order & Chaos, Gameloft trademarks, the original client, original assets, or any third-party protocol vocabulary.

## Main packages

| Package | Role |
|---|---|
| `internal/accounts` | temporary accounts, profile identity, token lifecycle |
| `internal/edge` | HTTP/TLS edge, authorization/profile/config responses |
| `internal/lobby` | room create/join/leave and server advertisement |
| `internal/gs` | game-server login, seat state, ready/load/start, player actions |
| `internal/match` | shared 30 Hz clock, action relay, reconnect replay |
| `internal/session` | rooms, seats, hero claims, survivor promotion |
| `internal/wire/glblock` | independently written block/field packet builders |
| `internal/wire/gs` | independently written game-server packet builders/parsers |
| `internal/wire/msgpack` | small protocol utility used by the server |
| `internal/netx` | bounded in-match writes for WAN peers |

## Build

```text
go build ./...
go vet ./...
go test ./... -count=1
```

The production deployment uses a separately supplied runtime directory for certificates, accounts and configuration. Those files are not part of this source snapshot.

## Runtime boundary

The source snapshot is not a turnkey public deployment. Operators must supply their own certificate, environment configuration, account storage and authorization decisions. Never commit those values.
