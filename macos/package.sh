#!/bin/bash
set -euo pipefail
cd "$(dirname "$0")/.."
: "${VERSION:?Set the release version, for example 0.1.3}"
ARCHS="arm64 x86_64" bash macos/build.sh
APP="$PWD/dist/macos/Metron.app"
DMG="$PWD/dist/macos/Metron-$VERSION-universal.dmg"
stage=$(mktemp -d "$PWD/dist/macos/.dmg.XXXXXX")
trap 'rm -rf "$stage"' EXIT
ditto "$APP" "$stage/Metron.app"
ln -s /Applications "$stage/Applications"
hdiutil create -volname Metron -srcfolder "$stage" -ov -format UDZO "$DMG"
hdiutil verify "$DMG"
(cd dist/macos && shasum -a 256 "$(basename "$DMG")" > "$(basename "$DMG").sha256")
echo "$DMG"
