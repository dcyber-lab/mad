import MadKit
import SwiftUI

/// What waits on the person: the inbox's items, each drawing its own acts.
struct NeedsView: View {
    @ObservedObject var model: MacModel
    /// An item's "open" act: where it is settled at length.
    let openItem: (DeckSnapshot.InboxItem) -> Void

    @State private var asking: (item: DeckSnapshot.InboxItem, act: DeckSnapshot.Act)?
    @State private var typed = ""

    var body: some View {
        VStack(spacing: 0) {
            LinkBanner(link: model.link)
            content
        }
        .alert(asking?.act.label ?? "", isPresented: Binding(get: { asking != nil }, set: { if !$0 { asking = nil } })) {
            TextField("Say it in words", text: $typed, axis: .vertical)
            Button("Send") {
                if let a = asking { Task { await model.perform(a.item, a.act, text: typed) } }
            }
            Button("Cancel", role: .cancel) {}
        }
    }

    @ViewBuilder private var content: some View {
        if let snap = model.snapshot {
            if snap.inbox.isEmpty {
                ContentUnavailableView("Nothing waits on you", systemImage: "checkmark.circle",
                    description: Text("Agents that need you will show up here."))
            } else {
                List {
                    if !snap.waiting.isEmpty {
                        Section("Needs you") { cards(snap.waiting, snap) }
                    }
                    if !snap.finished.isEmpty {
                        Section("Finished") { cards(snap.finished, snap) }
                    }
                }
                .refreshable { await model.refresh() }
            }
        } else if case .failed = model.link {
            Spacer()
        } else {
            ProgressView().frame(maxHeight: .infinity)
        }
    }

    private func cards(_ items: [DeckSnapshot.InboxItem], _ snap: DeckSnapshot) -> some View {
        ForEach(items) { item in
            ItemCard(item: item, agent: snap.agent(id: item.agent), busy: model.busy.contains(item.key)) { act in
                if act.isOpen {
                    openItem(item)
                } else if act.input {
                    typed = ""
                    asking = (item, act)
                } else {
                    Task { await model.perform(item, act) }
                }
            }
        }
    }
}

struct ItemCard: View {
    let item: DeckSnapshot.InboxItem
    let agent: DeckSnapshot.Agent?
    let busy: Bool
    let act: (DeckSnapshot.Act) -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack(alignment: .firstTextBaseline) {
                Text(item.title).font(.body.weight(.semibold))
                if let a = agent, a.label != item.title {
                    Text(a.label).font(.subheadline).foregroundStyle(.secondary)
                }
                Spacer()
                if let since = item.since {
                    // Short and quiet ("2m"), re-read every half minute.
                    TimelineView(.periodic(from: .now, by: 30)) { ctx in
                        Text(Age.short(from: since, to: ctx.date)).font(.subheadline).foregroundStyle(.secondary)
                    }
                }
            }
            if let c = item.context, !c.isEmpty {
                Text(c).font(.subheadline).foregroundStyle(.secondary)
            }
            if let t = item.text, !t.isEmpty {
                Text(t)
                    .font(item.mono ? .system(.callout, design: .monospaced) : .body)
                    .lineLimit(item.full ? nil : 6)
                    .textSelection(.enabled)
            }
            if let b = item.body, !b.isEmpty {
                Text(b)
                    .font(item.mono ? .system(.footnote, design: .monospaced) : .callout)
                    .foregroundStyle(item.full ? .primary : .secondary)
                    .lineLimit(item.full ? nil : 4)
                    .textSelection(.enabled)
            }
            if !item.acts.isEmpty {
                FlowButtons(acts: item.acts, disabled: busy, tap: act)
                    .padding(.top, 6)
            }
        }
        .padding(.vertical, 10)
        .overlay(alignment: .topTrailing) { if busy { ProgressView() } }
    }
}

/// An item's acts as buttons, in the order it gives them, wrapping.
/// A button's words: they wrap inside the button when the label is long.
private struct ActLabel: View {
    let text: String
    init(_ text: String) { self.text = text }
    var body: some View { Text(text).multilineTextAlignment(.center).fixedSize(horizontal: false, vertical: true) }
}

struct FlowButtons: View {
    let acts: [DeckSnapshot.Act]
    let disabled: Bool
    let tap: (DeckSnapshot.Act) -> Void

    var body: some View {
        WrapLayout(spacing: 10) {
            ForEach(acts) { a in
                if a.primary {
                    Button { tap(a) } label: { ActLabel(a.label) }.buttonStyle(.borderedProminent)
                } else {
                    Button { tap(a) } label: { ActLabel(a.label) }.buttonStyle(.bordered)
                }
            }
        }
        .controlSize(.regular)
        .disabled(disabled)
    }
}

/// Lays children left to right, wrapping to a new line when the row is full.
/// A child wider than a line gets a line of its own at most the container's
/// width, and wraps what it holds inside (a long button label).
struct WrapLayout: Layout {
    var spacing: CGFloat = 8

    func sizeThatFits(proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) -> CGSize {
        arrange(subviews, width: proposal.width ?? .infinity).size
    }

    func placeSubviews(in bounds: CGRect, proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) {
        let r = arrange(subviews, width: bounds.width)
        for (i, p) in r.origins.enumerated() {
            subviews[i].place(at: CGPoint(x: bounds.minX + p.x, y: bounds.minY + p.y),
                              proposal: ProposedViewSize(width: r.sizes[i].width, height: r.sizes[i].height))
        }
    }

    private func arrange(_ subviews: Subviews, width: CGFloat) -> (size: CGSize, origins: [CGPoint], sizes: [CGSize]) {
        var origins: [CGPoint] = [], sizes: [CGSize] = []
        var x: CGFloat = 0, y: CGFloat = 0, rowH: CGFloat = 0, maxW: CGFloat = 0
        for s in subviews {
            // What it wants on one line, then what it takes when it may be no wider than a line.
            let ideal = s.sizeThatFits(.unspecified)
            let w = min(ideal.width, width)
            let sz = s.sizeThatFits(ProposedViewSize(width: w, height: nil))
            let size = CGSize(width: min(max(sz.width, 0), w), height: sz.height)
            if x > 0, x + size.width > width { x = 0; y += rowH + spacing; rowH = 0 }
            origins.append(CGPoint(x: x, y: y))
            sizes.append(size)
            x += size.width + spacing
            rowH = max(rowH, size.height)
            maxW = max(maxW, x - spacing)
        }
        return (CGSize(width: maxW, height: y + rowH), origins, sizes)
    }
}

enum Age {
    /// "now", "5m", "3h", "2d": how long ago, in one short word.
    static func short(from: Date, to now: Date) -> String {
        let s = max(0, Int(now.timeIntervalSince(from)))
        switch s {
        case ..<60: return "now"
        case ..<3600: return "\(s / 60)m"
        case ..<86400: return "\(s / 3600)h"
        default: return "\(s / 86400)d"
        }
    }
}
