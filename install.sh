#!/bin/sh
# Installer for txt, a local browser-based editor for .txt and .md files.
#
#   curl -fsSL https://raw.githubusercontent.com/likhithkr7/txt/main/install.sh | sh
#
# Downloads the prebuilt binary for this machine from GitHub Releases,
# verifies its SHA-256 checksum, and puts it on your PATH.
#
# Environment variables:
#   VERSION           version to install, e.g. 0.2.0 (default: latest release)
#   INSTALL_DIR       where to put the binary (default: first writable of
#                     ~/.local/bin, ~/bin, /usr/local/bin)
#   TXT_REPO          GitHub repository (default: likhithkr7/txt)
#   TXT_DOWNLOAD_URL  base URL for release assets, for mirrors
#                     (default: https://github.com/$TXT_REPO/releases/download)

set -eu

# Everything runs inside main, called on the last line, so a partially
# downloaded script never executes.
main() {
    REPO="${TXT_REPO:-likhithkr7/txt}"
    VERSION="${VERSION:-latest}"
    DOWNLOAD_BASE="${TXT_DOWNLOAD_URL:-https://github.com/${REPO}/releases/download}"

    setup_colors
    detect_platform
    resolve_version
    choose_install_dir

    NAME="txt-${VERSION}-${OS}-${ARCH}${EXT}"
    TARGET="${INSTALL_DIR}/txt${EXT}"

    PREVIOUS=""
    if [ -x "$TARGET" ]; then
        PREVIOUS=$("$TARGET" -version 2>/dev/null | sed -n 's/^txt //p' || true)
    fi

    TMP=$(mktemp -d 2>/dev/null || mktemp -d -t txtinstall)
    trap 'rm -rf "$TMP"' EXIT INT TERM

    step "Downloading ${BOLD}${NAME}${RESET}"
    download "${DOWNLOAD_BASE}/v${VERSION}/${NAME}" "${TMP}/${NAME}" ||
        fail "Download failed: ${DOWNLOAD_BASE}/v${VERSION}/${NAME}
   If v${VERSION} was just tagged, the release build may still be running."
    download "${DOWNLOAD_BASE}/v${VERSION}/checksums.txt" "${TMP}/checksums.txt" ||
        fail "Could not download checksums.txt for v${VERSION}."
    verify_checksum

    step "Installing to ${BOLD}${TARGET}${RESET}"
    chmod 755 "${TMP}/${NAME}"
    if [ "$USE_SUDO" = 1 ]; then
        sudo mkdir -p "$INSTALL_DIR"
        sudo mv -f "${TMP}/${NAME}" "$TARGET"
    else
        mkdir -p "$INSTALL_DIR"
        mv -f "${TMP}/${NAME}" "$TARGET"
    fi

    if [ -n "$PREVIOUS" ] && [ "$PREVIOUS" != "$VERSION" ]; then
        ok "txt updated: v${PREVIOUS} -> v${VERSION}"
    elif [ -n "$PREVIOUS" ]; then
        ok "txt v${VERSION} reinstalled"
    else
        ok "txt v${VERSION} installed"
    fi

    ensure_on_path

    printf '\nGet started:\n\n'
    printf '  %btxt%b            # edit the .txt and .md files in this folder\n' "$ACCENT" "$RESET"
    printf '  %btxt ~/notes%b    # or any folder\n' "$ACCENT" "$RESET"
    printf '  %btxt -h%b         # all options\n\n' "$ACCENT" "$RESET"
}

setup_colors() {
    if [ -t 1 ] && [ -z "${NO_COLOR:-}" ]; then
        BOLD='\033[1m'; ACCENT='\033[38;5;131m'; GREEN='\033[32m'
        YELLOW='\033[33m'; RED='\033[31m'; RESET='\033[0m'
    else
        BOLD=''; ACCENT=''; GREEN=''; YELLOW=''; RED=''; RESET=''
    fi
}

step() { printf ' %b›%b %b\n' "$ACCENT" "$RESET" "$1"; }
ok()   { printf ' %b✓%b %b\n' "$GREEN" "$RESET" "$1"; }
warn() { printf ' %b!%b %b\n' "$YELLOW" "$RESET" "$1"; }
fail() { printf ' %b✗%b %b\n' "$RED" "$RESET" "$1" >&2; exit 1; }

detect_platform() {
    case "$(uname -s)" in
        Darwin) OS=darwin ;;
        Linux) OS=linux ;;
        MINGW* | MSYS* | CYGWIN*) OS=windows ;;
        *) fail "Unsupported OS: $(uname -s). Build from source instead: https://github.com/${REPO}" ;;
    esac
    case "$(uname -m)" in
        x86_64 | amd64) ARCH=amd64 ;;
        arm64 | aarch64) ARCH=arm64 ;;
        *) fail "Unsupported CPU: $(uname -m). Build from source instead: https://github.com/${REPO}" ;;
    esac
    # Rosetta: a shell running under translation on Apple silicon reports
    # x86_64; prefer the native arm64 build
    if [ "$OS" = darwin ] && [ "$ARCH" = amd64 ] &&
        [ "$(sysctl -n sysctl.proc_translated 2>/dev/null || echo 0)" = 1 ]; then
        ARCH=arm64
    fi
    EXT=""
    [ "$OS" = windows ] && EXT=".exe"
    return 0
}

