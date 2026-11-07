#!/bin/bash
set -euo pipefail
cd "$(dirname "$0")/.."
stage=$(mktemp -d "${TMPDIR:-/tmp}/metron-tests.XXXXXX")
trap 'rm -rf "$stage"' EXIT
target="$(uname -m)-apple-macos14.0"
xcrun swiftc -target "$target" -parse-as-library macos/Sources/Shared.swift macos/Tests/LogicTests.swift -o "$stage/logic-tests"
"$stage/logic-tests"
xcrun swiftc -target "$target" -parse-as-library macos/Sources/Shared.swift macos/Sources/Dashboard.swift macos/Tests/LayoutTests.swift -o "$stage/layout-tests"
"$stage/layout-tests"
