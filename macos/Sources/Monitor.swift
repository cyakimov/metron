import AppKit
import SwiftUI
import UserNotifications
import WidgetKit
import ServiceManagement

struct HelperResponse: Decodable {
    let version: Int
    let provider: String
    let snapshot: Snapshot?
    let error: String?
    let retryAt: Date?
}

// Each provider owns a helper so an unavailable account cannot block the other.
final class HelperClient: @unchecked Sendable {
    private let lock = NSLock()
    private var process: Process?
    func stop() { lock.lock(); defer { lock.unlock() }; process?.terminate(); process = nil }
    func fetch(_ id: String, codexPath: String) throws -> HelperResponse {
        let task = Process()
        guard let executable = Bundle.main.url(forAuxiliaryExecutable: "metron-helper") else { throw CocoaError(.fileNoSuchFile) }
        task.executableURL = executable
        var env = ProcessInfo.processInfo.environment
        let extra = codexPath.isEmpty ? [] : [URL(fileURLWithPath: codexPath).deletingLastPathComponent().path]
        env["PATH"] = (extra + ["/opt/homebrew/bin", "/usr/local/bin", NSHomeDirectory() + "/.local/bin", "/usr/bin", "/bin"] + [env["PATH"] ?? ""]).joined(separator: ":")
        task.environment = env
        let input = Pipe(), output = Pipe()
        task.standardInput = input; task.standardOutput = output; task.standardError = FileHandle.nullDevice
        lock.lock(); process = task; lock.unlock()
        defer { stop() }
        try task.run()
        try input.fileHandleForWriting.write(contentsOf: JSONSerialization.data(withJSONObject: ["version": 1, "provider": id]))
        try input.fileHandleForWriting.write(contentsOf: Data([10]))
        try input.fileHandleForWriting.close()
        let deadline = DispatchWorkItem { if task.isRunning { task.terminate() } }
        DispatchQueue.global().asyncAfter(deadline: .now() + 22, execute: deadline)
        defer { deadline.cancel() }
        var data = Data()
        while let chunk = try output.fileHandleForReading.read(upToCount: 65536), !chunk.isEmpty {
            data.append(chunk)
            if data.count > 4 * 1024 * 1024 { throw CocoaError(.fileReadTooLarge) }
        }
        task.waitUntilExit()
        let response = try SharedStore.decoder.decode(HelperResponse.self, from: data)
        guard response.version == 1, response.provider == id else { throw CocoaError(.fileReadCorruptFile) }
        return response
    }
}

@MainActor final class Monitor: ObservableObject {
    @Published var states = [ProviderState(id: "claude"), ProviderState(id: "codex")]
    @Published var alerts = UserDefaults.standard.bool(forKey: "alerts")
    @Published var notificationError: String?
    private var records: [String: AlertRecord] = [:]
    private let clients = ["claude": HelperClient(), "codex": HelperClient()]
    private var timer: Timer?
    init() {
        for cached in SharedStore.read() {
            if let i = states.firstIndex(where: { $0.id == cached.id }) { states[i].snapshot = cached.snapshot; states[i].error = "Waiting for update" }
        }
        if let data = UserDefaults.standard.data(forKey: "alertRecords"), let saved = try? JSONDecoder().decode([String: AlertRecord].self, from: data) { records = saved }
        timer = Timer.scheduledTimer(withTimeInterval: 1, repeats: true) { [weak self] _ in Task { @MainActor in self?.refresh() } }
        NSWorkspace.shared.notificationCenter.addObserver(forName: NSWorkspace.didWakeNotification, object: nil, queue: .main) { [weak self] _ in Task { @MainActor in self?.refresh() } }
        refresh()
    }
    func shutdown() { timer?.invalidate(); clients.values.forEach { $0.stop() } }
    func refresh(manual: Bool = false) {
        for i in states.indices {
            let now = Date()
            guard !states[i].fetching, now >= (states[i].retryAt ?? .distantPast), manual || now >= states[i].nextFetch else { continue }
            states[i].fetching = true
            let id = states[i].id, client = clients[id]!
            let path = UserDefaults.standard.string(forKey: "codexPath") ?? ""
            Task {
                let result = await Task.detached { Result { try client.fetch(id, codexPath: path) } }.value
                states[i].fetching = false
                let configured = UserDefaults.standard.double(forKey: id + "Interval")
                let interval = configured > 0 ? max(30, configured) : (id == "claude" ? 300.0 : 60.0)
                states[i].nextFetch = Date().addingTimeInterval(interval)
                switch result {
                case .success(let response):
                    states[i].retryAt = response.retryAt
                    states[i].error = response.error
                    if let snapshot = response.snapshot { states[i].snapshot = snapshot; processAlerts(id, snapshot) }
                case .failure:
                    states[i].error = "Could not read usage. Check CLI location and sign-in."
                }
                SharedStore.write(states)
                WidgetCenter.shared.reloadAllTimelines()
            }
        }
    }
    func setAlerts(_ enabled: Bool) {
        if !enabled { alerts = false; UserDefaults.standard.set(false, forKey: "alerts"); return }
        UNUserNotificationCenter.current().requestAuthorization(options: [.alert, .sound]) { granted, error in
            Task { @MainActor in
                self.alerts = granted
                UserDefaults.standard.set(granted, forKey: "alerts")
                self.notificationError = granted ? nil : error?.localizedDescription ?? "Notifications are disabled. Enable Metron in System Settings → Notifications."
            }
        }
    }
    private func processAlerts(_ id: String, _ snapshot: Snapshot) {
        for window in snapshot.windows {
            let key = id + ":" + window.id
            let (record, message) = AlertDecision.evaluate(previous: records[key], window: window)
            records[key] = record
            if alerts, let message {
                let content = UNMutableNotificationContent()
                content.title = "\(id == "claude" ? "Claude" : "Codex") · \(window.Label)"
                content.body = message; content.sound = .default
                UNUserNotificationCenter.current().add(UNNotificationRequest(identifier: key, content: content, trigger: nil))
            }
        }
        if let data = try? JSONEncoder().encode(records) { UserDefaults.standard.set(data, forKey: "alertRecords") }
    }
}
