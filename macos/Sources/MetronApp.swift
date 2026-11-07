import SwiftUI
import AppKit
import ServiceManagement
import Combine

@MainActor final class AppDelegate: NSObject, NSApplicationDelegate, NSPopoverDelegate {
    let monitor = Monitor()
    private var statusItem: NSStatusItem!
    private let popover = NSPopover()
    private var preferences: NSWindow?
    private var subscription: AnyCancellable?
    private let layout = PopoverLayout()
    func applicationDidFinishLaunching(_ notification: Notification) {
        statusItem = NSStatusBar.system.statusItem(withLength: NSStatusItem.variableLength)
        statusItem.button?.target = self
        statusItem.button?.action = #selector(togglePopover)
        statusItem.button?.font = .monospacedDigitSystemFont(ofSize: 12, weight: .medium)
        popover.behavior = .transient
        popover.animates = false
        popover.delegate = self
        popover.contentViewController = NSHostingController(rootView: MonitoredDashboard(monitor: monitor, layout: layout, showSettings: { [weak self] in self?.showPreferences() }, resize: { [weak self] size in
            self?.popover.contentSize = size
        }))
        updateTitle()
        subscription = monitor.objectWillChange.sink { [weak self] _ in
            DispatchQueue.main.async { self?.updateTitle() }
        }
        DispatchQueue.main.async { self.togglePopover() }
    }
    private func updateTitle() {
        statusItem.button?.title = "✧ " + monitor.states.map { state in
            "\(state.id == "claude" ? "C" : "O") \(state.snapshot?.statusBarWindow.map { "\(Int($0.remaining))%" } ?? "--")\(state.stale ? "·" : "")"
        }.joined(separator: "  ")
        statusItem.button?.setAccessibilityLabel("Metron quota remaining")
        statusItem.button?.toolTip = monitor.states.map { state in
            state.snapshot?.statusBarWindow.map { "\(state.name): \($0.Label), \(Int($0.remaining))% remaining" }
                ?? "\(state.name): usage unavailable"
        }.joined(separator: "\n")
    }
    @objc private func togglePopover() {
        if popover.isShown { popover.performClose(nil) }
        else if let button = statusItem.button {
            let screen = button.window?.screen ?? NSScreen.main
            if let screen {
                let anchorY = button.window?.convertToScreen(button.convert(button.bounds, to: nil)).minY ?? screen.visibleFrame.maxY
                layout.maximumHeight = max(180, anchorY - screen.visibleFrame.minY - 24)
            }
            layout.prepareToOpen(popover)
            NSApp.activate(ignoringOtherApps: true)
            popover.show(relativeTo: button.bounds, of: button, preferredEdge: .minY)
            if let window = popover.contentViewController?.view.window {
                window.makeKey()
                window.makeFirstResponder(nil)
            }
        }
    }
    func popoverDidShow(_ notification: Notification) { layout.finishOpening() }
    func popoverDidClose(_ notification: Notification) { layout.finishOpening() }
    func application(_ application: NSApplication, open urls: [URL]) {
        if !popover.isShown { togglePopover() }
    }
    private func showPreferences() {
        popover.performClose(nil)
        if preferences == nil {
            let window = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 470, height: 500), styleMask: [.titled, .closable], backing: .buffered, defer: false)
            window.title = "Metron Settings"
            window.isReleasedWhenClosed = false
            window.contentView = NSHostingView(rootView: Preferences(monitor: monitor))
            window.center()
            preferences = window
        }
        preferences?.makeKeyAndOrderFront(nil)
        NSApp.activate(ignoringOtherApps: true)
    }
    func applicationWillTerminate(_ notification: Notification) { monitor.shutdown() }
}
@main struct MetronApp: App {
    @NSApplicationDelegateAdaptor(AppDelegate.self) var delegate
    var body: some Scene { Settings { EmptyView() } }
}

struct MonitoredDashboard: View {
    @ObservedObject var monitor: Monitor
    @ObservedObject var layout: PopoverLayout
    var showSettings: () -> Void
    var resize: (NSSize) -> Void

    var body: some View {
        Dashboard(states: monitor.states, refresh: { monitor.refresh(manual: true) }, quit: {
            monitor.shutdown()
            NSApp.terminate(nil)
        }, layout: layout, showSettings: showSettings, resize: resize)
    }
}

struct Preferences: View {
    @ObservedObject var monitor: Monitor
    @AppStorage("claudeInterval") private var claudeInterval = 300.0
    @AppStorage("codexInterval") private var codexInterval = 60.0
    @AppStorage("codexPath") private var codexPath = ""
    @State private var login = SMAppService.mainApp.status == .enabled
    @State private var loginError: String?
    var body: some View {
        Form {
            Section("Monitoring") {
                Picker("Claude refresh", selection: $claudeInterval) { Text("1 minute").tag(60.0); Text("5 minutes").tag(300.0); Text("10 minutes").tag(600.0) }
                Picker("Codex refresh", selection: $codexInterval) { Text("30 seconds").tag(30.0); Text("1 minute").tag(60.0); Text("5 minutes").tag(300.0) }
                TextField("Codex executable", text: $codexPath, prompt: Text("Automatic detection"))
                Text("Uses your existing Claude Code and Codex sign-ins. Retry delays take precedence over refresh intervals.").font(.caption).foregroundStyle(.secondary)
            }
            Section("Notifications") {
                Toggle("Low quota and recovery alerts", isOn: Binding(get: { monitor.alerts }, set: { monitor.setAlerts($0) }))
                Text("Warn at 30% and 10% remaining. Notify when a confirmed reset restores a low allowance.").font(.caption).foregroundStyle(.secondary)
                if let error = monitor.notificationError { Text(error).font(.caption).foregroundStyle(.orange) }
            }
            Section("Startup") {
                Toggle("Launch at login", isOn: $login).onChange(of: login) { _, enabled in
                    do { if enabled { try SMAppService.mainApp.register() } else { try SMAppService.mainApp.unregister() }; loginError = nil }
                    catch { loginError = error.localizedDescription; login = SMAppService.mainApp.status == .enabled }
                }
                if let loginError { Text(loginError).font(.caption).foregroundStyle(.orange) }
            }
            Section("Widgets") {
                Text("Add Metron from Edit Widgets on your desktop or in Notification Center. Keep Metron running to receive fresh usage and alerts.").font(.caption).foregroundStyle(.secondary)
            }
        }.formStyle(.grouped).padding(8).frame(width: 470, height: 500)
    }
}
