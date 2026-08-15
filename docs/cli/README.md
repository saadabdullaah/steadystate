# Installing platformctl

`platformctl` is the supported SteadyState developer entrypoint. Release
archives are built with CGO disabled for Windows, Linux, and macOS on AMD64
and ARM64. Windows archives use ZIP; Linux and macOS use `tar.gz`.

## Windows

```powershell
.\scripts\install-platformctl.ps1 -Version v1.0.1
platformctl version --output json
```

## Linux and macOS

```sh
PLATFORMCTL_VERSION=v1.0.1 ./scripts/install-platformctl.sh
platformctl version --output json
```

Both installers download the archive and release checksum manifest over
HTTPS, require an exact filename match, and verify SHA-256 before installing.
They do not modify shell startup files or silently update an installation.

For commands and flags, see the generated
[command reference](platformctl.md). For verification, compatibility,
upgrade, rollback, and break-glass behavior, see
[operations](operations.md).
