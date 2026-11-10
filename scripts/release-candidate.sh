#!/usr/bin/env bash
set -euo pipefail

: "${VERSION:?VERSION is required, for example VERSION=0.2.0-rc.1}"

repo_root=$(cd "$(dirname "$0")/.." && pwd)
cd "$repo_root"

if [[ "${ALLOW_DIRTY:-0}" != "1" ]]; then
  if ! git diff --quiet || ! git diff --cached --quiet; then
    echo "refusing to package a dirty checkout; commit the source or set ALLOW_DIRTY=1 for local development" >&2
    exit 1
  fi
fi

source_commit=$(git rev-parse HEAD)
release_root="$repo_root/dist/release-candidate"
version_root="$release_root/releases/download/v$VERSION"
latest_root="$release_root/releases/latest"
stage_root=$(mktemp -d "${TMPDIR:-/tmp}/leadfox-release.XXXXXX")
trap 'rm -rf "$stage_root"' EXIT

rm -rf "$release_root"
mkdir -p "$version_root" "$latest_root"

sha256_file() {
  if command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1" | awk '{print $1}'
  else
    sha256sum "$1" | awk '{print $1}'
  fi
}

specs=(
  "darwin arm64"
  "darwin amd64"
  "linux arm64"
  "linux amd64"
)

artifacts_json=()
checksums="$version_root/checksums.txt"
: > "$checksums"

for spec in "${specs[@]}"; do
  read -r os_name arch_name <<< "$spec"
  binary="$repo_root/dist/leadfox-$os_name-$arch_name"
  if [[ ! -f "$binary" ]]; then
    echo "missing $binary; run 'make dist VERSION=$VERSION' first" >&2
    exit 1
  fi
  if [[ ! -x "$binary" ]]; then
    echo "not executable: $binary" >&2
    exit 1
  fi

  file="leadfox_${VERSION}_${os_name}_${arch_name}.tar.gz"
  stage="$stage_root/$os_name-$arch_name"
  mkdir -p "$stage"
  cp "$binary" "$stage/leadfox"
  chmod 755 "$stage/leadfox"
  archive="$version_root/$file"
  tar -czf "$archive" -C "$stage" leadfox

  listing=$(tar -tzf "$archive")
  [[ "$listing" == "leadfox" ]] || { echo "archive does not contain exactly leadfox: $file" >&2; exit 1; }
  tar -tvzf "$archive" | awk 'NF && $1 ~ /^-/ && $NF == "leadfox" { found++ } END { exit !(found == 1) }'

  sha256=$(sha256_file "$archive")
  printf '%s  %s\n' "$sha256" "$file" >> "$checksums"
  artifacts_json+=("$os_name|$arch_name|$file|$sha256")
done

node - "$VERSION" "$source_commit" "$version_root/release-manifest.json" "${artifacts_json[@]}" <<'NODE'
const fs = require('node:fs');
const [version, sourceCommit, output, ...items] = process.argv.slice(2);
const artifacts = items.map((item) => {
  const [os, arch, file, sha256] = item.split('|');
  return { os, arch, file, sha256 };
});
const manifest = {
  version,
  source_commit: sourceCommit,
  artifacts,
};
fs.writeFileSync(output, `${JSON.stringify(manifest, null, 2)}\n`, { mode: 0o644 });
NODE

cp "$version_root/release-manifest.json" "$latest_root/release-manifest.json"
cp "$version_root/checksums.txt" "$latest_root/checksums.txt"

echo "release candidate created: $release_root"
echo "version: $VERSION"
echo "source commit: $source_commit"
echo "artifacts: ${#artifacts_json[@]}"
