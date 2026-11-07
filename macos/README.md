# Metron for macOS

Native menu bar monitoring for Claude and Codex, backed by the existing Go providers.
Requires macOS 14 or later.
Building from source requires an SDK from Xcode 26 or later and the Go version specified by the root module.

## Homebrew distribution

The macOS app release workflow builds a universal application containing Apple Silicon and Intel versions of the app, provider helper, and widget extension.
It packages `Metron.app` in a DMG with an Applications shortcut, publishes the DMG and its SHA-256 checksum to the matching GitHub release, and opens a pull request adding or updating `Casks/metron-app.rb` in `cyakimov/homebrew-tap`.
The cask is generated from `macos/metron-app.rb.in` using the released artifact's checksum.
The existing `metron` terminal formula continues independently.

After the first app release and its cask pull request are merged, installation will be:

```sh
brew install --cask cyakimov/tap/metron-app
open -a Metron
```

Subsequent versions use `brew upgrade --cask cyakimov/tap/metron-app`.
The cask quits Metron when uninstalling or upgrading its app bundle.

CI builds are ad-hoc signed and are not notarized by Apple.
macOS may block first launch and require the user to approve the app in System Settings > Privacy & Security.
The cask retains Homebrew's normal quarantine behavior.
Apple Developer membership, Developer ID signing, and notarization can be added later without changing the install command.

## Build and run locally

From the repository root:

```sh
bash macos/build.sh
open dist/macos/Metron.app
```

The default build targets the current Mac's architecture and signs ad hoc.
Use `ARCHS="arm64 x86_64" VERSION=0.1.3 bash macos/build.sh` for a universal build with matching app and extension versions.
Set `SIGNING_IDENTITY` to a configured signing identity to override ad-hoc signing.

The app bundles its provider helper; installing the Metron terminal binary is unnecessary.
Existing Claude Code and Codex CLI authentication is required.
Finder launches do not inherit custom shell environment variables.
Select the Codex executable in settings when automatic detection is insufficient.

The app opens its menu bar popover on launch.
Click the star and account percentages to reopen it.
Status bar percentages prefer remaining 5-hour quota and fall back to an available limit when no 5-hour window is reported.
The tooltip identifies the selected window for each provider; `--` means no quota window is available.
The gear opens settings; the power button quits.
The popover shows every quota window in compact provider sections and scrolls only when the content exceeds available display height.
Expand account details to read notices, account information, and full errors.
On macOS 26 and later, controls use Liquid Glass; older supported systems use standard native controls.
Notifications and launch at login are opt-in.

## Release

Merge the native app changes and tap CI changes before publishing the first app release.
Push a stable `vX.Y.Z` tag to run both the existing CLI Homebrew workflow and the macOS app release workflow.
The app workflow requires only the existing `HOMEBREW_TAP_TOKEN` secret, with permission to push branches and open pull requests in the tap.
It does not require an Apple account or Apple signing secrets.

To build and inspect a DMG locally:

```sh
VERSION=0.1.3 bash macos/package.sh
```

The output is `dist/macos/Metron-0.1.3-universal.dmg` and its `.sha256` file.
CI uploads these assets without replacing an existing release asset.
If the tap pull request step fails after publication, rerun only that failed job; avoid rebuilding and replacing an already published DMG.
The workflow can also be dispatched manually with an existing stable tag.
The first such tag must contain the native app and release scripts.
Merge the generated cask pull request after its checks pass to make the version installable through Homebrew.

## Widgets and saved state

The WidgetKit extension provides small provider views and a medium combined view.
The app and extension declare the App Group `group.com.cyakimov.metron` for credential-free snapshots.
Widget refreshes are controlled by macOS and can lag behind the menu bar.
Keep Metron running to poll accounts and deliver notifications.
An expired countdown remains pending until the provider confirms the reset.

Widget discovery has been verified locally using `pluginkit`.
Widget rendering, App Group access on another Mac, notification delivery, and launch at login still need interactive acceptance testing on a distributed build.
Ad-hoc signing does not establish provisioned App Group access on other Macs.

## Validation

```sh
go test -race ./...
go vet ./...
bash macos/test.sh
ARCHS="arm64 x86_64" bash macos/build.sh
codesign --verify --deep --strict dist/macos/Metron.app
```

Logic checks cover quota selection, alert transitions, reset confirmation, persistence, over-limit display, and provider timestamps.
Native layout checks cover preparation before opening, visible quota rows, overflow, screen resizing, countdown boundaries, and unavailable providers without reading real accounts.
