# Compatibility Guide

## Current client profile

The community beta client tested during the project used an ARM 32-bit native library (`armeabi-v7a`). This means some modern 64-bit-only Android devices may reject installation before the game starts.

A device report should include:

- exact model;
- Android and vendor OS version;
- ABI support;
- installation error, if any;
- Wi-Fi or mobile network.

## Common outcomes

| Symptom | Likely class |
|---|---|
| “App not compatible” before install | ABI/device or package compatibility |
| Signature conflict | another installation signed differently |
| Login works, room fails | lobby/provider/account state |
| Guest reaches lobby but cannot ready | guest/device identity path; use normal beta login |
| Random tiny visual hitch | client render/frame pacing or network path; measure before changing server |

## Reporting rule

Do not ask users to share passwords, access tokens, private IPs, device identifiers, or personal information.
