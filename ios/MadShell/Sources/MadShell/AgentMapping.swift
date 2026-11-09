import Foundation
import MadKit
import MadUI

/// An agent's store and the deck onto MadUI's `AgentConversation`: what the conversation screen draws.
/// The first message the person sent when they started an agent, and when.
public struct PendingFirst: Equatable, Sendable {
    public var text: String
    public var since: Date
    public init(text: String, since: Date) { self.text = text; self.since = since }
}

public enum AgentMapping {
    /// How long after the start an agent with no transcript at all is shown as working on the first message: past it,
    /// one that wrote nothing (it exited) shows as what it is.
    static let startingWindow: TimeInterval = 30

    /// The screen for the agent `store` follows, as `deck` last said it stands.
    @MainActor
    public static func conversation(store: AgentStore, deck: Deck, pending: PendingFirst? = nil) -> AgentConversation {
        conversation(id: store.id, transcript: store.transcript, live: store.live, deck: deck, pending: pending)
    }

    /// The same from plain values, so a test needs no store.
    /// `pending` is the first message the person sent when they started the agent: until the transcript has a message of
    /// theirs it is shown as their bubble, and the agent as working, so the page never opens empty.
    public static func conversation(id: String, transcript: ChatTranscript, live: LiveState, deck: Deck,
                                    pending: PendingFirst? = nil, now: Date = Date()) -> AgentConversation {
        let agent = deck.agent(id: id)
        let (canSend, hint) = sending(agent)
        let project = deck.projects.first { $0.agents.contains { $0.id == id } }
        let place = project.map { p in [p.name, p.branch ?? ""].filter { !$0.isEmpty }.joined(separator: " / ") } ?? ""
        let cost = (agent?.cost ?? 0) > 0 ? MadWords.money(agent?.cost ?? 0) : ""
        var state = agent.map { CharacterState(status: $0.state) } ?? .stopped
        var shown = entries(transcript, deck: deck)
        if let first = pendingEntry(pending, transcript: transcript) {
            shown.insert(first, at: 0)
        }
        // working while it has not written anything yet, for a short while: one that exited before its first line
        // is not shown as working for ever
        if let pending, transcript.entries.isEmpty, now.timeIntervalSince(pending.since) < startingWindow,
           [.idle, .starting, .stopped].contains(state) {
            state = .working
        }
        return AgentConversation(
            title: agent?.label ?? id,
            agentID: id,
            kind: agent?.kind ?? "",
            state: state,
            entries: shown,
            live: liveBubble(live, transcript: transcript, now: now),
            ask: ask(for: id, deck: deck, now: now),
            create: CreateMapping.waiting(agent: id, deck: deck).flatMap { CreateMapping.card($0, deck: deck, now: now) },
            canSend: canSend,
            sendHint: hint,
            place: place,
            cost: cost,
            details: agent.map { a in
                AgentDetails(since: a.created.map { MadWords.age(now.timeIntervalSince($0)) } ?? "", branch: project?.branch ?? "",
                             changed: project?.changed ?? 0, ahead: project?.ahead ?? 0, cost: cost, tokens: tokens(a),
                             model: a.model, session: a.session.isEmpty ? a.id : a.session,
                             directory: shortPath(a.dir.isEmpty ? (project?.path ?? "") : a.dir))
            })
    }

    /// The person's first message as a bubble, while the transcript has none of theirs yet (it is theirs from then on, so
    /// the bubble is never twice).
    /// A slash command is written as a "command" entry, not a "user" one: it ends the bubble too.
    static func pendingEntry(_ pending: PendingFirst?, transcript: ChatTranscript) -> ConvEntry? {
        guard let pending, !pending.text.isEmpty,
              !transcript.entries.contains(where: { $0.kind == "user" || $0.kind == "command" }) else { return nil }
        return .user("pending", pending.text, images: [])
    }

    /// Tokens in and out as the Details pane says them: "182k / 9.4k"; empty when there are none.
    static func tokens(_ a: Agent) -> String {
        guard a.tokensIn > 0 || a.tokensOut > 0 else { return "" }
        return "\(count(a.tokensIn)) / \(count(a.tokensOut))"
    }

    /// A count of tokens, short: 950, 9.4k, 182k, 1.2M.
    static func count(_ n: Int) -> String {
        if n < 1000 { return "\(n)" }
        if n < 10_000 { return String(format: "%.1fk", Double(n) / 1000) }
        if n < 1_000_000 { return "\(Int((Double(n) / 1000).rounded()))k" }
        return String(format: "%.1fM", Double(n) / 1_000_000)
    }

    // MARK: finishing a branch

    /// The finish menu as buttons, in the page's words: push 推送, pr 推送并开 PR (the primary), rebase 变基到 base,
    /// merge 合并到 base; the person's own keep their names.
    public static func finishButtons(_ menu: FinishMenu) -> [FinishButton] {
        let base = menu.base.isEmpty ? "main" : menu.base
        return menu.actions.enumerated().map { i, a in
            FinishButton(index: i, name: a.name, label: finishLabel(a, base: base), primary: a.kind == "pr")
        }
    }

    static func finishLabel(_ a: FinishAction, base: String) -> String {
        switch a.kind {
        case "push": return L("推送")
        case "pr": return L("推送并开 PR")
        case "rebase": return Lf("变基到 %@", base)
        case "merge": return Lf("合并到 %@", base)
        default: return a.name
        }
    }

    /// What a finish run says afterwards: "推送 · <its last line>", or "推送: <what went wrong>".
    public static func finishNote(_ b: FinishButton, _ r: FinishResult) -> String {
        let last = r.lastLine
        return r.ok ? b.label + " · " + (last.isEmpty ? L("完成") : last) : b.label + ": " + (last.isEmpty ? L("没有成功") : last)
    }

