import Foundation
import MadKit
import MadUI

/// A bot's request to create an agent or a team, onto what the native screens draw: the confirm card, mad's result line
/// and the grey lines. Every word goes by the request's `type`, the act's verb and the outcome's status, never by the
/// server's English (`context`, `text`).
public enum CreateMapping {
    /// The tool names of mad's MCP: claude prefixes them `mcp__mad__`, other kinds have their own prefixes.
    static func isCreateTool(_ name: String) -> Bool { name.hasSuffix("agent_new") || name.hasSuffix("team_start") }

    /// mad's outcome DM to the asking bot ("[mad] your request c-1 to create …"): plumbing, the result line already says it.
    static func isOutcomeDM(_ text: String) -> Bool { text.hasPrefix("[mad] your request c-") }

    // MARK: the card

    /// The confirm card of a waiting create item: only where the whole spec shows (name, project, roles or model and channel, the
    /// first task or message). `inChannel` puts the asker's label in the head.
    public static func card(_ item: InboxItem, deck: Deck, inChannel: Bool = false, now: Date = Date()) -> CreateCard? {
        guard item.kind == "create", let spec = item.create else { return nil }
        let asker = item.title
        let head: String
        if spec.isTeam { head = inChannel ? Lf("%@ 想创建一个团队", asker) : L("想创建一个团队") }
        else { head = inChannel ? Lf("%@ 想创建一个 agent", asker) : L("想创建一个 agent") }
        var rows: [CreateCard.Row] = []
        if spec.isTeam {
            for (i, r) in spec.roles.enumerated() {
                let plain = r.role == "负责人" || r.role.lowercased() == "lead"
                rows.append(.init(id: "role\(i)", left: r.role, tag: r.lead && !plain ? L("负责人") : "", right: modelWords(kind: r.kind, model: r.model)))
            }
        } else {
            rows.append(.init(id: "model", left: L("模型"), right: modelWords(kind: spec.kind, model: spec.model)))
            if !spec.channel.isEmpty { rows.append(.init(id: "channel", left: L("加入群聊"), right: spec.channel)) }
        }
        let agent = deck.agent(id: item.agent)
        let waited: String
        if let since = item.since {
            let seconds = now.timeIntervalSince(since)
            waited = seconds < 60 ? L("刚刚") : Lf("等了 %@", MadWords.age(seconds))
        } else { waited = "" }
        return CreateCard(
            id: item.key, head: head, askerID: item.agent ?? "", askerKind: agent?.kind ?? "", name: spec.name,
            project: spec.projectName.isEmpty ? "" : Lf("项目 %@", spec.projectName), rows: rows,
            firstLabel: spec.isTeam ? L("第一件事") : L("第一句话"),
            first: spec.isTeam ? spec.goal : spec.message, waited: waited,
            allow: item.acts.first { $0.verb == "allow" && !$0.input && !$0.row }?.id,
            deny: item.acts.first { $0.verb == "deny" && $0.row && !$0.input }?.id,
            change: item.acts.first { $0.verb == "deny" && $0.input && $0.kind == "create" }?.id)
    }

    /// The waiting create item of agent `id` (the asking bot's conversation), else the first one that came from channel `channel`.
    static func waiting(agent id: String? = nil, channel: String? = nil, deck: Deck) -> InboxItem? {
        deck.inbox.waiting.first { item in
            guard item.kind == "create", item.create != nil else { return false }
            if let id { return item.agent == id }
            if let channel { return item.from == channel }
            return false
        }
    }

    // MARK: model words

    /// "Claude", "Codex", "Pi": a kind as the app shows it.
    static func kindName(_ kind: String) -> String {
        switch kind {
        case "claude": return "Claude"
        case "codex": return "Codex"
        case "pi": return "Pi"
        case "agy": return "Agy"
        case "": return ""
        default: return kind.prefix(1).uppercased() + kind.dropFirst()
        }
    }

    /// The kind alone, or with its model: an alias is capitalised ("Claude Sonnet"), a full model id is shown as given ("Codex gpt-5.5").
    public static func modelWords(kind: String, model: String) -> String {
        let name = kindName(kind)
        guard !model.isEmpty else { return name }
        let alias = !model.contains("-") && !model.contains(".") && !model.contains(where: \.isNumber)
        let m = alias ? model.prefix(1).uppercased() + model.dropFirst() : model
        return name.isEmpty ? m : name + " " + m
    }

    // MARK: result lines

    /// The line for a tool step's result: `{status: "asked", request, item: "create:c-…"}` looks the item up in the deck's
    /// `creates` and draws its outcome (nil while it has none); `{status: "created", …}` (made without asking) is drawn at once.
    static func line(toolResult text: String, step: String, deck: Deck) -> CreateLine? {
        guard let data = text.data(using: .utf8), let json = try? JSONSerialization.jsonObject(with: data) as? [String: Any] else { return nil }
        let status = json["status"] as? String ?? json["outcome"] as? String ?? ""
        if let key = json["item"] as? String, let item = deck.creates.first(where: { $0.key == key }) { return line(item, deck: deck) }
        guard status == "created" else { return nil }
        let d = json["data"] as? [String: Any] ?? json
        func str(_ k: String) -> String { d[k] as? String ?? json[k] as? String ?? "" }
        return line(id: step, status: "created", isTeam: !str("team").isEmpty, name: str("name"), team: str("team"), agent: str("agent"),
                    channel: str("channel"), reason: "", words: "", deck: deck)
    }

