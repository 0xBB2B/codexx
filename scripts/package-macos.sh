#!/usr/bin/env bash
set -euo pipefail

VERSION="${1:?用法: package-macos.sh <版本> <owner/repo>}"
REPOSITORY="${2:?用法: package-macos.sh <版本> <owner/repo>}"

[[ $VERSION =~ ^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.]+)?$ ]] || { echo "版本号格式不对: $VERSION" >&2; exit 1; }

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DIST="$ROOT/dist"
STAGE="$DIST/macos-stage"
APP="$STAGE/ModelBridge.app"
DMG="$DIST/model-bridge-$VERSION-macos.dmg"

build_core() {
  (cd "$ROOT/core" && GOOS=darwin GOARCH=arm64 go build \
    -ldflags "-X modelbridge/internal/version.value=$VERSION -X modelbridge/internal/update.repository=$REPOSITORY" \
    -o "$APP/Contents/MacOS/model-bridge-core" ./cmd/model-bridge-core)
}

build_shell() {
  swift build -c release --arch arm64 --package-path "$ROOT/macos"
  SHELL_BIN="$(swift build -c release --arch arm64 --package-path "$ROOT/macos" --show-bin-path)/ModelBridge"
}

assemble_app() {
  mkdir -p "$APP/Contents/MacOS"
  cp "$SHELL_BIN" "$APP/Contents/MacOS/ModelBridge"
  sed "s/__VERSION__/$VERSION/g" "$ROOT/macos/Resources/Info.plist" > "$APP/Contents/Info.plist"
}

sign_adhoc() {
  codesign --force --deep -s - "$APP"
}

make_dmg() {
  ln -s /Applications "$STAGE/Applications"
  rm -f "$DMG"
  hdiutil create -volname "ModelBridge" -srcfolder "$STAGE" -ov -format UDZO "$DMG"
}

trap 'rm -rf "$STAGE"' EXIT
rm -rf "$STAGE"
mkdir -p "$STAGE"
build_shell
assemble_app
build_core
sign_adhoc
make_dmg
