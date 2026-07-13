#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
VERSIONS_FILE="$ROOT/scripts/versions.env"
PLATFORM="linux-amd64"
TOOLS_ROOT="$ROOT/.tools"
BIN_DIR="$TOOLS_ROOT/bin/$PLATFORM"
GO_ROOT="$TOOLS_ROOT/go/$PLATFORM"
DOWNLOAD_DIR="$TOOLS_ROOT/downloads"
export GOCACHE="$TOOLS_ROOT/cache/go-build/$PLATFORM"
export GOMODCACHE="$TOOLS_ROOT/cache/go-mod/$PLATFORM"
export GOPATH="$TOOLS_ROOT/gopath/$PLATFORM"
export XDG_CACHE_HOME="$TOOLS_ROOT/cache/xdg/$PLATFORM"
FORCE=false
BASE_ONLY=false
SKIP_LINT=false

for argument in "$@"; do
  case "$argument" in
    --force) FORCE=true ;;
    --base-only) BASE_ONLY=true ;;
    --skip-lint) SKIP_LINT=true ;;
    *) echo "Unknown argument: $argument" >&2; exit 2 ;;
  esac
done

while IFS='=' read -r key value; do
  [[ -z "$key" || "$key" == \#* ]] && continue
  export "$key=$value"
done < "$VERSIONS_FILE"

mkdir -p "$BIN_DIR" "$DOWNLOAD_DIR" "$GO_ROOT" "$GOCACHE" "$GOMODCACHE" "$GOPATH" "$XDG_CACHE_HOME"

download_verified() {
  local url="$1" destination="$2" expected="$3"
  if [[ -f "$destination" && "$FORCE" == false ]]; then
    local actual
    actual="$(sha256sum "$destination" | awk '{print $1}')"
    [[ "$actual" == "$expected" ]] && return
  fi
  curl --fail --location --retry 3 --output "$destination" "$url"
  echo "$expected  $destination" | sha256sum --check --status
}

download_with_checksum_url() {
  local url="$1" destination="$2" checksum_url="$3"
  local expected
  expected="$(curl --fail --location --retry 3 "$checksum_url" | awk 'NR == 1 {print $1}')"
  download_verified "$url" "$destination" "$expected"
}

go_archive="$DOWNLOAD_DIR/go${GO_VERSION}.linux-amd64.tar.gz"
download_verified "https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz" "$go_archive" "$GO_LINUX_AMD64_SHA256"
if [[ ! -x "$GO_ROOT/bin/go" || "$FORCE" == true ]]; then
  rm -rf "$GO_ROOT"
  mkdir -p "$GO_ROOT"
  tar -xzf "$go_archive" --strip-components=1 -C "$GO_ROOT"
fi

download_with_checksum_url \
  "https://dl.k8s.io/release/v${KUBERNETES_VERSION}/bin/linux/amd64/kubectl" \
  "$BIN_DIR/kubectl" \
  "https://dl.k8s.io/release/v${KUBERNETES_VERSION}/bin/linux/amd64/kubectl.sha256"
download_with_checksum_url \
  "https://kind.sigs.k8s.io/dl/v${KIND_VERSION}/kind-linux-amd64" \
  "$BIN_DIR/kind" \
  "https://kind.sigs.k8s.io/dl/v${KIND_VERSION}/kind-linux-amd64.sha256sum"

helm_archive="$DOWNLOAD_DIR/helm-v${HELM_VERSION}-linux-amd64.tar.gz"
download_with_checksum_url \
  "https://get.helm.sh/helm-v${HELM_VERSION}-linux-amd64.tar.gz" \
  "$helm_archive" \
  "https://get.helm.sh/helm-v${HELM_VERSION}-linux-amd64.tar.gz.sha256sum"
tar -xzf "$helm_archive" -C "$TOOLS_ROOT"
cp "$TOOLS_ROOT/linux-amd64/helm" "$BIN_DIR/helm"

download_verified \
  "https://github.com/kubernetes-sigs/kubebuilder/releases/download/v${KUBEBUILDER_VERSION}/kubebuilder_linux_amd64" \
  "$BIN_DIR/kubebuilder" \
  "$KUBEBUILDER_LINUX_AMD64_SHA256"

chmod +x "$BIN_DIR/kubectl" "$BIN_DIR/kind" "$BIN_DIR/helm" "$BIN_DIR/kubebuilder"

install_go_tool() {
  local name="$1" package="$2" version="$3"
  local marker="$BIN_DIR/$name.version"
  if [[ "$FORCE" == false && -x "$BIN_DIR/$name" && -f "$marker" && "$(tr -d '\r\n' < "$marker")" == "$version" ]]; then
    return
  fi
  echo "Installing $name $version"
  PATH="$GO_ROOT/bin:$PATH" GOBIN="$BIN_DIR" "$GO_ROOT/bin/go" install "$package@v$version"
  printf '%s\n' "$version" > "$marker"
}

if [[ "$BASE_ONLY" == false ]]; then
  install_go_tool controller-gen sigs.k8s.io/controller-tools/cmd/controller-gen "$CONTROLLER_TOOLS_VERSION"
  install_go_tool kustomize sigs.k8s.io/kustomize/kustomize/v5 "$KUSTOMIZE_VERSION"
  install_go_tool setup-envtest sigs.k8s.io/controller-runtime/tools/setup-envtest "$SETUP_ENVTEST_VERSION"
  if [[ "$SKIP_LINT" == false ]]; then
    install_go_tool golangci-lint github.com/golangci/golangci-lint/v2/cmd/golangci-lint "$GOLANGCI_LINT_VERSION"
  fi
fi

echo "Verified Linux tools installed under $BIN_DIR"
