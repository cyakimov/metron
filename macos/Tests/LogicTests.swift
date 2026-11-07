import Foundation

@main struct LogicTests {
    static func main() throws {
        let start = Date(timeIntervalSince1970: 1_800_000_000)
        func window(_ used: Double, _ reset: Date? = nil) -> QuotaWindow {
            QuotaWindow(id: "session", Label: "5h", UsedPercent: used, Duration: 18_000_000_000_000, ResetsAt: reset ?? start)
        }
        let (baseline, initial) = AlertDecision.evaluate(previous: nil, window: window(20))
        precondition(initial == nil)
        let (low, warning) = AlertDecision.evaluate(previous: baseline, window: window(72))
        precondition(warning == "28% remaining")
        let (unchanged, duplicate) = AlertDecision.evaluate(previous: low, window: window(75))
        precondition(duplicate == nil)
        let (critical, escalation) = AlertDecision.evaluate(previous: unchanged, window: window(95))
        precondition(escalation == "5% remaining")
        let (_, noFalseReset) = AlertDecision.evaluate(previous: critical, window: window(20))
        precondition(noFalseReset == nil)
        let (restored, recovery) = AlertDecision.evaluate(previous: critical, window: window(2, start.addingTimeInterval(18_000)))
        precondition(recovery == "Allowance restored")
        let (_, nextCycle) = AlertDecision.evaluate(previous: restored, window: window(75, start.addingTimeInterval(18_000)))
        precondition(nextCycle == "25% remaining")
        precondition(window(150).remaining == 0)
        let encoded = try JSONEncoder().encode(critical)
        let persisted = try JSONDecoder().decode(AlertRecord.self, from: encoded)
        precondition(AlertDecision.evaluate(previous: persisted, window: window(96)).1 == nil)
        let fixture = #"{"Windows":[{"ID":"5h","Label":"5h","UsedPercent":35,"Duration":18000000000000,"ResetsAt":"2026-11-07T03:00:00.123456789Z"}],"Details":null,"Notices":null,"ObservedAt":"2026-11-07T01:00:00Z"}"#
        let snapshot = try SharedStore.decoder.decode(Snapshot.self, from: Data(fixture.utf8))
        precondition(snapshot.remaining == 65 && snapshot.windows.first?.ResetsAt != nil)
        precondition(snapshot.statusBarWindow?.remaining == 65)
        let weekly = QuotaWindow(id: "weekly", Label: "7d", UsedPercent: 80, Duration: 604_800_000_000_000, ResetsAt: start)
        let mixed = Snapshot(Windows: [weekly, window(10)], Details: nil, Notices: nil, ObservedAt: start)
        precondition(mixed.statusBarWindow?.remaining == 90 && mixed.remaining == 20,
                     "Status bar must use 5h even when weekly quota is lower")
        let multiple = Snapshot(Windows: [window(10), window(30), weekly], Details: nil, Notices: nil, ObservedAt: start)
        precondition(multiple.statusBarWindow?.remaining == 70)
        let weeklyOnly = Snapshot(Windows: [weekly], Details: nil, Notices: nil, ObservedAt: start)
        precondition(weeklyOnly.statusBarWindow?.remaining == 20, "Available weekly quota must stay visible when 5h is absent")
        precondition(weeklyOnly.statusBarWindow?.Label == "7d")
        precondition(Snapshot(Windows: nil, Details: nil, Notices: nil, ObservedAt: start).statusBarWindow?.remaining == nil)
        let exhausted = Snapshot(Windows: [window(150), weekly], Details: nil, Notices: nil, ObservedAt: start)
        precondition(exhausted.statusBarWindow?.remaining == 0)
        print("Passed alert transitions, reset confirmation, persistence, over-limit display, snapshot decoding, and preferred 5h status-bar selection with weekly fallback")
    }
}
