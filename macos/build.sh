#!/bin/bash
set -euo pipefail
cd "$(dirname "$0")/.."
BUILD_DIR="$PWD/dist/macos"
APP="$BUILD_DIR/Metron.app"
VERSION="${VERSION:-0.1.0}"
SIGNING_IDENTITY="${SIGNING_IDENTITY:--}"
ARCHS="${ARCHS:-$(uname -m)}"
if [[ ! "$VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "VERSION must be a stable version such as 0.1.3" >&2
  exit 1
fi
mkdir -p "$BUILD_DIR"
stage=$(mktemp -d "$BUILD_DIR/.build.XXXXXX")
trap 'rm -rf "$stage"' EXIT
helper_inputs=()
app_inputs=()
widget_inputs=()
for arch in $ARCHS; do
  case "$arch" in
    arm64) go_arch=arm64 ;;
    x86_64) go_arch=amd64 ;;
    *) echo "Unsupported architecture: $arch" >&2; exit 1 ;;
  esac
  GOOS=darwin GOARCH="$go_arch" CGO_ENABLED=0 go build -trimpath -o "$stage/helper-$arch" ./cmd/metron-helper
  xcrun swiftc -parse-as-library -O -target "$arch-apple-macos14.0" macos/Sources/*.swift -o "$stage/app-$arch"
  xcrun swiftc -parse-as-library -O -application-extension -target "$arch-apple-macos14.0" macos/Sources/Shared.swift macos/Widget/MetronWidget.swift -o "$stage/widget-$arch"
  helper_inputs+=("$stage/helper-$arch")
  app_inputs+=("$stage/app-$arch")
  widget_inputs+=("$stage/widget-$arch")
done
rm -rf "$APP"
WIDGET="$APP/Contents/PlugIns/MetronWidget.appex"
mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources" "$WIDGET/Contents/MacOS"
xcrun lipo -create "${helper_inputs[@]}" -output "$APP/Contents/MacOS/metron-helper"
xcrun lipo -create "${app_inputs[@]}" -output "$APP/Contents/MacOS/Metron"
xcrun lipo -create "${widget_inputs[@]}" -output "$WIDGET/Contents/MacOS/MetronWidget"
xcrun swift macos/Assets/AppIcon.swift "$stage/AppIcon.iconset"
xcrun iconutil -c icns "$stage/AppIcon.iconset" -o "$APP/Contents/Resources/AppIcon.icns"
cp macos/Info.plist "$APP/Contents/Info.plist"
cp macos/Widget/Info.plist "$WIDGET/Contents/Info.plist"
for plist in "$APP/Contents/Info.plist" "$WIDGET/Contents/Info.plist"; do
  /usr/libexec/PlistBuddy -c "Set :CFBundleShortVersionString $VERSION" "$plist"
  /usr/libexec/PlistBuddy -c "Set :CFBundleVersion $VERSION" "$plist"
done
cp macos/App.entitlements "$stage/app.entitlements"
cp macos/Widget.entitlements "$stage/widget.entitlements"
sign_args=(--force --sign "$SIGNING_IDENTITY")
if [[ "$SIGNING_IDENTITY" != - ]]; then
  sign_args+=(--options runtime --timestamp)
  if [[ -n "${SIGNING_KEYCHAIN:-}" ]]; then sign_args+=(--keychain "$SIGNING_KEYCHAIN"); fi
fi
codesign "${sign_args[@]}" "$APP/Contents/MacOS/metron-helper"
codesign "${sign_args[@]}" --entitlements "$stage/widget.entitlements" "$WIDGET"
codesign "${sign_args[@]}" --entitlements "$stage/app.entitlements" "$APP"
codesign --verify --deep --strict "$APP"
echo "$APP"
