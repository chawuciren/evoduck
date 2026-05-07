#!/bin/sh

set -eu

OWNER="chawuciren"
REPO="evoduck"
BIN_NAME="evoduck"
INSTALL_DIR="${EVODUCK_INSTALL_DIR:-${HOME}/.local/bin}"

log() {
  printf '%s\n' "$*"
}

print_brand_header() {
  cat <<'EOF'
                    ░░░░
                ██████████░░
              ██████████████░
            ████  ██  ████████░
            ████  ██  ██████████  ██░░
        ░░██████████████████████ ██▓▓
      ░░██████████████▓▓██████▓▓
      ███████████████▓▓▓▓██████░
      ████████████▓▓▓▓▓▓████░░
        ████████▓▓▓▓▓▓██░░
          ░░██      ██
            ██      ██

████████╗██╗   ██╗ ██████╗ ██████╗ ██╗   ██╗ ██████╗██╗  ██╗
██╔════╝██║   ██║██╔═══██╗██╔══██╗██║   ██║██╔════╝██║ ██╔╝
█████╗  ██║   ██║██║   ██║██║  ██║██║   ██║██║     █████╔╝
██╔══╝  ╚██╗ ██╔╝██║   ██║██║  ██║██║   ██║██║     ██╔═██╗
███████╗ ╚████╔╝ ╚██████╔╝██████╔╝╚██████╔╝╚██████╗██║  ██╗
╚══════╝  ╚═══╝   ╚═════╝ ╚═════╝  ╚═════╝  ╚═════╝╚═╝  ╚═╝
 ░░░░░░    ░░░     ░░░░░   ░░░░░    ░░░░░    ░░░░░  ░░   ░░

AI Agent Gateway | installer

EOF
}

fail() {
  printf 'Error: %s\n' "$*" >&2
  exit 1
}

need_cmd() {
  command -v "$1" >/dev/null 2>&1 || fail "missing required command: $1"
}

detect_platform() {
  os=$(uname -s)
  arch=$(uname -m)

  case "$os" in
    Linux) platform_os="linux" ;;
    Darwin) platform_os="darwin" ;;
    *) fail "unsupported operating system: $os" ;;
  esac

  case "$arch" in
    x86_64|amd64) platform_arch="amd64" ;;
    arm64|aarch64) platform_arch="arm64" ;;
    *) fail "unsupported architecture: $arch" ;;
  esac

  ASSET_NAME="${BIN_NAME}-${platform_os}-${platform_arch}.tar.gz"
}

ensure_install_dir() {
  mkdir -p "$INSTALL_DIR"
}

download_release() {
  version="${EVODUCK_VERSION:-latest}"
  if [ "$version" = "latest" ]; then
    url="https://github.com/${OWNER}/${REPO}/releases/latest/download/${ASSET_NAME}"
  else
    url="https://github.com/${OWNER}/${REPO}/releases/download/${version}/${ASSET_NAME}"
  fi

  tmp_dir=$(mktemp -d)
  archive_path="${tmp_dir}/${ASSET_NAME}"

  cleanup() {
    rm -rf "$tmp_dir"
  }
  trap cleanup EXIT INT TERM

  log "Downloading ${url}"
  curl -fsSL "$url" -o "$archive_path"

  tar -xzf "$archive_path" -C "$tmp_dir"

  if [ ! -f "${tmp_dir}/${BIN_NAME}" ]; then
    fail "archive does not contain ${BIN_NAME} at its root"
  fi

  replace_binary "${tmp_dir}/${BIN_NAME}" "${INSTALL_DIR}/${BIN_NAME}"
}

replace_binary() {
  src="$1"
  dst="$2"
  new_path="${dst}.new"
  backup_path="${dst}.bak"

  install -m 0755 "$src" "$new_path"
  rm -f "$backup_path"
  if [ -e "$dst" ]; then
    mv "$dst" "$backup_path"
  fi
  if mv "$new_path" "$dst"; then
    chmod 0755 "$dst"
    rm -f "$backup_path"
  else
    if [ ! -e "$dst" ] && [ -e "$backup_path" ]; then
      mv "$backup_path" "$dst"
    fi
    fail "failed to replace ${dst}; check whether another process is holding it"
  fi
}

register_autostart() {
  target="$1"
  if "$target" install; then
    log "Registered autostart configuration"
    return
  fi

  log "Warning: failed to register autostart configuration via ${target} install"
}

update_existing() {
  target="${INSTALL_DIR}/${BIN_NAME}"
  log "Existing EvoDuck installation detected, updating..."
  if "$target" update; then
    register_autostart "$target"
    return
  fi

  log "Built-in update failed or is unavailable, falling back to script update."
  download_release
  register_autostart "$target"
}

profile_path() {
  if [ -n "${ZDOTDIR:-}" ] && [ -f "${ZDOTDIR}/.zshrc" ]; then
    printf '%s\n' "${ZDOTDIR}/.zshrc"
  elif [ -f "${HOME}/.zshrc" ]; then
    printf '%s\n' "${HOME}/.zshrc"
  elif [ -f "${HOME}/.bashrc" ]; then
    printf '%s\n' "${HOME}/.bashrc"
  else
    printf '%s\n' "${HOME}/.profile"
  fi
}

ensure_path() {
  case ":$PATH:" in
    *":${INSTALL_DIR}:"*)
      return
      ;;
  esac

  profile=$(profile_path)
  block_start="# >>> EvoDuck installer >>>"
  block_end="# <<< EvoDuck installer <<<"
  line="export PATH=\"${INSTALL_DIR}:\$PATH\""
  touch "$profile"

  if grep -F "$block_start" "$profile" >/dev/null 2>&1; then
    return
  fi

  {
    printf '\n%s\n' "$block_start"
    printf '%s\n' "$line"
    printf '%s\n' "$block_end"
  } >> "$profile"

  log "Added ${INSTALL_DIR} to PATH in ${profile}"
  log "Open a new shell or run: export PATH=\"${INSTALL_DIR}:\$PATH\""
}

main() {
  print_brand_header
  need_cmd curl
  need_cmd tar
  detect_platform
  ensure_install_dir
  target="${INSTALL_DIR}/${BIN_NAME}"
  if [ -e "$target" ]; then
    update_existing
    log "Updated ${BIN_NAME} at ${target}"
  else
    log "Installing EvoDuck..."
    download_release
    register_autostart "$target"
    log "Installed ${BIN_NAME} to ${target}"
  fi
  ensure_path
  log "Runtime data remains under ${HOME}/.evoduck"
  log "Run: ${BIN_NAME} --help"
}

main "$@"
