import Foundation
import MadKit
import MadUI

/// An agent's store and the deck onto MadUI's `AgentConversation`: what the conversation screen draws.
public enum AgentMapping {
    /// The screen for the agent `store` follows, as `deck` last said it stands.
    @MainActor
    public static func conversation(store: AgentStore, deck: Deck) -> AgentConversation {
        conversation(id: store.id, transcript: store.transcript, live: store.live, deck: deck)
    }

    /// The same from plain values, so a test needs no store.
    public static func conversation(id: String, transcript: ChatTranscript, live: LiveState, deck: Deck,
                                    now: Date = Date()) -> AgentConversation {
        let agent = deck.agent(id: id)
        let (canSend, hint) = sending(agent)
        return AgentConversation(
            title: agent?.label ?? id,
            agentID: id,
            kind: agent?.kind ?? "",
            state: agent.map { CharacterState(status: $0.state) } ?? .stopped,
            entries: entries(transcript),
            live: liveBubble(live, transcript: transcript, now: now),
            ask: ask(for: id, deck: deck),
            canSend: canSend,
            sendHint: hint)
    }

    // MARK: entries

    /// The transcript's rows: a tool call with its result under it (paired by id), user and agent text;
    /// commands, thinking and a result with no call are left out, as the page leaves them. Ids are
    /// positions among the rows, which only grow while the transcript does not reset.
    static func entries(_ transcript: ChatTranscript) -> [ConvEntry] {
        var out: [ConvEntry] = []
        if transcript.earlier { out.append(.earlier("earlier", "更早的消息不在这里显示")) }
        for (i, row) in transcript.rows.enumerated() {
            switch row {
            case let .user(e):
                out.append(.user("r\(i)", e.text ?? "", images: images(e)))
            case let .text(e):
                out.append(.text("r\(i)", e.text ?? ""))
                // pictures an agent's message carries follow it
                out += images(e).enumerated().map { ConvEntry(id: "r\(i).\($0.offset)", .image($0.element)) }
            case let .tool(call, result):
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

    /// An agent can take a message from the phone when it runs headless or is already in window mode;
    /// one in a terminal is answered in the terminal.
    static func sending(_ agent: Agent?) -> (Bool, String) {
        guard let agent else { return (false, "这个 agent 已经不在了") }
        if agent.headless || agent.inWindowMode { return (true, "") }
        return (false, "这个 agent 在终端里运行，回复请到 Deck 里的终端")
    }

    // MARK: the ask

    /// The agent's top blocking item among what waits on the person (the inbox lists blocking first, oldest first).
    static func item(for id: String, deck: Deck) -> InboxItem? {
        let mine = deck.inbox.waiting.filter { $0.agent == id }
        return mine.first { $0.blocks } ?? mine.first
    }

    static func ask(for id: String, deck: Deck) -> AskCard? {
        guard let item = item(for: id, deck: deck) else { return nil }
        let acts = item.acts.filter { !$0.isOpen }.map {
            AskAct(id: $0.id, label: $0.label, primary: $0.primary, input: $0.input)
        }
        guard !acts.isEmpty else { return nil }
        let title = item.context.flatMap { $0.isEmpty ? nil : $0 } ?? item.title
        var detail = item.text ?? ""
        if item.full, let body = item.body, !body.isEmpty { detail += (detail.isEmpty ? "" : "\n") + body }
        return AskCard(id: item.key, title: item.kind == "permission" ? "想用 \(title)" : title,
                       detail: detail, mono: item.mono, acts: acts)
    }

    /// What an act of the card sends back: the item's key and the act it was made from. Nil when the
    /// item is gone or answered meanwhile.
    public static func answer(actID: String, text: String?, agent id: String, deck: Deck)
        -> (key: String, verb: String, option: String?, text: String?)? {
        guard let item = item(for: id, deck: deck), let act = item.acts.first(where: { $0.id == actID }) else { return nil }
        return (item.key, act.verb, act.option, text.flatMap { $0.isEmpty ? nil : $0 })
    }
}
