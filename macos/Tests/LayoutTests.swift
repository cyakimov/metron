import AppKit
import SwiftUI

@MainActor private final class LayoutFixture: ObservableObject {
    @Published var states = [ProviderState(id: "claude"), ProviderState(id: "codex")]
    let layout = PopoverLayout()
    let popover = NSPopover()
    var renderedSize = NSSize.zero

    func load(windows count: Int) {
        let windows = (0..<count).map { index in
            QuotaWindow(id: "limit-\(index)", Label: index == 0 ? "Session" : "Weekly model allowance \(index)",
                        UsedPercent: index == 0 ? 110 : 42, Duration: 0, ResetsAt: Date().addingTimeInterval(3600))
        }
        states = ["claude", "codex"].map {
            ProviderState(id: $0, snapshot: Snapshot(Windows: windows, Details: ["Account details"], Notices: nil, ObservedAt: .now))
        }
    }
}

private struct FixtureDashboard: View {
    @ObservedObject var fixture: LayoutFixture
    var body: some View {
        Dashboard(states: fixture.states, refresh: {}, quit: {}, layout: fixture.layout, showSettings: {}, resize: {
            fixture.renderedSize = $0
            fixture.popover.contentSize = $0
        })
    }
}

@main struct LayoutTests {
    @MainActor static func main() {
        let now = Date(timeIntervalSince1970: 1_000_000)
        precondition(QuotaTimeLabel.reset(nil, now: now) == "Reset time not reported")
        precondition(QuotaTimeLabel.reset(now, now: now) == "Reset pending confirmation")
        precondition(QuotaTimeLabel.reset(now.addingTimeInterval(-1), now: now) == "Reset pending confirmation")
        precondition(QuotaTimeLabel.reset(now.addingTimeInterval(0.1), now: now) == "Resets in 1s")
        precondition(QuotaTimeLabel.reset(now.addingTimeInterval(3661), now: now) == "Resets in 1h 1m")
        precondition(QuotaTimeLabel.reset(now.addingTimeInterval(90000), now: now) == "Resets in 1d 1h")
        precondition(QuotaTimeLabel.updated(now.addingTimeInterval(10), now: now) == "Updated just now")
        precondition(QuotaTimeLabel.updated(now.addingTimeInterval(-61), now: now) == "Updated 1m 1s ago")
        NSApplication.shared.setActivationPolicy(.prohibited)
        let fixture = LayoutFixture()
        let controller = NSHostingController(rootView: FixtureDashboard(fixture: fixture))
        fixture.popover.contentViewController = controller
        let host = controller.view

        func settle() {
            for _ in 0..<10 {
                host.layoutSubtreeIfNeeded()
                RunLoop.main.run(until: Date().addingTimeInterval(0.02))
            }
            precondition(fixture.renderedSize.width == 392, "Popover width changed")
            precondition(fixture.renderedSize.height <= fixture.layout.maximumHeight, "Popover exceeds screen height")
        }

        fixture.layout.prepareToOpen(fixture.popover)
        let preparedHeight = fixture.renderedSize.height
        precondition(preparedHeight > 150, "Layout must settle before showing, without waiting for a run-loop turn")
        settle()
        precondition(fixture.renderedSize.height == preparedHeight, "Opening must not resize after preparation")
        fixture.layout.finishOpening()
        settle()
        let loadingHeight = fixture.renderedSize.height
        precondition(loadingHeight > 150, "Loading providers must remain visible")

        fixture.load(windows: 3)
        settle()
        let quotaHeight = fixture.renderedSize.height
        precondition(quotaHeight > 400 && quotaHeight < 700, "Six quota bars must fit without scrolling, got \(quotaHeight)")

        fixture.layout.prepareToOpen(fixture.popover)
        let openingHeight = fixture.renderedSize.height
        fixture.load(windows: 20)
        settle()
        precondition(fixture.renderedSize.height == openingHeight, "Incoming data must not resize during opening")
        fixture.layout.finishOpening()
        settle()
        precondition(fixture.renderedSize.height == 700, "Overflow must use available height")

        fixture.layout.maximumHeight = 340
        fixture.layout.prepareToOpen(fixture.popover)
        precondition(fixture.renderedSize.height == 340, "Screen bounds must settle before reopening")
        settle()
        fixture.layout.finishOpening()
        settle()
        precondition(fixture.renderedSize.height == 340, "Overflow must adapt to smaller screens")

        fixture.layout.maximumHeight = 700
        fixture.states = [ProviderState(id: "claude", error: "Sign-in required"), ProviderState(id: "codex", error: "Provider unavailable")]
        settle()
        precondition(fixture.renderedSize.height > 150 && fixture.renderedSize.height < quotaHeight,
                     "Unavailable providers must remain visible and release unused space")
        print("Passed time-label boundaries and native layout: preparation before opening, stable opening size, deferred updates, visible quota bars, overflow, screen resizing, and unavailable providers")
    }
}
