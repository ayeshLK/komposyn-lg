#!/usr/bin/env bash
set -euo pipefail

root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
resolver="$root_dir/scripts/resolve-core-version.sh"
expected_version='v0.7.1'

python3 - "$root_dir/release-please-config.json" <<'PY'
import json
import sys

with open(sys.argv[1], encoding="utf-8") as config_file:
    config = json.load(config_file)
if config["packages"]["."]["include-component-in-tag"] is not False:
    raise SystemExit("Release Please must produce plain vX.Y.Z tags for release.yml")
PY

resolved="$(bash "$resolver" github.com/ayeshLK/komposyn "$expected_version")"
[[ "$resolved" == "$expected_version" ]] || {
	echo "resolver returned $resolved instead of $expected_version" >&2
	exit 1
}

tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT
fixture="$tmp_dir/fixture"
mkdir -p "$fixture/scripts" "$fixture/core"
cp "$root_dir/scripts/build-release.sh" "$fixture/scripts/"
cp "$resolver" "$fixture/scripts/"
cat > "$fixture/go.mod" <<'EOF'
module example.com/provenance-fixture

go 1.26.0

require github.com/ayeshLK/komposyn v0.7.1

replace github.com/ayeshLK/komposyn => ./core
EOF
cat > "$fixture/core/go.mod" <<'EOF'
module github.com/ayeshLK/komposyn

go 1.26.0
EOF
if (cd "$fixture" && GOWORK=off bash scripts/build-release.sh v0.7.1 "$tmp_dir/replaced-output"); then
	echo 'release builder accepted a locally replaced core module' >&2
	exit 1
fi
[[ ! -e "$tmp_dir/replaced-output" ]] || {
	echo 'release builder created output before rejecting a local replacement' >&2
	exit 1
}

cat > "$fixture/go.mod" <<'EOF'
module example.com/provenance-fixture

go 1.26.0

require github.com/ayeshLK/komposyn v0.7.1
EOF
cat > "$fixture/go.work" <<'EOF'
go 1.26.0

use .

replace github.com/ayeshLK/komposyn => ./core
EOF
if (cd "$fixture" && GOWORK="$fixture/go.work" bash scripts/build-release.sh v0.7.1 "$tmp_dir/workspace-output"); then
	echo 'release builder accepted a Go workspace override' >&2
	exit 1
fi
[[ ! -e "$tmp_dir/workspace-output" ]] || {
	echo 'release builder created output before rejecting a workspace override' >&2
	exit 1
}
echo 'release provenance checks passed'
