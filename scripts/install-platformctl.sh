#!/usr/bin/env sh
set -eu

version="${PLATFORMCTL_VERSION:-v0.8.0}"
install_directory="${PLATFORMCTL_INSTALL_DIR:-$HOME/.local/bin}"
printf '%s' "$version" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+$' || {
  echo "PLATFORMCTL_VERSION must be strict vMAJOR.MINOR.PATCH" >&2
  exit 2
}
case "$(uname -s)" in
  Linux) operating_system=linux ;;
  Darwin) operating_system=darwin ;;
  *) echo "Unsupported operating system" >&2; exit 2 ;;
esac
case "$(uname -m)" in
  x86_64|amd64) architecture=amd64 ;;
  arm64|aarch64) architecture=arm64 ;;
  *) echo "Unsupported architecture" >&2; exit 2 ;;
esac

release_version="${version#v}"
archive_name="platformctl_${release_version}_${operating_system}_${architecture}.tar.gz"
release_root="https://github.com/saadabdullaah/steadystate/releases/download/${version}"
temporary_root="$(mktemp -d "${TMPDIR:-/tmp}/steadystate-platformctl.XXXXXXXX")"
trap 'rm -rf "$temporary_root"' EXIT INT TERM

curl --fail --location --retry 5 --retry-all-errors --output "$temporary_root/$archive_name" "$release_root/$archive_name"
curl --fail --location --retry 5 --retry-all-errors --output "$temporary_root/checksums.txt" "$release_root/checksums.txt"
expected="$(awk -v name="$archive_name" '$2 == name {print $1}' "$temporary_root/checksums.txt")"
test -n "$expected" || { echo "Release checksum does not contain $archive_name" >&2; exit 8; }
if command -v sha256sum >/dev/null 2>&1; then
  actual="$(sha256sum "$temporary_root/$archive_name" | awk '{print $1}')"
else
  actual="$(shasum -a 256 "$temporary_root/$archive_name" | awk '{print $1}')"
fi
test "$actual" = "$expected" || { echo "Checksum verification failed for $archive_name" >&2; exit 8; }
tar -xzf "$temporary_root/$archive_name" -C "$temporary_root"
mkdir -p "$install_directory"
install -m 0755 "$temporary_root/platformctl" "$install_directory/platformctl"
printf 'Installed verified platformctl %s to %s\n' "$version" "$install_directory"
