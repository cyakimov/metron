import SwiftUI
import AppKit

@MainActor final class PopoverLayout: ObservableObject {
    static let chromeHeight: CGFloat = 110
    @Published var maximumHeight: CGFloat = 700
    @Published private(set) var openingViewportHeight: CGFloat?

    func prepareToOpen(_ popover: NSPopover) {
        openingViewportHeight = nil
        guard let view = popover.contentViewController?.view else { return }
        view.needsLayout = true
        view.layoutSubtreeIfNeeded()
        // Keep incoming snapshots from resizing the popover during AppKit's opening animation.
        openingViewportHeight = max(1, popover.contentSize.height - Self.chromeHeight)
        view.needsLayout = true
        view.layoutSubtreeIfNeeded()
    }

    func finishOpening() { openingViewportHeight = nil }
}

private struct NativeActionStyle: ViewModifier {
    @ViewBuilder func body(content: Content) -> some View {
        if #available(macOS 26, *) {
            content.buttonStyle(.glass).controlSize(.small)
        } else {
            content.buttonStyle(.bordered).controlSize(.small)
        }
    }
}

struct Dashboard: View {
    let states: [ProviderState]
    var refresh: () -> Void
    var quit: () -> Void
    @ObservedObject var layout: PopoverLayout
    var showSettings: () -> Void
    var resize: (NSSize) -> Void
    @State private var contentHeight: CGFloat = 1
    private var viewportHeight: CGFloat {
        layout.openingViewportHeight ?? min(contentHeight, max(1, layout.maximumHeight - PopoverLayout.chromeHeight))
    }

    var body: some View {
        VStack(spacing: 0) {
            HStack {
                Text("Metron").font(.headline)
                Spacer()
                Button { refresh() } label: { Image(systemName: "arrow.clockwise") }
                    .modifier(NativeActionStyle())
                    .help("Refresh usage").accessibilityLabel("Refresh usage")
                    .disabled(states.allSatisfy(\.fetching))
            }.frame(height: 32)
            Divider()
            ScrollView {
                TimelineView(.periodic(from: .now, by: 1)) { context in
                    VStack(spacing: 12) {
                        ForEach(Array(states.enumerated()), id: \.element.id) { index, state in
                            if index > 0 { Divider() }
                            ProviderSection(state: state, now: context.date)
                        }
                    }
                }
                .frame(maxWidth: .infinity)
                .fixedSize(horizontal: false, vertical: true)
                .onGeometryChange(for: CGFloat.self) { proxy in
                    proxy.size.height
                } action: { height in
                    contentHeight = height
                }
            }
            .scrollBounceBehavior(.basedOnSize)
            .frame(height: viewportHeight)
            .padding(.vertical, 12)
            Divider()
            HStack(spacing: 8) {
                Text(states.contains(where: \.fetching) ? "Refreshing…" : "Account-wide usage")
                    .font(.caption).foregroundStyle(.secondary)
                Spacer()
                Button { showSettings() } label: { Image(systemName: "gearshape") }
                    .modifier(NativeActionStyle()).help("Settings").accessibilityLabel("Settings")
                Button { quit() } label: { Image(systemName: "power") }
                    .modifier(NativeActionStyle()).help("Quit Metron").accessibilityLabel("Quit Metron")
            }.frame(height: 28)
        }
        .padding(.horizontal, 14).padding(.vertical, 12)
        .frame(width: 392)
        .onChange(of: viewportHeight, initial: true) { _, height in
            resize(NSSize(width: 392, height: height + PopoverLayout.chromeHeight))
        }
    }
}

