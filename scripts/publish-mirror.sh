#!/usr/bin/env bash
set -euo pipefail

tag="${1:?release tag is required}"
source_repo="vappcloud/vappcloud-terraform-provider"
mirror_repo="vappcloud/terraform-provider-vappcloud"
tmp="$(mktemp -d)"
source_dir="$tmp/source"
mirror_dir="$tmp/mirror"
mkdir -p "$source_dir" "$mirror_dir"
trap 'rm -rf "$tmp"' EXIT

gh release download "$tag" --repo "$source_repo" --dir "$source_dir"
notes="$(gh release view "$tag" --repo "$source_repo" --json body --jq .body)"
if gh release view "$tag" --repo "$mirror_repo" >/dev/null 2>&1; then
  echo "mirror release $tag already exists" >&2
  exit 1
fi
gh release create "$tag" "$source_dir"/* --repo "$mirror_repo" --title "$tag" --notes "$notes"
gh release download "$tag" --repo "$mirror_repo" --dir "$mirror_dir"

source_names="$(find "$source_dir" -maxdepth 1 -type f -exec basename {} \; | sort)"
mirror_names="$(find "$mirror_dir" -maxdepth 1 -type f -exec basename {} \; | sort)"
test "$source_names" = "$mirror_names"

while IFS= read -r asset; do
  cmp "$source_dir/$asset" "$mirror_dir/$asset"
done <<EOF
$source_names
EOF

checksum_file="$(find "$source_dir" -maxdepth 1 -type f -name '*_SHA256SUMS' -print -quit)"
signature_file="${checksum_file}.sig"
test -n "$checksum_file"
test -f "$signature_file"
gpg --batch --verify "$signature_file" "$checksum_file"

if command -v sha256sum >/dev/null 2>&1; then
  (cd "$source_dir" && sha256sum --check "$(basename "$checksum_file")")
  (cd "$mirror_dir" && sha256sum --check "$(basename "$checksum_file")")
else
  (cd "$source_dir" && shasum -a 256 --check "$(basename "$checksum_file")")
  (cd "$mirror_dir" && shasum -a 256 --check "$(basename "$checksum_file")")
fi
