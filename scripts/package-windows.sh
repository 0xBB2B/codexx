#!/usr/bin/env bash
set -euo pipefail

VERSION="${1:?用法: package-windows.sh <版本> <owner/repo>}"
REPOSITORY="${2:?用法: package-windows.sh <版本> <owner/repo>}"

[[ $VERSION =~ ^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.]+)?$ ]] || { echo "版本号格式不对: $VERSION" >&2; exit 1; }

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUT="$ROOT/dist/windows-amd64"

(cd "$ROOT/core" && GOOS=windows GOARCH=amd64 go build \
  -ldflags "-H windowsgui -X modelbridge/internal/version.value=$VERSION -X modelbridge/internal/update.repository=$REPOSITORY" \
  -o "$OUT/model-bridge-core.exe" ./cmd/model-bridge-core)

dotnet publish "$ROOT/windows/ModelBridge.App/ModelBridge.App.csproj" -c Release -r win-x64 --self-contained true -o "$OUT" -p:Version="$VERSION"

MSYS2_ARG_CONV_EXCL='*' "/c/Program Files (x86)/Inno Setup 6/ISCC.exe" "$(cygpath -w "$ROOT/windows/installer/model-bridge.iss")" "/DAppVersion=$VERSION" "/DSourceDir=$(cygpath -w "$OUT")"
