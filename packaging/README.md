# winget

Manifest for submitting mtop to the public winget catalog. Unlike brew and
scoop, winget packages live in Microsoft's
[winget-pkgs](https://github.com/microsoft/winget-pkgs) repo and go through
their review, so this can't be self-hosted. To publish a new version:

1. Bump `PackageVersion` and the installer url/hash in these three files.
2. Validate: `winget validate --manifest packaging/winget`
3. Open a PR adding them under `manifests/e/eladser/mtop/<version>/` in
   winget-pkgs (or use `wingetcreate submit`).

Until that's merged, Windows users install with scoop (see the main README).