struct ProviderSection: View {
    let state: ProviderState
    let now: Date
    @Environment(\.colorScheme) private var colorScheme
    @State private var detailsExpanded = false
    private var accent: Color {
        guard state.id == "claude" else { return .indigo }
        return colorScheme == .dark
            ? Color(red: 0.91, green: 0.62, blue: 0.47)
            : Color(red: 0.46, green: 0.20, blue: 0.11)
    }
    private var hasDetails: Bool {
        state.error != nil || !(state.snapshot?.Details ?? []).isEmpty || !(state.snapshot?.Notices ?? []).isEmpty
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack(spacing: 6) {
                Text(state.name).font(.headline)
                if state.fetching { ProgressView().controlSize(.mini).accessibilityLabel("Refreshing \(state.name)") }
                Spacer()
                if state.error != nil || (state.snapshot != nil && state.stale) {
                    Label(state.snapshot == nil ? "Unavailable" : "Stale", systemImage: "exclamationmark.circle")
                        .font(.caption).foregroundStyle(.orange)
                } else if let snapshot = state.snapshot {
                    Text(QuotaTimeLabel.updated(snapshot.ObservedAt, now: now))
                        .font(.caption2).foregroundStyle(.secondary)
                }
            }
            if let snapshot = state.snapshot {
                ForEach(snapshot.windows) { window in QuotaRow(window: window, accent: accent, now: now) }
                if snapshot.windows.isEmpty {
                    Text("No quota windows reported").font(.caption).foregroundStyle(.secondary)
                }
            } else if state.error == nil {
                Text("Reading account limits…").font(.caption).foregroundStyle(.secondary)
            }
            if hasDetails {
                DisclosureGroup(state.error == nil ? "Account details" : "Account details and error", isExpanded: $detailsExpanded) {
                    VStack(alignment: .leading, spacing: 6) {
                        ForEach(state.snapshot?.Details ?? [], id: \.self) { Text($0) }
                        ForEach(state.snapshot?.Notices ?? [], id: \.self) { notice in
                            Label(notice, systemImage: "info.circle").foregroundStyle(.orange)
                        }
                        if let error = state.error {
                            Text(error)
                            if state.snapshot == nil {
                                Text(state.id == "claude" ? "In Terminal: claude auth login" : "In Terminal: codex login")
                                    .font(.system(.caption, design: .monospaced))
                            }
                        }
                    }
                    .font(.caption).foregroundStyle(.secondary)
                    .textSelection(.enabled)
                    .fixedSize(horizontal: false, vertical: true)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(.top, 4)
                }.font(.caption)
            }
        }.frame(maxWidth: .infinity, alignment: .leading)
    }
}

struct QuotaRow: View {
    let window: QuotaWindow
    let accent: Color
    let now: Date
    private var tint: Color { window.remaining <= 10 ? .red : window.remaining <= 30 ? .orange : accent }

    var body: some View {
        VStack(alignment: .leading, spacing: 3) {
            HStack(alignment: .firstTextBaseline) {
                Text(window.Label).font(.subheadline).fixedSize(horizontal: false, vertical: true)
                Spacer(minLength: 8)
                Text("\(Int(window.remaining))% left").font(.subheadline.weight(.medium)).monospacedDigit()
                    .foregroundStyle(window.remaining <= 30 ? tint : .primary).fixedSize()
            }
            ProgressView(value: window.remaining, total: 100)
                .tint(tint)
                .accessibilityLabel(window.Label)
                .accessibilityValue("\(Int(window.remaining)) percent remaining")
            HStack {
                Text(QuotaTimeLabel.reset(window.ResetsAt, now: now)).monospacedDigit()
                Spacer(minLength: 4)
                if window.UsedPercent > 100 { Text("\(Int(window.UsedPercent))% used").fixedSize() }
            }.font(.caption2).foregroundStyle(.secondary)
        }
    }
}

// Explicit text avoids SwiftUI's live date formatting work during every animation layout pass.
enum QuotaTimeLabel {
    static func updated(_ observedAt: Date, now: Date) -> String {
        let seconds = max(0, Int(now.timeIntervalSince(observedAt)))
        return seconds == 0 ? "Updated just now" : "Updated \(duration(seconds)) ago"
    }

    static func reset(_ reset: Date?, now: Date) -> String {
        guard let reset else { return "Reset time not reported" }
        guard reset > now else { return "Reset pending confirmation" }
        return "Resets in \(duration(Int(ceil(reset.timeIntervalSince(now)))))"
    }

    private static func duration(_ seconds: Int) -> String {
        if seconds >= 86400 { return "\(seconds / 86400)d \(seconds % 86400 / 3600)h" }
        if seconds >= 3600 { return "\(seconds / 3600)h \(seconds % 3600 / 60)m" }
        if seconds >= 60 { return "\(seconds / 60)m \(seconds % 60)s" }
        return "\(seconds)s"
    }
}
