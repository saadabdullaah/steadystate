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
INCLUDE_SECURITY=false

for argument in "$@"; do
  case "$argument" in
    --force) FORCE=true ;;
    --base-only) BASE_ONLY=true ;;
    --skip-lint) SKIP_LINT=true ;;
    --include-security) INCLUDE_SECURITY=true ;;
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
  "https://github.com/argoproj/argo-rollouts/releases/download/v${ARGO_ROLLOUTS_VERSION}/kubectl-argo-rollouts-linux-amd64" \
  "$BIN_DIR/kubectl-argo-rollouts" \
  "$ARGO_ROLLOUTS_CLI_LINUX_AMD64_SHA256"

k6_archive="$DOWNLOAD_DIR/k6-v${K6_VERSION}-linux-amd64.tar.gz"
download_verified \
  "https://github.com/grafana/k6/releases/download/v${K6_VERSION}/k6-v${K6_VERSION}-linux-amd64.tar.gz" \
  "$k6_archive" \
  "$K6_LINUX_AMD64_SHA256"
mkdir -p "$TOOLS_ROOT/k6-extract"
tar -xzf "$k6_archive" -C "$TOOLS_ROOT/k6-extract"
cp "$TOOLS_ROOT/k6-extract/k6-v${K6_VERSION}-linux-amd64/k6" "$BIN_DIR/k6"

if [[ "$BASE_ONLY" == false || "$INCLUDE_SECURITY" == true ]]; then
  kyverno_archive="$DOWNLOAD_DIR/kyverno-cli_v${KYVERNO_VERSION}_linux_x86_64.tar.gz"
  download_verified \
    "https://github.com/kyverno/kyverno/releases/download/v${KYVERNO_VERSION}/kyverno-cli_v${KYVERNO_VERSION}_linux_x86_64.tar.gz" \
    "$kyverno_archive" \
    "$KYVERNO_CLI_LINUX_AMD64_SHA256"
  mkdir -p "$TOOLS_ROOT/kyverno-extract/linux-amd64"
  tar -xzf "$kyverno_archive" -C "$TOOLS_ROOT/kyverno-extract/linux-amd64"
  cp "$TOOLS_ROOT/kyverno-extract/linux-amd64/kyverno" "$BIN_DIR/kyverno"
  chmod +x "$BIN_DIR/kyverno"

  download_verified \
    "https://github.com/sigstore/cosign/releases/download/v${COSIGN_VERSION}/cosign-linux-amd64" \
    "$BIN_DIR/cosign" \
    "$COSIGN_LINUX_AMD64_SHA256"
  syft_archive="$DOWNLOAD_DIR/syft_${SYFT_VERSION}_linux_amd64.tar.gz"
  download_verified \
    "https://github.com/anchore/syft/releases/download/v${SYFT_VERSION}/syft_${SYFT_VERSION}_linux_amd64.tar.gz" \
    "$syft_archive" \
    "$SYFT_LINUX_AMD64_SHA256"
  mkdir -p "$TOOLS_ROOT/syft-extract/linux-amd64"
  tar -xzf "$syft_archive" -C "$TOOLS_ROOT/syft-extract/linux-amd64"
  cp "$TOOLS_ROOT/syft-extract/linux-amd64/syft" "$BIN_DIR/syft"

  download_verified \
    "https://github.com/getsops/sops/releases/download/v${SOPS_VERSION}/sops-v${SOPS_VERSION}.linux.amd64" \
    "$BIN_DIR/sops" \
    "$SOPS_LINUX_AMD64_SHA256"
  age_archive="$DOWNLOAD_DIR/age-v${AGE_VERSION}-linux-amd64.tar.gz"
  download_verified \
    "https://github.com/FiloSottile/age/releases/download/v${AGE_VERSION}/age-v${AGE_VERSION}-linux-amd64.tar.gz" \
    "$age_archive" \
    "$AGE_LINUX_AMD64_SHA256"
  mkdir -p "$TOOLS_ROOT/age-extract/linux-amd64"
  tar -xzf "$age_archive" -C "$TOOLS_ROOT/age-extract/linux-amd64"
  cp "$TOOLS_ROOT/age-extract/linux-amd64/age/age" "$BIN_DIR/age"
  cp "$TOOLS_ROOT/age-extract/linux-amd64/age/age-keygen" "$BIN_DIR/age-keygen"
  chmod +x "$BIN_DIR/cosign" "$BIN_DIR/syft" "$BIN_DIR/sops" "$BIN_DIR/age" "$BIN_DIR/age-keygen"
fi

download_verified \
  "https://github.com/kubernetes-sigs/kubebuilder/releases/download/v${KUBEBUILDER_VERSION}/kubebuilder_linux_amd64" \
  "$BIN_DIR/kubebuilder" \
  "$KUBEBUILDER_LINUX_AMD64_SHA256"

chmod +x "$BIN_DIR/kubectl" "$BIN_DIR/kind" "$BIN_DIR/helm" "$BIN_DIR/kubebuilder" "$BIN_DIR/kubectl-argo-rollouts" "$BIN_DIR/k6"

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
