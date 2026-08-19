# Preservation and Observation Method

## Clean-room posture

The project records externally observable behavior and writes independent server implementations. Contributors must not copy proprietary code or redistribute original game data.

## Evidence workflow

1. State the question precisely.
2. Identify the layer under test.
3. Capture the smallest useful observation.
4. Remove credentials and personal data.
5. Reproduce locally where possible.
6. Label the result as Verified, Strong evidence, Hypothesis, or Parked.
7. Record what would falsify it.

## Measurement examples

- endpoint status and response shape without tokens;
- room-state transitions;
- frame-clock interval distributions;
- TCP connection health and RTT ranges;
- device/ABI compatibility results;
- user-visible screen and exact time of failure.

## Do not publish

- raw PCAPs;
- unredacted server journals;
- Frida scripts or binary patch recipes;
- absolute memory addresses;
- original assets or extracted tables;
- provider credentials or infrastructure access details.

The private research archive may retain evidence locally under access control, but it must not be treated as a publication source.

## Validation record

Real-client validation is recorded separately in [`REAL_CLIENT_VALIDATION.md`](REAL_CLIENT_VALIDATION.md).
It distinguishes deterministic tests, local client runs, WAN runs, and cross-geography public-beta runs.
The distinction matters: a unit test is not a real-client claim, and a single public-beta route is not a
global latency guarantee.
