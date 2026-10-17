#!/usr/bin/env bash
set -euo pipefail

candidate_dir="${1:?usage: verify-published-upgrades.sh <candidate-release-dir> <temp-dir>}"
temp_dir="${2:?usage: verify-published-upgrades.sh <candidate-release-dir> <temp-dir>}"
repo='ayeshLK/komposyn'
identity='^https://github.com/ayeshLK/komposyn/.github/workflows/release.yml@refs/heads/master$'
issuer='https://token.actions.githubusercontent.com'
mkdir -p "$temp_dir"

verify_release_assets() {
	local tag="$1"
	local directory="$2"
	local asset="$3"
	mkdir -p "$directory"
	gh release download "$tag" --repo "$repo" --dir "$directory" \
		--pattern "$asset" --pattern "$asset.sig" --pattern "$asset.pem" \
		--pattern SHA256SUMS --pattern SHA256SUMS.sig --pattern SHA256SUMS.pem
	cosign verify-blob --signature "$directory/SHA256SUMS.sig" \
		--certificate "$directory/SHA256SUMS.pem" \
		--certificate-identity-regexp "$identity" \
		--certificate-oidc-issuer "$issuer" "$directory/SHA256SUMS" >/dev/null
	cosign verify-blob --signature "$directory/$asset.sig" \
		--certificate "$directory/$asset.pem" \
		--certificate-identity-regexp "$identity" \
		--certificate-oidc-issuer "$issuer" "$directory/$asset" >/dev/null
	(
		cd "$directory"
		grep -F "  $asset" SHA256SUMS | sha256sum -c - >/dev/null
	)
	mkdir "$directory/extracted"
	tar -xzf "$directory/$asset" -C "$directory/extracted"
	printf '%s\n' "$directory/extracted/komposyn-lg"
}

v060_binary="$(verify_release_assets v0.6.0 "$temp_dir/v0.6.0" komposyn-lg_0.6.0_linux_amd64.tar.gz)"
v070_binary="$(verify_release_assets v0.7.0 "$temp_dir/v0.7.0" komposyn-lg_0.7.0_linux_amd64.tar.gz)"
bash "$(dirname "${BASH_SOURCE[0]}")/verify-release.sh" "$candidate_dir" "$v060_binary"
bash "$(dirname "${BASH_SOURCE[0]}")/verify-release.sh" "$candidate_dir" "$v070_binary"
