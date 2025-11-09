#!/usr/bin/env bash
set -euo pipefail

API_BASE="${MELTICA_API:-http://localhost:8880}"
JQ_BIN="${JQ_BIN:-jq}"

usage() {
  cat <<USAGE
Usage: $(basename "$0") <command> [options]

Commands:
  assign <strategy> <tag> <hash>   Reassign a tag to point at the provided hash (defaults refresh=true)
  delete <strategy> <tag>          Remove a tag alias (use --allow-orphan to force removing the final alias)

Environment:
  MELTICA_API   Base URL for the control plane (default: http://localhost:8880)
  JQ_BIN        jq executable to use (default: jq)
USAGE
  exit 1
}

require_jq() {
  if ! command -v "$JQ_BIN" >/dev/null 2>&1; then
    echo "error: jq is required (set JQ_BIN if installed elsewhere)" >&2
    exit 1
  fi
}

fetch_current_hash() {
  local strategy tag
  strategy=$1
  tag=$2
  require_jq
  curl -sSf "${API_BASE}/strategies/modules/${strategy}" |
    "$JQ_BIN" -er --arg tag "$tag" 'if .tagAliases[$tag] then .tagAliases[$tag] else empty end'
}

assign_tag() {
  local strategy tag hash current refresh refresh_flag
  strategy=$1
  tag=$2
  hash=$3
  refresh_flag=${4:-true}
  current=$(fetch_current_hash "$strategy" "$tag" || true)
  if [[ -n "$current" && "$current" == "$hash" ]]; then
    echo "tag ${tag} already points to ${hash}"
    exit 0
  fi
  echo "Tag ${tag} ${current:+currently points to ${current} }will move to ${hash}."
  read -r -p "Proceed? [y/N] " answer
  if [[ ! "$answer" =~ ^[Yy]$ ]]; then
    echo "aborted"
    exit 1
  fi
  curl -sSf -X PUT "${API_BASE}/strategies/modules/${strategy}/tags/${tag}" \
    -H 'Content-Type: application/json' \
    -d "{\"hash\":\"${hash}\",\"refresh\":${refresh_flag}}" | "$JQ_BIN"
}

delete_tag() {
  local strategy tag allow_orphan current
  strategy=$1
  tag=$2
  allow_orphan=${ALLOW_ORPHAN:-false}
  current=$(fetch_current_hash "$strategy" "$tag" || true)
  if [[ -z "$current" ]]; then
    echo "tag ${tag} not found for ${strategy}" >&2
    exit 1
  fi
  echo "Tag ${tag} currently points to ${current}."
  if [[ "$allow_orphan" != true ]]; then
    echo "Warning: removing this tag without --allow-orphan requires another alias to reference the same hash."
  fi
  read -r -p "Delete tag ${tag}? [y/N] " answer
  if [[ ! "$answer" =~ ^[Yy]$ ]]; then
    echo "aborted"
    exit 1
  fi
  curl -sSf -X DELETE "${API_BASE}/strategies/modules/${strategy}/tags/${tag}?allowOrphan=${allow_orphan}" | "$JQ_BIN"
}

main() {
  if [[ $# -lt 1 ]]; then
    usage
  fi
  local cmd=$1
  shift
  case "$cmd" in
    assign)
      [[ $# -ge 3 ]] || usage
      assign_tag "$1" "$2" "$3" "${REFRESH:-true}"
      ;;
    delete)
      [[ $# -ge 2 ]] || usage
      delete_tag "$1" "$2"
      ;;
    *)
      usage
      ;;
  esac
}

main "$@"
