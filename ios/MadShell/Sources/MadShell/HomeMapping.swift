import Foundation
import MadKit
import MadUI

/// MadKit's `Deck` onto MadUI's `HomeModel`: what the home list and the Today page draw, as the approved design (v5)
/// draws them.
public enum HomeMapping {
    /// The list for `deck`:
    /// - what waits on the person on top (`waiting`), oldest first, as the inbox lists it; an agent that waits is
    ///   there, not in its project;
    /// - "群聊": the channels and the person's DMs, the ones with a dot first, then the newest message first (the
    ///   page's order);
    /// - then a section per project, its git and cost on its head, with the agents that are not already in a team's
    ///   chat (a team with a channel is one row of the chats) and do not wait.
    public static func model(from deck: Deck, fallbackName: String = "mad", now: Date = Date()) -> HomeModel {
        let needs = Set(deck.inbox.waiting.compactMap(\.agent))
        let teamed = Set(deck.channels.compactMap(\.team).flatMap(\.roles).compactMap(\.agent))
        let waiting = waitingCards(deck, now: now)
        let pinned = Set(waiting.map(\.agentID))

        var sections: [HomeSection] = []
        let chats = chatRows(deck, needs: needs)
        if !chats.isEmpty { sections.append(HomeSection(id: "chats", title: L("群聊"), kind: .chats, rows: chats)) }
        for p in deck.projects {
            let rows = p.agents.filter { !teamed.contains($0.id) && !pinned.contains($0.id) }
                .map { agentRow($0, deck: deck, needs: needs, now: now) }
            if !rows.isEmpty {
                sections.append(HomeSection(id: p.id, title: p.name, kind: .project(name: p.name), rows: rows, meta: meta(p)))
            }
        }
        return HomeModel(machine: deck.machine ?? fallbackName, sections: sections, usage: usageLine(deck.usage),
                         waiting: waiting, today: today(deck, waiting: waiting.count, now: now))
    }

    /// A project's head line: its branch, files changed, commits not pushed, and what its agents have cost.
    static func meta(_ p: Project) -> HomeProjectMeta {
        HomeProjectMeta(branch: p.branch ?? "", changed: p.changed, ahead: p.ahead, cost: p.agents.reduce(0.0) { $0 + $1.cost })
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
        // everyone in the channel counts, the team's roles and the helpers it brought in
        let ids = (c.team?.roles ?? []).compactMap(\.agent) + c.members.map(\.id)
        return ids.contains { needs.contains($0) }
    }

    /// What a channel row says when there is no message to show (the server leaves the agents' bookkeeping out of the
    /// last line, so a channel that has only that has none): how many wait and work, or that it is quiet.
    static func activityLine(_ members: [HomeMember], dm: Bool, paused: Bool) -> String {
        if paused { return L("已暂停") }
        let waiting = members.filter { $0.state == .needsYou }.count
        let working = members.filter { $0.state == .working }.count
        if dm { return waiting > 0 ? L("在等你") : working > 0 ? L("在工作") : L("空闲") }
        let parts = [waiting > 0 ? Lf("%lld 个在等你", waiting) : "", working > 0 ? Lf("%lld 个在工作", working) : ""].filter { !$0.isEmpty }
        return parts.isEmpty ? L("空闲") : parts.joined(separator: " · ")
    }

    static func chatRow(_ c: Channel, dot: Bool, deck: Deck) -> HomeRow {
        let ids = c.team != nil ? (c.team?.roles ?? []).compactMap(\.agent) : c.members.map(\.id)
        let labels = Dictionary(c.members.map { ($0.id, $0.label) }, uniquingKeysWith: { a, _ in a })
        func member(_ id: String) -> HomeMember {
            let agent = deck.agent(id: id)
            return HomeMember(id: id, kind: agent?.kind ?? c.members.first { $0.id == id }?.kind ?? "",
                              state: agent.map { CharacterState(status: $0.state) } ?? .idle,
                              name: labels[id] ?? agent?.label ?? id, preview: agent?.last ?? "")
        }
        let members = ids.map(member)
        // a team channel's other members whose agent exists: out of the tile, in the state
        let helpers = c.team == nil ? [] : c.members.map(\.id).filter { id in !ids.contains(id) && deck.agent(id: id) != nil }.map(member)
        return HomeRow(id: c.id, kind: c.dm ? .dm : .channel, name: c.dm ? c.title : c.id,
                       preview: c.lastText.isEmpty ? activityLine(members + helpers, dm: c.dm, paused: c.paused) : c.lastText,
                       needsYou: dot, members: members, helpers: helpers, unread: c.unread)
    }

    static func agentRow(_ a: Agent, deck: Deck, needs: Set<String>, now: Date = Date()) -> HomeRow {
        // An untitled agent has an empty name: the row reads "codex unti0001" from its kind and id.
        let name = (a.name?.isEmpty == false ? a.name : a.title) ?? ""
        let state = CharacterState(status: a.state)
        // working: "**Bash** go test ./..." and how long; otherwise its last line
        var tool = "", preview = a.last, age = ""
        if state == .working {
            if let t = a.tool, !t.isEmpty {
                tool = t
                preview = a.target ?? ""
            }
            if let since = a.since { age = MadWords.age(now.timeIntervalSince(since)) }
        }
        return HomeRow(id: a.id, kind: .agent, agentKind: a.kind, name: name, preview: preview,
                       state: state, needsYou: needs.contains(a.id), dimmed: isQuiet(a.state), tool: tool, age: age)
    }