    /// A path with the home folder as ~ ("~/src/mad").
    static func shortPath(_ path: String) -> String {
        let home = NSHomeDirectory()
        if !home.isEmpty, path.hasPrefix(home + "/") { return "~" + path.dropFirst(home.count) }
        // a Mac's home seen from the phone: /Users/<name>/...
        let parts = path.split(separator: "/", omittingEmptySubsequences: true)
        if parts.count > 2, parts[0] == "Users" { return "~/" + parts.dropFirst(2).joined(separator: "/") }
        return path
    }

    // MARK: entries

    /// The transcript's rows: a tool call with its result under it (paired by id), user and agent text;
    /// commands, thinking and a result with no call are left out, as the page leaves them. Ids are
    /// positions among the rows, which only grow while the transcript does not reset.
    static func entries(_ transcript: ChatTranscript, deck: Deck? = nil) -> [ConvEntry] {
        var out: [ConvEntry] = []
        if transcript.earlier { out.append(.earlier("earlier", L("更早的消息不在这里显示"))) }
        for (i, row) in transcript.rows.enumerated() {
            switch row {
            case let .user(e):
                // mad's outcome message to the bot is plumbing: the result line says it
                if CreateMapping.isOutcomeDM(e.text ?? "") { continue }
                out.append(.user("r\(i)", e.text ?? "", images: images(e)))
            case let .text(e):
                out.append(.text("r\(i)", e.text ?? ""))
                // pictures an agent's message carries follow it
                out += images(e).enumerated().map { ConvEntry(id: "r\(i).\($0.offset)", .image($0.element)) }
            case let .tool(call, result):
                // agent_new / team_start: mad's own line from the request's outcome, never the tool's text. Nothing while there
                // is no outcome (the card is the place) or the item is gone without one; an error stays an error step.
                if CreateMapping.isCreateTool(call.tool ?? ""), let result, !(result.error ?? false), let deck {
                    if let line = CreateMapping.line(toolResult: result.text ?? "", step: "r\(i)", deck: deck) { out.append(ConvEntry(id: "r\(i)", .create(line))) }
                    continue
                }
                out.append(.tool("r\(i)", ConvTool(
                    name: call.tool ?? "tool", label: call.label ?? "", input: call.input ?? "",
                    result: result?.text ?? (result == nil ? nil : ""), error: result?.error ?? call.error)))
            }
        }
        return out
    }

    static func images(_ e: ChatEntry) -> [ConvImage] {
        e.images.map { ConvImage(alt: "image", source: $0.url ?? "") }
    }

    /// The text blocks of the message being written, while the transcript has no entry for it.
    static func liveBubble(_ live: LiveState, transcript: ChatTranscript, now: Date) -> LiveBubble? {
        guard let m = live.visible(in: transcript, now: now) else { return nil }
        let text = m.blocks.filter { $0.kind == "text" }.compactMap(\.text).joined(separator: "\n\n")
        // an ended message that wrote no text (a tool call only) is not a bubble
        if m.done && text.isEmpty { return nil }
        return LiveBubble(markdown: text)
    }

    // MARK: sending

    /// An agent can take a message from the phone only in window mode (the server marks ordinary terminal
    /// agents headless too); one in a terminal is answered in the terminal.
    static func sending(_ agent: Agent?) -> (Bool, String) {
        guard let agent else { return (false, L("这个 agent 已经不在了")) }
        if agent.inWindowMode { return (true, "") }
        return (false, L("这个 agent 在终端里运行，回复请到 Deck 里的终端"))
    }

    // MARK: the ask

    /// The agent's top blocking item among what waits on the person (the inbox lists blocking first, oldest first).
    static func item(for id: String, deck: Deck) -> InboxItem? {
        // a create request has its own card
        let mine = deck.inbox.waiting.filter { $0.agent == id && $0.kind != "create" }
        return mine.first { $0.blocks } ?? mine.first
    }

    static func ask(for id: String, deck: Deck, now: Date = Date()) -> AskCard? {
        guard let item = item(for: id, deck: deck) else { return nil }
        let acts = ActWords.acts(of: item)
        guard !acts.isEmpty else { return nil }
        let title = item.context.flatMap { $0.isEmpty ? nil : $0 } ?? item.title
        var detail = item.text ?? ""
        if item.full, let body = item.body, !body.isEmpty { detail += (detail.isEmpty ? "" : "\n") + body }
        let permission = item.kind == "permission"
        // a command is "想运行一条命令" (the card's head), any other tool "想用 Edit"
        let head = permission ? (title == "Bash" || item.mono ? L("想运行一条命令") : Lf("想用 %@", title)) : title
        if permission && item.mono && !detail.isEmpty && !detail.hasPrefix("$ ") { detail = "$ " + detail }
        return AskCard(id: item.key, title: head, detail: detail, mono: item.mono, acts: acts,
                       waited: item.since.map { Lf("等了 %@", MadWords.age(now.timeIntervalSince($0))) } ?? "",
                       permission: permission)
    }

    /// What an act of the card sends back: the item's key and the act it was made from. Nil when the
    /// item is gone, answered meanwhile, or no longer the one the card `cardID` showed (a replacement
    /// prompt must not take a stale card's Allow).
    public static func answer(cardID: String, actID: String, text: String?, agent id: String, deck: Deck)
        -> (key: String, verb: String, option: String?, text: String?)? {
        let waiting = deck.inbox.waiting.first { $0.agent == id && $0.key == cardID && $0.kind == "create" }
        guard let item = waiting ?? item(for: id, deck: deck), item.key == cardID, let act = item.acts.first(where: { $0.id == actID }) else { return nil }
        return (item.key, act.verb, act.option, text.flatMap { $0.isEmpty ? nil : $0 })
    }
}
