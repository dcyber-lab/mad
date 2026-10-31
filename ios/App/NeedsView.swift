import MadKit
import SwiftUI

/// What waits on the person: the inbox's items, each drawing its own acts.
struct NeedsView: View {
    @ObservedObject var model: MacModel
    let openDeck: () -> Void

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
                    openDeck()
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
        VStack(alignment: .leading, spacing: 6) {
            HStack(alignment: .firstTextBaseline) {
                Text(item.title).font(.headline)
                if let a = agent, a.label != item.title {
                    Text(a.label).font(.subheadline).foregroundStyle(.secondary)
                }
                Spacer()
                if let since = item.since {
                    Text(since, style: .relative).font(.caption).foregroundStyle(.secondary)
                }
            }
            if let c = item.context, !c.isEmpty {
                Text(c).font(.caption).foregroundStyle(.secondary)
            }
            if let t = item.text, !t.isEmpty {
                Text(t)
                    .font(item.mono ? .system(.callout, design: .monospaced) : .callout)
                    .lineLimit(item.full ? nil : 6)
                    .textSelection(.enabled)
            }
            if let b = item.body, !b.isEmpty {
                Text(b)
                    .font(item.mono ? .system(.footnote, design: .monospaced) : .footnote)
                    .foregroundStyle(item.full ? .primary : .secondary)
                    .lineLimit(item.full ? nil : 4)
                    .textSelection(.enabled)
            }
            if !item.acts.isEmpty {
                FlowButtons(acts: item.acts, disabled: busy, tap: act)
                    .padding(.top, 2)
            }
        }
        .padding(.vertical, 4)
        .overlay(alignment: .topTrailing) { if busy { ProgressView() } }
    }
}

/// An item's acts as buttons, in the order it gives them, wrapping.
struct FlowButtons: View {
    let acts: [DeckSnapshot.Act]
    let disabled: Bool
    let tap: (DeckSnapshot.Act) -> Void

    var body: some View {
        WrapLayout(spacing: 8) {
            ForEach(acts) { a in
                if a.primary {
                    Button(a.label) { tap(a) }.buttonStyle(.borderedProminent)
                } else {
                    Button(a.label) { tap(a) }.buttonStyle(.bordered)
                }
            }
        }
        .controlSize(.small)
        .disabled(disabled)
    }
}

/// Lays children left to right, wrapping to a new line when the row is full.
struct WrapLayout: Layout {
    var spacing: CGFloat = 8

    func sizeThatFits(proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) -> CGSize {
        arrange(subviews, width: proposal.width ?? .infinity).size
    }

    func placeSubviews(in bounds: CGRect, proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) {
        let r = arrange(subviews, width: bounds.width)
        for (i, p) in r.origins.enumerated() {
            subviews[i].place(at: CGPoint(x: bounds.minX + p.x, y: bounds.minY + p.y), proposal: .unspecified)
        }
    }

    private func arrange(_ subviews: Subviews, width: CGFloat) -> (size: CGSize, origins: [CGPoint]) {
        var origins: [CGPoint] = []
        var x: CGFloat = 0, y: CGFloat = 0, rowH: CGFloat = 0, maxW: CGFloat = 0
        for s in subviews {
            let sz = s.sizeThatFits(.unspecified)
            if x > 0, x + sz.width > width { x = 0; y += rowH + spacing; rowH = 0 }
            origins.append(CGPoint(x: x, y: y))
            x += sz.width + spacing
            rowH = max(rowH, sz.height)
            maxW = max(maxW, x - spacing)
        }
        return (CGSize(width: maxW, height: y + rowH), origins)
    }
}
