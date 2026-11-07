import WidgetKit
import SwiftUI
struct Entry: TimelineEntry {
    let date: Date
    let states: [ProviderState]
    let selected: String
}
struct TimelineSource: TimelineProvider {
    let selected: String
    func placeholder(in context: Context) -> Entry { Entry(date: .now, states: [], selected: selected) }
    func getSnapshot(in context: Context, completion: @escaping (Entry) -> Void) {
        completion(Entry(date: .now, states: SharedStore.read(), selected: selected))
    }
    func getTimeline(in context: Context, completion: @escaping (Timeline<Entry>) -> Void) {
        completion(Timeline(entries: [Entry(date: .now, states: SharedStore.read(), selected: selected)], policy: .after(Date().addingTimeInterval(300))))
    }
}
struct WidgetView: View {
    let entry: Entry
    @Environment(\.widgetFamily) var family
    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            Label("Metron", systemImage: "sparkles").font(.caption.weight(.semibold)).foregroundStyle(.secondary)
            HStack(alignment: .top, spacing: 18) {
                ForEach(family == .systemSmall ? [entry.selected] : ["claude", "codex"], id: \.self) { id in
                    let state = entry.states.first { $0.id == id }
                    VStack(alignment: .leading, spacing: 6) {
                        Text(id == "claude" ? "Claude" : "Codex").font(.caption.weight(.medium))
                        Text(state?.snapshot?.remaining.map { "\(Int($0))%" } ?? "--").font(.system(size: 28, weight: .semibold, design: .rounded)).minimumScaleFactor(0.7)
                        Text(state?.stale == false ? "remaining" : "stale · open Metron").font(.system(size: 10)).foregroundStyle(.secondary)
                        if let snapshot = state?.snapshot {
                            if let window = snapshot.windows.min(by: { $0.remaining < $1.remaining }) {
                                Text(window.Label).font(.system(size: 10)).lineLimit(1)
                                if let reset = window.ResetsAt {
                                    if reset > entry.date { Text(reset, style: .relative).font(.system(size: 10)).foregroundStyle(.secondary) }
                                    else { Text("Reset pending").font(.system(size: 10)) }
                                }
                            }
                            Text(snapshot.ObservedAt, style: .relative).font(.system(size: 9)).foregroundStyle(.tertiary)
                        } else { Text("Open Metron to sync").font(.system(size: 10)).foregroundStyle(.secondary) }
                    }.frame(maxWidth: .infinity, alignment: .leading)
                }
            }
        }.containerBackground(for: .widget) { Color.indigo.opacity(0.06) }.widgetURL(URL(string: "metron://open"))
    }
}
struct UsageWidget: Widget {
    let provider: String
    init() { provider = "claude" }
    init(provider: String) { self.provider = provider }
    var body: some WidgetConfiguration {
        StaticConfiguration(kind: "MetronUsage-" + provider, provider: TimelineSource(selected: provider)) { entry in WidgetView(entry: entry) }
            .configurationDisplayName(provider == "claude" ? "Claude limits" : "Codex limits")
            .description("Your allowance at a glance.").supportedFamilies([.systemSmall])
    }
}
struct CombinedWidget: Widget {
    var body: some WidgetConfiguration {
        StaticConfiguration(kind: "MetronUsage-combined", provider: TimelineSource(selected: "claude")) { entry in WidgetView(entry: entry) }
            .configurationDisplayName("AI limits").description("Claude and Codex together.").supportedFamilies([.systemMedium])
    }
}
@main struct MetronWidgets: WidgetBundle {
    var body: some Widget {
        UsageWidget(provider: "claude")
        UsageWidget(provider: "codex")
        CombinedWidget()
    }
}
