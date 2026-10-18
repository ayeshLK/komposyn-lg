#!/usr/bin/env bash
set -euo pipefail

source_path="${1:?usage: copy-release-executable.sh <source> <destination>}"
destination_path="${2:?usage: copy-release-executable.sh <source> <destination>}"

# Windows can retain an executable's image lock briefly after the stopped
# collector releases its lease. Allow up to five seconds for that lock to clear.
# Never force removal or terminate a process to make the fixture succeed.
max_attempts=1
case "$(uname -s)" in
  MINGW*|MSYS*|CYGWIN*) max_attempts=51 ;;
esac
for ((attempt = 1; attempt <= max_attempts; attempt++)); do
  if cp "$source_path" "$destination_path"; then
    exit 0
  else
    copy_status=$?
  fi
  if ((attempt < max_attempts)); then
    sleep 0.1
  fi
done
echo "unable to copy release executable after $max_attempts attempt(s): $destination_path" >&2
exit "$copy_status"
