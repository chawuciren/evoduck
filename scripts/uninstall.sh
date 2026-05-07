#!/bin/sh

set -eu

BIN_NAME="evoduck"
INSTALL_DIR="${EVODUCK_INSTALL_DIR:-${HOME}/.local/bin}"
TARGET="${INSTALL_DIR}/${BIN_NAME}"
DATA_DIR="${HOME}/.evoduck"
START_MARKER="# >>> EvoDuck installer >>>"
END_MARKER="# <<< EvoDuck installer <<<"

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

AI Agent Gateway | uninstaller

EOF
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

remove_autostart_direct() {
  case "$(uname -s)" in
    Darwin)
      autostart_path="${HOME}/Library/LaunchAgents/com.evoduck.plist"
      ;;
    Linux)
      autostart_path="${HOME}/.config/autostart/evoduck.desktop"
      ;;
    *)
      return
      ;;
  esac

  if [ -e "$autostart_path" ]; then
    rm -f "$autostart_path"
    log "Removed autostart entry at ${autostart_path}"
  fi
}

stop_service() {
  if [ ! -x "$TARGET" ]; then
    return
  fi

  if "$TARGET" service stop >/dev/null 2>&1; then
    log "Stopped EvoDuck service"
    return
  fi

  log "Warning: failed to stop EvoDuck service via ${TARGET} service stop"
}

remove_autostart() {
  if [ -x "$TARGET" ]; then
    if "$TARGET" uninstall >/dev/null 2>&1; then
      log "Removed autostart configuration"
      return
    fi
    log "Built-in autostart removal failed, falling back to direct cleanup."
  fi

  remove_autostart_direct
}

remove_binary() {
  removed=0
  for path in "$TARGET" "${TARGET}.new" "${TARGET}.bak"; do
    if [ -e "$path" ]; then
      rm -f "$path"
      removed=1
      log "Removed $path"
    fi
  done

  if [ "$removed" -eq 0 ]; then
    log "No installed EvoDuck binary found in ${INSTALL_DIR}"
  fi
}

remove_path_block() {
  profile=$(profile_path)
  if [ ! -f "$profile" ]; then
    return
  fi

  removed=0
  if grep -F "$START_MARKER" "$profile" >/dev/null 2>&1; then
    tmp_file=$(mktemp)
    awk -v start="$START_MARKER" -v end="$END_MARKER" '
      $0 == start { skip=1; next }
      $0 == end { skip=0; next }
      skip != 1 { print }
    ' "$profile" > "$tmp_file"
    mv "$tmp_file" "$profile"
    removed=1
  fi

  legacy_line='export PATH="$HOME/.local/bin:$PATH"'
  if grep -F "$legacy_line" "$profile" >/dev/null 2>&1; then
    tmp_file=$(mktemp)
    grep -F -v "$legacy_line" "$profile" > "$tmp_file"
    mv "$tmp_file" "$profile"
    removed=1
  fi

  if [ "$removed" -eq 1 ]; then
    log "Removed EvoDuck PATH entry from ${profile}"
  fi
}

prompt_remove_data() {
  if [ ! -e "$DATA_DIR" ]; then
    return
  fi

  printf 'Remove runtime data at %s? [y/N]: ' "$DATA_DIR"
  read answer || answer=""
  case "$answer" in
    y|Y|yes|YES)
      rm -rf "$DATA_DIR"
      log "Removed runtime data at ${DATA_DIR}"
      ;;
    *)
      log "Keeping runtime data at ${DATA_DIR}"
      ;;
  esac
}

main() {
  print_brand_header
  stop_service
  remove_autostart
  remove_binary
  remove_path_block
  prompt_remove_data
  log "EvoDuck uninstall finished."
}

main "$@"