    /// mad's line for an item that was answered or is being made (`creates` of the deck), from its outcome.
    public static func line(_ item: InboxItem, deck: Deck) -> CreateLine? {
        guard let spec = item.create, let o = item.outcome else { return nil }
        return line(id: item.key, status: o.status, isTeam: spec.isTeam, name: spec.name, team: o.team, agent: o.agent, channel: o.channel,
                    reason: o.reason, words: o.reply, deck: deck, spec: spec)
    }

    /// mad's line for an event line of a channel (`created`, `create_declined` ...), from its `data`; nil for any other event.
    public static func line(event m: BusMessage, deck: Deck?) -> CreateLine? {
        let status: String
        switch m.event {
        case "created": status = "created"
        case "create_declined": status = "declined"
        case "create_changes": status = "changes"
        case "create_expired": status = "expired"
        case "create_failed": status = "failed"
        default: return nil
        }
        let d = m.data
        return line(id: m.ref.isEmpty ? "e\(m.seq)" : m.ref, status: status, isTeam: d["type"] == "team", name: d["name"] ?? "",
                    team: d["team"] ?? "", agent: d["agent"] ?? "", channel: d["channel"] ?? "", reason: d["reason"] ?? "",
                    words: d["words"] ?? "", deck: deck)
    }

    /// `spec` is the request, when known: what a made thing that is gone is drawn from (its characters, stopped).
    static func line(id: String, status: String, isTeam: Bool, name: String, team: String, agent: String, channel: String,
                     reason: String, words: String, deck: Deck?, spec: CreateSpec? = nil) -> CreateLine? {
        func note(_ text: String) -> CreateLine { CreateLine(id: id, phase: .note, title: text) }
        switch status {
        case "creating":
            return CreateLine(id: id, phase: .creating, title: isTeam ? Lf("正在创建团队「%@」…", name) : Lf("正在创建「%@」…", name))
        case "declined":
            return note(isTeam ? Lf("你拒绝了，没有创建团队「%@」", name) : Lf("你拒绝了，没有创建「%@」", name))
        case "changes":
            return note(Lf("你要改一下：%@", words))
        case "expired":
            return note(isTeam ? Lf("没等到你确认，没有创建团队「%@」", name) : Lf("没等到你确认，没有创建「%@」", name))
        case "failed":
            return note(isTeam ? Lf("没建成团队「%@」：%@", name, reason) : Lf("没建成「%@」：%@", name, reason))
        case "created":
            let title = isTeam ? Lf("团队「%@」建好了", name) : Lf("「%@」建好了", name)
            if isTeam {
                guard let channel = deck?.channel(id: channel.isEmpty ? team : channel) else {
                    // gone: the tile of its roles' characters, stopped (a request's roles are in `spec`; without it, one for the name)
                    let roles = spec?.roles ?? []
                    let members: [CreateLine.Member] = roles.isEmpty
                        ? [.init(id: name, kind: "claude", state: .stopped)]
                        : roles.map { .init(id: $0.role, kind: $0.kind.isEmpty ? "claude" : $0.kind, state: .stopped) }
                    return CreateLine(id: id, phase: .created, title: title, sub: L("已经不在了"), mark: .team(members: members))
                }
                let split = ChannelMapping.split(channel: channel, deck: deck)
                let members = split.team.filter { !$0.dim }
                let lead = split.team.first { $0.isLead && !$0.dim }
                let sub = Lf("%lld 个成员", members.count) + " · "
                    + (lead.map { Lf("负责人%@", $0.state.title) } ?? (deck?.projects.first { p in p.agents.contains { a in members.contains { $0.id == a.id } } }?.name ?? ""))
                return CreateLine(id: id, phase: .created, title: title, sub: sub,
                                  mark: .team(members: members.map { .init(id: $0.id, kind: $0.kind, state: $0.state) }),
                                  target: .channel(channel.id))
            }
            guard let a = deck?.agent(id: agent) else {
                return CreateLine(id: id, phase: .created, title: title, sub: L("已经不在了"),
                                  mark: .agent(id: agent.isEmpty ? name : agent, kind: spec?.kind.isEmpty == false ? spec!.kind : "claude", state: .stopped))
            }
            let state = CharacterState(status: a.state)
            let sub = [modelWords(kind: a.kind, model: a.model), state.title].filter { !$0.isEmpty }.joined(separator: " · ")
            return CreateLine(id: id, phase: .created, title: title, sub: sub, mark: .agent(id: a.id, kind: a.kind, state: state),
                              target: .agent(a.id))
        default:
            return nil
        }
    }
}
