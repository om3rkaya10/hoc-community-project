# Contributing

Thank you for helping preserve and maintain the HOC Community Project.

## Before opening a change

1. Read `README.md`, `LICENSE`, `NOTICE`, `CONTENT_POLICY.md`, `RELEASE.md`, and `TESTING.md`.
2. Confirm that your contribution is original or that you have the right to submit it.
3. Do not submit Gameloft client code, extracted assets, APK/OBB/SO files, raw decompilation, raw Frida scripts, credentials, or unredacted captures.
4. Keep changes narrow. Compatibility is more important than architectural elegance.
5. Explain whether the change affects documentation, deterministic server behavior, or a real-client path.

## Evidence requirement

Every protocol or behavior claim should include one of:

- a deterministic test;
- a redacted log or aggregate measurement;
- a documented real-client validation level (`V3`, `V4`, or `V5`);
- an explicit `hypothesis` or `parked` label.

Do not present an inference as an official protocol specification.

## Pull requests

A pull request should state:

- what changed;
- why it is needed;
- which compatibility behavior it may affect;
- tests run and their output;
- whether a live client run is required;
- what secrets or proprietary materials were deliberately excluded.

Use a focused branch and keep unrelated cleanup out of the change.

## Server changes

For `server/hoc-server` changes:

```text
go build ./...
go vet ./...
go test ./... -count=1
```

Do not change pinned wire behavior, frame-clock ownership, account identity semantics, or reconnect state transitions without a focused regression test and an explicit compatibility note.

Changes that require a phone/Nox/VPS run must say so before merge. Documentation-only changes do not require live testing.

## Commit style

Use concise, factual commit subjects, for example:

```text
Document cross-geography validation levels
Add regression test for room survivor state
Clarify server release package boundary
```

## Contributor licensing

By submitting a contribution, you represent that you have the right to submit it and that you agree to the contribution terms in `LICENSE`. Unless a separate written agreement applies, your contribution is made available under the repository's HOC Community Server Community Source License.

## Communication

Be respectful and avoid requesting passwords, tokens, private logs, device identifiers, or other sensitive data. Use redacted, reproducible evidence instead.

## Scope boundary

This project maintains an unofficial community server and preservation handbook. It does not claim to own or represent Gameloft's original client, assets, trademarks, or service.
