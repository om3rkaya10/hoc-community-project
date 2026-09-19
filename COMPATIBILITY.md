# Compatibility Guide

## Current client profile

The community beta client ships as two separate APKs of the same build (Global Public Beta 0.3.3):

- `armeabi-v7a` (ARM 32-bit) — the original build, runs on every device that still has 32-bit support;
- `arm64-v8a` (ARM 64-bit) — added 2026-09-14 for devices that dropped 32-bit support entirely (for example Pixel 7 and newer, some Android 15/16 devices), which rejected the 32-bit APK before the game started. Use build 0.3.1 arm64 (2026-09-16) or newer: earlier 64-bit builds never rendered private messages.

Use build 0.3.2 (2026-09-16) of either ABI on Android 15/16: earlier builds crashed while a match loaded (loading bar stuck at 72.5 %) on devices whose allocator places a guard page right after the terrain heightmap — seen on a Galaxy A36 5G and a Honor 200. The fault is a bounds-check bug in the original client's `TerrainTiled::GetHeight`, not a GPU or server issue.

Use build 0.3.3 (2026-09-18) of either ABI to browse the tablet page safely: earlier builds could close the game when a tablet card was tapped in the lobby (`Hero::GetCampTypeColor` called without a hero object — the community lobby has no 3D hero model). Reproduced on a Redmi Note 13 (Android 15) and on Nox; fixed on the client side.

Both talk to the same server and carry the same client-side fixes; the native patch set is ported per ABI from one source diff and each code patch asserts the stock instruction it replaces. They are kept as separate packages on purpose so a regression in one ABI cannot affect players on the other. Install whichever your device accepts; the two are signed with the same key, so switching does not require an uninstall.

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
| Match loading stops at 72.5 % and the game closes | client build older than 0.3.2 on Android 15/16; update the APK |
| Game closes when tapping a tablet card in the lobby | client build older than 0.3.3; update the APK |
| Tablet shows as locked / "ascended" without having been ascended; "purchase successful" but no tablet delivered | server older than v0.1.9 (ascension and tablet copies) |
| Dropped out of a match (network hand-off) and could not get back in; teammates saw the player leave within seconds; "Synchronizing" then thrown out again | server older than v0.1.10 (silent-drop takeover, seat kept during the flap guard, solo matches rejoinable, no clock rewind over the replay) |
| A room a friend created is not in the room list, or joining it is refused | the two clients are on different lockstep builds (current client vs. 60 fps client); both must run the same client build to share a match |
| Tablet cannot be ascended | both energy bars must be full; ascension is done from EDIT TABLET → SAVE, not from the equip drawer |

## Reporting rule

Do not ask users to share passwords, access tokens, private IPs, device identifiers, or personal information.
