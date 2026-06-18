# winget

The first version was submitted by hand (these manifests, validated with
`winget validate --manifest packaging/winget`, PR'd into Microsoft's
[winget-pkgs](https://github.com/microsoft/winget-pkgs)).

After that it's automatic: `.github/workflows/release.yml` runs
winget-releaser on each published GitHub release and opens the bump PR. It
needs a `WINGET_TOKEN` repo secret, a classic PAT with public_repo scope, so
it can push to the winget-pkgs fork. Microsoft still merges on their side.

The manifests here are kept as a reference for the format.