    /// A stopped, exited or sleeping agent is drawn at half strength.
    static func isQuiet(_ state: String) -> Bool { ["stopped", "exited", "asleep"].contains(state) }

    // MARK: what waits

    /// What waits on the person, oldest first (the inbox's order): one card for each agent's top item (its blocking one
    /// first), and the items that are no agent's (a mention, the budget) after.
    static func waitingCards(_ deck: Deck, now: Date) -> [HomeWaiting] {
        var seen = Set<String>()
        var out: [HomeWaiting] = []
        let items = deck.inbox.waiting
        for item in items {
            let acts = ActWords.acts(of: item)
            if let id = item.agent, !id.isEmpty {
                guard !seen.contains(id) else { continue }
                seen.insert(id)
                // the agent's blocking item, when it has one, is the one its card shows
                let top = items.first { $0.agent == id && $0.blocks } ?? item
                out.append(card(top, agent: deck.agent(id: id), deck: deck, now: now))
            } else if !acts.isEmpty {
                out.append(card(item, agent: nil, deck: deck, now: now))
            }
        }
        return out
    }

    static func card(_ item: InboxItem, agent: Agent?, deck: Deck, now: Date) -> HomeWaiting {
        let project = agent.flatMap { a in deck.projects.first { $0.agents.contains { $0.id == a.id } }?.name } ?? ""
        let what: String
        switch item.kind {
        case "permission": what = item.title == "Bash" || item.mono ? L("想运行一条命令") : Lf("想用 %@", item.title)
        case "question": what = L("问了一个问题")
        default: what = item.context.flatMap { $0.isEmpty ? nil : $0 } ?? item.title
        }
        let whereLine = [project, agent?.kind ?? "", what].filter { !$0.isEmpty }.joined(separator: " · ")
        var detail = item.text ?? ""
        if detail.isEmpty { detail = item.body ?? item.title }
        if item.mono, !detail.hasPrefix("$ ") { detail = "$ " + detail }
        return HomeWaiting(id: item.key, agentID: agent?.id ?? item.open.agent ?? item.open.channel ?? "",
                           agentKind: agent?.kind ?? "", name: agent?.label ?? item.title, project: project,
                           whereLine: whereLine, age: item.since.map { MadWords.age(now.timeIntervalSince($0)) } ?? "",
                           cost: (agent?.cost ?? 0) > 0 ? MadWords.money(agent?.cost ?? 0) : "",
                           detail: detail, mono: item.mono, acts: ActWords.acts(of: item))
    }

    // MARK: Today

    /// The Today page: the date, the counts, what all the agents have cost, who works (longest at it first) and who
    /// just finished (the inbox's finished items, then agents whose run finished unseen).
    static func today(_ deck: Deck, waiting: Int, now: Date) -> TodayModel {
        let agents = deck.agents
        let working = agents.filter { CharacterState(status: $0.state) == .working }
            .sorted { ($0.since ?? now) < ($1.since ?? now) }
        let finishedIDs = deck.inbox.finished.compactMap(\.agent)
        var done: [Agent] = finishedIDs.compactMap { deck.agent(id: $0) }
        for a in agents where CharacterState(status: a.state) == .done && !done.contains(where: { $0.id == a.id }) { done.append(a) }
        let total = agents.reduce(0.0) { $0 + $1.cost }
        func place(_ a: Agent) -> String {
            guard let team = a.team, !team.isEmpty else { return "" }
            return deck.channels.first { $0.team?.name == team }.map { "#" + $0.id } ?? ""
        }
        let workLines = working.map { a in
            TodayLine(id: a.id, kind: a.kind, state: .working, name: a.label, place: place(a), tool: a.tool ?? "",
                      detail: (a.tool ?? "").isEmpty ? a.last : (a.target ?? ""),
                      number: [a.since.map { MadWords.age(now.timeIntervalSince($0)) } ?? "", a.cost > 0 ? MadWords.money(a.cost) : ""]
                        .filter { !$0.isEmpty }.joined(separator: " · "))
        }
        let doneLines = done.map { a in
            TodayLine(id: a.id, kind: a.kind, state: .done, name: a.label, place: place(a), detail: a.last,
                      number: a.since.map { MadWords.ago(now.timeIntervalSince($0)) } ?? "")
        }
        return TodayModel(date: MadWords.today(now), waiting: waiting, working: working.count, done: done.count,
                          spend: total > 0 ? MadWords.money(total) : "", workingLines: workLines, doneLines: doneLines)
    }

    /// One line of usage: "Claude 5 小时 42% · 本周 71%" for the first kind that has something.
    public static func usageLine(_ usage: [Usage]) -> String? {
        guard let u = usage.first(where: \.known) else { return nil }
        var parts: [String] = []
        if u.fiveHour.known { parts.append(Lf("5 小时 %lld%%", Int(u.fiveHour.used.rounded()))) }
        if u.sevenDay.known { parts.append(Lf("本周 %lld%%", Int(u.sevenDay.used.rounded()))) }
        return ([u.title] + [parts.joined(separator: " · ")]).joined(separator: " ")
    }
}
