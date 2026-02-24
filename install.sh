#!/bin/sh
set -eu

REPO="terassyi/tomei"
GITHUB_BASE="https://github.com/${REPO}"

# --- Detect OS ---
detect_os() {
    os=$(uname -s | tr '[:upper:]' '[:lower:]')
    case "$os" in
        linux)  echo "linux" ;;
        darwin) echo "darwin" ;;
        *)
            echo "Error: unsupported OS: $os" >&2
            exit 1
            ;;
    esac
}

# --- Detect architecture ---
detect_arch() {
    arch=$(uname -m)
    case "$arch" in
        x86_64)         echo "amd64" ;;
        aarch64|arm64)  echo "arm64" ;;
        *)
            echo "Error: unsupported architecture: $arch" >&2
            exit 1
            ;;
    esac
}

# --- Resolve latest version tag from GitHub ---
resolve_latest_version() {
    url="${GITHUB_BASE}/releases/latest"
    redirect=$(curl -fsS -o /dev/null -w '%{redirect_url}' "$url")
    if [ -z "$redirect" ]; then
        echo "Error: failed to resolve latest version (no stable release found)" >&2
        echo "Hint: use TOMEI_VERSION=v0.1.0-rc to install a pre-release" >&2
        exit 1
    fi
    echo "$redirect" | grep -o '[^/]*$'
}

# --- Detect checksum command ---
detect_sha256_cmd() {
    if command -v sha256sum >/dev/null 2>&1; then
        echo "sha256sum"
    elif command -v shasum >/dev/null 2>&1; then
        echo "shasum"
    else
        echo "Error: neither sha256sum nor shasum found" >&2
        exit 1
    fi
}

# --- Verify checksum ---
verify_checksum() {
    checksums_file="$1"
    target_file="$2"
    sha_cmd="$3"

    expected=$(grep "  ${target_file}$" "$checksums_file" | awk '{print $1}')
    if [ -z "$expected" ]; then
        echo "Error: checksum not found for ${target_file} in checksums.txt" >&2
        exit 1
    fi

    if [ "$sha_cmd" = "sha256sum" ]; then
        actual=$(sha256sum "$target_file" | awk '{print $1}')
    else
        actual=$(shasum -a 256 "$target_file" | awk '{print $1}')
    fi

    if [ "$expected" != "$actual" ]; then
        echo "Error: checksum mismatch for ${target_file}" >&2
        echo "  expected: ${expected}" >&2
        echo "  actual:   ${actual}" >&2
        exit 1
    fi
}

# --- Main ---
main() {
    os=$(detect_os)
    arch=$(detect_arch)
    sha_cmd=$(detect_sha256_cmd)

    version="${TOMEI_VERSION:-}"
    if [ -z "$version" ]; then
        version=$(resolve_latest_version)
    fi

    install_dir="${TOMEI_INSTALL_DIR:-${HOME}/.local/bin}"

    tarball="tomei_${version}_${os}_${arch}.tar.gz"
    base_url="${GITHUB_BASE}/releases/download/${version}"

    tmpdir=$(mktemp -d)
    trap 'rm -rf "$tmpdir"' EXIT

    echo "Installing tomei ${version} (${os}/${arch})..."

    # Download archive and checksums
    echo "Downloading ${tarball}..."
    curl -fsSL "${base_url}/${tarball}" -o "${tmpdir}/${tarball}"
    curl -fsSL "${base_url}/checksums.txt" -o "${tmpdir}/checksums.txt"

    # Verify checksum
    echo "Verifying checksum..."
    (cd "$tmpdir" && verify_checksum checksums.txt "$tarball" "$sha_cmd")

    # Extract and install
    tar xzf "${tmpdir}/${tarball}" -C "$tmpdir"
    mkdir -p "$install_dir"
    mv "${tmpdir}/tomei" "${install_dir}/tomei"
    chmod +x "${install_dir}/tomei"

    echo "Installed tomei to ${install_dir}/tomei"

    # PATH check
    case ":${PATH}:" in
        *":${install_dir}:"*) ;;
        *)
            echo ""
            echo "Add ${install_dir} to your PATH:"
            echo "  export PATH=\"${install_dir}:\$PATH\""
            ;;
    esac
}

main
