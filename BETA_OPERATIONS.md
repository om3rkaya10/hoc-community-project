# Public Beta Operations

## Release posture

The community server is a public beta, not a guaranteed production service. The goal is playability and evidence gathering, not perfect latency for every geography.

## Bug cadence

Collect reports for approximately 15 days, classify them, then ship a small grouped fix set. Avoid emergency speculative changes to pinned wire behavior.

## Required bug-report fields

- country and ISP/carrier;
- Wi-Fi or mobile data;
- device and Android version;
- local time and timezone;
- last visible game state;
- exact reproduction steps;
- screenshot/video if safe.

Never request passwords or access tokens.

## Rollback posture

Keep a known-good server artifact and configuration. If a provider or release regression occurs, use maintenance messaging and hostname/DNS migration rather than distributing a new client unnecessarily.

## Guest accounts

A normal beta login is the supported path. Guest/device identities may reach the lobby but are not the canonical ready-flow test path.
