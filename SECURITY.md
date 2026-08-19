# Security

## Never commit

- passwords or tokens;
- private keys or SSH material;
- account databases;
- production environment files;
- raw logs with credentials;
- APK/OBB/SO binaries;
- unredacted PCAPs.

## If a secret is exposed

1. Revoke or rotate it immediately.
2. Remove it from working files and history where appropriate.
3. Check logs and access records.
4. Do not paste the secret into an issue.

## Reporting

Use a private channel for infrastructure/security reports. Public issues should contain only redacted, reproducible facts.