have() { command -v "$1" >/dev/null 2>&1; }

download() { # url, output file
    if have curl; then
        curl -fsSL --retry 2 --connect-timeout 10 -o "$2" "$1"
    elif have wget; then
        wget -q --tries=3 --timeout=15 -O "$2" "$1"
    else
        fail "curl or wget is required."
    fi
}

resolve_version() {
    if [ "$VERSION" != latest ]; then
        VERSION="${VERSION#v}"
        return
    fi
    step "Finding the latest release of ${BOLD}${REPO}${RESET}"
    tag=""
    if have curl; then
        # github.com/<repo>/releases/latest redirects to .../tag/<tag>; this
        # avoids the GitHub API and its rate limits
        url=$(curl -fsSLI -o /dev/null -w '%{url_effective}' "https://github.com/${REPO}/releases/latest" 2>/dev/null || true)
        case "$url" in */tag/*) tag="${url##*/tag/}" ;; esac
    fi
    if [ -z "$tag" ]; then
        json=$(download "https://api.github.com/repos/${REPO}/releases/latest" /dev/stdout 2>/dev/null || true)
        tag=$(printf '%s' "$json" | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -n 1)
    fi
    [ -n "$tag" ] || fail "Could not find a release of ${REPO}. Set VERSION=x.y.z to pick one."
    VERSION="${tag#v}"
}

verify_checksum() {
    expected=$(awk -v f="$NAME" '$2 == f || $2 == "*" f { print $1 }' "${TMP}/checksums.txt")
    [ -n "$expected" ] || fail "No checksum listed for ${NAME}."
    if have sha256sum; then
        actual=$(sha256sum "${TMP}/${NAME}" | awk '{ print $1 }')
    elif have shasum; then
        actual=$(shasum -a 256 "${TMP}/${NAME}" | awk '{ print $1 }')
    else
        warn "No sha256sum or shasum found; skipping checksum verification."
        return
    fi
    [ "$actual" = "$expected" ] || fail "Checksum mismatch for ${NAME}; refusing to install.
   expected ${expected}
   got      ${actual}"
    ok "Checksum verified"
}

writable_dir() { # creates the directory if needed
    mkdir -p "$1" 2>/dev/null && [ -w "$1" ]
}

on_path() {
    case ":${PATH}:" in *":$1:"*) return 0 ;; esac
    return 1
}

choose_install_dir() {
    USE_SUDO=0
    if [ -n "${INSTALL_DIR:-}" ]; then
        writable_dir "$INSTALL_DIR" || fail "INSTALL_DIR ${INSTALL_DIR} is not writable."
        return
    fi
    # Prefer a writable directory that's already on PATH, then the usual
    # per-user directory, and only fall back to sudo for /usr/local/bin
    for dir in "${HOME}/.local/bin" "${HOME}/bin" /usr/local/bin; do
        if on_path "$dir" && writable_dir "$dir"; then
            INSTALL_DIR="$dir"
            return
        fi
    done
    if writable_dir "${HOME}/.local/bin"; then
        INSTALL_DIR="${HOME}/.local/bin"
    elif have sudo; then
        INSTALL_DIR=/usr/local/bin
        USE_SUDO=1
        warn "Installing to /usr/local/bin needs sudo; you may be asked for your password."
    else
        fail "No writable install directory found. Set INSTALL_DIR to choose one."
    fi
}

# If the install directory isn't on PATH, add it to the shell's startup file
ensure_on_path() {
    on_path "$INSTALL_DIR" && return
    case "$(basename "${SHELL:-sh}")" in
        zsh) rc="${ZDOTDIR:-$HOME}/.zshrc"; line="export PATH=\"${INSTALL_DIR}:\$PATH\"" ;;
        bash)
            rc="${HOME}/.bashrc"
            [ "$OS" = darwin ] && rc="${HOME}/.bash_profile"
            line="export PATH=\"${INSTALL_DIR}:\$PATH\"" ;;
        fish) rc="${HOME}/.config/fish/config.fish"; line="fish_add_path \"${INSTALL_DIR}\"" ;;
        *) rc="${HOME}/.profile"; line="export PATH=\"${INSTALL_DIR}:\$PATH\"" ;;
    esac
    if [ -f "$rc" ] && grep -Fq "$INSTALL_DIR" "$rc"; then
        warn "${INSTALL_DIR} is set up in ${rc}, but not active in this shell yet."
    else
        mkdir -p "$(dirname "$rc")"
        printf '\n# Added by the txt installer\n%s\n' "$line" >>"$rc"
        ok "Added ${INSTALL_DIR} to your PATH in ${rc}"
    fi
    warn "To use txt in this terminal, run: ${BOLD}source ${rc}${RESET}"
}

main "$@"
