import Foundation
import MadKit
import MadUI

/// MadKit's `Deck` onto MadUI's `HomeModel`: what the home list draws, as the page's list draws it.
public enum HomeMapping {
    /// The list for `deck`:
    /// - "群聊" first: the channels and the person's DMs, the ones with a red dot first, then the newest
    ///   message first (the page's order);
    /// - then a section per project, with the agents that are not already in a team's chat (a team
    ///   with a channel is one row of the chats).
    public static func model(from deck: Deck, fallbackName: String = "mad") -> HomeModel {
        let needs = Set(deck.inbox.waiting.compactMap(\.agent))
        let teamed = Set(deck.channels.compactMap(\.team).flatMap(\.roles).compactMap(\.agent))

        var sections: [HomeSection] = []
        let chats = chatRows(deck, needs: needs)
        if !chats.isEmpty { sections.append(HomeSection(id: "chats", title: "群聊", kind: .chats, rows: chats)) }
        for p in deck.projects {
            let rows = p.agents.filter { !teamed.contains($0.id) }.map { agentRow($0, deck: deck, needs: needs) }
            if !rows.isEmpty { sections.append(HomeSection(id: p.id, title: p.name, kind: .project(name: p.name), rows: rows)) }
        }
        return HomeModel(machine: deck.machine ?? fallbackName, sections: sections, usage: usageLine(deck.usage))
    }

    static func chatRows(_ deck: Deck, needs: Set<String>) -> [HomeRow] {
        let shown = deck.channels.filter { !$0.dm || $0.yours }
        let ranked = shown.enumerated().map { (index: $0.offset, channel: $0.element, dot: hasDot($0.element, needs: needs)) }
        // Stable: a dot first, then the newest; what ties keeps the server's order.
        let sorted = ranked.sorted { a, b in
            if a.dot != b.dot { return a.dot }
            let (x, y) = (a.channel.lastAt ?? .distantPast, b.channel.lastAt ?? .distantPast)
            if x != y { return x > y }
            return a.index < b.index
        }
        return sorted.map { chatRow($0.channel, dot: $0.dot, deck: deck) }
    }

    /// Something waits on the person in the channel: it asks them, an unread DM, or a member has an
    /// item in the inbox (the page's `chanDot`).
    static func hasDot(_ c: Channel, needs: Set<String>) -> Bool {
        if c.mention || (c.dm && c.unread > 0) { return true }
        let ids = c.team != nil ? (c.team?.roles ?? []).compactMap(\.agent) : c.members.map(\.id)
        return ids.contains { needs.contains($0) }
    }

    static func chatRow(_ c: Channel, dot: Bool, deck: Deck) -> HomeRow {
        let ids = c.team != nil ? (c.team?.roles ?? []).compactMap(\.agent) : c.members.map(\.id)
        let labels = Dictionary(c.members.map { ($0.id, $0.label) }, uniquingKeysWith: { a, _ in a })
        let members = ids.map { id -> HomeMember in
            let agent = deck.agent(id: id)
            return HomeMember(id: id, kind: agent?.kind ?? c.members.first { $0.id == id }?.kind ?? "",
                              state: agent.map { CharacterState(status: $0.state) } ?? .idle,
                              name: labels[id] ?? agent?.label ?? id, preview: agent?.last ?? "")
        }
        return HomeRow(id: c.id, kind: c.dm ? .dm : .channel, name: c.dm ? c.title : c.id, preview: c.lastText,
                       needsYou: dot, members: members)
    }

    static func agentRow(_ a: Agent, deck: Deck, needs: Set<String>) -> HomeRow {
        // An untitled agent has an empty name: the row reads "codex unti0001" from its kind and id.
        let name = (a.name?.isEmpty == false ? a.name : a.title) ?? ""
        return HomeRow(id: a.id, kind: .agent, agentKind: a.kind, name: name, preview: a.last,
                       state: CharacterState(status: a.state), needsYou: needs.contains(a.id), dimmed: isQuiet(a.state))
    }

    /// A stopped, exited or sleeping agent is drawn at half strength.
    static func isQuiet(_ state: String) -> Bool { ["stopped", "exited", "asleep"].contains(state) }

    /// One line for a footer: "Claude 5 小时 42% · 本周 71%" for the first kind that has something.
    public static func usageLine(_ usage: [Usage]) -> String? {
        guard let u = usage.first(where: \.known) else { return nil }
        var parts: [String] = []
        if u.fiveHour.known { parts.append("5 小时 \(Int(u.fiveHour.used.rounded()))%") }
        if u.sevenDay.known { parts.append("本周 \(Int(u.sevenDay.used.rounded()))%") }
        return ([u.title] + [parts.joined(separator: " · ")]).joined(separator: " ")
    }
}
