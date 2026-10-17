#!/usr/bin/env bash
set -euo pipefail

module_path="${1:?usage: resolve-core-version.sh <module-path> <expected-version>}"
expected_version="${2:?usage: resolve-core-version.sh <module-path> <expected-version>}"

workspace="$(go env GOWORK)"
if [[ -n "$workspace" && "$workspace" != off ]]; then
	echo "refusing release build from Go workspace $workspace" >&2
	exit 1
fi

metadata="$(go list -m -f '{{.Version}}|{{if .Replace}}replaced{{end}}|{{if .Main}}main{{end}}' "$module_path")" || {
	echo "unable to resolve core module $module_path" >&2
	exit 1
}
IFS='|' read -r resolved_version replaced main_module <<<"$metadata"
if [[ "$resolved_version" != "$expected_version" || -n "$replaced" || -n "$main_module" ]]; then
	echo "release core must resolve to the published $expected_version module without replacements; got $metadata" >&2
	exit 1
fi

printf '%s\n' "$resolved_version"
