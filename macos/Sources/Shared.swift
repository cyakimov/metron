import Foundation

struct QuotaWindow: Codable, Identifiable {
    let id: String
    let Label: String
    let UsedPercent: Double
    let Duration: Double
    let ResetsAt: Date?
    enum CodingKeys: String, CodingKey { case id = "ID", Label, UsedPercent, Duration, ResetsAt }
    var remaining: Double { max(0, min(100, 100 - UsedPercent)) }
}
struct Snapshot: Codable {
    let Windows: [QuotaWindow]?
    let Details: [String]?
    let Notices: [String]?
    let ObservedAt: Date
    var windows: [QuotaWindow] { Windows ?? [] }
    var remaining: Double? { windows.map(\.remaining).min() }
    var statusBarWindow: QuotaWindow? {
        let fiveHourWindows = windows.filter { $0.Duration == 18_000_000_000_000 }
        return (fiveHourWindows.isEmpty ? windows : fiveHourWindows).min { $0.remaining < $1.remaining }
    }
}
struct ProviderState: Codable, Identifiable {
    let id: String
    var snapshot: Snapshot?
    var error: String?
    var retryAt: Date?
    var nextFetch: Date = .distantPast
    var fetching = false
    var name: String { id == "claude" ? "Claude" : "Codex" }
    var stale: Bool {
        guard let snapshot else { return true }
        return error != nil || Date().timeIntervalSince(snapshot.ObservedAt) > (id == "claude" ? 660 : 180)
    }
}
enum SharedStore {
    static let group = "group.com.cyakimov.metron"
    static var url: URL {
        let base = FileManager.default.containerURL(forSecurityApplicationGroupIdentifier: group)
            ?? FileManager.default.homeDirectoryForCurrentUser.appendingPathComponent("Library/Application Support/Metron")
        try? FileManager.default.createDirectory(at: base, withIntermediateDirectories: true)
        return base.appendingPathComponent("snapshots.json")
    }
    static var decoder: JSONDecoder {
        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .custom { decoder in
            let text = try decoder.singleValueContainer().decode(String.self)
            let formatter = ISO8601DateFormatter()
            formatter.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
            if let date = formatter.date(from: text) { return date }
            formatter.formatOptions = [.withInternetDateTime]
            if let date = formatter.date(from: text) { return date }
            throw DecodingError.dataCorrupted(.init(codingPath: decoder.codingPath, debugDescription: "Invalid date"))
        }
        return decoder
    }
    static func read() -> [ProviderState] {
        guard let data = try? Data(contentsOf: url), let states = try? decoder.decode([ProviderState].self, from: data) else { return [] }
        return states
    }
    static func write(_ states: [ProviderState]) {
        let encoder = JSONEncoder()
        encoder.dateEncodingStrategy = .iso8601
        if let data = try? encoder.encode(states) { try? data.write(to: url, options: .atomic) }
    }
}
struct AlertRecord: Codable {
    var used: Double
    var reset: Date?
    var warned: Int
}
struct AlertDecision {
    static func evaluate(previous: AlertRecord?, window: QuotaWindow) -> (AlertRecord, String?) {
        let tier = window.remaining <= 10 ? 2 : window.remaining <= 30 ? 1 : 0
        guard let previous else { return (AlertRecord(used: window.UsedPercent, reset: window.ResetsAt, warned: tier), nil) }
        let resetChanged = previous.reset != nil && window.ResetsAt != nil && window.ResetsAt! > previous.reset!
        let recovered = resetChanged && previous.used >= 70 && window.UsedPercent < 70
        let warned = resetChanged ? tier : max(previous.warned, tier)
        let message: String? = recovered ? "Allowance restored" : tier > previous.warned ? "\(Int(window.remaining))% remaining" : nil
        return (AlertRecord(used: window.UsedPercent, reset: window.ResetsAt, warned: warned), message)
    }
}
