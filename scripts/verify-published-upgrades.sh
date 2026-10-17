#!/usr/bin/env bash
set -euo pipefail

candidate_dir="${1:?usage: verify-published-upgrades.sh <candidate-release-dir> <temp-dir>}"
temp_dir="${2:?usage: verify-published-upgrades.sh <candidate-release-dir> <temp-dir>}"
expected_target="${3:-}"
repo='ayeshLK/komposyn'
identity='^https://github.com/ayeshLK/komposyn/.github/workflows/release.yml@refs/heads/master$'
issuer='https://token.actions.githubusercontent.com'
host_os="$(uname -s)"
host_arch="$(uname -m)"
case "$host_os:$host_arch" in
	Linux:x86_64|Linux:amd64) target=linux_amd64; archive_suffix=tar.gz; executable=komposyn-lg ;;
	Linux:aarch64|Linux:arm64) target=linux_arm64; archive_suffix=tar.gz; executable=komposyn-lg ;;
	Darwin:x86_64) target=darwin_amd64; archive_suffix=tar.gz; executable=komposyn-lg ;;
	Darwin:arm64) target=darwin_arm64; archive_suffix=tar.gz; executable=komposyn-lg ;;
	MINGW64_NT*:x86_64|MSYS_NT*:x86_64|CYGWIN_NT*:x86_64) target=windows_amd64; archive_suffix=zip; executable=komposyn-lg.exe ;;
	MINGWARM64_NT*:aarch64|MINGWARM64_NT*:arm64|MSYS_NT*:aarch64|MSYS_NT*:arm64|CYGWIN_NT*:aarch64|CYGWIN_NT*:arm64) target=windows_arm64; archive_suffix=zip; executable=komposyn-lg.exe ;;
	*) echo "unsupported native historical fixture host: $host_os/$host_arch" >&2; exit 1 ;;
esac
if [[ -n "$expected_target" && "$expected_target" != "$target" ]]; then
	echo "native target $target does not match workflow target $expected_target" >&2
	exit 1
fi
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
	if [[ "$archive_suffix" == zip ]]; then
		if command -v unzip >/dev/null 2>&1; then
			unzip -q "$directory/$asset" -d "$directory/extracted"
		else
			powershell.exe -NoProfile -Command "Expand-Archive -LiteralPath '$(cygpath -w "$directory/$asset")' -DestinationPath '$(cygpath -w "$directory/extracted")' -Force"
		fi
	else
		tar -xzf "$directory/$asset" -C "$directory/extracted"
	fi
	printf '%s\n' "$directory/extracted/$executable"
}

v060_binary="$(verify_release_assets v0.6.0 "$temp_dir/v0.6.0" "komposyn-lg_0.6.0_${target}.${archive_suffix}")"
v070_binary="$(verify_release_assets v0.7.0 "$temp_dir/v0.7.0" "komposyn-lg_0.7.0_${target}.${archive_suffix}")"
bash "$(dirname "${BASH_SOURCE[0]}")/verify-release.sh" "$candidate_dir" "$v060_binary"
bash "$(dirname "${BASH_SOURCE[0]}")/verify-release.sh" "$candidate_dir" "$v070_binary"
