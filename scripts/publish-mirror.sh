#!/usr/bin/env bash
set -euo pipefail

tag="${1:?release tag is required}"
source_repo="vappcloud/vappcloud-terraform-provider"
mirror_repo="vappcloud/terraform-provider-vappcloud"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

gh release download "$tag" --repo "$source_repo" --dir "$tmp"
notes="$(gh release view "$tag" --repo "$source_repo" --json body --jq .body)"
if gh release view "$tag" --repo "$mirror_repo" >/dev/null 2>&1; then
  echo "mirror release $tag already exists" >&2
  exit 1
fi
gh release create "$tag" "$tmp"/* --repo "$mirror_repo" --title "$tag" --notes "$notes"

source_names="$(find "$tmp" -type f -maxdepth 1 -exec basename {} \; | sort)"
mirror_names="$(gh release view "$tag" --repo "$mirror_repo" --json assets --jq '.assets[].name' | sort)"
test "$source_names" = "$mirror_names"
