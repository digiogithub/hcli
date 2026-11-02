#!/usr/bin/env bash
# Build and package hcli release archives.
#
#   scripts/release.sh stage   <goos/goarch>...   build binaries into dist/stage/
#   scripts/release.sh archive                    archive every dist/stage/* dir
#   scripts/release.sh sums                       write dist/SHA256SUMS
#
# Layout produced by `stage` (one directory per target, which becomes the
# top-level directory of the archive):
#
#   dist/stage/hcli_<version>_<os>_<arch>/
#     hcli[.exe]  LICENSE  README.md
#     completions/hcli.bash  hcli.zsh  hcli.fish  hcli.ps1
#
# `archive` turns each one into dist/hcli_<version>_<os>_<arch>.tar.gz (.zip
# for windows). Staging and archiving are separate steps so that the CI can
# sign the binaries in between (codesign/notarize on macOS; on Windows the
# windows-sign job re-zips after Authenticode signing).
#
# Environment:
#   VERSION  version string injected into the binary (default: git describe).
#            A leading "v" is dropped from the archive names only.
#   COMMIT   commit hash (default: git rev-parse --short HEAD)
#   DATE     build date, RFC 3339 (default: commit date, for reproducibility)
#   GO       go command (default: go)
#
# Pure Go: everything is built with CGO_ENABLED=0, so any host can produce any
# target. No UPX (it rewrites the PE/Mach-O, trips antivirus heuristics and is
# not worth it for a ~15 MB CLI).
set -euo pipefail

cd "$(dirname "$0")/.."

GO="${GO:-go}"
PKG=github.com/digiogithub/hcli/internal/version
VERSION="${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo dev)}"
COMMIT="${COMMIT:-$(git rev-parse --short HEAD 2>/dev/null || echo unknown)}"
DATE="${DATE:-$(git log -1 --format=%cI 2>/dev/null || date -u +%Y-%m-%dT%H:%M:%SZ)}"
LDFLAGS="-s -w -X $PKG.Version=$VERSION -X $PKG.Commit=$COMMIT -X $PKG.Date=$DATE"
FILE_VERSION="${VERSION#v}"

DIST=dist
STAGE="$DIST/stage"

log() { printf '==> %s\n' "$*" >&2; }

build() { # goos goarch output
  CGO_ENABLED=0 GOOS="$1" GOARCH="$2" "$GO" build -trimpath -ldflags "$LDFLAGS" -o "$3" .
}

completions() { # outdir
  local host="$DIST/.host/hcli" out="$1"
  log "building host binary for completion scripts"
  build "$("$GO" env GOOS)" "$("$GO" env GOARCH)" "$host"
  mkdir -p "$out"
  "$host" completion bash > "$out/hcli.bash"
  "$host" completion zsh > "$out/hcli.zsh"
  "$host" completion fish > "$out/hcli.fish"
  "$host" completion powershell > "$out/hcli.ps1"
}

stage() {
  [ "$#" -gt 0 ] || { echo "stage: no targets (e.g. linux/amd64)" >&2; exit 2; }
  log "version $VERSION, commit $COMMIT, date $DATE"
  local comp="$DIST/.completions"
  rm -rf "$comp"
  completions "$comp"
  for target in "$@"; do
    local goos="${target%/*}" goarch="${target#*/}" ext=""
    [ "$goos" = windows ] && ext=.exe
    local name="hcli_${FILE_VERSION}_${goos}_${goarch}"
    local dir="$STAGE/$name"
    log "building $name"
    rm -rf "$dir"
    mkdir -p "$dir/completions"
    build "$goos" "$goarch" "$dir/hcli$ext"
    cp LICENSE README.md "$dir/"
    cp "$comp"/* "$dir/completions/"
  done
}

archive() {
  [ -d "$STAGE" ] || { echo "archive: nothing staged in $STAGE" >&2; exit 1; }
  local count=0
  for dir in "$STAGE"/hcli_*; do
    [ -d "$dir" ] || continue
    local name
    name="$(basename "$dir")"
    case "$name" in
      *_windows_*)
        rm -f "$DIST/$name.zip"
        (cd "$STAGE" && zip -qr "../$name.zip" "$name")
        log "$DIST/$name.zip"
        ;;
      *)
        # COPYFILE_DISABLE keeps macOS tar from adding ._ AppleDouble files.
        COPYFILE_DISABLE=1 tar -C "$STAGE" -czf "$DIST/$name.tar.gz" "$name"
        log "$DIST/$name.tar.gz"
        ;;
    esac
    count=$((count + 1))
  done
  [ "$count" -gt 0 ] || { echo "archive: no hcli_* directories in $STAGE" >&2; exit 1; }
}

sums() {
  (
    cd "$DIST"
    shopt -s nullglob
    files=(hcli_*.tar.gz hcli_*.zip)
    [ "${#files[@]}" -gt 0 ] || { echo "sums: no archives in $DIST" >&2; exit 1; }
    if command -v sha256sum >/dev/null; then
      sha256sum "${files[@]}" > SHA256SUMS
    else
      shasum -a 256 "${files[@]}" > SHA256SUMS
    fi
    cat SHA256SUMS
  )
}

cmd="${1:-}"
shift || true
case "$cmd" in
  stage) stage "$@" ;;
  archive) archive ;;
  sums) sums ;;
  *) echo "usage: $0 stage <goos/goarch>... | archive | sums" >&2; exit 2 ;;
esac
