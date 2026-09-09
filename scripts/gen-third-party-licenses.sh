#!/usr/bin/env bash
# Collects the licence text of every module linked into the binary.
#
# MIT and BSD both require reproducing the copyright and permission notice in
# binary redistributions, so this file ships inside every release archive.
# Regenerate with `make licenses` whenever dependencies change.
set -euo pipefail
cd "$(dirname "$0")/.."

# Byte-order sorting, so the file is identical on a developer's machine and on
# CI. Without this, locales that collate case-insensitively order the modules
# differently and the freshness check in the release workflow fails on ordering
# alone.
export LC_ALL=C

out=THIRD_PARTY_LICENSES
mod=$(go list -m)

{
  echo "Third-party licences"
  echo "===================="
  echo
  echo "CommHub links the modules below. Each is reproduced in full, as their"
  echo "licences require. CommHub's own licence is in LICENSE."
  echo
} > "$out"

go list -deps -f '{{if and (not .Standard) .Module}}{{.Module.Path}}{{"\t"}}{{.Module.Dir}}{{end}}' ./cmd/commhub \
  | sort -u | while IFS=$'\t' read -r path dir; do
  [ "$path" = "$mod" ] && continue
  [ -n "$dir" ] || continue
  lic=$(find "$dir" -maxdepth 1 -iregex '.*/\(LICENSE\|LICENCE\|COPYING\|LICENSE\.txt\|LICENSE\.md\)' 2>/dev/null | head -1)
  [ -n "$lic" ] || { echo "  !! no licence file found for $path" >&2; continue; }
  {
    echo
    echo "--------------------------------------------------------------------"
    echo "$path"
    echo "--------------------------------------------------------------------"
    echo
    cat "$lic"
  } >> "$out"
done

echo "wrote $out ($(grep -c '^----' "$out" | awk '{print $1/2}') modules)"
