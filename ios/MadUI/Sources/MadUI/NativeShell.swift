import MadKit
import SwiftUI

/// The native window of mad, on the iPhone and on the Mac: a sidebar and a detail. For now both are
/// placeholders (a plain list of what the Mac has, and a note); the real home, channel and agent
/// views are built into this shell by the agents who own them, reading the stores here.
///
/// It owns a `DeckStore` for `api` and polls it while it is on screen and the app is active. Child
/// views read the store from the environment (`@Environment(DeckStore.self)`).
public struct NativeShell: View {
    @State private var deck: DeckStore
    @State private var selection: Selection?
    @Environment(\.scenePhase) private var scenePhase

    /// What the sidebar can select.
    public enum Selection: Hashable, Sendable {
        case channel(String)
        case agent(String)
    }

    public init(api: MadAPI, title: String = "mad") {
        _deck = State(initialValue: DeckStore(api: api))
        self.title = title
    }

    private let title: String

    public var body: some View {
        NavigationSplitView {
            Sidebar(selection: $selection)
                .navigationTitle(deck.deck?.machine ?? title)
        } detail: {
            Detail(selection: selection)
        }
        .environment(deck)
        // Polls while the app is active; the task restarts when it comes back, so it looks at once.
        .task(id: scenePhase) {
            guard scenePhase == .active else { return }
            await deck.run()
        }
        .overlay(alignment: .top) { LinkNote(link: deck.link) }
    }
}

/// A thin note at the top when the Mac cannot be reached or refuses the token.
public struct LinkNote: View {
    let link: LinkState
    public init(link: LinkState) { self.link = link }

    public var body: some View {
        switch link {
        case .refused: note("Token refused: pair this Mac again.", "key.slash")
        case .offline: note("The Mac cannot be reached.", "wifi.slash")
        case .failed(let why): note(why, "exclamationmark.triangle")
        case .connecting, .ok: EmptyView()
        }
    }

    private func note(_ text: String, _ icon: String) -> some View {
        Label(text, systemImage: icon)
            .font(.subheadline)
            .foregroundStyle(.orange)
            .padding(.horizontal, 12).padding(.vertical, 8)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(.regularMaterial)
    }
}

struct Sidebar: View {
    @Environment(DeckStore.self) private var deck
    @Binding var selection: NativeShell.Selection?

    var body: some View {
        List(selection: $selection) {
            if let d = deck.deck {
                if !d.channels.isEmpty {
                    Section("Channels") {
                        ForEach(d.channels) { c in
                            VStack(alignment: .leading, spacing: 2) {
                                Text(c.title)
                                if !c.lastText.isEmpty { Text(c.lastText).font(.subheadline).foregroundStyle(.secondary).lineLimit(1) }
                            }
                            .tag(NativeShell.Selection.channel(c.id))
                        }
                    }
                }
                ForEach(d.projects.filter { !$0.agents.isEmpty }) { p in
                    Section(p.name) {
                        ForEach(p.agents) { a in
                            HStack {
                                Text(a.label)
                                Spacer()
                                Text(d.word(for: a.state)).font(.subheadline).foregroundStyle(.secondary)
                            }
                            .tag(NativeShell.Selection.agent(a.id))
                        }
                    }
                }
            }
        }
        .overlay {
            if deck.deck == nil, deck.link == .connecting { ProgressView() }
        }
    }
}

struct Detail: View {
    @Environment(DeckStore.self) private var deck
    let selection: NativeShell.Selection?

    var body: some View {
        switch selection {
        case .channel(let id)?:
            placeholder(deck.channel(id: id)?.title ?? id, "The channel view comes here.")
        case .agent(let id)?:
            placeholder(deck.agent(id: id)?.label ?? id, "The agent's conversation comes here.")
        case nil:
            placeholder("mad", "Choose a channel or an agent.")
        }
    }

    private func placeholder(_ title: String, _ note: String) -> some View {
        VStack(spacing: 8) {
            Text(title).font(.title2.weight(.semibold))
            Text(note).foregroundStyle(.secondary)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }
}
